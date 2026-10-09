package ipc

import (
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestSchema7CancelPayloadsMatchTheSchema: the
// request and both result shapes validate against the schema file, and
// the envelope carries cancel_job under the current schema (8 since the
// record frame).
func TestSchema7CancelPayloadsMatchTheSchema(t *testing.T) {
	cr := CancelRequest{JobID: plantest.JobID, Reason: "wrong change window"}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/cancel_request", cr)
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/cancel_request", CancelRequest{JobID: plantest.JobID})
	at := time.Date(2026, 9, 15, 16, 0, 0, 0, time.UTC)
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/cancel_result", CancelResult{JobID: plantest.JobID, ArtifactDir: "/tmp/j", State: CancelStateCancelling, RequestedAt: &at})
	// The terminal form with a real outcome is checked against the schema by
	// the daemon's cancel test, which has a summary the runner wrote.
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/cancel_result", CancelResult{JobID: plantest.JobID, ArtifactDir: "/tmp/j", State: CancelStateTerminal})
	req, err := NewRequest("r1", OpCancelJob, "0.10.0", cr)
	if err != nil {
		t.Fatal(err)
	}
	// Schema 11: the execution plan at 12, its configuration block.
	if req.IPCSchemaVersion != 11 || SchemaVersion != 11 {
		t.Errorf("schema %d, want 11", req.IPCSchemaVersion)
	}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/request", req)
}

// TestCancelRequestValidation: the identifier, the reason's
// bound, and control characters.
func TestCancelRequestValidation(t *testing.T) {
	ok := CancelRequest{JobID: plantest.JobID, Reason: strings.Repeat("r", MaxCancelReasonBytes)}
	if err := ok.Validate(); err != nil {
		t.Errorf("a %d-byte reason: %v", MaxCancelReasonBytes, err)
	}
	for name, r := range map[string]CancelRequest{
		"bad id":   {JobID: "job-1"},
		"too long": {JobID: plantest.JobID, Reason: strings.Repeat("r", MaxCancelReasonBytes+1)},
		"control":  {JobID: plantest.JobID, Reason: "line one\nline two"},
		"delete":   {JobID: plantest.JobID, Reason: "x\x7fy"},
	} {
		if err := r.Validate(); err == nil || errorcodes.Of(err) != "job_request_malformed" {
			t.Errorf("%s: %v", name, err)
		}
	}
}
