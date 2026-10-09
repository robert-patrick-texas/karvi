package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/capacity"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

// newHoldHarness is the gate harness with a system transport that holds
// every command for five seconds after marking that it was sent, so a
// context can be cancelled while a command is in flight.
func newHoldHarness(t *testing.T) *gateHarness {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "command-sent")
	script := filepath.Join(dir, "fake-ssh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncase \" $* \" in *' -O '*) exit 1;; esac\nprintf 'dev#'\nwhile IFS= read -r line; do\n  [ \"$line\" = exit ] && exit 0\n  : >\""+marker+"\"\n  sleep 5\n  printf '%s\\r\\nout\\r\\ndev#' \"$line\"\ndone\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	os.MkdirAll(home, 0o700)
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{
		fmt.Sprintf("ssh.transports.system=%q", script), `ssh.host-key-policy="insecure"`, fmt.Sprintf("ssh.known-hosts-file=%q", filepath.Join(dir, "known_hosts")),
	}})
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
	grant := credentialpackage.CredentialGrant{CredentialID: plantest.GrantA, Method: credentialpackage.MethodEmbeddedSecret, Username: credentials.NewSecretString("u"), Password: credentials.NewSecretString("p"), Policy: "default", Backend: "test", NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)}
	var debugLines []string
	e := New(Options{
		Config: cfg, Operator: credentials.Operator{Username: "netops", UID: 1000, Home: home}, ActivityID: plantest.JobID, JobID: plantest.JobID, ActivityType: "run",
		Commands: plantest.Commands, DispatchOrder: "default", Grants: grants{grant}, Protection: "local-peer",
		Capacity: capMgr, Store: store, ScratchDir: testsocket.Dir(t), ControlRoot: filepath.Join(dir, "control"), Home: home, AskpassPath: "/bin/true",
		Debug: func(s string) { debugLines = append(debugLines, s) },
	})
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("debug:\n%s", strings.Join(debugLines, "\n"))
		}
	})
	return &gateHarness{t: t, exec: e, store: store, marker: marker, target: target}
}

// cancelWhenSent cancels the context as soon as the transport marks a
// command sent.
func cancelWhenSent(t *testing.T, marker string, cancel func()) {
	t.Helper()
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(marker); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
}

// TestInterruptMidCommandRecordsTheCommandInFlightCancelled: a context
// cancelled with no cause while a command is at the device records that
// command cancelled with the code cancelled, not with the driver's error,
// and the remaining commands cancelled after it.
func TestInterruptMidCommandRecordsTheCommandInFlightCancelled(t *testing.T) {
	h := newHoldHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelWhenSent(t, h.marker, cancel)
	start := time.Now()
	res, recs := h.run(ctx)
	if !h.transportAttempted() {
		t.Fatalf("the command never reached the transport: result %+v, records %d", res, len(recs))
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Errorf("the executor waited %s for the held command; the cancel did not interrupt it", took)
	}
	if res.Success || res.ErrorCode != "cancelled" {
		t.Errorf("result %+v, want cancelled", res)
	}
	if len(recs) != len(plantest.Commands) {
		t.Fatalf("%d records, want %d", len(recs), len(plantest.Commands))
	}
	for i, r := range recs {
		if r.Status != "cancelled" || r.Error == nil || r.Error.Code != "cancelled" || r.Error.Category != "shutdown" {
			t.Errorf("record %d: status=%s error=%+v, want cancelled with code cancelled", i+1, r.Status, r.Error)
		}
	}
	if recs[0].Error != nil && recs[0].Error.Operation != "execute_command" {
		t.Errorf("record 1 operation %q, want execute_command", recs[0].Error.Operation)
	}
}

// TestCancelJobCauseCarriesTheReasonIntoEveryRecord: a
// context cancelled with the cancel_job cause records the cause's message,
// reason included, on the command in flight and the rest.
func TestCancelJobCauseCarriesTheReasonIntoEveryRecord(t *testing.T) {
	h := newHoldHarness(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	cancelWhenSent(t, h.marker, func() { cancel(JobCancelled("wrong change window")) })
	res, recs := h.run(ctx)
	if res.ErrorCode != "cancelled" {
		t.Errorf("result %+v, want cancelled", res)
	}
	want := "the job was cancelled by the operator: wrong change window"
	for i, r := range recs {
		if r.Status != "cancelled" || r.Error == nil || r.Error.Code != "cancelled" || r.Error.Message != want {
			t.Errorf("record %d: status=%s error=%+v, want cancelled with message %q", i+1, r.Status, r.Error, want)
		}
	}
}

// TestCancelCauseDistinguishesTheThreeCancellations.
func TestCancelCauseDistinguishesTheThreeCancellations(t *testing.T) {
	plain, cancelPlain := context.WithCancel(context.Background())
	cancelPlain()
	if CancelCause(plain) != nil || ShutdownCause(plain) != nil {
		t.Error("a plain cancel carries a cause")
	}
	job, cancelJob := context.WithCancelCause(context.Background())
	cancelJob(JobCancelled("x"))
	if c := CancelCause(job); c == nil || errorcodes.Of(c) != "cancelled" || ShutdownCause(job) != nil {
		t.Errorf("job cancel cause %v", c)
	}
	down, cancelDown := context.WithCancelCause(context.Background())
	cancelDown(ErrShutdownForced)
	if CancelCause(down) != nil || ShutdownCause(down) != ErrShutdownForced {
		t.Error("a shutdown cause reads as a job cancel")
	}
	status, code, cause := cancelStatus(job)
	if status != "cancelled" || code != "cancelled" || errorcodes.Of(cause) != "cancelled" || bareMessage(cause) != "the job was cancelled by the operator: x" {
		t.Errorf("cancelStatus(job) = %s %s %v", status, code, cause)
	}
	if status, code, _ := cancelStatus(down); status != "incomplete_shutdown" || code != "shutdown_incomplete" {
		t.Errorf("cancelStatus(shutdown) = %s %s", status, code)
	}
}
