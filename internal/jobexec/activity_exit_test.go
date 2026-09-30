package jobexec

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/dispatch"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

func TestDetermineExitTreatsSessionChannelRefusalAsConnectionFailure(t *testing.T) {
	summary := dispatch.Summary{
		Counts:  dispatch.Counts{Failed: 1},
		Results: []dispatch.Result{{Success: false, ErrorCode: "ssh_session_channel_refused"}},
	}
	if got := determineExit("command", summary, nil, nil, nil); got != exitcode.ExitConnectionFailure {
		t.Fatalf("exit = %d (%s), want %d (%s)", got, exitcode.ExitName(got), exitcode.ExitConnectionFailure, exitcode.ExitName(exitcode.ExitConnectionFailure))
	}
}
