package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertAccounting is the one accounting check: given a finished job
// directory, every target
// has exactly one record per command, every record's status is in the
// terminal set (records.CommandRecord.Validate), the summary's device
// counts partition the total into completed, not started, and incomplete,
// the scoreboard-style identity holds for the command counts too, and
// failed-devices.txt names each device with a non-succeeded record exactly
// once and no other. The outcome tests call it on every path: success,
// device failure, the ICMP skip, the concurrent pair, the forced stop, the
// drain, grace expiry, and the shortened grace.
func assertAccounting(t *testing.T, dir string, targets, commands int) {
	t.Helper()
	recs := readRecords(t, dir)
	perDevice := map[string]int{}
	nonSucceeded := map[string]bool{}
	for _, r := range recs {
		perDevice[r.Device.CanonicalName]++
		if r.Status != "succeeded" {
			nonSucceeded[r.Device.CanonicalName] = true
		}
	}
	if len(recs) != targets*commands {
		t.Errorf("accounting: %d records, want %d targets x %d commands", len(recs), targets, commands)
	}
	if len(perDevice) != targets {
		t.Errorf("accounting: records name %d devices, want %d", len(perDevice), targets)
	}
	for name, n := range perDevice {
		if n != commands {
			t.Errorf("accounting: %s has %d records, want %d", name, n, commands)
		}
	}
	s := readSummary(t, dir)
	c := s.DeviceCounts
	if c["total"] != targets || c["completed"]+c["not_started"]+c["incomplete"]+c["cancelled"] != c["total"] || c["succeeded"]+c["failed"] != c["completed"] {
		t.Errorf("accounting: device counts %+v do not partition %d targets", c, targets)
	}
	total := 0
	for _, n := range s.RequestedCommandCounts {
		total += n
	}
	if total != len(recs) {
		t.Errorf("accounting: requested command counts %+v sum to %d, want %d records", s.RequestedCommandCounts, total, len(recs))
	}
	failed, err := os.ReadFile(filepath.Join(dir, "failed-devices.txt"))
	if err != nil {
		t.Fatalf("accounting: %v", err)
	}
	listed := map[string]int{}
	for _, name := range strings.Fields(string(failed)) {
		listed[name]++
	}
	for name := range nonSucceeded {
		if listed[name] != 1 {
			t.Errorf("accounting: failed-devices.txt lists %s %d times, want once", name, listed[name])
		}
	}
	for name := range listed {
		if !nonSucceeded[name] {
			t.Errorf("accounting: failed-devices.txt lists %s, which has only succeeded records", name)
		}
	}
}
