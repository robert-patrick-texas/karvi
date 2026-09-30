package records

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestSessionInitRecordRequiresItsProfile: a session_init record names
// the profile it ran; a
// requested record may say none.
func TestSessionInitRecordRequiresItsProfile(t *testing.T) {
	for _, tc := range []struct {
		kind, profile, code string
	}{
		{"session_init", "iosxe-init", ""},
		{"session_init", "none", "record_session_init_profile_missing"},
		{"session_init", "", "record_session_init_profile_missing"},
		{"requested", "none", ""},
		{"requested", "iosxe-init", ""},
	} {
		r := CommandRecord{RecordID: "r", ActivityID: "a", ActivityType: "command", Sequence: 1, CommandKind: tc.kind, SessionInitProfile: tc.profile, Status: "succeeded", OutputEncoding: "utf-8"}
		if got := errorcodes.Of(r.Validate()); got != tc.code {
			t.Errorf("%s profile %q: code %q, want %q", tc.kind, tc.profile, got, tc.code)
		}
	}
}
