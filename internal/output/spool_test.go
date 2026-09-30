package output

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSpoolNameAndOwner: the name is the metadata
// and is parsed from the right, so a device name with dots keeps its pid.
func TestSpoolNameAndOwner(t *testing.T) {
	name := SpoolName("260927-101500-00", "rtr1.example.net", 3, 4242)
	if name != "260927-101500-00.rtr1.example.net.3.4242.spool" {
		t.Fatalf("name %q", name)
	}
	if pid, ok := spoolOwner(name); !ok || pid != 4242 {
		t.Fatalf("owner of %q: %d %v", name, pid, ok)
	}
	for _, other := range []string{"notes.txt", "a.b.spool", "260927-101500-00.dev.x.12.spool", "260927-101500-00.dev.1.0.spool", "260927-101500-00.dev.1.12.spool.bak"} {
		if _, ok := spoolOwner(other); ok {
			t.Errorf("%q taken for a spool", other)
		}
	}
}

// TestSweepSpools: a spool whose pid is dead is removed and logged, one
// whose pid is a live karvi process is kept, another uid's or any other
// file is not touched.
func TestSweepSpools(t *testing.T) {
	dir := t.TempDir()
	// A pid that is certainly not alive: a child that has exited and been
	// waited for.
	child := exec.Command("true")
	if err := child.Run(); err != nil {
		t.Skip("no `true` on this host:", err)
	}
	dead := child.Process.Pid
	live := os.Getpid()
	prev := spoolInUse
	spoolInUse = func(pid int) bool { return pid == live }
	defer func() { spoolInUse = prev }()
	plant := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	gone := plant(SpoolName("260927-101500-00", "dev-a", 1, dead))
	kept := plant(SpoolName("260927-101500-00", "dev-b", 1, live))
	other := plant("README")
	sub := filepath.Join(dir, SpoolName("x", "y", 1, dead))
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	var logged []string
	removed := SweepSpools(dir, func(name string) { logged = append(logged, name) })
	if len(removed) != 1 || removed[0] != filepath.Base(gone) || len(logged) != 1 || logged[0] != removed[0] {
		t.Fatalf("removed %v logged %v", removed, logged)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Errorf("the dead pid's spool is still there")
	}
	for _, p := range []string{kept, other, sub} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was touched: %v", p, err)
		}
	}
	if SweepSpools(filepath.Join(dir, "absent"), nil) != nil {
		t.Errorf("a missing directory sweeps nothing")
	}
}
