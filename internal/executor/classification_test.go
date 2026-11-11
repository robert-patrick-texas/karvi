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

// TestOpenStatus: an open's failure gives the first record its status: a
// device's refusal of the credentials, and a change prompt karvi does not
// answer, are authentication errors; a code openStatus does not name is left
// to the category (emitFailureSet).
func TestOpenStatus(t *testing.T) {
	for code, want := range map[string]string{
		"authentication_failed":           "authentication_error",
		"authentication_change_requested": "authentication_error",
		"privilege_failed":                "privilege_error",
		"paging_disable_failed":           "device_error",
		"connection_refused":              "",
	} {
		if got := openStatus(code); got != want {
			t.Errorf("openStatus(%q) = %q, want %q", code, got, want)
		}
	}
}
