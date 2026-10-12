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
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestEnsureDaemonReplacesAnIdleDaemonOfAnotherBuild (EXAMPLES chapter 57,
// issue 5): a daemon of another build holding no job, of an older IPC schema
// or of this one, is drained at its own schema, exits, and is replaced in the
// same call by a daemon of this build, the client saying so unless --quiet.
func TestEnsureDaemonReplacesAnIdleDaemonOfAnotherBuild(t *testing.T) {
	older := func(t *testing.T, socket string) <-chan struct{} {
		return startCLILifecycleFixture(t, socket, 2, "0.8.0", 0)
	}
	other := func(t *testing.T, socket string) <-chan struct{} {
		return startStopFixtureBuild(t, socket, 0, fixtureBuild, nil).done
	}
	for _, tc := range []struct {
		name           string
		start          func(*testing.T, string) <-chan struct{}
		version, build string
		quiet          bool
	}{
		{"an older schema", older, "0.8.0", "none", false},
		{"this schema, another build", other, buildinfo.Version, fixtureBuild, false},
		{"this schema, another build, --quiet", other, buildinfo.Version, fixtureBuild, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, g, socket := daemonTestRuntime(t)
			g.quiet = tc.quiet
			done := tc.start(t, socket)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var stderr bytes.Buffer
			rt, err := ensureTestDaemon(ctx, g, &stderr)
			if err != nil {
				t.Fatalf("ensureDaemon: %v (stderr %q)", err, stderr.String())
			}
			defer stopTestDaemon(t, socket, rt.MaxFrame)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("the drained daemon did not exit")
			}
			want := fmt.Sprintf("daemon drained version=%s build=%s\ndaemon started socket=%s\n", tc.version, tc.build, socket)
			if tc.quiet {
				want = ""
			}
			if stderr.String() != want {
				t.Fatalf("stderr=%q, want %q", stderr.String(), want)
			}
			probe, err := daemon.Probe(ctx, socket, rt.MaxFrame)
			if err != nil || !probe.Compatible || probe.Status.PID == os.Getpid() {
				t.Fatalf("the daemon after: %+v err=%v", probe, err)
			}
		})
	}
}

// TestEnsureDaemonRefusesWhileADaemonHoldsJobs (EXAMPLES chapter 57, issue
// 5): a daemon of another build holding jobs is drained and the submission
// refused, the daemon's build over the client's; the daemon stays to finish
// them, a second client is refused alike, and no daemon is launched. A
// daemon of this build already draining is refused the same way.
func TestEnsureDaemonRefusesWhileADaemonHoldsJobs(t *testing.T) {
	own := buildinfo.BuildOrNone()
	for _, tc := range []struct {
		name     string
		build    string
		draining bool
		jobs     int64
	}{
		{"another build", fixtureBuild, false, 2},
		{"this build, draining", own, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, g, socket := daemonTestRuntime(t)
			f := startStopFixtureBuild(t, socket, tc.jobs, tc.build, nil)
			f.draining.Store(tc.draining)
			want := fmt.Sprintf("daemon_draining: %s; none submitted; retry when it exits, or --no-daemon\n  daemon build %s %s\n  client build %s %[3]s", app.JobsRunning(tc.jobs), tc.build, buildinfo.Version, own)
			for range 2 {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				var stderr bytes.Buffer
				_, err := ensureTestDaemon(ctx, g, &stderr)
				cancel()
				if code := errorcodes.ExitAt(err, "daemon_start_failed"); err == nil || code != exitcode.ExitJobRejected {
					t.Fatalf("code=%d err=%v", code, err)
				}
				if got := errorcodes.Message(err); got != want {
					t.Fatalf("refusal=%q, want %q", got, want)
				}
				if stderr.Len() != 0 {
					t.Fatalf("stderr=%q", stderr.String())
				}
			}
			if ops := f.operations(); ops != "drain,drain" {
				t.Fatalf("operations %q", ops)
			}
			if !f.draining.Load() {
				t.Fatal("the daemon was not drained")
			}
			if _, statErr := os.Stat(filepath.Join(base, "logs", "daemon.log")); !os.IsNotExist(statErr) {
				t.Fatalf("a daemon was launched beside it; daemon.log stat=%v", statErr)
			}
		})
	}
}

// TestEnsureDaemonUsesItsOwnBuild: a daemon of this build, not draining, is
// used as it is, jobs or none, nothing drained and nothing said.
func TestEnsureDaemonUsesItsOwnBuild(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	f := startStopFixture(t, socket, 1, nil)
	var stderr bytes.Buffer
	if _, err := ensureTestDaemon(context.Background(), g, &stderr); err != nil || stderr.Len() != 0 {
		t.Fatalf("err=%v stderr=%q", err, stderr.String())
	}
	if ops := f.operations(); ops != "" {
		t.Fatalf("operations %q", ops)
	}
}

// TestEnsureDaemonRefusesADrainedDaemonThatStays: a drained daemon holding no
// job that has not exited within daemon.start-timeout refuses the
// submission, naming karvi daemon stop; none is launched beside it.
func TestEnsureDaemonRefusesADrainedDaemonThatStays(t *testing.T) {
	base, g, socket := daemonTestRuntime(t)
	g.sets = append(g.sets, `daemon.start-timeout="1s"`)
	f := startStopFixtureBuild(t, socket, 0, fixtureBuild, nil)
	f.stay.Store(true)
	start := time.Now()
	_, err := ensureTestDaemon(context.Background(), g, &bytes.Buffer{})
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("refused after %s, before the wait", elapsed)
	}
	want := fmt.Sprintf("daemon_draining: no job running, not exited after 1s; none submitted; karvi daemon stop, or --no-daemon\n  daemon build %s %s\n  client build %s %[2]s", fixtureBuild, buildinfo.Version, buildinfo.BuildOrNone())
	if got := errorcodes.Message(err); got != want || errorcodes.ExitAt(err, "daemon_start_failed") != exitcode.ExitJobRejected {
		t.Fatalf("refusal=%q, want %q", got, want)
	}
	if _, statErr := os.Stat(filepath.Join(base, "logs", "daemon.log")); !os.IsNotExist(statErr) {
		t.Fatalf("a daemon was launched beside it; daemon.log stat=%v", statErr)
	}
}

// TestEnsureDaemonRefusesANewerSchema: a daemon whose IPC schema is newer
// than the client's, which the client cannot speak, is refused, its schema
// over the client's, naming karvi daemon stop from the newer karvi; it is
// neither drained nor replaced.
func TestEnsureDaemonRefusesANewerSchema(t *testing.T) {
	base, g, socket := daemonTestRuntime(t)
	done := startCLILifecycleFixture(t, socket, ipc.SchemaVersion+1, "0.30.0", 0)
	_, err := ensureTestDaemon(context.Background(), g, &bytes.Buffer{})
	want := fmt.Sprintf("daemon_incompatible: the daemon speaks a newer IPC schema; none submitted; karvi daemon stop from the newer karvi, or --no-daemon\n  daemon ipc_schema %d\n  client ipc_schema %d", ipc.SchemaVersion+1, ipc.SchemaVersion)
	if got := errorcodes.Message(err); got != want || errorcodes.ExitAt(err, "daemon_start_failed") != exitcode.ExitJobRejected {
		t.Fatalf("refusal=%q, want %q", got, want)
	}
	if _, statErr := os.Stat(filepath.Join(base, "logs", "daemon.log")); !os.IsNotExist(statErr) {
		t.Fatalf("a daemon was launched beside it; daemon.log stat=%v", statErr)
	}
	select {
	case <-done:
		t.Fatal("the newer daemon was stopped")
	default:
	}
}

// TestFollowAndCancelAcrossBuilds (EXAMPLES chapter 57, issue 6): job follow
// and job cancel use a daemon of another build at the client's IPC schema;
// one of another schema, older or newer, is refused, daemon_incompatible,
// naming the karvi that speaks it, the daemon judged before the directory,
// so that a job not ended is never job_orphaned; a finished job still
// renders from its directory.
func TestFollowAndCancelAcrossBuilds(t *testing.T) {
	verb := func(g globalOptions, args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		got := Main(append(append([]string{}, globalArgs(g)...), args...), strings.NewReader(""), &stdout, &stderr)
		return got, stdout.String(), stderr.String()
	}
	t.Run("another build", func(t *testing.T) {
		_, g, socket := daemonTestRuntime(t)
		f := startStopFixtureBuild(t, socket, 1, fixtureBuild, nil)
		if got, _, stderr := verb(g, "job", "follow", fixtureJobID); got != exitcode.ExitCancelled {
			t.Fatalf("follow: exit=%d stderr=%q", got, stderr)
		}
		if got, stdout, stderr := verb(g, "job", "cancel", fixtureJobID, "--reason", "r"); got != 0 || !strings.HasPrefix(stdout, "cancel requested: job "+fixtureJobID) {
			t.Fatalf("cancel: exit=%d stdout=%q stderr=%q", got, stdout, stderr)
		}
		if ops := f.operations(); ops != "follow,cancel:r" {
			t.Fatalf("operations %q", ops)
		}
	})
	for _, tc := range []struct {
		name    string
		schema  int
		version string
		speaker string
	}{
		{"an older schema", 2, "0.8.0", "an older IPC schema; %s its jobs with karvi 0.8.0"},
		{"a newer schema", ipc.SchemaVersion + 1, "0.30.0", "a newer IPC schema; %s its jobs with the newer karvi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, g, socket := daemonTestRuntime(t)
			startCLILifecycleFixture(t, socket, tc.schema, tc.version, 1)
			refusal := func(v string) string {
				return fmt.Sprintf("daemon_incompatible: the daemon speaks "+tc.speaker+"\n  daemon ipc_schema %d\n  client ipc_schema %d\n", v, tc.schema, ipc.SchemaVersion)
			}
			if got, _, stderr := verb(g, "job", "cancel", fixtureJobID); got != exitcode.ExitJobRejected || stderr != refusal("cancel") {
				t.Fatalf("cancel: exit=%d stderr=%q, want %q", got, stderr, refusal("cancel"))
			}
			// No directory, then one without a summary: the job may be that
			// daemon's.
			if got, _, stderr := verb(g, "job", "follow", fixtureJobID); got != exitcode.ExitJobRejected || stderr != refusal("follow") {
				t.Fatalf("follow, no directory: exit=%d stderr=%q", got, stderr)
			}
			common, _ := testCommon(g)
			dir, _ := app.JobDirectoryFor(common, fixtureJobID)
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatal(err)
			}
			if got, _, stderr := verb(g, "job", "follow", fixtureJobID); got != exitcode.ExitJobRejected || stderr != refusal("follow") {
				t.Fatalf("follow, not ended: exit=%d stderr=%q", got, stderr)
			}
			finishedDirectory(t, g, fixtureJobID, records.Summary{FinalStatus: "completed", ExitCode: 0, ExitName: "ExitSuccess"})
			if got, _, stderr := verb(g, "job", "follow", fixtureJobID); got != 0 || stderr != "" {
				t.Fatalf("follow, ended: exit=%d stderr=%q", got, stderr)
			}
		})
	}
}

// TestDaemonStatusStacksAnotherBuild covers the status beside a daemon of
// another build (EXAMPLES chapter 57, issues 1 and 2): each pair stacked,
// the daemon's over the client's, compatible false, and the remediation.
func TestDaemonStatusStacksAnotherBuild(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	startCLILifecycleFixture(t, socket, ipc.SchemaVersion, buildinfo.Version, 0)
	var stdout, stderr bytes.Buffer
	args := []string{"--set", fmt.Sprintf("basedir=%q", base), "daemon", "status"}
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("status exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	want := fmt.Sprintf("daemon_version: %[1]s\nclient_version: %[1]s\ndaemon_commit: %[2]s\nclient_commit: %[2]s\ndaemon_ipc_schema: %[3]d\nclient_ipc_schema: %[3]d\ndaemon_build: %[4]s\nclient_build: %[5]s\ncompatible: false\nremediation: karvi daemon restart\nstarted_at: ", buildinfo.Version, buildinfo.Commit, ipc.SchemaVersion, fixtureBuild, buildinfo.BuildOrNone())
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout=%q missing %q", stdout.String(), want)
	}
}

func TestDaemonStatusReportsOlderSchemaAndRemediation(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	startCLILifecycleFixture(t, socket, 2, "0.8.0", 0)
	var stdout, stderr bytes.Buffer
	args := []string{"--set", fmt.Sprintf("basedir=%q", base), "daemon", "status"}
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("status exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	for _, want := range []string{"daemon_version: 0.8.0\n", "daemon_ipc_schema: 2\n", "daemon_build:\n", "compatible: false\n", "remediation: karvi daemon restart\n"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout=%q missing %q", stdout.String(), want)
		}
	}
}

// TestDaemonStopReachesAnOlderSchema: daemon stop reaches a daemon of an
// older IPC schema at its own and names both schemas.
func TestDaemonStopReachesAnOlderSchema(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	done := startCLILifecycleFixture(t, socket, 2, "0.8.0", 0)
	var stdout, stderr bytes.Buffer
	args := []string{"--set", fmt.Sprintf("basedir=%q", base), "daemon", "stop"}
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("daemon stop exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "ipc_schema=2") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("older daemon did not stop")
	}
}

func TestDaemonRestartRefusesActiveJobsWithoutForce(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	startCLILifecycleFixture(t, socket, 2, "0.8.0", 2)
	var stdout, stderr bytes.Buffer
	args := []string{"--set", fmt.Sprintf("basedir=%q", base), "daemon", "restart"}
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitJobRejected {
		t.Fatalf("restart exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "daemon_active_jobs") || !strings.Contains(stderr.String(), "2 active job") {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("active daemon socket was removed: %v", err)
	}
}

func daemonTestRuntime(t *testing.T) (string, globalOptions, string) {
	t.Helper()
	tmp := testsocket.Dir(t)
	home := filepath.Join(tmp, "home")
	base := filepath.Join(tmp, "state")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	g := globalOptions{sets: []string{fmt.Sprintf("basedir=%q", base)}}
	rt, err := testRuntime(g)
	if err != nil {
		t.Fatalf("ResolveDaemonRuntime err=%v", err)
	}
	return base, g, rt.Socket
}

// fixtureBuild is the build the lifecycle fixture names at this schema: a
// build of another program (EXAMPLES chapter 57, issue 2's example).
const fixtureBuild = "9Au3WHU839F5xE-67H93"

// startCLILifecycleFixture is a lifecycle-only daemon at schema, naming
// version, and holding activeJobs: it answers ping and status, a drain, and
// a stop, at its own schema alone. Drained, it exits holding no job, as a
// daemon does (EXAMPLES chapter 57, issue 4); stopped, it exits. The test's
// end closes it.
func startCLILifecycleFixture(t *testing.T, socket string, schema int, version string, activeJobs int64) <-chan struct{} {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer os.Remove(socket)
		defer listener.Close()
		state := "running"
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var req ipc.Request
			if err := ipc.Read(conn, &req, 1<<20); err != nil {
				conn.Close()
				continue
			}
			if req.IPCSchemaVersion != schema {
				_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Error: &ipc.Error{Code: "ipc_schema_incompatible", Message: fmt.Sprintf("client schema %d, daemon schema %d", req.IPCSchemaVersion, schema), Details: map[string]any{}}}, 1<<20)
				conn.Close()
				continue
			}
			if req.Operation == "stop" {
				// A stop is refused while jobs run unless it names force, as
				// a daemon does: the daemon alone counts its jobs (EXAMPLES
				// chapter 57, issue 10).
				var stop daemon.StopRequest
				_ = json.Unmarshal(req.Payload, &stop)
				if stop.Mode != daemon.StopForce && activeJobs > 0 {
					_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Error: &ipc.Error{Code: "daemon_active_jobs", Message: fmt.Sprintf("daemon reports %d active job(s)", activeJobs), Details: map[string]any{}}}, 1<<20)
					conn.Close()
					continue
				}
				raw, _ := json.Marshal(map[string]any{"stopping": true})
				_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: raw}, 1<<20)
				conn.Close()
				return
			}
			if req.Operation == "drain" {
				state = "draining"
			}
			status := daemon.Status{IPCSchemaVersion: schema, Status: state, PID: os.Getpid(), UID: os.Geteuid(), Socket: socket, StartedAt: time.Now(), ActiveJobs: activeJobs, Version: version, Commit: buildinfo.Commit}
			if schema == ipc.SchemaVersion {
				// A daemon of this schema names its build, never the test's;
				// an older one names none.
				status.Build = fixtureBuild
			}
			raw, _ := json.Marshal(status)
			_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: raw}, 1<<20)
			conn.Close()
			if req.Operation == "drain" && activeJobs == 0 {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	return done
}
