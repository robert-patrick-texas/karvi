package app

import (
	"errors"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

func TestLoginOpenHostKeyFailureUsesRegisteredExit(t *testing.T) {
	result := failedResult("connection_open_failed", errors.New("host_key_not_enrolled: secure mode"))
	if result.ExitCode != exitcode.ExitHostKeyFailure {
		t.Fatalf("got exit %d", result.ExitCode)
	}
	if result.Error != "host_key_not_enrolled: secure mode" {
		t.Fatalf("error = %q", result.Error)
	}
}

func TestFailedResultCodesUncodedErrorsAtTheSite(t *testing.T) {
	result := failedResult("scoreboard_write_failed", errors.New("disk full"))
	if result.ExitCode != exitcode.ExitOutputFailure || result.Error != "scoreboard_write_failed: disk full" {
		t.Fatalf("result = %+v", result)
	}
}
