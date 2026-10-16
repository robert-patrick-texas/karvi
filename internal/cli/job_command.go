package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/watchui"
	"github.com/robert-patrick-texas/karvi/records"
)

// The job command word: the list of jobs, and operations on one accepted
// job, named by the job ID of its receipt. cancel moved verbatim from
// daemon.

// jobListLast is job list's count without --last: twenty rows and the
// heading fit a terminal of 24 lines (EXAMPLES chapter 61, issue 1).
const jobListLast = 20

// jobList is karvi job list [--last N] [--failed] [--target NAME]...
// [--operator NAME]... [--format text|json]: the jobs of the output root
// from their folders' summaries, newest first, every filter judging a job
// before the count (app.ListJobs). The text is watch's table fitted to the
// terminal (watchui.ListTable), the JSON an array of the rows. It needs no
// daemon and no credential.
func jobList(inv *Invocation, streams app.IO) int {
	g := inv.Global
	last := jobListLast
	if inv.Set(optLast) {
		last = inv.Int(optLast)
	}
	if last < 0 {
		return g.usageError(streams.Stderr, "cli_option_value_invalid", "--last takes 0 or more, not %d", last)
	}
	var targets []string
	for _, t := range inv.Targets {
		targets = append(targets, t.Value)
	}
	if err := app.CheckJobListTargets(targets); err != nil {
		return g.reportError(streams.Stderr, "inventory_positive_selector_missing", err)
	}
	common, code, ok := g.read(g.flags(), streams.Stderr)
	if !ok {
		return code
	}
	entries, err := app.ListJobs(common, app.JobListOptions{Last: last, Failed: inv.Flag(optFailed), Targets: targets, Operators: inv.Strings(optOperator)})
	if err != nil {
		return g.reportError(streams.Stderr, "output_root_unavailable", err)
	}
	if inv.String(optFormatTJ) == "json" {
		b, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Fprintf(streams.Stdout, "%s\n", b)
		return 0
	}
	if len(entries) == 0 {
		fmt.Fprintln(streams.Stdout, "(no jobs)")
		return 0
	}
	cfg := common.Config
	table := watchui.ListTable{Rows: listRows(entries), ByDevice: len(targets) > 0, Width: jobexec.DisplayTerminalWidth(streams.Stdout),
		Colour: display.ColorEnabled(cfg.String("display.color"), cfg.String("display.theme"), jobexec.DisplayDestination(streams.Stdout)),
		Theme:  display.EffectiveTheme(cfg.String("display.theme")), Colors: displayColors(cfg)}
	for _, line := range table.Lines() {
		fmt.Fprintln(streams.Stdout, line)
	}
	return 0
}

// listRows are the table's rows from the list's: an unfinished job's
// status unfinished and its counts but the total not known (a folder
// without a summary's total and kind not known either), a finished
// job's from its summary's device_counts (an exercise's devices all
// done), and under --target the row's device with its outcome.
func listRows(entries []app.JobListEntry) []watchui.ListRow {
	rows := make([]watchui.ListRow, len(entries))
	for i, e := range entries {
		s := e.Summary
		r := watchui.ListRow{JobID: e.JobID, Invalid: e.Err != nil, NoSummary: e.NoSummary, Path: e.SummaryPath, Operator: e.Name(), Kind: s.Kind, Status: "unfinished", Total: len(s.Devices)}
		if e.Finished {
			r.Status, r.Finished, r.Duration = s.FinalStatus, true, time.Duration(s.DurationNS)
			if n, ok := s.DeviceCounts["total"]; ok {
				r.Total = n
			}
			r.Completed = r.Total
			if n, ok := s.DeviceCounts["completed"]; ok {
				r.Completed = n
			}
			r.Fails = s.DeviceCounts["failed"]
		}
		for _, d := range s.Devices {
			r.Devices = append(r.Devices, d.Name)
		}
		if e.Device != nil {
			r.Device, r.Outcome = e.Device.Name, e.Device.Outcome
			if !e.Finished {
				r.Outcome = "unfinished"
			}
		}
		rows[i] = r
	}
	return rows
}

// jobCancel is karvi job cancel JOB-ID [--reason TEXT] [--follow]
// [--format text|json]: one cancel_job to the same-UID daemon, never
// launching one. Both answers are results and exit
// 0; --follow then waits for the job's terminal and exits with the job's
// own exit. An interrupt during that wait ends only the wait; the cancel
// already stands in the daemon.
func jobCancel(ctx context.Context, inv *Invocation, streams app.IO) int {
	g := inv.Global
	jobID := inv.Positional[0]
	format := inv.String(optFormatTJ)
	if err := app.CheckJobID(jobID); err != nil {
		return g.reportError(streams.Stderr, "job_request_malformed", err)
	}
	rt, code, ok := g.runtime(streams.Stderr)
	if !ok {
		return code
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	probe, err := daemon.Probe(probeCtx, rt.Socket, rt.MaxFrame)
	probeCancel()
	if err := otherSchema("cancel", probe, err); err != nil {
		return g.reportError(streams.Stderr, "daemon_incompatible", err)
	}
	if err != nil {
		return g.reportError(streams.Stderr, "daemon_unreachable", fmt.Errorf("daemon not running or unreadable, so there is nothing to cancel: %w", err))
	}
	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	res, err := daemon.CancelJob(callCtx, rt.Socket, rt.MaxFrame, ipc.CancelRequest{JobID: jobID, Reason: inv.String(optReason)})
	callCancel()
	if err != nil {
		return g.reportError(streams.Stderr, "daemon_unreachable", err)
	}
	if format == "json" {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Fprintf(streams.Stdout, "%s\n", b)
	} else if !g.quiet {
		fmt.Fprintln(streams.Stdout, cancelResultLine(res))
	}
	if res.State != ipc.CancelStateCancelling || !inv.Flag(optFollow) {
		return 0
	}
	terminal, err := app.FollowJobToTerminal(ctx, rt, jobID, res.ArtifactDir)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(streams.Stderr, app.InterruptedLine(jobID, res.ArtifactDir, "continues cancelling in the daemon"))
			return exitcode.ExitCancelled
		}
		return g.reportError(streams.Stderr, "follow_stream_lost", err)
	}
	o := terminal.Outcome
	if format == "json" {
		b, _ := json.MarshalIndent(o, "", "  ")
		fmt.Fprintf(streams.Stdout, "%s\n", b)
	} else if !g.quiet {
		if c := o.Summary.CancelledBy(); c != nil {
			fmt.Fprintf(streams.Stdout, "%s (exit %d)\n", app.CancelledLine(jobID, c.Reason, res.ArtifactDir), o.ExitCode)
		} else {
			fmt.Fprintf(streams.Stdout, "job %s ended: %s (exit %d); artifacts %s\n", jobID, o.Summary.FinalStatus, o.ExitCode, res.ArtifactDir)
		}
	}
	return o.ExitCode
}

// otherSchema is follow's and cancel's refusal of a daemon whose IPC schema
// is not the client's, and nil for any other probe (EXAMPLES chapter 57,
// issue 6): the verbs use a daemon of any version or build at the client's
// schema, and name the karvi that speaks another's, an older daemon's by the
// version it answered with.
func otherSchema(verb string, probe daemon.ProbeResult, probeErr error) error {
	var mismatch *ipc.SchemaMismatchError
	switch {
	case probeErr == nil && probe.DaemonSchema != probe.ClientSchema:
		return errorcodes.Errorf("daemon_incompatible", "the daemon speaks an older IPC schema; %s its jobs with karvi %s\n%s", verb, displayUnknown(probe.Status.Version), schemasStacked(probe.DaemonSchema, probe.ClientSchema))
	case errors.As(probeErr, &mismatch):
		return errorcodes.Errorf("daemon_incompatible", "the daemon speaks a newer IPC schema; %s its jobs with the newer karvi\n%s", verb, schemasStacked(mismatch.DaemonSchema, mismatch.ClientSchema))
	}
	return nil
}

// cancelResultLine renders the two cancel answers.
func cancelResultLine(res ipc.CancelResult) string {
	if res.State == ipc.CancelStateTerminal && res.Outcome != nil {
		return fmt.Sprintf("job %s already ended: %s (exit %d); artifacts %s", res.JobID, res.Outcome.Summary.FinalStatus, res.Outcome.ExitCode, res.ArtifactDir)
	}
	return fmt.Sprintf("cancel requested: job %s; artifacts %s", res.JobID, res.ArtifactDir)
}

// jobFollow is karvi job follow JOB-ID [--format text|jsonl|json] [--echo]
// [--border | --noborder]: the whole
// job from the zero cursor through the run's own follow loop, rendered as
// the foreground run renders it, ending with the footer and a collection's
// line; the exit is
// the job's own. The daemon serves the job when it holds it; otherwise the
// job's directory, derived from the ID, is followed to the job's end
// (app.FollowJobDirectory). The verb never launches a daemon.
func jobFollow(ctx context.Context, inv *Invocation, streams app.IO) int {
	g := inv.Global
	jobID := inv.Positional[0]
	format := inv.String(optFormat)
	if format == "" {
		format = "text"
	}
	render := app.FollowRenderOptions{Format: format, Echo: inv.Flag(optEcho), DynamicBorder: inv.Flag(optBorder), NoBorder: inv.Flag(optNoBorder)}
	// A malformed ID fails before the read, as the daemon would fail it.
	if err := app.CheckJobID(jobID); err != nil {
		return g.reportError(streams.Stderr, "job_request_malformed", err)
	}
	common, code, ok := g.read(g.flags(), streams.Stderr)
	if !ok {
		return code
	}
	// The directory is located first; found is whether it exists yet.
	folder, err := app.LocateJobDirectory(common, jobID)
	if err != nil {
		return g.reportError(streams.Stderr, "config_load_failed", err)
	}
	rt, err := app.ResolveDaemonRuntime(common)
	if err != nil {
		return g.reportError(streams.Stderr, "config_load_failed", err)
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	probe, err := daemon.Probe(probeCtx, rt.Socket, rt.MaxFrame)
	probeCancel()
	if err != nil && ctx.Err() != nil {
		// An interrupt during the probe is an interrupt,
		// not an unreachable daemon.
		fmt.Fprintln(streams.Stderr, app.InterruptedLine(jobID, "", "continues in the daemon"))
		return exitcode.ExitCancelled
	}
	// The daemon first, when reachable at the client's IPC schema, of any
	// version or build (EXAMPLES chapter 57, issue 6).
	daemonErr := otherSchema("follow", probe, err)
	switch {
	case daemonErr != nil:
	case err != nil:
		daemonErr = errorcodes.Errorf("daemon_unreachable", "daemon not running or unreadable: %w", err)
	default:
		// A broken stdout must surface as a write error, not a SIGPIPE
		// death, as for the daemon-backed run.
		signal.Ignore(syscall.SIGPIPE)
		terminal, artifactDir, err := app.FollowJobRendering(ctx, common, rt, jobID, render, streams.Stdout, streams.Stderr)
		if err == nil {
			return jobFollowResult(g, streams, jobID, terminal.Outcome.ArtifactDir, terminal.Outcome.Summary, terminal.Outcome.ExitCode)
		}
		if ctx.Err() != nil {
			fmt.Fprintln(streams.Stderr, app.InterruptedLine(jobID, artifactDir, "continues in the daemon"))
			return exitcode.ExitCancelled
		}
		if errorcodes.Of(err) != "job_unknown" {
			if artifactDir != "" {
				fmt.Fprintf(streams.Stderr, "job %s continues in the daemon; artifacts %s\n", jobID, artifactDir)
			} else {
				fmt.Fprintf(streams.Stderr, "job %s continues in the daemon\n", jobID)
			}
			return g.reportError(streams.Stderr, "follow_stream_lost", err)
		}
		daemonErr = err
	}
	// Then the directory.
	state := app.JobDirectoryAbsent
	if folder.Found {
		state, err = app.InspectJobDirectory(folder.Path)
		if err != nil {
			return g.reportError(streams.Stderr, "run_output_records_unreadable", err)
		}
	}
	// A job not ended, or no directory, may be the job of a daemon at
	// another IPC schema, which is judged before the directory (EXAMPLES
	// chapter 57, issue 6).
	if errorcodes.Of(daemonErr) == "daemon_incompatible" && state != app.JobDirectoryFinished {
		return g.reportError(streams.Stderr, "daemon_incompatible", daemonErr)
	}
	if state == app.JobDirectoryAbsent {
		return g.reportError(streams.Stderr, "job_unknown", errorcodes.Errorf("job_unknown", "no job %s: %v, and %s does not exist", jobID, daemonErr, folder.Path))
	}
	signal.Ignore(syscall.SIGPIPE)
	summary, err := app.FollowJobDirectory(ctx, common, folder, jobID, render, streams.Stdout, streams.Stderr)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(streams.Stderr, app.InterruptedLine(jobID, folder.Path, "continues"))
			return exitcode.ExitCancelled
		}
		return g.reportError(streams.Stderr, "run_output_records_unreadable", err)
	}
	return jobFollowResult(g, streams, jobID, folder.Path, summary, summary.ExitCode)
}

// jobFollowResult prints the cancelled line when the job ended cancelled
// (records.Summary.CancelledBy, not the block alone) and returns the job's
// exit; the display ended with its footer and a collection's line, or its
// summary line.
func jobFollowResult(g globalOptions, streams app.IO, jobID, artifactDir string, summary records.Summary, exit int) int {
	if !g.quiet {
		if c := summary.CancelledBy(); c != nil {
			fmt.Fprintln(streams.Stderr, app.CancelledLine(jobID, c.Reason, artifactDir))
		}
	}
	return exit
}
