package capacity

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/testdir"
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

// TestOwnPrivateFilesWidened: a lock file this operator owns at 0600 under
// the shared root (made by hand, say) is set to the group's mode at its next
// use.
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

// TestPlaceFollowsALinkAtTheRootAsNewDoes: the root is the site's, and
// Place follows a link at it, or above it, as New does, the root's checks
// made on what the link points to. Under auto and for an explicit root
// alike Place takes what New takes: a link to a shared root, or a scratch
// root that is a link, is the root; a link to a folder closed to the
// operator, to a root whose server lock is a link, or to nothing is passed
// by with the reason, or refused, where New falls back with one warning or
// refuses. Place makes nothing.
func TestPlaceFollowsALinkAtTheRootAsNewDoes(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes every folder")
	}
	link := func(t *testing.T, target, at string) {
		t.Helper()
		if err := os.Symlink(target, at); err != nil {
			t.Fatal(err)
		}
	}
	scratchAt := func(t *testing.T, scratch string) {
		t.Helper()
		if err := os.Mkdir(scratch, 0o770); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		// put makes the scratch root and the capacity root in it, and
		// returns the reason Place passes the root by, "" when it is taken.
		put func(t *testing.T, scratch, root string) string
	}{
		{"a link to a shared root", func(t *testing.T, scratch, root string) string {
			scratchAt(t, scratch)
			link(t, sharedRoot(t), root)
			return ""
		}},
		{"the scratch root a link", func(t *testing.T, scratch, root string) string {
			shm := filepath.Join(t.TempDir(), "shm")
			scratchAt(t, shm)
			link(t, shm, scratch)
			if err := os.Mkdir(filepath.Join(shm, "capacity"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(shm, "capacity"), os.ModeSetgid|0o770); err != nil {
				t.Fatal(err)
			}
			return ""
		}},
		{"a link to a closed folder", func(t *testing.T, scratch, root string) string {
			scratchAt(t, scratch)
			closed := filepath.Join(t.TempDir(), "closed")
			if err := os.Mkdir(closed, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(closed, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(closed, 0o700) })
			link(t, closed, root)
			return "not writable by the operator"
		}},
		{"a link to a root whose server lock is a link", func(t *testing.T, scratch, root string) string {
			scratchAt(t, scratch)
			target := sharedRoot(t)
			plantLink(t, filepath.Join(target, "server.lock"), "file")
			link(t, target, root)
			return filepath.Join(root, "server.lock") + " is a link, not a file"
		}},
		{"a link to nothing", func(t *testing.T, scratch, root string) string {
			scratchAt(t, scratch)
			link(t, filepath.Join(t.TempDir(), "absent"), root)
			return "not a real directory"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scratch := filepath.Join(t.TempDir(), "shm")
			base := withScratchRoot(t, scratch)
			fallback := filepath.Join(base, "state", "capacity")
			root := filepath.Join(scratch, "capacity")
			reason := tc.put(t, scratch, root)
			// What stands in the root, through the link.
			names := func() string {
				entries, err := os.ReadDir(root)
				if err != nil {
					return err.Error()
				}
				out := []string{}
				for _, e := range entries {
					out = append(out, e.Name())
				}
				return strings.Join(out, " ")
			}
			before := names()
			pl, perr := Place("auto", "", base)
			explicit, xerr := Place(root, "", base)
			if after := names(); after != before {
				t.Errorf("Place made in the root: %q, was %q", after, before)
			}
			var warned []string
			m, err := New("auto", "", base, "job", 4, func(s string) { warned = append(warned, s) })
			if err != nil {
				t.Fatalf("auto: New %v", err)
			}
			_, nerr := New(root, "", base, "job", 4, nil)
			if reason == "" {
				if perr != nil || pl.Path != root || len(pl.Passed) != 0 || xerr != nil || explicit.Path != root {
					t.Errorf("Place: auto %+v %v; explicit %+v %v; want %s", pl, perr, explicit, xerr, root)
				}
				if m.Root != root || len(warned) != 0 || nerr != nil {
					t.Errorf("New: auto took %s (warnings %q); explicit %v", m.Root, warned, nerr)
				}
				return
			}
			want := []osutil.Passed{{Path: root, Reason: reason}}
			if perr != nil || pl.Path != fallback || !slices.Equal(pl.Passed, want) {
				t.Errorf("auto: Place %+v %v\nwant %s passed %+v", pl, perr, fallback, want)
			}
			if refusal := "capacity_root_unusable: " + root + ": " + reason; errorcodes.Message(xerr) != refusal {
				t.Errorf("explicit: Place %v\nwant %s", xerr, refusal)
			}
			if m.Root != fallback || len(warned) != 1 {
				t.Errorf("auto: New took %s (warnings %q), want %s", m.Root, warned, fallback)
			}
			if errorcodes.Of(nerr) != "capacity_root_unusable" {
				t.Errorf("explicit: New %v", nerr)
			}
		})
	}
}

// within runs f and fails the test when it has not returned in ten seconds.
func within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s still waits after 10s", what)
	}
}

// oddLedger puts at a ledger's path a FIFO ("fifo"), a symbolic link to a
// copy of data elsewhere ("link"), or a sparse regular file one byte past
// maxLedgerBytes ("big"), and returns a check that it stands as it was put
// and the link's target holds data. A FIFO a read still waits on is
// released at the test's end.
func oddLedger(t *testing.T, path, kind string, data []byte) func() {
	t.Helper()
	switch kind {
	case "fifo":
		if err := syscall.Mkfifo(path, 0o660); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
				f.Close()
			}
		})
		return func() {
			if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeNamedPipe == 0 {
				t.Errorf("the FIFO: %v %v", fi, err)
			}
		}
	case "link":
		target := filepath.Join(t.TempDir(), "ledger.json")
		if err := os.WriteFile(target, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		return func() {
			if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Errorf("the link: %v %v", fi, err)
			}
			if b, err := os.ReadFile(target); err != nil || string(b) != string(data) {
				t.Errorf("the link's target:\n%s\nwas\n%s (%v)", b, data, err)
			}
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o660)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxLedgerBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return func() {
		if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() || fi.Size() != maxLedgerBytes+1 {
			t.Errorf("the large file: %v %v", fi, err)
		}
	}
}

// TestLedgerNotARegularFileIsAnError: a FIFO, a link to a ledger
// elsewhere, and a file larger than a ledger may be, at a device ledger's
// name, fail the lease with an error naming the ledger, as a ledger that
// cannot be read does, never waited on or read through, and are left as
// they were; a FIFO at the server ledger's name makes an explicit root
// capacity_root_unusable.
func TestLedgerNotARegularFileIsAnError(t *testing.T) {
	for _, kind := range []string{"fifo", "link", "big"} {
		t.Run(kind, func(t *testing.T) {
			m, err := New(sharedRoot(t), "", "", "job", 4, nil)
			if err != nil {
				t.Fatal(err)
			}
			l, err := m.Acquire(context.Background(), "router1", 2)
			if err != nil {
				t.Fatal(err)
			}
			ledger := l.devicePath + ".json"
			data, err := os.ReadFile(ledger)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(ledger); err != nil {
				t.Fatal(err)
			}
			stands := oddLedger(t, ledger, kind, data)
			var lerr error
			within(t, "Acquire", func() { _, lerr = m.Acquire(context.Background(), "router1", 2) })
			if lerr == nil || !strings.Contains(lerr.Error(), ledger) {
				t.Fatalf("a lease over the %s: %v", kind, lerr)
			}
			if kind == "big" && !strings.Contains(lerr.Error(), "more than the") {
				t.Errorf("the large file's error: %v", lerr)
			}
			stands()
		})
	}
	t.Run("server", func(t *testing.T) {
		root := sharedRoot(t)
		stands := oddLedger(t, filepath.Join(root, "server.json"), "fifo", nil)
		var err error
		within(t, "New", func() { _, err = New(root, "", "", "job", 4, nil) })
		if errorcodes.Of(err) != "capacity_root_unusable" || !strings.Contains(err.Error(), filepath.Join(root, "server.json")) {
			t.Fatalf("the root: %v", err)
		}
		stands()
	})
}

// sharedRoot makes a capacity root as setup shared makes one: setgid,
// group-writable, no sticky bit, so every member writes in its folders.
func sharedRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "cap")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	return root
}

// plantLink puts at a name a link to a file this operator owns at 0600
// ("file") or to nothing ("dangling"), as a member could in a shared root,
// and returns a check that the link stands and its target is as it was:
// the file at 0600, holding what it held, or still absent.
func plantLink(t *testing.T, at, kind string) func() {
	t.Helper()
	target := filepath.Join(t.TempDir(), "id_ed25519")
	if kind == "file" {
		if err := os.WriteFile(target, []byte("secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, at); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if fi, err := os.Lstat(at); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("the link: %v %v", fi, err)
		}
		fi, err := os.Lstat(target)
		if kind == "dangling" {
			if err == nil {
				t.Errorf("the link's target was made, %v", fi.Mode())
			} else if !os.IsNotExist(err) {
				t.Errorf("the link's target: %v", err)
			}
			return
		}
		if err != nil || fi.Mode() != 0o600 {
			t.Errorf("the link's target is %v (%v), want -rw-------", fi.Mode(), err)
		}
		if b, err := os.ReadFile(target); err != nil || string(b) != "secret\n" {
			t.Errorf("the link's target holds %q (%v)", b, err)
		}
	}
}

// TestLedgerLockLinkIsRefused: a link at a lock's name in a shared root,
// to a file this operator owns at 0600 or to nothing, is refused, never
// opened through: at the server lock an explicit root, and at a device lock
// the lease and the assessment, are capacity_root_unusable, the message
// naming the link and its removal; the link's target keeps its mode, or is
// not made.
func TestLedgerLockLinkIsRefused(t *testing.T) {
	refusal := func(lock string) string {
		return "capacity_root_unusable: " + lock + " is a link, not a file; remove it"
	}
	for _, kind := range []string{"file", "dangling"} {
		t.Run(kind+"/server", func(t *testing.T) {
			root := sharedRoot(t)
			lock := filepath.Join(root, "server.lock")
			stands := plantLink(t, lock, kind)
			_, err := New(root, "", "", "job", 4, nil)
			if errorcodes.Of(err) != "capacity_root_unusable" || errorcodes.Message(err) != refusal(lock) {
				t.Errorf("New: %v\nwant %s", err, refusal(lock))
			}
			stands()
		})
		t.Run(kind+"/device", func(t *testing.T) {
			m, err := New(sharedRoot(t), "", "", "job", 4, nil)
			if err != nil {
				t.Fatal(err)
			}
			l, err := m.Acquire(context.Background(), "router1", 2)
			if err != nil {
				t.Fatal(err)
			}
			if err := l.Release(); err != nil {
				t.Fatal(err)
			}
			lock := l.devicePath + ".lock"
			if err := os.Remove(lock); err != nil {
				t.Fatal(err)
			}
			stands := plantLink(t, lock, kind)
			if _, err := m.Acquire(context.Background(), "router1", 2); errorcodes.Of(err) != "capacity_root_unusable" || errorcodes.Message(err) != refusal(lock) {
				t.Errorf("Acquire: %v\nwant %s", err, refusal(lock))
			}
			if _, err := m.Assess(map[string]int{"router1": 2}); errorcodes.Of(err) != "capacity_root_unusable" || errorcodes.Message(err) != refusal(lock) {
				t.Errorf("Assess: %v\nwant %s", err, refusal(lock))
			}
			stands()
		})
	}
}

// TestLedgerDevicesLinkIsRefused: below a shared root, devices must be a
// real folder. A link there, to a folder this operator owns or to nothing,
// and a file there make an explicit root capacity_root_unusable, in New
// and in Place alike, naming devices; New's refusal of a link, and the
// lease's of a link put there after New, name the link and its removal.
// Nothing is made, or set to another mode, where a link points.
func TestLedgerDevicesLinkIsRefused(t *testing.T) {
	refusal := func(devices string) string {
		return "capacity_root_unusable: " + devices + " is a link, not a folder; remove it"
	}
	// elsewhere is a folder of this operator's at 0700, holding nothing,
	// and a check that it stays so.
	elsewhere := func(t *testing.T) (string, func()) {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		return dir, func() {
			t.Helper()
			fi, err := os.Stat(dir)
			if err != nil || fi.Mode() != os.ModeDir|0o700 {
				t.Errorf("the folder the link points to is %v (%v), want drwx------", fi.Mode(), err)
			}
			if names, err := os.ReadDir(dir); err != nil || len(names) != 0 {
				t.Errorf("made where the link points: %v %v", names, err)
			}
		}
	}
	for _, kind := range []string{"folder", "dangling", "file"} {
		t.Run(kind, func(t *testing.T) {
			root := sharedRoot(t)
			devices := filepath.Join(root, "devices")
			var stands func()
			switch kind {
			case "folder":
				var dir string
				dir, stands = elsewhere(t)
				if err := os.Symlink(dir, devices); err != nil {
					t.Fatal(err)
				}
			case "dangling":
				absent := filepath.Join(t.TempDir(), "absent")
				if err := os.Symlink(absent, devices); err != nil {
					t.Fatal(err)
				}
				stands = func() {
					if _, err := os.Lstat(absent); !os.IsNotExist(err) {
						t.Errorf("made where the link points: %v", err)
					}
				}
			case "file":
				if err := os.WriteFile(devices, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(devices, 0o600); err != nil {
					t.Fatal(err)
				}
				stands = func() {
					if fi, err := os.Lstat(devices); err != nil || fi.Mode() != 0o600 {
						t.Errorf("the file at devices is %v (%v), want -rw-------", fi.Mode(), err)
					}
				}
			}
			_, err := New(root, "", "", "job", 4, nil)
			if errorcodes.Of(err) != "capacity_root_unusable" || !strings.Contains(err.Error(), devices) {
				t.Errorf("New: %v", err)
			}
			if kind != "file" && errorcodes.Message(err) != refusal(devices) {
				t.Errorf("New: %v\nwant %s", err, refusal(devices))
			}
			if _, err := Place(root, "", ""); errorcodes.Of(err) != "capacity_root_unusable" || !strings.Contains(err.Error(), devices) {
				t.Errorf("Place: %v", err)
			}
			stands()
		})
	}
	t.Run("after New", func(t *testing.T) {
		root := sharedRoot(t)
		m, err := New(root, "", "", "job", 4, nil)
		if err != nil {
			t.Fatal(err)
		}
		devices := filepath.Join(root, "devices")
		if err := os.Remove(devices); err != nil {
			t.Fatal(err)
		}
		dir, stands := elsewhere(t)
		if err := os.Symlink(dir, devices); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Acquire(context.Background(), "router1", 2); errorcodes.Of(err) != "capacity_root_unusable" || errorcodes.Message(err) != refusal(devices) {
			t.Errorf("Acquire: %v\nwant %s", err, refusal(devices))
		}
		stands()
	})
}

// TestMalformedLedgerNamesItsFile: a ledger that is not JSON fails the
// lease capacity_ledger_malformed, the message naming the file as it is
// and leaving the reason to the code's description.
func TestMalformedLedgerNamesItsFile(t *testing.T) {
	m, err := New(sharedRoot(t), "", "", "job", 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := m.Acquire(context.Background(), "router1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	ledger := l.devicePath + ".json"
	if err := os.WriteFile(ledger, []byte("{router1\n"), 0o660); err != nil {
		t.Fatal(err)
	}
	_, err = m.Acquire(context.Background(), "router1", 2)
	if errorcodes.Of(err) != "capacity_ledger_malformed" || !strings.HasPrefix(err.Error(), "capacity_ledger_malformed: "+ledger+": ") {
		t.Fatalf("Acquire: %v", err)
	}
}

// TestPlaceJudgesTheServerLedgerAsNewDoes: Place and New agree on a root
// whose server lock or server ledger New cannot open, read, or decode: a
// link at the lock, to a file or to nothing; a lock that is a folder, a
// socket, or closed to the operator; a ledger that is a link, a FIFO,
// larger than a ledger may be, closed to the operator, or not JSON. Under
// auto Place passes the shared root by, its reason naming what stands
// there, and takes the private folder New takes; an explicit root is
// refused capacity_root_unusable, naming it. A FIFO at the lock opens and
// locks, and a ledger holding a lease decodes, so both take that root.
// Place makes nothing in the root.
func TestPlaceJudgesTheServerLedgerAsNewDoes(t *testing.T) {
	for _, tc := range []struct {
		name, file, reason string
		put                func(t *testing.T, path string) func()
		closed             bool
	}{
		{"lock link to a file", "server.lock", " is a link, not a file", func(t *testing.T, p string) func() { return plantLink(t, p, "file") }, false},
		{"lock link to nothing", "server.lock", " is a link, not a file", func(t *testing.T, p string) func() { return plantLink(t, p, "dangling") }, false},
		{"lock a folder", "server.lock", " exists and is not a file", func(t *testing.T, p string) func() {
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
			return func() {}
		}, false},
		{"lock a socket", "server.lock", " exists and is not a file", func(t *testing.T, p string) func() {
			l, err := net.Listen("unix", p)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { l.Close() })
			return func() {}
		}, false},
		{"lock closed", "server.lock", " closed to the operator", func(t *testing.T, p string) func() {
			if err := os.WriteFile(p, nil, 0o400); err != nil {
				t.Fatal(err)
			}
			return func() {}
		}, true},
		{"lock a FIFO", "server.lock", "", func(t *testing.T, p string) func() {
			if err := syscall.Mkfifo(p, 0o660); err != nil {
				t.Fatal(err)
			}
			return func() {}
		}, false},
		{"ledger link", "server.json", " is a link, not a file", func(t *testing.T, p string) func() { return oddLedger(t, p, "link", []byte("[]\n")) }, false},
		{"ledger a FIFO", "server.json", " exists and is not a file", func(t *testing.T, p string) func() { return oddLedger(t, p, "fifo", nil) }, false},
		{"ledger too large", "server.json", fmt.Sprintf(" is %d bytes, more than the %d allowed", maxLedgerBytes+1, maxLedgerBytes), func(t *testing.T, p string) func() { return oddLedger(t, p, "big", nil) }, false},
		{"ledger closed", "server.json", " not readable by the operator", func(t *testing.T, p string) func() {
			if err := os.WriteFile(p, []byte("[]\n"), 0o000); err != nil {
				t.Fatal(err)
			}
			return func() {}
		}, true},
		{"ledger not JSON", "server.json", ": invalid character 'r' looking for beginning of object key string", func(t *testing.T, p string) func() {
			if err := os.WriteFile(p, []byte("{router1\n"), 0o660); err != nil {
				t.Fatal(err)
			}
			return func() {}
		}, false},
		{"ledger holding a lease", "server.json", "", func(t *testing.T, p string) func() {
			lease := `[{"ID":"a1","JobID":"job","BootID":"b","Start":"s","PID":1,"Created":"2026-11-01T09:00:00Z"}]` + "\n"
			if err := os.WriteFile(p, []byte(lease), 0o660); err != nil {
				t.Fatal(err)
			}
			return func() {}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.closed && os.Geteuid() == 0 {
				t.Skip("root opens every file")
			}
			// A short folder: the socket's path is bounded at 107 bytes.
			scratch := filepath.Join(testdir.Short(t), "shm")
			if err := os.Mkdir(scratch, 0o770); err != nil {
				t.Fatal(err)
			}
			base := withScratchRoot(t, scratch)
			fallback := filepath.Join(base, "state", "capacity")
			root := filepath.Join(scratch, "capacity")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, os.ModeSetgid|0o770); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, tc.file)
			stands := tc.put(t, path)
			names := func() []string {
				t.Helper()
				entries, err := os.ReadDir(root)
				if err != nil {
					t.Fatal(err)
				}
				out := []string{}
				for _, e := range entries {
					out = append(out, e.Name())
				}
				return out
			}
			before := names()
			pl, err := Place("auto", "", base)
			if err != nil {
				t.Fatalf("Place: %v", err)
			}
			if after := names(); !slices.Equal(after, before) {
				t.Errorf("Place made in the root: %v, was %v", after, before)
			}
			var warned []string
			m, err := New("auto", "", base, "job", 4, func(s string) { warned = append(warned, s) })
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			explicit, xerr := Place(root, "", base)
			if tc.reason == "" {
				if pl.Path != root || len(pl.Passed) != 0 || m.Root != root || len(warned) != 0 || xerr != nil || explicit.Path != root {
					t.Fatalf("auto: place %+v, New took %s (warnings %q); explicit: place %+v %v", pl, m.Root, warned, explicit, xerr)
				}
				return
			}
			want := []osutil.Passed{{Path: root, Reason: path + tc.reason}}
			if pl.Path != fallback || !slices.Equal(pl.Passed, want) {
				t.Errorf("auto: place %+v\nwant %s passed %+v", pl, fallback, want)
			}
			if m.Root != fallback || len(warned) != 1 {
				t.Errorf("auto: New took %s (warnings %q), want %s", m.Root, warned, fallback)
			}
			if refusal := "capacity_root_unusable: " + root + ": " + path + tc.reason; errorcodes.Of(xerr) != "capacity_root_unusable" || errorcodes.Message(xerr) != refusal {
				t.Errorf("explicit: Place %v\nwant %s", xerr, refusal)
			}
			if _, err := New(root, "", base, "job", 4, nil); errorcodes.Of(err) != "capacity_root_unusable" {
				t.Errorf("explicit: New %v", err)
			}
			stands()
		})
	}
}
