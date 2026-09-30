package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/credentialframe"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// The client half of this package holds protocol primitives only:
// lifecycle calls and the schema 6 job calls
// over ipc and credentialframe types. The submission sequence that uses
// them is app.RunViaDaemon, so this package reaches no loader.

// MinLifecycleSchema is the oldest released daemon envelope whose
// ping/status/stop operations are known to be wire-compatible. This narrow
// compatibility is deliberately not used for job submission.
const MinLifecycleSchema = 1

// ProbeResult describes the daemon reached at a socket and whether it can
// accept jobs from this executable. Compatible is the pair's equality: the
// daemon's version and IPC schema both equal this executable's. The IPC
// schema alone
// let the execution plan move under an unchanged schema twice (v0.16.0 and
// v0.19.0), the status saying compatible while the first run was refused at
// prepare_job; the version alone would read a pre-release tree of the
// released version as compatible while its wire had moved. Either
// difference is incompatible, and the lifecycle operations (ping, status,
// stop, restart) keep their reach over every supported schema so the
// operator can see the mismatch and restart.
type ProbeResult struct {
	Status       Status
	DaemonSchema int
	ClientSchema int
	Compatible   bool
}

// ClientVersion is this executable's version, the one a compatible daemon
// reports.
const ClientVersion = buildinfo.Version

func Ping(ctx context.Context, socket string, maxFrame int64) (Status, error) {
	return pingSchema(ctx, socket, maxFrame, ipc.SchemaVersion)
}

// Probe first attempts the current exact schema. When an older released
// lifecycle envelope answers, Probe repeats only the non-mutating ping using
// that daemon's schema so status and version can be reported accurately.
func Probe(ctx context.Context, socket string, maxFrame int64) (ProbeResult, error) {
	status, err := Ping(ctx, socket, maxFrame)
	if err == nil {
		// The schema matched; the version decides (a same-schema daemon of
		// another release, or a released daemon beside a pre-release tree).
		return ProbeResult{Status: status, DaemonSchema: ipc.SchemaVersion, ClientSchema: ipc.SchemaVersion, Compatible: status.Version == ClientVersion}, nil
	}
	var mismatch *ipc.SchemaMismatchError
	if !errors.As(err, &mismatch) {
		return ProbeResult{}, err
	}
	if !lifecycleSchemaSupported(mismatch.DaemonSchema) {
		return ProbeResult{}, mismatch
	}
	status, err = pingSchema(ctx, socket, maxFrame, mismatch.DaemonSchema)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("probe daemon using IPC schema %d: %w", mismatch.DaemonSchema, err)
	}
	return ProbeResult{Status: status, DaemonSchema: mismatch.DaemonSchema, ClientSchema: ipc.SchemaVersion, Compatible: false}, nil
}

// StopCompatible explicitly stops the daemon reached at socket, including an
// older same-UID daemon using a released lifecycle schema. This operation is
// invoked only by an operator's daemon stop/restart command; run submission
// never kills or replaces an incompatible daemon automatically. The mode is
// sent only to a current-schema daemon; an older daemon always stops at once.
func StopCompatible(ctx context.Context, socket string, maxFrame int64, mode string) (ProbeResult, error) {
	probe, err := Probe(ctx, socket, maxFrame)
	if err != nil {
		return ProbeResult{}, err
	}
	id, err := osutil.NewID(now())
	if err != nil {
		return probe, err
	}
	var payload any = map[string]any{}
	if probe.Compatible {
		payload = StopRequest{Mode: mode}
	}
	req, err := ipc.NewRequestForSchema(probe.DaemonSchema, id, "stop", buildinfo.Version, payload)
	if err != nil {
		return probe, err
	}
	var result map[string]any
	if err := ipc.CallForSchema(ctx, socket, req, probe.DaemonSchema, maxFrame, &result); err != nil {
		return probe, err
	}
	return probe, nil
}

// Drain stops a current-schema daemon from admitting new jobs while its active
// jobs continue.
func Drain(ctx context.Context, socket string, maxFrame int64) (Status, error) {
	id, err := osutil.NewID(now())
	if err != nil {
		return Status{}, err
	}
	req, err := ipc.NewRequest(id, "drain", buildinfo.Version, map[string]any{})
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := ipc.Call(ctx, socket, req, maxFrame, &status); err != nil {
		return Status{}, err
	}
	return status, nil
}

func Stop(ctx context.Context, socket string, maxFrame int64) error {
	id, _ := osutil.NewID(now())
	req, err := ipc.NewRequest(id, "stop", buildinfo.Version, map[string]any{})
	if err != nil {
		return err
	}
	var result map[string]any
	return ipc.Call(ctx, socket, req, maxFrame, &result)
}

// PrepareJob sends the draft and header and returns the validated prepare
// result. An unaccepted preparation is returned as
// is; the caller reads its findings.
func PrepareJob(ctx context.Context, socket string, maxFrame int64, pr ipc.PrepareRequest) (ipc.PrepareResult, error) {
	id, err := osutil.NewID(now())
	if err != nil {
		return ipc.PrepareResult{}, err
	}
	req, err := ipc.NewRequest(id, ipc.OpPrepareJob, buildinfo.Version, pr)
	if err != nil {
		return ipc.PrepareResult{}, err
	}
	var result ipc.PrepareResult
	if err := ipc.Call(ctx, socket, req, maxFrame, &result); err != nil {
		return ipc.PrepareResult{}, err
	}
	if err := result.Validate(); err != nil {
		return ipc.PrepareResult{}, err
	}
	if result.Preparation.DraftDigest != pr.Header.PlanDigest {
		return ipc.PrepareResult{}, errorcodes.Errorf("plan_digest_mismatch", "the daemon prepared draft %s, not the %s sent", result.Preparation.DraftDigest, pr.Header.PlanDigest)
	}
	return result, nil
}

// peerUID is the client-side kernel peer check on the credential socket;
// a variable so a test can report a foreign UID.
var peerUID = ipc.PeerUID

// ProvideCredentials sends one credential frame over the channel prepare
// returned: it dials the channel socket, refuses
// any peer UID but its own, sets the connection deadline to the channel
// expiry, writes the frame, and reads the one receipt. The token is one
// use; a retry after any failure needs a new preparation. The protected
// package comes from a credentialpackage.Protector and stays the
// caller's to destroy.
func ProvideCredentials(ctx context.Context, channel ipc.CredentialChannel, preparationID string, protected credentialpackage.Protected) (credentialframe.Receipt, error) {
	d := net.Dialer{}
	raw, err := d.DialContext(ctx, "unix", channel.Socket)
	if err != nil {
		return credentialframe.Receipt{}, errorcodes.Errorf("daemon_submit_failed", "dial credential channel %s: %w", channel.Socket, err)
	}
	conn, ok := raw.(*net.UnixConn)
	if !ok {
		raw.Close()
		return credentialframe.Receipt{}, errorcodes.Errorf("daemon_submit_failed", "credential channel %s is not a Unix socket", channel.Socket)
	}
	defer conn.Close()
	uid, err := peerUID(conn)
	if err != nil || int(uid) != os.Geteuid() {
		return credentialframe.Receipt{}, errorcodes.Errorf("peer_uid_denied", "credential channel peer uid %d is not this process's %d", uid, os.Geteuid())
	}
	_ = conn.SetDeadline(channel.ExpiresAt)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(channel.ExpiresAt) {
		_ = conn.SetDeadline(deadline)
	}
	if err := credentialframe.Write(conn, credentialframe.Header{PreparationID: preparationID, Token: channel.Token, Envelope: protected.Envelope}, protected.Body); err != nil {
		return credentialframe.Receipt{}, err
	}
	return credentialframe.ReadReceipt(conn)
}

// CheckReceipt applies the receipt rule: accepted, naming the
// package sent and its digest; an unaccepted receipt aborts with its first
// finding's code and every finding listed.
func CheckReceipt(receipt credentialframe.Receipt, projection credentialpackage.SafePackageProjection) error {
	if !receipt.Accepted {
		if len(receipt.Findings) == 0 {
			return errorcodes.Errorf("credential_package_invalid", "the daemon refused the package and gave no finding")
		}
		parts := make([]string, 0, len(receipt.Findings))
		for _, f := range receipt.Findings {
			parts = append(parts, fmt.Sprintf("%s (%s)", f.Message, f.Code))
		}
		return errorcodes.Errorf(receipt.Findings[0].Code, "%d finding(s): %s", len(receipt.Findings), strings.Join(parts, "; "))
	}
	if receipt.PackageID != projection.PackageID || receipt.PackageDigest != projection.PackageDigest {
		return errorcodes.Errorf("ipc_result_malformed", "the receipt names package %s digest %s, not %s %s", receipt.PackageID, receipt.PackageDigest, projection.PackageID, projection.PackageDigest)
	}
	return nil
}

// CommitJob sends the final plan under the header's idempotency key and
// returns the validated receipt at acceptance. Callers run it under a
// context without
// cancellation; the commit is short.
func CommitJob(ctx context.Context, socket string, maxFrame int64, cr ipc.CommitRequest) (ipc.CommitResult, error) {
	id, err := osutil.NewID(now())
	if err != nil {
		return ipc.CommitResult{}, err
	}
	req, err := ipc.NewRequest(id, ipc.OpCommitJob, buildinfo.Version, cr)
	if err != nil {
		return ipc.CommitResult{}, err
	}
	req.IdempotencyKey = cr.Header.IdempotencyKey
	var result ipc.CommitResult
	if err := ipc.Call(ctx, socket, req, maxFrame, &result); err != nil {
		return ipc.CommitResult{}, err
	}
	if err := result.Receipt.Validate(&cr); err != nil {
		return ipc.CommitResult{}, err
	}
	return result, nil
}

func pingSchema(ctx context.Context, socket string, maxFrame int64, schema int) (Status, error) {
	id, err := osutil.NewID(now())
	if err != nil {
		return Status{}, err
	}
	req, err := ipc.NewRequestForSchema(schema, id, "ping", buildinfo.Version, map[string]any{})
	if err != nil {
		return Status{}, err
	}
	var raw json.RawMessage
	if err := ipc.CallForSchema(ctx, socket, req, schema, maxFrame, &raw); err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(raw, &status); err != nil {
		return Status{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err == nil {
		_, status.ActiveJobsReported = fields["active_jobs"]
	}
	if status.IPCSchemaVersion != schema {
		return Status{}, errorcodes.Errorf("daemon_status_schema_mismatch", "daemon status schema %d does not match response schema %d", status.IPCSchemaVersion, schema)
	}
	return status, nil
}

func lifecycleSchemaSupported(schema int) bool {
	return schema >= MinLifecycleSchema && schema <= ipc.SchemaVersion
}

// FollowJob runs one follow_job stream (schema 8): onStart receives the
// validated start, then
// onRecord each record frame's line, LF-terminated, as commands.jsonl
// holds it, after the continuity check; the terminal is returned. The
// cursor is the sequence of the last record delivered, beginning at the
// start's first_sequence minus one: a daemon with no file to catch the
// follower up from begins later than the request's cursor,
// which the start says and the caller reports.
func FollowJob(ctx context.Context, socket string, maxFrame int64, fr ipc.FollowRequest, onStart func(ipc.FollowStart) error, onRecord func(sequence int64, line []byte) error) (ipc.FollowTerminal, error) {
	id, err := osutil.NewID(now())
	if err != nil {
		return ipc.FollowTerminal{}, err
	}
	req, err := ipc.NewRequest(id, ipc.OpFollowJob, buildinfo.Version, fr)
	if err != nil {
		return ipc.FollowTerminal{}, err
	}
	var (
		started  bool
		cursor   = fr.Cursor
		terminal ipc.FollowTerminal
		ended    bool
	)
	err = ipc.Stream(ctx, socket, req, maxFrame, func(resp ipc.Response) error {
		switch {
		case !started:
			if resp.Event != "" {
				return errorcodes.Errorf("ipc_result_malformed", "follow_job began with event %q", resp.Event)
			}
			var start ipc.FollowStart
			if err := json.Unmarshal(resp.Result, &start); err != nil {
				return errorcodes.Errorf("ipc_result_malformed", "follow start: %w", err)
			}
			if err := start.Validate(&fr); err != nil {
				return err
			}
			started = true
			cursor = start.FirstSequence - 1
			return onStart(start)
		case resp.Event == ipc.EventRecord:
			var frame ipc.RecordFrame
			if err := json.Unmarshal(resp.Result, &frame); err != nil {
				return errorcodes.Errorf("ipc_result_malformed", "record frame: %w", err)
			}
			if err := frame.Validate(cursor); err != nil {
				return err
			}
			// The frame's record is the line without its LF; the LF is put
			// back once, on the raw message's own buffer.
			if err := onRecord(frame.Sequence, append(frame.Record, '\n')); err != nil {
				return err
			}
			cursor = frame.Sequence
			return nil
		case resp.Event == ipc.EventStreamTerminal:
			if err := json.Unmarshal(resp.Result, &terminal); err != nil {
				return errorcodes.Errorf("ipc_result_malformed", "follow terminal: %w", err)
			}
			if terminal.Cursor != cursor {
				return errorcodes.Errorf("ipc_result_malformed", "terminal cursor %d differs from the followed %d", terminal.Cursor, cursor)
			}
			ended = true
			return io.EOF
		default:
			return errorcodes.Errorf("ipc_result_malformed", "follow_job event %q is unknown", resp.Event)
		}
	})
	if err != nil {
		return ipc.FollowTerminal{}, err
	}
	if !ended {
		return ipc.FollowTerminal{}, io.ErrUnexpectedEOF
	}
	return terminal, nil
}

// CancelJob sends cancel_job: the daemon answers at
// once with cancelling or terminal; job_unknown and the payload errors are
// returned as coded errors.
func CancelJob(ctx context.Context, socket string, maxFrame int64, cr ipc.CancelRequest) (ipc.CancelResult, error) {
	id, err := osutil.NewID(now())
	if err != nil {
		return ipc.CancelResult{}, err
	}
	req, err := ipc.NewRequest(id, ipc.OpCancelJob, buildinfo.Version, cr)
	if err != nil {
		return ipc.CancelResult{}, err
	}
	var out ipc.CancelResult
	if err := ipc.Call(ctx, socket, req, maxFrame, &out); err != nil {
		return ipc.CancelResult{}, err
	}
	return out, nil
}
