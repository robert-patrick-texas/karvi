package executor

import (
	"context"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
)

// TestTargetNoticesOnTheFirstRecord: the plan target's planning notices
// are written on the device's
// first record, with the details as the client wrote them, and on no other
// record; a target without notices adds none.
func TestTargetNoticesOnTheFirstRecord(t *testing.T) {
	h := newGateHarness(t, executionplan.PingSettings{}, nil)
	h.target.Notices = []executionplan.TargetNotice{{Code: "platform_unknown_fallback", Message: "platform \"cisco_iosx\" is not a known platform; proceeding as generic (platform-resolution.on-unknown = \"warn\")", Details: map[string]string{"supplied": "cisco_iosx", "source": "lab line 3", "used": "generic"}}}
	h.exec.opts.Commands = []string{"show clock", "show version"}
	_, recs := h.run(context.Background())
	if len(recs) != 2 {
		t.Fatalf("records %d", len(recs))
	}
	if noticeCodes(recs[0]) != "platform_unknown_fallback" || noticeCodes(recs[1]) != "" {
		t.Fatalf("notices: first %q second %q", noticeCodes(recs[0]), noticeCodes(recs[1]))
	}
	n := recs[0].Notices[0]
	if n.Details["supplied"] != "cisco_iosx" || n.Details["source"] != "lab line 3" || n.Details["used"] != "generic" || n.Message == "" {
		t.Fatalf("notice %+v", n)
	}
}

// TestUnknownPlatformRefusedBeforeConnection is the executor's backstop: a
// plan target naming a platform this configuration does not
// know (a daemon without the client's alias table) is refused before any
// connection with platform_unknown on every record, never driven as
// generic.
func TestUnknownPlatformRefusedBeforeConnection(t *testing.T) {
	h := newGateHarness(t, executionplan.PingSettings{}, nil)
	h.target.Device.Platform = "c9300"
	h.target.Notices = []executionplan.TargetNotice{{Code: "platform_not_set", Message: "no platform; proceeding as c9300 (platform-resolution.default)"}}
	h.exec.opts.Commands = []string{"show clock", "show version"}
	res, recs := h.run(context.Background())
	if res.Success || res.ErrorCode != "platform_unknown" || h.transportAttempted() {
		t.Fatalf("result %+v transport attempted=%v", res, h.transportAttempted())
	}
	if len(recs) != 2 || recs[0].Status != "connection_error" || recs[0].Error == nil || recs[0].Error.Code != "platform_unknown" || recs[1].Status != "not_attempted_prior_command_failure" {
		t.Fatalf("records %+v", recs)
	}
	if recs[0].Platform != "c9300" {
		t.Fatalf("the record's platform %q, want the plan's c9300", recs[0].Platform)
	}
	// A failure set's first record carries the planning notice too
	// (whatever the record's kind or status).
	if noticeCodes(recs[0]) != "platform_not_set" || noticeCodes(recs[1]) != "" {
		t.Fatalf("notices on the failure set: first %q second %q", noticeCodes(recs[0]), noticeCodes(recs[1]))
	}
}
