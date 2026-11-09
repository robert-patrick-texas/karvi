package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testdir"
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
// per line, then before (when set) and the terminal with the job's exit 0.
func followDaemon(t *testing.T, jobID, dir string, lines [][]byte, before func()) string {
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
		reply := func(event string, v any) {
			raw, _ := json.Marshal(v)
			_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Event: event, Result: raw}, 1<<24)
		}
		reply("", ipc.FollowStart{JobID: jobID, ArtifactDir: dir, FirstSequence: 1})
		for i, line := range lines {
			reply(ipc.EventRecord, ipc.RecordFrame{Sequence: int64(i + 1), Record: line})
		}
		if before != nil {
			before()
		}
		reply(ipc.EventStreamTerminal, ipc.FollowTerminal{Cursor: int64(len(lines)), Outcome: ipc.ActivityOutcome{ActivityID: jobID, JobID: jobID, ArtifactDir: dir, Summary: records.Summary{FinalStatus: "error"}, ExitCode: 101, ExitName: "ExitPartialFailure"}})
	}()
	return socket
}

// gatherFollow follows the job through the daemon at socket under sets.
func gatherFollow(t *testing.T, socket, jobID string, sets ...string) (string, string, error) {
	t.Helper()
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append([]string{`basedir="` + home + `"`, `sharedroot="none"`, `timezone="UTC"`}, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	rt := DaemonRuntime{Config: cfg, Socket: socket, MaxFrame: 1 << 24}
	_, _, err = FollowJobRendering(ctx, CommonOptions{Config: cfg, Operator: credentials.Operator{Home: home}}, rt, jobID, FollowRenderOptions{Format: "text"}, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// TestDaemonFollowGathered: the daemon's follow gathers when the follower's
// configuration says so (display.run.gather, never display.command.gather)
// and the job's folder holds its commands.jsonl, reading the answers back
// from it at the end and closing it; the key false, or the file absent at
// the start (a job that keeps none), is today's display.
func TestDaemonFollowGathered(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, want := labJob(t, filepath.Join(dir, "commands.jsonl"))
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
		now := time.Now().UTC().Format(time.RFC3339Nano)
		board := `{"schema_version":5,"activity_id":"` + ff.id + `","job_id":"` + ff.id + `","status":"running","started_at":"` + now + `","last_updated_at":"` + now + `"}`
		if err := os.WriteFile(filepath.Join(ff.boards, ff.id+".json"), []byte(board), 0o640); err != nil {
			t.Fatal(err)
		}
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
