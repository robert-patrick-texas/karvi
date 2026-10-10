package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/records"
)

// The job command word: operations on one accepted job, named by the job
// ID of its receipt. cancel moved verbatim from daemon.

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
		return reportError(streams.Stderr, "job_request_malformed", err)
	}
	rt, code, ok := g.runtime(streams.Stderr)
	if !ok {
		return code
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	probe, err := daemon.Probe(probeCtx, rt.Socket, rt.MaxFrame)
	probeCancel()
	if err != nil {
		return reportError(streams.Stderr, "daemon_unreachable", fmt.Errorf("daemon not running or unreadable, so there is nothing to cancel: %w", err))
	}
	if !probe.Compatible {
		return reportError(streams.Stderr, "daemon_incompatible", fmt.Errorf("%w; a job of that daemon ends only with \"karvi daemon stop --force\"", incompatibleDaemonError(probe)))
	}
	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	res, err := daemon.CancelJob(callCtx, rt.Socket, rt.MaxFrame, ipc.CancelRequest{JobID: jobID, Reason: inv.String(optReason)})
	callCancel()
	if err != nil {
		return reportError(streams.Stderr, "daemon_unreachable", err)
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
		return reportError(streams.Stderr, "follow_stream_lost", err)
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
// job's directory, derived from the ID, is read for a finished job. The
// verb never launches a daemon.
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
		return reportError(streams.Stderr, "job_request_malformed", err)
	}
	common, code, ok := g.read(g.flags(), streams.Stderr)
	if !ok {
		return code
	}
	// The directory is located first; found is whether it exists yet.
	dir, dirFound, err := app.LocateJobDirectory(common, jobID)
	if err != nil {
		return reportError(streams.Stderr, "config_load_failed", err)
	}
	rt, err := app.ResolveDaemonRuntime(common)
	if err != nil {
		return reportError(streams.Stderr, "config_load_failed", err)
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
	// The daemon first, when reachable and compatible.
	var daemonErr error
	switch {
	case err != nil:
		daemonErr = errorcodes.Errorf("daemon_unreachable", "daemon not running or unreadable: %w", err)
	case !probe.Compatible:
		daemonErr = errorcodes.Errorf("daemon_incompatible", "%w", incompatibleDaemonError(probe))
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
			return reportError(streams.Stderr, "follow_stream_lost", err)
		}
		daemonErr = err
	}
	// Then the directory.
	state := app.JobDirectoryAbsent
	if dirFound {
		state, err = app.InspectJobDirectory(dir)
		if err != nil {
			return reportError(streams.Stderr, "run_output_records_unreadable", err)
		}
	}
	switch state {
	case app.JobDirectoryFinished:
		signal.Ignore(syscall.SIGPIPE)
		summary, err := app.RenderJobDirectory(common, dir, render, streams.Stdout)
		if err != nil {
			return reportError(streams.Stderr, "run_output_records_unreadable", err)
		}
		return jobFollowResult(g, streams, jobID, dir, summary, summary.ExitCode)
	case app.JobDirectoryOrphaned:
		return reportError(streams.Stderr, "job_orphaned", errorcodes.Errorf("job_orphaned", "job %s: %s holds no summary and no running daemon holds the job (%v); the daemon that ran it is gone, or output.files.summary-json was false for the job", jobID, dir, daemonErr))
	}
	if errorcodes.Of(daemonErr) == "daemon_incompatible" {
		// No directory to fall back on: the job may be that daemon's (3.4).
		return reportError(streams.Stderr, "daemon_incompatible", daemonErr)
	}
	return reportError(streams.Stderr, "job_unknown", errorcodes.Errorf("job_unknown", "no job %s: %v, and %s does not exist", jobID, daemonErr, dir))
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
