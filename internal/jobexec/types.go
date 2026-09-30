// Package jobexec runs one accepted job from its final execution plan and
// credential package: the store, audit, scoreboard,
// metrics, capacity, dispatch, executor, summary, and renderer. It resolves
// no name and no credential; the daemon and the direct modes both call Run.
package jobexec

import (
	"context"
	"io"
	"log/slog"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/executor"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

// IO owns the three conventional process streams used by an invocation.
type IO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// ActivityResult is the safe terminal result returned locally or over IPC.
type ActivityResult struct {
	ExitCode    int             `json:"exit_code"`
	ExitName    string          `json:"exit_name"`
	ActivityID  string          `json:"activity_id"`
	JobID       string          `json:"job_id,omitempty"`
	ArtifactDir string          `json:"artifact_dir,omitempty"`
	Summary     records.Summary `json:"summary"`
	Error       string          `json:"error,omitempty"`
}

// Request is everything Run needs: the final plan,
// the grant provider, the configuration snapshot, the operator, the common
// flags, the activity type and ID, the persist flag, and what the manifest
// records beside the plan: the commit header, the package projection, and
// the operator's selection.
// The shutdown causes live in the executor, which
// reads them per unit; the runner reads them for the unstarted devices,
// the counts, the exit, and the audit reason.
var (
	ErrShutdownForced       = executor.ErrShutdownForced
	ErrShutdownGraceExpired = executor.ErrShutdownGraceExpired
	ErrJobCancelled         = executor.ErrJobCancelled
)

// ShutdownCause reports the shutdown cause a cancelled context carries, or
// nil when the cancellation is not a shutdown.
func ShutdownCause(ctx context.Context) error { return executor.ShutdownCause(ctx) }

// JobCancelled builds the cancel_job cause carrying the operator's reason;
// CancelCause reads it back from a context.
func JobCancelled(reason string) error      { return executor.JobCancelled(reason) }
func CancelCause(ctx context.Context) error { return executor.CancelCause(ctx) }

// JobCancelRequest is the cancel_job request a context cause carries;
// CancelRequestOf reads it back.
type JobCancelRequest = executor.JobCancelRequest

func CancelRequestOf(ctx context.Context) (JobCancelRequest, bool) {
	return executor.CancelRequestOf(ctx)
}

type Request struct {
	Plan       executionplan.ExecutionPlan
	Header     executionplan.PublicJobHeader
	Package    credentialpackage.SafePackageProjection
	Grants     executor.GrantProvider
	Protection string
	Mode       executionplan.Mode

	Config   configload.Snapshot
	Operator credentials.Operator
	Quiet    bool
	Debug    bool

	// ActivityType is command or run; ActivityID is the activity's identifier
	// and, for a run, its job ID.
	ActivityType string
	ActivityID   string

	Selection      records.Selection
	CandidateCount int // command: devices in the ordered target set
	// Daemon is true when the daemon runs the job; the scoreboard says so.
	Daemon bool

	// OnAccepted is called once the job is accepted: the manifest is
	// durable and the started audit record written. The daemon answers
	// commit_job at this point, its receipt carrying warnings, the admission
	// warnings raised before any device was contacted (spool_width_narrowed),
	// each
	// "code: message".
	OnAccepted func(artifactDir string, warnings []string)
	// OnDurable receives each record with the store's notice after its
	// durability barrier; the daemon sends the record to its followers over
	// the socket.
	OnDurable func(*records.CommandRecord, output.Notice)

	// Logger receives the job's operational events when the daemon runs it
	// (the spool sweep's removals, a narrowed width), since the daemon's job
	// writes to no standard error; nil
	// sends them to the debug stream.
	Logger *slog.Logger

	// Preparations and Timing are what the daemon measured before the run,
	// embedded in an exercise report; ignored by a
	// live run.
	Preparations []executionplan.PreparationReport
	Timing       records.ReportTiming
}
