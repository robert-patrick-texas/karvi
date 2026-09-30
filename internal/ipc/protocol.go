// Package ipc implements the bounded, newline-delimited JSON protocol used by
// the private per-UID karvi daemon socket.
package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// SchemaVersion is the daemon IPC schema this executable serves and speaks:
// 5 carries prepare_job and
// commit_job and no submit_job; 6 adds follow_job;
// 7 adds cancel_job; 8 carries each record in
// follow_job's stream in place of the notice that located it in
// commands.jsonl; 9 names jobs by the job
// ID `YYMMDD-HHMMSS-xx` in commit_job, follow_job, and cancel_job,
// which each side's validator requires of the other.
const SchemaVersion = 10

// SchemaMismatchError reports the protocol versions observed on each side of
// an IPC exchange. Callers use the structured values to provide safe upgrade
// remediation instead of parsing an error string.
type SchemaMismatchError struct {
	DaemonSchema int
	ClientSchema int
}

// ErrorCode returns the registered error code for a schema mismatch.
func (e *SchemaMismatchError) ErrorCode() string { return "ipc_schema_incompatible" }

func (e *SchemaMismatchError) Error() string {
	return fmt.Sprintf("daemon IPC schema %d is incompatible with client schema %d", e.DaemonSchema, e.ClientSchema)
}

type Request struct {
	IPCSchemaVersion int             `json:"ipc_schema_version"`
	RequestID        string          `json:"request_id"`
	Operation        string          `json:"operation"`
	ClientPID        int             `json:"client_pid"`
	ClientVersion    string          `json:"client_version"`
	IdempotencyKey   string          `json:"idempotency_key,omitempty"`
	Payload          json.RawMessage `json:"payload"`
}

type Error struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details"`
}

type Response struct {
	IPCSchemaVersion int             `json:"ipc_schema_version"`
	RequestID        string          `json:"request_id"`
	Result           json.RawMessage `json:"result,omitempty"`
	Error            *Error          `json:"error,omitempty"`
	// Event names a streamed response of follow_job (schema 6):
	// record or stream_terminal (schema 8); empty on a plain result.
	Event string `json:"event,omitempty"`
}

func NewRequest(id, op, version string, payload any) (Request, error) {
	return NewRequestForSchema(SchemaVersion, id, op, version, payload)
}

// NewRequestForSchema creates a request using an explicitly selected wire
// schema. It exists only for the stable ping/status/stop lifecycle envelope so
// a newly installed client can inspect and explicitly stop an older same-UID
// daemon. Job submission always uses NewRequest and the current exact schema.
func NewRequestForSchema(schema int, id, op, version string, payload any) (Request, error) {
	if schema <= 0 {
		return Request{}, errorcodes.Errorf("ipc_schema_invalid", "invalid IPC schema %d", schema)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Request{}, err
	}
	return Request{IPCSchemaVersion: schema, RequestID: id, Operation: op, ClientPID: os.Getpid(), ClientVersion: version, IdempotencyKey: id, Payload: raw}, nil
}

func Call(ctx context.Context, socket string, req Request, maxBytes int64, out any) error {
	return CallForSchema(ctx, socket, req, SchemaVersion, maxBytes, out)
}

// CallForSchema performs one IPC exchange and validates the response against
// expectedSchema. It is intentionally separate from Call so lifecycle recovery
// can speak a known older envelope without weakening exact-schema job calls.
func CallForSchema(ctx context.Context, socket string, req Request, expectedSchema int, maxBytes int64, out any) error {
	if expectedSchema <= 0 {
		return errorcodes.Errorf("ipc_schema_invalid", "invalid expected IPC schema %d", expectedSchema)
	}
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := Write(conn, req, maxBytes); err != nil {
		return err
	}
	var resp Response
	if err := Read(conn, &resp, maxBytes); err != nil {
		return err
	}
	if resp.IPCSchemaVersion != expectedSchema {
		return &SchemaMismatchError{DaemonSchema: resp.IPCSchemaVersion, ClientSchema: expectedSchema}
	}
	if resp.RequestID != req.RequestID {
		return errorcodes.Errorf("ipc_request_id_mismatch", "daemon response request ID mismatch")
	}
	if (len(resp.Result) == 0) == (resp.Error == nil) {
		return errorcodes.Errorf("ipc_response_ambiguous", "daemon response must contain exactly one of result or error")
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}
	if out != nil {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// Stream performs one exchange whose answer is a sequence of responses on
// the connection (follow_job): it writes req, then
// reads response lines through one buffered reader until fn returns
// io.EOF (the stream's terminal), an error response arrives, or the
// connection ends. Read cannot serve a stream: it buffers past the line it
// returns, so the lines behind it would be lost.
func Stream(ctx context.Context, socket string, req Request, maxBytes int64, fn func(Response) error) error {
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()
	if err := Write(conn, req, maxBytes); err != nil {
		return err
	}
	lines := NewLineReader(conn, maxBytes)
	for {
		var resp Response
		if err := lines.Next(&resp); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if resp.IPCSchemaVersion != SchemaVersion {
			return &SchemaMismatchError{DaemonSchema: resp.IPCSchemaVersion, ClientSchema: SchemaVersion}
		}
		if resp.RequestID != req.RequestID {
			return errorcodes.Errorf("ipc_request_id_mismatch", "daemon response request ID mismatch")
		}
		if (len(resp.Result) == 0) == (resp.Error == nil) {
			return errorcodes.Errorf("ipc_response_ambiguous", "daemon response must contain exactly one of result or error")
		}
		if resp.Error != nil {
			return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
		}
		if err := fn(resp); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// LineReader reads newline-delimited envelopes from one connection with a
// buffer that persists across calls, which a stream needs.
type LineReader struct {
	br  *bufio.Reader
	max int64
}

// NewLineReader wraps r; each line is bounded by maxBytes.
func NewLineReader(r io.Reader, maxBytes int64) *LineReader {
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	return &LineReader{br: bufio.NewReaderSize(r, 64<<10), max: maxBytes}
}

// Next decodes the next line into dst; io.ErrUnexpectedEOF when the
// connection ended before a line.
func (l *LineReader) Next(dst any) error {
	line, err := l.br.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return err
	}
	if int64(len(line)) > l.max {
		return fmt.Errorf("ipc_frame_too_large: maximum %d bytes", l.max)
	}
	if len(line) == 0 {
		return io.ErrUnexpectedEOF
	}
	if line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if !json.Valid(line) {
		return fmt.Errorf("ipc_invalid_json")
	}
	return json.Unmarshal(line, dst)
}

func Read(r io.Reader, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	br := bufio.NewReader(io.LimitReader(r, maxBytes+1))
	line, err := br.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return err
	}
	if int64(len(line)) > maxBytes {
		return fmt.Errorf("ipc_frame_too_large: maximum %d bytes", maxBytes)
	}
	if len(line) == 0 {
		return io.ErrUnexpectedEOF
	}
	if line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if !json.Valid(line) {
		return fmt.Errorf("ipc_invalid_json")
	}
	return json.Unmarshal(line, dst)
}

func Write(w io.Writer, value any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if int64(len(data)+1) > maxBytes {
		return fmt.Errorf("ipc_frame_too_large: maximum %d bytes", maxBytes)
	}
	data = append(data, '\n')
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

// PeerUID reads Linux SO_PEERCRED from a Unix-domain connection. The daemon
// rejects every client whose effective UID differs from its own.
func PeerUID(conn *net.UnixConn) (uint32, error) {
	cred, err := peerCred(conn)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}

// PeerPID is the connecting process's PID from SO_PEERCRED, recorded with
// a cancel_job request.
func PeerPID(conn *net.UnixConn) (int32, error) {
	cred, err := peerCred(conn)
	if err != nil {
		return 0, err
	}
	return cred.Pid, nil
}

func peerCred(conn *net.UnixConn) (*syscall.Ucred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cred *syscall.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		cred, controlErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	if controlErr != nil {
		return nil, controlErr
	}
	if cred == nil {
		return nil, errorcodes.Errorf("ipc_peer_credentials_missing", "SO_PEERCRED returned no credential")
	}
	return cred, nil
}

func Wait(ctx context.Context, socket string) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		dctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		conn, err := (&net.Dialer{}).DialContext(dctx, "unix", socket)
		cancel()
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
