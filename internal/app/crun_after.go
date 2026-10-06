package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

// The collection hook, crun.after: one executable a
// site names, run by the client once a crun has ended and its display is
// printed, on the in-process path and the daemon path alike, never after a
// run or command given --cd. It runs in the
// collection directory with the replaced files' names on standard input, one
// per line, and the job in the environment; its output goes to karvi's
// standard error, after the display, so a --format jsonl standard output
// stays records only. A hook that cannot start, exits non-zero, or runs past
// crun.after-timeout is the warning crun_after_failed; the collection stands
// and the run's exit code is unchanged. The audit holds a crun.after event
// either way. A detached run has no client at its end, so no hook (the CLI's
// gate); a run without a collection summary (a follow lost, an interrupt) has
// nothing to hand the hook.

// collectionHook is one run of crun.after: the configured path and bound, and
// what the hook is told about the job.
type collectionHook struct {
	Path    string
	Timeout time.Duration
	JobID   string
	JobDir  string // empty under --nof
	Exit    int    // the run's exit code
	Summary *records.CollectionSummary
}

// hookOutcome is what the hook did: its exit code, how long it ran, whether
// the bound ended it, and the error a warning names (nil when it exited 0).
type hookOutcome struct {
	exit     int
	duration time.Duration
	timedOut bool
	err      error
}

// killGrace is how long the hook's process group has after SIGTERM before it
// is killed.
const killGrace = 5 * time.Second

// RunCollectionHook runs crun.after for a crun that ended with a collection
// summary; without one, or with the key empty, it does nothing. Every failure
// is a warning on stderr, never an exit code.
func RunCollectionHook(ctx context.Context, common CommonOptions, result ActivityResult, stderr io.Writer) {
	c := result.Summary.Collection
	if c == nil {
		return
	}
	cfg, operator, err := prepareConfig(common)
	if err != nil {
		warning(stderr, errorcodes.Message(errorcodes.Errorf("crun_after_failed", "the configuration could not be loaded for crun.after: %s", safeError(err))))
		return
	}
	path := cfg.String("crun.after")
	if path == "" {
		return
	}
	h := collectionHook{Path: path, Timeout: cfg.Duration("crun.after-timeout"), JobID: result.JobID, JobDir: result.ArtifactDir, Exit: result.ExitCode, Summary: c}
	o := h.run(ctx, stderr)
	if o.err != nil {
		warning(stderr, errorcodes.Message(o.err))
	}
	if err := writeCollectionHookAudit(cfg, operator, result, h, o); err != nil {
		warning(stderr, "audit_write_failed: the crun.after event: "+safeError(err))
	}
}

// replacedFiles is the hook's standard input: the replaced files' names,
// sorted, one per line; empty when nothing was replaced.
func (h collectionHook) replacedFiles() string {
	files := make([]string, 0, len(h.Summary.Devices))
	for _, d := range h.Summary.Devices {
		if d.Outcome == records.CollectionReplaced {
			files = append(files, d.File)
		}
	}
	if len(files) == 0 {
		return ""
	}
	sort.Strings(files)
	return strings.Join(files, "\n") + "\n"
}

// run executes the hook under its bound and returns what it did. The hook
// leads its own process group, so the bound ends a shell and its children
// together: SIGTERM to the group, then SIGKILL after killGrace.
func (h collectionHook) run(ctx context.Context, stderr io.Writer) hookOutcome {
	tctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	cmd := exec.CommandContext(tctx, h.Path)
	cmd.Dir = h.Summary.Directory
	cmd.Env = append(os.Environ(),
		"KARVI_JOB_ID="+h.JobID,
		"KARVI_JOB_DIR="+h.JobDir,
		"KARVI_CRUN_DIRECTORY="+h.Summary.Directory,
		fmt.Sprintf("KARVI_CRUN_REPLACED=%d", h.Summary.Replaced),
		fmt.Sprintf("KARVI_CRUN_KEPT=%d", h.Summary.Kept),
		fmt.Sprintf("KARVI_EXIT=%d", h.Exit))
	cmd.Stdin = strings.NewReader(h.replacedFiles())
	cmd.Stdout, cmd.Stderr = stderr, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = killGrace
	start := time.Now()
	err := cmd.Run()
	o := hookOutcome{duration: time.Since(start)}
	if cmd.ProcessState != nil {
		o.exit = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case ctx.Err() != nil:
		o.err = errorcodes.Errorf("crun_after_failed", "%s was interrupted after %s", h.Path, o.duration.Round(time.Millisecond))
	case errors.Is(tctx.Err(), context.DeadlineExceeded):
		o.timedOut = true
		o.err = errorcodes.Errorf("crun_after_failed", "%s ran past crun.after-timeout (%s) and was ended", h.Path, h.Timeout)
	case errors.As(err, &exitErr):
		o.err = errorcodes.Errorf("crun_after_failed", "%s exited %d", h.Path, o.exit)
	default:
		o.err = errorcodes.Errorf("crun_after_failed", "%s could not be started: %v", h.Path, err)
	}
	return o
}

// writeCollectionHookAudit records the hook in the audit as crun.after.succeeded
// or crun.after.failed: the path, the directory and counts, the
// exit code, the duration, whether the bound ended it, and the diagnostic.
func writeCollectionHookAudit(cfg configload.Snapshot, operator credentials.Operator, result ActivityResult, h collectionHook, o hookOutcome) error {
	sink, err := audit.New(cfg, operator.Home)
	if err != nil {
		return err
	}
	defer sink.Close()
	outcome, severity, diagnostic := "succeeded", "info", ""
	if o.err != nil {
		outcome, severity, diagnostic = "failed", "warning", errorcodes.Message(o.err)
	}
	eventID, _ := osutil.NewID(time.Now())
	return sink.WriteAudit(records.AuditRecord{SchemaVersion: 1, EventID: eventID, EventName: "crun.after." + outcome, Timestamp: time.Now(), Outcome: outcome, Severity: severity,
		Operator:   osutil.RecordOperator(operator),
		Process:    map[string]any{"pid": os.Getpid(), "version": buildinfo.Version, "host": hostname()},
		ActivityID: result.ActivityID, JobID: result.JobID,
		Action:  map[string]any{"mode": "crun", "operation": "after", "command_count": 0, "command_sha256_array": []string{}},
		Policy:  map[string]any{"config_digest": cfg.Digest},
		Result:  map[string]any{"exit_code": o.exit},
		Source:  map[string]any{"client": "karvi"},
		Details: map[string]any{"path": h.Path, "directory": h.Summary.Directory, "replaced": h.Summary.Replaced, "kept": h.Summary.Kept, "duration_ms": o.duration.Milliseconds(), "timed_out": o.timedOut, "diagnostic": diagnostic}})
}
