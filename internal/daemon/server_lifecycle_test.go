package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if status.Status != "running" || status.ActiveJobs != 2 {
		t.Fatalf("status = %+v", status)
	}
}

// TestStopWithoutModeIsIfIdle: a stop that names no mode is if_idle, refused
// while a job runs (EXAMPLES chapter 57, issue 4).
func TestStopWithoutModeIsIfIdle(t *testing.T) {
	s, socket, _ := startTestServer(t)
	s.active.Store(1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := ipc.NewRequest("stop-without-mode", "stop", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := ipc.Call(ctx, socket, req, 1<<20, &result); err == nil || !strings.Contains(err.Error(), "daemon_active_jobs") {
		t.Fatalf("stop without a mode: %v %v", result, err)
	}
	if status, err := Ping(ctx, socket, 1<<20); err != nil || status.Status != "running" {
		t.Fatalf("status after the refusal = %+v, %v", status, err)
	}
}

// TestDrainedIdleDaemonExitsAtOnce: a drain of a daemon holding no job is
// answered, draining, and the daemon exits at once, Serve returning nil (exit
// 0), its socket removed and "stopping: drained" logged (EXAMPLES chapter 57,
// issue 4).
func TestDrainedIdleDaemonExitsAtOnce(t *testing.T) {
	dir := testsocket.Dir(t)
	socket := filepath.Join(dir, "d.sock")
	var log bytes.Buffer
	s := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid(), Logger: slog.New(slog.NewTextHandler(&log, nil))}
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx) }()
	t.Cleanup(stop)
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ipc.Wait(waitCtx, socket); err != nil {
		t.Fatal(err)
	}
	status, err := Drain(waitCtx, socket, 1<<20, ipc.SchemaVersion)
	if err != nil || status.Status != "draining" || status.ActiveJobs != 0 {
		t.Fatalf("drain = %+v, %v", status, err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the drained daemon holding no job did not exit")
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Errorf("the socket remains: %v", err)
	}
	if !strings.Contains(log.String(), "stopping: drained") {
		t.Errorf("the log lacks the drained stop: %q", log.String())
	}
}

// TestAStopTakesOverFromADrain: a forced stop or a signal after a drain ends
// the daemon holding a job, and the job's end after it logs no drained stop,
// so the log names the stop that ended the daemon.
func TestAStopTakesOverFromADrain(t *testing.T) {
	for _, how := range []string{"force", "signal"} {
		t.Run(how, func(t *testing.T) {
			dir := testsocket.Dir(t)
			socket := filepath.Join(dir, "d.sock")
			var log bytes.Buffer
			signals := make(chan os.Signal, 1)
			s := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid(), Logger: slog.New(slog.NewTextHandler(&log, nil)), Signals: signals}
			s.active.Store(1)
			ctx, stop := context.WithCancel(context.Background())
			served := make(chan error, 1)
			go func() { served <- s.Serve(ctx) }()
			t.Cleanup(stop)
			waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := ipc.Wait(waitCtx, socket); err != nil {
				t.Fatal(err)
			}
			if _, err := Drain(waitCtx, socket, 1<<20, ipc.SchemaVersion); err != nil {
				t.Fatal(err)
			}
			if how == "force" {
				if _, err := StopCompatible(waitCtx, socket, 1<<20, StopForce); err != nil {
					t.Fatal(err)
				}
			} else {
				// The grace is zero here (no configuration): it expires at once.
				signals <- os.Interrupt
			}
			select {
			case <-served:
			case <-time.After(5 * time.Second):
				t.Fatal("the daemon did not stop")
			}
			s.active.Add(-1)
			s.exitIfDrained()
			if strings.Contains(log.String(), "stopping: drained") {
				t.Errorf("the job's end after the %s logged the drained stop: %q", how, log.String())
			}
		})
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
