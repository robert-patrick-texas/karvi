package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/records"
)

const fixtureJobID = "260915-170000-00"

func cancelInvocation(t *testing.T, g globalOptions, args ...string) *Invocation {
	t.Helper()
	inv, err := Parse(append([]string{"job", "cancel"}, args...))
	if err != nil {
		t.Fatal(err)
	}
	inv.Global = g
	return inv
}

// TestJobCancelRequiresOneJobID: job cancel takes exactly one job ID.
func TestJobCancelRequiresOneJobID(t *testing.T) {
	for _, args := range [][]string{{"job", "cancel"}, {"job", "cancel", "a", "b"}, {"job", "cancel", "--reason"}} {
		var stdout, stderr bytes.Buffer
		if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitUsageError {
			t.Errorf("%q: exit=%d stderr=%q", args, got, stderr.String())
		}
	}
	if _, msg := parseCode(t, "job", "cancel"); !strings.Contains(msg, "cli_positional_missing") {
		t.Errorf("message %q", msg)
	}
}

// TestJobCancelPrintsTheTwoAnswers: a running job
// prints the requested line and exits 0 with the reason sent; a finished
// job prints the already-ended line and exits 0; json prints the result.
func TestJobCancelPrintsTheTwoAnswers(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	f := startStopFixture(t, socket, 1, nil)
	var stdout, stderr bytes.Buffer
	if got := jobCancel(context.Background(), cancelInvocation(t, g, fixtureJobID, "--reason", "wrong window"), app.IO{Stdout: &stdout, Stderr: &stderr}); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	if want := "cancel requested: job " + fixtureJobID + "; artifacts /tmp/jobs/" + fixtureJobID + "\n"; stdout.String() != want {
		t.Errorf("stdout=%q want %q", stdout.String(), want)
	}
	if !strings.Contains(f.operations(), "cancel:wrong window") {
		t.Errorf("operations=%q", f.operations())
	}
	stdout.Reset()
	if got := jobCancel(context.Background(), cancelInvocation(t, g, fixtureJobID, "--format", "json"), app.IO{Stdout: &stdout, Stderr: &stderr}); got != 0 {
		t.Fatalf("json: exit=%d stderr=%q", got, stderr.String())
	}
	var res ipc.CancelResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil || res.State != ipc.CancelStateCancelling || res.JobID != fixtureJobID || res.RequestedAt == nil {
		t.Errorf("json result %+v %v: %s", res, err, stdout.String())
	}
	f.active.Store(0)
	stdout.Reset()
	if got := jobCancel(context.Background(), cancelInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != 0 {
		t.Fatalf("terminal: exit=%d stderr=%q", got, stderr.String())
	}
	if want := "job " + fixtureJobID + " already ended: completed (exit 0); artifacts /tmp/jobs/" + fixtureJobID + "\n"; stdout.String() != want {
		t.Errorf("stdout=%q want %q", stdout.String(), want)
	}
}

// TestJobCancelUnknownJobAndNoDaemon: an unknown job and an absent daemon.
func TestJobCancelUnknownJobAndNoDaemon(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	var stdout, stderr bytes.Buffer
	if got := jobCancel(context.Background(), cancelInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got == 0 || !strings.Contains(stderr.String(), "daemon_unreachable") || !strings.Contains(stderr.String(), "nothing to cancel") {
		t.Errorf("no daemon: exit=%d stderr=%q", got, stderr.String())
	}
	startStopFixture(t, socket, 1, nil)
	stderr.Reset()
	unknown := "260915-170000-" + unknownJobSuffix
	if got := jobCancel(context.Background(), cancelInvocation(t, g, unknown), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitJobRejected || !strings.Contains(stderr.String(), "job_unknown") {
		t.Errorf("unknown job: exit=%d stderr=%q", got, stderr.String())
	}
}

// TestJobCancelFollowExitsWithTheJob: --follow waits for
// the terminal and exits with the job's exit, printing the cancelled line.
func TestJobCancelFollowExitsWithTheJob(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	f := startStopFixture(t, socket, 1, nil)
	var stdout, stderr bytes.Buffer
	got := jobCancel(context.Background(), cancelInvocation(t, g, fixtureJobID, "--follow", "--reason", "x"), app.IO{Stdout: &stdout, Stderr: &stderr})
	if got != exitcode.ExitCancelled {
		t.Fatalf("exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	want := "cancel requested: job " + fixtureJobID + "; artifacts /tmp/jobs/" + fixtureJobID + "\n" +
		"job " + fixtureJobID + " cancelled: fixture reason; artifacts /tmp/jobs/" + fixtureJobID + " (exit 113)\n"
	if stdout.String() != want {
		t.Errorf("stdout=%q want %q", stdout.String(), want)
	}
	if ops := f.operations(); !strings.Contains(ops, "cancel:x") || !strings.Contains(ops, "follow") {
		t.Errorf("operations=%q", ops)
	}
}

// TestCancelledLineForms covers the cancelled line with and without artifacts.
func TestCancelledLineForms(t *testing.T) {
	if got := app.CancelledLine("j", "why", "/a"); got != "job j cancelled: why; artifacts /a" {
		t.Errorf("%q", got)
	}
	if got := app.CancelledLine("j", "", "/a"); got != "job j cancelled; artifacts /a" {
		t.Errorf("%q", got)
	}
}

// TestCancelledLineOnlyForAJobThatEndedCancelled: the follow reader prints the
// cancelled line for a summary whose final status is cancelled, and the
// result line alone for a summary that carries the block under another
// final status: an exercise, which never ends cancelled, or a live job
// whose work had finished when the cancel was accepted.
func TestCancelledLineOnlyForAJobThatEndedCancelled(t *testing.T) {
	block := &records.Cancellation{Reason: "wrong change window"}
	for _, c := range []struct {
		status   string
		block    *records.Cancellation
		exit     int
		wantLine bool
	}{
		{"cancelled", block, exitcode.ExitCancelled, true},
		{"exercised", block, exitcode.ExitSuccess, false},
		{"succeeded", block, exitcode.ExitSuccess, false},
		{"succeeded", nil, exitcode.ExitSuccess, false},
	} {
		summary := records.Summary{FinalStatus: c.status, Cancellation: c.block}
		if got := summary.CancelledBy() != nil; got != c.wantLine {
			t.Errorf("%s with block=%v: CancelledBy=%v", c.status, c.block != nil, got)
		}
		var stdout, stderr bytes.Buffer
		exit := jobFollowResult(globalOptions{}, app.IO{Stdout: &stdout, Stderr: &stderr}, "j", "/a", summary, c.exit)
		// No result line after the display: its footer and a
		// collection's line end it.
		if exit != c.exit || strings.Contains(stderr.String(), "job j cancelled") != c.wantLine || strings.Contains(stderr.String(), " exit=") {
			t.Errorf("%s with block=%v: exit=%d stderr=%q", c.status, c.block != nil, exit, stderr.String())
		}
	}
	// A crun's follow prints nothing after the display either: the
	// collection's line is the display's (display.collection.footer).
	var stderr bytes.Buffer
	crun := records.Summary{FinalStatus: "completed", Collection: &records.CollectionSummary{Directory: "/c", Replaced: 2, Kept: 1}}
	jobFollowResult(globalOptions{}, app.IO{Stdout: &bytes.Buffer{}, Stderr: &stderr}, "j", "/a", crun, 0)
	if stderr.Len() != 0 {
		t.Errorf("crun: stderr=%q", stderr.String())
	}
}
