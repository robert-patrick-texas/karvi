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
