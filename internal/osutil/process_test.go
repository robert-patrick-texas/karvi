package osutil

import (
	"os"
	"strings"
	"testing"
)

func TestParseCPUList(t *testing.T) {
	for input, want := range map[string]int{"0": 1, "0-3": 4, "0-3,8,10-11": 7, "": 0} {
		if got := parseCPUList(input); got != want {
			t.Fatalf("parseCPUList(%q)=%d want %d", input, got, want)
		}
	}
}

func TestApplyGOMAXPROCSMatchesEffectiveCPU(t *testing.T) {
	p := ApplyGOMAXPROCS()
	if p.EffectiveCPUs < 1 || p.GOMAXPROCS != p.EffectiveCPUs {
		t.Fatalf("unexpected CPU profile: %+v", p)
	}
}

// TestProcessCommandName: the sweep's owner check reads argv[0]'s base name
// of a live process and nothing of a dead or absent one.
func TestProcessCommandName(t *testing.T) {
	if got := ProcessCommandName(os.Getpid()); got == "" || strings.Contains(got, "/") {
		t.Fatalf("own name %q", got)
	}
	if got := ProcessCommandName(0); got != "" {
		t.Fatalf("pid 0: %q", got)
	}
}

// TestProcessAliveOtherUser: init belongs to root, so an unprivileged
// signal check answers EPERM; the process is alive all the same, as another
// operator's lease holder is to the capacity ledger's reaper.
func TestProcessAliveOtherUser(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root signals every process")
	}
	start := ProcessStartIdentity(1)
	if start == "" {
		t.Skip("no /proc/1/stat")
	}
	if !ProcessAlive(1, start) {
		t.Fatal("pid 1 judged dead: EPERM from the signal check is a live process of another user")
	}
	if ProcessAlive(1, start+"0") {
		t.Fatal("pid 1 with another start identity judged alive")
	}
}
