package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/records"
)

const planReportSchema = "../../schema/plan-report.schema.json"

// exercise runs the exercise sequence against the fixture: prepare, the
// frame, the commit in exercise mode, and the follow to the terminal.
func (f *v5Fixture) exercise(jobID string, commands []string, devices []inventory.Device, seed canary.Value) (submission, ipc.CommitResult, ipc.FollowTerminal, records.PlanReport) {
	f.t.Helper()
	sub := f.prepareAndPackageMode(jobID, commands, devices, fixedInput{user: "u", pass: seed.Raw}, executionplan.ModeExercise)
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		f.t.Fatalf("frame: %+v %v", receipt, err)
	}
	result, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if err != nil {
		f.t.Fatal(err)
	}
	if result.Receipt.Mode != executionplan.ModeExercise || result.Receipt.ReportPath != filepath.Join(result.Receipt.ArtifactDir, "exercise.json") {
		f.t.Fatalf("receipt=%+v", result.Receipt)
	}
	out := f.follow(jobID, 0, 1<<20)
	if out.err != nil || len(out.records) != 0 {
		f.t.Fatalf("exercise stream: err=%v notices=%d (an exercise carries no record)", out.err, len(out.records))
	}
	raw, err := os.ReadFile(result.Receipt.ReportPath)
	if err != nil {
		f.t.Fatalf("%v; terminal exit=%s error=%q", err, out.terminal.Outcome.ExitName, out.terminal.Outcome.Error)
	}
	var report records.PlanReport
	if err := json.Unmarshal(raw, &report); err != nil {
		f.t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		f.t.Fatalf("exercise.json: %v", err)
	}
	canarytest.SchemaParity(f.t, planReportSchema, report)
	return sub, result, out.terminal, report
}

// TestExerciseValidatesWithoutDeviceContact: the exercise sequence on the
// Go fake device finalizes with
// exercise.json valid against the schema, both targets ready, an empty
// commands.jsonl, the summary exercised with exit 0, the run.exercised
// audit record, no fake-device session, the exercise stream empty (decision
// 4.9(i)), an identical receipt on replay, and the seeded password absent
// from every file of the job directory.
func TestExerciseValidatesWithoutDeviceContact(t *testing.T) {
	seed := canarytest.Seed(t)
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true, Sets: []string{`security.child-environment-allowlist=["KARVI_TEST_FAKE_DIR"]`}})
	fakeDir := t.TempDir()
	t.Setenv("KARVI_TEST_FAKE_DIR", fakeDir)
	jobID := mustID(t)
	sub, result, terminal, report := f.exercise(jobID, []string{"show clock", "show version"}, []inventory.Device{direct("127.0.0.1"), direct("core-a.example")}, seed)
	dir := result.Receipt.ArtifactDir
	if report.Kind != records.ReportExercise || report.Outcome != records.OutcomeExercised || !report.JobSubmitted || !report.TargetDataSubmitted || report.DeviceContacted || report.JobID != jobID {
		t.Fatalf("report: kind=%s outcome=%s job=%s", report.Kind, report.Outcome, report.JobID)
	}
	for _, tr := range report.Targets {
		if tr.Readiness != records.ReadinessReady || tr.CredentialBinding.Status != records.BindingBound || tr.IntendedTransport.Available != records.CheckAvailable || tr.IntendedTransport.HostKeyPolicy != "insecure" || tr.IntendedTransport.SessionInitProfile != executionplan.SessionInitNone {
			t.Fatalf("target %s: %+v", tr.TargetID, tr)
		}
		codes := map[string]bool{}
		for _, fd := range tr.Findings {
			codes[fd.Code] = true
			if fd.Severity == executionplan.SeverityError {
				t.Fatalf("target %s carries an error finding: %+v", tr.TargetID, fd)
			}
		}
		if !codes["transport_assessment"] || !codes["host_key_enrollment"] {
			t.Fatalf("target %s findings: %+v", tr.TargetID, tr.Findings)
		}
	}
	if len(report.Preparations) != 1 || len(report.Daemons) != 1 || report.CredentialPackage == nil || report.Timing.DaemonValidationNS == nil || report.Timing.DaemonPrepareNS == nil || report.Timing.PackageTransferNS == nil || report.Timing.ClientConfigNS != nil {
		t.Fatalf("report embeds: preparations=%d daemons=%d package=%v timing=%+v", len(report.Preparations), len(report.Daemons), report.CredentialPackage != nil, report.Timing)
	}
	jobLevel := map[string]bool{}
	for _, fd := range report.Findings {
		jobLevel[fd.Code] = true
	}
	if !jobLevel["dispatch_assessment"] || !jobLevel["capacity_assessment"] {
		t.Fatalf("job-level findings: %+v", report.Findings)
	}
	if terminal.Outcome.ExitCode != 0 || terminal.Outcome.Summary.FinalStatus != "exercised" || terminal.Outcome.Summary.Mode != "exercise" || terminal.Outcome.Summary.Paths["exercise"] != result.Receipt.ReportPath {
		t.Fatalf("terminal outcome: %+v", terminal.Outcome)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "commands.jsonl")); len(b) != 0 {
		t.Fatalf("commands.jsonl is not empty: %s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "failed-devices.txt")); len(b) != 0 {
		t.Fatalf("failed-devices.txt is not empty: %s", b)
	}
	audit, err := os.ReadFile(f.cfg.String("audit.file"))
	if err != nil || !strings.Contains(string(audit), `"event_name":"run.exercised"`) || !strings.Contains(string(audit), `"device_contacted":false`) || strings.Contains(string(audit), `"event_name":"run.completed"`) {
		t.Fatalf("audit: err=%v\n%s", err, audit)
	}
	if _, err := os.Stat(filepath.Join(fakeDir, "log")); err == nil {
		t.Fatal("the fake device saw a session")
	}
	var sb records.ScoreboardSnapshot
	entries, _ := filepath.Glob(filepath.Join(f.cfg.String("watch.directory"), "*"))
	for _, e := range entries {
		b, _ := os.ReadFile(e)
		_ = json.Unmarshal(b, &sb)
	}
	if sb.Status != "exercised" {
		t.Fatalf("scoreboard status %q", sb.Status)
	}
	// Replay of the same commit returns the identical receipt, report path
	// included.
	again, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if err != nil || !reflect.DeepEqual(again.Receipt, result.Receipt) {
		t.Fatalf("replay: err=%v %+v vs %+v", err, again.Receipt, result.Receipt)
	}
	tree, err := canary.ScanTree(dir, seed)
	if err != nil || len(tree.Hits) != 0 {
		t.Fatalf("the job directory carries the password: err=%v hits=%v", err, tree.Hits)
	}
	if len(f.s.Preparations()) != 0 {
		t.Fatalf("preparations retained: %v", f.s.Preparations())
	}
	t.Logf("exercise %s: %d targets ready, %d job-level findings, exit %s, %d files scanned clean", jobID, len(report.Targets), len(report.Findings), terminal.Outcome.ExitName, tree.Files)
}

// TestExerciseSecureWithoutEnrollmentIsNotReady is the exercise's secure
// vector: under ssh.host-key-policy secure with an empty trust store, every
// SSH target is not_ready with host_key_not_enrolled, the outcome is
// not_ready, the exit is the host-key exit, and the summary names
// exercise_not_ready.
func TestExerciseSecureWithoutEnrollmentIsNotReady(t *testing.T) {
	seed := canarytest.Seed(t)
	trust := filepath.Join(t.TempDir(), "trust")
	if err := os.Mkdir(trust, 0o700); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(trust, "known_hosts")
	if err := os.WriteFile(store, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true, Sets: []string{`ssh.host-key-policy="secure"`, `ssh.known-hosts-file="` + store + `"`}})
	jobID := mustID(t)
	_, result, terminal, report := f.exercise(jobID, []string{"show clock"}, []inventory.Device{direct("127.0.0.1")}, seed)
	if report.Outcome != records.OutcomeNotReady || report.Targets[0].Readiness != records.ReadinessNotReady || report.Counts.Errors != 1 {
		t.Fatalf("report: outcome=%s readiness=%s counts=%+v", report.Outcome, report.Targets[0].Readiness, report.Counts)
	}
	found := false
	for _, fd := range report.Targets[0].Findings {
		if fd.Code == "host_key_not_enrolled" && fd.Severity == executionplan.SeverityError && fd.Stage == "host_key" {
			found = true
		}
	}
	if !found {
		t.Fatalf("findings: %+v", report.Targets[0].Findings)
	}
	o := terminal.Outcome
	if o.ExitCode != exitcode.ExitHostKeyFailure || o.Summary.FinalStatus != "exercised" || len(o.Summary.TerminalCauses) != 1 || o.Summary.TerminalCauses[0] != "exercise_not_ready" || !strings.HasPrefix(o.Error, "exercise_not_ready: 1 of 1 targets not ready") {
		t.Fatalf("outcome: exit=%d status=%s causes=%v error=%q", o.ExitCode, o.Summary.FinalStatus, o.Summary.TerminalCauses, o.Error)
	}
	if _, err := os.Stat(result.Receipt.ReportPath); err != nil {
		t.Fatal(err)
	}
	t.Logf("exercise %s: not_ready, exit %s, %s", jobID, o.ExitName, o.Error)
}

// TestCancelOfAnExerciseChangesNothing: an exercise never ends cancelled.
// cancel_job is sent the
// moment commit_job answers, over 250 targets; it answers cancelling while
// the exercise runs (every round of the executed example, since the
// exercise takes 45 to 170 ms here and the cancel 3) or terminal once it has
// finished, and either way the job ends exercised at exit 0 with its report
// valid and every target ready. An accepted cancel wrote one
// run.cancel_requested audit record, and a summary that carries the
// cancellation block carries that request; a terminal answer wrote no
// record and leaves the block null. The block can be null after an accepted
// cancel only when the cancel landed after the summary was built, which the
// test admits and logs.
func TestCancelOfAnExerciseChangesNothing(t *testing.T) {
	seed := canarytest.Seed(t)
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true, Sets: []string{`security.child-environment-allowlist=["KARVI_TEST_FAKE_DIR"]`}})
	t.Setenv("KARVI_TEST_FAKE_DIR", t.TempDir())
	var devices []inventory.Device
	for i := 1; i <= 250; i++ {
		devices = append(devices, direct(fmt.Sprintf("127.0.1.%d", i)))
	}
	jobID := mustID(t)
	sub := f.prepareAndPackageMode(jobID, []string{"show clock"}, devices, fixedInput{user: "u", pass: seed.Raw}, executionplan.ModeExercise)
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	result, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if err != nil {
		t.Fatal(err)
	}
	const reason = "cancel of an exercise"
	res, err := CancelJob(f.ctx(), f.socket, 1<<20, ipc.CancelRequest{JobID: jobID, Reason: reason})
	if err != nil {
		t.Fatal(err)
	}
	out := f.follow(jobID, 0, 1<<20)
	if out.err != nil || len(out.records) != 0 || out.terminal.Outcome.ExitCode != exitcode.ExitSuccess {
		t.Fatalf("stream: err=%v notices=%d terminal=%+v", out.err, len(out.records), out.terminal.Outcome)
	}
	dir := result.Receipt.ArtifactDir
	var report records.PlanReport
	raw, err := os.ReadFile(result.Receipt.ReportPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("exercise.json: %v", err)
	}
	if report.Outcome != records.OutcomeExercised || len(report.Targets) != len(devices) {
		t.Fatalf("report: outcome=%s targets=%d", report.Outcome, len(report.Targets))
	}
	for _, tr := range report.Targets {
		if tr.Readiness != records.ReadinessReady {
			t.Fatalf("target %s: readiness=%s", tr.TargetID, tr.Readiness)
		}
	}
	var summary records.Summary
	raw, err = os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.FinalStatus != "exercised" || summary.ExitCode != exitcode.ExitSuccess || summary.DeviceCounts["cancelled"] != 0 {
		t.Errorf("summary: final_status=%s exit=%d counts=%v", summary.FinalStatus, summary.ExitCode, summary.DeviceCounts)
	}
	audit, _ := os.ReadFile(f.cfg.String("audit.file"))
	requested := strings.Count(string(audit), `"event_name":"run.cancel_requested"`)
	switch res.State {
	case ipc.CancelStateCancelling:
		if requested != 1 {
			t.Errorf("an accepted cancel wrote %d run.cancel_requested records, want 1", requested)
		}
		if k := summary.Cancellation; k == nil {
			t.Logf("the cancel was accepted after the summary was built; the audit record alone names it")
		} else if k.Reason != reason || res.RequestedAt == nil || !k.RequestedAt.Equal(*res.RequestedAt) {
			t.Errorf("cancellation=%+v, want the accepted request (%v)", k, res.RequestedAt)
		}
	case ipc.CancelStateTerminal:
		t.Logf("the exercise finished before the cancel arrived")
		if requested != 0 || summary.Cancellation != nil {
			t.Errorf("a terminal answer: %d audit records, cancellation=%+v", requested, summary.Cancellation)
		}
	default:
		t.Fatalf("cancel_job state %q", res.State)
	}
}
