package app

import (
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
)

// TestReportShowsEachSleepInItsPlace: the dry-run text names the plan's
// sleeps by the command each precedes, and a device's own list shows each in
// its place (EXAMPLES chapter 95); none, nothing.
func TestReportShowsEachSleepInItsPlace(t *testing.T) {
	if got := describeSleeps([]int64{0, int64(90 * time.Second), 0, int64(5 * time.Second)}); got != "1m30s before command 2; 5s before command 4" {
		t.Fatalf("describeSleeps: %q", got)
	}
	if got := describeSleeps([]int64{}); got != "" {
		t.Fatalf("no sleep: %q", got)
	}
	list := executionplan.CommandList{Commands: []string{"clear counters", "show interfaces counters"}, SleepsNS: []int64{0, int64(20 * time.Second)}}
	if got := joinCommands(list); got != "clear counters; --sleep 20s; show interfaces counters" {
		t.Fatalf("joinCommands: %q", got)
	}
	list.SleepsNS = []int64{}
	if got := joinCommands(list); got != "clear counters; show interfaces counters" {
		t.Fatalf("joinCommands, no sleep: %q", got)
	}
}
