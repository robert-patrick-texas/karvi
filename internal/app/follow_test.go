package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testdir"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestNotKeptLine is the not-kept sentence: named
// from the cursor and the start's first_sequence, for the first follow of
// a job without commands.jsonl and for a resume; empty when the stream
// starts right after the cursor. The key it names is the one that was
// false: output.persist-command for a job without a folder (the start's
// artifact directory empty, a --nof job), output.files.commands-jsonl for
// one with a folder.
func TestNotKeptLine(t *testing.T) {
	for _, c := range []struct {
		cursor, first int64
		dir           string
		want          string
	}{
		{0, 1, "/j", ""},
		{4, 5, "", ""},
		{0, 5, "/j", "records 1 to 4 were before this follow and are not kept (output.files.commands-jsonl is false)"},
		{2, 8, "/j", "records 3 to 7 were before this follow and are not kept (output.files.commands-jsonl is false)"},
		{0, 5, "", "records 1 to 4 were before this follow and are not kept (output.persist-command is false)"},
	} {
		if got := NotKeptLine(c.cursor, c.first, c.dir); got != c.want {
			t.Errorf("cursor %d first %d dir %q: %q", c.cursor, c.first, c.dir, got)
		}
	}
}

// TestJobFollowSaysTheAdmissionWarningsOnce: a follow by job ID alone
// prints the start's admission warnings, and a resume after a lost stream,
// whose start repeats them, prints them no more.
func TestJobFollowSaysTheAdmissionWarningsOnce(t *testing.T) {
	const jobID = "261101-120000-00"
	const warning = "spool_width_narrowed: the job runs 4 wide, not 32"
	socket := filepath.Join(testdir.Short(t), "d.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	// The first stream is lost after its start; the second ends with the
	// terminal.
	go func() {
		for attempt := 1; ; attempt++ {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var req ipc.Request
			if err := ipc.Read(conn, &req, 1<<20); err != nil {
				conn.Close()
				continue
			}
			reply := func(event string, v any) {
				raw, _ := json.Marshal(v)
				_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Event: event, Result: raw}, 1<<20)
			}
			reply("", ipc.FollowStart{JobID: jobID, ArtifactDir: "/tmp/jobs/" + jobID, FirstSequence: 1, Warnings: []string{warning}})
			if attempt > 1 {
				reply(ipc.EventStreamTerminal, ipc.FollowTerminal{Outcome: ipc.ActivityOutcome{ActivityID: jobID, JobID: jobID, ArtifactDir: "/tmp/jobs/" + jobID, Summary: records.Summary{FinalStatus: "complete"}}})
			}
			conn.Close()
		}
	}()
	cfg, err := configload.Load(configload.Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{"display.color=never"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	rt := DaemonRuntime{Config: cfg, Socket: socket, MaxFrame: 1 << 20}
	if _, _, err := FollowJobRendering(ctx, CommonOptions{Config: cfg}, rt, jobID, FollowRenderOptions{Format: "text"}, &stdout, &stderr); err != nil {
		t.Fatalf("follow: %v (stderr %q)", err, stderr.String())
	}
	if n := strings.Count(stderr.String(), "spool_width_narrowed"); n != 1 {
		t.Fatalf("the admission warning was said %d times, want once:\n%s", n, stderr.String())
	}
}
