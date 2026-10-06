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
	// dirMode and fileMode are what the ledger creates under Root
	// (ledgerModes).
	dirMode, fileMode os.FileMode
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

// New resolves the ledger's root (sessions.shared-capacity-root, raw; home
// the operator's, base the private root) and prepares it: under "auto"
// the scratch root's capacity folder where the scratch root exists, passed
// by with one warning when this operator cannot lease in it; else, or
// then, the private <basedir>/state/capacity. An explicit path is used or
// refused.
func New(raw, home, base, jobID string, limit int, warn func(string)) (*Manager, error) {
	if limit < 1 {
		limit = 32
	}
	f, err := Choice(raw, home, base)
	if err != nil {
		return nil, err
	}
	m := &Manager{Root: f.Path, JobID: jobID, ServerLimit: limit, PollMin: 50 * time.Millisecond, PollMax: 250 * time.Millisecond, Warn: warn}
	if f.Path != "" {
		err := m.ensureRoot()
		if err == nil {
			return m, nil
		}
		if f.Fallback == "" {
			return nil, errorcodes.Ensure(err, "capacity_root_unusable")
		}
		if warn != nil {
			warn(fmt.Sprintf("shared capacity root unavailable (%v); using private fallback %s", err, f.Fallback))
		}
	}
	m.Root = f.Fallback
	if err := m.ensureRoot(); err != nil {
		return nil, err
	}
	return m, nil
}

// Choice is the ledger's chain (osutil.ScratchFolderChoice): the scratch
// root's capacity folder, then the private one, or an explicit path.
func Choice(raw, home, base string) (osutil.ScratchFolder, error) {
	return osutil.ScratchFolderChoice(raw, home, "sessions.shared-capacity-root", "capacity", base)
}

// Place is New's twin, creating nothing: a present root is judged by the
// file system's rule and by the ledger's own, the sticky bit on another
// operator's root and a devices folder closed to the operator.
func Place(raw, home, base string) (osutil.Place, error) {
	f, err := Choice(raw, home, base)
	if err != nil {
		return osutil.Place{}, err
	}
	return f.Place(rootReason, "capacity_root_unusable")
}

// rootReason is the ledger's own judgement of a present root, the reason
// it is passed by in ensureRoot's terms, "" when it passes.
func rootReason(root string) string {
	fi, err := os.Stat(root)
	if err != nil {
		return err.Error()
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && fi.Mode()&os.ModeSticky != 0 && int(st.Uid) != os.Geteuid() {
		return fmt.Sprintf("the sticky bit, owned by uid %d: a ledger another operator wrote could not be replaced", st.Uid)
	}
	devices := filepath.Join(root, "devices")
	if fi, err := os.Stat(devices); err == nil {
		// ensureDir sets the operator's own devices folder right.
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() && syscall.Access(devices, 0o3) != nil { // W_OK|X_OK
			return devices + " not writable by the operator"
		}
	}
	return ""
}

// ensureRoot prepares m.Root and takes its modes: the root is made only in
// a parent that exists (osutil.MakeSharedDirectory), its devices
// directory is made, and the root must be one this operator can lease in,
// so a shared root that is present but closed to the operator is said once
// here and the fallback taken, not met by every device's admission. Under
// the sticky bit a ledger another operator wrote could not be replaced.
func (m *Manager) ensureRoot() error {
	if m.Root == "" {
		return errorcodes.Errorf("capacity_root_blank", "capacity root is blank")
	}
	if err := osutil.MakeSharedDirectory(m.Root, 0o700); err != nil {
		return err
	}
	m.dirMode, m.fileMode = ledgerModes(m.Root)
	fi, err := os.Stat(m.Root)
	if err != nil {
		return err
	}
	if err := syscall.Access(m.Root, 0o3); err != nil { // W_OK|X_OK
		return errorcodes.Errorf("capacity_root_unusable", "%s: %v; %s", m.Root, err, sharedRootShape)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && fi.Mode()&os.ModeSticky != 0 && int(st.Uid) != os.Geteuid() {
		return errorcodes.Errorf("capacity_root_unusable", "%s has the sticky bit and is owned by uid %d, not %d: a ledger another operator wrote could not be replaced; %s", m.Root, st.Uid, os.Geteuid(), sharedRootShape)
	}
	devices := filepath.Join(m.Root, "devices")
	if err := m.ensureDir(devices); err != nil {
		return errorcodes.Errorf("capacity_root_unusable", "create %s: %v; %s", devices, err, sharedRootShape)
	}
	if err := syscall.Access(devices, 0o3); err != nil { // W_OK|X_OK
		return errorcodes.Errorf("capacity_root_unusable", "%s: %v; %s", devices, err, sharedRootShape)
	}
	if _, err := m.readLedger(filepath.Join(m.Root, "server")); err != nil {
		return errorcodes.Errorf("capacity_root_unusable", "%v; %s", err, sharedRootShape)
	}
	return nil
}

const sharedRootShape = "a shared capacity root needs mode 2770 in the operators' group (group write and search, setgid, no sticky bit), as sudo karvi setup shared makes it"

// ledgerModes are the modes of what the ledger creates under root: under a
// root carrying the setgid bit, the group-shared root, the root's own
// permission bits for a directory and the same without search for a file,
// so that every member of the group takes and releases leases in a ledger
// another member made (2770 gives 0660); elsewhere 0700 and 0600, one
// operator's.
func ledgerModes(root string) (dir, file os.FileMode) {
	if fi, err := os.Stat(root); err == nil && fi.IsDir() && fi.Mode()&os.ModeSetgid != 0 {
		perm := fi.Mode().Perm()
		return os.ModeSetgid | perm, perm &^ 0o111
	}
	return 0o700, 0o600
}

// ensureDir makes a directory of the ledger at dirMode, explicitly so the
// umask cannot narrow it. One that exists is set to dirMode when this
// operator owns it at other permission bits, as openLock sets a lock file;
// another member's is left as it is.
func (m *Manager) ensureDir(dir string) error {
	err := os.Mkdir(dir, m.dirMode.Perm())
	if err == nil {
		return os.Chmod(dir, m.dirMode)
	}
	if !os.IsExist(err) {
		return err
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) == os.Geteuid() && fi.Mode().Perm() != m.dirMode.Perm() {
		return os.Chmod(dir, m.dirMode)
	}
	return nil
}

// openLock opens a ledger's lock file, created at fileMode when missing. A
// file this operator owns at another mode (made under the umask, or by an
// earlier release at 0600) is set to fileMode; another member's is
// left as it is.
func (m *Manager) openLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, m.fileMode)
	if err != nil {
		return nil, err
	}
	if err := ownMode(f, m.fileMode); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// ownMode sets mode on f when this process owns it and its permission bits
// differ.
func ownMode(f *os.File, mode os.FileMode) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) == os.Geteuid() && fi.Mode().Perm() != mode.Perm() {
		return f.Chmod(mode)
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
	err := m.withLedger(path, func(entries []entry) ([]entry, error) {
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
	return m.withLedger(path, func(entries []entry) ([]entry, error) {
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
func (m *Manager) withLedger(path string, fn func([]entry) ([]entry, error)) error {
	if err := m.ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	lock, err := m.openLock(path)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	entries, err := loadEntries(path)
	if err != nil {
		return err
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
	if err := tmp.Chmod(m.fileMode); err != nil {
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
	n, err := m.readLedger(filepath.Join(m.Root, "server"))
	if err != nil {
		return a, err
	}
	a.ServerInUse = n
	for device, cap := range caps {
		if cap < 1 {
			cap = 1
		}
		deviceSum := sha256.Sum256([]byte(device))
		n, err := m.readLedger(filepath.Join(m.Root, "devices", hex.EncodeToString(deviceSum[:])))
		if err != nil {
			return a, err
		}
		a.Devices[device] = DeviceAssessment{Cap: cap, InUse: n}
	}
	return a, nil
}

// readLedger counts a ledger's live entries without writing it.
func (m *Manager) readLedger(path string) (int, error) {
	lock, err := m.openLock(path)
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
	entries, err := loadEntries(path)
	if err != nil {
		return 0, err
	}
	return len(reap(entries)), nil
}

// loadEntries reads a ledger's entries; a missing or empty file has none.
// A file that cannot be read is an error, never an empty ledger: the
// write that follows would replace another operator's leases.
func loadEntries(path string) ([]entry, error) {
	entries := []entry{}
	b, err := os.ReadFile(path + ".json")
	if os.IsNotExist(err) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &entries); err != nil {
			return nil, errorcodes.Errorf("capacity_ledger_malformed", "malformed capacity ledger %s: %w", path, err)
		}
	}
	return entries, nil
}
