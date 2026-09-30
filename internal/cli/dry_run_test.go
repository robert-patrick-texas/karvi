package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/records"
)

const planReportSchema = "../../schema/plan-report.schema.json"

// dryRunSets is the configuration a dry-run test runs under: the test
// runtime's base directory, a fake system ssh binary that must never be
// invoked, and the daemon-resolution gate open so a daemon-authority
// target can be planned.
func dryRunSets(t *testing.T, base string) []string {
	t.Helper()
	fake := filepath.Join(base, "fake-ssh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'fake-ssh invoked' >>\"$0.log\"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	trust := filepath.Join(base, "trust")
	if err := os.MkdirAll(trust, 0o700); err != nil {
		t.Fatal(err)
	}
	return []string{
		"--set", fmt.Sprintf("basedir=%q", base),
		"--set", fmt.Sprintf("ssh.transports.system=%q", fake),
		"--set", "audit.journald-required=false",
		"--set", fmt.Sprintf("audit.file=%q", filepath.Join(base, "audit.jsonl")),
		"--set", fmt.Sprintf("ssh.known-hosts-file=%q", filepath.Join(trust, "known_hosts")),
		"--set", "display.color=never",
		"--set", "name.allow-daemon-resolution=true",
	}
}

// globalsFor is the launcher's view of sets: the daemon serve child loads
// the same configuration the client runs under (the fake transport, the
// audit file, the trust store), never the developer's own.
func globalsFor(sets []string) globalOptions {
	g := globalOptions{}
	for i := 0; i+1 < len(sets); i += 2 {
		if sets[i] == "--set" {
			g.sets = append(g.sets, sets[i+1])
		}
	}
	return g
}

func dryRunArgs(sets []string, extra ...string) []string {
	args := append([]string{}, sets...)
	args = append(args, "run", "--dry-run", "--target", "127.0.0.1", "--target", "core-a.example", "--address-authority", "core-a.example=daemon", "--transport", "system")
	args = append(args, extra...)
	return append(args, "show", "clock")
}

func stopTestDaemon(t *testing.T, socket string, maxFrame int64) {
	t.Helper()
	sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()
	_, _ = daemon.StopCompatible(sctx, socket, maxFrame, daemon.StopForce)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); os.IsNotExist(err) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestRunDryRunAgainstADaemon: against
// a daemon the real launcher started, run --dry-run with a canary password
// produces a valid inspection report that matches the schema, the daemon
// log carries no prepare_job, provide_credentials, or commit_job line, no
// job directory appears, the fake device is never invoked, and the canary
// is absent from both streams.
func TestRunDryRunAgainstADaemon(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	g := globalsFor(sets)
	seed := canarytest.Seed(t)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", seed.Raw)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var launch bytes.Buffer
	rt, err := ensureDaemon(ctx, g, &launch)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (%s)", err, launch.String())
	}
	defer stopTestDaemon(t, socket, rt.MaxFrame)

	var stdout, stderr bytes.Buffer
	if got := Main(dryRunArgs(sets, "--format", "json"), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	var report records.PlanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report: %v\n%s", err, stdout.String())
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParity(t, planReportSchema, report)
	if report.Kind != records.ReportInspection || report.Outcome != records.OutcomePlanned || report.JobSubmitted || report.TargetDataSubmitted || report.DeviceContacted || report.NextOperation != records.NextCommitLiveJob {
		t.Fatalf("report shape: kind=%s outcome=%s submitted=%t/%t contacted=%t next=%s", report.Kind, report.Outcome, report.JobSubmitted, report.TargetDataSubmitted, report.DeviceContacted, report.NextOperation)
	}
	if len(report.Daemons) != 1 || report.Daemons[0].Status != executionplan.DaemonRunning || !report.Daemons[0].Reachable || report.Daemons[0].AutoLaunched || report.Daemons[0].IPCSchemaVersion != ipc.SchemaVersion {
		t.Fatalf("daemon row: %+v", report.Daemons)
	}
	if len(report.Targets) != 2 || report.Targets[0].Readiness != records.ReadinessPlanned || report.Targets[0].CredentialBinding.Status != records.BindingBound || report.Targets[0].CredentialBinding.DeviceUsername != "u" {
		t.Fatalf("client target: %+v", report.Targets)
	}
	if report.Targets[1].Readiness != records.ReadinessDeferred || report.Targets[1].Address.ResolutionActor != records.Deferred || report.Targets[1].CredentialBinding.Status != records.Deferred {
		t.Fatalf("daemon target must be deferred: %+v", report.Targets[1])
	}
	if report.Timing.DaemonProbeNS == nil || report.Timing.ClientCredentialNS == nil || report.Timing.DaemonPrepareNS != nil {
		t.Fatalf("timing: %+v", report.Timing)
	}
	// Nothing reached the daemon beyond the probe, nothing was written
	// under the jobs tree, and the fake device was never invoked.
	log, _ := os.ReadFile(rt.LogPath)
	for _, op := range []string{"prepare_job", "provide_credentials", "commit_job"} {
		if strings.Contains(string(log), op) {
			t.Errorf("the daemon log names %s:\n%s", op, log)
		}
	}
	if entries, _ := filepath.Glob(filepath.Join(base, "jobs", "*", "*")); len(entries) != 0 {
		t.Errorf("a job directory appeared: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(base, "fake-ssh.log")); err == nil {
		t.Error("the fake device was invoked")
	}
	for name, stream := range map[string][]byte{"stdout": stdout.Bytes(), "stderr": stderr.Bytes(), "daemon log": log} {
		if hits := canary.Scan(name, stream, seed); len(hits) != 0 {
			t.Errorf("%s carries the canary: %v", name, hits)
		}
	}
	t.Logf("inspection report %d bytes, %d targets, daemon %s; stderr %q", stdout.Len(), len(report.Targets), report.Daemons[0].Status, stderr.String())
}

// TestRunDryRunWithoutADaemon: with no daemon the
// dry-run reports absent, launches nothing, and exits 0; with --no-daemon
// it probes nothing.
func TestRunDryRunWithoutADaemon(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	var stdout, stderr bytes.Buffer
	if got := Main(dryRunArgs(sets), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"outcome: planned", "daemon local: absent", "finding: info daemon_absent", "- name:127.0.0.1: planned", "- name:core-a.example: deferred", "credential: <deferred_to_exercise_or_live_prepare>", "next: commit_live_job"} {
		if !strings.Contains(out, want) {
			t.Errorf("text report lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Errorf("a daemon socket appeared at %s", socket)
	}
	if _, err := os.Stat(filepath.Join(base, "state", "daemon.json")); !os.IsNotExist(err) {
		t.Error("a daemon state file appeared")
	}
	if strings.Contains(stderr.String(), "daemon started") {
		t.Errorf("a daemon was launched: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if got := Main(dryRunArgs(sets, "--no-daemon", "--format", "jsonl"), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("--no-daemon exit=%d stderr=%q", got, stderr.String())
	}
	var report records.PlanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || len(report.Daemons) != 0 || len(report.Findings) != 0 {
		t.Fatalf("--no-daemon report: err=%v daemons=%d findings=%d", err, len(report.Daemons), len(report.Findings))
	}
	if strings.Count(stdout.String(), "\n") != 1 {
		t.Errorf("jsonl must be one line, got %d", strings.Count(stdout.String(), "\n"))
	}
	// The session-init profile: none
	// without a map for the bound target, deferred for the daemon-authority
	// one; with a map the bound target shows the selection.
	profiles := func(r records.PlanReport) []string {
		out := []string{}
		for _, tr := range r.Targets {
			out = append(out, tr.IntendedTransport.SessionInitProfile)
		}
		return out
	}
	if got := profiles(report); strings.Join(got, ",") != "none,"+records.Deferred {
		t.Errorf("profiles without a map: %v", got)
	}
	mapped := append(append([]string{}, sets...), "--set", `session-init.quiet.commands=[]`, "--set", `session-init-map.0.profile="quiet"`, "--set", `session-init-map.0.name="*"`)
	stdout.Reset()
	stderr.Reset()
	if got := Main(dryRunArgs(mapped, "--no-daemon", "--format", "jsonl"), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("mapped exit=%d stderr=%q", got, stderr.String())
	}
	report = records.PlanReport{}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if got := profiles(report); strings.Join(got, ",") != "quiet,"+records.Deferred {
		t.Errorf("profiles with a map: %v", got)
	}
	// Equal address-cidr prefixes fail planning with the rules named.
	ambiguous := append([]string{}, sets...)
	for _, set := range []string{`session-init.quiet.commands=[]`,
		`session-init-map.0.profile="quiet"`, `session-init-map.0.address-cidr="127.0.0.0/8"`,
		`session-init-map.1.profile="quiet"`, `session-init-map.1.address-cidr="127.0.0.0/8"`, `session-init-map.1.site="*"`,
		`session-init-map.2.profile="quiet"`, `session-init-map.2.name="*"`} {
		ambiguous = append(ambiguous, "--set", set)
	}
	stdout.Reset()
	stderr.Reset()
	if got := Main(dryRunArgs(ambiguous, "--no-daemon"), strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitConfigValidationError || !strings.Contains(stderr.String(), "session_init_prefix_ambiguous") || !strings.Contains(stderr.String(), "rules session-init-map.0 (--set[") {
		t.Errorf("ambiguous map: exit=%d stderr=%q", got, stderr.String())
	}
}

// TestRunDryRunIncompatibleDaemonIsInvalid: an older-schema daemon answers
// the probe; the report says incompatible with an error finding, the
// outcome is invalid, and the exit is the finding code's (112).
func TestRunDryRunIncompatibleDaemonIsInvalid(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	done := startCLILifecycleFixture(t, socket, 2, "0.8.0", 0)
	defer func() {
		// The fixture ends on a stop request, which the older schema accepts.
		sctx, scancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer scancel()
		_, _ = daemon.StopCompatible(sctx, socket, 1<<20, daemon.StopForce)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("the lifecycle fixture did not stop")
		}
	}()
	var stdout, stderr bytes.Buffer
	got := Main(dryRunArgs(sets, "--format", "json"), strings.NewReader(""), &stdout, &stderr)
	if got != exitcode.ExitJobRejected || !strings.HasPrefix(stderr.String(), "daemon_incompatible: ") {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	var report records.PlanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	if report.Outcome != records.OutcomeInvalid || report.Daemons[0].Status != executionplan.DaemonIncompatible || report.Daemons[0].IPCSchemaVersion != 2 || report.Counts.Errors != 1 {
		t.Fatalf("report: outcome=%s daemon=%+v counts=%+v", report.Outcome, report.Daemons[0], report.Counts)
	}
}

// TestRunDryRunAbortsNameTheStage: a failure before the draft exists
// aborts with the live run's code and exit, the message naming the client
// planning stage; and the rehearsal-flag pairs are refused.
func TestRunDryRunAbortsNameTheStage(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	var stdout, stderr bytes.Buffer
	args := append(append([]string{}, sets...), "--set", "name.allow-daemon-resolution=false", "run", "--dry-run", "--target", "127.0.0.1", "--address-authority", "127.0.0.1=daemon", "--transport", "system", "show", "clock")
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitPermissionError || !strings.HasPrefix(stderr.String(), "daemon_resolution_not_allowed: client planning: ") {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("an abort produced a report: %s", stdout.String())
	}
	for extra, code := range map[string]string{"--exercise": "run_mode_conflict", "--detach": "run_mode_conflict"} {
		stdout.Reset()
		stderr.Reset()
		got := Main(dryRunArgs(sets, extra), strings.NewReader(""), &stdout, &stderr)
		// Both pairs are rehearsal-flag conflicts now that every flag is built.
		if got != exitcode.ExitUsageError || !strings.HasPrefix(stderr.String(), code+": ") {
			t.Fatalf("%s: exit=%d stderr=%q", extra, got, stderr.String())
		}
	}
	inv := mustParse(t, "run", "--dry-run", "--target", "r1", "show", "clock")
	if !inv.Flag(optDryRun) || len(inv.Pending) != 0 {
		t.Fatalf("--dry-run must be built: pending=%v", inv.Pending)
	}
}
