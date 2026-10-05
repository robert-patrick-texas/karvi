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

// TestRecordChannel: a record is a shell's or an exec channel's; an unset
// channel is written shell, and a shell record carries no exec field; an
// exec record's stderr names its encoding.
func TestRecordChannel(t *testing.T) {
	status, empty, bad, utf := 0, "", "latin-1", "utf-8"
	for _, tc := range []struct {
		name string
		edit func(*CommandRecord)
		code string
	}{
		{"unset", func(r *CommandRecord) {}, ""},
		{"exec that ran", func(r *CommandRecord) {
			r.Channel, r.ExitStatus, r.Stderr, r.StderrEncoding, r.PromptSource = ChannelExec, &status, &empty, &utf, PromptSourceNone
		}, ""},
		{"exec not run", func(r *CommandRecord) { r.Channel = ChannelExec }, ""},
		{"unknown", func(r *CommandRecord) { r.Channel = "netconf" }, "record_channel_invalid"},
		{"shell with a status", func(r *CommandRecord) { r.Channel, r.ExitStatus = ChannelShell, &status }, "record_channel_invalid"},
		{"exec stderr without encoding", func(r *CommandRecord) { r.Channel, r.Stderr = ChannelExec, &empty }, "record_stderr_encoding_invalid"},
		{"exec stderr bad encoding", func(r *CommandRecord) { r.Channel, r.Stderr, r.StderrEncoding = ChannelExec, &empty, &bad }, "record_stderr_encoding_invalid"},
	} {
		r := CommandRecord{RecordID: "r", ActivityID: "a", ActivityType: "command", Sequence: 1, CommandKind: "requested", Status: "succeeded", OutputEncoding: "utf-8"}
		tc.edit(&r)
		if got := errorcodes.Of(r.Validate()); got != tc.code {
			t.Errorf("%s: code %q, want %q", tc.name, got, tc.code)
		}
		if tc.name == "unset" && r.Channel != ChannelShell {
			t.Errorf("an unset channel is written %q", r.Channel)
		}
	}
}
