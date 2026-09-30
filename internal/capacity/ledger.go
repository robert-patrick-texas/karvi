// Package capacity coordinates credential-free server and per-device leases.
// The on-disk ledger is protected by flock and reaps only verifiably dead
// process identities.
package capacity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

type Manager struct {
	Root, JobID      string
	ServerLimit      int
	PollMin, PollMax time.Duration
	Warn             func(string)
}
type Lease struct {
	manager                        *Manager
	serverID, deviceID, devicePath string
	once                           bool
}
type entry struct {
	ID, JobID, BootID, Start string
	PID                      int
	Created                  time.Time
}

func New(root, fallback, jobID string, limit int, warn func(string)) (*Manager, error) {
	if limit < 1 {
		limit = 32
	}
	chosen := root
	if err := ensureRoot(chosen); err != nil {
		if fallback == "" {
			return nil, err
		}
		if warn != nil {
			warn(fmt.Sprintf("shared capacity root unavailable (%v); using private fallback %s", err, fallback))
		}
		chosen = fallback
		if err := ensureRoot(chosen); err != nil {
			return nil, err
		}
	}
	return &Manager{Root: chosen, JobID: jobID, ServerLimit: limit, PollMin: 50 * time.Millisecond, PollMax: 250 * time.Millisecond, Warn: warn}, nil
}
func ensureRoot(root string) error {
	if root == "" {
		return errorcodes.Errorf("capacity_root_blank", "capacity root is blank")
	}
	if err := os.MkdirAll(filepath.Join(root, "devices"), 0700); err != nil {
		return err
	}
	return nil
}
func (m *Manager) Acquire(ctx context.Context, device string, cap int) (*Lease, error) {
	if cap < 1 {
		cap = 1
	}
	deviceSum := sha256.Sum256([]byte(device))
	devicePath := filepath.Join(m.Root, "devices", hex.EncodeToString(deviceSum[:]))
	for {
		serverID, ok, err := m.try(filepath.Join(m.Root, "server"), m.ServerLimit)
		if err != nil {
			return nil, err
		}
		if !ok {
			if err := m.wait(ctx); err != nil {
				return nil, err
			}
			continue
		}
		deviceID, dok, err := m.try(devicePath, cap)
		if err != nil {
			_ = m.release(filepath.Join(m.Root, "server"), serverID)
			return nil, err
		}
		if dok {
			return &Lease{manager: m, serverID: serverID, deviceID: deviceID, devicePath: devicePath}, nil
		}
		_ = m.release(filepath.Join(m.Root, "server"), serverID)
		if err := m.wait(ctx); err != nil {
			return nil, err
		}
	}
}
func (m *Manager) wait(ctx context.Context) error {
	d := m.PollMin
	if d <= 0 {
		d = 50 * time.Millisecond
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
func (m *Manager) try(path string, limit int) (string, bool, error) {
	var id string
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", false, err
	}
	id = hex.EncodeToString(raw)
	ok := false
	err := withLedger(path, func(entries []entry) ([]entry, error) {
		entries = reap(entries)
		if len(entries) >= limit {
			return entries, nil
		}
		entries = append(entries, entry{ID: id, JobID: m.JobID, BootID: osutil.BootID(), PID: os.Getpid(), Start: osutil.ProcessStartIdentity(os.Getpid()), Created: time.Now()})
		ok = true
		return entries, nil
	})
	return id, ok, err
}
func (l *Lease) Release() error {
	if l == nil || l.once {
		return nil
	}
	l.once = true
	e1 := l.manager.release(l.devicePath, l.deviceID)
	e2 := l.manager.release(filepath.Join(l.manager.Root, "server"), l.serverID)
	if e1 != nil {
		return e1
	}
	return e2
}
func (m *Manager) release(path, id string) error {
	return withLedger(path, func(entries []entry) ([]entry, error) {
		out := entries[:0]
		for _, e := range entries {
			if e.ID != id {
				out = append(out, e)
			}
		}
		return out, nil
	})
}
func reap(entries []entry) []entry {
	boot := osutil.BootID()
	out := entries[:0]
	for _, e := range entries {
		if e.BootID == boot && osutil.ProcessAlive(e.PID, e.Start) {
			out = append(out, e)
		}
	}
	return out
}
func withLedger(path string, fn func([]entry) ([]entry, error)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	entries := []entry{}
	if b, err := os.ReadFile(path + ".json"); err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, &entries); err != nil {
			return errorcodes.Errorf("capacity_ledger_malformed", "malformed capacity ledger %s: %w", path, err)
		}
	}
	next, err := fn(entries)
	if err != nil {
		return err
	}
	// Indented like every .json file karvi writes; the ledger is small, and
	// its cost is the rename and the lock.
	data, err := json.MarshalIndent(next, "", osutil.JSONIndent)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ledger-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path+".json")
}

// Assessment is a read-only view of the ledgers:
// the server limit and its live entries, and each asked device's cap and
// live entries. Nothing is appended and no lease is taken.
type Assessment struct {
	ServerLimit int
	ServerInUse int
	Devices     map[string]DeviceAssessment
}

// DeviceAssessment is one device ledger's cap and live entries.
type DeviceAssessment struct {
	Cap   int
	InUse int
}

// Assess reads the server ledger and the ledger of every device in caps
// under a shared lock with the reaper applied.
func (m *Manager) Assess(caps map[string]int) (Assessment, error) {
	a := Assessment{ServerLimit: m.ServerLimit, Devices: map[string]DeviceAssessment{}}
	n, err := readLedger(filepath.Join(m.Root, "server"))
	if err != nil {
		return a, err
	}
	a.ServerInUse = n
	for device, cap := range caps {
		if cap < 1 {
			cap = 1
		}
		deviceSum := sha256.Sum256([]byte(device))
		n, err := readLedger(filepath.Join(m.Root, "devices", hex.EncodeToString(deviceSum[:])))
		if err != nil {
			return a, err
		}
		a.Devices[device] = DeviceAssessment{Cap: cap, InUse: n}
	}
	return a, nil
}

// readLedger counts a ledger's live entries without writing it.
func readLedger(path string) (int, error) {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_SH); err != nil {
		return 0, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	entries := []entry{}
	if b, err := os.ReadFile(path + ".json"); err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, &entries); err != nil {
			return 0, errorcodes.Errorf("capacity_ledger_malformed", "malformed capacity ledger %s: %w", path, err)
		}
	}
	return len(reap(entries)), nil
}
