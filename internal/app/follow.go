package app

import (
	"context"
	"errors"
	"fmt"
	"io"

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
	)
	// The renderer's artifacts value is the footer's label: the folder, or
	// none for a run that keeps no files (28.4).
	if o.render && artifactDir != "" {
		r, err := jobexec.NewRunRenderer(o.cfg, o.quiet, o.debug, ArtifactsLabel(artifactDir), o.format, o.echo, o.dynamic, o.noBorder, stdout)
		if err != nil {
			return ipc.FollowTerminal{}, err
		}
		renderer = r
	}
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
					// start's admission warnings are its (IPC 10); the committing
					// client printed them from the receipt.
					for _, w := range start.Warnings {
						warning(o.stderr, w)
					}
				}
				if line := NotKeptLine(cursor, start.FirstSequence); line != "" {
					warning(o.stderr, line)
				}
				if renderer == nil && o.render {
					r, err := jobexec.NewRunRenderer(o.cfg, o.quiet, o.debug, ArtifactsLabel(start.ArtifactDir), o.format, o.echo, o.dynamic, o.noBorder, stdout)
					if err != nil {
						return err
					}
					renderer = r
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
					return terminal, err
				}
			}
			return terminal, nil
		}
		if ctx.Err() != nil {
			return ipc.FollowTerminal{}, ctx.Err()
		}
		code := errorcodes.Of(err)
		switch {
		case code == "terminal_write_failed", code == "run_output_record_decode_failed", code == "ipc_result_malformed", code == "job_unknown", code == "follow_cursor_stale", code == "ipc_frame_too_large":
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
// --follow). An interrupt returns the context's error;
// the job continues in the daemon either way.
func FollowJobToTerminal(ctx context.Context, common CommonOptions, jobID, artifactDir string) (ipc.FollowTerminal, error) {
	rt, err := ResolveDaemonRuntime(common)
	if err != nil {
		return ipc.FollowTerminal{}, err
	}
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
func FollowJobRendering(ctx context.Context, common CommonOptions, jobID string, o FollowRenderOptions, stdout, stderr io.Writer) (ipc.FollowTerminal, string, error) {
	rt, err := ResolveDaemonRuntime(common)
	if err != nil {
		return ipc.FollowTerminal{}, "", err
	}
	var artifactDir string
	terminal, err := followJob(ctx, followOptions{cfg: rt.Config, socket: rt.Socket, maxFrame: rt.MaxFrame, jobID: jobID, render: true, format: o.Format, quiet: common.Quiet, debug: common.Debug, echo: o.Echo, dynamic: o.DynamicBorder, noBorder: o.NoBorder, learned: &artifactDir, stderr: stderr}, "", stdout)
	return terminal, artifactDir, err
}
