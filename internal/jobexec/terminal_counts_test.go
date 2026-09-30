package jobexec

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/dispatch"
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
