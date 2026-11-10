package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
)

// followRetries bounds consecutive stream failures without progress
// before follow_stream_lost.
const followRetries = 3

// followOptions is what the follow loop needs beyond the socket.
type followOptions struct {
	cfg      configload.Snapshot
	socket   string
	maxFrame int64
	jobID    string
	render   bool
	format   string
	quiet    bool
	debug    bool
	echo     bool
	dynamic  bool
	noBorder bool
	// devices is the job's devices for the gathered display's progress
	// line: the submitting client's plan's; 0 when the follower holds no
	// plan (job follow), which takes them from the job's summary.json, the
	// one read that judges the job's records (judgeRecords).
	devices int
	// learned receives the artifact directory announced by the stream's
	// start, for a follower that did not know it (job follow).
	learned *string
	// gather is job follow's --gather as given (FollowRenderOptions.Gather):
	// a job that kept no commands.jsonl is refused at the first start.
	gather bool
	// gathering, when set, is set true once the display gathers (the
	// renderer's Gather took it), so an interrupt's line can name --gather
	// (InterruptedLine).
	gathering *bool
	// stderr takes the not-kept line (NotKeptLine); nil prints none.
	stderr io.Writer
}

// NotKeptLine is what the client says when the stream starts later than
// its cursor, first being the start's first_sequence: the job writes no
// commands.jsonl, so the daemon has
// nothing to catch a follower up from, and the records from the cursor to
// the edge are not kept anywhere. It is the one sentence for the first
// follow of such a job and for a resume after follow_lagged or a lost
// stream; empty when nothing was missed. It names the key that was false
// by the start's artifactDir, as judgeRecords does: none, a job without a
// folder, output.persist-command; a folder, output.files.commands-jsonl.
func NotKeptLine(cursor, first int64, artifactDir string) string {
	if first <= cursor+1 {
		return ""
	}
	key := "output.files.commands-jsonl"
	if artifactDir == "" {
		key = "output.persist-command"
	}
	return fmt.Sprintf("records %d to %d were before this follow and are not kept (%s is false)", cursor+1, first-1, key)
}

// followJob follows the accepted job to its terminal, rendering every
// record as its frame arrives when render is set. It resumes from the
// last delivered sequence
// after a lost stream or follow_lagged, up to followRetries consecutive
// failures without progress. A frame that does not continue the stream,
// or a record the renderer cannot decode, stops rendering with its code;
// the job continues in the daemon either way.
func followJob(ctx context.Context, o followOptions, artifactDir string, stdout io.Writer) (ipc.FollowTerminal, error) {
	var (
		renderer *jobexec.RunRenderer
		cursor   int64
		failures int
		lastErr  error
		// warned is set once the start's admission warnings are said; a
		// resume's start repeats them and they are not said again.
		warned bool
		// gathered is set once the renderer was asked to gather, at the
		// first start, which draws the progress line.
		gathered bool
		// judged is the job's records as job follow --gather's first start
		// judged them (judgeRecords), before anything is said, and handed
		// to gatherInto; nil until then, and without --gather, where
		// gatherInto judges them itself, once. A later start (a resume
		// after a lost stream) is never judged again: the first found that
		// the job kept its commands.jsonl, and a file lost since is the
		// read-back's run_output_records_unreadable at the end, not a job
		// that kept none (DECISIONS point 4, the third "Settled at S6",
		// gather-2026-11).
		judged *jobRecords
	)
	// The renderer's artifacts value is the footer's label: the folder, or
	// none for a run that keeps no files (28.4).
	if o.render && artifactDir != "" {
		r, err := jobexec.NewRunRenderer(o.cfg, o.quiet, o.debug, ArtifactsLabel(artifactDir), o.format, o.echo, o.dynamic, o.noBorder, stdout, o.stderr)
		if err != nil {
			return ipc.FollowTerminal{}, err
		}
		renderer = r
	}
	// The gathered display's progress line is erased at every end of the
	// follow, before the client's own lines (an interrupt, a lost stream);
	// the display's end erased it already.
	defer func() {
		if renderer != nil {
			renderer.StopProgress()
		}
	}()
	for failures < followRetries {
		if ctx.Err() != nil {
			return ipc.FollowTerminal{}, ctx.Err()
		}
		progressed := false
		terminal, err := daemon.FollowJob(ctx, o.socket, o.maxFrame, ipc.FollowRequest{JobID: o.jobID, Cursor: cursor},
			func(start ipc.FollowStart) error {
				// A follow named by job ID alone (job follow) learns the
				// artifact directory from the start (it writes nothing), so
				// every failure from here names the folder, and builds its
				// renderer then.
				if o.learned != nil {
					*o.learned = start.ArtifactDir
				}
				// job follow --gather judges the job's records here, at the
				// first start, before the admission warnings, the not-kept
				// line, and the renderer: a job that kept no
				// commands.jsonl is refused, the refusal the only line on
				// standard error, the job untouched, a running one
				// continuing in the daemon (DECISIONS point 4, settled at
				// S6, gather-2026-11); the code is terminal below, never
				// retried. The judgement is the gathering's too
				// (gatherInto), never made twice, so under --gather the
				// display is gathered or refused, never silently today's.
				if o.gather && judged == nil {
					records, err := judgeRecords(start.ArtifactDir)
					if err != nil {
						return err
					}
					if records.notKept != "" {
						return gatherNotKept(o.jobID, records.notKept)
					}
					judged = &records
				}
				// A follow by job ID alone did not see the receipt, so the
				// first start's admission warnings are its (IPC 10); the
				// committing client printed them from the receipt.
				if o.learned != nil && !warned {
					jobexec.WriteAdmissionWarnings(o.stderr, o.cfg, start.Warnings)
					warned = true
				}
				if line := NotKeptLine(cursor, start.FirstSequence, start.ArtifactDir); line != "" {
					jobexec.WriteWarning(o.stderr, o.cfg, line)
				}
				if renderer == nil && o.render {
					r, err := jobexec.NewRunRenderer(o.cfg, o.quiet, o.debug, ArtifactsLabel(start.ArtifactDir), o.format, o.echo, o.dynamic, o.noBorder, stdout, o.stderr)
					if err != nil {
						return err
					}
					renderer = r
				}
				// The gathered display starts with the follow, its progress
				// line drawn now, after the start's lines (a renderer built
				// from the receipt, run's and crun's, too).
				if renderer != nil && !gathered {
					gathered = true
					if err := o.gatherInto(ctx, renderer, start.ArtifactDir, judged); err != nil {
						return err
					}
					o.stderr = renderer.Stderr()
				}
				return nil
			},
			func(sequence int64, line []byte) error {
				if o.render {
					if err := renderer.Line(line); err != nil {
						return err
					}
				}
				cursor = sequence
				progressed = true
				return nil
			})
		if err == nil {
			if renderer != nil {
				// The terminal frame's summary ends the display, with the
				// outcome's exit, the job's own as the client reports it.
				summary := terminal.Outcome.Summary
				summary.ExitCode, summary.ExitName = terminal.Outcome.ExitCode, terminal.Outcome.ExitName
				if err := renderer.Finish(&summary); err != nil {
					return terminal, &EndedError{Exit: summary.ExitCode, Err: err}
				}
			}
			return terminal, nil
		}
		if ctx.Err() != nil {
			return ipc.FollowTerminal{}, ctx.Err()
		}
		code := errorcodes.Of(err)
		switch {
		case code == "terminal_write_failed", code == "run_output_record_decode_failed", code == "run_output_records_unreadable", code == "ipc_result_malformed", code == "job_unknown", code == "follow_cursor_stale", code == "ipc_frame_too_large", code == "gather_option_conflict":
			return ipc.FollowTerminal{}, err
		}
		// follow_lagged, a lost connection, or a daemon that closed the
		// stream: resume from the verified cursor.
		lastErr = err
		if progressed {
			failures = 0
		} else {
			failures++
		}
	}
	return ipc.FollowTerminal{}, errorcodes.Errorf("follow_stream_lost", "the follow stream failed %d times without progress; last: %v", followRetries, errorText(lastErr))
}

// gatherInto has the follow's renderer gather, once, at the first start,
// where the display gathers: the follower's configuration says so
// (display.run.gather, the follower's own, so job follow gathers a job run
// without it) and the renderer can (Gathers: text, never --format jsonl or
// json, so a site's key never breaks a script). The job's records are
// judged then by judgeRecords, or were judged already under job follow's
// --gather (judged), the one judgement for the refusal and the gathering;
// a job that kept none shows today's display (the key alone). The answers
// are read back at the end from ARTIFACTDIR/commands.jsonl with the folder
// follow's checks (openChecked), the file the follower's own: only the
// daemon's own account reaches it (a service platform's daemon is the
// service account's, its followers that account), so no tree's group is
// asked; a file gone since the start is that read-back's
// run_output_records_unreadable. The display stops at ctx's end (the
// invocation's interrupt). Its progress line, at a terminal, counts the
// plan's devices, or for a follower holding no plan the summary's. The
// summary's own errors (run_output_records_unreadable,
// run_output_record_decode_failed) end the follow, the job unaffected.
func (o followOptions) gatherInto(ctx context.Context, r *jobexec.RunRenderer, artifactDir string, judged *jobRecords) error {
	if !o.cfg.Bool("display.run.gather") || !r.Gathers() {
		return nil
	}
	if judged == nil {
		records, err := judgeRecords(artifactDir)
		if err != nil {
			return err
		}
		judged = &records
	}
	if judged.notKept != "" {
		return nil
	}
	devices := judged.devices
	if o.devices > 0 {
		devices = o.devices
	}
	gathering, err := r.Gather(ctx, func() (*os.File, error) { return openChecked(judged.path, -1) }, func() (int, error) { return devices, nil })
	if gathering && o.gathering != nil {
		*o.gathering = true
	}
	return err
}

// jobRecords is the daemon follow's judgement of the job's commands.jsonl
// (judgeRecords): where none was kept, notKept, the key that was false when
// the job ran; where it was, path, the file the read-back opens, and
// devices, the job's devices as its summary names them.
type jobRecords struct {
	notKept string
	path    string
	devices int
}

// judgeRecords judges whether the job kept its commands.jsonl from the
// folder the follow's start names, by the folder follow's rule: no folder,
// none (output.persist-command was false); its summary.json naming no
// commands_jsonl, none (output.files.commands-jsonl was false); naming it
// (whether it names it, never the path it names), kept, the read-back
// opening ARTIFACTDIR/commands.jsonl at the end (DECISIONS point 4, the
// third "Settled at S6", gather-2026-11). The file's name is never looked
// at: a file gone since is the read-back's error, not a job that kept
// none. A summary that cannot be read is readSummary's error.
func judgeRecords(artifactDir string) (jobRecords, error) {
	if artifactDir == "" {
		return jobRecords{notKept: "output.persist-command"}, nil
	}
	summary, err := readSummary(artifactDir)
	if err != nil {
		return jobRecords{}, err
	}
	if summary.Paths["commands_jsonl"] == "" {
		return jobRecords{notKept: "output.files.commands-jsonl"}, nil
	}
	return jobRecords{path: filepath.Join(artifactDir, "commands.jsonl"), devices: len(summary.Devices)}, nil
}

// gatherNotKept is job follow --gather's refusal of a job that kept no
// commands.jsonl, key the output key that was false when it ran
// (DECISIONS point 4, settled at S6, gather-2026-11):
// gather_option_conflict (usage, exit 4), the command line refused, not a
// follow lost. One message for both follow paths, the daemon's
// (followJob, from judgeRecords) and the folder's (FollowJobDirectory),
// the job ID and the key its only parts.
func gatherNotKept(jobID, key string) error {
	return errorcodes.Errorf("gather_option_conflict", "--gather needs the job's commands.jsonl; job %s kept none (%s was false)", jobID, key)
}

// EndedError is a followed job's display failing at the job's end, from
// the follow's terminal or the folder's final summary: the job ended, its
// exit Exit, and is unaffected (its summary keeps it).
type EndedError struct {
	Exit int
	Err  error
}

func (e *EndedError) Error() string { return e.Err.Error() }
func (e *EndedError) Unwrap() error { return e.Err }

// EndedLine is what a client says before the error of a display that
// failed at the job's end (EndedError), as the follow's failure line says
// that a job continues: "job ID ended exit=N; artifacts DIR"; "" for any
// other error.
func EndedLine(jobID, artifactDir string, err error) string {
	var ended *EndedError
	if !errors.As(err, &ended) {
		return ""
	}
	return fmt.Sprintf("job %s ended exit=%d; artifacts %s", jobID, ended.Exit, ArtifactsLabel(artifactDir))
}

// InterruptedDisplay is what a client says when the invocation's interrupt
// stopped a gathered display at a job's end (EndedError, the code
// cancelled; DECISIONS point 4, "Settled at S4"): the job's ended line,
// "job ID ended exit=N; artifacts DIR", and the invocation's error,
// "cancelled: the display of job ID was interrupted", its exit 113. ok is
// false for any other error, an interrupt while the job ran among them
// (the follow's own line says the job continues).
func InterruptedDisplay(jobID, artifactDir string, err error) (line string, cause error, ok bool) {
	var ended *EndedError
	if !errors.As(err, &ended) || errorcodes.Of(ended.Err) != "cancelled" {
		return "", nil, false
	}
	return EndedLine(jobID, artifactDir, err), errorcodes.Errorf("cancelled", "the display of job %s was interrupted", jobID), true
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	var mismatch *ipc.SchemaMismatchError
	if errors.As(err, &mismatch) {
		return fmt.Sprintf("daemon schema %d", mismatch.DaemonSchema)
	}
	return err.Error()
}

// FollowJobToTerminal follows an accepted job to its terminal without
// rendering records, as run --follow=false waits (for job cancel
// --follow), through the invocation's runtime rt. An interrupt returns the
// context's error; the job continues in the daemon either way.
func FollowJobToTerminal(ctx context.Context, rt DaemonRuntime, jobID, artifactDir string) (ipc.FollowTerminal, error) {
	return followJob(ctx, followOptions{cfg: rt.Config, socket: rt.Socket, maxFrame: rt.MaxFrame, jobID: jobID}, artifactDir, io.Discard)
}

// FollowRenderOptions are job follow's rendering options, run's own with
// the same meanings.
type FollowRenderOptions struct {
	Format        string
	Echo          bool
	DynamicBorder bool
	NoBorder      bool
	// Gather is --gather as given on the command line (DECISIONS point 4,
	// settled at S6, gather-2026-11): a job that kept no commands.jsonl is
	// refused, gather_option_conflict, when the follow learns it, before
	// anything is said or shown, whatever the key's final value. The key
	// alone (display.run.gather, set by a file or --set) shapes the
	// display, and keeps today's display and warning for such a job.
	Gather bool
	// Gathering, when set, is set true once the display gathers (the
	// renderer's Gather took it), so the line of an interrupted follow
	// can name --gather (InterruptedLine).
	Gathering *bool
}

// FollowJobRendering follows a job named by its ID from the zero cursor to
// its terminal and renders every verified record as the foreground run
// would have: the daemon catches the follower up from
// the file, feeds it live, and sends the terminal. The artifact directory is
// learned from the announced canonical file. An interrupt returns the
// context's error; the job continues in the daemon either way.
// The artifact directory is returned as soon as the stream announced it,
// with the terminal or with the error, and is empty when no start arrived.
// rt is the invocation's runtime, resolved from its read.
func FollowJobRendering(ctx context.Context, common CommonOptions, rt DaemonRuntime, jobID string, o FollowRenderOptions, stdout, stderr io.Writer) (ipc.FollowTerminal, string, error) {
	var artifactDir string
	terminal, err := followJob(ctx, followOptions{cfg: rt.Config, socket: rt.Socket, maxFrame: rt.MaxFrame, jobID: jobID, render: true, format: o.Format, quiet: common.Quiet, debug: common.Debug, echo: o.Echo, dynamic: o.DynamicBorder, noBorder: o.NoBorder, learned: &artifactDir, gather: o.Gather, gathering: o.Gathering, stderr: stderr}, "", stdout)
	return terminal, artifactDir, err
}
