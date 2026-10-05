package osutil

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

// ~ in ssh.control-path-root is the operator's home, not a place derived
// from basedir (it was basedir's grandparent).
func TestControlPathRootHome(t *testing.T) {
	dir := t.TempDir()
	home, base := filepath.Join(dir, "home"), filepath.Join(dir, "x", "y", "base")
	got, err := ControlPathRoot("~/sockets", base, home, "u", os.Geteuid())
	if err != nil || got != filepath.Join(home, "sockets") {
		t.Fatalf("%q %v, want %s", got, err, filepath.Join(home, "sockets"))
	}
	place, err := ControlPathRootPlace("~/other", base, home, "u", os.Geteuid())
	if err != nil || place != filepath.Join(home, "other") {
		t.Fatalf("place %q %v", place, err)
	}
	if _, err := os.Stat(place); !os.IsNotExist(err) {
		t.Fatalf("the place was made: %v", err)
	}
}

// The place is where ControlPathRoot resolves, without making it: the
// scratch root's folder when the root exists, else the basedir's.
func TestControlPathRootPlace(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	withScratchRoot(t, filepath.Join(dir, "shm"))
	if got, err := ControlPathRootPlace("auto", base, dir, "u", os.Geteuid()); err != nil || got != filepath.Join(base, "socket", "ssh") {
		t.Fatalf("without the scratch root: %q %v", got, err)
	}
	if err := os.Mkdir(ScratchRoot, 0o770); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(ScratchRoot, "u", "sockets")
	if got, err := ControlPathRootPlace("auto", base, dir, "u", os.Geteuid()); err != nil || got != want {
		t.Fatalf("with the scratch root: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(ScratchRoot, "u")); !os.IsNotExist(err) {
		t.Fatalf("the folder was made: %v", err)
	}
	if got, err := ControlPathRoot("auto", base, dir, "u", os.Geteuid()); err != nil || got != want {
		t.Fatalf("resolved: %q %v", got, err)
	}
	// An exposed folder is passed over, by the place as by the resolution.
	if err := os.Chmod(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := ControlPathRootPlace("auto", base, dir, "u", os.Geteuid()); err != nil || got != filepath.Join(base, "socket", "ssh") {
		t.Fatalf("an exposed folder: %q %v", got, err)
	}
}

// A root of 73 bytes fits a socket's name and OpenSSH's binding suffix in
// 107 bytes; one more is control_path_root_too_long, naming the root and
// its length.
func TestCheckControlPathRoot(t *testing.T) {
	root := "/" + strings.Repeat("r", 72)
	if err := CheckControlPathRoot(root); err != nil {
		t.Fatalf("73 bytes: %v", err)
	}
	if n := len(root) + 1 + ControlSocketNameLength + 17; n != 107 {
		t.Fatalf("the longest path is %d bytes", n)
	}
	err := CheckControlPathRoot(root + "r")
	if errorcodes.Of(err) != "control_path_root_too_long" || !strings.Contains(err.Error(), root+"r is 74 bytes") {
		t.Fatalf("74 bytes: %v", err)
	}
	name, err := NewControlSocketName()
	if err != nil || !controlSocketName(name) {
		t.Fatalf("a fresh name %q %v", name, err)
	}
}

// The sweep removes a socket of karvi's name that refuses a connection, and
// leaves one that answers, one of another name, and a file of karvi's name.
func TestSweepControlSockets(t *testing.T) {
	root := testsocket.Dir(t)
	listen := func(name string) *net.UnixListener {
		l, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, name), Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	dead := listen("0123456789abcdef")
	dead.SetUnlinkOnClose(false)
	dead.Close()
	live := listen("fedcba9876543210")
	defer live.Close()
	other := listen("not-karvis")
	other.SetUnlinkOnClose(false)
	other.Close()
	if err := os.WriteFile(filepath.Join(root, "00112233445566aa"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var logged []string
	removed := SweepControlSockets(root, func(name string) { logged = append(logged, name) })
	if strings.Join(removed, ",") != "0123456789abcdef" || strings.Join(logged, ",") != "0123456789abcdef" {
		t.Fatalf("removed %q logged %q", removed, logged)
	}
	for _, name := range []string{"fedcba9876543210", "not-karvis", "00112233445566aa"} {
		if _, err := os.Lstat(filepath.Join(root, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if SweepControlSockets(filepath.Join(root, "absent"), nil) != nil {
		t.Fatal("an absent root")
	}
}
