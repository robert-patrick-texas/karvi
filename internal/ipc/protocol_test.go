package ipc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

func TestSchemaVersionMatchesBuildInformation(t *testing.T) {
	if ipc.SchemaVersion != buildinfo.DaemonIPCSchema {
		t.Fatalf("ipc schema=%d buildinfo schema=%d", ipc.SchemaVersion, buildinfo.DaemonIPCSchema)
	}
}

func TestCallReturnsStructuredSchemaMismatch(t *testing.T) {
	t.Helper()
	socket := filepath.Join(testsocket.Dir(t), "daemon.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		var req ipc.Request
		if err := ipc.Read(conn, &req, 1<<20); err != nil {
			serverErr <- err
			return
		}
		serverErr <- ipc.Write(conn, ipc.Response{
			IPCSchemaVersion: 2,
			RequestID:        req.RequestID,
			Error: &ipc.Error{
				Code:    "ipc_schema_incompatible",
				Message: "client schema 4, daemon schema 2",
				Details: map[string]any{},
			},
		}, 1<<20)
	}()

	req, err := ipc.NewRequest("request-2", "ping", buildinfo.Version, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = ipc.Call(ctx, socket, req, 1<<20, &map[string]any{})
	var mismatch *ipc.SchemaMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error=%v, want SchemaMismatchError", err)
	}
	if mismatch.DaemonSchema != 2 || mismatch.ClientSchema != ipc.SchemaVersion {
		t.Fatalf("mismatch=%+v", mismatch)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestCallForSchemaAcceptsReleasedLifecycleEnvelope(t *testing.T) {
	socket := filepath.Join(testsocket.Dir(t), "daemon.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		var req ipc.Request
		if err := ipc.Read(conn, &req, 1<<20); err != nil {
			serverErr <- err
			return
		}
		if req.IPCSchemaVersion != 2 {
			serverErr <- fmt.Errorf("request schema=%d", req.IPCSchemaVersion)
			return
		}
		raw, err := json.Marshal(map[string]any{"status": "running"})
		if err != nil {
			serverErr <- err
			return
		}
		serverErr <- ipc.Write(conn, ipc.Response{IPCSchemaVersion: 2, RequestID: req.RequestID, Result: raw}, 1<<20)
	}()

	req, err := ipc.NewRequestForSchema(2, "request-3", "ping", buildinfo.Version, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var got map[string]any
	if err := ipc.CallForSchema(ctx, socket, req, 2, 1<<20, &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "running" {
		t.Fatalf("result=%v", got)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}
