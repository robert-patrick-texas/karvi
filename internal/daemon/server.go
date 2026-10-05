// Package daemon owns the single-UID local orchestration process: the
// schema 6 envelope on socket/daemon.sock (ping, status, drain, stop,
// prepare_job, commit_job, follow_job) and the credential frame on
// socket/credentials.sock. Both
// listeners are owner-only Unix-domain sockets and every accepted connection
// is authenticated with SO_PEERCRED before a byte is decoded. The daemon
// validates plans and runs them; it resolves no selector and no credential.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
)

type Server struct {
	Socket, StatePath string
	MaxFrame          int64
	MaxJobs           int
	UID               int
	StartedAt         time.Time

	// Config and Operator are the daemon's own: execution policy, output
	// roots, DNS timeout, and the identity every job runs under.
	Config   configload.Snapshot
	Operator credentials.Operator
	// Logger receives one line per request outcome; nil logs nothing.
	Logger *slog.Logger
	// Clock replaces time.Now for tests.
	Clock func() time.Time
	// Lookup and Capabilities replace the system resolver for tests.
	Lookup       resolver.LookupFunc
	Capabilities *resolver.Capabilities
	// MaxPreparations bounds the preparation table (128 by default).
	MaxPreparations int
	// Signals delivers SIGTERM and SIGINT to the server: the first drains
	// and waits up to daemon.shutdown-grace-seconds
	// for active jobs, a second shortens the wait to
	// daemon.forced-grace-seconds, and expiry cancels the jobs with the
	// grace-expired cause. Nil means no signal path (tests, and the parent
	// context alone).
	Signals <-chan os.Signal
	// IdleTimeout is daemon.shutdown-idle-timer: after this long with no
	// active job, no preparation in progress,
	// and no request but ping and status, the daemon stops itself as
	// "daemon stop" on an idle daemon would, exit 0. Zero never stops.
	IdleTimeout time.Duration

	cancel context.CancelFunc
	jobs   chan struct{}
	// The job context and its wait group: every
	// accepted job runs under jobCtx, which a forced stop cancels with its
	// cause, and Serve waits for jobWait before it removes the socket.
	jobCtx     context.Context
	cancelJobs context.CancelCauseFunc
	jobWait    sync.WaitGroup
	active     atomic.Int64
	total      atomic.Int64
	// lastActive is the idle clock: the instant of the last request that
	// was not ping or status, or of the last job's end, whichever is later.
	lastActive atomic.Int64
	draining   atomic.Bool
	mu         sync.Mutex

	preparations preparationTable
	idempotency  idempotencyTable
	jobTable     jobTable

	// audit is the daemon's own sink for run.cancel_requested, opened for
	// the serve; the job's runner has its own.
	audit *audit.Sink
}

type Status struct {
	IPCSchemaVersion int       `json:"ipc_schema_version"`
	Status           string    `json:"status"`
	PID              int       `json:"pid"`
	UID              int       `json:"uid"`
	Socket           string    `json:"socket"`
	StartedAt        time.Time `json:"started_at"`
	ActiveJobs       int64     `json:"active_jobs"`
	AcceptedJobs     int64     `json:"accepted_jobs"`
	Version          string    `json:"version"`
	// ActiveJobsReported is false when a daemon's status omitted active_jobs,
	// which only older released lifecycle envelopes can do.
	ActiveJobsReported bool `json:"-"`
}

// StopRequest selects how the daemon treats active jobs when asked to stop.
type StopRequest struct {
	Mode string `json:"mode,omitempty"`
}

const (
	// StopIfIdle stops only when no job is active.
	StopIfIdle = "if_idle"
	// StopForce stops without waiting; unfinished work is cancelled and still
	// receives terminal records.
	StopForce = "force"
)

// CredentialSocketName is the credential channel's socket beside daemon.sock.
const CredentialSocketName = "credentials.sock"

// CredentialSocket is the credential channel's path for a daemon socket.
func CredentialSocket(daemonSocket string) string {
	return filepath.Join(filepath.Dir(daemonSocket), CredentialSocketName)
}

func (s *Server) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

func (s *Server) logf(op, requestID, preparationID, jobID, code string) {
	if s.Logger == nil {
		return
	}
	s.Logger.Info("request", slog.String("operation", op), slog.String("request_id", requestID), slog.String("preparation_id", preparationID), slog.String("job_id", jobID), slog.String("code", code))
}

func (s *Server) Serve(parent context.Context) error {
	osutil.ApplyGOMAXPROCS()
	if s.Socket == "" {
		return errorcodes.Errorf("daemon_socket_blank", "daemon socket is blank")
	}
	if s.UID == 0 && os.Geteuid() != 0 {
		s.UID = os.Geteuid()
	}
	if s.MaxFrame <= 0 {
		s.MaxFrame = 8 << 20
	}
	if s.MaxJobs <= 0 {
		s.MaxJobs = 32
	}
	if s.MaxPreparations <= 0 {
		s.MaxPreparations = MaxPreparations
	}
	if s.Operator.UID == 0 && s.Operator.Username == "" {
		if op, err := osutil.CurrentOperator(); err == nil {
			s.Operator = op
		}
	}
	s.StartedAt = s.now()
	s.touch()
	s.jobs = make(chan struct{}, s.MaxJobs)
	s.preparations.init()
	s.idempotency.init()
	s.jobTable.init()
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	defer cancel()
	s.jobCtx, s.cancelJobs = context.WithCancelCause(parent)
	defer s.cancelJobs(nil)
	if sink, err := audit.New(s.Config); err != nil {
		if s.Logger != nil {
			s.Logger.Warn("audit sink unavailable to the daemon", slog.String("error", err.Error()))
		}
	} else {
		s.audit = sink
		defer sink.Close()
	}
	// The spool directory is resolved and swept at the daemon's start:
	// what a daemon that died with a
	// command in flight left under spooldir goes now, logged by name. A
	// directory that cannot be resolved is logged and does not stop the
	// daemon; the job that needs it is refused at its admission with the
	// same code.
	s.sweepSpools()
	// So is the control-path root: a socket a master killed outright left
	// goes, logged by name; a root that cannot be resolved is logged.
	s.sweepControlSockets()
	if err := os.MkdirAll(filepath.Dir(s.Socket), 0700); err != nil {
		return err
	}
	if err := removeStaleSocket(s.Socket, s.UID); err != nil {
		return err
	}
	credentialSocket := CredentialSocket(s.Socket)
	if err := removeStaleSocket(credentialSocket, s.UID); err != nil {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: s.Socket, Net: "unix"})
	if err != nil {
		return err
	}
	// The socket files stay until Serve returns, after the accounting;
	// closing the listeners only stops accepts.
	listener.SetUnlinkOnClose(false)
	defer listener.Close()
	defer os.Remove(s.Socket)
	if err := os.Chmod(s.Socket, 0600); err != nil {
		return err
	}
	frames, err := net.ListenUnix("unix", &net.UnixAddr{Name: credentialSocket, Net: "unix"})
	if err != nil {
		return err
	}
	frames.SetUnlinkOnClose(false)
	defer frames.Close()
	defer os.Remove(credentialSocket)
	if err := os.Chmod(credentialSocket, 0600); err != nil {
		return err
	}
	if err := s.writeState("running"); err != nil {
		return err
	}
	defer os.Remove(s.StatePath)
	go func() { <-ctx.Done(); _ = listener.Close(); _ = frames.Close() }()
	if s.Signals != nil {
		go s.handleSignals(ctx)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := frames.AcceptUnix()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(c *net.UnixConn) { defer wg.Done(); defer c.Close(); s.handleFrame(ctx, c) }(conn)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.preparations.sweep(s.now())
				s.stopIfIdle()
			}
		}
	}()
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			return err
		}
		wg.Add(1)
		go func(c *net.UnixConn) { defer wg.Done(); defer c.Close(); s.handle(ctx, c) }(conn)
	}
	_ = s.writeState("draining")
	wg.Wait()
	s.awaitAccounting()
	s.preparations.destroyAll()
	return nil
}

// awaitAccounting waits for the job goroutines to finalize their records
// and summaries before the socket and state file go. Under a forced stop
// the jobs are already cancelled with their
// cause and finish within milliseconds; the wait is bounded by
// daemon.forced-grace-seconds with a floor of one second, so a configured
// zero still attempts the terminal writes.
// handleSignals is the signal-driven shutdown. The first signal drains:
// new submissions
// are refused with daemon_draining while active jobs get up to
// daemon.shutdown-grace-seconds to finish, after which the daemon stops. A
// second signal shortens the remaining wait to daemon.forced-grace-seconds.
// At expiry the jobs are cancelled with the grace-expired cause and their
// accounting is still awaited (through awaitAccounting). A stop
// request over IPC during the wait behaves as it does at any other time.
func (s *Server) handleSignals(ctx context.Context) {
	var sig os.Signal
	select {
	case sig = <-s.Signals:
	case <-ctx.Done():
		return
	}
	s.mu.Lock()
	s.draining.Store(true)
	s.mu.Unlock()
	_ = s.writeState("draining")
	grace := time.Duration(s.Config.Int("daemon.shutdown-grace-seconds")) * time.Second
	s.logShutdown("daemon_draining", sig, grace)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	deadline := time.Now().Add(grace)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.active.Load() == 0 {
			s.logShutdown("daemon_stopped", sig, 0)
			s.cancel()
			return
		}
		select {
		case <-ticker.C:
		case sig = <-s.Signals:
			forced := time.Duration(s.Config.Int("daemon.forced-grace-seconds")) * time.Second
			if time.Until(deadline) > forced {
				deadline = time.Now().Add(forced)
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(forced)
				s.logShutdown("shutdown_grace_shortened", sig, forced)
			}
		case <-timer.C:
			s.logShutdown("shutdown_grace_expired", sig, 0)
			s.cancelJobs(jobexec.ErrShutdownGraceExpired)
			s.cancel()
			return
		case <-ctx.Done():
			return
		}
	}
}

// sweepSpools is the daemon-start half of the spool sweep.
func (s *Server) sweepSpools() {
	dir, err := osutil.ResolveSpoolDir(s.Config.String("spooldir"), s.Operator.Home, s.Operator.UID)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("spool directory unavailable", slog.String("code", errorcodes.Of(err)), slog.String("error", err.Error()))
		}
		return
	}
	output.SweepSpools(dir, func(name string) {
		if s.Logger != nil {
			s.Logger.Info("removed the abandoned spool", slog.String("code", "spool_abandoned_removed"), slog.String("path", filepath.Join(dir, name)))
		}
	})
}

// sweepControlSockets is the daemon-start half of the control socket
// sweep: a socket a master killed outright left. It makes nothing: a root
// not yet made holds no socket.
func (s *Server) sweepControlSockets() {
	base, err := osutil.BaseDirPath(s.Config.String("basedir"), s.Operator.Home, s.Operator.Username)
	if err == nil {
		var root string
		if root, err = osutil.ControlPathRootPlace(s.Config.String("ssh.control-path-root"), base, s.Operator.Home, s.Operator.Username, s.Operator.UID); err == nil {
			osutil.SweepControlSockets(root, func(name string) {
				if s.Logger != nil {
					s.Logger.Info("removed the abandoned control socket", slog.String("code", "control_socket_abandoned_removed"), slog.String("path", filepath.Join(root, name)))
				}
			})
			return
		}
	}
	if s.Logger != nil {
		s.Logger.Warn("control-path root unavailable", slog.String("code", errorcodes.Of(err)), slog.String("error", err.Error()))
	}
}

func (s *Server) logShutdown(code string, sig os.Signal, wait time.Duration) {
	if s.Logger == nil {
		return
	}
	s.Logger.Info("shutdown", slog.String("code", code), slog.String("signal", sig.String()), slog.Int64("active_jobs", s.active.Load()), slog.Duration("wait", wait))
}

// accountingBound is how long a stopping daemon waits for a job's terminal
// writes: daemon.forced-grace-seconds with a floor of two seconds, one for
// the SSH driver's pipe wait after the kill and one for the writes (a
// one-second floor lost that race under a held grandchild).
func (s *Server) accountingBound() time.Duration {
	bound := time.Duration(s.Config.Int("daemon.forced-grace-seconds")) * time.Second
	if bound < 2*time.Second {
		bound = 2 * time.Second
	}
	return bound
}

func (s *Server) awaitAccounting() {
	done := make(chan struct{})
	go func() { s.jobWait.Wait(); close(done) }()
	bound := s.accountingBound()
	select {
	case <-done:
	case <-time.After(bound):
		if s.Logger != nil {
			s.Logger.Warn("shutdown", slog.String("code", "shutdown_incomplete"), slog.Int64("active_jobs", s.active.Load()), slog.Duration("waited", bound))
		}
	}
}

func (s *Server) handle(ctx context.Context, conn *net.UnixConn) {
	uid, err := ipc.PeerUID(conn)
	if err != nil || int(uid) != s.UID {
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, Error: &ipc.Error{Code: "peer_uid_denied", Message: "daemon accepts only the same effective UID", Details: map[string]any{}}}, s.MaxFrame)
		return
	}
	var req ipc.Request
	if err := ipc.Read(conn, &req, s.MaxFrame); err != nil {
		s.writeError(conn, "ipc_read_failed", err, "")
		return
	}
	if req.IPCSchemaVersion != ipc.SchemaVersion {
		s.writeError(conn, "ipc_schema_incompatible", fmt.Errorf("client schema %d, daemon schema %d", req.IPCSchemaVersion, ipc.SchemaVersion), req.RequestID)
		return
	}
	s.preparations.sweep(s.now())
	if req.Operation != "ping" && req.Operation != "status" {
		// A probe keeps no daemon alive: a monitor's status, or the
		// launcher's ping, is not use.
		s.touch()
	}
	switch req.Operation {
	case "ping", "status":
		s.writeResult(conn, req.RequestID, s.status(s.state()))
	case "drain":
		s.draining.Store(true)
		_ = s.writeState("draining")
		s.writeResult(conn, req.RequestID, s.status("draining"))
	case "stop":
		var stop StopRequest
		if len(req.Payload) > 0 {
			if err := json.Unmarshal(req.Payload, &stop); err != nil {
				s.writeError(conn, "daemon_stop_request_malformed", err, req.RequestID)
				return
			}
		}
		if stop.Mode != "" && stop.Mode != StopIfIdle && stop.Mode != StopForce {
			s.writeError(conn, "daemon_stop_mode_unknown", fmt.Errorf("unknown stop mode %q", stop.Mode), req.RequestID)
			return
		}
		if !s.beginStop(stop.Mode) {
			s.writeError(conn, "daemon_active_jobs", fmt.Errorf("daemon reports %d active job(s)", s.active.Load()), req.RequestID)
			return
		}
		_ = s.writeState("draining")
		s.writeResult(conn, req.RequestID, map[string]any{"stopping": true})
		if stop.Mode == StopForce {
			// Forcing cancels the jobs with the shutdown cause; Serve then
			// awaits their accounting.
			s.cancelJobs(jobexec.ErrShutdownForced)
		}
		go func() { time.Sleep(25 * time.Millisecond); s.cancel() }()
	case ipc.OpPrepareJob:
		s.prepareJob(ctx, conn, req)
	case ipc.OpCommitJob:
		s.commitJob(ctx, conn, req)
	case ipc.OpFollowJob:
		s.followJob(ctx, conn, req)
	case ipc.OpCancelJob:
		s.cancelJob(conn, req)
	case ipc.OpProvideCredentials:
		s.logf(req.Operation, req.RequestID, "", "", "credential_channel_required")
		s.writeError(conn, "credential_channel_required", fmt.Errorf("provide_credentials is the credential frame on %s, not an envelope operation", CredentialSocket(s.Socket)), req.RequestID)
	default:
		s.writeError(conn, "ipc_operation_unknown", fmt.Errorf("unknown operation %q", req.Operation), req.RequestID)
	}
}

// touch restarts the idle clock.
func (s *Server) touch() { s.lastActive.Store(s.now().UnixNano()) }

// stopIfIdle is the idle timer's check, on the minute ticker: with
// IdleTimeout set and the clock's instant that long ago, no preparation
// live, and no active job (beginStop's own test, under the admission lock),
// the daemon drains as "daemon stop" does and Serve returns 0. A
// preparation is a client between prepare and commit, so its job is not
// yet active but is coming.
func (s *Server) stopIfIdle() {
	if s.IdleTimeout <= 0 || s.draining.Load() {
		return
	}
	idle := s.now().Sub(time.Unix(0, s.lastActive.Load()))
	if idle < s.IdleTimeout || s.preparations.count() > 0 {
		return
	}
	if !s.beginStop(StopIfIdle) {
		return
	}
	_ = s.writeState("draining")
	if s.Logger != nil {
		s.Logger.Info("stopping: idle", slog.String("idle", idle.Truncate(time.Second).String()), slog.String("key", "daemon.shutdown-idle-timer"), slog.String("value", s.IdleTimeout.String()))
	}
	s.cancel()
}

// admit and beginStop share one lock so no job can be admitted between an
// idle check and the transition to draining.
func (s *Server) admit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draining.Load() {
		return false
	}
	s.active.Add(1)
	return true
}

func (s *Server) beginStop(mode string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode == StopIfIdle && s.active.Load() > 0 {
		return false
	}
	s.draining.Store(true)
	return true
}

func (s *Server) state() string {
	if s.draining.Load() {
		return "draining"
	}
	return "running"
}

func (s *Server) status(state string) Status {
	return Status{IPCSchemaVersion: ipc.SchemaVersion, Status: state, PID: os.Getpid(), UID: s.UID, Socket: s.Socket, StartedAt: s.StartedAt, ActiveJobs: s.active.Load(), AcceptedJobs: s.total.Load(), Version: buildinfo.Version}
}
func (s *Server) writeState(state string) error {
	if s.StatePath == "" {
		return nil
	}
	return osutil.AtomicJSON(s.StatePath, s.status(state), 0600)
}
func (s *Server) writeResult(conn io.Writer, id string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		s.writeError(conn, "ipc_encode_failed", err, id)
		return
	}
	_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: id, Result: raw}, s.MaxFrame)
}
func (s *Server) writeError(conn io.Writer, code string, err error, id string) {
	_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: id, Error: &ipc.Error{Code: code, Message: errorcodes.Message(err), Retryable: false, Details: map[string]any{}}}, s.MaxFrame)
}

// writeCoded reports err under its own registered code when it carries one,
// else under fallback.
func (s *Server) writeCoded(conn io.Writer, fallback string, err error, id string) string {
	code := errorcodes.Of(err)
	if code == "" {
		code = fallback
	}
	s.writeError(conn, code, err, id)
	return code
}
func removeStaleSocket(path string, uid int) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return errorcodes.Errorf("daemon_socket_path_not_socket", "daemon socket path exists and is not a socket: %s", path)
	}
	conn, dialErr := net.DialTimeout("unix", path, 150*time.Millisecond)
	if dialErr == nil {
		conn.Close()
		return errorcodes.Errorf("daemon_already_running", "daemon already running at %s", path)
	}
	// The parent directory is private and owner-checked by osutil; a dead socket
	// in that directory is safe to remove.
	_ = uid
	return os.Remove(path)
}
