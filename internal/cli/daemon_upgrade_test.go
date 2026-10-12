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

	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

func TestEnsureDaemonRejectsOlderDaemonWithoutSpawningReplacement(t *testing.T) {
	base, g, socket := daemonTestRuntime(t)
	fixtureDone := startCLILifecycleFixture(t, socket, 2, "0.8.0", 0)
	defer func() {
		select {
		case <-fixtureDone:
		default:
			_ = os.Remove(socket)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := ensureTestDaemon(ctx, g, &bytes.Buffer{})
	if code := errorcodes.ExitAt(err, "daemon_start_failed"); err == nil || code != exitcode.ExitJobRejected {
		t.Fatalf("code=%d err=%v", code, err)
	}
	for _, want := range []string{"daemon_incompatible", "version 0.8.0", "IPC schema 2", "must both match", "karvi daemon restart", "no job was submitted"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error=%q missing %q", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(base, "logs", "daemon.log")); !os.IsNotExist(statErr) {
		t.Fatalf("ensureDaemon attempted to spawn a replacement; daemon.log stat=%v", statErr)
	}

	// Explicit lifecycle recovery remains available through the new client even
	// though normal job submission is exact-schema only.
	var stdout, stderr bytes.Buffer
	args := []string{"--set", fmt.Sprintf("basedir=%q", base), "daemon", "stop"}
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("daemon stop exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "ipc_schema=2") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	select {
	case <-fixtureDone:
	case <-time.After(time.Second):
		t.Fatal("older daemon did not stop")
	}
}

// TestEnsureDaemonRejectsSameSchemaOtherBuild covers the build rule on the
// run path and the status (EXAMPLES chapter 57, issues 1 and 2): a daemon of
// another build at this executable's IPC schema and version is incompatible,
// the message names both builds, no replacement is launched, and status
// stacks each pair, the daemon's over the client's, with the remediation.
func TestEnsureDaemonRejectsSameSchemaOtherBuild(t *testing.T) {
	base, g, socket := daemonTestRuntime(t)
	fixtureDone := startCLILifecycleFixture(t, socket, ipc.SchemaVersion, buildinfo.Version, 0)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = daemon.StopCompatible(ctx, socket, 1<<20, daemon.StopForce)
		<-fixtureDone
	}()
	own := buildinfo.BuildOrNone()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := ensureTestDaemon(ctx, g, &bytes.Buffer{})
	if code := errorcodes.ExitAt(err, "daemon_start_failed"); err == nil || code != exitcode.ExitJobRejected {
		t.Fatalf("code=%d err=%v", code, err)
	}
	for _, want := range []string{fmt.Sprintf("daemon_incompatible: running daemon version %s build %s", buildinfo.Version, fixtureBuild), fmt.Sprintf("IPC schema %d; karvi %s build %s uses schema %d, and the schema and the build must both match", ipc.SchemaVersion, buildinfo.Version, own, ipc.SchemaVersion), "karvi daemon restart", "no job was submitted"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error=%q missing %q", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(base, "logs", "daemon.log")); !os.IsNotExist(statErr) {
		t.Fatalf("ensureDaemon attempted to spawn a replacement; daemon.log stat=%v", statErr)
	}

	var stdout, stderr bytes.Buffer
	args := []string{"--set", fmt.Sprintf("basedir=%q", base), "daemon", "status"}
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("status exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	want := fmt.Sprintf("daemon_version: %[1]s\nclient_version: %[1]s\ndaemon_commit: %[2]s\nclient_commit: %[2]s\ndaemon_ipc_schema: %[3]d\nclient_ipc_schema: %[3]d\ndaemon_build: %[4]s\nclient_build: %[5]s\ncompatible: false\nremediation: karvi daemon restart\nstarted_at: ", buildinfo.Version, buildinfo.Commit, ipc.SchemaVersion, fixtureBuild, own)
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout=%q missing %q", stdout.String(), want)
	}
}

func TestDaemonStatusReportsOlderSchemaAndRemediation(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	fixtureDone := startCLILifecycleFixture(t, socket, 2, "0.8.0", 0)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = daemon.StopCompatible(ctx, socket, 1<<20, daemon.StopForce)
		<-fixtureDone
	}()

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

func TestDaemonRestartRefusesActiveJobsWithoutForce(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	fixtureDone := startCLILifecycleFixture(t, socket, 2, "0.8.0", 2)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = daemon.StopCompatible(ctx, socket, 1<<20, daemon.StopForce)
		<-fixtureDone
	}()

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
				raw, _ := json.Marshal(map[string]any{"stopping": true})
				_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: raw}, 1<<20)
				conn.Close()
				return
			}
			status := daemon.Status{IPCSchemaVersion: schema, Status: "running", PID: os.Getpid(), UID: os.Geteuid(), Socket: socket, StartedAt: time.Now(), ActiveJobs: activeJobs, Version: version, Commit: buildinfo.Commit}
			if schema == ipc.SchemaVersion {
				// A daemon of this schema names its build, never the test's;
				// an older one names none.
				status.Build = fixtureBuild
			}
			raw, _ := json.Marshal(status)
			_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: raw}, 1<<20)
			conn.Close()
		}
	}()
	return done
}
