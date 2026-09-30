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
		stop := handle(conn, *socket, *schema, *version)
		_ = conn.Close()
		if stop {
			return
		}
	}
}

func handle(conn *net.UnixConn, socket string, schema int, version string) bool {
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
	switch req.Operation {
	case "ping", "status":
		status := daemon.Status{IPCSchemaVersion: schema, Status: "running", PID: os.Getpid(), UID: os.Geteuid(), Socket: socket, StartedAt: time.Now(), ActiveJobs: 0, AcceptedJobs: 0, Version: version}
		raw, _ := json.Marshal(status)
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

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
