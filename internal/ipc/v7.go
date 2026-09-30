package ipc

import (
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// Schema 7 adds cancel_job:
// one request, one response. The daemon cancels the named job's context
// with the cancel_job cause and answers at once, without waiting for the
// accounting, which the caller observes through follow_job or the artifact
// directory. The job ID is the idempotency key: a repeated request answers
// the first request's time and cancels nothing again.
const (
	OpCancelJob = "cancel_job"

	// CancelStateCancelling: the job was running and its context is now
	// cancelled. CancelStateTerminal: the job had already finished; the
	// result carries its outcome.
	CancelStateCancelling = "cancelling"
	CancelStateTerminal   = "terminal"

	// MaxCancelReasonBytes bounds the operator's free-text reason.
	MaxCancelReasonBytes = 256
)

// CancelRequest is the cancel_job payload.
type CancelRequest struct {
	JobID  string `json:"job_id"`
	Reason string `json:"reason,omitempty"`
}

// Validate checks the identifier and the reason's bounds: at most
// MaxCancelReasonBytes, no control characters, never interpreted.
func (r *CancelRequest) Validate() error {
	if !executionplan.ValidJobID(r.JobID) {
		return errorcodes.Errorf("job_request_malformed", "job_id %q is not a job ID (YYMMDD-HHMMSS-xx)", r.JobID)
	}
	if len(r.Reason) > MaxCancelReasonBytes {
		return errorcodes.Errorf("job_request_malformed", "reason is %d bytes; at most %d", len(r.Reason), MaxCancelReasonBytes)
	}
	for _, c := range r.Reason {
		if c < 0x20 || c == 0x7f {
			return errorcodes.Errorf("job_request_malformed", "reason contains a control character")
		}
	}
	return nil
}

// CancelResult answers cancel_job. RequestedAt is set under cancelling and
// names the first request's time; Outcome is set under terminal.
type CancelResult struct {
	JobID       string           `json:"job_id"`
	ArtifactDir string           `json:"artifact_dir"`
	State       string           `json:"state"`
	RequestedAt *time.Time       `json:"requested_at,omitempty"`
	Outcome     *ActivityOutcome `json:"outcome,omitempty"`
}
