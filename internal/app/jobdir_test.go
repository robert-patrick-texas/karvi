package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// The job directory is derived from the ID's instant
// in the effective timezone, the path the runner wrote.
func TestJobDirectoryForIDAcrossMidnight(t *testing.T) {
	load := func(sets ...string) configload.Snapshot {
		snap, err := configload.Load(configload.Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: sets})
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	// The stamp is read in the effective timezone: one
	// ID names the same wall-clock second in every zone, and the day folder
	// is the stamp's date.
	id := "260915-233000-07"
	at, err := JobIDTime(id, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !at.Equal(time.Date(2026, 9, 15, 23, 30, 0, 0, time.UTC)) {
		t.Fatalf("instant %v", at)
	}
	for _, zone := range []string{"America/New_York", "UTC", "Asia/Tokyo"} {
		dir, _, _, err := jobDirectoryFor(load(`timezone="`+zone+`"`), "/base", "/home/x", id)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("/base", "jobs", "260915", id); dir != want {
			t.Errorf("%s: %s, want %s", zone, dir, want)
		}
	}
	dir, _, _, err := jobDirectoryFor(load(`output.root="/var/karvi/out"`, `timezone="UTC"`), "/base", "/home/x", id)
	if err != nil || dir != filepath.Join("/var/karvi/out", "260915", id) {
		t.Fatalf("output.root: %s %v", dir, err)
	}
	for _, bad := range []string{"", "x", "260915-233000-0", "261315-233000-00", "20260915T233000.123456-0400-0123456789abcdefghjk"} {
		if _, err := JobIDTime(bad, time.UTC); err == nil || errorcodes.Of(err) != "job_request_malformed" {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// TestLocateJobDirectoryUnreachable: a job folder whose day folder cannot
// be read is found, and InspectJobDirectory names why,
// run_output_records_unreadable, never a job that does not exist (EXAMPLES
// chapter 61, issue 4); an absent one is not found.
func TestLocateJobDirectoryUnreachable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "jobs")
	cfg, err := configload.Load(configload.Options{HomeDir: dir, SkipAuto: true, Environment: []string{}, Sets: []string{`basedir="` + dir + `"`, `sharedroot="none"`, `output.root="` + root + `"`, `timezone="UTC"`}})
	if err != nil {
		t.Fatal(err)
	}
	common := CommonOptions{Config: cfg, Operator: credentials.Operator{Home: dir}}
	id := "261016-015356-00"
	day := filepath.Join(root, "261016")
	if err := os.MkdirAll(filepath.Join(day, id), 0o700); err != nil {
		t.Fatal(err)
	}
	if f, err := LocateJobDirectory(common, "261016-015357-00"); err != nil || f.Found {
		t.Fatalf("absent: %+v %v", f, err)
	}
	if err := os.Chmod(day, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(day, 0o700) })
	f, err := LocateJobDirectory(common, id)
	if err != nil || !f.Found || f.Path != filepath.Join(day, id) {
		t.Fatalf("unreachable: %+v %v", f, err)
	}
	if state, err := InspectJobDirectory(f.Path); state != JobDirectoryAbsent || errorcodes.Of(err) != "run_output_records_unreadable" || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("inspect: %v %v", state, err)
	}
}

// TestInspectJobDirectorySummaryNotARegularFile: a FIFO, a link, a link to
// no file, and a file larger than a summary may be, at the folder's
// summary.json, are each run_output_records_unreadable, as a summary that
// cannot be read is: never waited on or read through.
func TestInspectJobDirectorySummaryNotARegularFile(t *testing.T) {
	for _, kind := range oddSummaryKinds {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "261016-015356-00")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			oddSummary(t, dir, kind)
			var state JobDirectoryState
			var err error
			within(t, "InspectJobDirectory", func() { state, err = InspectJobDirectory(dir) })
			if state != JobDirectoryAbsent || errorcodes.Of(err) != "run_output_records_unreadable" {
				t.Fatalf("inspect: %v %v", state, err)
			}
		})
	}
}

// folderFollow is a job folder FollowJobDirectory reads, under a
// configuration whose scoreboards are the test's own, and sets after them:
// the folder, its commands.jsonl path, and the scoreboards folder.
type folderFollow struct {
	common   CommonOptions
	folder   JobFolder
	id       string
	commands string
	boards   string
}

func newFolderFollow(t *testing.T, sets ...string) folderFollow {
	t.Helper()
	dir := t.TempDir()
	boards := filepath.Join(dir, "scoreboards")
	if err := os.Mkdir(boards, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{HomeDir: dir, SkipAuto: true, Environment: []string{}, Sets: append([]string{`basedir="` + dir + `"`, `sharedroot="none"`, `scoreboards="` + boards + `"`, `timezone="UTC"`}, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	id := "261016-015356-00"
	job := filepath.Join(dir, "jobs", "261016", id)
	if err := os.MkdirAll(job, 0o700); err != nil {
		t.Fatal(err)
	}
	return folderFollow{
		common:   CommonOptions{Config: cfg, Operator: credentials.Operator{Home: dir}},
		folder:   JobFolder{Path: job, Found: true, Group: -1},
		id:       id,
		commands: filepath.Join(job, "commands.jsonl"),
		boards:   boards,
	}
}

// summary writes the folder's summary.json, naming its commands.jsonl,
// final under status (exit 0) or, with status empty, not yet final:
// written aside and renamed into place, as the store writes it.
func (ff folderFollow) summary(t *testing.T, status string) {
	t.Helper()
	ff.final(t, status, 0)
}

// final is summary with the job's exit.
func (ff folderFollow) final(t *testing.T, status string, exit int) {
	t.Helper()
	final := ""
	if status != "" {
		final = `,"final_status":"` + status + `","primary_exit_code":` + strconv.Itoa(exit)
	}
	body := `{"schema_version":5,"job_id":"` + ff.id + `","kind":"run","devices":[{"name":"router1"}],"device_counts":{"total":1},"paths":{"commands_jsonl":"` + ff.commands + `"}` + final + `}`
	tmp := filepath.Join(ff.folder.Path, ".summary.json.tmp")
	if err := os.WriteFile(tmp, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(ff.folder.Path, "summary.json")); err != nil {
		t.Fatal(err)
	}
}

// follow runs FollowJobDirectory over the folder, rendering jsonl to out.
func (ff folderFollow) follow(ctx context.Context, out io.Writer) (records.Summary, error) {
	return FollowJobDirectory(ctx, ff.common, ff.folder, ff.id, FollowRenderOptions{Format: "jsonl"}, out, io.Discard)
}

// TestFollowJobFolderRefusesAFIFOAtCommandsJSONL: a FIFO at a job folder's
// commands.jsonl, beside a summary naming the file, is
// run_output_records_unreadable for the folder follow, at once: the open
// never waits on the FIFO for a writer.
func TestFollowJobFolderRefusesAFIFOAtCommandsJSONL(t *testing.T) {
	ff := newFolderFollow(t)
	ff.summary(t, "complete")
	if err := syscall.Mkfifo(ff.commands, 0o640); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseFIFO(ff.commands) })
	var out bytes.Buffer
	var err error
	within(t, "the folder follow", func() { _, err = ff.follow(context.Background(), &out) })
	if errorcodes.Of(err) != "run_output_records_unreadable" || !strings.Contains(err.Error(), ff.commands+" is not a regular file") || out.Len() != 0 {
		t.Fatalf("follow: %v; rendered %q", err, out.String())
	}
}

// lockedBuffer is a bytes.Buffer one goroutine writes while another reads.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestFollowJobFolderReadsRecordsWrittenAfterItsOpen: the folder follow of
// a running job renders the record it finds when it opens commands.jsonl,
// then one the job appends after the open, read from the same descriptor
// at the file's new end, and ends once the summary is final.
func TestFollowJobFolderReadsRecordsWrittenAfterItsOpen(t *testing.T) {
	ff := newFolderFollow(t)
	first := `{"sequence":1,"record_id":"r1","device":{"canonical_name":"router1"}}` + "\n"
	second := `{"sequence":2,"record_id":"r2","device":{"canonical_name":"router1"}}` + "\n"
	if err := os.WriteFile(ff.commands, []byte(first), 0o640); err != nil {
		t.Fatal(err)
	}
	ff.summary(t, "")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	board := `{"schema_version":5,"activity_id":"` + ff.id + `","job_id":"` + ff.id + `","status":"running","started_at":"` + now + `","last_updated_at":"` + now + `"}`
	if err := os.WriteFile(filepath.Join(ff.boards, ff.id+".json"), []byte(board), 0o640); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		summary records.Summary
		err     error
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out := &lockedBuffer{}
	done := make(chan outcome, 1)
	go func() {
		s, err := ff.follow(ctx, out)
		done <- outcome{s, err}
	}()
	// The first record rendered: the file is open.
	for !strings.HasPrefix(out.String(), first) {
		select {
		case got := <-done:
			t.Fatalf("the follow ended before the first record: %v; rendered %q", got.err, out.String())
		case <-ctx.Done():
			t.Fatalf("the first record was not rendered; rendered %q", out.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	f, err := os.OpenFile(ff.commands, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(second); err != nil {
		t.Fatal(err)
	}
	f.Close()
	ff.summary(t, "complete")
	var got outcome
	select {
	case got = <-done:
	case <-ctx.Done():
		t.Fatalf("the follow did not end; rendered %q", out.String())
	}
	if got.err != nil || got.summary.FinalStatus != "complete" || !strings.HasPrefix(out.String(), first+second) {
		t.Fatalf("follow: %v, status %q; rendered %q", got.err, got.summary.FinalStatus, out.String())
	}
}
