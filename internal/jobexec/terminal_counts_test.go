package jobexec

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/dispatch"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestTerminalCountsMoveCancelledDevicesOutOfFailed: a device the cancel
// interrupted is counted cancelled, neither
// completed nor failed; one that failed on its own before the cancel keeps
// its failure; the devices never started join the cancelled count.
func TestTerminalCountsMoveCancelledDevicesOutOfFailed(t *testing.T) {
	s := dispatch.Summary{
		Counts:  dispatch.Counts{Total: 4, Terminal: 3, Succeeded: 1, Failed: 2, NotStarted: 1},
		Results: []dispatch.Result{{Success: true}, {Success: false, ErrorCode: "ssh_process_failed"}, {Success: false, ErrorCode: "cancelled"}},
	}
	incomplete, cancelled := terminalCounts(&s, nil, true)
	if incomplete != 0 || cancelled != 2 {
		t.Errorf("incomplete=%d cancelled=%d, want 0 and 2", incomplete, cancelled)
	}
	if s.Counts.Terminal != 2 || s.Counts.Succeeded != 1 || s.Counts.Failed != 1 || s.Counts.NotStarted != 0 {
		t.Errorf("counts after: %+v", s.Counts)
	}
	if s.Counts.Terminal+s.Counts.NotStarted+incomplete+cancelled != s.Counts.Total {
		t.Errorf("counts do not partition the total: %+v + %d + %d", s.Counts, incomplete, cancelled)
	}
}

// TestTerminalCountsUnderShutdownAreUnchanged:
// a shutdown cause counts the interrupted and unstarted devices incomplete.
func TestTerminalCountsUnderShutdownAreUnchanged(t *testing.T) {
	s := dispatch.Summary{
		Counts:  dispatch.Counts{Total: 3, Terminal: 2, Succeeded: 1, Failed: 1, NotStarted: 1},
		Results: []dispatch.Result{{Success: true}, {Success: false, ErrorCode: "shutdown_incomplete"}},
	}
	incomplete, cancelled := terminalCounts(&s, ErrShutdownForced, true)
	if incomplete != 2 || cancelled != 0 || s.Counts.Terminal != 1 || s.Counts.Failed != 0 || s.Counts.NotStarted != 0 {
		t.Errorf("incomplete=%d cancelled=%d counts=%+v", incomplete, cancelled, s.Counts)
	}
}

// TestTerminalCountsLeaveAnOrdinaryRunAlone.
func TestTerminalCountsLeaveAnOrdinaryRunAlone(t *testing.T) {
	s := dispatch.Summary{Counts: dispatch.Counts{Total: 2, Terminal: 2, Succeeded: 1, Failed: 1}, Results: []dispatch.Result{{Success: true}, {Success: false, ErrorCode: "ssh_process_failed"}}}
	if incomplete, cancelled := terminalCounts(&s, nil, false); incomplete != 0 || cancelled != 0 || s.Counts.Failed != 1 || s.Counts.Terminal != 2 {
		t.Errorf("incomplete=%d cancelled=%d counts=%+v", incomplete, cancelled, s.Counts)
	}
}

// TestDeviceOutcomesAgreeWithTheCounts: the summary's devices, in the
// plan's order whatever the order of the results, each take the category
// terminalCounts counts it in: a device that failed on its own before the
// cancel keeps its failure, the one the cancel interrupted and the one never
// started are cancelled; under a shutdown both are incomplete; in an ordinary
// run with a halt the device never started is not_started.
func TestDeviceOutcomesAgreeWithTheCounts(t *testing.T) {
	targets := []executionplan.ExecutionTarget{{TargetID: "name:a"}, {TargetID: "name:b"}, {TargetID: "name:c"}, {TargetID: "name:d"}}
	devices := []records.SummaryDevice{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}
	results := func(cut string) []dispatch.Result {
		return []dispatch.Result{
			{Task: dispatch.Task{Key: "name:c"}, Success: false, ErrorCode: cut},
			{Task: dispatch.Task{Key: "name:a"}, Success: true},
			{Task: dispatch.Task{Key: "name:b"}, Success: false, ErrorCode: "ssh_process_failed"},
		}
	}
	for _, c := range []struct {
		name      string
		cut       string
		shutdown  error
		cancelled bool
		want      string
	}{
		{"a cancel", "cancelled", nil, true, "a=succeeded b=failed c=cancelled d=cancelled"},
		{"a shutdown", "shutdown_incomplete", ErrShutdownForced, true, "a=succeeded b=failed c=incomplete d=incomplete"},
		{"a halt", "host_key_mismatch", nil, false, "a=succeeded b=failed c=failed d=not_started"},
	} {
		s := dispatch.Summary{Counts: dispatch.Counts{Total: 4, Terminal: 3, Succeeded: 1, Failed: 2, NotStarted: 1}, Results: results(c.cut)}
		got := deviceOutcomes(devices, targets, s, c.shutdown, c.cancelled)
		var words []string
		counted := map[string]int{}
		for _, d := range got {
			words = append(words, d.Name+"="+d.Outcome)
			counted[d.Outcome]++
		}
		if strings.Join(words, " ") != c.want {
			t.Errorf("%s: %s, want %s", c.name, strings.Join(words, " "), c.want)
		}
		incomplete, cancelled := terminalCounts(&s, c.shutdown, c.cancelled)
		if counted["succeeded"] != s.Counts.Succeeded || counted["failed"] != s.Counts.Failed || counted["not_started"] != s.Counts.NotStarted || counted["incomplete"] != incomplete || counted["cancelled"] != cancelled {
			t.Errorf("%s: outcomes %v against counts %+v, incomplete %d, cancelled %d", c.name, counted, s.Counts, incomplete, cancelled)
		}
		if devices[0].Outcome != "" {
			t.Fatalf("%s: the accepted devices were changed", c.name)
		}
	}
}
