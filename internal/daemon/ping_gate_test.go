package daemon

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/icmpgate"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/records"
)

// scriptedPinger answers each address from a script (the injected Pinger
// through the daemon's detection hook).
type scriptedPinger struct {
	outcomes map[string][]icmpgate.Outcome
	calls    int
}

func (p *scriptedPinger) Method() string { return icmpgate.MethodSocket }
func (p *scriptedPinger) Probe(_ context.Context, address netip.Addr, _ int, _ time.Duration) ([]icmpgate.Outcome, error) {
	p.calls++
	return p.outcomes[address.String()], nil
}

func pingReply(seq int) icmpgate.Outcome {
	ns := (50 * time.Microsecond).Nanoseconds()
	return icmpgate.Outcome{Sequence: seq, SentAt: time.Now(), Status: icmpgate.StatusReply, RTTNS: &ns, From: "127.0.0.1"}
}
func pingTimeout(seq int) icmpgate.Outcome {
	return icmpgate.Outcome{Sequence: seq, SentAt: time.Now(), Status: icmpgate.StatusTimeout}
}

// hookDetect replaces icmpgate.Detect for the test and records the options
// the daemon passed.
func hookDetect(t *testing.T, c icmpgate.Capability) *icmpgate.Options {
	t.Helper()
	saved := icmpgate.Detect
	var got icmpgate.Options
	calls := 0
	icmpgate.Detect = func(o icmpgate.Options) icmpgate.Capability { got = o; calls++; return c }
	t.Cleanup(func() { icmpgate.Detect = saved })
	return &got
}

func readRecords(t *testing.T, dir string) []records.CommandRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "commands.jsonl"))
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

func readSummary(t *testing.T, dir string) records.Summary {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s records.Summary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestLiveJobGatesEachTargetAndSkipsOnlyTheUnreachableOne is the ICMP
// gate at the daemon: a live job over one target that answers
// both probes and one that answers neither opens exactly one session at
// the fake device, skips the other as icmp_unreachable, carries the ping
// object on every record and the block in the summary, and ends 101.
func TestLiveJobGatesEachTargetAndSkipsOnlyTheUnreachableOne(t *testing.T) {
	fakeDir := t.TempDir()
	t.Setenv("KARVI_TEST_FAKE_DIR", fakeDir)
	// The fake device's overlap barrier waits for the other marker; a
	// single "show b" session proceeds at once when "ready-a" exists.
	if err := os.WriteFile(filepath.Join(fakeDir, "ready-a"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pinger := &scriptedPinger{outcomes: map[string][]icmpgate.Outcome{
		"127.0.0.1": {pingReply(1), pingReply(2)},
		"127.0.0.2": {pingTimeout(1), pingTimeout(2)},
	}}
	opts := hookDetect(t, icmpgate.WithPinger(pinger))
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true, Sets: []string{`security.child-environment-allowlist=["KARVI_TEST_FAKE_DIR"]`, "network.ping-targets=true", `network.ping-timeout="250ms"`, "network.ping-system=false"}})
	devices := []inventory.Device{direct("127.0.0.1"), direct("127.0.0.2")}
	sub := f.prepareAndPackageWith(mustID(t), []string{"show b"}, devices, fixedInput{"alice", "pw"})
	if c := sub.final.Configuration; c["network.ping-targets"] != true || c["network.ping-timeout"] != "250ms" {
		t.Fatalf("the plan's configuration: network.ping-targets=%v network.ping-timeout=%v", c["network.ping-targets"], c["network.ping-timeout"])
	}
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	result, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if err != nil {
		t.Fatal(err)
	}
	terminal := f.followToEnd(result.Receipt.JobID)
	if terminal.Outcome.ExitCode != 101 {
		t.Fatalf("outcome %+v", terminal.Outcome)
	}
	if pinger.calls != 2 || !opts.Socket || opts.System {
		t.Errorf("pinger calls=%d detect options=%+v", pinger.calls, *opts)
	}
	log, _ := os.ReadFile(filepath.Join(fakeDir, "log"))
	if lines := strings.Split(strings.TrimSpace(string(log)), "\n"); len(lines) != 1 || !strings.HasPrefix(lines[0], "show b ") {
		t.Errorf("fake device log %q: want exactly one session", log)
	}
	recs := readRecords(t, result.Receipt.ArtifactDir)
	if len(recs) != 2 {
		t.Fatalf("%d records", len(recs))
	}
	for _, r := range recs {
		if r.Ping == nil || r.Timing.PingNS == nil {
			t.Fatalf("record for %s has no ping object", r.SelectedAddress)
		}
		switch r.SelectedAddress {
		case "127.0.0.1":
			if r.Status != "succeeded" || r.Ping.Decision != records.PingDecisionProceed || r.Ping.Replies != 2 || len(r.Notices) != 0 {
				t.Errorf("reachable target: status=%s ping=%+v notices=%+v", r.Status, r.Ping, r.Notices)
			}
		case "127.0.0.2":
			if r.Status != records.StatusICMPUnreachable || r.Error == nil || r.Error.Code != "icmp_unreachable" || r.Ping.Decision != records.PingDecisionSkip || r.Ping.Replies != 0 {
				t.Errorf("unreachable target: status=%s error=%+v ping=%+v", r.Status, r.Error, r.Ping)
			}
			if r.Credential == nil {
				t.Error("the skipped record lost its credential projection")
			}
		default:
			t.Errorf("unexpected address %s", r.SelectedAddress)
		}
		if r.Ping.Method != icmpgate.MethodSocket || r.Ping.TimeoutNS != int64(250*time.Millisecond) || r.Ping.ExecutionEndpoint != executionplan.EndpointLocal {
			t.Errorf("ping object %+v", r.Ping)
		}
	}
	failed, _ := os.ReadFile(filepath.Join(result.Receipt.ArtifactDir, "failed-devices.txt"))
	if !strings.Contains(string(failed), "127.0.0.2") || strings.Contains(string(failed), "127.0.0.1") {
		t.Errorf("failed-devices.txt %q", failed)
	}
	assertAccounting(t, result.Receipt.ArtifactDir, 2, 1)
	s := readSummary(t, result.Receipt.ArtifactDir)
	if s.Ping == nil || !s.Ping.Enabled || s.Ping.Method != icmpgate.MethodSocket || s.Ping.Devices[records.PingDevicesGated] != 2 || s.Ping.Devices[records.PingDevicesProceeded] != 1 || s.Ping.Devices[records.PingDevicesSkipped] != 1 || s.Ping.ProbesSent != 4 || s.Ping.Replies != 2 || s.Ping.Timeouts != 2 {
		t.Errorf("summary ping block %+v", s.Ping)
	}
	if s.DeviceCounts["failed"] != 1 || s.DeviceCounts["succeeded"] != 1 {
		t.Errorf("device counts %+v", s.DeviceCounts)
	}
	// The follow stream carried both records, ping objects included.
	followedRecords := f.follow(result.Receipt.JobID, 0, 1<<20)
	if followedRecords.err != nil || len(followedRecords.records) != 2 {
		t.Errorf("follow: err=%v records=%d", followedRecords.err, len(followedRecords.records))
	}
	verifyRecords(t, filepath.Join(result.Receipt.ArtifactDir, "commands.jsonl"), 1, followedRecords.records)
}

// TestLiveJobWithoutAnICMPMethodIsRefusedAtCommit: detection fails once,
// the commit is refused with the code,
// and no job directory exists.
func TestLiveJobWithoutAnICMPMethodIsRefusedAtCommit(t *testing.T) {
	hookDetect(t, icmpgate.Capability{Method: icmpgate.MethodUnavailable, Reason: "ICMP sockets refused (permission denied; net.ipv4.ping_group_range=1 0) and no ping executable on PATH"})
	f := newV5FixtureWith(t, v5Options{Sets: []string{"network.ping-targets=true"}})
	sub := f.prepareAndPackageWith(mustID(t), []string{"show clock"}, []inventory.Device{direct("127.0.0.1")}, fixedInput{"alice", "pw"})
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	_, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if errorcodes.Of(err) != "icmp_capability_unavailable" || !strings.Contains(err.Error(), "ping_group_range") {
		t.Fatalf("commit: %v", err)
	}
	jobs, _ := filepath.Glob(filepath.Join(f.cfg.String("basedir"), "jobs", "*", "*"))
	if len(jobs) != 0 {
		t.Errorf("job directories after a refused commit: %v", jobs)
	}
}

// TestDisabledGateDetectsNothing: with the key at its default
// the daemon never calls detection or a pinger.
func TestDisabledGateDetectsNothing(t *testing.T) {
	calls := 0
	saved := icmpgate.Detect
	icmpgate.Detect = func(icmpgate.Options) icmpgate.Capability { calls++; return icmpgate.Capability{} }
	t.Cleanup(func() { icmpgate.Detect = saved })
	f := newV5Fixture(t)
	sub := f.prepareAndPackage(mustID(t), []string{"show clock"})
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	result, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if err != nil {
		t.Fatal(err)
	}
	terminal := f.followToEnd(result.Receipt.JobID)
	if terminal.Outcome.ExitCode != 0 || calls != 0 {
		t.Fatalf("outcome %+v detect calls=%d", terminal.Outcome, calls)
	}
	for _, r := range readRecords(t, result.Receipt.ArtifactDir) {
		if r.Ping != nil || r.Timing.PingNS != nil {
			t.Errorf("record %d carries a ping object with the gate disabled", r.Sequence)
		}
	}
	if s := readSummary(t, result.Receipt.ArtifactDir); s.Ping != nil {
		t.Errorf("summary ping block %+v with the gate disabled", s.Ping)
	}
}

// TestExerciseReportsTheICMPCapabilityWithoutProbing: with the gate
// enabled an exercise detects the method
// once and never probes; available is an info finding with the method on
// every target, unavailable an error that makes every target not_ready,
// the outcome not_ready, the exit 8, and the cause exercise_not_ready; the
// exercise summary carries the block with the capability.
func TestExerciseReportsTheICMPCapabilityWithoutProbing(t *testing.T) {
	pinger := &scriptedPinger{}
	hookDetect(t, icmpgate.WithPinger(pinger))
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true, Sets: []string{"network.ping-targets=true", `network.ping-timeout="250ms"`}})
	_, result, terminal, report := f.exercise(mustID(t), []string{"show clock"}, []inventory.Device{direct("127.0.0.1"), direct("127.0.0.2")}, canarytest.Seed(t))
	if terminal.Outcome.ExitCode != 0 || report.Outcome != records.OutcomeExercised || pinger.calls != 0 {
		t.Fatalf("outcome=%+v report outcome=%s probe calls=%d", terminal.Outcome, report.Outcome, pinger.calls)
	}
	for _, tr := range report.Targets {
		if tr.Readiness != records.ReadinessReady || tr.IntendedPing.Capability != records.CheckAvailable || tr.IntendedPing.Method != icmpgate.MethodSocket || !tr.IntendedPing.Enabled || tr.IntendedPing.TimeoutNS != int64(250*time.Millisecond) {
			t.Errorf("%s: readiness=%s intended_ping=%+v", tr.TargetID, tr.Readiness, tr.IntendedPing)
		}
		found := false
		for _, fd := range tr.Findings {
			if fd.Code == "icmp_assessment" && fd.Severity == executionplan.SeverityInfo && fd.Stage == "ping" && fd.Details["method"] == icmpgate.MethodSocket {
				found = true
			}
		}
		if !found {
			t.Errorf("%s lacks the icmp_assessment finding: %+v", tr.TargetID, tr.Findings)
		}
	}
	s := readSummary(t, result.Receipt.ArtifactDir)
	if s.Ping == nil || !s.Ping.Enabled || s.Ping.Method != icmpgate.MethodSocket || s.Ping.Capability != records.CheckAvailable || s.Ping.Devices[records.PingDevicesGated] != 0 || s.Ping.ProbesSent != 0 || s.FinalStatus != "exercised" {
		t.Errorf("exercise summary ping %+v final=%s", s.Ping, s.FinalStatus)
	}

	// Unavailable: every target not ready, exit 8, the cause named.
	hookDetect(t, icmpgate.Capability{Method: icmpgate.MethodUnavailable, Reason: "ICMP sockets refused (permission denied; net.ipv4.ping_group_range=1 0) and no ping executable on PATH"})
	_, result, terminal, report = f.exercise(mustID(t), []string{"show clock"}, []inventory.Device{direct("127.0.0.1")}, canarytest.Seed(t))
	if terminal.Outcome.ExitCode != 8 || report.Outcome != records.OutcomeNotReady || !strings.Contains(terminal.Outcome.Error, "exercise_not_ready") || !strings.Contains(terminal.Outcome.Error, "icmp_capability_unavailable") {
		t.Fatalf("unavailable: outcome=%+v report outcome=%s", terminal.Outcome, report.Outcome)
	}
	tr := report.Targets[0]
	if tr.Readiness != records.ReadinessNotReady || tr.IntendedPing.Capability != records.CheckUnavailable || tr.IntendedPing.Method != "" {
		t.Errorf("readiness=%s intended_ping=%+v", tr.Readiness, tr.IntendedPing)
	}
	if s := readSummary(t, result.Receipt.ArtifactDir); s.Ping == nil || s.Ping.Capability != records.CheckUnavailable || s.Ping.Method != icmpgate.MethodUnavailable || len(s.TerminalCauses) != 1 || s.TerminalCauses[0] != "exercise_not_ready" {
		t.Errorf("summary %+v causes=%v", s.Ping, s.TerminalCauses)
	}

	// Disabled: capability stays not_checked and detection is never called.
	calls := 0
	saved := icmpgate.Detect
	icmpgate.Detect = func(icmpgate.Options) icmpgate.Capability { calls++; return icmpgate.Capability{} }
	t.Cleanup(func() { icmpgate.Detect = saved })
	f2 := newV5FixtureWith(t, v5Options{GoFakeDevice: true})
	_, result, terminal, report = f2.exercise(mustID(t), []string{"show clock"}, []inventory.Device{direct("127.0.0.1")}, canarytest.Seed(t))
	if terminal.Outcome.ExitCode != 0 || calls != 0 || report.Targets[0].IntendedPing.Capability != records.CheckNotChecked || report.Targets[0].IntendedPing.Method != "" {
		t.Errorf("disabled: exit=%d detect calls=%d intended_ping=%+v", terminal.Outcome.ExitCode, calls, report.Targets[0].IntendedPing)
	}
	if s := readSummary(t, result.Receipt.ArtifactDir); s.Ping != nil {
		t.Errorf("disabled exercise summary carries %+v", s.Ping)
	}
}
