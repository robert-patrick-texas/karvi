package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/records"
)

// exerciseArgs is the exercise invocation over the dry-run test's targets.
func exerciseArgs(sets []string, extra ...string) []string {
	args := append([]string{}, sets...)
	args = append(args, "run", "--exercise", "--target", "127.0.0.1", "--target", "localhost", "--address-authority", "localhost=daemon", "--transport", "system")
	args = append(args, extra...)
	return append(args, "show", "clock")
}

// exerciseRuntime is the dry-run runtime plus a stub askpass helper on
// PATH, which the exercise locates and never runs, and a daemon the real
// launcher started.
func exerciseRuntime(t *testing.T) (string, []string, string, int64, func()) {
	t.Helper()
	base, _, socket := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	g := globalsFor(sets)
	helper := filepath.Join(base, "karvi-askpass")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho 'askpass invoked' >>\"$0.log\"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", base+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NETUSER", "u")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var launch bytes.Buffer
	rt, err := ensureTestDaemon(ctx, g, &launch)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (%s)", err, launch.String())
	}
	return base, sets, socket, rt.MaxFrame, func() { stopTestDaemon(t, socket, rt.MaxFrame) }
}

// TestRunExerciseAgainstADaemon: with a
// canary password, run --exercise --format json exits 0, prints the
// daemon's exercise report, which validates and matches the schema; the
// daemon log carries prepare_job, provide_credentials, and commit_job once
// each; the summary is exercised; the fake device and the askpass helper
// are never invoked; and the canary is absent from both streams, the job
// directory, and the daemon log.
func TestRunExerciseAgainstADaemon(t *testing.T) {
	base, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	seed := canarytest.Seed(t)
	t.Setenv("NETPASS", seed.Raw)
	var stdout, stderr bytes.Buffer
	if got := Main(exerciseArgs(sets, "--format", "json"), strings.NewReader(""), &stdout, &stderr); got != 0 {
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
	if report.Kind != records.ReportExercise || report.Outcome != records.OutcomeExercised || !report.JobSubmitted || report.DeviceContacted || report.NextOperation != records.NextSubmitNewLiveJob || len(report.Targets) != 2 {
		t.Fatalf("report: kind=%s outcome=%s targets=%d", report.Kind, report.Outcome, len(report.Targets))
	}
	for _, tr := range report.Targets {
		if tr.Readiness != records.ReadinessReady || tr.CredentialBinding.Status != records.BindingBound || tr.IntendedTransport.Available != records.CheckAvailable {
			t.Fatalf("target %s: %+v", tr.TargetID, tr)
		}
	}
	// The report carries the outcome; no result line (28.4).
	if strings.Contains(stderr.String(), " exit=") {
		t.Fatalf("stderr: %q", stderr.String())
	}
	jobs, _ := filepath.Glob(filepath.Join(base, "jobs", "*", report.JobID))
	if len(jobs) != 1 {
		t.Fatalf("job directories: %v", jobs)
	}
	summary, err := os.ReadFile(filepath.Join(jobs[0], "summary.json"))
	if err != nil || !strings.Contains(compactJSON(t, summary), `"final_status":"exercised"`) || !strings.Contains(compactJSON(t, summary), `"mode":"exercise"`) {
		t.Fatalf("summary: err=%v %s", err, summary)
	}
	if b, _ := os.ReadFile(filepath.Join(jobs[0], "commands.jsonl")); len(b) != 0 {
		t.Fatalf("commands.jsonl is not empty: %s", b)
	}
	log, _ := os.ReadFile(filepath.Join(base, "logs", "daemon.log"))
	// One prepare, one frame, and one commit accepted; the commit's exit
	// line follows the terminal and may land after this read.
	for _, op := range []string{"operation=prepare_job ", "operation=provide_credentials "} {
		if n := strings.Count(string(log), op); n != 1 {
			t.Errorf("the daemon log names %s %d times, want once:\n%s", op, n, log)
		}
	}
	if n := strings.Count(string(log), "code=accepted"); n != 3 || !strings.Contains(string(log), "operation=commit_job") {
		t.Errorf("the daemon log shows %d accepted operations, want 3 (prepare, frame, commit):\n%s", n, log)
	}
	for _, invoked := range []string{"fake-ssh.log", "karvi-askpass.log"} {
		if _, err := os.Stat(filepath.Join(base, invoked)); err == nil {
			t.Errorf("%s: invoked during an exercise", invoked)
		}
	}
	for name, stream := range map[string][]byte{"stdout": stdout.Bytes(), "stderr": stderr.Bytes(), "daemon log": log} {
		if hits := canary.Scan(name, stream, seed); len(hits) != 0 {
			t.Errorf("%s carries the canary: %v", name, hits)
		}
	}
	if tree, err := canary.ScanTree(jobs[0], seed); err != nil || len(tree.Hits) != 0 {
		t.Errorf("the job directory carries the canary: err=%v hits=%v", err, tree.Hits)
	}
	// The text rendering of the same report shape.
	stdout.Reset()
	stderr.Reset()
	if got := Main(exerciseArgs(sets), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("text exit=%d stderr=%q", got, stderr.String())
	}
	for _, want := range []string{"exercise ", "status: exercised", "job_submitted: true", "device_contacted: false", "daemon local: running", "preparation ", "- name:127.0.0.1: ready", "  address: client 127.0.0.1", "- name:localhost: ready", "  address: daemon 127.0.0.1 (resolver_context=", "  credential: bound ", "  transport: system available (system ", "host_key=accept-new enrolled=no", "  intended_ping: disabled", "next: submit_new_live_job (no device was contacted; this is not a command result)"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("text report lacks %q:\n%s", want, stdout.String())
		}
	}
	t.Logf("exercise %s: json and text reports rendered; stderr %q", report.JobID, strings.TrimSpace(stderr.String()))
}

// TestRunExerciseNotReadyExitsWithTheFindingsCode: under a secure host-key
// policy with an empty trust store the exercise finalizes not_ready, the
// client prints the report and exercise_not_ready, and exits with the
// finding's code's exit (109).
func TestRunExerciseNotReadyExitsWithTheFindingsCode(t *testing.T) {
	base, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	// The fixture's trust directory already exists; an empty store there
	// is the secure vector.
	store := filepath.Join(base, "trust", "known_hosts")
	if err := os.WriteFile(store, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// The daemon already runs under the launcher's configuration; the
	// host-key policy is the daemon's own, so the
	// daemon is restarted under the secure policy through its KARVI__
	// environment, which the launcher passes on.
	stop()
	t.Setenv("KARVI__SSH__HOST_KEY_POLICY", "secure")
	t.Setenv("KARVI__SSH__KNOWN_HOSTS_FILE", store)
	sets = append(sets, "--set", "ssh.host-key-policy=\"secure\"", "--set", "ssh.known-hosts-file=\""+store+"\"")
	_, _, socket := daemonTestRuntime2(t, base)
	g := globalsFor(sets)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var launch bytes.Buffer
	rt, err := ensureTestDaemon(ctx, g, &launch)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (%s)", err, launch.String())
	}
	defer stopTestDaemon(t, socket, rt.MaxFrame)
	var stdout, stderr bytes.Buffer
	got := Main(exerciseArgs(sets, "--format", "json"), strings.NewReader(""), &stdout, &stderr)
	if got != exitcode.ExitHostKeyFailure || !strings.Contains(stderr.String(), "exercise_not_ready: 2 of 2 targets not ready") {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	var report records.PlanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report.Outcome != records.OutcomeNotReady {
		t.Fatalf("report: err=%v outcome=%s", err, report.Outcome)
	}
	t.Logf("not ready: exit %d, %s", got, strings.TrimSpace(stderr.String()))
}

// TestRunExerciseConflictsAndStages: the rehearsal-flag pairs and
// --no-daemon are run_mode_conflict, and a client-planning abort names its
// stage.
func TestRunExerciseConflictsAndStages(t *testing.T) {
	_, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	for _, tc := range []struct {
		args []string
		code string
	}{
		{exerciseArgs(sets, "--dry-run"), "run_mode_conflict"},
		{exerciseArgs(sets, "--no-daemon"), "run_mode_conflict"},
		{exerciseArgs(sets, "--detach"), "run_mode_conflict"},
	} {
		var stdout, stderr bytes.Buffer
		if got := Main(tc.args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitUsageError || !strings.HasPrefix(stderr.String(), tc.code+": ") {
			t.Errorf("%v: exit=%d stderr=%q", tc.args[len(sets):], got, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	args := append(append([]string{}, sets...), "--set", "name.allow-daemon-resolution=false", "run", "--exercise", "--target", "127.0.0.1", "--address-authority", "127.0.0.1=daemon", "--transport", "system", "show", "clock")
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitPermissionError || !strings.Contains(stderr.String(), "daemon_resolution_not_allowed: client planning: ") {
		t.Fatalf("client planning: exit=%d stderr=%q", got, stderr.String())
	}
	// A prepare refused on daemon DNS aborts under executor_dns_failed with
	// the daemon DNS preparation stage named, before any
	// package exists.
	stdout.Reset()
	stderr.Reset()
	args = append(append([]string{}, sets...), "run", "--exercise", "--target", "nxdomain.invalid", "--address-authority", "nxdomain.invalid=daemon", "--transport", "system", "show", "clock")
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitNameResolutionError || !strings.Contains(stderr.String(), "executor_dns_failed: daemon DNS preparation: 1 finding(s): name:nxdomain.invalid: ") {
		t.Fatalf("daemon DNS: exit=%d stderr=%q", got, stderr.String())
	}
	inv := mustParse(t, "run", "--exercise", "--target", "r1", "show", "clock")
	if !inv.Flag(optExercise) || len(inv.Pending) != 0 {
		t.Fatalf("--exercise must be built: pending=%v", inv.Pending)
	}
}

// daemonTestRuntime2 resolves the runtime again for an existing base.
func daemonTestRuntime2(t *testing.T, base string) (string, globalOptions, string) {
	t.Helper()
	g := globalOptions{sets: []string{"basedir=\"" + base + "\""}}
	rt, err := testRuntime(g)
	if err != nil {
		t.Fatal(err)
	}
	return base, g, rt.Socket
}
