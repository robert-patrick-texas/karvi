package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/capacity"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
	"github.com/robert-patrick-texas/karvi/records"
)

// sessionHarness runs one cisco_iosxe device through the executor over a
// system transport script that logs every line it receives, answers show
// slow after three seconds (past the one-second command timeout), rejects
// show bogus with the platform's error, ends as OpenSSH does after unanswered
// keepalives on show mute, holds a [confirm] on clear counters and reload and
// a value prompt on copy running-config startup-config, and answers anything
// else; receiving show slow marks the marker file.
type sessionHarness struct {
	*gateHarness
	spool string // the executor's spool directory
	log   string
	mu    sync.Mutex
	debug []string
}

// Extra sets follow the harness's own; one naming execution.command-timeout
// replaces the one-second default.
func newSessionHarness(t *testing.T, commands []string, halt bool, sets ...string) *sessionHarness {
	t.Helper()
	dir := t.TempDir()
	log, marker := filepath.Join(dir, "device.log"), filepath.Join(dir, "slow-sent")
	script := filepath.Join(dir, "fake-ssh")
	body := "#!/bin/sh\ncase \" $* \" in *' -O '*) exit 1;; esac\nCR=$(printf '\\r')\nprintf 'dev#'\nwhile IFS= read -r line; do\n" +
		"  printf '%s\\n' \"$line\" >>\"" + log + "\"\n" +
		"  case \"$line\" in\n" +
		"    exit) exit 0;;\n" +
		"    'show slow') : >\"" + marker + "\"; sleep 3; printf '%s\\r\\nslow\\r\\ndev#' \"$line\";;\n" +
		"    'show mute') echo 'Timeout, server 127.0.0.1 not responding.' >&2; exit 255;;\n" +
		"    'show big') printf '%s\\r\\n' \"$line\"; i=0; while [ $i -lt 40 ]; do printf '%072d\\r\\n' 0; i=$((i+1)); done; printf 'dev#';;\n" +
		"    'show bogus') printf '%s\\r\\n%% Invalid input detected at marker.\\r\\ndev#' \"$line\";;\n" +
		// A [confirm] takes one key without a line; after it the prompt
		// returns (clear counters) or nothing more is written (reload).
		"    'clear counters') printf '%s\\r\\nClear counters on all interfaces [confirm]' \"$line\"; key=$(head -c1); printf '<%s>\\n' \"$key\" | tr '\\r' 'R' >>\"" + log + "\"; printf '\\r\\ndev#';;\n" +
		"    reload) printf '%s\\r\\nProceed with reload? [confirm]' \"$line\"; head -c1 >/dev/null; printf '\\r\\n'; exec sleep 30;;\n" +
		// A value prompt, with the device's trailing space, takes one line
		// ended by the return the session writes (no newline, so it is read
		// a byte at a time), echoes it, and answers [OK].
		"    'copy running-config startup-config') printf '%s\\r\\nDestination filename [startup-config]? ' \"$line\"; v=''; while k=$(head -c1) && [ -n \"$k\" ] && [ \"$k\" != \"$CR\" ]; do v=\"$v$k\"; done; printf '<value:%s>\\n' \"$v\" >>\"" + log + "\"; printf '%s\\r\\nBuilding configuration...\\r\\n[OK]\\r\\ndev#' \"$v\";;\n" +
		"    *) printf '%s\\r\\nok\\r\\ndev#' \"$line\";;\n" +
		"  esac\ndone\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	os.MkdirAll(home, 0o700)
	spool := filepath.Join(dir, "spool")
	os.MkdirAll(spool, 0o700)
	all := []string{fmt.Sprintf("ssh.transports.system=%q", script), `ssh.host-key-policy="insecure"`, fmt.Sprintf("ssh.known-hosts-file=%q", filepath.Join(dir, "known_hosts")), fmt.Sprintf("execution.halt-device-on-command-error=%t", halt)}
	if !slices.ContainsFunc(sets, func(s string) bool { return strings.HasPrefix(s, "execution.command-timeout=") }) {
		all = append(all, `execution.command-timeout="1s"`)
	}
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append(all, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	store, err := output.Create(output.Options{Root: filepath.Join(dir, "job"), ID: plantest.JobID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	capMgr, err := capacity.New(filepath.Join(dir, "capacity"), "", "", plantest.JobID, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	target := plantest.DirectTarget()
	target.Device.Platform = "cisco_iosxe"
	grant := credentialpackage.CredentialGrant{CredentialID: plantest.GrantA, Method: credentialpackage.MethodEmbeddedSecret, Username: credentials.NewSecretString("u"), Password: credentials.NewSecretString("p"), Policy: "default", Backend: "test", NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)}
	h := &sessionHarness{log: log}
	e := New(Options{
		Config: cfg, Operator: credentials.Operator{Username: "netops", UID: 1000, Home: home}, ActivityID: plantest.JobID, JobID: plantest.JobID, ActivityType: "run",
		Commands: commands, DispatchOrder: "default", Grants: grants{grant}, Protection: "local-peer",
		Capacity: capMgr, Store: store, ScratchDir: testsocket.Dir(t), ControlRoot: filepath.Join(dir, "control"), Home: home, AskpassPath: "/bin/true", SpoolDir: spool,
		Debug: func(s string) { h.mu.Lock(); h.debug = append(h.debug, s); h.mu.Unlock() },
	})
	h.gateHarness = &gateHarness{t: t, exec: e, store: store, marker: marker, target: target}
	h.spool = spool
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("debug:\n%s", strings.Join(h.debug, "\n"))
		}
	})
	return h
}

// deviceSaw is the requested commands the device received, paging removed.
func (h *sessionHarness) deviceSaw() string {
	data, _ := os.ReadFile(h.log)
	out := []string{}
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(l, "show ") || strings.HasPrefix(l, "copy ") || strings.HasPrefix(l, "<value:") || l == "clear counters" || l == "reload" || l == "<R>" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "|")
}

func (h *sessionHarness) debugHas(prefix string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.debug {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// TestContinueStopsAfterASessionEndingFailure: under
// --continue-device-on-error a command timeout ends
// the session, so the next command is not sent and is
// not_attempted_prior_command_failure without an error object.
func TestContinueStopsAfterASessionEndingFailure(t *testing.T) {
	h := newSessionHarness(t, []string{"show slow", "show version"}, false)
	res, recs := h.run(context.Background())
	if len(recs) != 2 || recs[0].Status != "timeout" || recs[1].Status != "not_attempted_prior_command_failure" || recs[1].Error != nil {
		t.Fatalf("records: %s", describe(recs))
	}
	if res.Success || res.ErrorCode != "command_timeout" {
		t.Fatalf("result %+v", res)
	}
	if got := h.deviceSaw(); got != "show slow" {
		t.Fatalf("device saw %q; nothing may follow the timeout", got)
	}
	if !h.debugHas(`device session ended target="127.0.0.1" index=1 remaining=1`) {
		t.Fatal("no session-ended debug line")
	}
}

// TestContinueSendsAfterADeviceErrorAndKeepsTheFirstFailure: a
// rejected command leaves the session usable and the next is sent; a later
// timeout ends it; the device's code is its first failure; connection_reused
// is false on the first command sent and true after it.
func TestContinueSendsAfterADeviceErrorAndKeepsTheFirstFailure(t *testing.T) {
	h := newSessionHarness(t, []string{"show bogus", "show version", "show slow", "show clock"}, false)
	res, recs := h.run(context.Background())
	want := []string{"device_error", "succeeded", "timeout", "not_attempted_prior_command_failure"}
	for i, r := range recs {
		if i >= len(want) || r.Status != want[i] {
			t.Fatalf("records: %s", describe(recs))
		}
	}
	if res.ErrorCode != "device_command_error" {
		t.Fatalf("device error code %q, want the first failure device_command_error", res.ErrorCode)
	}
	reused := []string{}
	for _, r := range recs {
		if r.ConnectionReused == nil {
			reused = append(reused, "null")
		} else {
			reused = append(reused, fmt.Sprint(*r.ConnectionReused))
		}
	}
	if got := strings.Join(reused, ","); got != "false,true,true,null" {
		t.Fatalf("connection_reused %s, want false,true,true,null", got)
	}
	if got := h.deviceSaw(); got != "show bogus|show version|show slow" {
		t.Fatalf("device saw %q", got)
	}
}

// TestDefaultHaltAfterADeviceError: the halt rule is unchanged.
func TestDefaultHaltAfterADeviceError(t *testing.T) {
	h := newSessionHarness(t, []string{"show bogus", "show version"}, true)
	res, recs := h.run(context.Background())
	if len(recs) != 2 || recs[0].Status != "device_error" || recs[1].Status != "not_attempted_prior_command_failure" || res.ErrorCode != "device_command_error" {
		t.Fatalf("result %+v records: %s", res, describe(recs))
	}
	if got := h.deviceSaw(); got != "show bogus" {
		t.Fatalf("device saw %q", got)
	}
	if h.debugHas("device session ended") {
		t.Fatal("a usable session's halt logged the session as ended")
	}
}

// TestCancelAfterAFailureKeepsTheFailure: under continue a device that failed
// and was then cancelled keeps its first failure's code, so the job counts it
// failed rather than cancelled.
func TestCancelAfterAFailureKeepsTheFailure(t *testing.T) {
	h := newSessionHarness(t, []string{"show bogus", "show slow", "show clock"}, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelWhenSent(t, h.marker, cancel)
	res, recs := h.run(ctx)
	if len(recs) != 3 || recs[0].Status != "device_error" || recs[1].Status != "cancelled" || recs[2].Status != "cancelled" {
		t.Fatalf("records: %s", describe(recs))
	}
	if res.ErrorCode != "device_command_error" {
		t.Fatalf("device code %q, want the failure before the cancel", res.ErrorCode)
	}
}

func describe(recs []records.CommandRecord) string {
	parts := []string{}
	for _, r := range recs {
		code := ""
		if r.Error != nil {
			code = r.Error.Code
		}
		parts = append(parts, fmt.Sprintf("%d:%s:%s:%s", r.CommandIndex, r.Command, r.Status, code))
	}
	return strings.Join(parts, " ")
}

// TestUnansweredKeepalivesEndTheDevice:
// OpenSSH ending the session after ServerAliveCountMax unanswered keepalives
// is connection_error with session_keepalive_timeout in karvi's words, and
// the rest is not attempted under --continue-device-on-error too.
func TestUnansweredKeepalivesEndTheDevice(t *testing.T) {
	h := newSessionHarness(t, []string{"show version", "show mute", "show clock"}, false, `ssh.server-alive-interval="2s"`, `ssh.server-alive-count-max=4`)
	res, recs := h.run(context.Background())
	if len(recs) != 3 || recs[0].Status != "succeeded" || recs[1].Status != "connection_error" || recs[2].Status != "not_attempted_prior_command_failure" {
		t.Fatalf("records: %s", describe(recs))
	}
	e := recs[1].Error
	if e == nil || e.Code != "session_keepalive_timeout" || !e.Retryable || !strings.Contains(e.Message, "none of 4 keepalives sent every 2s") || !strings.Contains(e.Message, "OpenSSH: Timeout, server 127.0.0.1 not responding.") {
		t.Fatalf("error: %+v", e)
	}
	if res.ErrorCode != "session_keepalive_timeout" {
		t.Fatalf("result: %+v", res)
	}
}

// TestBlindSendConfirmsAndGoesOn: the step carries the command's blind
// returns and the blind wait, the
// [confirm] takes the return, the record is the ordinary success with the
// exchange as its output, and the next command is sent.
func TestBlindSendConfirmsAndGoesOn(t *testing.T) {
	h := newSessionHarness(t, []string{"clear counters", "show clock"}, true, `execution.blind-wait="2s"`)
	h.exec.opts.BlindReturns = []int{1, 0}
	res, recs := h.run(context.Background())
	if !res.Success || len(recs) != 2 || recs[0].Status != "succeeded" || recs[1].Status != "succeeded" {
		t.Fatalf("records: %s", describe(recs))
	}
	if recs[0].Output != "Clear counters on all interfaces [confirm]\n" || len(recs[0].Notices) != 0 || recs[0].PromptObserved == nil || !*recs[0].PromptObserved {
		t.Fatalf("confirmed record: output %q notices %+v prompt_observed %v", recs[0].Output, recs[0].Notices, recs[0].PromptObserved)
	}
	if got := h.deviceSaw(); got != "clear counters|<R>|show clock" {
		t.Fatalf("device saw %q", got)
	}
}

// TestBlindSendWithoutAPromptEndsTheDevice: the blind wait passes
// with nothing more from the device: the record succeeds with the notice
// and all bytes read, the session is closed, the command after it is
// not_attempted_prior_command_failure naming the blind send, and the
// device fails as command_session_lost; as the last command, the device
// succeeds.
func TestBlindSendWithoutAPromptEndsTheDevice(t *testing.T) {
	h := newSessionHarness(t, []string{"reload", "show clock"}, false, `execution.blind-wait="500ms"`)
	h.exec.opts.BlindReturns = []int{1, 0}
	started := time.Now()
	res, recs := h.run(context.Background())
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("took %s; the blind wait must bound it", elapsed)
	}
	if res.Success || res.ErrorCode != "command_session_lost" || len(recs) != 2 || recs[0].Status != "succeeded" || recs[1].Status != "not_attempted_prior_command_failure" {
		t.Fatalf("result %+v records: %s", res, describe(recs))
	}
	if recs[0].Error != nil || recs[0].PromptObserved == nil || *recs[0].PromptObserved || recs[0].Prompt != "" || !strings.Contains(recs[0].Output, "Proceed with reload? [confirm]") {
		t.Fatalf("blind record: %+v", recs[0])
	}
	// The sent statement's record has the prompt it was typed at, though no
	// prompt came back; the one not sent has none.
	if !strings.HasSuffix(recs[0].PromptBefore, "#") || recs[1].PromptBefore != "" {
		t.Fatalf("promptbefore: sent %q, not sent %q", recs[0].PromptBefore, recs[1].PromptBefore)
	}
	if len(recs[0].Notices) != 1 || recs[0].Notices[0].Code != "prompt_not_observed_after_blind_send" || recs[0].Notices[0].Details["reason"] != "blind_wait_expired" {
		t.Fatalf("notices: %+v", recs[0].Notices)
	}
	if recs[1].Error == nil || recs[1].Error.Code != "command_session_lost" || !strings.Contains(recs[1].Error.Message, "blind send of command 1") {
		t.Fatalf("not-attempted record: %+v", recs[1].Error)
	}
	if got := h.deviceSaw(); got != "reload" {
		t.Fatalf("device saw %q", got)
	}

	h = newSessionHarness(t, []string{"show clock", "reload"}, false, `execution.blind-wait="500ms"`)
	h.exec.opts.BlindReturns = []int{0, 1}
	res, recs = h.run(context.Background())
	if !res.Success || len(recs) != 2 || recs[1].Status != "succeeded" || len(recs[1].Notices) != 1 {
		t.Fatalf("last-command result %+v records: %s", res, describe(recs))
	}
}

// TestExpectationAnswersTheCopyPrompt: the declaration answers the value
// prompt with a name, the exchange is the record's output, the command is
// an ordinary success, and the next command is sent. The command start
// event names the pattern and the response's byte count, not the response.
func TestExpectationAnswersTheCopyPrompt(t *testing.T) {
	h := newSessionHarness(t, []string{"copy running-config startup-config", "show clock"}, true)
	h.exec.opts.Expectations = [][]executionplan.Expectation{{{Pattern: `filename \[startup-config\]\?`, Response: "backup"}}, nil}
	res, recs := h.run(context.Background())
	if !res.Success || len(recs) != 2 || recs[0].Status != "succeeded" || recs[1].Status != "succeeded" {
		t.Fatalf("records: %s", describe(recs))
	}
	if recs[0].Output != "Destination filename [startup-config]? backup\nBuilding configuration...\n[OK]\n" || len(recs[0].Notices) != 0 || recs[0].PromptObserved == nil || !*recs[0].PromptObserved {
		t.Fatalf("copy record: output %q notices %+v prompt_observed %v", recs[0].Output, recs[0].Notices, recs[0].PromptObserved)
	}
	if got := h.deviceSaw(); got != "copy running-config startup-config|<value:backup>|show clock" {
		t.Fatalf("device saw %q", got)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	found := false
	for _, l := range h.debug {
		if strings.HasPrefix(l, "device command start ") && strings.Contains(l, "index=1 ") {
			found = true
			if !strings.Contains(l, `blind=false blind_returns=0 expectations=[{pattern:"filename \\[startup-config\\]\\?" response_bytes:6}]`) || strings.Contains(l, "backup") {
				t.Fatalf("command start event: %s", l)
			}
		}
	}
	if !found {
		t.Fatal("no command start event for the copy")
	}
}

// TestBlindFlagWithExpectations: a
// command blind by the flag alone, with its [confirm] answered by a
// declaration, is the blind success with the notice, without a returns
// clause, within the blind wait; the session ends and the command after
// it is not attempted.
func TestBlindFlagWithExpectations(t *testing.T) {
	h := newSessionHarness(t, []string{"reload", "show clock"}, false, `execution.blind-wait="500ms"`)
	h.exec.opts.Blind = []bool{true, false}
	h.exec.opts.Expectations = [][]executionplan.Expectation{{{Pattern: `confirm\]`, Response: ""}}, nil}
	started := time.Now()
	res, recs := h.run(context.Background())
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("took %s; the blind wait must bound it", elapsed)
	}
	if res.Success || res.ErrorCode != "command_session_lost" || len(recs) != 2 || recs[0].Status != "succeeded" || recs[1].Status != "not_attempted_prior_command_failure" {
		t.Fatalf("result %+v records: %s", res, describe(recs))
	}
	if recs[0].Error != nil || recs[0].PromptObserved == nil || *recs[0].PromptObserved || !strings.Contains(recs[0].Output, "Proceed with reload? [confirm]") {
		t.Fatalf("blind record: %+v", recs[0])
	}
	n := recs[0].Notices
	if len(n) != 1 || n[0].Code != "prompt_not_observed_after_blind_send" || fmt.Sprint(n[0].Details["blind_returns"]) != "0" || !strings.HasPrefix(n[0].Message, "the prompt did not return after the command: the blind wait") {
		t.Fatalf("notices: %+v", n)
	}
	if got := h.deviceSaw(); got != "reload" {
		t.Fatalf("device saw %q", got)
	}
}

// TestInvalidExpectationPatternContactsNothing: a pattern the plan's
// Validate would refuse is execution_plan_invalid before any connection,
// the executor's answer to a plan that reached it unchecked: requested
// command 1 carries the error and the rest are not attempted, as every
// failure before the first command is recorded.
func TestInvalidExpectationPatternContactsNothing(t *testing.T) {
	h := newSessionHarness(t, []string{"show clock", "show version"}, true)
	h.exec.opts.Expectations = [][]executionplan.Expectation{nil, {{Pattern: `(`, Response: ""}}}
	res, recs := h.run(context.Background())
	if res.Success || res.ErrorCode != "execution_plan_invalid" || len(recs) != 2 {
		t.Fatalf("result %+v records: %s", res, describe(recs))
	}
	if recs[0].Error == nil || recs[0].Error.Code != "execution_plan_invalid" || !strings.Contains(recs[0].Error.Message, "command 2 declaration 1") {
		t.Fatalf("first record: %+v", recs[0].Error)
	}
	if recs[1].Status != "not_attempted_prior_command_failure" || recs[1].Error != nil {
		t.Fatalf("second record: %s", describe(recs[1:]))
	}
	if got := h.deviceSaw(); got != "" {
		t.Fatalf("device saw %q; nothing must be contacted", got)
	}
}

// TestSpooledRecordIsWrittenFromItsFileAndTheSpoolRemoved is the spool at
// the executor: with the threshold at 0 every answer
// spools; the record carries the reader's count and digest and its line
// holds the output, written from the file, and the spool is removed once
// the record is appended and handed on. A timeout's record holds what
// settled by the cut, from its spool the same way.
func TestSpooledRecordIsWrittenFromItsFileAndTheSpoolRemoved(t *testing.T) {
	h := newSessionHarness(t, []string{"show version", "show slow"}, false, "output.spool-threshold-bytes=0")
	res, recs := h.run(context.Background())
	if len(recs) != 2 || recs[0].Status != "succeeded" || recs[1].Status != "timeout" {
		t.Fatalf("records: %s", describe(recs))
	}
	if res.Success || res.ErrorCode != "command_timeout" {
		t.Fatalf("result %+v", res)
	}
	want := "ok\n"
	sum := sha256.Sum256([]byte(want))
	if recs[0].Output != want || recs[0].OutputBytes != 3 || recs[0].OutputSHA256 != hex.EncodeToString(sum[:]) || recs[0].OutputEncoding != "utf-8" {
		t.Fatalf("the spooled record: output %q bytes %d sha %s enc %s", recs[0].Output, recs[0].OutputBytes, recs[0].OutputSHA256, recs[0].OutputEncoding)
	}
	// The cut: the echo was the first line and the fake had answered
	// nothing else within the second, so nothing settled; the record says
	// so through its own fields, from the same path.
	if recs[1].OutputBytes != int64(len(recs[1].Output)) {
		t.Fatalf("the timeout's record: %d bytes for %q", recs[1].OutputBytes, recs[1].Output)
	}
	entries, err := os.ReadDir(h.spool)
	if err != nil || len(entries) != 0 {
		t.Fatalf("spools left after the records: %v (%v)", entries, err)
	}
	if !h.debugHas("device command spooled target=") || !h.debugHas("device command spool removed target=") {
		t.Fatalf("the spool's opening and removal are not in the debug stream:\n%s", strings.Join(h.debug, "\n"))
	}
}
