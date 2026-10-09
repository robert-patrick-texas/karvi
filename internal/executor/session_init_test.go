package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/dispatch"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/icmpgate"
	"github.com/robert-patrick-texas/karvi/records"
)

// withProfile gives the harness's target the session-init profile init
// from the plan's table, with at most two requested commands. A zero
// timeout is
// execution.command-timeout.
func (h *gateHarness) withProfile(onError string, timeout time.Duration, commands ...string) {
	if commands == nil {
		commands = []string{}
	}
	h.target.SessionInitProfile = "init"
	if len(h.exec.opts.Commands) > 2 {
		// The gate harness's plan holds four requested commands; two are
		// enough for a profile's records.
		h.exec.opts.Commands = h.exec.opts.Commands[:2]
	}
	h.exec.opts.SessionInit = map[string]executionplan.SessionInitProfile{"init": {Commands: commands, OnError: onError, CommandTimeoutNS: int64(timeout)}}
}

// deviceSawAll is every line the device received but the paging commands
// and exit.
func (h *sessionHarness) deviceSawAll() string {
	data, _ := os.ReadFile(h.log)
	out := []string{}
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		switch l {
		case "", "exit", "terminal length 0", "terminal width 512":
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "|")
}

// kinds renders each record as kind:index/count:status:code.
func kinds(recs []records.CommandRecord) string {
	parts := []string{}
	for _, r := range recs {
		code := ""
		if r.Error != nil {
			code = r.Error.Code + "@" + r.Error.Operation
		}
		parts = append(parts, fmt.Sprintf("%s:%d/%d:%s:%s", r.CommandKind, r.CommandIndex, r.CommandCount, r.Status, code))
	}
	return strings.Join(parts, " ")
}

func expectKinds(t *testing.T, recs []records.CommandRecord, want ...string) {
	t.Helper()
	if got := kinds(recs); got != strings.Join(want, " ") {
		t.Fatalf("records:\n got %s\nwant %s", got, strings.Join(want, " "))
	}
}

// expectProfileTiming checks every record's profile name and that
// session_init_ns is set exactly on the requested records when set is true.
func expectProfileTiming(t *testing.T, recs []records.CommandRecord, profile string, set bool) {
	t.Helper()
	for _, r := range recs {
		if r.SessionInitProfile != profile {
			t.Errorf("%s %d session_init_profile %q, want %q", r.CommandKind, r.CommandIndex, r.SessionInitProfile, profile)
		}
		want := set && r.CommandKind == kindRequested
		if (r.Timing.SessionInitNS != nil) != want {
			t.Errorf("%s %d session_init_ns %v, want set=%v", r.CommandKind, r.CommandIndex, r.Timing.SessionInitNS, want)
		}
	}
}

func noticeCodes(r records.CommandRecord) string {
	codes := []string{}
	for _, n := range r.Notices {
		codes = append(codes, n.Code)
	}
	return strings.Join(codes, ",")
}

// TestProfileRunsBeforeTheRequestedCommands: the profile
// is sent after Prepare on the same session, its records come first with
// their own index and count, connection_reused is false on the first
// command sent, and the profile's duration is on the requested record.
func TestProfileRunsBeforeTheRequestedCommands(t *testing.T) {
	h := newSessionHarness(t, []string{"show version"}, true)
	h.withProfile(executionplan.SessionInitFailDevice, 0, "terminal width 511", "show clock")
	res, recs := h.run(context.Background())
	expectKinds(t, recs, "session_init:1/2:succeeded:", "session_init:2/2:succeeded:", "requested:1/1:succeeded:")
	expectProfileTiming(t, recs, "init", true)
	if !res.Success || res.ErrorCode != "" {
		t.Fatalf("result %+v", res)
	}
	if got := h.deviceSawAll(); got != "terminal width 511|show clock|show version" {
		t.Fatalf("device saw %q", got)
	}
	reused := []string{}
	for _, r := range recs {
		reused = append(reused, fmt.Sprint(*r.ConnectionReused))
	}
	if got := strings.Join(reused, ","); got != "false,true,true" {
		t.Fatalf("connection_reused %s", got)
	}
	if ns := *recs[2].Timing.SessionInitNS; ns <= 0 || ns > recs[2].Timing.TotalNS {
		t.Fatalf("session_init_ns %d", ns)
	}
	if !h.debugHas(`device command start target="127.0.0.1" kind=session_init index=1 total=2 `) || !h.debugHas(`device command complete target="127.0.0.1" kind=session_init index=2 status=succeeded`) || !h.debugHas(`device command start target="127.0.0.1" index=1 total=1 `) {
		t.Fatal("debug lines do not mark the profile's commands")
	}
}

// TestProfileFailDeviceStopsTheDevice: a rejected profile
// command under fail-device leaves the rest of the profile not attempted and
// every requested command not_attempted_session_init_failure, without error
// objects; the device fails with the profile command's code.
func TestProfileFailDeviceStopsTheDevice(t *testing.T) {
	h := newSessionHarness(t, []string{"show version", "show users"}, false)
	h.withProfile(executionplan.SessionInitFailDevice, 0, "show bogus", "show clock")
	res, recs := h.run(context.Background())
	expectKinds(t, recs, "session_init:1/2:device_error:device_command_error@session_init", "session_init:2/2:not_attempted_prior_command_failure:", "requested:1/2:not_attempted_session_init_failure:", "requested:2/2:not_attempted_session_init_failure:")
	expectProfileTiming(t, recs, "init", true)
	if res.Success || res.ErrorCode != "device_command_error" {
		t.Fatalf("result %+v", res)
	}
	if got := h.deviceSaw(); got != "show bogus" {
		t.Fatalf("device saw %q", got)
	}
	for _, r := range recs {
		if len(r.Notices) != 0 {
			t.Fatalf("notices on %s %d: %s", r.CommandKind, r.CommandIndex, noticeCodes(r))
		}
	}
}

// TestProfileContinueSendsTheRestWithANotice: under
// continue a rejected command leaves the session usable, the rest is sent,
// the device's result is its requested commands', and requested record 1
// carries the notice.
func TestProfileContinueSendsTheRestWithANotice(t *testing.T) {
	h := newSessionHarness(t, []string{"show version", "show users"}, true)
	h.withProfile(executionplan.SessionInitContinue, 0, "show bogus", "show clock")
	res, recs := h.run(context.Background())
	expectKinds(t, recs, "session_init:1/2:device_error:device_command_error@session_init", "session_init:2/2:succeeded:", "requested:1/2:succeeded:", "requested:2/2:succeeded:")
	expectProfileTiming(t, recs, "init", true)
	if !res.Success || res.ErrorCode != "" {
		t.Fatalf("result %+v", res)
	}
	if got := h.deviceSaw(); got != "show bogus|show clock|show version|show users" {
		t.Fatalf("device saw %q", got)
	}
	if noticeCodes(recs[0]) != "" || noticeCodes(recs[1]) != "" || noticeCodes(recs[3]) != "" || noticeCodes(recs[2]) != "session_init_command_failed" {
		t.Fatalf("notices: %q %q %q %q", noticeCodes(recs[0]), noticeCodes(recs[1]), noticeCodes(recs[2]), noticeCodes(recs[3]))
	}
	n := recs[2].Notices[0]
	if n.Details["profile"] != "init" || n.Details["command_index"] != float64(1) || n.Details["code"] != "device_command_error" || n.Message != "session-init profile init command 1/2 failed (device_command_error); continuing because on-error is continue" {
		t.Fatalf("notice %+v", n)
	}
}

// TestProfileContinueStopsWhenTheSessionEnds: a timeout
// ends the session, so under continue the profile stops as under
// fail-device and nothing more is sent.
func TestProfileContinueStopsWhenTheSessionEnds(t *testing.T) {
	h := newSessionHarness(t, []string{"show version"}, false)
	h.withProfile(executionplan.SessionInitContinue, 0, "show slow", "show clock")
	res, recs := h.run(context.Background())
	expectKinds(t, recs, "session_init:1/2:timeout:command_timeout@session_init", "session_init:2/2:not_attempted_prior_command_failure:", "requested:1/1:not_attempted_session_init_failure:")
	expectProfileTiming(t, recs, "init", true)
	if res.Success || res.ErrorCode != "command_timeout" {
		t.Fatalf("result %+v", res)
	}
	if got := h.deviceSaw(); got != "show slow" {
		t.Fatalf("device saw %q", got)
	}
	if noticeCodes(recs[2]) != "" {
		t.Fatalf("notice on a stopped device: %s", noticeCodes(recs[2]))
	}
	if !h.debugHas(`device session ended target="127.0.0.1" kind=session_init index=1 remaining=2`) {
		t.Fatal("no session-ended debug line")
	}
}

// TestProfileContinueFailureThenSessionEnd: a rejected command under
// continue does not count; the session-ending failure after it is the
// device's code, and the rejected one is still a notice.
func TestProfileContinueFailureThenSessionEnd(t *testing.T) {
	h := newSessionHarness(t, []string{"show version"}, false)
	h.withProfile(executionplan.SessionInitContinue, 0, "show bogus", "show slow")
	res, recs := h.run(context.Background())
	expectKinds(t, recs, "session_init:1/2:device_error:device_command_error@session_init", "session_init:2/2:timeout:command_timeout@session_init", "requested:1/1:not_attempted_session_init_failure:")
	if res.Success || res.ErrorCode != "command_timeout" {
		t.Fatalf("result %+v, want the session-ending code", res)
	}
	if noticeCodes(recs[2]) != "session_init_command_failed" || recs[2].Notices[0].Details["command_index"] != float64(1) {
		t.Fatalf("notices %+v", recs[2].Notices)
	}
}

// TestProfileCommandTimeoutOverridesTheDefault: the profile's
// command-timeout, not execution.command-timeout (1s here), bounds its
// commands; the requested commands keep the default.
func TestProfileCommandTimeoutOverridesTheDefault(t *testing.T) {
	h := newSessionHarness(t, []string{"show version"}, true)
	h.withProfile(executionplan.SessionInitFailDevice, 5*time.Second, "show slow")
	res, recs := h.run(context.Background())
	expectKinds(t, recs, "session_init:1/1:succeeded:", "requested:1/1:succeeded:")
	if !res.Success {
		t.Fatalf("result %+v", res)
	}
	if ns := time.Duration(*recs[1].Timing.SessionInitNS); ns < 2500*time.Millisecond {
		t.Fatalf("session_init_ns %s, want the slow command's duration", ns)
	}
}

// TestProfileCancelDuringTheProfile: the command in flight and
// every later one of both kinds are cancelled with error objects; the
// profile's duration is on the requested record.
func TestProfileCancelDuringTheProfile(t *testing.T) {
	h := newSessionHarness(t, []string{"show version"}, false)
	h.withProfile(executionplan.SessionInitContinue, 5*time.Second, "show clock", "show slow")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelWhenSent(t, h.marker, cancel)
	res, recs := h.run(ctx)
	expectKinds(t, recs, "session_init:1/2:succeeded:", "session_init:2/2:cancelled:cancelled@session_init", "requested:1/1:cancelled:cancelled@execute_command")
	expectProfileTiming(t, recs, "init", true)
	if res.Success || res.ErrorCode != "cancelled" {
		t.Fatalf("result %+v", res)
	}
}

// TestProfileOpenFailure (records by path): a device
// whose session never opened keeps the failure on requested record 1; the
// profile's records are not attempted without error objects, and no
// session_init_ns is set.
func TestProfileOpenFailure(t *testing.T) {
	h := newSessionHarness(t, []string{"show version", "show users"}, true)
	script := filepath.Join(filepath.Dir(h.log), "fake-ssh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'ssh: connect to host 127.0.0.1 port 22: Connection refused' >&2\nexit 255\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.withProfile(executionplan.SessionInitFailDevice, 0, "show clock")
	res, recs := h.run(context.Background())
	if len(recs) != 3 || recs[0].CommandKind != kindSessionInit || recs[0].Status != "not_attempted_prior_command_failure" || recs[0].Error != nil ||
		recs[1].CommandKind != kindRequested || recs[1].CommandIndex != 1 || recs[1].Error == nil || recs[1].Status == "not_attempted_prior_command_failure" ||
		recs[2].Status != "not_attempted_prior_command_failure" || recs[2].Error != nil {
		t.Fatalf("records: %s", kinds(recs))
	}
	expectProfileTiming(t, recs, "init", false)
	if res.Success || res.ErrorCode != recs[1].Error.Code {
		t.Fatalf("result %+v", res)
	}
}

// TestNoProfileAndAnEmptyProfile: none writes no session-init record and
// says none; [] names the profile and sends nothing, so session_init_ns
// stays null.
func TestNoProfileAndAnEmptyProfile(t *testing.T) {
	h := newSessionHarness(t, []string{"show version"}, true)
	_, recs := h.run(context.Background())
	expectKinds(t, recs, "requested:1/1:succeeded:")
	expectProfileTiming(t, recs, executionplan.SessionInitNone, false)

	h = newSessionHarness(t, []string{"show version"}, true)
	h.withProfile(executionplan.SessionInitFailDevice, 0)
	_, recs = h.run(context.Background())
	expectKinds(t, recs, "requested:1/1:succeeded:")
	expectProfileTiming(t, recs, "init", false)
	if *recs[0].ConnectionReused {
		t.Fatal("connection_reused true on the first command sent")
	}
}

// TestProfileGateSkip: an ICMP skip keeps icmp_unreachable on requested
// record 1 and writes the profile's records first, not attempted.
func TestProfileGateSkip(t *testing.T) {
	p := &scriptedPinger{outcomes: map[string][]icmpgate.Outcome{"127.0.0.1": {timeoutOutcome(1), timeoutOutcome(2)}}}
	h := newGateHarness(t, enabled, p)
	h.withProfile(executionplan.SessionInitFailDevice, 0, "show clock")
	res, recs := h.run(context.Background())
	expectKinds(t, recs, "session_init:1/1:not_attempted_prior_command_failure:", "requested:1/2:icmp_unreachable:icmp_unreachable@icmp_gate", "requested:2/2:not_attempted_prior_command_failure:")
	expectProfileTiming(t, recs, "init", false)
	if res.ErrorCode != "icmp_unreachable" || h.transportAttempted() {
		t.Fatalf("result %+v transport attempted=%v", res, h.transportAttempted())
	}
	for _, r := range recs {
		if r.Ping == nil {
			t.Fatalf("%s %d without the ping block", r.CommandKind, r.CommandIndex)
		}
	}
}

// TestProfileGateDegradedNoticeOnTheFirstRecord: the packet-loss
// notice is on session-init record 1, the device's first record.
func TestProfileGateDegradedNoticeOnTheFirstRecord(t *testing.T) {
	p := &scriptedPinger{outcomes: map[string][]icmpgate.Outcome{"127.0.0.1": {reply(1, 2*time.Millisecond), timeoutOutcome(2)}}}
	h := newGateHarness(t, enabled, p)
	h.withProfile(executionplan.SessionInitFailDevice, 0, "show clock")
	_, recs := h.run(context.Background())
	expectKinds(t, recs, "session_init:1/1:succeeded:", "requested:1/2:succeeded:", "requested:2/2:succeeded:")
	if noticeCodes(recs[0]) != "icmp_packet_loss" || noticeCodes(recs[1]) != "" || noticeCodes(recs[2]) != "" {
		t.Fatalf("notices %q %q %q", noticeCodes(recs[0]), noticeCodes(recs[1]), noticeCodes(recs[2]))
	}
}

// TestEmitUnstartedWithAProfile: a device not started writes the profile's
// records first, each with the same status and error object as the
// requested ones.
func TestEmitUnstartedWithAProfile(t *testing.T) {
	h := newGateHarness(t, nil, nil)
	h.withProfile(executionplan.SessionInitFailDevice, 0, "terminal width 511", "show clock")
	h.exec.EmitUnstarted(Work{Target: h.target, QueuedAt: time.Now()}, dispatch.Context{Mode: "serial", ScopePosition: 1}, "not_started_halt", "halt_error_count", nil)
	recs := readRecords(t, h)
	expectKinds(t, recs, "session_init:1/2:not_started_halt:halt_error_count@dispatch", "session_init:2/2:not_started_halt:halt_error_count@dispatch", "requested:1/2:not_started_halt:halt_error_count@dispatch", "requested:2/2:not_started_halt:halt_error_count@dispatch")
	expectProfileTiming(t, recs, "init", false)
}

// TestUnknownProfileNameContactsNothing: a target naming a profile the
// plan's table does not hold is execution_target_invalid before any
// transport.
func TestUnknownProfileNameContactsNothing(t *testing.T) {
	h := newGateHarness(t, nil, nil)
	h.target.SessionInitProfile = "missing"
	res, recs := h.run(context.Background())
	if res.ErrorCode != "execution_target_invalid" || h.transportAttempted() {
		t.Fatalf("result %+v transport attempted=%v", res, h.transportAttempted())
	}
	if len(recs) != len(plantest.Commands) || recs[0].CommandKind != kindRequested || recs[0].Error == nil || recs[0].Error.Code != "execution_target_invalid" {
		t.Fatalf("records: %s", kinds(recs))
	}
}

func readRecords(t *testing.T, h *gateHarness) []records.CommandRecord {
	t.Helper()
	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(h.store.Paths().CommandsJSONL)
	if err != nil {
		t.Fatal(err)
	}
	var out []records.CommandRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r records.CommandRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		if err := r.Validate(); err != nil {
			t.Errorf("record %d: %v", r.Sequence, err)
		}
		out = append(out, r)
	}
	return out
}
