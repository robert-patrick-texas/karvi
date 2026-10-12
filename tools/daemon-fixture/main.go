// Command daemon-fixture provides a lifecycle-only daemon of another build or
// an older IPC schema for the upgrade tests: it answers ping, status, drain,
// and stop. It is a development tool and is not installed as a karvi operator
// executable.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
)

func main() {
	socket := flag.String("socket", "", "Unix socket path")
	schema := flag.Int("schema", 2, "emulated IPC schema")
	version := flag.String("version", "0.8.0", "emulated daemon version")
	build := flag.String("build", "", "emulated daemon build (none by default, as a release before builds)")
	jobs := flag.Int64("jobs", 0, "emulated active jobs: drained, a fixture holding any stays, answering draining")
	flag.Parse()
	if *socket == "" || *schema <= 0 {
		fmt.Fprintln(os.Stderr, "--socket and a positive --schema are required")
		os.Exit(2)
	}
	if err := os.MkdirAll(filepath.Dir(*socket), 0700); err != nil {
		fatal(err)
	}
	_ = os.Remove(*socket)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: *socket, Net: "unix"})
	if err != nil {
		fatal(err)
	}
	defer listener.Close()
	defer os.Remove(*socket)
	if err := os.Chmod(*socket, 0600); err != nil {
		fatal(err)
	}
	f := &fixture{socket: *socket, schema: *schema, version: *version, build: *build, jobs: *jobs}
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			fatal(err)
		}
		stop := f.handle(conn)
		_ = conn.Close()
		if stop {
			return
		}
	}
}

// fixture is the emulated daemon: its schema, version, build, and jobs, and
// whether it has been drained.
type fixture struct {
	socket, version, build string
	schema                 int
	jobs                   int64
	draining               bool
}

func (f *fixture) handle(conn *net.UnixConn) bool {
	socket, schema := f.socket, f.schema
	uid, err := ipc.PeerUID(conn)
	if err != nil || int(uid) != os.Geteuid() {
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, Error: &ipc.Error{Code: "peer_uid_denied", Message: "same UID required", Details: map[string]any{}}}, 1<<20)
		return false
	}
	var req ipc.Request
	if err := ipc.Read(conn, &req, 1<<20); err != nil {
		return false
	}
	if req.IPCSchemaVersion != schema {
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Error: &ipc.Error{Code: "ipc_schema_incompatible", Message: fmt.Sprintf("client schema %d, daemon schema %d", req.IPCSchemaVersion, schema), Details: map[string]any{}}}, 1<<20)
		return false
	}
	status := func() json.RawMessage {
		state := "running"
		if f.draining {
			state = "draining"
		}
		raw, _ := json.Marshal(daemon.Status{IPCSchemaVersion: schema, Status: state, PID: os.Getpid(), UID: os.Geteuid(), Socket: socket, StartedAt: time.Now(), ActiveJobs: f.jobs, AcceptedJobs: f.jobs, Version: f.version, Build: f.build})
		return raw
	}
	switch req.Operation {
	case "ping", "status":
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: status()}, 1<<20)
		return false
	case "drain":
		// A drained daemon answers draining and exits holding no job; one
		// holding jobs stays to finish them (EXAMPLES chapter 57, issue 4).
		f.draining = true
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: status()}, 1<<20)
		return f.jobs == 0
	case "stop":
		// The daemon alone counts its jobs: a stop that does not name force
		// is refused while any run (EXAMPLES chapter 57, issue 10).
		var stop daemon.StopRequest
		_ = json.Unmarshal(req.Payload, &stop)
		if stop.Mode != daemon.StopForce && f.jobs > 0 {
			_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Error: &ipc.Error{Code: "daemon_active_jobs", Message: fmt.Sprintf("daemon reports %d active job(s)", f.jobs), Details: map[string]any{}}}, 1<<20)
			return false
		}
		raw, _ := json.Marshal(map[string]any{"stopping": true})
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: raw}, 1<<20)
		return true
	default:
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Error: &ipc.Error{Code: "ipc_operation_unknown", Message: req.Operation, Details: map[string]any{}}}, 1<<20)
		return false
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
