package jobexec

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/dispatch"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/capacity"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/executor"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/icmpgate"
	"github.com/robert-patrick-texas/karvi/internal/metrics"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/internal/transportselect"
	"github.com/robert-patrick-texas/karvi/records"
)

// Run executes the plan and writes every durable artifact of the job. It
// returns the safe activity result; the exit code follows the dispatch
// summary and the output state as before.
func Run(ctx context.Context, req Request, streams IO) ActivityResult {
	if streams.Stdout == nil {
		streams.Stdout = io.Discard
	}
	if streams.Stderr == nil {
		streams.Stderr = io.Discard
	}
	plan := req.Plan
	if len(plan.Targets) == 0 || plan.CommandCount() == 0 {
		return FailedResult("activity_scope_empty", fmt.Errorf("at least one device and one command are required"))
	}
	if err := plan.Verify(); err != nil {
		return FailedResult("plan_digest_mismatch", err)
	}
	cpuProfile := osutil.ApplyGOMAXPROCS()
	if cpuProfile.PreviousGOMAXPROCS != cpuProfile.GOMAXPROCS && !req.Quiet {
		fmt.Fprintf(streams.Stderr, "warning: adjusted GOMAXPROCS from %d to effective CPU count %d\n", cpuProfile.PreviousGOMAXPROCS, cpuProfile.GOMAXPROCS)
	}
	now := time.Now()
	id := req.ActivityID
	if id == "" {
		var err error
		id, err = osutil.NewID(now)
		if err != nil {
			return FailedResult("activity_id_generation_failed", err)
		}
	}
	jobID := ""
	if req.ActivityType == "run" {
		jobID = id
	}
	result := ActivityResult{ActivityID: id, JobID: jobID}
	cfg := req.Config
	// The ICMP gate's capability, once per live job and before anything is
	// written: an enabled gate with no
	// allowed method refuses the job here, so a daemon commit is refused
	// with the code and no device sees one misleading error per target. An
	// exercise reports the capability instead.
	var pinger icmpgate.Pinger
	if plan.Ping.Enabled && req.Mode != executionplan.ModeExercise {
		capability := icmpgate.Detect(icmpgate.Options{Socket: cfg.Bool("network.ping-socket"), System: cfg.Bool("network.ping-system")})
		if !capability.Available {
			return FailedResult("icmp_capability_unavailable", errorcodes.Errorf("icmp_capability_unavailable", "the ICMP gate is enabled but %s", capability.Reason))
		}
		pinger = capability.Pinger()
	}
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), req.Operator.Home, req.Operator.Username)
	if err != nil {
		return FailedResult("base_directory_unavailable", err)
	}
	directoryMode := osutil.DirectoryMode(cfg.String("output.directory-mode"))
	if err := osutil.EnsureStateTree(base, req.Operator.UID, directoryMode); err != nil {
		return FailedResult("state_tree_create_failed", err)
	}
	scratch, err := osutil.ResolveScratch(cfg.String("tempdir"), base, req.Operator.Home, req.Operator.Username, req.Operator.UID)
	if err != nil {
		return FailedResult("scratch_directory_unavailable", err)
	}
	// The spool directory: resolved at
	// admission, so a job that cannot spool fails here, before any device
	// is touched. It is never the scratch directory above: tempdir's chain
	// prefers a tmpfs by design, and a spool must cost disk. The sweep and
	// the free-space rule follow once the job's width is known (below).
	spoolDir, err := osutil.ResolveSpoolDir(cfg.String("spooldir"), req.Operator.Home, req.Operator.UID)
	if err != nil {
		return FailedResult("spool_directory_unavailable", err)
	}
	controlRoot, err := osutil.ControlPathRoot(cfg.String("ssh.control-path-root"), base, req.Operator.Home, req.Operator.Username, req.Operator.UID)
	if err != nil {
		return FailedResult("control_path_root_unavailable", err)
	}
	location, err := display.Location(cfg.String("timezone"))
	if err != nil {
		return FailedResult("config_display_timestamp_invalid", err)
	}
	// The text files' header time is display.timestamp in that zone, as the
	// display's own lines are; both keys were
	// validated at load, so this cannot fail on a loaded configuration.
	formatter, err := display.NewFormatter(cfg.String("display.timestamp"), cfg.String("timezone"))
	if err != nil {
		return FailedResult("config_display_timestamp_invalid", err)
	}
	// The invocation decides the job's files on every path: the plan
	// carries its output.root resolved, its
	// output.files switches, and output.persist-command. The keys that
	// describe this host's disk (directory-mode, fsync, the space preflight)
	// are read from the configuration here, the daemon's on its path.
	// The directory is the one the client reserved under the day folder of
	// the ID's own stamp, so it is derived from
	// the ID and not from this clock: a job accepted across midnight or
	// stamped in another process still lands where its ID says.
	stamped, err := osutil.JobIDTime(id, location)
	if err != nil {
		return FailedResult("job_request_malformed", err)
	}
	artifact := osutil.JobDirectory(plan.Output.Root, id, stamped, location)
	// output.persist-command=false (`--nof`): the activity runs
	// and displays, and no job folder and no output file is created. The
	// store skips every file and is otherwise the same, so the records are
	// validated, counted, and rendered from memory as they always are, and
	// on the daemon's path they reach the follower over the socket. The
	// audit log and the scoreboard are not the job's output files and stay
	// as configured.
	skip := skippedFiles(plan.Output)
	// shownArtifact is the footer's <artifacts>: the folder, or "none", so
	// that the line does not end in a bare "artifacts=" that reads as a
	// fault. The result's ArtifactDir is empty: there is no folder.
	shownArtifact := artifact
	// Every file skipped, by --nof or by all eight output.files keys, is one
	// case: no folder, so none is named.
	if skip == output.AllFiles {
		artifact, shownArtifact = "", "none"
	}
	result.ArtifactDir = artifact
	if artifact != "" {
		// The reserved directory is claimed empty; one
		// holding another job's files refuses this job before a byte is
		// written.
		if err := osutil.ClaimJobDirectory(artifact, directoryMode); err != nil {
			return FailedResult("output_directory_in_use", err)
		}
	}
	warn := func(msg string) {
		if streams.Stderr != nil {
			fmt.Fprintf(streams.Stderr, "warning: %s\n", msg)
		}
	}
	// A crun's directory is checked once, before any device is contacted,
	// with the runner's crun.directory-mode for a missing one.
	var collection *output.CollectionOptions
	if c := plan.Output.Collection; c != nil {
		if err := osutil.EnsureCollectionDirectory(c.Directory, osutil.DirectoryMode(cfg.String("crun.directory-mode"))); err != nil {
			return FailedResult("crun_directory_not_writable", err)
		}
		collection = &output.CollectionOptions{Directory: c.Directory, FileMode: osutil.FileMode(c.FileMode), Filters: plan.PlatformFilters, Suffix: c.Suffix}
	}
	debug := DebugLogger(req.Debug, cfg, streams.Stderr)
	serverLimit := cfg.Int("dispatch.server-max-inflight")
	cpu := osutil.EffectiveCPU()
	if serverLimit == 0 {
		serverLimit = minInt(256, maxInt(32, 8*cpu.EffectiveCPUs))
	}
	// logEvent carries an operational event off the client's standard
	// error: into the daemon's log when the daemon runs the job, else onto
	// the debug stream in process.
	logEvent := func(level slog.Level, code, msg string) {
		if req.Logger != nil {
			req.Logger.Log(ctx, level, msg, slog.String("code", code), slog.String("job_id", id))
			return
		}
		debug(code + ": " + msg)
	}
	// The spool directory's sweep, then the one free-space check of
	// admission:
	// the places this job writes, grouped by volume and read once each
	// under `freecheck`. The output root asks its finished size (the device
	// estimate for each of its copies), a crun's collection directory one
	// copy, the spool directory the width in flight × the command limit, and
	// every volume the floor once. The width the job would run at is the
	// smaller of its workers (the wave's maximum, or 1 for a serial job),
	// the server's cap, and its device count; `auto` narrows that width to
	// what fits above the fixed terms and says so; `never` reads nothing.
	// The dispatch settings the job runs under take the narrowed width; the
	// plan and its manifest stay as committed.
	output.SweepSpools(spoolDir, func(name string) {
		logEvent(slog.LevelInfo, "spool_abandoned_removed", "removed the abandoned spool "+filepath.Join(spoolDir, name))
	})
	osutil.SweepControlSockets(controlRoot, func(name string) {
		logEvent(slog.LevelInfo, "control_socket_abandoned_removed", "removed the abandoned control socket "+filepath.Join(controlRoot, name))
	})
	dispatchSettings := plan.Dispatch
	expected, multiplier := cfg.Int64("output.expected-bytes-per-device"), cfg.Float("output.reserve-multiplier")
	var places []output.Place
	if artifact != "" {
		places = append(places, output.Place{Path: artifact, Need: output.FinishedSize(expected, len(plan.Targets), skip.OutputCopies(), multiplier)})
	}
	if collection != nil {
		places = append(places, output.Place{Path: collection.Directory, Need: output.FinishedSize(expected, len(plan.Targets), 1, multiplier)})
	}
	places = append(places, output.Place{Path: spoolDir, Spool: true})
	freecheck := cfg.String("freecheck")
	admission, err := output.Preflight{Check: freecheck, Floor: cfg.Int64("output.min-free-bytes-after-job"), Places: places, Limit: cfg.Int64("output.max-command-bytes"), Width: jobWidth(dispatchSettings, serverLimit, len(plan.Targets))}.Run()
	if err != nil {
		return FailedResult("output_preflight_space", err)
	}
	// admissionWarnings ride the daemon's receipt and follow start to the
	// client, since the daemon's job has no standard error.
	var admissionWarnings []string
	if admission.Warning != "" {
		warn(admission.Warning)
		logEvent(slog.LevelWarn, "spool_width_narrowed", admission.Warning)
		admissionWarnings = append(admissionWarnings, admission.Warning)
		dispatchSettings = narrowDispatch(dispatchSettings, admission.Width)
	}
	debug(fmt.Sprintf("spooldir=%s freecheck=%s volumes=%d width=%d", spoolDir, freecheck, admission.Volumes, admission.Width))
	// The store opens the job's files only once every volume has passed
	// (a failed preflight leaves the reserved folder empty, as the
	// reservation left it).
	store, err := output.Create(output.Options{
		// The daemon's followers take each record's notice from the store,
		// in sequence.
		Warn: warn, Skip: skip, OnDurable: req.OnDurable, Timestamp: formatter.Timestamp,
		Root: artifact, ID: id, CropNames: plan.Output.CropToDot, Collection: collection, DirectoryMode: directoryMode, Fsync: cfg.Bool("output.fsync-command-records"),
		MaxJobBytes: plan.Output.MaxJobBytes,
	})
	if err != nil {
		return FailedResult("output_store_create_failed", err)
	}
	defer store.Close()
	if err := store.WriteCommands(plan.Commands); err != nil {
		return FailedResult("output_commands_write_failed", err)
	}
	if err := store.WriteCommandLists(plan.PlatformCommands); err != nil {
		return FailedResult("output_commands_write_failed", err)
	}
	auditSink, err := audit.New(cfg)
	if err != nil {
		return FailedResult("audit_sink_open_failed", err)
	}
	defer auditSink.Close()
	for _, w := range cfg.Warnings {
		warn(w)
	}
	debug(fmt.Sprintf("activity=%s activity_id=%s plan_id=%s plan_digest=%s config_digest=%s config_sources=%q", req.ActivityType, id, plan.PlanID, plan.PlanDigest, cfg.Digest, strings.Join(cfg.Sources, ",")))
	capManager, err := capacity.New(cfg.String("sessions.shared-capacity-root"), filepath.Join(base, "state", "capacity"), id, serverLimit, warn)
	if err != nil {
		return FailedResult("shared_capacity_unavailable", err)
	}
	capManager.PollMin = cfg.Duration("dispatch.admission-poll-min")
	capManager.PollMax = cfg.Duration("dispatch.admission-poll-max")
	sampler := metrics.New(id, cfg.Duration("metrics.process-sample-interval"), cfg.Duration("dispatch.wave-cpu-half-life"), cfg.Int("dispatch.wave-cpu-warmup-samples"), cfg.Float("dispatch.wave-cpu-threshold-percent"), cfg.Float("dispatch.wave-cpu-target-zone-percent"))
	sampler.Start(ctx)
	scoreboardWriter, err := scoreboard.NewWriter(cfg.String("watch.directory"), filepath.Join(base, "state", "scoreboards"), cfg.Bool("watch.enabled"), warn)
	if err != nil {
		return FailedResult("scoreboard_directory_unavailable", err)
	}
	// The run's or cmd's ID was reserved by its job directory.
	scoreboardWriter.Use(id)
	producer := producerInfo()
	// The scoreboard's schema 2 state: the
	// targets with their states, the inputs and the commands from the plan,
	// the metrics and the collection counts from the store at each write.
	// The scoreboard's first width is the one the job starts at (one for
	// serial, the pool, the wave's start), after any narrowing above.
	board := newScoreboardState(req, id, jobID, now, producer, store, buildPlan(cfg, req.ActivityType, dispatchSettings, nil).StartWidth())
	initial := board.snapshot()
	if err := scoreboardWriter.Write(initial); err != nil {
		return FailedResult("scoreboard_write_failed", err)
	}
	manifest, err := buildManifest(req, id, now)
	if err != nil {
		return FailedResult("output_manifest_write_failed", err)
	}
	if err := store.WriteManifest(manifest); err != nil {
		return FailedResult("output_manifest_write_failed", err)
	}
	if err := writeActivityAudit(auditSink, req, id, jobID, "started", "started", 0, ""); err != nil {
		return FailedResult("audit_write_failed", err)
	}
	if req.OnAccepted != nil {
		req.OnAccepted(artifact, admissionWarnings)
	}
	if req.Mode == executionplan.ModeExercise {
		// The exercise branch: everything above is
		// the accepted job; nothing below it is built.
		return runExercise(ctx, req, exerciseState{id: id, jobID: jobID, artifact: artifact, started: now, store: store, auditSink: auditSink, scoreboardWriter: scoreboardWriter, initial: initial, capManager: capManager, sampler: sampler, base: base})
	}
	renderer, err := newRecordRenderer(streams.Stdout, plan.Output.Format, cfg, req.Quiet, req.Debug, id, shownArtifact, req.ActivityType, plan.Output.Echo, plan.Output.DynamicBorder, plan.Output.NoBorder)
	if err != nil {
		return FailedResult("config_display_border_invalid", err)
	}
	// The daemon's job has no reader of its standard output: the renderer
	// keeps the counts and formats nothing.
	renderer.daemon = req.Daemon
	devExec := executor.New(executor.Options{
		Config: cfg, Operator: req.Operator, ActivityID: id, JobID: jobID, ActivityType: req.ActivityType,
		Commands: plan.Commands, PlatformCommands: plan.PlatformCommands, BlindReturns: plan.BlindReturns, BlindWait: time.Duration(plan.BlindWaitNS), Blind: plan.Blind, Expectations: plan.Expectations,
		SessionInit: plan.SessionInit, CandidateCount: req.CandidateCount, DispatchOrder: plan.Dispatch.DispatchOrder, ShuffleKey: plan.Dispatch.ShuffleKey,
		Grants: req.Grants, Protection: req.Protection, HaltOnCommandError: cfg.Bool("execution.halt-device-on-command-error") && !plan.Dispatch.ContinueDeviceOnError,
		Ping: plan.Ping, Pinger: pinger,
		Capacity: capManager, Store: store, Audit: auditSink, Metrics: sampler,
		ScratchDir: scratch, ControlRoot: controlRoot, Home: req.Operator.Home, BaseDir: base, SpoolDir: spoolDir, InFlight: board.inFlight,
		OnRecord: renderer.OnRecordFrom, Warn: warn, Debug: debug,
	})
	tasks := make([]dispatch.Task, len(plan.Targets))
	queued := time.Now()
	for i, t := range plan.Targets {
		tasks[i] = dispatch.Task{Key: t.TargetID, Position: i + 1, Value: executor.Work{Target: t, QueuedAt: queued}}
	}
	dispatchPlan := buildPlan(cfg, req.ActivityType, dispatchSettings, tasks)
	eventSink := dispatch.EventSinkFunc(func(e dispatch.Event) {
		if e.Kind == "wave_decision" {
			sampler.RecordWave(e.Wave, e.PreviousWidth, e.Width, e.Reason)
		}
		board.apply(e, now)
		if err := scoreboardWriter.Write(board.snapshot()); err != nil {
			renderer.SetError("scoreboard_write_failed", err)
		}
	})
	// The heartbeat: the snapshot rewritten every
	// watch.refresh while the devices run, carrying the running byte count.
	stopHeartbeat := scoreboardWriter.Heartbeat(cfg.Duration("watch.refresh"), board.snapshot)
	summary := (dispatch.LocalDispatcher{Signal: sampler}).Execute(ctx, dispatchPlan, devExec, eventSink)
	stopHeartbeat()
	seen := map[int]bool{}
	for _, r := range summary.Results {
		seen[r.Task.Position] = true
	}
	// Each reason is a registered code; run_halted is the fallback.
	// Under a shutdown cause every unfinished unit is incomplete_shutdown
	// and the halt reason names the cause.
	shutdown := ShutdownCause(ctx)
	unstartedStatus, unstartedReason := "not_started_halt", "run_halted"
	var unstartedCause error
	switch {
	case shutdown != nil:
		unstartedStatus, unstartedReason = "incomplete_shutdown", "shutdown_incomplete"
		summary.HaltReason = errorcodes.Of(shutdown)
	case summary.GateReason == "error_count":
		unstartedStatus, unstartedReason = "not_started_wave_gate", "wave_gate_error_count"
	case summary.GateReason == "error_percent":
		unstartedStatus, unstartedReason = "not_started_wave_gate", "wave_gate_error_percent"
	case summary.GateReason != "":
		unstartedStatus = "not_started_wave_gate"
	case summary.HaltReason == "cancelled" || ctx.Err() != nil:
		unstartedStatus, unstartedReason, unstartedCause = "cancelled", "cancelled", CancelCause(ctx)
	case summary.HaltReason == "host_key_mismatch":
		unstartedReason = "halt_host_key_mismatch"
	case summary.HaltReason == "error_count":
		unstartedReason = "halt_error_count"
	case summary.HaltReason == "error_percent":
		unstartedReason = "halt_error_percent"
	case summary.HaltReason == "invalid_dispatch_mode":
		unstartedReason = "halt_invalid_dispatch_mode"
	}
	for _, task := range tasks {
		if seen[task.Position] {
			continue
		}
		work := task.Value.(executor.Work)
		devExec.EmitUnstarted(work, dispatch.Context{Mode: plan.Dispatch.Mode, ScopePosition: task.Position, DesiredWidth: summary.FinalWidth}, unstartedStatus, unstartedReason, unstartedCause)
	}
	if err := store.Close(); err != nil {
		renderer.SetError("output_store_close_failed", err)
	}
	// A record that could not be appended (output.max-job-bytes, a full
	// disk) is the job's output failure, said on standard error and in the
	// summary's causes; the device's own result carries the code nowhere.
	if err := store.Err(); err != nil {
		renderer.SetError("output_write_failed", err)
	}
	incomplete, cancelled := terminalCounts(&summary, shutdown, ctx.Err() != nil)
	exit := determineExit(req.ActivityType, summary, renderer.Error(), ctx.Err(), shutdown)
	ended := time.Now()
	finalStatus := finalScoreboardStatus(exit, summary)
	board.finishQueued(unstartedStatus)
	finalSnap := board.snapshot()
	finalSnap.Status = finalStatus
	finalSnap.LastUpdatedAt, finalSnap.EndedAt, finalSnap.ElapsedNS = ended, &ended, ended.Sub(now).Nanoseconds()
	finalSnap.WaveNumber, finalSnap.Width = summary.Waves, maxInt(1, summary.FinalWidth)
	finalSnap.Counts = records.Counts{Total: len(plan.Targets), Completed: summary.Counts.Terminal, Succeeded: summary.Counts.Succeeded, Failed: summary.Counts.Failed, NotStarted: summary.Counts.NotStarted, Incomplete: incomplete, Cancelled: cancelled}
	if summary.HaltReason != "" || summary.GateReason != "" {
		finalSnap.Halt = map[string]any{"halt_reason": summary.HaltReason, "gate_reason": summary.GateReason}
	}
	if summary.Counts.Failed > 0 {
		finalSnap.ErrorSummary = map[string]any{"code": "device_errors", "category": "device", "count": summary.Counts.Failed}
	}
	if err := scoreboardWriter.Write(finalSnap); err != nil && exit == exitcode.ExitSuccess {
		exit = exitcode.ExitOutputFailure
		renderer.SetError("scoreboard_write_failed", err)
	}
	pingSummary := devExec.PingSummary()
	metric := sampler.Final(map[string]any{"devices": len(plan.Targets), "commands": plan.CommandCount(), "dispatch": plan.Dispatch.Mode, "output_bytes": store.Bytes(), "ping": pingSummary}, renderer.ErrorCounts(), map[string]any{"bytes": store.Bytes()}, []string{"engineering-preview build does not yet expose full admission fairness histograms"})
	if err := store.WriteMetrics(metric); err != nil && exit == exitcode.ExitSuccess {
		exit = exitcode.ExitOutputFailure
		renderer.SetError("output_metrics_write_failed", err)
	}
	recordSummary := buildSummary(req, store.Paths(), id, jobID, now, ended, exit, summary, incomplete, cancelled, cancellationOf(ctx), shutdown, renderer, auditSink.Status(), metric.Bottleneck, pingSummary)
	recordSummary.Collection = store.CollectionSummary()
	if err := store.WriteSummary(recordSummary); err != nil {
		exit = exitcode.ExitOutputFailure
		renderer.SetError("output_summary_write_failed", err)
		recordSummary.ExitCode, recordSummary.ExitName = exit, exitcode.ExitName(exit)
	}
	completedExtra := []map[string]any{}
	if c := cancellationOf(ctx); c != nil {
		completedExtra = append(completedExtra, map[string]any{"cancellation": c.AuditDetails()})
	}
	if err := writeActivityAudit(auditSink, req, id, jobID, "completed", finalStatus, exit, summary.HaltReason+summary.GateReason, completedExtra...); err != nil && exit == exitcode.ExitSuccess {
		exit = exitcode.ExitOutputFailure
		recordSummary.ExitCode, recordSummary.ExitName = exit, exitcode.ExitName(exit)
		_ = store.WriteSummary(recordSummary)
	}
	// Render the operator-facing footer only after durable finalization has
	// completed so its exit-status field reflects the process result rather
	// than an earlier execution-only result. A footer write failure is itself
	// an output failure; update the durable summary and scoreboard best-effort
	// so machine consumers still receive the authoritative outcome.
	// The display's end: the footer with this process's exit and elapsed
	// time; under jsonl a run's summary as the stream's last line, as the
	// daemon path ends.
	endDisplay := func() error {
		if req.ActivityType == "run" && renderer.format == "jsonl" {
			return renderer.finish(&recordSummary)
		}
		return renderer.WriteFooter(ended, exit, ended.Sub(now), recordSummary.Collection)
	}
	if err := endDisplay(); err != nil {
		exit = exitcode.ExitOutputFailure
		renderer.SetError("terminal_write_failed", err)
		finalStatus = finalScoreboardStatus(exit, summary)
		finalSnap.Status = finalStatus
		finalSnap.LastUpdatedAt = time.Now()
		recordSummary.ExitCode, recordSummary.ExitName = exit, exitcode.ExitName(exit)
		_ = scoreboardWriter.Write(finalSnap)
		_ = store.WriteSummary(recordSummary)
	}
	result.ExitCode, result.ExitName, result.Summary = exit, exitcode.ExitName(exit), recordSummary
	if renderer.Error() != nil {
		result.Error = errorcodes.Message(renderer.Error())
	} else if req.ActivityType == "command" && exit != exitcode.ExitSuccess {
		// A direct command has no later summary screen to make a device failure
		// visible.  Preserve --quiet as "no routine narration", not "suppress
		// errors", and return the first stable device error code to the CLI.
		// The complete structured details remain in commands.jsonl.
		for _, deviceResult := range summary.Results {
			if !deviceResult.Success && deviceResult.ErrorCode != "" {
				result.Error = fmt.Sprintf("%s: command failed for %s", deviceResult.ErrorCode, deviceResult.Task.Key)
				break
			}
		}
	}
	return result
}

// Policy is the daemon's execution policy from its own configuration,
// with its digest.
func Policy(cfg configload.Snapshot) (records.ExecutionPolicy, error) {
	p := records.ExecutionPolicy{HostKeyPolicy: cfg.String("ssh.host-key-policy"), KnownHostsFile: cfg.String("ssh.known-hosts-file"), HaltOnHostKeyMismatch: cfg.Bool("ssh.halt-run-on-host-key-mismatch"), AllowTelnet: cfg.Bool("security.allow-telnet")}
	sum, err := p.Sum()
	if err != nil {
		return p, err
	}
	p.Digest = sum
	return p, nil
}

// buildManifest is the version 2 manifest: typed
// around the header, the plan, the package projection, and the policy.
func buildManifest(req Request, id string, accepted time.Time) (records.Manifest, error) {
	policy, err := Policy(req.Config)
	if err != nil {
		return records.Manifest{}, err
	}
	initial := make([]records.InitialState, 0, len(req.Plan.Targets)*req.Plan.CommandCount())
	for _, t := range req.Plan.Targets {
		for i := range req.Plan.CommandsFor(t.Device.Platform) {
			initial = append(initial, records.InitialState{DeviceID: t.TargetID, CommandIndex: i + 1, State: "not_started"})
		}
	}
	selection := req.Selection
	if selection.Inputs == nil {
		selection.Inputs = []records.TargetInput{}
	}
	if selection.Excludes == nil {
		selection.Excludes = []string{}
	}
	if selection.AddressAuthorities == nil {
		selection.AddressAuthorities = []string{}
	}
	mode := req.Mode
	if mode == "" {
		mode = executionplan.ModeLive
	}
	return records.Manifest{
		SchemaVersion: records.JobSchemaVersion, JobID: req.Header.JobID, ActivityID: id, AcceptedAt: accepted,
		Operator: osutil.RecordOperator(req.Operator), App: map[string]any{"build": buildinfo.Current()}, Mode: string(mode),
		Header: req.Header, Plan: req.Plan, CredentialPackage: req.Package, Policy: policy, Selection: selection, InitialStates: initial,
	}, nil
}

// skippedFiles is the files a job does not write, from the plan's output
// settings: each output.files switch that
// is false, or every file when output.persist-command is false (`--nof`).
// All eight false and persist false are one value, output.AllFiles, for
// which the store makes no folder.
func skippedFiles(o executionplan.OutputSettings) output.FileSet {
	if !o.Persist {
		return output.AllFiles
	}
	f := o.Files
	return output.FileSet{
		CommandsJSONL: !f.CommandsJSONL, CommandsTxt: !f.CommandsTxt, ErrorsJSONL: !f.ErrorsJSONL, FailedDevicesTxt: !f.FailedDevicesTxt,
		ManifestJSON: !f.ManifestJSON, MetricsJSON: !f.MetricsJSON, SummaryJSON: !f.SummaryJSON, OutputTxt: !f.OutputTxt,
	}
}

// summaryFiles is the summary's "paths" and "output" for a job's store:
// only the files this job writes (output.Paths leaves a skipped file's path
// empty), so no reader is sent to a file that will not exist. The devices'
// output.TARGET.txt files are not listed: there is one per device and the
// name follows from the device's (output.TextFileName).
func summaryFiles(paths output.Paths, outputBytes int64) (files map[string]string, out map[string]any) {
	files = map[string]string{}
	for key, path := range map[string]string{"commands_jsonl": paths.CommandsJSONL, "commands_txt": paths.CommandsText, "errors_jsonl": paths.ErrorsJSONL, "failed_devices": paths.FailedDevices, "metrics": paths.Metrics, "summary": paths.Summary, "manifest": paths.Manifest} {
		if path != "" {
			files[key] = path
		}
	}
	out = map[string]any{"bytes": outputBytes}
	if paths.CommandsJSONL != "" {
		out["commands_jsonl"] = paths.CommandsJSONL
	}
	return files, out
}

func buildSummary(req Request, paths output.Paths, id, jobID string, start, end time.Time, code int, d dispatch.Summary, incomplete, cancelled int, cancellation *records.Cancellation, shutdown error, rr *recordRenderer, auditStatus audit.Status, bottleneck map[string]any, ping *records.PingSummary) records.Summary {
	causes := []string{}
	switch {
	case shutdown != nil:
		causes = append(causes, "shutdown:"+errorcodes.Of(shutdown))
	case d.HaltReason != "":
		causes = append(causes, "halt:"+d.HaltReason)
	}
	if d.GateReason != "" {
		causes = append(causes, "gate:"+d.GateReason)
	}
	if rr.Error() != nil {
		causes = append(causes, "output:"+errorcodes.Message(rr.Error()))
	}
	if len(causes) == 0 && d.Counts.Failed > 0 {
		causes = append(causes, "device_failures")
	}
	authorities := map[string]int{}
	for _, t := range req.Plan.Targets {
		authorities[string(t.AddressPlan.Authority)]++
	}
	mode := req.Mode
	if mode == "" {
		mode = executionplan.ModeLive
	}
	files, out := summaryFiles(paths, rr.OutputBytes())
	return records.Summary{SchemaVersion: records.JobSchemaVersion, JobID: jobID, ActivityID: id, StartedAt: start, EndedAt: end, DurationNS: end.Sub(start).Nanoseconds(), FinalStatus: finalScoreboardStatus(code, d), ExitCode: code, ExitName: exitcode.ExitName(code), TerminalCauses: causes, DispatchOrder: req.Plan.Dispatch.DispatchOrder, ShuffleKey: req.Plan.Dispatch.ShuffleKey,
		PlanID: req.Plan.PlanID, PlanDigest: req.Plan.PlanDigest.String(), Mode: string(mode), AddressAuthorityCounts: authorities, Ping: ping, Cancellation: cancellation,
		DeviceCounts: map[string]int{"total": d.Counts.Total, "completed": d.Counts.Terminal, "succeeded": d.Counts.Succeeded, "failed": d.Counts.Failed, "not_started": d.Counts.NotStarted, "incomplete": incomplete, "cancelled": cancelled}, RequestedCommandCounts: rr.RequestedCounts(), SessionInitCounts: rr.SessionInitCounts(), Halt: map[string]any{"run_wide": d.HaltReason, "wave_gate": d.GateReason}, Output: out, AuditSinkStatus: map[string]any{"journald": auditStatus.Journald, "file": auditStatus.File, "warnings": auditStatus.Warnings}, Bottleneck: bottleneck, Recovery: map[string]any{"status": "not_required"}, Paths: files}
}

func writeActivityAudit(s *audit.Sink, req Request, id, jobID, event, outcome string, code int, reason string, extra ...map[string]any) error {
	eventID, _ := osutil.NewID(time.Now())
	telnet := false
	for _, t := range req.Plan.Targets {
		if t.Device.Transport == transportselect.KindTelnet {
			telnet = true
		}
	}
	return s.WriteAudit(records.AuditRecord{SchemaVersion: 1, EventID: eventID, EventName: req.ActivityType + "." + event, Timestamp: time.Now(), Outcome: outcome, Severity: func() string {
		if code == 0 {
			return "info"
		}
		return "warning"
	}(), Operator: osutil.RecordOperator(req.Operator), Process: map[string]any{"pid": os.Getpid(), "executable": buildinfo.AppName, "version": buildinfo.Version, "host": hostname()}, ActivityID: id, JobID: jobID, Action: map[string]any{"mode": req.ActivityType, "operation": "command_sequence", "command_count": len(req.Plan.Commands), "command_sha256_array": commandHashes(req.Plan.Commands), "selector_digest": digestJSON(req.Selection), "plan_id": req.Plan.PlanID, "plan_digest": req.Plan.PlanDigest.String()}, Policy: map[string]any{"config_digest": req.Config.Digest, "telnet": telnet, "fips_required": req.Config.Bool("security.require-fips"), "ssh_host_key_policy": req.Config.String("ssh.host-key-policy"), "ssh_halt_run_on_host_key_mismatch": req.Config.Bool("ssh.halt-run-on-host-key-mismatch")}, Result: map[string]any{"code": exitcode.ExitName(code), "exit_code": code, "reason": reason}, Source: map[string]any{"client": "karvi"}, Details: auditDetails(len(req.Plan.Targets), extra...)})
}

func auditDetails(devices int, extra ...map[string]any) map[string]any {
	d := map[string]any{"device_count": devices}
	for _, m := range extra {
		for k, v := range m {
			d[k] = v
		}
	}
	return d
}

// FailedResult reports err under the site code that names its cause, unless
// err carries a more specific registered code; the exit comes from the
// registry.
func FailedResult(code string, err error) ActivityResult {
	err = errorcodes.Ensure(err, code)
	return ActivityResult{ExitCode: errorcodes.ExitAt(err, code), ExitName: exitcode.ExitName(errorcodes.ExitAt(err, code)), Error: errorcodes.Message(err)}
}
