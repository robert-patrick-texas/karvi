package jobexec

import (
	"context"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/output"
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
	st := exerciseState{id: "activity", jobID: "job", store: store}
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	rq := JobCancelRequest{Reason: "wrong change window", RequestedAt: at, RequestID: "request", RequesterPID: 4242, RequesterUID: 1000}
	build := func(ctx context.Context, outcome string, code int) records.Summary {
		return buildExerciseSummary(Request{Mode: executionplan.ModeExercise}, st, "exercise.json", at, at.Add(time.Second), code, outcome, map[string]int{}, cancellationOf(ctx), audit.Status{}, nil, nil)
	}

	cancelled, cancel := context.WithCancelCause(context.Background())
	cancel(rq.Cause())
	for _, c := range []struct {
		outcome string
		code    int
	}{{records.OutcomeExercised, exitcode.ExitSuccess}, {records.OutcomeNotReady, exitcode.ExitHostKeyFailure}} {
		s := build(cancelled, c.outcome, c.code)
		if s.FinalStatus != "exercised" || s.ExitCode != c.code {
			t.Errorf("%s under a cancel: final_status=%s exit=%d, want exercised and %d", c.outcome, s.FinalStatus, s.ExitCode, c.code)
		}
		k := s.Cancellation
		if k == nil || k.Reason != rq.Reason || !k.RequestedAt.Equal(at) || k.RequestID != rq.RequestID || k.Requester.PID != rq.RequesterPID || k.Requester.UID != rq.RequesterUID {
			t.Errorf("%s under a cancel: cancellation=%+v, want the request", c.outcome, k)
		}
	}

	shutdown, stop := context.WithCancelCause(context.Background())
	stop(ErrShutdownForced)
	if s := build(shutdown, records.OutcomeExercised, exitcode.ExitSuccess); s.Cancellation != nil || s.FinalStatus != "exercised" {
		t.Errorf("under a shutdown cause: cancellation=%+v final_status=%s", s.Cancellation, s.FinalStatus)
	}
	if s := build(context.Background(), records.OutcomeExercised, exitcode.ExitSuccess); s.Cancellation != nil {
		t.Errorf("without a cancel: cancellation=%+v", s.Cancellation)
	}
}
