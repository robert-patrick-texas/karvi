package jobexec

import (
	"bytes"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/records"
)

// kindRecord is a synthetic record of one device for the text display rules
// of the record kinds and the stopped session-init records.
func kindRecord(device, kind string, index, count int, command, status, output string, err *records.StructuredError) records.CommandRecord {
	r := records.CommandRecord{
		Device: records.DeviceProjection{ID: "name:" + device, CanonicalName: device}, InputTarget: device, SelectedAddress: "192.0.2.1",
		Platform: "cisco_iosxe", Transport: "system", CommandKind: kind, SessionInitProfile: "none",
		CommandIndex: index, CommandCount: count, Command: command, Status: status, Output: output, OutputEncoding: "utf-8",
		Error: err, Notices: []records.Notice{}, Timing: records.Timing{EndedAt: time.Now()},
	}
	if kind == "session_init" {
		r.SessionInitProfile = "iosxe-init"
	}
	if status == "succeeded" || status == "device_error" { // sent: the prompt was observed
		r.Prompt, r.PromptSource = device+"#", "observed"
	}
	return r
}

var deviceErr = &records.StructuredError{Code: "device_command_error", Category: "device", Message: "device reported a command error", Operation: "session_init"}

// renderKinds renders records through one renderer of the activity with a
// dashed border and a short header, and returns the output and the renderer.
func renderKinds(t *testing.T, activity string, quiet, echo bool, sets []string, recs ...records.CommandRecord) (string, *recordRenderer) {
	t.Helper()
	sets = append([]string{
		"display." + activity + ".header=\"<target> header\"",
		"display." + activity + ".border=\"<repeat:-:4>\\n\"",
		"display.command.footer=",
		"display.run.footer=",
	}, sets...)
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: sets})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r, err := newRecordRenderer(&out, "text", cfg, quiet, false, "activity-1", "/tmp/artifacts", activity, echo, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	if err := r.WriteFooter(time.Now(), 0, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	return out.String(), r
}

// TestSucceededSessionInitIsHiddenAndCountedApart: the profile's succeeded
// records show nothing (the header waits for the first shown record, no
// border is added), the ping line precedes the device's first record though
// that record is hidden, and the counts are split by kind.
func TestSucceededSessionInitIsHiddenAndCountedApart(t *testing.T) {
	first := kindRecord("sw1", "session_init", 1, 2, "terminal width 511", "succeeded", "", nil)
	first.Ping = &records.PingReport{Address: "192.0.2.1", Family: "ipv4", Method: "socket", ExecutionEndpoint: "local", Probes: 2, Outcomes: []records.PingOutcome{replyOut, fastOut}, Replies: 2, Decision: records.PingDecisionProceed}
	second := kindRecord("sw1", "session_init", 2, 2, "show clock", "succeeded", "*10:00\n", nil)
	second.Ping = first.Ping
	requested := kindRecord("sw1", "requested", 1, 2, "show version", "succeeded", "IOS XE\n", nil)
	requested.Ping = first.Ping
	requested2 := kindRecord("sw1", "requested", 2, 2, "show users", "succeeded", "users\n", nil)
	requested2.Ping = first.Ping
	got, r := renderKinds(t, "run", false, false, nil, first, second, requested, requested2)
	want := "! sw1 [192.0.2.1] ping(1) 2.1ms, ping(2) <0.1ms, proceeding\nsw1 header\nIOS XE\n----\nusers\n"
	if got != want {
		t.Fatalf("output:\n%q\nwant:\n%q", got, want)
	}
	if c := r.RequestedCounts(); len(c) != 1 || c["succeeded"] != 2 {
		t.Errorf("requested counts %v", c)
	}
	if c := r.SessionInitCounts(); len(c) != 1 || c["succeeded"] != 2 {
		t.Errorf("session-init counts %v", c)
	}
	if _, empty := renderKinds(t, "run", false, false, nil, requested); len(empty.SessionInitCounts()) != 0 {
		t.Errorf("session-init counts without session-init records: %v", empty.SessionInitCounts())
	}
}

// TestFailedSessionInitIsShownWithItsKind: a session-init record with an
// error object is shown as a failed requested one, its line naming the
// profile and position; the command text only under --echo; the rest of the
// profile, not sent, is hidden; the stopped requested records stack as one
// line each without borders, under --quiet too.
func TestFailedSessionInitIsShownWithItsKind(t *testing.T) {
	recs := []records.CommandRecord{
		kindRecord("sw1", "session_init", 1, 2, "show bogus", "device_error", "% Invalid input\n", deviceErr),
		kindRecord("sw1", "session_init", 2, 2, "show clock", "not_attempted_prior_command_failure", "", nil),
		kindRecord("sw1", "requested", 1, 2, "show version", "not_attempted_session_init_failure", "", nil),
		kindRecord("sw1", "requested", 2, 2, "show users", "not_attempted_session_init_failure", "", nil),
		kindRecord("sw2", "requested", 1, 1, "show version", "succeeded", "IOS XE\n", nil),
	}
	failure := "karvi: target=sw1 session_init=iosxe-init command=1/2 status=device_error error=device_command_error: device reported a command error\n"
	stopped := "karvi: target=sw1 status=not_attempted_session_init_failure command=1/2\nkarvi: target=sw1 status=not_attempted_session_init_failure command=2/2\n"
	got, _ := renderKinds(t, "run", false, false, nil, recs...)
	if want := "sw1 header\n% Invalid input\n" + failure + stopped + "----\nsw2 header\nIOS XE\n"; got != want {
		t.Fatalf("output:\n%q\nwant:\n%q", got, want)
	}
	got, _ = renderKinds(t, "run", false, true, nil, recs...)
	if want := "sw1 header\nsw1#show bogus\n% Invalid input\n" + failure + stopped + "----\nsw2 header\nsw2#show version\nIOS XE\n"; got != want {
		t.Fatalf("--echo output:\n%q\nwant:\n%q", got, want)
	}
	got, _ = renderKinds(t, "run", true, false, nil, recs...)
	if want := "% Invalid input\n" + failure + stopped + "IOS XE\n"; got != want {
		t.Fatalf("--quiet output:\n%q\nwant:\n%q", got, want)
	}
}

// TestNotAttemptedRequestedLinesStackUnderTheFailure: the default halt's
// not-attempted records render one line each in command and run, with the
// border kept for the next shown record (ex22's N1-N3).
func TestNotAttemptedRequestedLinesStackUnderTheFailure(t *testing.T) {
	failed := kindRecord("sw1", "requested", 1, 3, "show bogus", "device_error", "% Invalid input\n", &records.StructuredError{Code: "device_command_error", Category: "device", Message: "device reported a command error", Operation: "execute_command"})
	rest := []records.CommandRecord{
		failed,
		kindRecord("sw1", "requested", 2, 3, "show version", "not_attempted_prior_command_failure", "", nil),
		kindRecord("sw1", "requested", 3, 3, "show clock", "not_attempted_prior_command_failure", "", nil),
	}
	lines := "% Invalid input\nkarvi: target=sw1 status=device_error error=device_command_error: device reported a command error\nkarvi: target=sw1 status=not_attempted_prior_command_failure command=2/3\nkarvi: target=sw1 status=not_attempted_prior_command_failure command=3/3\n"
	for _, activity := range []string{"command", "run"} {
		got, r := renderKinds(t, activity, false, false, nil, rest...)
		if want := "sw1 header\n" + lines; got != want {
			t.Errorf("%s output:\n%q\nwant:\n%q", activity, got, want)
		}
		if c := r.RequestedCounts(); c["device_error"] != 1 || c["not_attempted_prior_command_failure"] != 2 || len(r.SessionInitCounts()) != 0 {
			t.Errorf("%s counts %v %v", activity, c, r.SessionInitCounts())
		}
		if got, _ := renderKinds(t, activity, true, false, nil, rest...); got != lines {
			t.Errorf("%s --quiet output:\n%q\nwant:\n%q", activity, got, lines)
		}
	}
}

// TestPingLineOncePerDeviceWhenDevicesInterleave: run's parallel devices
// interleave; each device's ping line precedes its first record only.
func TestPingLineOncePerDeviceWhenDevicesInterleave(t *testing.T) {
	ping := &records.PingReport{Address: "192.0.2.1", Family: "ipv4", Method: "socket", ExecutionEndpoint: "local", Probes: 2, Outcomes: []records.PingOutcome{replyOut, fastOut}, Replies: 2, Decision: records.PingDecisionProceed}
	var recs []records.CommandRecord
	for _, spec := range []struct {
		device string
		index  int
	}{{"sw1", 1}, {"sw2", 1}, {"sw1", 2}, {"sw2", 2}} {
		rec := kindRecord(spec.device, "requested", spec.index, 2, "show clock", "succeeded", "ok\n", nil)
		rec.Ping = ping
		recs = append(recs, rec)
	}
	got, _ := renderKinds(t, "run", false, false, nil, recs...)
	want := "! sw1 [192.0.2.1] ping(1) 2.1ms, ping(2) <0.1ms, proceeding\nsw1 header\nok\n----\n! sw2 [192.0.2.1] ping(1) 2.1ms, ping(2) <0.1ms, proceeding\nsw2 header\nok\n----\nsw1 header\nok\n----\nsw2 header\nok\n"
	if got != want {
		t.Fatalf("output:\n%q\nwant:\n%q", got, want)
	}
}

// TestStoppedSessionInitRecordsAreHidden:
// a session-init record not started, cancelled, or stopped by a shutdown
// carries its error object but is hidden, since the device's requested
// records show the same status; the counts still include it.
func TestStoppedSessionInitRecordsAreHidden(t *testing.T) {
	for _, tc := range []struct {
		status string
		err    *records.StructuredError
	}{
		{"not_started_halt", &records.StructuredError{Code: "halt_error_count", Category: "shutdown", Message: "halt_error_count", Operation: "dispatch"}},
		{"not_started_wave_gate", &records.StructuredError{Code: "wave_gate_error_count", Category: "shutdown", Message: "wave_gate_error_count", Operation: "dispatch"}},
		{"cancelled", &records.StructuredError{Code: "cancelled", Category: "shutdown", Message: "the job was cancelled by the operator", Operation: "session_init"}},
		{"incomplete_shutdown", &records.StructuredError{Code: "shutdown_incomplete", Category: "shutdown", Message: "the daemon was stopped", Operation: "session_init"}},
	} {
		requestedErr := *tc.err
		requestedErr.Operation = "execute_command"
		if tc.err.Operation == "dispatch" {
			requestedErr.Operation = "dispatch"
		}
		recs := []records.CommandRecord{
			kindRecord("sw1", "session_init", 1, 2, "terminal width 511", tc.status, "", tc.err),
			kindRecord("sw1", "session_init", 2, 2, "show clock", tc.status, "", tc.err),
			kindRecord("sw1", "requested", 1, 1, "show version", tc.status, "", &requestedErr),
			kindRecord("sw2", "requested", 1, 1, "show version", "succeeded", "IOS XE\n", nil),
		}
		line := "karvi: target=sw1 status=" + tc.status + " error=" + tc.err.Code + ": " + tc.err.Message + "\n"
		got, r := renderKinds(t, "run", false, false, nil, recs...)
		if want := "sw1 header\n" + line + "----\nsw2 header\nIOS XE\n"; got != want {
			t.Errorf("%s output:\n%q\nwant:\n%q", tc.status, got, want)
		}
		if c := r.SessionInitCounts(); c[tc.status] != 2 {
			t.Errorf("%s session-init counts %v", tc.status, c)
		}
		if got, _ := renderKinds(t, "run", true, false, nil, recs...); got != line+"IOS XE\n" {
			t.Errorf("%s --quiet output:\n%q", tc.status, got)
		}
	}
}
