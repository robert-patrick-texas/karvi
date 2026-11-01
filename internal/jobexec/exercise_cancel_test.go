package jobexec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/capacity"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestExerciseSummaryNamesAnAcceptedCancelAndStaysExercised: an exercise
// never ends cancelled. A
// context the cancel_job cause cancelled puts the request in the summary's
// cancellation block, as a live job's summary has it, and changes neither
// the final status nor the exit; a shutdown cause and an untouched context
// leave the block null.
func TestExerciseSummaryNamesAnAcceptedCancelAndStaysExercised(t *testing.T) {
	store, err := output.Create(output.Options{Root: t.TempDir(), ID: "job"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	req := Request{Mode: executionplan.ModeExercise}
	accepted := acceptedSummary(req, store.Paths(), "job", at)
	rq := JobCancelRequest{Reason: "wrong change window", RequestedAt: at, RequestID: "request", RequesterPID: 4242, RequesterUID: 1000}
	build := func(ctx context.Context, outcome string, code int) records.Summary {
		return buildExerciseSummary(req, accepted, store.Paths(), "exercise.json", at.Add(time.Second), code, outcome, map[string]int{}, cancellationOf(ctx), audit.Status{}, nil, nil)
	}

	cancelled, cancel := context.WithCancelCause(context.Background())
	cancel(rq.Cause())
	for _, c := range []struct {
		outcome string
		code    int
	}{{records.OutcomeExercise, exitcode.ExitSuccess}, {records.OutcomeNotReady, exitcode.ExitHostKeyFailure}} {
		s := build(cancelled, c.outcome, c.code)
		if s.FinalStatus != "exercise" || s.ExitCode != c.code {
			t.Errorf("%s under a cancel: final_status=%s exit=%d, want exercised and %d", c.outcome, s.FinalStatus, s.ExitCode, c.code)
		}
		k := s.Cancellation
		if k == nil || k.Reason != rq.Reason || !k.RequestedAt.Equal(at) || k.RequestID != rq.RequestID || k.Requester.PID != rq.RequesterPID || k.Requester.UID != rq.RequesterUID {
			t.Errorf("%s under a cancel: cancellation=%+v, want the request", c.outcome, k)
		}
	}

	shutdown, stop := context.WithCancelCause(context.Background())
	stop(ErrShutdownForced)
	if s := build(shutdown, records.OutcomeExercise, exitcode.ExitSuccess); s.Cancellation != nil || s.FinalStatus != "exercise" {
		t.Errorf("under a shutdown cause: cancellation=%+v final_status=%s", s.Cancellation, s.FinalStatus)
	}
	if s := build(context.Background(), records.OutcomeExercise, exitcode.ExitSuccess); s.Cancellation != nil {
		t.Errorf("without a cancel: cancellation=%+v", s.Cancellation)
	}
}

// TestExerciseStopsItsHeartbeatOnAnEarlyReturn: an exercise that ends
// before its final scoreboard write (here at its report's validation, for
// a request naming no operator and no plan) stops the heartbeat it
// started, so the scoreboard of a job that has ended is not kept fresh as
// a live one's.
func TestExerciseStopsItsHeartbeatOnAnEarlyReturn(t *testing.T) {
	const id = "261101-120000-00"
	dir := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: dir, SkipAuto: true, Environment: []string{}, Sets: []string{
		`watch.refresh="250ms"`, `ssh.known-hosts-file="` + filepath.Join(dir, "known_hosts") + `"`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	board, err := scoreboard.NewWriter(filepath.Join(dir, "sb"), dir, dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	board.Use(id)
	ledger, err := capacity.New(filepath.Join(dir, "cap"), dir, dir, id, 32, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := exerciseState{id: id, artifact: filepath.Join(dir, "job"), scoreboardWriter: board, capManager: ledger, base: dir}
	result := runExercise(context.Background(), Request{Config: cfg, Mode: executionplan.ModeExercise, ActivityID: id}, st)
	if result.ExitCode == exitcode.ExitSuccess {
		t.Fatalf("the exercise without a plan succeeded: %+v", result)
	}
	// A stopped heartbeat writes nothing more: the scoreboard, removed,
	// stays removed through several of its intervals.
	_ = os.Remove(board.Path)
	refresh := cfg.Duration("watch.refresh")
	for end := time.Now().Add(4 * refresh); time.Now().Before(end); time.Sleep(refresh / 10) {
		if _, err := os.Stat(board.Path); err == nil {
			t.Fatalf("the heartbeat still writes %s after the exercise returned %q", board.Path, result.Error)
		}
	}
}
