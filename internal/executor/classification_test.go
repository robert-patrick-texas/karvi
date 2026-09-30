package executor

import (
	"errors"
	"testing"
)

func TestClassifyOpenPreservesSessionChannelRefusal(t *testing.T) {
	code, category := classifyOpen(errors.New("ssh_session_channel_refused: Master refused session request: Permission denied"))
	if code != "ssh_session_channel_refused" || category != "connection" {
		t.Fatalf("classification = %q/%q", code, category)
	}
}
