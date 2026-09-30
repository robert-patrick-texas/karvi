package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/records"
)

// stopFixture emulates a current-schema daemon whose active-job count the test
// controls, recording every lifecycle operation it receives.
type stopFixture struct {
	active   atomic.Int64
	draining atomic.Bool
	onDrain  func(*stopFixture)
	mu       sync.Mutex
	ops      []string
	done     chan struct{}

	// follow_job's answer: the start names the
	// fixture's artifact directory, no record follows, and terminal
	// replaces the default cancelled terminal.
	terminal *ipc.ActivityOutcome
}

func startStopFixture(t *testing.T, socket string, active int64, onDrain func(*stopFixture)) *stopFixture {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	f := &stopFixture{onDrain: onDrain, done: make(chan struct{})}
	f.active.Store(active)
	go func() {
		defer close(f.done)
		defer os.Remove(socket)
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			stop := f.serve(conn, socket)
			conn.Close()
			if stop {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-f.done
	})
	return f
}

func (f *stopFixture) serve(conn net.Conn, socket string) bool {
	var req ipc.Request
	if err := ipc.Read(conn, &req, 1<<20); err != nil {
		return false
	}
	reply := func(v any) {
		raw, _ := json.Marshal(v)
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Result: raw}, 1<<20)
	}
	status := func() daemon.Status {
		state := "running"
		if f.draining.Load() {
			state = "draining"
		}
		return daemon.Status{IPCSchemaVersion: ipc.SchemaVersion, Status: state, PID: os.Getpid(), UID: os.Geteuid(), Socket: socket, StartedAt: time.Now(), ActiveJobs: f.active.Load(), Version: buildinfo.Version}
	}
	switch req.Operation {
	case "ping", "status":
		reply(status())
	case "drain":
		f.record("drain")
		f.draining.Store(true)
		if f.onDrain != nil {
			f.onDrain(f)
		}
		reply(status())
	case "stop":
		var stop daemon.StopRequest
		_ = json.Unmarshal(req.Payload, &stop)
		f.record("stop:" + stop.Mode)
		if stop.Mode == daemon.StopIfIdle && f.active.Load() > 0 {
			_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Error: &ipc.Error{Code: "daemon_active_jobs", Message: fmt.Sprintf("daemon reports %d active job(s)", f.active.Load()), Details: map[string]any{}}}, 1<<20)
			return false
		}
		reply(map[string]any{"stopping": true})
		return true
	case ipc.OpCancelJob:
		// A running job answers cancelling, a finished
		// one terminal; the fixture's unknown ID answers job_unknown.
		var cr ipc.CancelRequest
		_ = json.Unmarshal(req.Payload, &cr)
		f.record("cancel:" + cr.Reason)
		if strings.HasSuffix(cr.JobID, unknownJobSuffix) {
			_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Error: &ipc.Error{Code: "job_unknown", Message: "the daemon holds no job " + cr.JobID, Details: map[string]any{}}}, 1<<20)
			return false
		}
		res := ipc.CancelResult{JobID: cr.JobID, ArtifactDir: "/tmp/jobs/" + cr.JobID}
		if f.active.Load() > 0 {
			at := time.Date(2026, 9, 15, 17, 0, 0, 0, time.UTC)
			res.State, res.RequestedAt = ipc.CancelStateCancelling, &at
		} else {
			res.State, res.Outcome = ipc.CancelStateTerminal, &ipc.ActivityOutcome{ExitCode: 0, ExitName: "ExitSuccess", ActivityID: cr.JobID, JobID: cr.JobID, Summary: records.Summary{FinalStatus: "completed"}}
		}
		reply(res)
	case ipc.OpFollowJob:
		// The start, then the terminal of a cancelled job with the
		// cancellation block; no record.
		var fr ipc.FollowRequest
		_ = json.Unmarshal(req.Payload, &fr)
		f.record("follow")
		if strings.HasSuffix(fr.JobID, unknownJobSuffix) {
			_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Error: &ipc.Error{Code: "job_unknown", Message: "the daemon holds no job " + fr.JobID, Details: map[string]any{}}}, 1<<20)
			return false
		}
		reply(ipc.FollowStart{JobID: fr.JobID, ArtifactDir: "/tmp/jobs/" + fr.JobID, HighestDurableSequence: fr.Cursor, ResumeCursor: fr.Cursor, FirstSequence: fr.Cursor + 1})
		summary := records.Summary{FinalStatus: "cancelled", Cancellation: &records.Cancellation{Reason: "fixture reason", RequestID: "r1"}}
		outcome := ipc.ActivityOutcome{ExitCode: exitcode.ExitCancelled, ExitName: "ExitCancelled", ActivityID: fr.JobID, JobID: fr.JobID, ArtifactDir: "/tmp/jobs/" + fr.JobID, Summary: summary}
		if f.terminal != nil {
			outcome = *f.terminal
		}
		raw, _ := json.Marshal(ipc.FollowTerminal{Cursor: fr.Cursor, Outcome: outcome})
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Event: ipc.EventStreamTerminal, Result: raw}, 1<<20)
	}
	return false
}

// unknownJobSuffix marks a job ID the fixture does not hold.
const unknownJobSuffix = "zz"

func (f *stopFixture) record(op string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, op)
}

func (f *stopFixture) operations() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.ops, ",")
}

func TestDaemonStopOptionsAreMutuallyExclusive(t *testing.T) {
	for _, args := range [][]string{
		{"daemon", "stop", "--grace", "--force"},
		{"daemon", "stop", "--grace", "--after=1s"},
		{"daemon", "restart", "--after=1s", "--force"},
		{"daemon", "stop", "--after=0s"},
	} {
		t.Run(strings.Join(args[1:], "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitUsageError {
				t.Fatalf("exit=%d stderr=%q", got, stderr.String())
			}
		})
	}
}

func TestDaemonStopWithoutOptionRefusesActiveJobs(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	f := startStopFixture(t, socket, 1, nil)
	var stdout, stderr bytes.Buffer
	if got := daemonStop(context.Background(), stopInvocation(t, g), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitJobRejected {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	for _, want := range []string{"daemon_active_jobs", "1 active job", "--grace", "--after", "--force"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr=%q missing %q", stderr.String(), want)
		}
	}
	if !strings.Contains(f.operations(), "stop:if_idle") {
		t.Fatalf("operations=%q", f.operations())
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("refused stop removed the daemon socket: %v", err)
	}
}

func TestDaemonStopGraceDrainsThenStops(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	f := startStopFixture(t, socket, 1, func(f *stopFixture) {
		go func() { time.Sleep(300 * time.Millisecond); f.active.Store(0) }()
	})
	var stdout, stderr bytes.Buffer
	if got := daemonStop(context.Background(), stopInvocation(t, g, "--grace"), app.IO{Stdout: &stdout, Stderr: &stderr}); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	ops := f.operations()
	if !strings.HasPrefix(ops, "drain") || !strings.HasSuffix(ops, "stop:force") {
		t.Fatalf("operations=%q, want drain then stop:force", ops)
	}
}

func TestDaemonStopAfterForcesWhenLimitElapses(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	f := startStopFixture(t, socket, 1, nil)
	var stdout, stderr bytes.Buffer
	if got := daemonStop(context.Background(), stopInvocation(t, g, "--after=300ms"), app.IO{Stdout: &stdout, Stderr: &stderr}); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	if !strings.Contains(stderr.String(), "forcing stop") {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if ops := f.operations(); !strings.HasPrefix(ops, "drain") || !strings.HasSuffix(ops, "stop:force") {
		t.Fatalf("operations=%q, want drain then stop:force", ops)
	}
}

func TestDaemonStopGraceInterruptLeavesDaemonDraining(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	f := startStopFixture(t, socket, 1, nil)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	var stdout, stderr bytes.Buffer
	if got := daemonStop(ctx, stopInvocation(t, g, "--grace"), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitCancelled {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	if !strings.Contains(stderr.String(), "remains draining") || !f.draining.Load() {
		t.Fatalf("stderr=%q draining=%t", stderr.String(), f.draining.Load())
	}
	if strings.Contains(f.operations(), "stop:") {
		t.Fatalf("interrupted wait still sent stop: %q", f.operations())
	}
}

// stopInvocation parses "daemon stop" with args and attaches the fixture's
// global options.
func stopInvocation(t *testing.T, g globalOptions, args ...string) *Invocation {
	t.Helper()
	inv, err := Parse(append([]string{"daemon", "stop"}, args...))
	if err != nil {
		t.Fatal(err)
	}
	inv.Global = g
	return inv
}
