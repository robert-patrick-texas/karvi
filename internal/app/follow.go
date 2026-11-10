package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	// plan (job follow), which reads them from the job's summary.json.
	devices int
	// learned receives the artifact directory announced by the stream's
	// start, for a follower that did not know it (job follow).
	learned *string
	// stderr takes the not-kept line (NotKeptLine); nil prints none.
	stderr io.Writer
}

// NotKeptLine is what the client says when the stream starts later than
// its cursor, first being the start's first_sequence: the job writes no
// commands.jsonl, so the daemon has
// nothing to catch a follower up from, and the records from the cursor to
// the edge are not kept anywhere. It is the one sentence for the first
// follow of such a job and for a resume after follow_lagged or a lost
// stream; empty when nothing was missed.
func NotKeptLine(cursor, first int64) string {
	if first <= cursor+1 {
		return ""
	}
	return fmt.Sprintf("records %d to %d were before this follow and are not kept (output.files.commands-jsonl is false)", cursor+1, first-1)
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
				// artifact directory from the
				// start and builds its renderer then.
				if o.learned != nil {
					*o.learned = start.ArtifactDir
					// A follow by job ID alone did not see the receipt, so the
					// first start's admission warnings are its (IPC 10); the
					// committing client printed them from the receipt.
					if !warned {
						jobexec.WriteAdmissionWarnings(o.stderr, o.cfg, start.Warnings)
						warned = true
					}
				}
				if line := NotKeptLine(cursor, start.FirstSequence); line != "" {
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
					if err := o.gatherInto(ctx, renderer, start.ArtifactDir); err != nil {
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
		case code == "terminal_write_failed", code == "run_output_record_decode_failed", code == "run_output_records_unreadable", code == "ipc_result_malformed", code == "job_unknown", code == "follow_cursor_stale", code == "ipc_frame_too_large":
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

// gatherInto has the follow's renderer gather when the follower's
// configuration says so (display.run.gather) and the job keeps its
// commands.jsonl in artifactDir: the store makes the file with the job's
// files, before the receipt and the follow's start, so one absent then is
// not kept, and the display is today's. At the end the answers are read
// back from it with the folder follow's checks (openChecked), the file
// the follower's own: only the daemon's own account reaches it (a service
// platform's daemon is the service account's, its followers that account),
// so no tree's group is asked. The display stops at ctx's end (the
// invocation's interrupt). Its progress line, at a terminal, counts the
// plan's devices, or a follower holding no plan reads them once from the
// job's summary.json, written at the job's acceptance, with the folder
// follow's codes: one that cannot be read is
// run_output_records_unreadable, one that is not a summary
// run_output_record_decode_failed, the follow ended and the job
// unaffected. Where the display does not gather (--format jsonl or json)
// or no line is drawn, it is never read.
func (o followOptions) gatherInto(ctx context.Context, r *jobexec.RunRenderer, artifactDir string) error {
	if artifactDir == "" || !o.cfg.Bool("display.run.gather") {
		return nil
	}
	path := filepath.Join(artifactDir, "commands.jsonl")
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	devices := func() (int, error) {
		if o.devices > 0 {
			return o.devices, nil
		}
		summary, err := readSummary(artifactDir)
		return len(summary.Devices), err
	}
	_, err := r.Gather(ctx, func() (*os.File, error) { return openChecked(path, -1) }, devices)
	return err
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
	terminal, err := followJob(ctx, followOptions{cfg: rt.Config, socket: rt.Socket, maxFrame: rt.MaxFrame, jobID: jobID, render: true, format: o.Format, quiet: common.Quiet, debug: common.Debug, echo: o.Echo, dynamic: o.DynamicBorder, noBorder: o.NoBorder, learned: &artifactDir, stderr: stderr}, "", stdout)
	return terminal, artifactDir, err
}
