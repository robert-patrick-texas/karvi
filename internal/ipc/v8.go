package ipc

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// follow_job (schema 6)
// is one request and many responses on the connection. Schema 8 changes
// what the responses carry: the record
// itself travels in each frame, where schema 6 announced the record's
// place in commands.jsonl (offset, length, digest) for the client to read
// the file. The first result is FollowStart; then zero or more responses
// with Event EventRecord carrying a RecordFrame; then one with Event
// EventStreamTerminal carrying FollowTerminal. No response carries a
// credential or a secret; the record carries the device's output as its
// commands.jsonl line does.
const (
	OpFollowJob = "follow_job"

	EventRecord         = "record"
	EventStreamTerminal = "stream_terminal"
)

// FollowQueue bounds a follower's pending records; overflow ends the
// stream with follow_lagged and the client resumes from its cursor.
const FollowQueue = 1024

// A cursor is the sequence of the last record the follower has; zero is
// the start. Schema 6's cursor carried the byte offset too, which located
// the next line for the client; the daemon resuming a follower from its
// own file needs the sequence alone.

// FollowRequest is the follow_job payload.
type FollowRequest struct {
	JobID  string `json:"job_id"`
	Cursor int64  `json:"cursor"`
}

// Validate checks the identifier and the cursor.
func (r *FollowRequest) Validate() error {
	if !executionplan.ValidJobID(r.JobID) {
		return errorcodes.Errorf("job_request_malformed", "job_id %q is not a job ID (YYMMDD-HHMMSS-xx)", r.JobID)
	}
	if r.Cursor < 0 {
		return errorcodes.Errorf("job_request_malformed", "cursor %d is not a sequence", r.Cursor)
	}
	return nil
}

// FollowStart is the first result of follow_job. ArtifactDir is the job's
// folder, empty when the job writes none. FirstSequence is the sequence
// of the first record the stream carries: the cursor plus one when the
// daemon can catch the follower up, later when it holds no file to catch
// up from, and the follower is told what it missed.
type FollowStart struct {
	JobID                  string `json:"job_id"`
	ArtifactDir            string `json:"artifact_dir"`
	HighestDurableSequence int64  `json:"highest_durable_sequence"`
	ResumeCursor           int64  `json:"resume_cursor"`
	FirstSequence          int64  `json:"first_sequence"`
	// Warnings are the job's admission warnings as the receipt carries
	// them (schema 10), so a follow named by job ID alone sees them too.
	Warnings []string `json:"warnings,omitempty"`
}

// Validate checks the start against the request.
func (s *FollowStart) Validate(req *FollowRequest) error {
	if s.JobID != req.JobID {
		return errorcodes.Errorf("ipc_result_malformed", "follow start names job %q, not %q", s.JobID, req.JobID)
	}
	if s.ArtifactDir != "" && s.ArtifactDir[0] != '/' {
		return errorcodes.Errorf("ipc_result_malformed", "follow start artifact_dir must be absolute")
	}
	if s.ResumeCursor != req.Cursor || s.HighestDurableSequence < s.ResumeCursor {
		return errorcodes.Errorf("ipc_result_malformed", "follow start cursor %d does not resume the request's %d", s.ResumeCursor, req.Cursor)
	}
	if s.FirstSequence <= s.ResumeCursor {
		return errorcodes.Errorf("ipc_result_malformed", "follow start first_sequence %d is not after the cursor %d", s.FirstSequence, s.ResumeCursor)
	}
	return nil
}

// RecordFrame is the result of an EventRecord response: one record, the
// bytes of its commands.jsonl line without the LF, and its sequence
// beside it so the follower checks continuity without decoding the
// record.
type RecordFrame struct {
	Sequence int64           `json:"sequence"`
	Record   json.RawMessage `json:"record"`
}

// Validate checks the frame continues from cursor.
func (f *RecordFrame) Validate(cursor int64) error {
	if f.Sequence != cursor+1 {
		return errorcodes.Errorf("ipc_result_malformed", "record sequence %d does not follow %d", f.Sequence, cursor)
	}
	if len(f.Record) < 2 {
		return errorcodes.Errorf("ipc_result_malformed", "record frame for sequence %d carries no record", f.Sequence)
	}
	return nil
}

// FollowTerminal ends the stream: the final durable sequence and the job's
// activity outcome with its summary, which commit_job carried in schema 5.
type FollowTerminal struct {
	Cursor  int64           `json:"cursor"`
	Outcome ActivityOutcome `json:"outcome"`
}

// recordFrameHalves is the envelope around a record in its frame: what
// goes before the record and what after, made once for the writer and the
// size so the two cannot differ.
func recordFrameHalves(requestID string, sequence int64) (before, after string, err error) {
	id, err := json.Marshal(requestID)
	if err != nil {
		return "", "", err
	}
	before = fmt.Sprintf(`{"ipc_schema_version":%d,"request_id":%s,"result":{"sequence":%d,"record":`, SchemaVersion, id, sequence)
	after = fmt.Sprintf("},\"event\":%q}\n", EventRecord)
	return before, after, nil
}

// RecordFrameSize is the exact length of the frame WriteRecordFrame writes
// for a record whose commands.jsonl line is lineLength bytes with its LF:
// what the daemon compares with daemon.max-ipc-frame-bytes before writing.
func RecordFrameSize(requestID string, sequence, lineLength int64) int64 {
	before, after, _ := recordFrameHalves(requestID, sequence)
	return lineLength - 1 + int64(len(before)+len(after))
}

// WriteRecordFrame writes one EventRecord response: the envelope around
// the record, which write puts on w between the two halves. The record is
// not marshalled here, so a large output goes to the socket in pieces
// exactly as it goes to commands.jsonl (output.WriteRecordJSON), never as
// a second copy. sequence is repeated in the result for the follower's
// continuity check. The caller bounds the frame and buffers w.
func WriteRecordFrame(w io.Writer, requestID string, sequence int64, write func(io.Writer) error) error {
	before, after, err := recordFrameHalves(requestID, sequence)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, before); err != nil {
		return err
	}
	if err := write(w); err != nil {
		return err
	}
	_, err = io.WriteString(w, after)
	return err
}
