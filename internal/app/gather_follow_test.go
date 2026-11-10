package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/testdir"
	"github.com/robert-patrick-texas/karvi/internal/testpty"
	"github.com/robert-patrick-texas/karvi/records"
)

// The gathered display (display.run.gather) on the follow paths: the
// daemon's follow, the client reading the answers back from the job's
// folder, and the folder follow. The records are a lab job's
// (internal/jobexec/testdata/gather), the display its agreed layout.

// gatherSets are the display settings the lab job's layout was made under,
// and the key.
var gatherSets = []string{"display.color=never", "display.run.footer=", "display.run.gather=true"}

// labJob is the lab job's commands.jsonl copied to path at mode 0640, its
// job ID, its lines, and the gathered layout.
func labJob(t *testing.T, path string) (jobID string, lines [][]byte, want string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "jobexec", "testdata", "gather", "two-commands.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	gathered, err := os.ReadFile(filepath.Join("..", "jobexec", "testdata", "gather", "two-commands.gathered"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(bytes.NewReader(raw))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		lines = append(lines, append([]byte(nil), scan.Bytes()...))
	}
	var first struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatal(err)
	}
	return first.JobID, lines, string(gathered)
}

// followDaemon serves one follow of jobID: the start naming dir, a frame
// per line, then before (when set) and the terminal with the job's exit
// 101.
func followDaemon(t *testing.T, jobID, dir string, lines [][]byte, before func()) string {
	t.Helper()
	return scriptedDaemon(t, jobID, dir, lines, nil, followEnd{exit: 101, final: "error", before: before})
}

// followConn is one connection a scriptedDaemon serves: its start's
// first_sequence, and the lines from (counted from 0) up to to as frames.
type followConn struct{ first, from, to int }

// followEnd is how a scriptedDaemon's last connection ends: after before
// (when set), the terminal with the job's exit and final status; or, with
// running set, no terminal, the connection open until the follower closes
// it (the job still running); or, with lost set, the connection and the
// listener closed (the daemon gone). For every connection: warnings are
// its start's admission warnings, beforeStart (when set) runs with the
// connection's index (from 0) as its request arrives, before its start,
// and served (when set) counts the follows answered.
type followEnd struct {
	exit          int
	final         string
	before        func()
	running, lost bool
	warnings      []string
	beforeStart   func(conn int)
	served        *atomic.Int32
}

// scriptedDaemon serves follows of jobID, one connection per conns entry
// (nil: one with every line from the first): each the start naming dir and
// resuming the request's cursor, then its frames; an earlier connection
// is closed after them, as a lost stream, and the last ends as end says.
func scriptedDaemon(t *testing.T, jobID, dir string, lines [][]byte, conns []followConn, end followEnd) string {
	t.Helper()
	socket := filepath.Join(testdir.Short(t), "d.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	if conns == nil {
		conns = []followConn{{1, 0, len(lines)}}
	}
	go func() {
		for i, c := range conns {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var req ipc.Request
			if err := ipc.Read(conn, &req, 1<<20); err != nil {
				conn.Close()
				return
			}
			if end.served != nil {
				end.served.Add(1)
			}
			if end.beforeStart != nil {
				end.beforeStart(i)
			}
			var fr ipc.FollowRequest
			_ = json.Unmarshal(req.Payload, &fr)
			reply := func(event string, v any) {
				raw, _ := json.Marshal(v)
				_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Event: event, Result: raw}, 1<<24)
			}
			reply("", ipc.FollowStart{JobID: jobID, ArtifactDir: dir, HighestDurableSequence: max(fr.Cursor, int64(c.to)), ResumeCursor: fr.Cursor, FirstSequence: int64(c.first), Warnings: end.warnings})
			for j := c.from; j < c.to; j++ {
				reply(ipc.EventRecord, ipc.RecordFrame{Sequence: int64(j + 1), Record: lines[j]})
			}
			switch {
			case i < len(conns)-1:
			case end.running:
				_, _ = io.Copy(io.Discard, conn)
			case end.lost:
				listener.Close()
			default:
				if end.before != nil {
					end.before()
				}
				reply(ipc.EventStreamTerminal, ipc.FollowTerminal{Cursor: int64(c.to), Outcome: ipc.ActivityOutcome{ActivityID: jobID, JobID: jobID, ArtifactDir: dir, Summary: records.Summary{FinalStatus: end.final}, ExitCode: end.exit, ExitName: exitcode.ExitName(end.exit)}})
			}
			conn.Close()
		}
	}()
	return socket
}

// gatherFollow follows the job through the daemon at socket under sets.
func gatherFollow(t *testing.T, socket, jobID string, sets ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	err := followWith(ctx, t, socket, jobID, &stdout, &stderr, sets...)
	return stdout.String(), stderr.String(), err
}

// followWith is gatherFollow under ctx, onto stdout and stderr.
func followWith(ctx context.Context, t *testing.T, socket, jobID string, stdout, stderr io.Writer, sets ...string) error {
	t.Helper()
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append([]string{`basedir="` + home + `"`, `sharedroot="none"`, `timezone="UTC"`}, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	rt := DaemonRuntime{Config: cfg, Socket: socket, MaxFrame: 1 << 24}
	_, _, err = FollowJobRendering(ctx, CommonOptions{Config: cfg, Operator: credentials.Operator{Home: home}}, rt, jobID, FollowRenderOptions{Format: "text"}, stdout, stderr)
	return err
}

// TestDaemonFollowGathered: the daemon's follow gathers when the follower's
// configuration says so (display.run.gather, never display.command.gather)
// and the job kept its commands.jsonl (its summary.json names it), reading
// the answers back from it at the end and closing it; the key false, or a
// summary naming none (a job that keeps none), is today's display.
func TestDaemonFollowGathered(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, want := labJob(t, filepath.Join(dir, "commands.jsonl"))
	summaryOfDevices(t, dir, 8, "")
	got, stderr, err := gatherFollow(t, followDaemon(t, jobID, dir, lines, nil), jobID, gatherSets...)
	if err != nil || got != want {
		t.Fatalf("follow: %v (stderr %q)\n%s\nwant:\n%s", err, stderr, got, want)
	}
	if fdOpen(filepath.Join(dir, "commands.jsonl")) {
		t.Fatal("commands.jsonl is still open after the follow")
	}
	today, _, err := gatherFollow(t, followDaemon(t, jobID, dir, lines, nil), jobID, append(gatherSets, "display.run.gather=false")...)
	if err != nil || today == want || !strings.Contains(today, "! router01 [127.0.0.1]") {
		t.Fatalf("not gathered: %v\n%s", err, today)
	}
	// A command's key is not the follow's: job follow shows every job by
	// the display.run keys.
	if got, _, err := gatherFollow(t, followDaemon(t, jobID, dir, lines, nil), jobID, append(gatherSets, "display.run.gather=false", "display.command.gather=true")...); err != nil || got != today {
		t.Fatalf("display.command.gather: %v\n%s\nwant today's:\n%s", err, got, today)
	}
	if err := os.Remove(filepath.Join(dir, "commands.jsonl")); err != nil {
		t.Fatal(err)
	}
	summaryNamingNone(t, dir)
	if got, _, err := gatherFollow(t, followDaemon(t, jobID, dir, lines, nil), jobID, gatherSets...); err != nil || got != today {
		t.Fatalf("no commands.jsonl: %v\n%s\nwant today's:\n%s", err, got, today)
	}
}

// TestDaemonFollowGatheredEndFails: a commands.jsonl that cannot be read
// back at the job's end (another file renamed over it, at mode 0644: the
// name is there at every moment, so the follow gathers whenever it looks)
// is run_output_records_unreadable, its path named once, nothing shown;
// the job ended, so the error is EndedError with the job's exit, and
// EndedLine says "job ID ended exit=N; artifacts DIR". Another follow
// error has no line.
func TestDaemonFollowGatheredEndFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.jsonl")
	jobID, lines, _ := labJob(t, path)
	summaryOfDevices(t, dir, 8, "")
	opened := filepath.Join(dir, "opened")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opened, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(opened, 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := gatherFollow(t, followDaemon(t, jobID, dir, lines, func() {
		if err := os.Rename(opened, path); err != nil {
			t.Error(err)
		}
	}), jobID, gatherSets...)
	var ended *EndedError
	if errorcodes.Of(err) != "run_output_records_unreadable" || !errors.As(err, &ended) || ended.Exit != 101 || got != "" {
		t.Fatalf("follow: %v (%T); shown %q", err, err, got)
	}
	if msg := errorcodes.Message(err); msg != "run_output_records_unreadable: "+path+" has mode 0644; the canonical file is 0640 or stricter" {
		t.Fatalf("message %q", msg)
	}
	if line := EndedLine(jobID, dir, err); line != "job "+jobID+" ended exit=101; artifacts "+dir {
		t.Fatalf("line %q", line)
	}
	if line := EndedLine(jobID, dir, errorcodes.Errorf("follow_stream_lost", "lost")); line != "" {
		t.Fatalf("a lost stream's line %q", line)
	}
}

// fdOpen says whether this process holds path open, the path resolved as
// the kernel names an open file (a TMPDIR behind a link).
func fdOpen(path string) bool {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	fds, _ := os.ReadDir("/proc/self/fd")
	for _, fd := range fds {
		if target, err := os.Readlink(filepath.Join("/proc/self/fd", fd.Name())); err == nil && target == path {
			return true
		}
	}
	return false
}

// TestFolderFollowGathered: job follow from the folder gathers a finished
// job's records under the key, read back with openChecked's checks and
// closed after, and
// shows today's display under the key false; the display's failure at the
// end is EndedError with the summary's exit: the file gone once the follow
// had opened it, or its mode opened up (the read-back's own checks).
func TestFolderFollowGathered(t *testing.T) {
	ff := newFolderFollow(t, gatherSets...)
	_, _, want := labJob(t, ff.commands)
	ff.summary(t, "complete")
	var out bytes.Buffer
	if _, err := FollowJobDirectory(context.Background(), ff.common, ff.folder, ff.id, FollowRenderOptions{Format: "text"}, &out, &bytes.Buffer{}); err != nil || out.String() != want {
		t.Fatalf("follow: %v\n%s\nwant:\n%s", err, out.String(), want)
	}
	if fdOpen(ff.commands) {
		t.Fatal("commands.jsonl is still open after the follow")
	}

	ff = newFolderFollow(t, append(gatherSets, "display.run.gather=false")...)
	labJob(t, ff.commands)
	ff.summary(t, "complete")
	out.Reset()
	if _, err := FollowJobDirectory(context.Background(), ff.common, ff.folder, ff.id, FollowRenderOptions{Format: "text"}, &out, &bytes.Buffer{}); err != nil || out.String() == want || !strings.Contains(out.String(), "! router01 [127.0.0.1]") {
		t.Fatalf("the key false: %v\n%s", err, out.String())
	}

	for _, c := range []struct {
		name   string
		change func(path string) error
		msg    string
	}{
		{"gone", os.Remove, "open %s: no such file or directory"},
		{"mode", func(path string) error { return os.Chmod(path, 0o644) }, "%s has mode 0644; the canonical file is 0640 or stricter"},
	} {
		ff = newFolderFollow(t, gatherSets...)
		labJob(t, ff.commands)
		ff.summary(t, "")
		ff.running(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		type outcome struct {
			summary records.Summary
			err     error
		}
		shown := &lockedBuffer{}
		done := make(chan outcome, 1)
		go func() {
			s, err := FollowJobDirectory(ctx, ff.common, ff.folder, ff.id, FollowRenderOptions{Format: "text"}, shown, &bytes.Buffer{})
			done <- outcome{s, err}
		}()
		for !fdOpen(ff.commands) {
			select {
			case got := <-done:
				t.Fatalf("%s: the follow ended before it opened commands.jsonl: %v", c.name, got.err)
			case <-ctx.Done():
				t.Fatalf("%s: the follow did not open commands.jsonl", c.name)
			case <-time.After(10 * time.Millisecond):
			}
		}
		if err := c.change(ff.commands); err != nil {
			t.Fatal(err)
		}
		ff.final(t, "error", 101)
		var got outcome
		select {
		case got = <-done:
		case <-ctx.Done():
			t.Fatalf("%s: the follow did not end", c.name)
		}
		var ended *EndedError
		if errorcodes.Of(got.err) != "run_output_records_unreadable" || !errors.As(got.err, &ended) || ended.Exit != 101 || shown.String() != "" {
			t.Fatalf("%s: follow: %v; shown %q", c.name, got.err, shown.String())
		}
		if msg := errorcodes.Message(got.err); msg != "run_output_records_unreadable: "+fmt.Sprintf(c.msg, ff.commands) {
			t.Fatalf("%s: message %q", c.name, msg)
		}
	}
}

// running writes the job's scoreboard, the job running and updated now,
// so a folder follow of its unfinished summary waits for its end.
func (ff folderFollow) running(t *testing.T) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	board := `{"schema_version":5,"activity_id":"` + ff.id + `","job_id":"` + ff.id + `","status":"running","started_at":"` + now + `","last_updated_at":"` + now + `"}`
	if err := os.WriteFile(filepath.Join(ff.boards, ff.id+".json"), []byte(board), 0o640); err != nil {
		t.Fatal(err)
	}
}

// TestOpenCheckedNamesAGoneFileOnce: the folder follow's open of a gone
// commands.jsonl names its path once (os.Open's error names it; it was
// named twice).
func TestOpenCheckedNamesAGoneFileOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commands.jsonl")
	_, err := openChecked(path, -1)
	if errorcodes.Of(err) != "run_output_records_unreadable" || strings.Count(err.Error(), path) != 1 {
		t.Fatalf("open: %v", err)
	}
}

// cancelOnWrite is a standard output that cancels the invocation's context
// at its nth write, as a Ctrl-C during the read-back does.
type cancelOnWrite struct {
	bytes.Buffer
	n      int
	cancel context.CancelFunc
}

func (w *cancelOnWrite) Write(p []byte) (int, error) {
	if w.n--; w.n == 0 {
		w.cancel()
	}
	return w.Buffer.Write(p)
}

// labCuts are where an interrupt during the lab job's read-back stops its
// gathered display (want), by the write of standard output it comes at:
// at the first group's header, before its answer's first piece; at that
// answer, before the next group (a failure group, which streams nothing).
func labCuts(want string) []struct {
	n     int
	shown string
} {
	return []struct {
		n     int
		shown string
	}{
		{1, "! show version\n! edge[1-2],router[01-04,10] (7)\n"},
		{2, want[:strings.Index(want, "\n\n! router09 (1)\n")+1]},
	}
}

// TestDaemonFollowGatheredInterrupted: the invocation's interrupt during
// the read-back at the job's end (DECISIONS point 4, "Settled at S4")
// stops the gathered display at once, before the answer's next piece or
// the next group: what was written stands, nothing after it, no footer;
// the job ended, so the error is EndedError with the job's exit and the
// code cancelled, and InterruptedDisplay gives the client's two lines, the
// job's ended line and the cancelled error. Another ended error, and an
// interrupt while the job ran (the context's error alone), are not an
// interrupted display.
func TestDaemonFollowGatheredInterrupted(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, want := labJob(t, filepath.Join(dir, "commands.jsonl"))
	summaryOfDevices(t, dir, 8, "")
	var err error
	for _, c := range labCuts(want) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		out := &cancelOnWrite{n: c.n, cancel: cancel}
		err = followWith(ctx, t, followDaemon(t, jobID, dir, lines, nil), jobID, out, &bytes.Buffer{}, gatherSets...)
		cancel()
		var ended *EndedError
		if errorcodes.Of(err) != "cancelled" || !errors.As(err, &ended) || ended.Exit != 101 {
			t.Fatalf("interrupted at write %d: follow: %v (%T)", c.n, err, err)
		}
		if got := out.String(); got != c.shown {
			t.Fatalf("interrupted at write %d: shown %q, want %q", c.n, got, c.shown)
		}
	}
	line, cause, ok := InterruptedDisplay(jobID, dir, err)
	if !ok || line != "job "+jobID+" ended exit=101; artifacts "+dir || errorcodes.Message(cause) != "cancelled: the display of job "+jobID+" was interrupted" {
		t.Fatalf("InterruptedDisplay: %v %q %v", ok, line, cause)
	}
	for _, other := range []error{&EndedError{Exit: 101, Err: errorcodes.Errorf("run_output_records_unreadable", "gone")}, context.Canceled, errorcodes.Errorf("cancelled", "interrupted")} {
		if _, _, ok := InterruptedDisplay(jobID, dir, other); ok {
			t.Errorf("%v is an interrupted display", other)
		}
	}
}

// TestFolderFollowGatheredInterrupted: job follow from the folder stops
// the same way, at the same places, EndedError with the summary's exit,
// the code cancelled.
func TestFolderFollowGatheredInterrupted(t *testing.T) {
	ff := newFolderFollow(t, gatherSets...)
	_, _, want := labJob(t, ff.commands)
	ff.final(t, "error", 101)
	for _, c := range labCuts(want) {
		ctx, cancel := context.WithCancel(context.Background())
		out := &cancelOnWrite{n: c.n, cancel: cancel}
		_, err := FollowJobDirectory(ctx, ff.common, ff.folder, ff.id, FollowRenderOptions{Format: "text"}, out, &bytes.Buffer{})
		cancel()
		var ended *EndedError
		if errorcodes.Of(err) != "cancelled" || !errors.As(err, &ended) || ended.Exit != 101 {
			t.Fatalf("interrupted at write %d: follow: %v (%T)", c.n, err, err)
		}
		if got := out.String(); got != c.shown {
			t.Fatalf("interrupted at write %d: shown %q, want %q", c.n, got, c.shown)
		}
	}
}

// summaryNamingNone writes dir's summary.json for a job that kept no
// commands.jsonl (output.files.commands-jsonl false): its paths name none.
func summaryNamingNone(t *testing.T, dir string) {
	t.Helper()
	body := `{"schema_version":5,"job_id":"x","kind":"run","devices":[{"name":"router01"}],"device_counts":{"total":1},"paths":{"summary":"summary.json"}}`
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
}

// summaryOfDevices writes dir's summary.json naming devices devices, as
// the store writes it at the job's acceptance.
func summaryOfDevices(t *testing.T, dir string, devices int, final string) {
	t.Helper()
	names := make([]string, devices)
	for i := range names {
		names[i] = fmt.Sprintf(`{"name":"router%02d"}`, i+1)
	}
	body := `{"schema_version":5,"job_id":"x","kind":"run","devices":[` + strings.Join(names, ",") + `],"device_counts":{"total":` + fmt.Sprint(devices) + `},"paths":{"commands_jsonl":"` + filepath.Join(dir, "commands.jsonl") + `"}` + final + `}`
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
}

// TestFollowProgressDevices: job follow at a terminal draws the progress
// line over the job's devices as its summary.json names them (the follow
// by job ID through the daemon reads it once, when the start names the
// folder, the read that judges the job's records; the folder follow reads
// it each poll), from done=0/M at the start to the lab job's last counts,
// erased before the blocks. Through the daemon a summary.json that cannot
// be read is run_output_records_unreadable, nothing shown, the job
// unaffected, at a terminal or not (TestFollowGatherJudgedBySummary);
// where the display does not gather it is never read
// (TestFollowRecordsNeverReadTheSummary).
func TestFollowProgressDevices(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, want := labJob(t, filepath.Join(dir, "commands.jsonl"))
	summaryOfDevices(t, dir, 8, "")
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err := followWith(ctx, t, followDaemon(t, jobID, dir, lines, nil), jobID, &out, slave, gatherSets...); err != nil || out.String() != want {
		t.Fatalf("follow: %v\n%s", err, out.String())
	}
	got := term.Wait(t, slave)
	if !strings.HasPrefix(got, "\rgather: done=0/8 fail=0\x1b[K") || !strings.HasSuffix(got, "\rgather: done=8/8 fail=1\x1b[K\r\x1b[K") {
		t.Fatalf("through the daemon the terminal was sent %q", got)
	}

	if err := os.Remove(filepath.Join(dir, "summary.json")); err != nil {
		t.Fatal(err)
	}
	master, slave = testpty.Open(t)
	term = testpty.Record(master)
	out.Reset()
	err := followWith(ctx, t, followDaemon(t, jobID, dir, lines, nil), jobID, &out, slave, gatherSets...)
	var ended *EndedError
	if errorcodes.Of(err) != "run_output_records_unreadable" || errors.As(err, &ended) || !strings.Contains(err.Error(), filepath.Join(dir, "summary.json")) || out.Len() != 0 {
		t.Fatalf("no summary.json: %v; shown %q", err, out.String())
	}
	if got := term.Wait(t, slave); strings.Contains(got, "gather:") {
		t.Fatalf("no summary.json: the terminal was sent %q", got)
	}

	ff := newFolderFollow(t, gatherSets...)
	labJob(t, ff.commands)
	summaryOfDevices(t, ff.folder.Path, 8, `,"final_status":"complete","primary_exit_code":0`)
	master, slave = testpty.Open(t)
	term = testpty.Record(master)
	out.Reset()
	if _, err := FollowJobDirectory(ctx, ff.common, ff.folder, ff.id, FollowRenderOptions{Format: "text"}, &out, slave); err != nil || out.String() != want {
		t.Fatalf("the folder follow: %v\n%s", err, out.String())
	}
	got = term.Wait(t, slave)
	if !strings.HasPrefix(got, "\rgather: done=0/8 fail=0\x1b[K") || !strings.HasSuffix(got, "\rgather: done=8/8 fail=1\x1b[K\r\x1b[K") {
		t.Fatalf("from the folder the terminal was sent %q", got)
	}
}

// TestReadSummaryNamesThePathOnce: a job's summary.json that cannot be
// read is named once in the message: one the open fails on, here gone
// (the open's error names it; it was named twice), and a link at the name
// (refused, named here).
func TestReadSummaryNamesThePathOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summary.json")
	_, err := readSummary(dir)
	if errorcodes.Of(err) != "run_output_records_unreadable" || strings.Count(err.Error(), path) != 1 || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("gone: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "elsewhere.json"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := readSummary(dir); errorcodes.Of(err) != "run_output_records_unreadable" || strings.Count(err.Error(), path) != 1 {
		t.Fatalf("a link: %v", err)
	}
}

// TestFollowRecordsAtATerminalNeverReadTheSummary: job follow --format
// jsonl or json never gathers, the site's display.run.gather true or not,
// so with standard error a terminal it never reads the job's summary.json
// for the progress line: the records pass through though the folder has
// none.
func TestFollowRecordsAtATerminalNeverReadTheSummary(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, _ := labJob(t, filepath.Join(dir, "commands.jsonl"))
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append([]string{`basedir="` + home + `"`, `sharedroot="none"`, `timezone="UTC"`}, gatherSets...)})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"jsonl", "json"} {
		master, slave := testpty.Open(t)
		term := testpty.Record(master)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var stdout bytes.Buffer
		rt := DaemonRuntime{Config: cfg, Socket: followDaemon(t, jobID, dir, lines, nil), MaxFrame: 1 << 24}
		_, _, err := FollowJobRendering(ctx, CommonOptions{Config: cfg, Operator: credentials.Operator{Home: home}}, rt, jobID, FollowRenderOptions{Format: format}, &stdout, slave)
		cancel()
		if got := term.Wait(t, slave); err != nil || strings.Count(stdout.String(), jobID) < len(lines) || strings.Contains(got, "gather:") {
			t.Errorf("--format %s: %v; %d bytes shown; the terminal was sent %q", format, err, stdout.Len(), got)
		}
	}
}

// refusingDaemon answers one follow with the error code, before any
// start.
func refusingDaemon(t *testing.T, code string) string {
	t.Helper()
	socket := filepath.Join(testdir.Short(t), "d.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req ipc.Request
		if err := ipc.Read(conn, &req, 1<<20); err != nil {
			return
		}
		_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Error: &ipc.Error{Code: code, Message: "no such job"}}, 1<<20)
	}()
	return socket
}

// TestRunFollowDrawsAtTheStart: run and crun through the daemon build
// their renderer from the receipt, and draw the progress line over the
// plan's devices when the follow's start comes, as job follow by ID does,
// not when the renderer is built: a follow refused before its start draws
// nothing; one that starts draws done=0/M then the job's counts, M the
// plan's though the job's summary.json (read for its records) names
// three.
func TestRunFollowDrawsAtTheStart(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, want := labJob(t, filepath.Join(dir, "commands.jsonl"))
	summaryOfDevices(t, dir, 3, "")
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append([]string{`basedir="` + home + `"`, `sharedroot="none"`, `timezone="UTC"`}, gatherSets...)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	follow := func(socket string, slave *os.File, out io.Writer) error {
		_, err := followJob(ctx, followOptions{cfg: cfg, socket: socket, maxFrame: 1 << 24, jobID: jobID, render: true, format: "text", devices: 8, stderr: slave}, dir, out)
		return err
	}

	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	if err := follow(refusingDaemon(t, "job_unknown"), slave, io.Discard); errorcodes.Of(err) != "job_unknown" {
		t.Fatalf("refused: %v", err)
	}
	if got := term.Wait(t, slave); got != "" {
		t.Errorf("refused before the start: the terminal was sent %q", got)
	}

	master, slave = testpty.Open(t)
	term = testpty.Record(master)
	var out bytes.Buffer
	if err := follow(followDaemon(t, jobID, dir, lines, nil), slave, &out); err != nil || out.String() != want {
		t.Fatalf("follow: %v\n%s", err, out.String())
	}
	if got := term.Wait(t, slave); !strings.HasPrefix(got, "\rgather: done=0/8 fail=0\x1b[K") || !strings.HasSuffix(got, "\rgather: done=8/8 fail=1\x1b[K\r\x1b[K") {
		t.Errorf("the terminal was sent %q", got)
	}
}

// answerRecord is the requested record of device's one command, show
// version, in a run job's commands.jsonl at sequence seq: its status, and
// out its answer; a record that did not succeed was cancelled.
func answerRecord(jobID, device string, seq int64, position int, status, out string) records.CommandRecord {
	text, enc, digest := output.EncodeOutput([]byte(out))
	r := records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: jobID + "-" + device, ActivityID: jobID, JobID: jobID, ActivityType: "run", CommandKind: "requested",
		Device: records.DeviceProjection{ID: "name:" + device, CanonicalName: device}, InputTarget: device, Channel: records.ChannelShell,
		Dispatch: records.DispatchContext{ScopePosition: position}, CommandIndex: 1, CommandCount: 1, Command: "show version", Sequence: seq,
		Status: status, Output: text, OutputEncoding: enc, OutputBytes: int64(len(out)), OutputSHA256: digest}
	if status != "success" {
		r.Error = &records.StructuredError{Code: "cancelled", Message: "the job was cancelled"}
	}
	return r
}

// writeRecords writes recs as the job's commands.jsonl at path, mode
// 0640, and gives its lines without their line feeds, the follow's frames.
func writeRecords(t *testing.T, path string, recs []records.CommandRecord) [][]byte {
	t.Helper()
	var file bytes.Buffer
	var lines [][]byte
	for i := range recs {
		var one bytes.Buffer
		if _, err := output.WriteRecordLine(&one, &recs[i], output.FromRecord(&recs[i])); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, bytes.TrimSuffix(bytes.Clone(one.Bytes()), []byte("\n")))
		file.Write(one.Bytes())
	}
	if err := os.WriteFile(path, file.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	return lines
}

// cancelAtText is a terminal's standard error (Terminal, so the follow
// takes it for one) that cancels the invocation's context once the
// terminal has been sent text, as a Ctrl-C at that moment does.
type cancelAtText struct {
	f      *os.File
	text   string
	cancel context.CancelFunc
	mu     sync.Mutex
	seen   bytes.Buffer
}

func (c *cancelAtText) Terminal() *os.File { return c.f }

func (c *cancelAtText) Write(p []byte) (int, error) {
	n, err := c.f.Write(p)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen.Write(p); strings.Contains(c.seen.String(), c.text) {
		c.cancel()
	}
	return n, err
}

// clearRestTest and eraseLineTest are the progress line's controls: clear
// to the line's end, and the line erased (its start, then cleared).
const (
	clearRestTest = "\x1b[K"
	eraseLineTest = "\r" + clearRestTest
)

// TestFollowsOwnLinesGoThroughTheProgressLine: the follow's own lines on
// standard error go through the progress line as every other line there
// does: a resume after a lost stream whose start comes past the follow's
// cursor (records 5 and 6 not kept) says so with the line erased first
// and drawn again after it with the same counts.
func TestFollowsOwnLinesGoThroughTheProgressLine(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, _ := labJob(t, filepath.Join(dir, "commands.jsonl"))
	summaryOfDevices(t, dir, 8, "")
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	socket := scriptedDaemon(t, jobID, dir, lines, []followConn{{1, 0, 4}, {7, 6, len(lines)}}, followEnd{exit: 101, final: "error"})
	var out bytes.Buffer
	err := followWith(ctx, t, socket, jobID, &out, slave, gatherSets...)
	got := term.Wait(t, slave)
	if err != nil {
		t.Fatalf("follow: %v; the terminal was sent %q", err, got)
	}
	const warning = "warning: records 5 to 6 were before this follow and are not kept (output.files.commands-jsonl is false)\n"
	at := strings.Index(got, warning)
	if at < 0 || !strings.HasSuffix(got[:at], clearRestTest+eraseLineTest) {
		t.Fatalf("the not-kept line not after an erase: %q", got)
	}
	before := got[:at-len(eraseLineTest)]
	drawn := before[strings.LastIndex(before, "\rgather: "):]
	if drawn != "\rgather: done=0/8 fail=0"+clearRestTest || !strings.HasPrefix(got[at+len(warning):], drawn) {
		t.Fatalf("the line not drawn again after the not-kept line: %q", got)
	}
	if !strings.HasSuffix(got, "\rgather: done=6/8 fail=1"+clearRestTest+eraseLineTest) {
		t.Fatalf("the terminal was sent %q", got)
	}
}

// TestFollowOfAJobCancelledElsewhere: a job cancelled from elsewhere (job
// cancel: its end exit 113, a device's record cancel), the follower's own
// interrupt never come, still ends with its cancel groups, then the
// footer with the job's exit, on both follow paths, through the daemon
// and from the folder: the display is not the interrupted one, and there
// is no error.
func TestFollowOfAJobCancelledElsewhere(t *testing.T) {
	sets := []string{"display.color=never", "display.run.gather=true"}
	ff := newFolderFollow(t, sets...)
	recs := []records.CommandRecord{
		answerRecord(ff.id, "router1", 1, 1, "success", "v\n"),
		answerRecord(ff.id, "router2", 2, 2, "cancel", ""),
	}
	const blocks = "! show version\n! router1 (1)\nv\n\n! router2 (1)\nkarvi: status=cancel error=cancelled: the job was cancelled\n"
	dir := t.TempDir()
	lines := writeRecords(t, filepath.Join(dir, "commands.jsonl"), recs)
	summaryOfDevices(t, dir, 2, "")
	got, stderr, err := gatherFollow(t, scriptedDaemon(t, ff.id, dir, lines, nil, followEnd{exit: 113, final: "cancel"}), ff.id, sets...)
	if want := blocks + "! exit=113 elapsed=0s artifacts=" + dir + "\n"; err != nil || got != want {
		t.Fatalf("through the daemon: %v (stderr %q)\n%q\nwant:\n%q", err, stderr, got, want)
	}

	writeRecords(t, ff.commands, recs)
	ff.final(t, "cancel", 113)
	var out, errOut bytes.Buffer
	_, err = FollowJobDirectory(context.Background(), ff.common, ff.folder, ff.id, FollowRenderOptions{Format: "text"}, &out, &errOut)
	if want := blocks + "! exit=113 elapsed=0s artifacts=" + ff.folder.Path + "\n"; err != nil || out.String() != want {
		t.Fatalf("from the folder: %v (stderr %q)\n%q\nwant:\n%q", err, errOut.String(), out.String(), want)
	}
}

// TestFollowInterruptedWhileTheJobRunsErasesTheLine: the invocation's
// interrupt while the job runs, once its last device has ended, ends the
// follow on both paths (through the daemon, and from the folder) with the
// context's error, nothing gathered printed, and the progress line erased
// before the follow returns, so the client's own line (the job
// continues) never follows a half line.
func TestFollowInterruptedWhileTheJobRunsErasesTheLine(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, _ := labJob(t, filepath.Join(dir, "commands.jsonl"))
	summaryOfDevices(t, dir, 8, "")
	client := "interrupted: job " + jobID + " continues; artifacts " + dir + "\n"
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	err := followWith(ctx, t, scriptedDaemon(t, jobID, dir, lines, nil, followEnd{running: true}), jobID, &out, &cancelAtText{f: slave, text: "done=8/8", cancel: cancel}, gatherSets...)
	if !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("through the daemon: %v; shown %q", err, out.String())
	}
	if _, err := io.WriteString(slave, client); err != nil {
		t.Fatal(err)
	}
	if got := term.Wait(t, slave); !strings.HasSuffix(got, "\rgather: done=8/8 fail=1"+clearRestTest+eraseLineTest+client) {
		t.Errorf("through the daemon the terminal was sent %q", got)
	}

	ff := newFolderFollow(t, gatherSets...)
	labJob(t, ff.commands)
	summaryOfDevices(t, ff.folder.Path, 8, "")
	ff.running(t)
	client = "interrupted: job " + ff.id + " continues; artifacts " + ff.folder.Path + "\n"
	master, slave = testpty.Open(t)
	term = testpty.Record(master)
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out.Reset()
	if _, err := FollowJobDirectory(ctx, ff.common, ff.folder, ff.id, FollowRenderOptions{Format: "text"}, &out, &cancelAtText{f: slave, text: "done=8/8", cancel: cancel}); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("from the folder: %v; shown %q", err, out.String())
	}
	if _, err := io.WriteString(slave, client); err != nil {
		t.Fatal(err)
	}
	if got := term.Wait(t, slave); !strings.HasSuffix(got, "\rgather: done=8/8 fail=1"+clearRestTest+eraseLineTest+client) {
		t.Errorf("from the folder the terminal was sent %q", got)
	}
}

// TestFollowLostErasesTheLine: a follow that ends without the job's end
// and without an interrupt erases the progress line before it returns:
// through the daemon a stream lost for good after the job's records (the
// daemon gone), follow_stream_lost; from the folder a job whose process
// is gone before its first record (no scoreboard), job_orphaned.
func TestFollowLostErasesTheLine(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, _ := labJob(t, filepath.Join(dir, "commands.jsonl"))
	summaryOfDevices(t, dir, 8, "")
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err := followWith(ctx, t, scriptedDaemon(t, jobID, dir, lines, nil, followEnd{lost: true}), jobID, &out, slave, gatherSets...); errorcodes.Of(err) != "follow_stream_lost" || out.Len() != 0 {
		t.Fatalf("through the daemon: %v; shown %q", err, out.String())
	}
	if got := term.Wait(t, slave); !strings.HasSuffix(got, "\rgather: done=8/8 fail=1"+clearRestTest+eraseLineTest) {
		t.Errorf("through the daemon the terminal was sent %q", got)
	}

	ff := newFolderFollow(t, gatherSets...)
	if err := os.WriteFile(ff.commands, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	summaryOfDevices(t, ff.folder.Path, 8, "")
	master, slave = testpty.Open(t)
	term = testpty.Record(master)
	out.Reset()
	if _, err := FollowJobDirectory(ctx, ff.common, ff.folder, ff.id, FollowRenderOptions{Format: "text"}, &out, slave); errorcodes.Of(err) != "job_orphaned" || out.Len() != 0 {
		t.Fatalf("from the folder: %v; shown %q", err, out.String())
	}
	if got := term.Wait(t, slave); got != "\rgather: done=0/8 fail=0"+clearRestTest+eraseLineTest {
		t.Errorf("from the folder the terminal was sent %q", got)
	}
}

// TestFollowGatheredInterruptedInsideAnAnswer: the invocation's interrupt
// while a large answer streams back (one line of 200,000 bytes, read back
// in pieces) stops the display between the answer's pieces on both follow
// paths: what was written stands, the cut line ended with its line
// break, nothing after it; the job ended, so the error is EndedError with
// the job's exit and the code cancelled.
func TestFollowGatheredInterruptedInsideAnAnswer(t *testing.T) {
	ff := newFolderFollow(t, gatherSets...)
	answer := strings.Repeat("x", 200000) + "\n"
	recs := []records.CommandRecord{answerRecord(ff.id, "router1", 1, 1, "success", answer)}
	const header = "! show version\n! router1 (1)\n"
	check := func(path string, err error, got string) {
		t.Helper()
		var ended *EndedError
		if errorcodes.Of(err) != "cancelled" || !errors.As(err, &ended) || ended.Exit != 0 {
			t.Fatalf("%s: follow: %v (%T)", path, err, err)
		}
		cut, ok := strings.CutPrefix(got, header)
		if !ok || !strings.HasSuffix(cut, "x\n") || len(cut) >= len(answer) || strings.Trim(cut, "x") != "\n" {
			t.Fatalf("%s: shown %d bytes, want the header, a part of the answer's one line, and a line break: %.120q ... %q", path, len(got), got, got[max(0, len(got)-20):])
		}
	}

	dir := t.TempDir()
	lines := writeRecords(t, filepath.Join(dir, "commands.jsonl"), recs)
	summaryOfDevices(t, dir, 1, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out := &cancelOnWrite{n: 2, cancel: cancel}
	err := followWith(ctx, t, scriptedDaemon(t, ff.id, dir, lines, nil, followEnd{exit: 0, final: "complete"}), ff.id, out, &bytes.Buffer{}, gatherSets...)
	check("through the daemon", err, out.String())

	writeRecords(t, ff.commands, recs)
	ff.final(t, "complete", 0)
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out = &cancelOnWrite{n: 2, cancel: cancel}
	_, err = FollowJobDirectory(ctx, ff.common, ff.folder, ff.id, FollowRenderOptions{Format: "text"}, out, &bytes.Buffer{})
	check("from the folder", err, out.String())
}
