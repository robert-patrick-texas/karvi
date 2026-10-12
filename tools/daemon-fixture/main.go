// Command daemon-fixture provides a protocol-oriented older-daemon process for
// release upgrade tests. It is a development tool and is not installed as a
// karvi operator executable.
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
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			fatal(err)
		}
		stop := handle(conn, *socket, *schema, *version, *build)
		_ = conn.Close()
		if stop {
			return
		}
	}
}

func handle(conn *net.UnixConn, socket string, schema int, version, build string) bool {
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
	status := func(state string) json.RawMessage {
		raw, _ := json.Marshal(daemon.Status{IPCSchemaVersion: schema, Status: state, PID: os.Getpid(), UID: os.Geteuid(), Socket: socket, StartedAt: time.Now(), ActiveJobs: 0, AcceptedJobs: 0, Version: version, Build: build})
		return raw
	}
	switch req.Operation {
	case "ping", "status":
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: status("running")}, 1<<20)
		return false
	case "drain":
		// A drained daemon holding no job answers and exits (EXAMPLES
		// chapter 57, issue 4).
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: schema, RequestID: req.RequestID, Result: status("draining")}, 1<<20)
		return true
	case "stop":
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
