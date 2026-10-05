package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestSchema8FollowPayloadsMatchTheSchema: the
// request, the start, a record frame carrying a real record, and the
// terminal validate against the schema file under schema 8.
func TestSchema8FollowPayloadsMatchTheSchema(t *testing.T) {
	fr := FollowRequest{JobID: plantest.JobID, Cursor: 3}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/follow_request", fr)
	start := FollowStart{JobID: plantest.JobID, ArtifactDir: "/tmp/jobs/2026-09-22/" + plantest.JobID, HighestDurableSequence: 5, ResumeCursor: 3, FirstSequence: 4, Warnings: []string{"spool_width_narrowed: /tmp/karvi-1000 has 1 bytes free"}}
	if err := start.Validate(&fr); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/follow_start", start)
	rec := sampleRecord()
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	frame := RecordFrame{Sequence: 4, Record: line}
	if err := frame.Validate(3); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/record_frame", frame)
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/follow_terminal", FollowTerminal{Cursor: 5, Outcome: ActivityOutcome{ExitCode: 0, ExitName: "ExitSuccess", ActivityID: plantest.JobID, JobID: plantest.JobID, ArtifactDir: start.ArtifactDir, Summary: records.Summary{SchemaVersion: 2, FinalStatus: "completed", Mode: "live"}}})
	req, err := NewRequest("r1", OpFollowJob, "0.13.0", fr)
	if err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/request", req)
	// Validation vectors: a start that does not resume the request, one
	// beginning at or before the cursor, a frame out of sequence, a
	// negative cursor.
	for name, s := range map[string]FollowStart{
		"other job":       {JobID: plantest.JobID[:len(plantest.JobID)-1] + "x", HighestDurableSequence: 5, ResumeCursor: 3, FirstSequence: 4},
		"other cursor":    {JobID: plantest.JobID, HighestDurableSequence: 5, ResumeCursor: 2, FirstSequence: 3},
		"edge behind":     {JobID: plantest.JobID, HighestDurableSequence: 2, ResumeCursor: 3, FirstSequence: 4},
		"first at cursor": {JobID: plantest.JobID, HighestDurableSequence: 5, ResumeCursor: 3, FirstSequence: 3},
		"relative folder": {JobID: plantest.JobID, ArtifactDir: "jobs/x", HighestDurableSequence: 5, ResumeCursor: 3, FirstSequence: 4},
	} {
		if err := s.Validate(&fr); errorcodes.Of(err) != "ipc_result_malformed" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := (&RecordFrame{Sequence: 5, Record: line}).Validate(3); errorcodes.Of(err) != "ipc_result_malformed" {
		t.Errorf("out of sequence: %v", err)
	}
	if err := (&RecordFrame{Sequence: 4}).Validate(3); errorcodes.Of(err) != "ipc_result_malformed" {
		t.Errorf("empty record: %v", err)
	}
	if err := (&FollowRequest{JobID: plantest.JobID, Cursor: -1}).Validate(); errorcodes.Of(err) != "job_request_malformed" {
		t.Errorf("negative cursor: %v", err)
	}
}

// TestWriteRecordFrame: the frame the daemon writes around a record
// decodes as a response whose result is the record frame, the record's
// bytes exactly as written between the halves, and its length is what
// RecordFrameSize says.
func TestWriteRecordFrame(t *testing.T) {
	rec := sampleRecord()
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := WriteRecordFrame(w, "req-\"7\"", 4, func(w io.Writer) error { _, err := w.Write(line); return err }); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	frame := buf.Bytes()
	if !bytes.HasSuffix(frame, []byte("\n")) || bytes.Count(frame, []byte("\n")) != 1 {
		t.Fatalf("frame is not one line: %q", frame)
	}
	// The size the daemon bounds is the frame's exact size, the request
	// ID's escapes and the sequence's digits included.
	if want := RecordFrameSize("req-\"7\"", 4, int64(len(line))+1); int64(len(frame)) != want {
		t.Fatalf("frame is %d bytes, RecordFrameSize says %d", len(frame), want)
	}
	var resp Response
	if err := NewLineReader(bytes.NewReader(frame), 1<<20).Next(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.IPCSchemaVersion != SchemaVersion || resp.RequestID != `req-"7"` || resp.Event != EventRecord || resp.Error != nil {
		t.Fatalf("response %+v", resp)
	}
	var rf RecordFrame
	if err := json.Unmarshal(resp.Result, &rf); err != nil {
		t.Fatal(err)
	}
	if rf.Sequence != 4 || !bytes.Equal(rf.Record, line) {
		t.Fatalf("frame carried sequence %d and %d bytes; want 4 and the %d-byte line", rf.Sequence, len(rf.Record), len(line))
	}
	if strings.Contains(string(frame), "\n"+"{") {
		t.Fatal("a second line began inside the frame")
	}
}

// sampleRecord is one valid command record for the schema checks.
func sampleRecord() *records.CommandRecord {
	now := time.Date(2026, 9, 22, 21, 0, 0, 0, time.UTC)
	return &records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: plantest.JobID, JobID: plantest.JobID, ActivityID: plantest.JobID, ActivityType: "run", Sequence: 4, Operator: records.Operator{Username: "u", UID: 1000}, Device: records.DeviceProjection{ID: "name:r1", Name: "r1", CanonicalName: "r1", Groups: []string{}}, InputTarget: "r1", TransformedName: "r1", DNSSuffixAction: "add-suffix:none", AddressCandidates: []string{}, Platform: "cisco_iosxe", Transport: "native", Port: 22, Channel: records.ChannelShell, Dispatch: records.DispatchContext{Mode: "serial"}, CommandIndex: 1, CommandCount: 1, CommandKind: "requested", Command: "show clock", CommandSHA256: strings.Repeat("a", 64), Status: "succeeded", Output: "*10:00:00.000 UTC\n", OutputBytes: 18, OutputEncoding: "utf-8", OutputSHA256: strings.Repeat("b", 64), Notices: []records.Notice{}, Timing: records.Timing{QueuedAt: now, EndedAt: now}}
}

// TestSchema10ReceiptCarriesWarnings: the receipt's
// admission warnings match the schema, are absent when none, and a follow
// start repeats them.
func TestSchema10ReceiptCarriesWarnings(t *testing.T) {
	at := time.Date(2026, 9, 27, 0, 33, 4, 0, time.UTC)
	r := JobReceipt{JobID: plantest.JobID, AcceptedAt: at, ArtifactDir: "/tmp/jobs/260927/" + plantest.JobID, PlanDigest: executionplan.SumCommands(plantest.Commands), Mode: executionplan.ModeLive, Warnings: []string{"spool_width_narrowed: /tmp/karvi-1000 has 60070989824 bytes free, 1073741824 per command in flight (output.max-command-bytes): the job runs 55 at a time instead of 64"}}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/job_receipt", r)
	b, _ := json.Marshal(JobReceipt{JobID: plantest.JobID, AcceptedAt: at, ArtifactDir: "/x", PlanDigest: executionplan.SumCommands(plantest.Commands), Mode: executionplan.ModeLive})
	if strings.Contains(string(b), "warnings") {
		t.Fatalf("no warnings, no field: %s", b)
	}
}
