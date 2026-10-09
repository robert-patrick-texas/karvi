package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/dispatch"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/capacity"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/icmpgate"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
	"github.com/robert-patrick-texas/karvi/records"
)

// scriptedPinger answers each address from a script and counts its calls
// (the injected Pinger).
type scriptedPinger struct {
	outcomes map[string][]icmpgate.Outcome
	err      error
	calls    int
	onProbe  func()
}

func (p *scriptedPinger) Method() string { return icmpgate.MethodSocket }
func (p *scriptedPinger) Probe(ctx context.Context, address netip.Addr, count int, timeout time.Duration) ([]icmpgate.Outcome, error) {
	p.calls++
	if p.onProbe != nil {
		p.onProbe()
	}
	if p.err != nil {
		return nil, p.err
	}
	return p.outcomes[address.String()], nil
}

func reply(seq int, rtt time.Duration) icmpgate.Outcome {
	ns := rtt.Nanoseconds()
	return icmpgate.Outcome{Sequence: seq, SentAt: time.Now(), Status: icmpgate.StatusReply, RTTNS: &ns, From: "127.0.0.1"}
}
func timeoutOutcome(seq int) icmpgate.Outcome {
	return icmpgate.Outcome{Sequence: seq, SentAt: time.Now(), Status: icmpgate.StatusTimeout}
}
func errorOutcome(seq int) icmpgate.Outcome {
	return icmpgate.Outcome{Sequence: seq, SentAt: time.Now(), Status: icmpgate.StatusError, From: "2001:db8::ff", Detail: "Destination unreachable: No route"}
}

type grants struct {
	g credentialpackage.CredentialGrant
}

func (g grants) ForTarget(string) (credentialpackage.CredentialGrant, bool) { return g.g, true }

// gateHarness runs one device through the executor with a marker script as
// the system transport: the marker file exists exactly when a transport
// open was attempted, which is how "no transport for a skipped target" is
// proven at this level.
type gateHarness struct {
	t      *testing.T
	exec   *DeviceExecutor
	store  *output.Store
	marker string
	target executionplan.ExecutionTarget
}

// newGateHarness is an executor over a fake ssh, its configuration taking
// sets after the harness's own (the ICMP gate's keys among them).
func newGateHarness(t *testing.T, sets []string, pinger icmpgate.Pinger) *gateHarness {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "transport-opened")
	script := filepath.Join(dir, "fake-ssh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncase \" $* \" in *' -O '*) exit 1;; esac\n: >\""+marker+"\"\nprintf 'dev#'\nwhile IFS= read -r line; do\n  [ \"$line\" = exit ] && exit 0\n  printf '%s\\r\\nok\\r\\ndev#' \"$line\"\ndone\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	os.MkdirAll(home, 0o700)
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append([]string{
		fmt.Sprintf("ssh.transports.system=%q", script), `ssh.host-key-policy="insecure"`, fmt.Sprintf("ssh.known-hosts-file=%q", filepath.Join(dir, "known_hosts")),
	}, sets...)})
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
	grant := credentialpackage.CredentialGrant{CredentialID: plantest.GrantA, Method: credentialpackage.MethodEmbeddedSecret, Username: credentials.NewSecretString("u"), Password: credentials.NewSecretString("p"), Policy: "default", Backend: "env", MatchedOn: credentials.Match{Category: "operator", SafeValue: "netops"}}
	var debugLines []string
	e := New(Options{
		Config: cfg, Operator: credentials.Operator{Username: "netops", UID: 1000, Home: home}, ActivityID: plantest.JobID, JobID: plantest.JobID, ActivityType: "run",
		Commands: plantest.Commands, DispatchOrder: "default", Grants: grants{grant}, Protection: "local-peer",
		Pinger: pinger, Capacity: capMgr, Store: store, ScratchDir: testsocket.Dir(t), ControlRoot: filepath.Join(dir, "control"), Home: home, AskpassPath: "/bin/true",
		Debug: func(s string) { debugLines = append(debugLines, s) },
	})
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("debug:\n%s", strings.Join(debugLines, "\n"))
		}
	})
	return &gateHarness{t: t, exec: e, store: store, marker: marker, target: target}
}

func (h *gateHarness) run(ctx context.Context) (dispatch.Result, []records.CommandRecord) {
	h.t.Helper()
	res := h.exec.Execute(ctx, dispatch.Task{Key: h.target.TargetID, Position: 1, Value: Work{Target: h.target, QueuedAt: time.Now()}}, dispatch.Context{Mode: "serial", ScopePosition: 1, DesiredWidth: 1, EffectiveInflight: 1})
	if err := h.store.Close(); err != nil {
		h.t.Fatal(err)
	}
	data, err := os.ReadFile(h.store.Paths().CommandsJSONL)
	if err != nil {
		h.t.Fatal(err)
	}
	var out []records.CommandRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var r records.CommandRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			h.t.Fatal(err)
		}
		if err := r.Validate(); err != nil {
			h.t.Errorf("record %d does not validate: %v", r.Sequence, err)
		}
		out = append(out, r)
	}
	return res, out
}

func (h *gateHarness) transportAttempted() bool {
	_, err := os.Stat(h.marker)
	return err == nil
}

var enabled = []string{"network.ping-targets=true", `network.ping-timeout="500ms"`}

// TestGateSkipsAnUnreachableTargetWithoutATransport: two misses fail the
// device as icmp_unreachable on command
// 1, leave the rest not attempted, put the same ping object on every
// record, and never run the transport binary.
func TestGateSkipsAnUnreachableTargetWithoutATransport(t *testing.T) {
	for _, tc := range []struct {
		name     string
		outcomes []icmpgate.Outcome
		detail   string
	}{
		{"two timeouts", []icmpgate.Outcome{timeoutOutcome(1), timeoutOutcome(2)}, "1: timeout; 2: timeout"},
		{"timeout and error", []icmpgate.Outcome{timeoutOutcome(1), errorOutcome(2)}, "2: error Destination unreachable: No route from 2001:db8::ff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &scriptedPinger{outcomes: map[string][]icmpgate.Outcome{"127.0.0.1": tc.outcomes}}
			h := newGateHarness(t, enabled, p)
			res, recs := h.run(context.Background())
			if res.Success || res.ErrorCode != "icmp_unreachable" || !res.External {
				t.Errorf("result %+v", res)
			}
			if p.calls != 1 || h.transportAttempted() {
				t.Errorf("calls=%d transport attempted=%v", p.calls, h.transportAttempted())
			}
			if len(recs) != len(plantest.Commands) {
				t.Fatalf("%d records", len(recs))
			}
			first := recs[0]
			if first.Status != records.StatusICMPUnreachable || first.Error == nil || first.Error.Code != "icmp_unreachable" || first.Error.Operation != "icmp_gate" || !first.Error.Retryable || !first.Error.External {
				t.Errorf("first record status=%s error=%+v", first.Status, first.Error)
			}
			if !strings.Contains(first.Error.Message, tc.detail) || !strings.Contains(first.Error.Message, "skipped because ping gating is enabled") {
				t.Errorf("message %q", first.Error.Message)
			}
			for i, r := range recs {
				if r.Ping == nil || r.Ping.Decision != records.PingDecisionSkip || r.Ping.Replies != 0 || r.Ping.Losses != 2 || r.Ping.Method != icmpgate.MethodSocket || r.Ping.ExecutionEndpoint != executionplan.EndpointLocal || r.Timing.PingNS == nil {
					t.Errorf("record %d ping=%+v ping_ns=%v", i+1, r.Ping, r.Timing.PingNS)
				}
				if i > 0 && r.Status != "not_attempted_prior_command_failure" {
					t.Errorf("record %d status %s", i+1, r.Status)
				}
				if len(r.Notices) != 0 {
					t.Errorf("record %d carries notices %+v", i+1, r.Notices)
				}
			}
			s := h.exec.PingSummary()
			if s == nil || !s.Enabled || s.Devices[records.PingDevicesGated] != 1 || s.Devices[records.PingDevicesSkipped] != 1 || s.ProbesSent != 2 || s.Replies != 0 || s.Timeouts+s.Errors != 2 {
				t.Errorf("summary %+v", s)
			}
		})
	}
}

// TestGateProceedsOnOneOrTwoReplies: the transport is
// attempted; one miss puts the packet-loss notice on the first record only.
func TestGateProceedsOnOneOrTwoReplies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		outcomes []icmpgate.Outcome
		decision string
		notices  int
	}{
		{"two replies", []icmpgate.Outcome{reply(1, 2100*time.Microsecond), reply(2, 40*time.Microsecond)}, records.PingDecisionProceed, 0},
		{"one reply", []icmpgate.Outcome{reply(1, 2100*time.Microsecond), timeoutOutcome(2)}, records.PingDecisionProceedDegraded, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &scriptedPinger{outcomes: map[string][]icmpgate.Outcome{"127.0.0.1": tc.outcomes}}
			h := newGateHarness(t, enabled, p)
			res, recs := h.run(context.Background())
			if p.calls != 1 || !h.transportAttempted() {
				t.Fatalf("calls=%d transport attempted=%v", p.calls, h.transportAttempted())
			}
			if res.ErrorCode == "icmp_unreachable" || res.ErrorCode == "icmp_capability_unavailable" {
				t.Errorf("gate code on a proceeding device: %+v", res)
			}
			if len(recs) == 0 {
				t.Fatal("no records")
			}
			for i, r := range recs {
				if r.Ping == nil || r.Ping.Decision != tc.decision || r.Timing.PingNS == nil {
					t.Errorf("record %d ping=%+v", i+1, r.Ping)
				}
				want := 0
				if i == 0 {
					want = tc.notices
				}
				if len(r.Notices) != want {
					t.Errorf("record %d notices %+v, want %d", i+1, r.Notices, want)
				}
				if i == 0 && want == 1 && (r.Notices[0].Code != "icmp_packet_loss" || !strings.Contains(r.Notices[0].Message, "packet loss 50%")) {
					t.Errorf("notice %+v", r.Notices[0])
				}
			}
			s := h.exec.PingSummary()
			degraded := 0
			if tc.notices == 1 {
				degraded = 1
			}
			if s.Devices[records.PingDevicesGated] != 1 || s.Devices[records.PingDevicesDegraded] != degraded || s.Devices[records.PingDevicesProceeded] != 1-degraded || s.Replies != 2-degraded {
				t.Errorf("summary %+v", s)
			}
		})
	}
}

// TestGateCapabilityFailureMidJobFailsTheDeviceAlone.
func TestGateCapabilityFailureMidJobFailsTheDeviceAlone(t *testing.T) {
	p := &scriptedPinger{err: fmt.Errorf("ping socket: permission denied")}
	h := newGateHarness(t, enabled, p)
	res, recs := h.run(context.Background())
	if res.Success || res.ErrorCode != "icmp_capability_unavailable" || res.External || h.transportAttempted() {
		t.Errorf("result %+v transport attempted=%v", res, h.transportAttempted())
	}
	first := recs[0]
	if first.Status != records.StatusICMPCapabilityUnavailable || first.Error == nil || first.Error.Code != "icmp_capability_unavailable" || first.Error.Category != "dependency" || !strings.Contains(first.Error.Message, "permission denied") {
		t.Errorf("first record status=%s error=%+v", first.Status, first.Error)
	}
	if first.Ping == nil || first.Ping.Decision != records.PingDecisionCapabilityUnavailable || len(first.Ping.Outcomes) != 2 || first.Ping.Outcomes[0].Status != icmpgate.StatusError {
		t.Errorf("ping %+v", first.Ping)
	}
	if s := h.exec.PingSummary(); s.Devices[records.PingDevicesCapabilityFailed] != 1 || s.ProbesSent != 0 {
		t.Errorf("summary %+v", s)
	}
}

// TestDisabledGateNeverTouchesThePinger: a counting pinger is never
// called, no ping object or duration is written, and the transport is
// attempted as without a gate.
func TestDisabledGateNeverTouchesThePinger(t *testing.T) {
	p := &scriptedPinger{}
	h := newGateHarness(t, []string{"network.ping-targets=false", `network.ping-timeout="500ms"`}, p)
	_, recs := h.run(context.Background())
	if p.calls != 0 || !h.transportAttempted() {
		t.Errorf("calls=%d transport attempted=%v", p.calls, h.transportAttempted())
	}
	for i, r := range recs {
		if r.Ping != nil || r.Timing.PingNS != nil {
			t.Errorf("record %d carries ping=%+v ping_ns=%v", i+1, r.Ping, r.Timing.PingNS)
		}
	}
	if h.exec.PingSummary() != nil {
		t.Error("summary block on a disabled gate")
	}
	h2 := newGateHarness(t, []string{"network.ping-targets=false", `network.ping-timeout="1ms"`}, nil)
	h2.run(context.Background()) // a nil pinger is never dereferenced when disabled
}

// TestGateCancelledDuringProbesTakesTheCancelledPath.
func TestGateCancelledDuringProbesTakesTheCancelledPath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &scriptedPinger{outcomes: map[string][]icmpgate.Outcome{"127.0.0.1": {timeoutOutcome(1), timeoutOutcome(2)}}, onProbe: cancel}
	h := newGateHarness(t, enabled, p)
	res, recs := h.run(ctx)
	if res.ErrorCode != "cancelled" || h.transportAttempted() {
		t.Errorf("result %+v transport attempted=%v", res, h.transportAttempted())
	}
	for i, r := range recs {
		if r.Status != "cancelled" {
			t.Errorf("record %d status %s", i+1, r.Status)
		}
	}
}

func TestDescribeOutcomesAndRTT(t *testing.T) {
	ns := func(d time.Duration) *int64 { v := d.Nanoseconds(); return &v }
	got := describeOutcomes([]records.PingOutcome{{Sequence: 1, Status: "reply", RTTNS: ns(2100 * time.Microsecond)}, {Sequence: 2, Status: "reply", RTTNS: ns(40 * time.Microsecond)}, {Sequence: 3, Status: "timeout"}, {Sequence: 4, Status: "error", Detail: "No route"}})
	if got != "1: 2.1ms; 2: <0.1ms; 3: timeout; 4: error No route" {
		t.Errorf("%q", got)
	}
}
