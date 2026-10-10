package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

func startTestServer(t *testing.T) (*Server, string, <-chan struct{}) {
	t.Helper()
	dir := testsocket.Dir(t)
	socket := filepath.Join(dir, "d.sock")
	s := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid()}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Serve(ctx)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ipc.Wait(waitCtx, socket); err != nil {
		t.Fatal(err)
	}
	return s, socket, done
}

func TestStopIfIdleRefusesWhileJobsAreActive(t *testing.T) {
	s, socket, _ := startTestServer(t)
	s.active.Store(2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := StopCompatible(ctx, socket, 1<<20, StopIfIdle)
	if err == nil || !strings.Contains(err.Error(), "daemon_active_jobs") || !strings.Contains(err.Error(), "2 active job") {
		t.Fatalf("stop if_idle error = %v", err)
	}
	status, err := Ping(ctx, socket, 1<<20)
	if err != nil {
		t.Fatalf("daemon stopped despite refusal: %v", err)
	}
	if status.Status != "running" || status.ActiveJobs != 2 || !status.ActiveJobsReported {
		t.Fatalf("status = %+v", status)
	}
}

func TestDrainRejectsNewSubmissions(t *testing.T) {
	_, socket, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status, err := Drain(ctx, socket, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "draining" {
		t.Fatalf("drain status = %q", status.Status)
	}
	draft := plantest.DraftPlan()
	if _, err := PrepareJob(ctx, socket, 1<<20, ipc.PrepareRequest{Header: plantest.Header(draft, executionplan.Draft), Draft: draft}); err == nil || !strings.Contains(err.Error(), "daemon_draining") {
		t.Fatalf("prepare on a draining daemon error = %v", err)
	}
	if status, err := Ping(ctx, socket, 1<<20); err != nil || status.Status != "draining" {
		t.Fatalf("status after drain = %+v, %v", status, err)
	}
}

func TestForceStopDoesNotWaitForActiveJobs(t *testing.T) {
	s, socket, done := startTestServer(t)
	s.active.Store(1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := StopCompatible(ctx, socket, 1<<20, StopForce); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("force stop did not end the daemon")
	}
}

func TestStopRejectsUnknownMode(t *testing.T) {
	_, socket, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := StopCompatible(ctx, socket, 1<<20, "later"); err == nil || !strings.Contains(err.Error(), "daemon_stop_mode_unknown") {
		t.Fatalf("unknown stop mode error = %v", err)
	}
}
