package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

func TestProbeAndStopCompatibleOlderDaemon(t *testing.T) {
	socket := filepath.Join(testsocket.Dir(t), "daemon.sock")
	fixture := startLifecycleFixture(t, socket, 2, "0.8.0")
	defer fixture.close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	probe, err := Probe(ctx, socket, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Compatible || probe.DaemonSchema != 2 || probe.ClientSchema != ipc.SchemaVersion {
		t.Fatalf("probe=%+v", probe)
	}
	if probe.Status.Version != "0.8.0" || probe.Status.ActiveJobs != 0 {
		t.Fatalf("status=%+v", probe.Status)
	}

	stopped, err := StopCompatible(ctx, socket, 1<<20, StopForce)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Compatible || stopped.DaemonSchema != 2 {
		t.Fatalf("stop probe=%+v", stopped)
	}
	select {
	case <-fixture.done:
	case <-ctx.Done():
		t.Fatal("older daemon fixture did not stop")
	}
}

// TestProbeSameSchemaOtherVersionIsIncompatible covers the pair rule: a
// daemon at this executable's IPC schema but
// another version is reached, reported, and incompatible; the same fixture
// at this executable's version is compatible.
func TestProbeSameSchemaOtherVersionIsIncompatible(t *testing.T) {
	for _, tc := range []struct {
		version    string
		compatible bool
	}{{"0.19.0", false}, {ClientVersion, true}} {
		socket := filepath.Join(testsocket.Dir(t), "daemon.sock")
		fixture := startLifecycleFixture(t, socket, ipc.SchemaVersion, tc.version)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		probe, err := Probe(ctx, socket, 1<<20)
		cancel()
		fixture.close()
		if err != nil {
			t.Fatal(err)
		}
		if probe.Compatible != tc.compatible || probe.DaemonSchema != ipc.SchemaVersion || probe.ClientSchema != ipc.SchemaVersion || probe.Status.Version != tc.version {
			t.Fatalf("version %s: probe=%+v", tc.version, probe)
		}
	}
}

func TestProbeRefusesUnknownNewerLifecycleSchema(t *testing.T) {
	socket := filepath.Join(testsocket.Dir(t), "daemon.sock")
	fixture := startLifecycleFixture(t, socket, ipc.SchemaVersion+1, "future")
	defer fixture.close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Probe(ctx, socket, 1<<20); err == nil {
		t.Fatal("expected unsupported newer schema error")
	}
}

type lifecycleFixture struct {
	listener net.Listener
	done     chan struct{}
	once     sync.Once
}

func startLifecycleFixture(t *testing.T, socket string, schema int, version string) *lifecycleFixture {
	t.Helper()
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	f := &lifecycleFixture{listener: listener, done: make(chan struct{})}
	go func() {
		defer f.finish(socket)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			stop := f.handle(conn, schema, version, socket)
			_ = conn.Close()
			if stop {
				_ = listener.Close()
				return
			}
		}
	}()
	return f
}

func (f *lifecycleFixture) handle(conn net.Conn, schema int, version, socket string) bool {
	var req ipc.Request
	if err := ipc.Read(conn, &req, 1<<20); err != nil {
		return false
	}
	if req.IPCSchemaVersion != schema {
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Error: &ipc.Error{Code: "ipc_schema_incompatible", Message: fmt.Sprintf("client schema %d, daemon schema %d", req.IPCSchemaVersion, schema), Details: map[string]any{}}}, 1<<20)
		return false
	}
	switch req.Operation {
	case "ping", "status":
		raw, _ := json.Marshal(Status{IPCSchemaVersion: schema, Status: "running", PID: os.Getpid(), UID: os.Geteuid(), Socket: socket, StartedAt: time.Now(), ActiveJobs: 0, AcceptedJobs: 7, Version: version})
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: raw}, 1<<20)
		return false
	case "stop":
		raw, _ := json.Marshal(map[string]any{"stopping": true})
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: raw}, 1<<20)
		return true
	default:
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Error: &ipc.Error{Code: "ipc_operation_unknown", Message: req.Operation, Details: map[string]any{}}}, 1<<20)
		return false
	}
}

func (f *lifecycleFixture) close() {
	f.once.Do(func() {
		_ = f.listener.Close()
	})
	<-f.done
}

func (f *lifecycleFixture) finish(socket string) {
	_ = os.Remove(socket)
	f.once.Do(func() {})
	close(f.done)
}
