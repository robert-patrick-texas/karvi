package capacity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

func TestAcquireRelease(t *testing.T) {
	m, err := New(t.TempDir(), "", "", "job", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := m.Acquire(context.Background(), "device", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

// TestLedgerModes: under a setgid root (the shared root setup shared makes)
// every directory and file the ledger creates is the group's, so another
// member takes and releases leases in it; under a private root they are
// the operator's alone. The umask cannot narrow either.
func TestLedgerModes(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	for _, c := range []struct {
		root      os.FileMode
		dir, file string
	}{
		{os.ModeSetgid | 0o770, "drwxrwx---", "-rw-rw----"},
		{os.ModeSetgid | 0o775, "drwxrwxr-x", "-rw-rw-r--"},
		{0o700, "drwx------", "-rw-------"},
	} {
		root := filepath.Join(t.TempDir(), "cap")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(root, c.root); err != nil {
			t.Fatal(err)
		}
		m, err := New(root, "", "", "job", 4, nil)
		if err != nil {
			t.Fatal(err)
		}
		l, err := m.Acquire(context.Background(), "device", 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.Release(); err != nil {
			t.Fatal(err)
		}
		err = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			if err != nil || p == root {
				return err
			}
			want := c.file
			if fi.IsDir() {
				want = c.dir
			}
			if got := fi.Mode().Perm().String(); got[1:] != want[1:] {
				t.Errorf("root %v: %s is %s, want %s", c.root, strings.TrimPrefix(p, root), got, want)
			}
			if fi.IsDir() && (fi.Mode()&os.ModeSetgid != 0) != (c.root&os.ModeSetgid != 0) {
				t.Errorf("root %v: %s setgid %v", c.root, strings.TrimPrefix(p, root), fi.Mode()&os.ModeSetgid != 0)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"server.lock", "server.json", "devices"} {
			if _, err := os.Stat(filepath.Join(root, name)); err != nil {
				t.Errorf("root %v: %v", c.root, err)
			}
		}
	}
}

// TestOwnPrivateFilesWidened: a lock file this operator made at 0600 under
// the shared root (an earlier release, or the umask) is set to the group's
// mode at its next use.
func TestOwnPrivateFilesWidened(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cap")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "server.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "", "", "job", 4, nil); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(root, "server.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o660 {
		t.Fatalf("server.lock is %v, want 0660", fi.Mode().Perm())
	}
}

// TestUnusableRootFallsBack: a shared root whose server lock this operator
// cannot open is said once, as capacity_root_unusable in the warning, and
// the private fallback is taken; no device meets the refusal.
func TestUnusableRootFallsBack(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens every file")
	}
	shared := filepath.Join(t.TempDir(), "capacity")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	// A lock open for reading alone, as another member's 0600 file is to
	// this operator: the open for writing fails before any mode is set.
	if err := os.WriteFile(filepath.Join(shared, "server.lock"), nil, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	base := withScratchRoot(t, filepath.Dir(shared))
	fallback := filepath.Join(base, "state", "capacity")
	var warned []string
	m, err := New("auto", "", base, "job", 4, func(s string) { warned = append(warned, s) })
	if err != nil {
		t.Fatal(err)
	}
	if m.Root != fallback {
		t.Fatalf("root %s, want the fallback %s", m.Root, fallback)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "capacity_root_unusable") || !strings.Contains(warned[0], "2770") {
		t.Fatalf("warnings %q", warned)
	}
}

// TestUnreadableLedgerIsAnError: a device ledger this operator cannot read
// (another member's file at 0600) fails the lease and is left as it was;
// read as empty, the write after it would replace that member's leases.
func TestUnreadableLedgerIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	root := filepath.Join(t.TempDir(), "cap")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	m, err := New(root, "", "", "job", 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := m.Acquire(context.Background(), "device", 2)
	if err != nil {
		t.Fatal(err)
	}
	ledger := l.devicePath + ".json"
	before, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ledger, 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire(context.Background(), "device", 2); err == nil {
		t.Fatal("a lease was taken over an unreadable ledger")
	}
	if err := os.Chmod(ledger, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the unreadable ledger was rewritten:\n%s\nwas\n%s", after, before)
	}
}

// TestOwnPrivateDirectoryWidened: a devices directory this operator made at
// 0700 under the shared root is set to the group's mode at its next use.
func TestOwnPrivateDirectoryWidened(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cap")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "devices"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "devices"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "", "", "job", 4, nil); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(root, "devices"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o770 || fi.Mode()&os.ModeSetgid == 0 {
		t.Fatalf("devices is %v, want 2770", fi.Mode())
	}
}

// TestAbsentSharedRootFallsBackQuietly: a shared root whose parent is absent
// (a host without the scratch root) takes the private fallback without a
// warning, and the scratch root is not made.
func TestAbsentSharedRootFallsBackQuietly(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "shm", "capacity")
	base := withScratchRoot(t, filepath.Dir(shared))
	fallback := filepath.Join(base, "state", "capacity")
	var warned []string
	m, err := New("auto", "", base, "job", 4, func(s string) { warned = append(warned, s) })
	if err != nil {
		t.Fatal(err)
	}
	if m.Root != fallback || len(warned) != 0 {
		t.Fatalf("root %s, warnings %q", m.Root, warned)
	}
	if _, err := os.Stat(filepath.Dir(shared)); !os.IsNotExist(err) {
		t.Fatalf("the scratch root was made: %v", err)
	}
}

// withScratchRoot points osutil.ScratchRoot at root for one test and
// returns a private root whose state folder exists, as an activity's has.
func withScratchRoot(t *testing.T, root string) string {
	t.Helper()
	saved := osutil.ScratchRoot
	osutil.ScratchRoot = root
	t.Cleanup(func() { osutil.ScratchRoot = saved })
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	return base
}

// TestPlaceNamesWhatNewTakes: the twin names, creating nothing, the root
// New then takes: the scratch root's folder, made in it when missing; the
// private folder past a shared one closed by another's sticky bit, listed;
// an explicit path refused when it cannot be used.
func TestPlaceNamesWhatNewTakes(t *testing.T) {
	dir := t.TempDir()
	scratch := filepath.Join(dir, "shm")
	if err := os.Mkdir(scratch, 0o770); err != nil {
		t.Fatal(err)
	}
	base := withScratchRoot(t, scratch)
	agree := func(name string, passed int) {
		t.Helper()
		pl, err := Place("auto", "", base)
		if err != nil || len(pl.Passed) != passed {
			t.Fatalf("%s: place %+v %v", name, pl, err)
		}
		m, err := New("auto", "", base, "job", 4, nil)
		if err != nil || m.Root != pl.Path {
			t.Fatalf("%s: New took %v %v, the place %s", name, m, err, pl.Path)
		}
	}
	if pl, _ := Place("auto", "", base); pl.Path != filepath.Join(scratch, "capacity") {
		t.Fatalf("the scratch root's folder: %+v", pl)
	}
	if _, err := os.Stat(filepath.Join(scratch, "capacity")); !os.IsNotExist(err) {
		t.Fatalf("the place was made: %v", err)
	}
	agree("absent, made in the root", 0)
	if os.Geteuid() != 0 {
		// The operator's own devices folder closed is set right by New.
		devices := filepath.Join(scratch, "capacity", "devices")
		if err := os.Chmod(devices, 0o500); err != nil {
			t.Fatal(err)
		}
		agree("own devices closed", 0)
		root := filepath.Join(scratch, "capacity")
		if err := os.Chmod(root, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(root, 0o700) })
		agree("root closed", 1)
		if _, err := New(filepath.Join(scratch, "capacity"), "", base, "job", 4, nil); errorcodes.Of(err) != "capacity_root_unusable" {
			t.Fatalf("explicit, closed: %v", err)
		}
		if _, err := Place(filepath.Join(scratch, "capacity"), "", base); errorcodes.Of(err) != "capacity_root_unusable" {
			t.Fatalf("explicit, closed, place: %v", err)
		}
	}
}
