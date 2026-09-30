package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/records"
)

// waitTerminal waits for the follower's terminal, which the daemon sends
// once the job's outcome is set; after it, cancel_job answers terminal.
func (h *heldJob) waitTerminal(t *testing.T, within time.Duration) followed {
	t.Helper()
	select {
	case fw := <-h.followed:
		h.followed <- fw // assertAccounted reads it again
		return fw
	case <-time.After(within):
		t.Fatalf("the follower received no terminal within %s", within)
		return followed{}
	}
}

// waitSummary waits for the job's summary, which the runner writes last.
func (h *heldJob) waitSummary(t *testing.T, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if _, err := os.Stat(filepath.Join(h.dir, "summary.json")); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no summary.json within %s", within)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCancelJobInFlightAccountsEveryUnitCancelled: a cancel_job while the
// first command is held. The daemon
// answers cancelling at once; every unit is cancelled with the code
// cancelled, the command in flight included, its message carrying the
// reason; the summary and scoreboard are cancelled at exit 113 with the
// three devices counted cancelled; the audit record names the reason; the
// follower receives the terminal; a repeated request answers the first
// request's time; and the daemon keeps serving.
func TestCancelJobInFlightAccountsEveryUnitCancelled(t *testing.T) {
	h := startHeldJob(t, "20s", nil)
	start := time.Now()
	res, err := CancelJob(h.f.ctx(), h.f.socket, 1<<20, ipc.CancelRequest{JobID: h.jobID, Reason: "wrong change window"})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("cancel_job answered after %s; it must not wait for the accounting", took)
	}
	if res.State != ipc.CancelStateCancelling || res.JobID != h.jobID || res.ArtifactDir != h.dir || res.RequestedAt == nil || res.Outcome != nil {
		t.Fatalf("result %+v", res)
	}
	again, err := CancelJob(h.f.ctx(), h.f.socket, 1<<20, ipc.CancelRequest{JobID: h.jobID, Reason: "second thoughts"})
	if err != nil || again.State != ipc.CancelStateCancelling || again.RequestedAt == nil || !again.RequestedAt.Equal(*res.RequestedAt) {
		t.Errorf("repeated request: %+v %v (first requested_at %s)", again, err, res.RequestedAt)
	}
	h.waitTerminal(t, 15*time.Second)
	if n := h.sent(); n != 1 {
		t.Errorf("the fake device received %d commands, want the one in flight", n)
	}
	h.assertAccounted(t, "cancelled", "cancelled", "cancelled", exitcode.ExitCancelled, "halt:cancelled", "cancelled")
	s := readSummary(t, h.dir)
	if c := s.DeviceCounts; c["cancelled"] != len(h.devices) || c["completed"] != 0 || c["failed"] != 0 || c["not_started"] != 0 {
		t.Errorf("device_counts %+v", c)
	}
	for _, r := range readRecords(t, h.dir) {
		if r.Error == nil || !strings.Contains(r.Error.Message, "wrong change window") {
			t.Errorf("record %s/%d message %+v does not carry the reason", r.SelectedAddress, r.CommandIndex, r.Error)
		}
	}
	// The cancellation block: the first request's time and
	// reason, this process as the requester, and the request ID.
	if c := s.Cancellation; c == nil || !c.RequestedAt.Equal(*res.RequestedAt) || c.Reason != "wrong change window" || c.Requester.PID != os.Getpid() || c.Requester.UID != os.Getuid() || c.RequestID == "" {
		t.Errorf("summary cancellation %+v (requested_at %s)", s.Cancellation, res.RequestedAt)
	}
	canarytest.SchemaParity(t, "../../schema/job-summary.schema.json", s)
	// The scoreboard schema declares its top-level keys by name only, so
	// the parity tool can check the counts block alone.
	sb := readScoreboard(t, h.f.cfg.String("watch.directory"))
	canarytest.SchemaParityAt(t, "../../schema/scoreboard.schema.json", "#/properties/counts", sb.Counts)
	if sb.Counts.Cancelled != len(h.devices) {
		t.Errorf("scoreboard counts %+v", sb.Counts)
	}
	// Scoreboard schema 2: the daemon's job says so, its mode
	// is run, every device is a target and ends cancelled, and the inputs
	// and metrics are present.
	if sb.SchemaVersion != records.ScoreboardSchemaVersion || !sb.Daemon || sb.Mode != "run" || len(sb.Targets) != len(h.devices) || sb.Inputs == nil || sb.Metrics == nil || sb.Commands == nil {
		t.Errorf("scoreboard schema=%d daemon=%v mode=%s targets=%d inputs=%v metrics=%v commands=%v", sb.SchemaVersion, sb.Daemon, sb.Mode, len(sb.Targets), sb.Inputs, sb.Metrics, sb.Commands)
	}
	for _, tg := range sb.Targets {
		if tg.State != records.TargetCancelled {
			t.Errorf("target %s state %s, want cancelled", tg.Name, tg.State)
		}
	}
	// The audit trail: one run.cancel_requested for the
	// request that cancelled, none for the repeated one, and the
	// run.completed record repeating the block.
	audit, _ := os.ReadFile(h.f.cfg.String("audit.file"))
	requested, completed := []string{}, ""
	for _, line := range strings.Split(string(audit), "\n") {
		switch {
		case strings.Contains(line, `"event_name":"run.cancel_requested"`):
			requested = append(requested, line)
		case strings.Contains(line, `"event_name":"run.completed"`):
			completed = line
		}
	}
	if len(requested) != 1 || !strings.Contains(requested[0], `"outcome":"informational"`) || !strings.Contains(requested[0], `"reason":"wrong change window"`) || !strings.Contains(requested[0], `"requester_pid":`+strconv.Itoa(os.Getpid())) || !strings.Contains(requested[0], `"job_id":"`+h.jobID+`"`) {
		t.Errorf("run.cancel_requested records: %d %v", len(requested), requested)
	}
	if !strings.Contains(completed, `"cancellation":{`) || !strings.Contains(completed, `"reason":"wrong change window"`) {
		t.Errorf("run.completed record lacks the cancellation block: %s", completed)
	}
	after, err := CancelJob(h.f.ctx(), h.f.socket, 1<<20, ipc.CancelRequest{JobID: h.jobID})
	if err != nil || after.State != ipc.CancelStateTerminal || after.Outcome == nil || after.Outcome.ExitCode != exitcode.ExitCancelled || after.RequestedAt != nil {
		t.Errorf("request after the end: %+v %v", after, err)
	}
	if after.Outcome != nil && (after.Outcome.Summary.Cancellation == nil || after.Outcome.Summary.Cancellation.Reason != "wrong change window") {
		t.Errorf("the terminal outcome's summary lacks the block: %+v", after.Outcome.Summary.Cancellation)
	}
	canarytest.SchemaParityAt(t, "../../schema/daemon-ipc.schema.json", "#/$defs/cancel_result", res)
	canarytest.SchemaParityAt(t, "../../schema/daemon-ipc.schema.json", "#/$defs/cancel_result", after)
	if st, err := Ping(h.f.ctx(), h.f.socket, 1<<20); err != nil || st.ActiveJobs != 0 {
		t.Errorf("daemon after the cancel: %+v %v", st, err)
	}
}

// TestCancelJobOnAFinishedJobAnswersTerminal: a job that
// finished on its own answers terminal with its outcome and no request
// time, and its accounting is untouched.
func TestCancelJobOnAFinishedJobAnswersTerminal(t *testing.T) {
	h := startHeldJob(t, "0s", nil)
	if fw := h.waitTerminal(t, 15*time.Second); fw.err != nil || fw.terminal.Outcome.ExitCode != exitcode.ExitSuccess {
		t.Fatalf("follow: %v exit=%d", fw.err, fw.terminal.Outcome.ExitCode)
	}
	res, err := CancelJob(h.f.ctx(), h.f.socket, 1<<20, ipc.CancelRequest{JobID: h.jobID, Reason: "too late"})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != ipc.CancelStateTerminal || res.Outcome == nil || res.Outcome.ExitCode != exitcode.ExitSuccess || res.RequestedAt != nil || res.ArtifactDir != h.dir {
		t.Errorf("result %+v", res)
	}
	s := readSummary(t, h.dir)
	if s.FinalStatus != "completed" || s.DeviceCounts["cancelled"] != 0 || s.DeviceCounts["succeeded"] != len(h.devices) || s.Cancellation != nil {
		t.Errorf("summary %s %+v cancellation=%+v", s.FinalStatus, s.DeviceCounts, s.Cancellation)
	}
	if audit, _ := os.ReadFile(h.f.cfg.String("audit.file")); strings.Contains(string(audit), "run.cancel_requested") {
		t.Error("a terminal answer wrote a run.cancel_requested record")
	}
}

// TestCancelJobUnknownAndMalformed (decisions 2.1, 2.4).
func TestCancelJobUnknownAndMalformed(t *testing.T) {
	f := newV5Fixture(t)
	_, err := CancelJob(f.ctx(), f.socket, 1<<20, ipc.CancelRequest{JobID: mustID(t)})
	if errorcodes.Of(err) != "job_unknown" {
		t.Errorf("unknown job: %v", err)
	}
	_, err = CancelJob(f.ctx(), f.socket, 1<<20, ipc.CancelRequest{JobID: "nope"})
	if errorcodes.Of(err) != "job_request_malformed" {
		t.Errorf("bad id: %v", err)
	}
	_, err = CancelJob(f.ctx(), f.socket, 1<<20, ipc.CancelRequest{JobID: mustID(t), Reason: strings.Repeat("x", ipc.MaxCancelReasonBytes+1)})
	if errorcodes.Of(err) != "job_request_malformed" {
		t.Errorf("long reason: %v", err)
	}
}

// TestCancelThenForcedStopKeepsTheCancelledOutcome: a forced
// stop arriving after a cancel changes nothing; the job ends cancelled,
// not incomplete, and the daemon still awaits its accounting.
func TestCancelThenForcedStopKeepsTheCancelledOutcome(t *testing.T) {
	h := startHeldJob(t, "20s", nil, "daemon.forced-grace-seconds=5")
	if _, err := CancelJob(h.f.ctx(), h.f.socket, 1<<20, ipc.CancelRequest{JobID: h.jobID, Reason: "abort"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := StopCompatible(ctx, h.f.socket, 1<<20, StopForce); err != nil {
		t.Fatal(err)
	}
	h.waitStopped(t, 20*time.Second)
	h.assertAccounted(t, "cancelled", "cancelled", "cancelled", exitcode.ExitCancelled, "halt:cancelled", "cancelled")
}

// TestCancelJobLeavesOtherJobsRunning: the cancel reaches
// one job's context and no other.
func TestCancelJobLeavesOtherJobsRunning(t *testing.T) {
	h := startHeldJob(t, "3s", nil)
	other := h.f.prepareAndPackageWith(mustID(t), h.commands, []inventory.Device{direct("127.0.0.4")}, fixedInput{"alice", "pw"})
	if receipt, err := h.f.provide(other); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	otherResult, err := CommitJob(h.f.ctx(), h.f.socket, 1<<20, other.request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CancelJob(h.f.ctx(), h.f.socket, 1<<20, ipc.CancelRequest{JobID: h.jobID}); err != nil {
		t.Fatal(err)
	}
	h.waitSummary(t, 15*time.Second)
	terminal := h.f.followToEnd(otherResult.Receipt.JobID)
	if terminal.Outcome.ExitCode != exitcode.ExitSuccess || terminal.Outcome.Summary.FinalStatus != "completed" {
		t.Errorf("the other job: exit=%d status=%s", terminal.Outcome.ExitCode, terminal.Outcome.Summary.FinalStatus)
	}
	if s := readSummary(t, h.dir); s.FinalStatus != "cancelled" {
		t.Errorf("the cancelled job: %s", s.FinalStatus)
	}
}
