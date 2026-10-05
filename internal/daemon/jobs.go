package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

// followWriteTimeout bounds one response write to a follower; a follower
// that stops reading is dropped rather than holding the goroutine, and
// with it the records queued for it.
const followWriteTimeout = 30 * time.Second

// job is one accepted job the daemon can be asked to follow: the
// receipt, the canonical file's path, the durable
// edge, the outcome when done, and the live subscribers. It holds no
// record history; a follower behind the edge is served from the file,
// which the daemon reads and sends as record frames, so the client never
// opens it.
type job struct {
	mu          sync.Mutex
	receipt     ipc.JobReceipt
	commands    string
	highest     int64 // highest durable sequence
	outcome     *ipc.ActivityOutcome
	done        chan struct{}
	subscribers map[*subscriber]struct{}

	// The cancel_job side: the job's own
	// cancel function and the first request's time, reason, request ID,
	// and requesting PID, kept after the job ends.
	cancel      context.CancelCauseFunc
	cancelled   *jobexec.JobCancelRequest
	auditCancel func(jobexec.JobCancelRequest)
}

// requestCancel applies a cancel_job request (decisions 2.2, 2.3): a
// finished job answers terminal with its outcome; a running job is
// cancelled with the cancel_job cause on the first request and answers
// cancelling with that request's time on every request.
func (j *job) requestCancel(reason, requestID string, pid, uid int, now time.Time) (ipc.CancelResult, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	res := ipc.CancelResult{JobID: j.receipt.JobID, ArtifactDir: j.receipt.ArtifactDir}
	if j.outcome != nil {
		o := *j.outcome
		res.State, res.Outcome = ipc.CancelStateTerminal, &o
		return res, false
	}
	first := false
	if j.cancelled == nil {
		rq := jobexec.JobCancelRequest{Reason: reason, RequestedAt: now, RequestID: requestID, RequesterPID: pid, RequesterUID: uid}
		j.cancelled = &rq
		first = true
		if j.cancel != nil {
			j.cancel(rq.Cause())
		}
	}
	at := j.cancelled.RequestedAt
	res.State, res.RequestedAt = ipc.CancelStateCancelling, &at
	return res, first
}

// durableRecord is one record on its way to a follower: its sequence,
// its line's offset and length in commands.jsonl, and
// the record without its output when the job keeps the file, which the
// live feed then reads at the offset, so the queue holds no response. A
// job without the file keeps the record as the store handed it: a
// response below the threshold whole, a spooled one with its output
// empty, sent with the notice that it is not kept.
type durableRecord struct {
	sequence int64
	offset   int64
	length   int64
	record   *records.CommandRecord
}

type subscriber struct {
	ch chan durableRecord
}

func newJob() *job {
	return &job{done: make(chan struct{}), subscribers: map[*subscriber]struct{}{}}
}

// open records the canonical file the job writes, which the store created
// before acceptance; a job that writes none (no folder, or
// output.files.commands-jsonl false for the invocation) has no path, and
// its followers are served from the edge.
// Any other failure to see the file is the store's error.
func (j *job) open(artifactDir string, receipt ipc.JobReceipt) error {
	path := ""
	if artifactDir != "" {
		path = filepath.Join(artifactDir, "commands.jsonl")
		if _, err := os.Lstat(path); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			path = ""
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.receipt, j.commands = receipt, path
	return nil
}

// durable is the OnDurable hook: advance the edge and hand the record to
// every live subscriber without blocking; a full queue drops the
// subscriber, whose stream then ends with follow_lagged.
// The queue holds the record, so a follower that stops reading holds in
// the daemon at most FollowQueue records or followWriteTimeout of them.
func (j *job) durable(r *records.CommandRecord, n output.Notice) {
	item := durableRecord{sequence: n.Sequence, offset: n.LineOffset, length: n.LineLength, record: r}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.commands != "" {
		// The file has the line: the queue carries the record without its
		// output, for the copy the frame bound may need.
		light := *r
		light.Output = ""
		item.record = &light
	}
	j.highest = n.Sequence
	for s := range j.subscribers {
		select {
		case s.ch <- item:
		default:
			close(s.ch)
			delete(j.subscribers, s)
		}
	}
}

// finish records the outcome and releases every waiter.
func (j *job) finish(o ipc.ActivityOutcome) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.outcome = &o
	close(j.done)
}

// subscribe registers a live subscriber and returns the durable edge at
// that moment, so the catch-up from the file and the live feed meet
// without a gap or a duplicate.
func (j *job) subscribe() (*subscriber, int64) {
	s := &subscriber{ch: make(chan durableRecord, ipc.FollowQueue)}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.subscribers[s] = struct{}{}
	return s, j.highest
}

func (j *job) unsubscribe(s *subscriber) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, ok := j.subscribers[s]; ok {
		delete(j.subscribers, s)
	}
}

// validateCursor checks a resume cursor against the edge:
// a sequence up to the highest durable one. Past it is
// follow_cursor_stale; whether the file still holds the records after it
// is found by the catch-up.
func (j *job) validateCursor(c int64) error {
	j.mu.Lock()
	highest := j.highest
	j.mu.Unlock()
	if c > highest {
		return errorcodes.Errorf("follow_cursor_stale", "cursor %d is beyond the durable edge %d; restart from the zero cursor", c, highest)
	}
	return nil
}

// catchUp reads the daemon's own canonical file from its start and gives
// fn each line after sequence after and up to sequence upto, without its
// LF: the record frames of a follower behind the edge, or
// resuming. A line's sequence is read from the line's head; the file is
// scanned from the start because a cursor is a sequence alone, and a
// resume is rare. The lines are those the store wrote, so
// the frames equal the file and the follower's jsonl equals it too.
// fn reports whether to go on; false ends the catch-up without error,
// for a follower that stopped reading.
func catchUp(path string, after, upto int64, fn func(sequence int64, line []byte) bool) error {
	if upto <= after || path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return errorcodes.Errorf("follow_cursor_stale", "the canonical file cannot be read for sequence %d: %v", after+1, err)
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 256<<10)
	expect := int64(1)
	for expect <= upto {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return errorcodes.Errorf("follow_cursor_stale", "the canonical file ends before sequence %d", expect)
		}
		var head struct {
			Sequence int64 `json:"sequence"`
		}
		if err := json.Unmarshal(line, &head); err != nil || head.Sequence != expect {
			return errorcodes.Errorf("follow_cursor_stale", "the canonical file carries sequence %d where %d was expected", head.Sequence, expect)
		}
		if expect > after && !fn(expect, line[:len(line)-1]) {
			return nil
		}
		expect++
	}
	return nil
}

// omitOutput is the record as the follow stream sends it when its line of
// lineLength bytes would pass the frame bound maxFrame: a shallow copy with
// Output (and an exec record's stderr) empty and one notice,
// follow_output_omitted, in its place, whose message names the sizes, the
// key, and where the output is: commands.jsonl when the job keeps it
// (kept), or not kept. The record's sizes and digests stay, so the copy
// still describes the streams.
func omitOutput(r *records.CommandRecord, lineLength, maxFrame int64, kept bool) *records.CommandRecord {
	where := "the output is not kept (output.files.commands-jsonl is false)"
	if kept {
		where = "the output is in commands.jsonl"
	}
	return omitted(r, fmt.Sprintf("%s left out of the follow stream: the record's %d-byte line is more than daemon.max-ipc-frame-bytes (%d) allows in a frame; %s", streams(r), lineLength, maxFrame, where), map[string]any{"line_bytes": lineLength, "max_frame_bytes": maxFrame})
}

// omitNotKept is the record of a spooled response (either stream) in a job
// that keeps no commands.jsonl, as the follow stream sends it: the stream
// was in the spool alone, which the record's append removed.
func omitNotKept(r *records.CommandRecord) *records.CommandRecord {
	return omitted(r, fmt.Sprintf("%s left out of the follow stream: it passed output.spool-threshold-bytes and the job keeps no commands.jsonl (output.files.commands-jsonl is false), so the output is not kept", streams(r)), map[string]any{"spooled": true})
}

// streams names what an omission leaves out: the output, and an exec
// record's stderr beside it.
func streams(r *records.CommandRecord) string {
	if r.Stderr == nil {
		return fmt.Sprintf("output of %d bytes", r.OutputBytes)
	}
	var stderr int64
	if r.StderrBytes != nil {
		stderr = *r.StderrBytes
	}
	return fmt.Sprintf("output of %d bytes and stderr of %d bytes", r.OutputBytes, stderr)
}

// spooledAway says whether a stream of the record was spooled, so that the
// record the executor kept holds none of it: the output, or an exec
// record's stderr.
func spooledAway(r *records.CommandRecord) bool {
	if r.Output == "" && r.OutputBytes > 0 {
		return true
	}
	return r.Stderr != nil && *r.Stderr == "" && r.StderrBytes != nil && *r.StderrBytes > 0
}

// omitted is the copy both omissions send: Output and stderr empty and the
// notice appended. The notices slice is the copy's own, so the executor's record,
// which the store and other followers still hold, is not changed under
// them.
func omitted(r *records.CommandRecord, message string, details map[string]any) *records.CommandRecord {
	notice := records.Notice{Code: "follow_output_omitted", Message: message, Details: details}
	omitted := *r
	omitted.Output = ""
	if r.Stderr != nil {
		empty := ""
		omitted.Stderr = &empty
	}
	omitted.Notices = append(append(make([]records.Notice, 0, len(r.Notices)+1), r.Notices...), notice)
	return &omitted
}

// readLineAt reads one record's line from the canonical file at its
// offset: length bytes with the LF, checked to carry the
// sequence, returned without the LF as the frames want it.
func readLineAt(path string, sequence, offset, length int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errorcodes.Errorf("follow_cursor_stale", "the canonical file cannot be read for sequence %d: %v", sequence, err)
	}
	defer f.Close()
	line := make([]byte, length)
	if _, err := f.ReadAt(line, offset); err != nil {
		return nil, errorcodes.Errorf("follow_cursor_stale", "the canonical file does not hold sequence %d at offset %d: %v", sequence, offset, err)
	}
	var head struct {
		Sequence int64 `json:"sequence"`
	}
	if line[len(line)-1] != '\n' || json.Unmarshal(line, &head) != nil || head.Sequence != sequence {
		return nil, errorcodes.Errorf("follow_cursor_stale", "the canonical file carries sequence %d at offset %d where %d was expected", head.Sequence, offset, sequence)
	}
	return line[:len(line)-1], nil
}

// jobTable holds the daemon's followable jobs, bounded like the receipts.
type jobTable struct {
	mu      sync.Mutex
	entries map[string]*job
	order   []string
}

// init makes the table under its lock, as the other tables do: Serve calls
// it before binding the socket, so a caller that reached the daemon through
// the socket is after it in time, and the lock makes that order one the race
// detector sees (a test that adds a job to a live server's table).
func (t *jobTable) init() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = map[string]*job{}
}

func (t *jobTable) add(jobID string, j *job) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[jobID] = j
	t.order = append(t.order, jobID)
	for len(t.order) > MaxReceipts {
		oldest := t.order[0]
		t.order = t.order[1:]
		delete(t.entries, oldest)
	}
}

func (t *jobTable) get(jobID string) *job {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.entries[jobID]
}

// followJob serves one follower: peer UID was checked by the
// caller; the job is looked up, the cursor validated, FollowStart sent,
// the follower subscribed, caught up from the file to the edge at
// subscription, fed live, and given the terminal when the outcome is set.
func (s *Server) followJob(ctx context.Context, conn net.Conn, req ipc.Request) {
	var fr ipc.FollowRequest
	if err := json.Unmarshal(req.Payload, &fr); err != nil {
		s.writeError(conn, "job_request_malformed", err, req.RequestID)
		return
	}
	if err := fr.Validate(); err != nil {
		s.writeCoded(conn, "job_request_malformed", err, req.RequestID)
		return
	}
	j := s.jobTable.get(fr.JobID)
	if j == nil {
		s.logf(req.Operation, req.RequestID, "", fr.JobID, "job_unknown")
		s.writeError(conn, "job_unknown", errorcodes.Errorf("job_unknown", "the daemon holds no job %s", fr.JobID), req.RequestID)
		return
	}
	if err := j.validateCursor(fr.Cursor); err != nil {
		s.logf(req.Operation, req.RequestID, "", fr.JobID, "follow_cursor_stale")
		s.writeCoded(conn, "follow_cursor_stale", err, req.RequestID)
		return
	}
	sub, highest := j.subscribe()
	defer j.unsubscribe(sub)
	j.mu.Lock()
	start := ipc.FollowStart{JobID: fr.JobID, ArtifactDir: j.receipt.ArtifactDir, HighestDurableSequence: highest, ResumeCursor: fr.Cursor, FirstSequence: fr.Cursor + 1, Warnings: j.receipt.Warnings}
	path := j.commands
	j.mu.Unlock()
	// A job without commands.jsonl has no catch-up: the
	// stream begins after the edge at subscription, and the start says so
	// with first_sequence, so the follower can name what it missed. The
	// records before the edge were delivered to the followers that held
	// them live; a follower dropped for lag or the write timeout resumes
	// here the same way.
	if path == "" {
		start.FirstSequence = highest + 1
	}
	write := func(event string, v any) bool {
		raw, err := json.Marshal(v)
		if err != nil {
			return false
		}
		_ = conn.SetWriteDeadline(time.Now().Add(followWriteTimeout))
		return ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Result: raw, Event: event}, s.MaxFrame) == nil
	}
	// writeRecord sends one record frame from the given writer of its
	// JSON, under the frame bound: lineLength is the line's
	// length with its LF, so the frame is that plus the envelope. A record
	// whose frame would pass the bound is sent from load instead, with its
	// output left out and the notice in its place (omitOutput); that copy
	// is small and is marshalled whole. Only a record whose other fields
	// alone pass the bound still ends the stream with ipc_frame_too_large.
	writeRecord := func(sequence, lineLength int64, record func(io.Writer) error, load func() (*records.CommandRecord, error)) bool {
		if ipc.RecordFrameSize(req.RequestID, sequence, lineLength) > s.MaxFrame {
			full, err := load()
			if err != nil {
				s.writeCoded(conn, "follow_cursor_stale", err, req.RequestID)
				return false
			}
			omitted, err := json.Marshal(omitOutput(full, lineLength, s.MaxFrame, path != ""))
			if err != nil {
				s.writeError(conn, "record_encode_failed", err, req.RequestID)
				return false
			}
			if ipc.RecordFrameSize(req.RequestID, sequence, int64(len(omitted))+1) > s.MaxFrame {
				s.logf(req.Operation, req.RequestID, "", fr.JobID, "ipc_frame_too_large")
				s.writeError(conn, "ipc_frame_too_large", errorcodes.Errorf("ipc_frame_too_large", "record %d is %d bytes without its output, more than daemon.max-ipc-frame-bytes (%d) allows in a frame", sequence, len(omitted), s.MaxFrame), req.RequestID)
				return false
			}
			s.logf(req.Operation, req.RequestID, "", fr.JobID, "follow_output_omitted")
			record = func(w io.Writer) error { _, err := w.Write(omitted); return err }
		}
		_ = conn.SetWriteDeadline(time.Now().Add(followWriteTimeout))
		w := bufio.NewWriterSize(conn, 64<<10)
		if err := ipc.WriteRecordFrame(w, req.RequestID, sequence, record); err != nil {
			return false
		}
		return w.Flush() == nil
	}
	if !write("", start) {
		return
	}
	sent, gone := start.FirstSequence-1, false
	err := catchUp(path, fr.Cursor, highest, func(sequence int64, line []byte) bool {
		// A caught-up line is sent from its bytes; only one over the bound
		// is decoded, for the copy without its output.
		load := func() (*records.CommandRecord, error) {
			var r records.CommandRecord
			if err := json.Unmarshal(line, &r); err != nil {
				return nil, errorcodes.Errorf("follow_cursor_stale", "the canonical file's sequence %d does not decode: %v", sequence, err)
			}
			return &r, nil
		}
		if !writeRecord(sequence, int64(len(line))+1, func(w io.Writer) error { _, err := w.Write(line); return err }, load) {
			gone = true
			return false
		}
		sent = sequence
		return true
	})
	if gone {
		return
	}
	if err != nil {
		s.writeCoded(conn, "follow_cursor_stale", err, req.RequestID)
		return
	}
	s.logf(req.Operation, req.RequestID, "", fr.JobID, "following")
	// The live feed: a job with the file sends the
	// line from the file at its offset, as the catch-up does; a job without
	// it sends the record, a spooled one with its output omitted and the
	// notice that it is not kept.
	feed := func(d durableRecord) bool {
		if d.sequence <= sent {
			return true
		}
		sent = d.sequence
		if path != "" {
			if ipc.RecordFrameSize(req.RequestID, d.sequence, d.length) > s.MaxFrame {
				// Over the bound: the copy without its output comes from
				// the queue's record, and the line is not read at all.
				return writeRecord(d.sequence, d.length, nil, func() (*records.CommandRecord, error) { return d.record, nil })
			}
			line, err := readLineAt(path, d.sequence, d.offset, d.length)
			if err != nil {
				s.writeCoded(conn, "follow_cursor_stale", err, req.RequestID)
				return false
			}
			load := func() (*records.CommandRecord, error) {
				var r records.CommandRecord
				if err := json.Unmarshal(line, &r); err != nil {
					return nil, errorcodes.Errorf("follow_cursor_stale", "the canonical file's sequence %d does not decode: %v", d.sequence, err)
				}
				return &r, nil
			}
			return writeRecord(d.sequence, d.length, func(w io.Writer) error { _, err := w.Write(line); return err }, load)
		}
		record := d.record
		if spooledAway(record) {
			record = omitNotKept(record)
			s.logf(req.Operation, req.RequestID, "", fr.JobID, "follow_output_omitted")
		}
		return writeRecord(d.sequence, d.length, func(w io.Writer) error { _, err := output.WriteRecordJSON(w, record); return err }, func() (*records.CommandRecord, error) { return record, nil })
	}
	// finish drains what arrived before the outcome, then ends the stream
	// with the terminal.
	finish := func() {
		for {
			select {
			case d, ok := <-sub.ch:
				if !ok {
					s.writeError(conn, "follow_lagged", errorcodes.Errorf("follow_lagged", "the follower fell more than %d records behind; resume from the last verified cursor", ipc.FollowQueue), req.RequestID)
					return
				}
				if !feed(d) {
					return
				}
				continue
			default:
			}
			break
		}
		j.mu.Lock()
		terminal := ipc.FollowTerminal{Cursor: j.highest, Outcome: *j.outcome}
		j.mu.Unlock()
		_ = write(ipc.EventStreamTerminal, terminal)
		s.logf(req.Operation, req.RequestID, "", fr.JobID, "stream_terminal")
	}
	for {
		select {
		case d, ok := <-sub.ch:
			if !ok {
				s.logf(req.Operation, req.RequestID, "", fr.JobID, "follow_lagged")
				s.writeError(conn, "follow_lagged", errorcodes.Errorf("follow_lagged", "the follower fell more than %d records behind; resume from the last verified cursor", ipc.FollowQueue), req.RequestID)
				return
			}
			if !feed(d) {
				return
			}
		case <-j.done:
			finish()
			return
		case <-ctx.Done():
			// The listener's cancel under a stop: the job is finalizing;
			// deliver its terminal to the follower
			// within the accounting bound rather than dropping the stream.
			select {
			case <-j.done:
				finish()
			case <-time.After(s.accountingBound()):
			}
			return
		}
	}
}

// outcomeOf converts the runner's result to the wire outcome.
func outcomeOf(r records.Summary, exitCode int, exitName, activityID, jobID, artifactDir, errText string) ipc.ActivityOutcome {
	return ipc.ActivityOutcome{ExitCode: exitCode, ExitName: exitName, ActivityID: activityID, JobID: jobID, ArtifactDir: artifactDir, Summary: r, Error: errText}
}
