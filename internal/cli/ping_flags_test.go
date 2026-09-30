package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/icmpgate"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestPingFlagConflictIsRefusedBeforeConfiguration: both flags exit 4 in
// every target-bearing mode, and with a
// nonexistent explicit configuration root, which proves the check runs
// before configuration loads.
func TestPingFlagConflictIsRefusedBeforeConfiguration(t *testing.T) {
	for _, args := range [][]string{
		{"--config", "/nonexistent/karvi.toml", "login", "--ping", "--noping", "router1"},
		{"--config", "/nonexistent/karvi.toml", "command", "--ping", "--noping", "router1", "show clock"},
		{"--config", "/nonexistent/karvi.toml", "run", "--noping", "--target", "router1", "--ping", "show clock"},
		{"--config", "/nonexistent/karvi.toml", "run", "--dry-run", "--ping", "--noping", "--target", "router1", "show clock"},
	} {
		t.Run(strings.Join(args[2:], "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitUsageError {
				t.Fatalf("exit=%d, want %d; stderr=%q", got, exitcode.ExitUsageError, stderr.String())
			}
			if !strings.HasPrefix(stderr.String(), "ping_flag_conflict: ") || !strings.Contains(stderr.String(), "mutually exclusive") {
				t.Fatalf("stderr=%q", stderr.String())
			}
		})
	}
	// Alone, either flag passes the parser and fails later on the missing root.
	var stdout, stderr bytes.Buffer
	if got := Main([]string{"--config", "/nonexistent/karvi.toml", "login", "--ping", "router1"}, strings.NewReader(""), &stdout, &stderr); got == exitcode.ExitUsageError || strings.Contains(stderr.String(), "cli_option_unavailable") {
		t.Fatalf("--ping alone: exit=%d stderr=%q", got, stderr.String())
	}
}

// TestPingFlagsReachThePlanThroughTheCLILayer: the dry-run report's
// intended_ping shows the effective
// setting; a flag outranks a configuration file, --set outranks the flag as
// it outranks every other flag, and the timeout key travels with it. No
// probe is sent: the report is the client's, and the fake transport is
// never invoked.
func TestPingFlagsReachThePlanThroughTheCLILayer(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	fileOn := filepath.Join(base, "ping-on.toml")
	if err := os.WriteFile(fileOn, []byte("[network]\nping-targets = true\nping-timeout = \"250ms\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name         string
		global, mode []string
		enabled      bool
		timeout      time.Duration
	}{
		{"default", nil, nil, false, 500 * time.Millisecond},
		{"--ping", nil, []string{"--ping"}, true, 500 * time.Millisecond},
		{"--noping", nil, []string{"--noping"}, false, 500 * time.Millisecond},
		{"file on, no flag", []string{"--config", fileOn}, nil, true, 250 * time.Millisecond},
		{"file on, --noping outranks the file", []string{"--config", fileOn}, []string{"--noping"}, false, 250 * time.Millisecond},
		{"--set true outranks --noping", []string{"--set", "network.ping-targets=true"}, []string{"--noping"}, true, 500 * time.Millisecond},
		{"--set false outranks --ping", []string{"--set", "network.ping-targets=false"}, []string{"--ping"}, false, 500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := dryRunArgs(append(append([]string{}, sets...), tc.global...), append([]string{"--no-daemon", "--format", "jsonl"}, tc.mode...)...)
			if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
				t.Fatalf("exit=%d stderr=%q", got, stderr.String())
			}
			var report records.PlanReport
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Plan.Ping.Enabled != tc.enabled || report.Plan.Ping.TimeoutNS != tc.timeout.Nanoseconds() || report.Plan.Ping.Probes != 2 {
				t.Errorf("plan ping %+v", report.Plan.Ping)
			}
			for _, target := range report.Targets {
				if target.IntendedPing.Enabled != tc.enabled || target.IntendedPing.TimeoutNS != tc.timeout.Nanoseconds() {
					t.Errorf("%s intended_ping %+v", target.TargetID, target.IntendedPing)
				}
			}
		})
	}
	if _, err := os.Stat(filepath.Join(base, "fake-ssh.log")); !os.IsNotExist(err) {
		t.Error("the fake transport was invoked by a dry-run")
	}
}

type loginPinger struct {
	outcomes []icmpgate.Outcome
	calls    int
}

func (p *loginPinger) Method() string { return icmpgate.MethodSocket }
func (p *loginPinger) Probe(context.Context, netip.Addr, int, time.Duration) ([]icmpgate.Outcome, error) {
	p.calls++
	return p.outcomes, nil
}

func hookDetect(t *testing.T, c icmpgate.Capability) *int {
	t.Helper()
	saved := icmpgate.Detect
	calls := 0
	icmpgate.Detect = func(icmpgate.Options) icmpgate.Capability { calls++; return c }
	t.Cleanup(func() { icmpgate.Detect = saved })
	return &calls
}

// TestLoginGateSkipsAnUnreachableTarget: login with --ping
// against a target that answers neither probe exits 110 under
// icmp_unreachable and never runs the transport binary.
func TestLoginGateSkipsAnUnreachableTarget(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	p := &loginPinger{outcomes: []icmpgate.Outcome{{Sequence: 1, SentAt: time.Now(), Status: icmpgate.StatusTimeout}, {Sequence: 2, SentAt: time.Now(), Status: icmpgate.StatusTimeout}}}
	detects := hookDetect(t, icmpgate.WithPinger(p))
	var stdout, stderr bytes.Buffer
	args := append(append([]string{}, sets...), "--set", `ssh.host-key-policy="insecure"`, "login", "--ping", "--management-address", "192.0.2.1", "router1")
	got := Main(args, strings.NewReader(""), &stdout, &stderr)
	if got != exitcode.ExitConnectionFailure || !strings.Contains(stderr.String(), "\nicmp_unreachable: ") {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	if !strings.HasPrefix(stderr.String(), "! router1 [192.0.2.1] ping(1) timeout, ping(2) timeout, skipped\n") {
		t.Errorf("the ping line does not precede the failure: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "192.0.2.1") || !strings.Contains(stderr.String(), "1: timeout; 2: timeout") || !strings.Contains(stderr.String(), "skipped because ping gating is enabled") {
		t.Errorf("stderr=%q", stderr.String())
	}
	if *detects != 1 || p.calls != 1 {
		t.Errorf("detect calls=%d probe calls=%d", *detects, p.calls)
	}
	if _, err := os.Stat(filepath.Join(base, "fake-ssh.log")); !os.IsNotExist(err) {
		t.Error("the transport binary ran for a skipped target")
	}
}

// TestLoginWithoutAnICMPMethodIsRefusedBeforeCredentials:
// with no credentials in the environment the refusal is still the
// capability's exit 8, not the credential resolver's 6, so the check
// precedes credential resolution.
func TestLoginWithoutAnICMPMethodIsRefusedBeforeCredentials(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "")
	t.Setenv("NETPASS", "")
	hookDetect(t, icmpgate.Capability{Method: icmpgate.MethodUnavailable, Reason: "ICMP sockets refused (permission denied; net.ipv4.ping_group_range=1 0) and the system ping method is disabled (network.ping-system=false)"})
	var stdout, stderr bytes.Buffer
	args := append(append([]string{}, sets...), "login", "--ping", "--management-address", "127.0.0.1", "router1")
	got := Main(args, strings.NewReader(""), &stdout, &stderr)
	if got != exitcode.ExitDependencyError || !strings.HasPrefix(stderr.String(), "icmp_capability_unavailable: ") || !strings.Contains(stderr.String(), "network.ping-system=false") {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	// Disabled, the same invocation never detects and fails later on credentials.
	stdout.Reset()
	stderr.Reset()
	calls := hookDetect(t, icmpgate.Capability{})
	args = append(append([]string{}, sets...), "login", "--noping", "--management-address", "127.0.0.1", "router1")
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitCredentialResolutionError || *calls != 0 {
		t.Fatalf("--noping: exit=%d detect calls=%d stderr=%q", got, *calls, stderr.String())
	}
}

func TestHelpListsThePingFlags(t *testing.T) {
	for _, mode := range []string{"login", "command", "run"} {
		var stdout, stderr bytes.Buffer
		if got := Main([]string{mode, "--help"}, strings.NewReader(""), &stdout, &stderr); got != 0 {
			t.Fatalf("%s --help exit=%d", mode, got)
		}
		out := stdout.String()
		if !strings.Contains(out, "  --ping ") || !strings.Contains(out, "  --noping ") || strings.Contains(out, "available:  --ping") || strings.Contains(out, ", --ping, --noping") {
			t.Errorf("%s help:\n%s", mode, out)
		}
	}
}

// TestLoginPrintsThePingLine: login has no records, so the
// one line and, under --debug, the notice go to stderr; --quiet and
// an empty display.ping.header suppress the line.
func TestLoginPrintsThePingLine(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	ns := (2100 * time.Microsecond).Nanoseconds()
	p := &loginPinger{outcomes: []icmpgate.Outcome{{Sequence: 1, SentAt: time.Now(), Status: icmpgate.StatusReply, RTTNS: &ns, From: "127.0.0.1"}, {Sequence: 2, SentAt: time.Now(), Status: icmpgate.StatusTimeout}}}
	hookDetect(t, icmpgate.WithPinger(p))
	run := func(extra ...string) string {
		var stdout, stderr bytes.Buffer
		args := append(append([]string{}, sets...), extra...)
		args = append(args, "login", "--ping", "--management-address", "127.0.0.1", "router1")
		Main(args, strings.NewReader(""), &stdout, &stderr) // the fake transport then fails the open
		return stderr.String()
	}
	if got := run(); !strings.Contains(got, "! router1 [127.0.0.1] ping(1) 2.1ms, ping(2) timeout, proceeding\n") || strings.Contains(got, "packet loss") {
		t.Errorf("stderr without --debug:\n%s", got)
	}
	if got := run("--debug"); !strings.Contains(got, "karvi: ICMP packet loss 50%; proceeding because one validated reply was received\n") {
		t.Errorf("stderr with --debug:\n%s", got)
	}
	if got := run("--set", `display.ping.header=""`); strings.Contains(got, "ping(1)") {
		t.Errorf("an empty display.ping.header still printed:\n%s", got)
	}
	if got := run("--quiet"); strings.Contains(got, "ping(1)") {
		t.Errorf("--quiet still printed:\n%s", got)
	}
}

// TestDryRunReportsTheICMPCapabilityWithoutProbing: with --ping the
// dry-run detects on the client
// and never probes; available fills capability and method; unavailable is
// a warning on every target with readiness, outcome, and exit unchanged;
// the text rendering shows the intended setting.
func TestDryRunReportsTheICMPCapabilityWithoutProbing(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	p := &loginPinger{}
	detects := hookDetect(t, icmpgate.WithPinger(p))
	var stdout, stderr bytes.Buffer
	if got := Main(dryRunArgs(sets, "--no-daemon", "--format", "jsonl", "--ping"), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	var report records.PlanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if *detects != 1 || p.calls != 0 || report.Outcome != records.OutcomePlanned || report.Counts.Warnings != 0 {
		t.Errorf("detect=%d probes=%d outcome=%s warnings=%d", *detects, p.calls, report.Outcome, report.Counts.Warnings)
	}
	for _, tr := range report.Targets {
		if !tr.IntendedPing.Enabled || tr.IntendedPing.Capability != records.CheckAvailable || tr.IntendedPing.Method != icmpgate.MethodSocket || len(tr.Findings) != 0 {
			t.Errorf("%s intended_ping=%+v findings=%+v", tr.TargetID, tr.IntendedPing, tr.Findings)
		}
	}
	stdout.Reset()
	if got := Main(dryRunArgs(sets, "--no-daemon", "--ping"), strings.NewReader(""), &stdout, &stderr); got != 0 || !strings.Contains(stdout.String(), "intended: ping=enabled (2 probes, 500ms, capability=available via socket) transport=system port=22") {
		t.Errorf("text rendering exit=%d:\n%s", got, stdout.String())
	}

	hookDetect(t, icmpgate.Capability{Method: icmpgate.MethodUnavailable, Reason: "ICMP sockets refused (permission denied; net.ipv4.ping_group_range=1 0) and no ping executable on PATH"})
	stdout.Reset()
	stderr.Reset()
	if got := Main(dryRunArgs(sets, "--no-daemon", "--format", "jsonl", "--ping"), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("unavailable: exit=%d stderr=%q", got, stderr.String())
	}
	report = records.PlanReport{} // omitempty fields would otherwise survive the decode
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Outcome != records.OutcomePlanned || report.Counts.Warnings != len(report.Targets) || report.Counts.Errors != 0 {
		t.Errorf("unavailable: outcome=%s counts=%+v", report.Outcome, report.Counts)
	}
	for _, tr := range report.Targets {
		if tr.IntendedPing.Capability != records.CheckUnavailable || tr.IntendedPing.Method != "" || len(tr.Findings) != 1 || tr.Findings[0].Code != "icmp_capability_unavailable" || tr.Findings[0].Severity != "warning" || tr.Findings[0].Stage != "ping" || tr.Findings[0].TargetID != tr.TargetID || !strings.Contains(tr.Findings[0].Message, "refuse the job at commit") {
			t.Errorf("%s readiness=%s intended_ping=%+v findings=%+v", tr.TargetID, tr.Readiness, tr.IntendedPing, tr.Findings)
		}
		if tr.Readiness == records.ReadinessInvalid {
			t.Errorf("%s became invalid on a warning", tr.TargetID)
		}
	}
	stdout.Reset()
	if got := Main(dryRunArgs(sets, "--no-daemon", "--ping"), strings.NewReader(""), &stdout, &stderr); got != 0 || !strings.Contains(stdout.String(), "capability=unavailable)") || !strings.Contains(stdout.String(), "finding: warning icmp_capability_unavailable") {
		t.Errorf("unavailable text rendering exit=%d:\n%s", got, stdout.String())
	}
	// Disabled: nothing is detected and the text says so.
	calls := hookDetect(t, icmpgate.Capability{})
	stdout.Reset()
	if got := Main(dryRunArgs(sets, "--no-daemon", "--noping"), strings.NewReader(""), &stdout, &stderr); got != 0 || *calls != 0 || !strings.Contains(stdout.String(), "intended: ping=disabled transport=") {
		t.Errorf("disabled: exit=%d detect calls=%d:\n%s", got, *calls, stdout.String())
	}
}
