package daemon

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/records"
)

// followed is what one follow_job stream delivered: the start, every
// record's line as commands.jsonl holds it, and the terminal.
type followed struct {
	start    ipc.FollowStart
	records  [][]byte
	terminal ipc.FollowTerminal
	err      error
}

// follow runs one follow_job from cursor with the given frame cap.
func (f *v5Fixture) follow(jobID string, cursor int64, maxFrame int64) followed {
	var out followed
	out.terminal, out.err = FollowJob(f.ctx(), f.socket, maxFrame, ipc.FollowRequest{JobID: jobID, Cursor: cursor},
		func(s ipc.FollowStart) error { out.start = s; return nil },
		func(_ int64, line []byte) error { out.records = append(out.records, line); return nil })
	return out
}

// followToEnd follows from the zero cursor to the terminal.
func (f *v5Fixture) followToEnd(jobID string) ipc.FollowTerminal {
	f.t.Helper()
	out := f.follow(jobID, 0, 1<<20)
	if out.err != nil {
		f.t.Fatalf("follow %s: %v", jobID, out.err)
	}
	return out.terminal
}

// verifyRecords checks what the stream delivered against the file: every
// line decodes to a record whose sequence follows the one
// before, and the lines together are the file byte for byte.
func verifyRecords(t *testing.T, path string, first int64, lines [][]byte) {
	t.Helper()
	var joined bytes.Buffer
	next := first
	for _, line := range lines {
		var rec records.CommandRecord
		if err := json.Unmarshal(bytes.TrimRight(line, "\n"), &rec); err != nil || rec.Sequence != next {
			t.Fatalf("sequence %d: record %+v err=%v", next, rec.RecordID, err)
		}
		if err := rec.Validate(); err != nil {
			t.Fatalf("sequence %d: %v", next, err)
		}
		joined.Write(line)
		next++
	}
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(joined.Bytes(), file) {
		t.Fatalf("the stream's %d lines (%d bytes) differ from %s (%d bytes)", len(lines), joined.Len(), path, len(file))
	}
}

// TestFollowLiveJobDeliversEveryRecord covers the schema 8 follow stream:
// a
// follower subscribed before the commit receives every record in order
// and a terminal whose outcome equals the summary; a follower started
// after the job ended receives the same records through the catch-up
// path, read by the daemon from its file; the records are the file's
// lines byte for byte; and the device output crosses the socket in the
// record frames and nowhere else.
func TestFollowLiveJobDeliversEveryRecord(t *testing.T) {
	seed := canarytest.Seed(t)
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true, Sets: []string{`security.child-environment-allowlist=["KARVI_TEST_FAKE_OUTPUT"]`}})
	big := strings.Repeat("x", 40<<10) + seed.Raw
	t.Setenv("KARVI_TEST_FAKE_OUTPUT", big)
	jobID := mustID(t)
	sub := f.prepareAndPackageWith(jobID, []string{"show clock", "show version"}, []inventory.Device{direct("127.0.0.1"), direct("core-a.example")}, fixedInput{user: "u", pass: "p"})
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	// Subscribe before the commit through a follower that starts as soon
	// as the receipt names the job.
	var early followed
	var wg sync.WaitGroup
	result, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if err != nil {
		t.Fatal(err)
	}
	wg.Add(1)
	go func() { defer wg.Done(); early = f.follow(jobID, 0, 1<<20) }()
	wg.Wait()
	if early.err != nil {
		t.Fatalf("live follow: %v", early.err)
	}
	if early.start.JobID != jobID || early.start.ArtifactDir != result.Receipt.ArtifactDir || early.start.FirstSequence != 1 {
		t.Fatalf("start=%+v", early.start)
	}
	if len(early.records) != 4 {
		t.Fatalf("records=%d, want 4 (two targets, two commands)", len(early.records))
	}
	path := filepath.Join(result.Receipt.ArtifactDir, "commands.jsonl")
	verifyRecords(t, path, 1, early.records)
	if early.terminal.Outcome.ExitCode != 0 || early.terminal.Outcome.Summary.FinalStatus != "completed" || early.terminal.Cursor != 4 {
		t.Fatalf("terminal=%+v", early.terminal)
	}
	summary, _ := os.ReadFile(filepath.Join(result.Receipt.ArtifactDir, "summary.json"))
	var onDisk records.Summary
	if err := json.Unmarshal(summary, &onDisk); err != nil || onDisk.FinalStatus != early.terminal.Outcome.Summary.FinalStatus || onDisk.ExitCode != early.terminal.Outcome.ExitCode {
		t.Fatalf("summary on disk %+v differs from the terminal %+v", onDisk.FinalStatus, early.terminal.Outcome.Summary.FinalStatus)
	}
	// One record carries a 40 KiB output, past the whole-line threshold
	// of the record writer, so it went to the socket in pieces.
	largest := 0
	for _, line := range early.records {
		if len(line) > largest {
			largest = len(line)
		}
	}
	if largest < 40<<10 {
		t.Fatalf("largest record %d bytes; the fake output did not reach the record", largest)
	}
	// A follower after the end is served from the file: the same records.
	late := f.follow(jobID, 0, 1<<20)
	if late.err != nil || len(late.records) != 4 || !sameTerminal(late.terminal, early.terminal) {
		t.Fatalf("late follow: err=%v records=%d terminal=%+v", late.err, len(late.records), late.terminal)
	}
	for i := range late.records {
		if !bytes.Equal(late.records[i], early.records[i]) {
			t.Fatalf("record %d differs between the live and the caught-up stream", i+1)
		}
	}
	// The canary crossed the socket in the records, as the file holds it,
	// and in no start or terminal.
	if !canary.Found(bytes.Join(late.records, nil), seed) {
		t.Fatal("the record frames lack the device output")
	}
	wire, _ := json.Marshal(struct {
		S ipc.FollowStart
		T ipc.FollowTerminal
	}{late.start, late.terminal})
	if canary.Found(wire, seed) {
		t.Fatal("the start or the terminal carried the device output")
	}
	t.Logf("job %s: %d records (largest line %d bytes), terminal %s, replayed identically from the file", jobID, len(early.records), largest, early.terminal.Outcome.ExitName)
}

// TestFollowResumeAndStaleCursor: a resume
// from a mid-stream cursor receives only the later records; a cursor past
// the edge is follow_cursor_stale; a negative one is malformed; an
// unknown job is job_unknown.
func TestFollowResumeAndStaleCursor(t *testing.T) {
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true})
	jobID := mustID(t)
	sub := f.prepareAndPackageWith(jobID, []string{"show a", "show b", "show c"}, []inventory.Device{direct("127.0.0.1")}, fixedInput{user: "u", pass: "p"})
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	if _, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request); err != nil {
		t.Fatal(err)
	}
	all := f.follow(jobID, 0, 1<<20)
	if all.err != nil || len(all.records) != 3 {
		t.Fatalf("follow: err=%v records=%d", all.err, len(all.records))
	}
	resumed := f.follow(jobID, 1, 1<<20)
	if resumed.err != nil || len(resumed.records) != 2 || !bytes.Equal(resumed.records[0], all.records[1]) || !bytes.Equal(resumed.records[1], all.records[2]) || !sameTerminal(resumed.terminal, all.terminal) {
		t.Fatalf("resume: err=%v records=%d", resumed.err, len(resumed.records))
	}
	if resumed.start.FirstSequence != 2 || resumed.start.ResumeCursor != 1 {
		t.Fatalf("resumed start=%+v", resumed.start)
	}
	atEnd := f.follow(jobID, all.terminal.Cursor, 1<<20)
	if atEnd.err != nil || len(atEnd.records) != 0 || !sameTerminal(atEnd.terminal, all.terminal) {
		t.Fatalf("resume at the end: err=%v records=%d", atEnd.err, len(atEnd.records))
	}
	for name, cursor := range map[string]int64{"one past the edge": 4, "far past the edge": 10} {
		if out := f.follow(jobID, cursor, 1<<20); errorcodes.Of(out.err) != "follow_cursor_stale" {
			t.Errorf("%s: %v", name, out.err)
		}
	}
	if out := f.follow(jobID, -1, 1<<20); errorcodes.Of(out.err) != "job_request_malformed" {
		t.Errorf("negative cursor: %v", out.err)
	}
	if out := f.follow(mustID(t), 0, 1<<20); errorcodes.Of(out.err) != "job_unknown" {
		t.Errorf("unknown job: %v", out.err)
	}
	t.Logf("job %s: resume from sequence 1 gave %d records; two stale cursors refused; unknown job refused", jobID, len(resumed.records))
}

// TestFollowLaggedSubscriberResumes: a subscriber whose
// queue overflows is ended with follow_lagged and resumes from its last
// delivered sequence without loss, the daemon reading the records after
// it from its file.
func TestFollowLaggedSubscriberResumes(t *testing.T) {
	j := newJob()
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.jsonl")
	var file bytes.Buffer
	// Two more records than the queue holds, appended through the hook.
	total := ipc.FollowQueue + 2
	s, _ := j.subscribe()
	for i := 1; i <= total; i++ {
		line := []byte(`{"sequence":` + itoa(i) + `,"record_id":"r` + itoa(i) + `"}` + "\n")
		file.Write(line)
		j.durable(&records.CommandRecord{Sequence: int64(i), RecordID: "r" + itoa(i)}, output.Notice{Sequence: int64(i), LineLength: int64(len(line)), RecordID: "r" + itoa(i)})
	}
	if err := os.WriteFile(path, file.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-s.ch; ok {
		// Drain: the channel was closed when the queue overflowed.
		for range s.ch {
		}
	}
	j.mu.Lock()
	if _, still := j.subscribers[s]; still {
		j.mu.Unlock()
		t.Fatal("the lagged subscriber was not dropped")
	}
	j.commands = path
	j.mu.Unlock()
	// The resume from the record before the overflow is served from the
	// file: sequence 2 onward, each line without its LF.
	var got []int64
	err := catchUp(path, 1, int64(total), func(sequence int64, line []byte) bool {
		if bytes.HasSuffix(line, []byte("\n")) || !bytes.Contains(line, []byte(`"sequence":`+itoa(int(sequence))+`,`)) {
			t.Fatalf("sequence %d: line %q", sequence, line)
		}
		got = append(got, sequence)
		return true
	})
	if err != nil || len(got) != total-1 || got[len(got)-1] != int64(total) {
		t.Fatalf("catch-up: err=%v records=%d", err, len(got))
	}
	if err := j.validateCursor(got[len(got)-1]); err != nil {
		t.Fatalf("the caught-up cursor must validate: %v", err)
	}
	if err := j.validateCursor(int64(total) + 1); errorcodes.Of(err) != "follow_cursor_stale" {
		t.Fatalf("a cursor past the edge must be stale: %v", err)
	}
	t.Logf("%d records through a %d-slot queue: subscriber dropped, %d recovered from the file", total, ipc.FollowQueue, len(got))
}

func itoa(i int) string { return strconv.Itoa(i) }

// TestFollowJobWithoutFileStartsAtTheEdge:
// a job that writes no commands.jsonl (open finds none and keeps no path)
// gives the follower that started with it every record live; a follower
// after the edge gets a start whose first_sequence is the edge plus one,
// no record, and the terminal; a resume from a mid-stream cursor the
// same; a cursor past the edge is still stale. The job is put in the
// server's table by hand, since until section (d) every job through the
// daemon writes the file.
func TestFollowJobWithoutFileStartsAtTheEdge(t *testing.T) {
	f := newV5Fixture(t)
	dir := t.TempDir()
	jobID := mustID(t)
	j := newJob()
	if err := j.open(dir, ipc.JobReceipt{JobID: jobID, ArtifactDir: dir}); err != nil || j.commands != "" {
		t.Fatalf("open on a folder without the file: path %q err %v", j.commands, err)
	}
	if err := j.open("", ipc.JobReceipt{JobID: jobID}); err != nil || j.commands != "" {
		t.Fatalf("open with no folder: path %q err %v", j.commands, err)
	}
	f.s.jobTable.add(jobID, j)
	// A follower subscribed before any record, as the run's own client is.
	var live followed
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); live = f.follow(jobID, 0, 1<<20) }()
	// Wait until it is subscribed: the start is written under the job's
	// lock after subscribe, so a subscriber count of one means the start
	// went out with highest 0.
	for i := 0; i < 200; i++ {
		j.mu.Lock()
		n := len(j.subscribers)
		j.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	const total = 3
	for i := 1; i <= total; i++ {
		r := &records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r" + itoa(i), ActivityID: jobID, JobID: jobID, ActivityType: "run", Sequence: int64(i), CommandKind: "requested", Status: "succeeded", OutputEncoding: "utf-8", Output: "out " + itoa(i)}
		j.durable(r, output.Notice{Sequence: int64(i), LineLength: 100, RecordID: r.RecordID})
	}
	outcome := ipc.ActivityOutcome{ExitCode: 0, ExitName: "ExitSuccess", ActivityID: jobID, JobID: jobID, Summary: records.Summary{FinalStatus: "completed"}}
	j.finish(outcome)
	wg.Wait()
	if live.err != nil || live.start.FirstSequence != 1 || len(live.records) != total || live.terminal.Cursor != total {
		t.Fatalf("live follow: err=%v start=%+v records=%d terminal=%+v", live.err, live.start, len(live.records), live.terminal)
	}
	for i, line := range live.records {
		var rec records.CommandRecord
		if err := json.Unmarshal(line, &rec); err != nil || rec.Sequence != int64(i+1) || rec.Output != "out "+itoa(i+1) {
			t.Fatalf("live record %d: %s (%v)", i+1, line, err)
		}
	}
	// After the edge: nothing to catch up from, the start says so.
	late := f.follow(jobID, 0, 1<<20)
	if late.err != nil || late.start.ArtifactDir != "" || late.start.HighestDurableSequence != total || late.start.FirstSequence != total+1 || len(late.records) != 0 || !sameTerminal(late.terminal, live.terminal) {
		t.Fatalf("late follow: err=%v start=%+v records=%d", late.err, late.start, len(late.records))
	}
	resumed := f.follow(jobID, 1, 1<<20)
	if resumed.err != nil || resumed.start.ResumeCursor != 1 || resumed.start.FirstSequence != total+1 || len(resumed.records) != 0 || resumed.terminal.Cursor != total {
		t.Fatalf("resume: err=%v start=%+v records=%d", resumed.err, resumed.start, len(resumed.records))
	}
	if out := f.follow(jobID, total+1, 1<<20); errorcodes.Of(out.err) != "follow_cursor_stale" {
		t.Fatalf("a cursor past the edge: %v", out.err)
	}
	t.Logf("job %s without commands.jsonl: the live follower got %d records; a follow from cursor 0 after the end got first_sequence %d and no record; from cursor 1 first_sequence %d", jobID, len(live.records), late.start.FirstSequence, resumed.start.FirstSequence)
}

// TestFollowFrameBoundOmitsOutput: a
// record whose frame would pass daemon.max-ipc-frame-bytes reaches the
// follower with its output left out and the notice follow_output_omitted
// in its place, output_bytes and output_sha256 intact, on the live path
// and on the catch-up path alike, while the file holds the whole line;
// and the stream goes on to its terminal. The client reads the same
// bound, so the frame the daemon sends must fit the client's reader too.
func TestFollowFrameBoundOmitsOutput(t *testing.T) {
	const bound = 64 << 10
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true, MaxFrame: bound, Sets: []string{`security.child-environment-allowlist=["KARVI_TEST_FAKE_OUTPUT"]`}})
	big := strings.Repeat("y", 100<<10)
	t.Setenv("KARVI_TEST_FAKE_OUTPUT", big)
	jobID := mustID(t)
	sub := f.prepareAndPackageWith(jobID, []string{"show clock", "show version"}, []inventory.Device{direct("127.0.0.1")}, fixedInput{user: "u", pass: "p"})
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	result, err := CommitJob(f.ctx(), f.socket, bound, sub.request)
	if err != nil {
		t.Fatal(err)
	}
	live := f.follow(jobID, 0, bound)
	if live.err != nil || len(live.records) != 2 || live.terminal.Cursor != 2 || live.terminal.Outcome.ExitCode != 0 {
		t.Fatalf("live follow: err=%v records=%d terminal=%+v", live.err, len(live.records), live.terminal)
	}
	path := filepath.Join(result.Receipt.ArtifactDir, "commands.jsonl")
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimRight(file, "\n"), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("the file holds %d lines", len(lines))
	}
	for i, line := range live.records {
		var sent, kept records.CommandRecord
		if err := json.Unmarshal(line, &sent); err != nil {
			t.Fatalf("record %d: %v", i+1, err)
		}
		if err := sent.Validate(); err != nil {
			t.Fatalf("record %d as sent: %v", i+1, err)
		}
		if err := json.Unmarshal(lines[i], &kept); err != nil {
			t.Fatalf("file line %d: %v", i+1, err)
		}
		if len(lines[i]) <= bound || !strings.Contains(kept.Output, big) || kept.OutputOmitted() != nil {
			t.Fatalf("file line %d (%d bytes) does not hold the whole output without a notice", i+1, len(lines[i]))
		}
		if len(line) > bound || sent.Output != "" || sent.OutputBytes != kept.OutputBytes || sent.OutputSHA256 != kept.OutputSHA256 || sent.OutputSHA256 == "" {
			t.Fatalf("record %d as sent (%d bytes): output %d bytes, output_bytes %d (file %d), sha %q", i+1, len(line), len(sent.Output), sent.OutputBytes, kept.OutputBytes, sent.OutputSHA256)
		}
		n := sent.OutputOmitted()
		if n == nil || len(sent.Notices) != len(kept.Notices)+1 {
			t.Fatalf("record %d as sent carries notices %+v", i+1, sent.Notices)
		}
		want := "output of " + itoa(int(kept.OutputBytes)) + " bytes left out of the follow stream: the record's " + itoa(len(lines[i])+1) + "-byte line is more than daemon.max-ipc-frame-bytes (" + itoa(bound) + ") allows in a frame; the output is in commands.jsonl"
		if n.Message != want || n.Details["line_bytes"] != float64(len(lines[i])+1) || n.Details["max_frame_bytes"] != float64(bound) {
			t.Fatalf("record %d notice: %q %v", i+1, n.Message, n.Details)
		}
	}
	// The catch-up path decodes the file's line for the same copy.
	late := f.follow(jobID, 0, bound)
	if late.err != nil || len(late.records) != 2 || !sameTerminal(late.terminal, live.terminal) {
		t.Fatalf("late follow: err=%v records=%d", late.err, len(late.records))
	}
	for i := range late.records {
		if !bytes.Equal(late.records[i], live.records[i]) {
			t.Fatalf("record %d differs between the live and the caught-up stream:\n%s\n%s", i+1, live.records[i], late.records[i])
		}
	}
	t.Logf("job %s: two %d-byte lines over the %d-byte bound sent as %d and %d bytes with %s, live and caught up alike", jobID, len(lines[0])+1, bound, len(live.records[0]), len(live.records[1]), "follow_output_omitted")
}

// TestOmitOutputLeavesTheRecordAlone: the copy has the notice and no
// output; the record the executor and the store hold is unchanged, its
// notices slice not shared; and the wording for a job without the file.
func TestOmitOutputLeavesTheRecordAlone(t *testing.T) {
	r := &records.CommandRecord{Sequence: 3, Output: "big", OutputBytes: 3, OutputSHA256: "d", Notices: []records.Notice{{Code: "icmp_packet_loss"}}}
	got := omitOutput(r, 70000, 65536, false)
	if r.Output != "big" || len(r.Notices) != 1 {
		t.Fatalf("the executor's record changed: %+v", r)
	}
	if got.Output != "" || got.OutputBytes != 3 || got.OutputSHA256 != "d" || got.Sequence != 3 || len(got.Notices) != 2 || got.Notices[0].Code != "icmp_packet_loss" {
		t.Fatalf("copy: %+v", got)
	}
	n := got.OutputOmitted()
	if n == nil || !strings.HasSuffix(n.Message, "; the output is not kept (output.files.commands-jsonl is false)") || !strings.Contains(n.Message, "70000-byte line is more than daemon.max-ipc-frame-bytes (65536)") {
		t.Fatalf("notice: %+v", n)
	}
	got.Notices[0].Code = "changed"
	if r.Notices[0].Code != "icmp_packet_loss" {
		t.Fatal("the copy's notices share the record's slice")
	}
}

// sameTerminal compares two terminals by cursor and outcome identity.
func sameTerminal(a, b ipc.FollowTerminal) bool {
	return a.Cursor == b.Cursor && a.Outcome.ExitCode == b.Outcome.ExitCode && a.Outcome.JobID == b.Outcome.JobID && a.Outcome.Summary.FinalStatus == b.Outcome.Summary.FinalStatus && a.Outcome.Summary.EndedAt.Equal(b.Outcome.Summary.EndedAt)
}

// TestFollowNotKeptSpooledRecord is the follow stream for a job without
// commands.jsonl: a record whose response passed the spool threshold
// reaches the live follower with its output omitted and the notice that it
// is not kept, output_bytes and output_sha256 intact; a record below the
// threshold is sent whole from the record as before.
func TestFollowNotKeptSpooledRecord(t *testing.T) {
	f := newV5Fixture(t)
	jobID := mustID(t)
	j := newJob()
	if err := j.open("", ipc.JobReceipt{JobID: jobID}); err != nil {
		t.Fatal(err)
	}
	f.s.jobTable.add(jobID, j)
	var live followed
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); live = f.follow(jobID, 0, 1<<20) }()
	for i := 0; i < 200; i++ {
		j.mu.Lock()
		n := len(j.subscribers)
		j.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	small := &records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r1", ActivityID: jobID, JobID: jobID, ActivityType: "run", Sequence: 1, CommandKind: "requested", Status: "succeeded", OutputEncoding: "utf-8", Output: "ok\n", OutputBytes: 3, OutputSHA256: "a"}
	spooled := &records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r2", ActivityID: jobID, JobID: jobID, ActivityType: "run", Sequence: 2, CommandKind: "requested", Status: "succeeded", OutputEncoding: "utf-8", OutputBytes: 5256000, OutputSHA256: "b", Notices: []records.Notice{}}
	j.durable(small, output.Notice{Sequence: 1, LineLength: 300, LineOffset: -1, RecordID: "r1"})
	j.durable(spooled, output.Notice{Sequence: 2, LineLength: 5256300, LineOffset: -1, RecordID: "r2"})
	j.finish(ipc.ActivityOutcome{ExitCode: 0, ExitName: "ExitSuccess", ActivityID: jobID, JobID: jobID, Summary: records.Summary{FinalStatus: "completed"}})
	wg.Wait()
	if live.err != nil || len(live.records) != 2 {
		t.Fatalf("live follow: err=%v records=%d", live.err, len(live.records))
	}
	var first, second records.CommandRecord
	if err := json.Unmarshal(live.records[0], &first); err != nil || first.Output != "ok\n" || first.OutputOmitted() != nil {
		t.Fatalf("the small record as sent: %s (%v)", live.records[0], err)
	}
	if err := json.Unmarshal(live.records[1], &second); err != nil || second.Output != "" || second.OutputBytes != 5256000 || second.OutputSHA256 != "b" {
		t.Fatalf("the spooled record as sent: %s (%v)", live.records[1], err)
	}
	n := second.OutputOmitted()
	if n == nil || n.Message != "output of 5256000 bytes left out of the follow stream: it passed output.spool-threshold-bytes and the job keeps no commands.jsonl (output.files.commands-jsonl is false), so the output is not kept" || n.Details["spooled"] != true {
		t.Fatalf("notice: %+v", n)
	}
	if spooled.Notices == nil || len(spooled.Notices) != 0 {
		t.Fatalf("the executor's record was changed: %+v", spooled.Notices)
	}
}
