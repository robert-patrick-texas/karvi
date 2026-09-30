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
	_, err := ensureDaemon(ctx, g, &bytes.Buffer{})
	if code := errorcodes.ExitAt(err, "daemon_start_failed"); err == nil || code != exitcode.ExitJobRejected {
		t.Fatalf("code=%d err=%v", code, err)
	}
	for _, want := range []string{"daemon_incompatible", "version 0.8.0", "IPC schema 2", "both must match", "karvi daemon restart", "no job was submitted"} {
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

// TestEnsureDaemonRejectsSameSchemaOtherVersion covers the version-and-schema
// pair rule on the run path and the status line: a daemon
// of another release at this executable's IPC schema is incompatible, the
// message names both versions and both schemas, no replacement is launched,
// and status says so with the remediation.
func TestEnsureDaemonRejectsSameSchemaOtherVersion(t *testing.T) {
	base, g, socket := daemonTestRuntime(t)
	fixtureDone := startCLILifecycleFixture(t, socket, ipc.SchemaVersion, "0.19.0", 0)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = daemon.StopCompatible(ctx, socket, 1<<20, daemon.StopForce)
		<-fixtureDone
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := ensureDaemon(ctx, g, &bytes.Buffer{})
	if code := errorcodes.ExitAt(err, "daemon_start_failed"); err == nil || code != exitcode.ExitJobRejected {
		t.Fatalf("code=%d err=%v", code, err)
	}
	for _, want := range []string{"daemon_incompatible: running daemon version 0.19.0", fmt.Sprintf("IPC schema %d; karvi %s uses schema %d, and both must match", ipc.SchemaVersion, daemon.ClientVersion, ipc.SchemaVersion), "karvi daemon restart", "no job was submitted"} {
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
	for _, want := range []string{"version: 0.19.0", fmt.Sprintf("daemon_ipc_schema: %d", ipc.SchemaVersion), fmt.Sprintf("client_ipc_schema: %d", ipc.SchemaVersion), "compatible: false", "remediation: karvi daemon restart"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout=%q missing %q", stdout.String(), want)
		}
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
	for _, want := range []string{"version: 0.8.0", "daemon_ipc_schema: 2", "compatible: false", "remediation: karvi daemon restart"} {
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
	rt, err := app.ResolveDaemonRuntime(g.common())
	if err != nil {
		t.Fatalf("ResolveDaemonRuntime err=%v", err)
	}
	return base, g, rt.Socket
}

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
			status := daemon.Status{IPCSchemaVersion: schema, Status: "running", PID: os.Getpid(), UID: os.Geteuid(), Socket: socket, StartedAt: time.Now(), ActiveJobs: activeJobs, Version: version}
			raw, _ := json.Marshal(status)
			_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: raw}, 1<<20)
			conn.Close()
		}
	}()
	return done
}
