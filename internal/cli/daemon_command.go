package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// runtime is the daemon's runtime from the invocation's one read, under the
// global options' keys. A failure is reported on stderr, and its exit
// returned.
func (g globalOptions) runtime(stderr io.Writer) (app.DaemonRuntime, int, bool) {
	common, code, ok := g.read(g.flags(), stderr)
	if !ok {
		return app.DaemonRuntime{}, code, false
	}
	rt, err := app.ResolveDaemonRuntime(common)
	if err != nil {
		return app.DaemonRuntime{}, g.reportError(stderr, "config_load_failed", err), false
	}
	return rt, 0, true
}

func daemonStart(ctx context.Context, inv *Invocation, streams app.IO) int {
	g := inv.Global
	if inv.Flag(optForeground) {
		return serveDaemon(g, streams.Stderr)
	}
	rt, code, ok := g.runtime(streams.Stderr)
	if !ok {
		return code
	}
	if err := ensureDaemon(ctx, rt, g, streams.Stderr); err != nil {
		return g.reportError(streams.Stderr, "daemon_start_failed", err)
	}
	if !g.quiet {
		fmt.Fprintln(streams.Stdout, "daemon running")
	}
	return 0
}

func daemonStatus(ctx context.Context, inv *Invocation, streams app.IO) int {
	g := inv.Global
	format := inv.String(optFormatTJ)
	rt, code, ok := g.runtime(streams.Stderr)
	if !ok {
		return code
	}
	cctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	probe, err := daemon.Probe(cctx, rt.Socket, rt.MaxFrame)
	if err != nil {
		return g.reportError(streams.Stderr, "daemon_unreachable", fmt.Errorf("daemon not running or unreadable: %w", err))
	}
	view := newDaemonStatusView(probe)
	switch format {
	case "json":
		b, _ := json.MarshalIndent(view, "", "  ")
		fmt.Fprintf(streams.Stdout, "%s\n", b)
	default:
		remedy := ""
		if !probe.Compatible {
			remedy = restartCommand(rt)
		}
		view.render(streams.Stdout, g.style(streams.Stdout), remedy)
	}
	return 0
}

// restartCommand is what restarts the account's daemon: karvi daemon restart,
// or under daemon.launch = "systemd" the unit's restart (EXAMPLES chapter 57,
// issue 15).
func restartCommand(rt app.DaemonRuntime) string {
	if rt.Systemd() {
		return "systemctl restart " + rt.Unit()
	}
	return "karvi daemon restart"
}

// daemonStatusView is daemon status's answer (EXAMPLES chapter 57, issue 2):
// the daemon's values that have a client's beside them prefixed daemon_, the
// client's client_, each pair the daemon's over the client's; the same keys in
// the text, in this order, and in JSON.
type daemonStatusView struct {
	Status          string    `json:"status"`
	PID             int       `json:"pid"`
	UID             int       `json:"uid"`
	Socket          string    `json:"socket"`
	DaemonVersion   string    `json:"daemon_version"`
	ClientVersion   string    `json:"client_version"`
	DaemonCommit    string    `json:"daemon_commit"`
	ClientCommit    string    `json:"client_commit"`
	DaemonIPCSchema int       `json:"daemon_ipc_schema"`
	ClientIPCSchema int       `json:"client_ipc_schema"`
	DaemonBuild     string    `json:"daemon_build"`
	ClientBuild     string    `json:"client_build"`
	Compatible      bool      `json:"compatible"`
	StartedAt       time.Time `json:"started_at"`
	ActiveJobs      int64     `json:"active_jobs"`
	AcceptedJobs    int64     `json:"accepted_jobs"`
}

func newDaemonStatusView(probe daemon.ProbeResult) daemonStatusView {
	s := probe.Status
	return daemonStatusView{
		Status: s.Status, PID: s.PID, UID: s.UID, Socket: s.Socket,
		DaemonVersion: s.Version, ClientVersion: buildinfo.Version,
		DaemonCommit: s.Commit, ClientCommit: buildinfo.Commit,
		DaemonIPCSchema: probe.DaemonSchema, ClientIPCSchema: probe.ClientSchema,
		DaemonBuild: s.Build, ClientBuild: buildinfo.BuildOrNone(),
		Compatible: probe.Compatible,
		StartedAt:  s.StartedAt, ActiveJobs: s.ActiveJobs, AcceptedJobs: s.AcceptedJobs,
	}
}

// render writes the text form: each pair's values in the success colour when
// they are equal and in the error colour when not, compatible's value alike,
// and remedy, when there is one, after compatible.
func (v daemonStatusView) render(w io.Writer, style display.LineStyle, remedy string) {
	line := func(key, value string) {
		if value == "" {
			fmt.Fprintf(w, "%s:\n", key)
			return
		}
		fmt.Fprintf(w, "%s: %s\n", key, value)
	}
	paint := func(value string, equal bool) string {
		color := style.Error
		if equal {
			color = style.Success
		}
		return display.ANSIStyle(value, color, style.Enabled, false)
	}
	pair := func(key, daemonValue, clientValue string, equal bool) {
		line("daemon_"+key, paint(daemonValue, equal))
		line("client_"+key, paint(clientValue, equal))
	}
	line("status", v.Status)
	line("pid", strconv.Itoa(v.PID))
	line("uid", strconv.Itoa(v.UID))
	line("socket", v.Socket)
	pair("version", v.DaemonVersion, v.ClientVersion, v.DaemonVersion == v.ClientVersion)
	pair("commit", v.DaemonCommit, v.ClientCommit, v.DaemonCommit == v.ClientCommit)
	pair("ipc_schema", strconv.Itoa(v.DaemonIPCSchema), strconv.Itoa(v.ClientIPCSchema), v.DaemonIPCSchema == v.ClientIPCSchema)
	pair("build", v.DaemonBuild, v.ClientBuild, v.DaemonBuild != "" && v.DaemonBuild == v.ClientBuild && v.ClientBuild != "none")
	line("compatible", paint(strconv.FormatBool(v.Compatible), v.Compatible))
	if remedy != "" {
		line("remediation", remedy)
	}
	line("started_at", v.StartedAt.Format(time.RFC3339Nano))
	line("active_jobs", strconv.FormatInt(v.ActiveJobs, 10))
	line("accepted_jobs", strconv.FormatInt(v.AcceptedJobs, 10))
}

// stopOptions holds the mutually exclusive waiting choices shared by daemon
// stop and restart. With none selected, the stop is refused while jobs run;
// quiet leaves out the line a wait begins with.
type stopOptions struct {
	grace bool
	after time.Duration
	force bool
	quiet bool
}

// parseStopOptions checks the parsed stop/restart options. It returns
// done=true when the caller should return code immediately.
func parseStopOptions(inv *Invocation, streams app.IO) (opts stopOptions, done bool, code int) {
	opts = stopOptions{grace: inv.Flag(optGrace), after: inv.Duration(optAfter), force: inv.Flag(optForce), quiet: inv.Global.quiet}
	afterSet := inv.Set(optAfter)
	if afterSet && opts.after <= 0 {
		return opts, true, inv.Global.usageError(streams.Stderr, "daemon_stop_after_not_positive", "--after requires a positive duration such as 90s or 5m")
	}
	selected := 0
	for _, on := range []bool{opts.grace, afterSet, opts.force} {
		if on {
			selected++
		}
	}
	if selected > 1 {
		return opts, true, inv.Global.usageError(streams.Stderr, "daemon_stop_options_conflict", "--grace, --after, and --force are mutually exclusive")
	}
	return opts, false, 0
}

func daemonStop(ctx context.Context, inv *Invocation, streams app.IO) int {
	g := inv.Global
	opts, done, code := parseStopOptions(inv, streams)
	if done {
		return code
	}
	rt, code, ok := g.runtime(streams.Stderr)
	if !ok {
		return code
	}
	if rt.Systemd() {
		return g.reportError(streams.Stderr, "daemon_managed", managed(rt, "stop"))
	}
	probe, err := stopDaemonRuntime(ctx, rt, opts, streams.Stderr)
	if err != nil {
		return g.reportError(streams.Stderr, "daemon_stop_failed", err)
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
	rt, code, ok := g.runtime(streams.Stderr)
	if !ok {
		return code
	}
	if rt.Systemd() {
		return g.reportError(streams.Stderr, "daemon_managed", managed(rt, "restart"))
	}
	if _, statErr := os.Lstat(rt.Socket); statErr == nil {
		stoppedProbe, stopErr := stopDaemonRuntime(ctx, rt, opts, streams.Stderr)
		if stopErr != nil {
			return g.reportError(streams.Stderr, "daemon_stop_failed", stopErr)
		}
		if !g.quiet && !stoppedProbe.Compatible {
			fmt.Fprintf(streams.Stderr, "stopped incompatible daemon version=%s ipc_schema=%d\n", displayUnknown(stoppedProbe.Status.Version), stoppedProbe.DaemonSchema)
		}
	} else if !os.IsNotExist(statErr) {
		return g.reportError(streams.Stderr, "daemon_socket_uninspectable", statErr)
	}
	quietStart := g
	quietStart.quiet = true
	if err := ensureDaemon(ctx, rt, quietStart, streams.Stderr); err != nil {
		return g.reportError(streams.Stderr, "daemon_start_failed", err)
	}
	if !g.quiet {
		fmt.Fprintln(streams.Stdout, "daemon restarted")
	}
	return 0
}

// managed is daemon stop's or restart's refusal under daemon.launch =
// "systemd" (EXAMPLES chapter 57, issue 15): the unit's daemon is stopped and
// restarted with systemctl, verb its command.
func managed(rt app.DaemonRuntime, verb string) error {
	return errorcodes.Errorf("daemon_managed", "daemon.launch is systemd; systemctl %s %s", verb, rt.Unit())
}

// stopDaemonRuntime applies the stop/restart option semantics and waits for the
// daemon to exit (EXAMPLES chapter 57, issue 4). Without an option the stop is
// if_idle, and the daemon, which alone counts its jobs, refuses it while any
// run. The waiting options drain the daemon reached, of any version or build,
// at its own IPC schema, and wait for it to exit after its last job, the wait
// begun with a line naming its jobs; --after forces the stop at its limit.
func stopDaemonRuntime(ctx context.Context, rt app.DaemonRuntime, opts stopOptions, stderr io.Writer) (daemon.ProbeResult, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	probe, err := daemon.Probe(probeCtx, rt.Socket, rt.MaxFrame)
	cancel()
	if err != nil {
		return probe, errorcodes.Ensure(err, "daemon_unreachable")
	}
	mode := daemon.StopIfIdle
	switch {
	case opts.grace || opts.after > 0:
		drainCtx, drainCancel := context.WithTimeout(ctx, 2*time.Second)
		drained, err := daemon.Drain(drainCtx, rt.Socket, rt.MaxFrame, probe.DaemonSchema)
		drainCancel()
		if err != nil {
			return probe, errorcodes.Ensure(err, "daemon_drain_failed")
		}
		if !opts.quiet && drained.ActiveJobs > 0 {
			fmt.Fprintln(stderr, drainingLine(probe.Status.Status == "draining", drained.ActiveJobs, opts.after))
		}
		gone, err := waitGone(ctx, rt, drained.PID, opts.after, stderr)
		if err != nil || gone {
			return probe, err
		}
		mode = daemon.StopForce
	case opts.force:
		mode = daemon.StopForce
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

// drainingLine is the line a --grace or --after wait begins with: the jobs
// the drain's answer names, whether the probe found the daemon draining
// before this command (an operator back after a broken wait), and the wait.
func drainingLine(before bool, jobs int64, after time.Duration) string {
	state := "daemon draining"
	if before {
		state = "daemon already draining"
	}
	wait := "waiting for it to exit"
	if after > 0 {
		wait = fmt.Sprintf("waiting up to %s, then forcing", after)
	}
	return fmt.Sprintf("%s: %s; %s", state, app.JobsRunning(jobs), wait)
}

// waitGone waits for a drained daemon, pid, to exit (daemonExited). At a
// positive limit a daemon still answering is reported and false returned, so
// the caller forces the stop; one no longer answering is leaving and is waited
// for up to the stop's own bound. The operator's interrupt ends the wait alone:
// the daemon finishes its jobs and exits.
func waitGone(ctx context.Context, rt app.DaemonRuntime, pid int, limit time.Duration, stderr io.Writer) (bool, error) {
	var deadline <-chan time.Time
	if limit > 0 {
		timer := time.NewTimer(limit)
		defer timer.Stop()
		deadline = timer.C
	}
	leaving := false
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if daemonExited(rt.Socket, pid) {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, errorcodes.Errorf("daemon_stop_wait_interrupted", "stopped waiting; the daemon finishes its jobs and exits; \"karvi daemon stop --force\" stops it now")
		case <-deadline:
			if leaving {
				return false, errorcodes.Errorf("daemon_stop_timeout", "daemon stop timed out")
			}
			pctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			probe, err := daemon.Probe(pctx, rt.Socket, rt.MaxFrame)
			cancel()
			if err == nil && probe.Status.PID == pid {
				fmt.Fprintf(stderr, "daemon still reports %d active job(s) after %s; forcing stop\n", probe.Status.ActiveJobs, limit)
				return false, nil
			}
			leaving = true
			deadline = time.After(stopTimeout(rt))
		case <-ticker.C:
		}
	}
}

// daemonExited says whether the daemon pid has left the socket: the socket
// removed, the last of its files a daemon removes as it exits, or the process
// gone, killed or exited before another daemon took the socket.
func daemonExited(socket string, pid int) bool {
	if _, err := os.Lstat(socket); errors.Is(err, fs.ErrNotExist) {
		return true
	}
	return pid > 0 && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func stopTimeout(rt app.DaemonRuntime) time.Duration {
	return startTimeout(rt) + time.Duration(rt.Config.Int("daemon.forced-grace-seconds"))*time.Second
}

// startTimeout is daemon.start-timeout, 5 s when unset: the wait for a
// launched daemon's socket and for a drained one to exit.
func startTimeout(rt app.DaemonRuntime) time.Duration {
	if timeout := rt.Config.Duration("daemon.start-timeout"); timeout > 0 {
		return timeout
	}
	return 5 * time.Second
}

// serveDaemon runs the daemon in this process. It takes no context from
// the command line: the CLI's first-signal cancel would end the server
// without accounting, so the server owns SIGTERM and SIGINT itself and
// drains, waits the grace, and accounts before it exits.
func serveDaemon(g globalOptions, stderr io.Writer) int {
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	// The daemon's own load's warnings go to its log, once, at its start.
	g.say = func(_ configload.Snapshot, w string) { logger.Warn("configuration warning", slog.String("warning", w)) }
	rt, code, ok := g.runtime(stderr)
	if !ok {
		return code
	}
	secretVariableNotice(logger)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	server := &daemon.Server{Socket: rt.Socket, StatePath: rt.StatePath, MaxFrame: rt.MaxFrame, MaxJobs: rt.MaxJobs, UID: rt.Operator.UID, Config: rt.Config, Operator: rt.Operator, Logger: logger, Signals: signals, IdleTimeout: rt.Config.Duration("daemon.shutdown-idle-timer")}
	if err := server.Serve(context.Background()); err != nil {
		return g.reportError(stderr, "daemon_serve_failed", err)
	}
	return 0
}

// ownBuild is this executable's build (buildinfo.Build); a test replaces it.
var ownBuild = buildinfo.Build

// ensureDaemon makes a daemon of this build answer at the socket of rt, the
// invocation's runtime, launching one with the global options g when none
// does (EXAMPLES chapter 57, issue 5): its own build, not draining, is used;
// another build, or any daemon draining, is drained at its own IPC schema,
// and one holding no job is waited for and replaced, while one with jobs
// refuses the submission until it has exited. Under daemon.launch =
// "systemd" it drains and launches nothing (systemdDaemon). Every error
// carries a registered code.
func ensureDaemon(ctx context.Context, rt app.DaemonRuntime, g globalOptions, stderr io.Writer) error {
	// A client that cannot read its own build has nothing to compare a
	// daemon with (EXAMPLES chapter 57, issue 2).
	if _, err := ownBuild(); err != nil {
		return errorcodes.Errorf("build_unreadable", "this karvi cannot read its own build: %v; --no-daemon runs without a daemon", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	probe, pingErr := daemon.Probe(pingCtx, rt.Socket, rt.MaxFrame)
	cancel()
	if rt.Systemd() {
		return systemdDaemon(rt, probe, pingErr)
	}
	var mismatch *ipc.SchemaMismatchError
	switch {
	case pingErr == nil && probe.Compatible && probe.Status.Status != "draining":
		return nil
	case pingErr == nil:
		if err := replaceDaemon(ctx, rt, probe, g.quiet, stderr); err != nil {
			return err
		}
	case errors.As(pingErr, &mismatch):
		// A newer schema, which this client cannot speak: the newer karvi
		// stops it.
		return errorcodes.Errorf("daemon_incompatible", "the daemon speaks a newer IPC schema; none submitted; karvi daemon stop from the newer karvi, or --no-daemon\n%s", schemasStacked(mismatch.DaemonSchema, mismatch.ClientSchema))
	}
	exe, err := os.Executable()
	if err != nil {
		return errorcodes.Errorf("karvi_executable_unlocatable", "locate karvi executable: %w", err)
	}
	log, err := os.OpenFile(rt.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return errorcodes.Errorf("daemon_log_open_failed", "open daemon log %s: %w", rt.LogPath, err)
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
		return errorcodes.Errorf("daemon_spawn_failed", "start daemon: %w", err)
	}
	childPID := cmd.Process.Pid // Release clears it
	_ = cmd.Process.Release()
	_ = log.Close()
	wctx, wcancel := context.WithTimeout(ctx, startTimeout(rt))
	defer wcancel()
	if err := ipc.Wait(wctx, rt.Socket); err != nil {
		return fmt.Errorf("daemon_start_failed: daemon failed to start; inspect %s: %w", rt.LogPath, err)
	}
	// The daemon's first answer after its socket appeared: two seconds,
	// since one second lapsed once under a full parallel `go test ./...`
	// on a four-CPU host.
	pctx, pcancel := context.WithTimeout(ctx, 2*time.Second)
	defer pcancel()
	status, err := daemon.Ping(pctx, rt.Socket, rt.MaxFrame)
	if err != nil {
		return errorcodes.Ensure(err, "daemon_start_failed")
	}
	// Two clients launching at once each start a child; the second child
	// finds the socket taken and exits, and the daemon answering is the
	// other client's. Only the client whose own child answers says so.
	if !g.quiet && status.PID == childPID {
		fmt.Fprintf(stderr, "daemon started socket=%s\n", rt.Socket)
	}
	return nil
}

// systemdDaemon is ensureDaemon under daemon.launch = "systemd" (EXAMPLES
// chapter 57, issue 15), from its probe: a daemon of this build, not
// draining, is used; any other daemon, or none, is the unit's to replace or
// start, never the client's, and refuses the submission naming the unit's
// systemctl. Nothing is drained or launched.
func systemdDaemon(rt app.DaemonRuntime, probe daemon.ProbeResult, pingErr error) error {
	var mismatch *ipc.SchemaMismatchError
	switch {
	case pingErr == nil && probe.Status.Status == "draining":
		return errorcodes.Errorf("daemon_draining", "%s; none submitted; retry when it exits, or --no-daemon\n%s", app.JobsRunning(probe.Status.ActiveJobs), buildsStacked(probe.Status))
	case pingErr == nil && !probe.Compatible:
		return errorcodes.Errorf("daemon_incompatible", "the daemon runs another build; none submitted; systemctl restart %s, or --no-daemon\n%s", rt.Unit(), buildsStacked(probe.Status))
	case pingErr == nil:
		return nil
	case errors.As(pingErr, &mismatch):
		return errorcodes.Errorf("daemon_incompatible", "the daemon speaks a newer IPC schema; none submitted; systemctl restart %s, or --no-daemon\n%s", rt.Unit(), schemasStacked(mismatch.DaemonSchema, mismatch.ClientSchema))
	}
	return errorcodes.Errorf("daemon_unreachable", "no daemon running; none submitted; systemctl start %s, or --no-daemon", rt.Unit())
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

// replaceDaemon drains the daemon probe found, of another build or draining,
// at its own IPC schema, and waits up to daemon.start-timeout for it to exit,
// saying so unless quiet, so that the caller starts one of this build
// (EXAMPLES chapter 57, issue 5). A daemon with jobs finishes them and exits
// by itself, killing nothing; until then the submission is refused, as it is
// when the drained daemon has not exited within the wait.
func replaceDaemon(ctx context.Context, rt app.DaemonRuntime, probe daemon.ProbeResult, quiet bool, stderr io.Writer) error {
	pid := probe.Status.PID
	drainCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	drained, err := daemon.Drain(drainCtx, rt.Socket, rt.MaxFrame, probe.DaemonSchema)
	cancel()
	if err != nil {
		if daemonExited(rt.Socket, pid) {
			// It left between the probe and the drain.
			return nil
		}
		return errorcodes.Ensure(err, "daemon_drain_failed")
	}
	if drained.ActiveJobs > 0 {
		return errorcodes.Errorf("daemon_draining", "%s; none submitted; retry when it exits, or --no-daemon\n%s", app.JobsRunning(drained.ActiveJobs), buildsStacked(probe.Status))
	}
	limit := startTimeout(rt)
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for !daemonExited(rt.Socket, pid) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errorcodes.Errorf("daemon_draining", "no job running, not exited after %s; none submitted; karvi daemon stop, or --no-daemon\n%s", limit, buildsStacked(probe.Status))
		case <-ticker.C:
		}
	}
	if !quiet {
		fmt.Fprintf(stderr, "daemon drained version=%s build=%s\n", displayUnknown(probe.Status.Version), displayNone(probe.Status.Build))
	}
	return nil
}

// buildsStacked is the two lines a refusal compares the daemon with this
// client by: the daemon's build over the client's, each with its version
// (EXAMPLES chapter 57, issue 5).
func buildsStacked(s daemon.Status) string {
	return fmt.Sprintf("  daemon build %s %s\n  client build %s %s", displayNone(s.Build), displayUnknown(s.Version), buildinfo.BuildOrNone(), buildinfo.Version)
}

// schemasStacked is the same for a daemon whose IPC schema is not the
// client's: the daemon's schema over the client's.
func schemasStacked(daemonSchema, clientSchema int) string {
	return fmt.Sprintf("  daemon ipc_schema %d\n  client ipc_schema %d", daemonSchema, clientSchema)
}

func displayUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

// displayNone is a build as karvi version names a missing one.
func displayNone(build string) string {
	if build == "" {
		return "none"
	}
	return build
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
