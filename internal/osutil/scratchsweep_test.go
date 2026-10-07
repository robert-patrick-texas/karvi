package osutil

import (
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

// TestScratchFileOwner: a name made by ScratchFilePattern carries the
// maker's pid; a name of another shape, an earlier release's without a pid
// among them, is not karvi's to sweep.
func TestScratchFileOwner(t *testing.T) {
	dir := t.TempDir()
	for _, k := range scratchFileKinds {
		f, err := os.CreateTemp(dir, ScratchFilePattern(k.prefix, k.suffix))
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		name := filepath.Base(f.Name())
		if pid, ok := scratchFileOwner(name); !ok || pid != os.Getpid() {
			t.Fatalf("%s: pid %d ok %v", name, pid, ok)
		}
	}
	for _, name := range []string{"karvi-ssh-123.conf", "karvi-ssh-x-1.conf", "karvi-ssh-12-.conf", "karvi-ssh-0-1.conf", "karvi-ssh-12-1a.conf", "karvi-script-12-1.conf", "notes.txt"} {
		if _, ok := scratchFileOwner(name); ok {
			t.Fatalf("%s taken as karvi's", name)
		}
	}
}

// TestAskpassSocketOwner: a name made by AskpassSocketName carries the
// maker's pid; an earlier release's name without a pid, and a name of
// another shape, are not karvi's to sweep.
func TestAskpassSocketOwner(t *testing.T) {
	name := AskpassSocketName("0123456789abcdef")
	if pid, ok := askpassSocketOwner(name); !ok || pid != os.Getpid() {
		t.Fatalf("%s: pid %d ok %v", name, pid, ok)
	}
	for _, name := range []string{"askpass-0123456789abcdef.sock", "askpass-12-0123456789ABCDEF.sock", "askpass-12-0123456789abcde.sock", "askpass-0-0123456789abcdef.sock", "askpass-x-0123456789abcdef.sock", "askpass-12-0123456789abcdef.txt", "askpass-notes.txt"} {
		if _, ok := askpassSocketOwner(name); ok {
			t.Fatalf("%s taken as karvi's", name)
		}
	}
}

// TestSweepScratch: a scratch file and an askpass socket whose pid is not
// alive as karvi are removed and reported; a file and a socket whose maker
// lives, a name of another shape (an earlier release's socket among them),
// and a directory are left.
func TestSweepScratch(t *testing.T) {
	const live = 4242
	saved := scratchOwnerAlive
	scratchOwnerAlive = func(pid int) bool { return pid == live }
	t.Cleanup(func() { scratchOwnerAlive = saved })
	dir := testsocket.Dir(t)
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dead := "karvi-ssh-" + strconv.Itoa(live+1) + "-17.conf"
	deadTiming := "karvi-script-" + strconv.Itoa(live+1) + "-18.timing"
	write(dead)
	write(deadTiming)
	write("karvi-ssh-" + strconv.Itoa(live) + "-19.conf")
	write("karvi-ssh-1234567.conf")
	write("askpass-notes.txt")
	if err := os.Mkdir(filepath.Join(dir, "karvi-ssh-1-1.conf"), 0o700); err != nil {
		t.Fatal(err)
	}
	gone := "askpass-" + strconv.Itoa(live+1) + "-0123456789abcdef.sock"
	for _, name := range []string{gone, "askpass-" + strconv.Itoa(live) + "-fedcba9876543210.sock", "askpass-0123456789abcdef.sock"} {
		l, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, name), Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		l.SetUnlinkOnClose(false)
		l.Close()
	}
	var logged []string
	removed := SweepScratch(dir, func(name string) { logged = append(logged, name) })
	want := []string{gone, dead, deadTiming}
	sort.Strings(want)
	sort.Strings(removed)
	if strings.Join(removed, " ") != strings.Join(want, " ") || len(logged) != len(want) {
		t.Fatalf("removed %v logged %v, want %v", removed, logged, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if len(left) != 6 {
		t.Fatalf("left %v", left)
	}
	// An absent scratch holds nothing and is not made.
	absent := filepath.Join(t.TempDir(), "scratch")
	if SweepScratch(absent, nil) != nil {
		t.Fatal("an absent scratch swept")
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("the sweep made the scratch: %v", err)
	}
}

// TestSweepScratchConnectsToNoSocket: the sweep judges an askpass socket by
// its name alone. A broker serves one connection, so a sweep that connected
// would take a live broker's one connection, and the job's helper would
// find no socket; a socket of a maker that died is removed unconnected too.
func TestSweepScratchConnectsToNoSocket(t *testing.T) {
	const live = 4242
	saved := scratchOwnerAlive
	scratchOwnerAlive = func(pid int) bool { return pid == live }
	t.Cleanup(func() { scratchOwnerAlive = saved })
	dir := testsocket.Dir(t)
	accepted := make(chan string, 2)
	for _, pid := range []int{live, live + 1} {
		name := "askpass-" + strconv.Itoa(pid) + "-0123456789abcdef.sock"
		l, err := net.Listen("unix", filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { l.Close() })
		go func() {
			if c, err := l.Accept(); err == nil {
				c.Close()
				accepted <- name
			}
		}()
	}
	removed := SweepScratch(dir, nil)
	if len(removed) != 1 || removed[0] != "askpass-"+strconv.Itoa(live+1)+"-0123456789abcdef.sock" {
		t.Fatalf("removed %v", removed)
	}
	select {
	case name := <-accepted:
		t.Fatalf("the sweep connected to %s", name)
	case <-time.After(200 * time.Millisecond):
	}
}
