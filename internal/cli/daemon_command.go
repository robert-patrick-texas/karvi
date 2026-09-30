package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

func daemonStart(ctx context.Context, inv *Invocation, streams app.IO) int {
	g := inv.Global
	if inv.Flag(optForeground) {
		return serveDaemon(g, streams.Stderr)
	}
	if _, err := ensureDaemon(ctx, g, streams.Stderr); err != nil {
		return reportError(streams.Stderr, "daemon_start_failed", err)
	}
	if !g.quiet {
		fmt.Fprintln(streams.Stdout, "daemon running")
	}
	return 0
}

func daemonStatus(ctx context.Context, inv *Invocation, streams app.IO) int {
	g := inv.Global
	format := inv.String(optFormatTJ)
	rt, err := app.ResolveDaemonRuntime(g.common())
	if err != nil {
		return reportError(streams.Stderr, "config_load_failed", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	probe, err := daemon.Probe(cctx, rt.Socket, rt.MaxFrame)
	if err != nil {
		return reportError(streams.Stderr, "daemon_unreachable", fmt.Errorf("daemon not running or unreadable: %w", err))
	}
	status := probe.Status
	switch format {
	case "json":
		out := struct {
			daemon.Status
			Compatible      bool   `json:"compatible"`
			ClientVersion   string `json:"client_version"`
			ClientIPCSchema int    `json:"client_ipc_schema"`
		}{Status: status, Compatible: probe.Compatible, ClientVersion: buildinfo.Version, ClientIPCSchema: probe.ClientSchema}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintf(streams.Stdout, "%s\n", b)
	default:
		fmt.Fprintf(streams.Stdout, "status: %s\npid: %d\nuid: %d\nsocket: %s\nversion: %s\ndaemon_ipc_schema: %d\nclient_version: %s\nclient_ipc_schema: %d\ncompatible: %t\nstarted_at: %s\nactive_jobs: %d\naccepted_jobs: %d\n", status.Status, status.PID, status.UID, status.Socket, status.Version, probe.DaemonSchema, buildinfo.Version, probe.ClientSchema, probe.Compatible, status.StartedAt.Format(time.RFC3339Nano), status.ActiveJobs, status.AcceptedJobs)
		if !probe.Compatible {
			fmt.Fprintln(streams.Stdout, "remediation: karvi daemon restart")
		}
	}
	return 0
}

// stopOptions holds the mutually exclusive waiting choices shared by daemon
// stop and restart. With none selected, the stop is refused while jobs run.
type stopOptions struct {
	grace bool
	after time.Duration
	force bool
}

// parseStopOptions checks the parsed stop/restart options. It returns
// done=true when the caller should return code immediately.
func parseStopOptions(inv *Invocation, streams app.IO) (opts stopOptions, done bool, code int) {
	opts = stopOptions{grace: inv.Flag(optGrace), after: inv.Duration(optAfter), force: inv.Flag(optForce)}
	afterSet := inv.Set(optAfter)
	if afterSet && opts.after <= 0 {
		return opts, true, usageError(streams.Stderr, "daemon_stop_after_not_positive", "--after requires a positive duration such as 90s or 5m")
	}
	selected := 0
	for _, on := range []bool{opts.grace, afterSet, opts.force} {
		if on {
			selected++
		}
	}
	if selected > 1 {
		return opts, true, usageError(streams.Stderr, "daemon_stop_options_conflict", "--grace, --after, and --force are mutually exclusive")
	}
	return opts, false, 0
}

func daemonStop(ctx context.Context, inv *Invocation, streams app.IO) int {
	g := inv.Global
	opts, done, code := parseStopOptions(inv, streams)
	if done {
		return code
	}
	rt, err := app.ResolveDaemonRuntime(g.common())
	if err != nil {
		return reportError(streams.Stderr, "config_load_failed", err)
	}
	probe, err := stopDaemonRuntime(ctx, rt, opts, streams.Stderr)
	if err != nil {
		return reportError(streams.Stderr, "daemon_stop_failed", err)
	}
	if !g.quiet {
		if probe.Compatible {
			fmt.Fprintln(streams.Stdout, "daemon stopped")
		} else {
			fmt.Fprintf(streams.Stdout, "daemon stopped (version=%s ipc_schema=%d; client_schema=%d)\n", displayUnknown(probe.Status.Version), probe.DaemonSchema, probe.ClientSchema)
		}
	}
	return 0
}

func daemonRestart(ctx context.Context, inv *Invocation, streams app.IO) int {
	g := inv.Global
	opts, done, code := parseStopOptions(inv, streams)
	if done {
		return code
	}
	rt, err := app.ResolveDaemonRuntime(g.common())
	if err != nil {
		return reportError(streams.Stderr, "config_load_failed", err)
	}
	if _, statErr := os.Lstat(rt.Socket); statErr == nil {
		stoppedProbe, stopErr := stopDaemonRuntime(ctx, rt, opts, streams.Stderr)
		if stopErr != nil {
			return reportError(streams.Stderr, "daemon_stop_failed", stopErr)
		}
		if !g.quiet && !stoppedProbe.Compatible {
			fmt.Fprintf(streams.Stderr, "stopped incompatible daemon version=%s ipc_schema=%d\n", displayUnknown(stoppedProbe.Status.Version), stoppedProbe.DaemonSchema)
		}
	} else if !os.IsNotExist(statErr) {
		return reportError(streams.Stderr, "daemon_socket_uninspectable", statErr)
	}
	quietStart := g
	quietStart.quiet = true
	if _, err := ensureDaemon(ctx, quietStart, streams.Stderr); err != nil {
		return reportError(streams.Stderr, "daemon_start_failed", err)
	}
	if !g.quiet {
		fmt.Fprintln(streams.Stdout, "daemon restarted")
	}
	return 0
}

// stopDaemonRuntime applies the stop/restart option semantics and waits for the
// daemon to exit. Waiting options drain a current-schema daemon first so new
// submissions cannot extend the wait.
func stopDaemonRuntime(ctx context.Context, rt app.DaemonRuntime, opts stopOptions, stderr io.Writer) (daemon.ProbeResult, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	probe, err := daemon.Probe(probeCtx, rt.Socket, rt.MaxFrame)
	cancel()
	if err != nil {
		return probe, errorcodes.Ensure(err, "daemon_unreachable")
	}
	mode := daemon.StopForce
	switch {
	case opts.grace || opts.after > 0:
		if probe.Compatible {
			drainCtx, drainCancel := context.WithTimeout(ctx, 2*time.Second)
			_, err := daemon.Drain(drainCtx, rt.Socket, rt.MaxFrame)
			drainCancel()
			if err != nil {
				return probe, errorcodes.Ensure(err, "daemon_drain_failed")
			}
		}
		if err := waitForIdle(ctx, rt, opts.after, stderr); err != nil {
			return probe, err
		}
	case !opts.force:
		if !probe.Compatible && (!probe.Status.ActiveJobsReported || probe.Status.ActiveJobs > 0) {
			return probe, activeJobsError(probe.Status)
		}
		if probe.Compatible {
			mode = daemon.StopIfIdle
		}
	}
	stopCtx, stopCancel := context.WithTimeout(ctx, stopTimeout(rt))
	defer stopCancel()
	probe, err = daemon.StopCompatible(stopCtx, rt.Socket, rt.MaxFrame, mode)
	if err != nil {
		if errorcodes.Of(err) == "daemon_active_jobs" {
			return probe, fmt.Errorf("%w; %s", err, activeJobsRemedy)
		}
		return probe, errorcodes.Ensure(err, "daemon_stop_failed")
	}
	for {
		if _, err := os.Stat(rt.Socket); os.IsNotExist(err) {
			return probe, nil
		}
		select {
		case <-stopCtx.Done():
			return probe, errorcodes.Errorf("daemon_stop_timeout", "daemon stop timed out")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

const activeJobsRemedy = "use --grace to wait for active jobs, --after=DURATION to wait up to a limit, or --force to stop now"

func activeJobsError(status daemon.Status) error {
	count := "an unknown number of"
	if status.ActiveJobsReported {
		count = strconv.FormatInt(status.ActiveJobs, 10)
	}
	return fmt.Errorf("daemon_active_jobs: daemon reports %s active job(s); %s", count, activeJobsRemedy)
}

// waitForIdle polls until the daemon reports no active jobs. With a positive
// limit it returns nil once the limit elapses so the caller forces the stop.
// Interrupting the wait leaves a drained daemon draining.
func waitForIdle(ctx context.Context, rt app.DaemonRuntime, limit time.Duration, stderr io.Writer) error {
	var deadline <-chan time.Time
	if limit > 0 {
		timer := time.NewTimer(limit)
		defer timer.Stop()
		deadline = timer.C
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		pctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		probe, err := daemon.Probe(pctx, rt.Socket, rt.MaxFrame)
		cancel()
		if err == nil && probe.Status.ActiveJobsReported && probe.Status.ActiveJobs == 0 {
			return nil
		}
		if err != nil && ctx.Err() == nil {
			return errorcodes.Ensure(err, "daemon_unreachable")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("daemon_stop_wait_interrupted: stopped waiting; the daemon remains draining and rejects new jobs; run \"karvi daemon stop --force\" to stop it now or \"karvi daemon status\" to check it")
		case <-deadline:
			fmt.Fprintf(stderr, "daemon still reports %d active job(s) after %s; forcing stop\n", probe.Status.ActiveJobs, limit)
			return nil
		case <-ticker.C:
		}
	}
}

func stopTimeout(rt app.DaemonRuntime) time.Duration {
	timeout := rt.Config.Duration("daemon.start-timeout")
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return timeout + time.Duration(rt.Config.Int("daemon.forced-grace-seconds"))*time.Second
}

// serveDaemon runs the daemon in this process. It takes no context from
// the command line: the CLI's first-signal cancel would end the server
// without accounting, so the server owns SIGTERM and SIGINT itself and
// drains, waits the grace, and accounts before it exits.
func serveDaemon(g globalOptions, stderr io.Writer) int {
	rt, err := app.ResolveDaemonRuntime(g.common())
	if err != nil {
		return reportError(stderr, "config_load_failed", err)
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	secretVariableNotice(logger)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	server := &daemon.Server{Socket: rt.Socket, StatePath: rt.StatePath, MaxFrame: rt.MaxFrame, MaxJobs: rt.MaxJobs, UID: rt.Operator.UID, Config: rt.Config, Operator: rt.Operator, Logger: logger, Signals: signals, IdleTimeout: rt.Config.Duration("daemon.shutdown-idle-timer")}
	if err := server.Serve(context.Background()); err != nil {
		return reportError(stderr, "daemon_serve_failed", err)
	}
	return 0
}

// ensureDaemon returns a running compatible daemon; every error carries a
// registered code.
func ensureDaemon(ctx context.Context, g globalOptions, stderr io.Writer) (app.DaemonRuntime, error) {
	rt, err := app.ResolveDaemonRuntime(g.common())
	if err != nil {
		return rt, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	probe, pingErr := daemon.Probe(pingCtx, rt.Socket, rt.MaxFrame)
	cancel()
	if pingErr == nil {
		if probe.Compatible {
			return rt, nil
		}
		return rt, incompatibleDaemonError(probe)
	}
	var mismatch *ipc.SchemaMismatchError
	if errors.As(pingErr, &mismatch) {
		return rt, fmt.Errorf("daemon_incompatible: running daemon uses unsupported IPC schema %d; karvi %s requires schema %d; no replacement was attempted; run \"karvi daemon stop\" with the matching older karvi executable, then retry", mismatch.DaemonSchema, buildinfo.Version, ipc.SchemaVersion)
	}
	exe, err := os.Executable()
	if err != nil {
		return rt, errorcodes.Errorf("karvi_executable_unlocatable", "locate karvi executable: %w", err)
	}
	log, err := os.OpenFile(rt.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return rt, errorcodes.Errorf("daemon_log_open_failed", "open daemon log %s: %w", rt.LogPath, err)
	}
	childArgs := globalArgs(g)
	childArgs = append(childArgs, "daemon", "serve")
	cmd := exec.Command(exe, childArgs...)
	cmd.Env = daemonEnvironment(rt.Config)
	cmd.Stdin = nil
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		log.Close()
		return rt, errorcodes.Errorf("daemon_spawn_failed", "start daemon: %w", err)
	}
	childPID := cmd.Process.Pid // Release clears it
	_ = cmd.Process.Release()
	_ = log.Close()
	timeout := rt.Config.Duration("daemon.start-timeout")
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	wctx, wcancel := context.WithTimeout(ctx, timeout)
	defer wcancel()
	if err := ipc.Wait(wctx, rt.Socket); err != nil {
		return rt, fmt.Errorf("daemon_start_failed: daemon failed to start; inspect %s: %w", rt.LogPath, err)
	}
	// The daemon's first answer after its socket appeared: two seconds,
	// since one second lapsed once under a full parallel `go test ./...`
	// on a four-CPU host.
	pctx, pcancel := context.WithTimeout(ctx, 2*time.Second)
	defer pcancel()
	status, err := daemon.Ping(pctx, rt.Socket, rt.MaxFrame)
	if err != nil {
		return rt, errorcodes.Ensure(err, "daemon_start_failed")
	}
	// Two clients launching at once each start a child; the second child
	// finds the socket taken and exits, and the daemon answering is the
	// other client's. Only the client whose own child answers says so.
	if !g.quiet && status.PID == childPID {
		fmt.Fprintf(stderr, "daemon started socket=%s\n", rt.Socket)
	}
	return rt, nil
}

// daemonEnvironment is what the launcher hands daemon serve: the child
// allow-list every ssh child receives, plus
// karvi's own non-secret variables (every KARVI__ configuration variable
// and KARVI_ASKPASS_PATH) and TZ and TMPDIR, which the local clock and the
// transport's scratch directory read. The operator's shell secrets never
// reach the daemon's process.
func daemonEnvironment(cfg configload.Snapshot) []string {
	extra := []string{}
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && (strings.HasPrefix(name, "KARVI__") || name == "KARVI_ASKPASS_PATH" || name == "TZ" || name == "TMPDIR") {
			extra = append(extra, entry)
		}
	}
	return osutil.ChildEnvironment(cfg.Strings("security.child-environment-allowlist"), extra...)
}

// secretVariableNotice writes one notice per built-in credential variable
// present in daemon serve's own environment: the foreground and
// service-unit paths karvi does not launch. The name only, never the
// value; the daemon does not read them.
func secretVariableNotice(logger *slog.Logger) {
	for _, name := range []string{"NETUSER", "NETPASS", "NETENABLE"} {
		if _, ok := os.LookupEnv(name); ok {
			logger.Warn(fmt.Sprintf("daemon_environment_secret_variable: %s is set in the daemon's environment and is not read; start the daemon from a shell without it", name), slog.String("variable", name))
		}
	}
}

// incompatibleDaemonError words a probe that found the daemon incompatible:
// its version and IPC schema are both named beside the client's, since either
// difference is the reason, and the remediation
// is the same for both.
func incompatibleDaemonError(probe daemon.ProbeResult) error {
	return fmt.Errorf("daemon_incompatible: running daemon version %s uses IPC schema %d; karvi %s uses schema %d, and both must match; no job was submitted and the daemon was not replaced automatically; run \"karvi daemon status\" and, when active_jobs is 0, run \"karvi daemon restart\", then retry (or use \"karvi run --no-daemon ...\" for an explicit foreground run)", displayUnknown(probe.Status.Version), probe.DaemonSchema, buildinfo.Version, probe.ClientSchema)
}

func displayUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func globalArgs(g globalOptions) []string {
	out := []string{}
	for _, v := range g.configs {
		out = append(out, "--config", v)
	}
	for _, v := range g.sets {
		out = append(out, "--set", v)
	}
	if g.quiet {
		out = append(out, "--quiet")
	}
	if g.debug {
		out = append(out, "--debug")
	}
	if g.timezone != "" {
		out = append(out, "--timezone", g.timezone)
	}
	if g.ansi != "" {
		out = append(out, "--ansi", g.ansi)
	}
	return out
}
