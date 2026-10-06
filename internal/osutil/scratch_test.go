package osutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// withScratchRoot points ScratchRoot at root for one test.
func withScratchRoot(t *testing.T, root string) {
	t.Helper()
	saved := ScratchRoot
	ScratchRoot = root
	t.Cleanup(func() { ScratchRoot = saved })
}

// TestScratchRootNeverCreated: on a host without the scratch root the
// chains move on to their private candidates and the root is not made, so
// no operator creates it closed to the others; where it exists each
// operator's folder is made inside it, private.
func TestScratchRootNeverCreated(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	withScratchRoot(t, filepath.Join(dir, "shm"))
	got, err := ResolveScratch("auto", base, dir, "u", os.Geteuid())
	if err != nil || got != filepath.Join(base, "tmp") {
		t.Fatalf("scratch %q %v, want %s", got, err, filepath.Join(base, "tmp"))
	}
	got, err = ControlPathRoot("auto", base, dir, "u", os.Geteuid())
	if err != nil || got != filepath.Join(base, "socket", "ssh") {
		t.Fatalf("control path %q %v", got, err)
	}
	if _, err := os.Stat(ScratchRoot); !os.IsNotExist(err) {
		t.Fatalf("the scratch root was made: %v", err)
	}

	if err := os.Mkdir(ScratchRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ScratchRoot, os.ModeSetgid|os.ModeSticky|0o770); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveScratch("auto", base, dir, "u", os.Geteuid())
	if err != nil || got != filepath.Join(ScratchRoot, "u") {
		t.Fatalf("scratch %q %v, want the folder in the scratch root", got, err)
	}
	got, err = ControlPathRoot("auto", base, dir, "u", os.Geteuid())
	if err != nil || got != filepath.Join(ScratchRoot, "u", "sockets") {
		t.Fatalf("control path %q %v", got, err)
	}
	if fi, err := os.Stat(filepath.Join(ScratchRoot, "u")); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("the operator's folder: %v %v", fi.Mode(), err)
	}
}

// TestMakeSharedDirectory: a missing parent is never made; under a setgid parent the directory takes the
// parent's bits and the setgid bit whatever the umask; under a plain parent
// the mode under the umask; one that exists is left as it is.
func TestMakeSharedDirectory(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	dir := t.TempDir()

	absent := filepath.Join(dir, "shm", "scoreboards")
	if err := MakeSharedDirectory(absent, 0o770); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent parent: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(absent)); !os.IsNotExist(err) {
		t.Fatalf("the parent was made: %v", err)
	}

	shared := filepath.Join(dir, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, os.ModeSetgid|os.ModeSticky|0o770); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(shared, "capacity")
	if err := MakeSharedDirectory(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o770 || fi.Mode()&os.ModeSetgid == 0 || fi.Mode()&os.ModeSticky != 0 {
		t.Fatalf("under a setgid parent: %v, want 2770", fi.Mode())
	}

	plain := filepath.Join(dir, "cap")
	if err := MakeSharedDirectory(plain, 0o770); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(plain); fi.Mode().Perm() != 0o700 {
		t.Fatalf("under a plain parent with umask 077: %v, want 0700", fi.Mode())
	}
	if err := os.Chmod(plain, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := MakeSharedDirectory(plain, 0o770); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(plain); fi.Mode().Perm() != 0o750 {
		t.Fatalf("an existing directory was changed: %v", fi.Mode())
	}
}
