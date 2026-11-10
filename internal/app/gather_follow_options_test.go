package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// Whether a job kept its commands.jsonl, on the daemon's follow (DECISIONS
// point 4, the first and third "Settled at S6" bullets, gather-2026-11):
// judged by the job's summary.json in the folder the follow's start
// names, the folder follow's rule, once, at the first start, the one
// judgement deciding job follow --gather's refusal and the gathering. No
// folder is a job that kept none (output.persist-command was false), a
// summary naming no commands_jsonl one that kept none
// (output.files.commands-jsonl was false); a summary naming it, kept, a
// file gone since the read-back's run_output_records_unreadable at the
// end. The refusal, gather_option_conflict, ends the follow at once (never
// retried into follow_stream_lost), nothing said or shown; it answers the
// option as given (FollowRenderOptions.Gather), never the key. The summary
// is read only where the display gathers, never under --format jsonl or
// json.

// followGather is a follow by job ID through the daemon at socket under
// the option as given (gather) and sets, onto stderr; it returns standard
// output, the folder the follow learned, and the error.
func followGather(t *testing.T, socket, jobID string, gather bool, stderr *bytes.Buffer, sets ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append([]string{`basedir="` + home + `"`, `sharedroot="none"`, `timezone="UTC"`}, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	rt := DaemonRuntime{Config: cfg, Socket: socket, MaxFrame: 1 << 24}
	var stdout bytes.Buffer
	_, learned, err := FollowJobRendering(ctx, CommonOptions{Config: cfg, Operator: credentials.Operator{Home: home}}, rt, jobID, FollowRenderOptions{Format: "text", Gather: gather}, &stdout, stderr)
	return stdout.String(), learned, err
}

// notKeptMessage is job follow --gather's refusal of job id, which kept no
// commands.jsonl, key being false when it ran.
func notKeptMessage(id, key string) string {
	return "gather_option_conflict: --gather needs the job's commands.jsonl; job " + id + " kept none (" + key + " was false)"
}

// TestFollowGatherRefusedAtTheStart: through the daemon, --gather of a job
// without a folder (output.persist-command false) or whose summary names
// no commands.jsonl (output.files.commands-jsonl false) is refused at the
// start, whatever the key (false here, and true): the one message, the
// job and the key its only parts, exit 4; nothing written to either stream
// (not the start's admission warning, not its not-kept line, records 1 and
// 2 before the follow); one follow, not retried. Without the option, the
// key true keeps today's display: the warning and the not-kept line said,
// the follow ending with the job.
func TestFollowGatherRefusedAtTheStart(t *testing.T) {
	jobID := "261016-015356-00"
	none := t.TempDir()
	summaryNamingNone(t, none)
	notKept := []followConn{{3, 2, 2}}
	for _, tc := range []struct {
		dir, key string
	}{
		{"", "output.persist-command"},
		{none, "output.files.commands-jsonl"},
	} {
		for _, key := range []string{"display.run.gather=false", "display.run.gather=true"} {
			served := new(atomic.Int32)
			socket := scriptedDaemon(t, jobID, tc.dir, nil, notKept, followEnd{final: "complete", warnings: []string{"an admission warning"}, served: served})
			var stderr bytes.Buffer
			stdout, _, err := followGather(t, socket, jobID, true, &stderr, "display.color=never", key)
			if errorcodes.Of(err) != "gather_option_conflict" || errorcodes.Message(err) != notKeptMessage(jobID, tc.key) || errorcodes.ExitAt(err, "follow_stream_lost") != exitcode.ExitUsageError {
				t.Errorf("dir %q, %s: %v\nwant %s", tc.dir, key, err, notKeptMessage(jobID, tc.key))
			}
			if stdout != "" || stderr.Len() != 0 || served.Load() != 1 {
				t.Errorf("dir %q, %s: stdout=%q stderr=%q follows=%d", tc.dir, key, stdout, stderr.String(), served.Load())
			}
		}
		var stderr bytes.Buffer
		_, _, err := followGather(t, scriptedDaemon(t, jobID, tc.dir, nil, notKept, followEnd{final: "complete", warnings: []string{"an admission warning"}}), jobID, false, &stderr, "display.color=never", "display.run.gather=true")
		if err != nil || !strings.Contains(stderr.String(), "an admission warning") || !strings.Contains(stderr.String(), "records 1 to 2 were before this follow and are not kept") {
			t.Errorf("dir %q, the key alone: %v stderr=%q", tc.dir, err, stderr.String())
		}
	}
}

// TestFollowGatherJudgedBySummary: the job's records are judged by its
// summary.json, never by the file's name, under --gather and under the key
// alone (here true): no folder, or a summary naming none, kept none
// (--gather refused, the key alone today's display, though a file lies at
// the name); a summary naming it, kept, the follow gathering: the lab
// layout from a regular file, and from a link at the name or a file gone
// since, the read-back's run_output_records_unreadable at the end (the
// job ended: EndedError), nothing shown, never a refusal or today's
// display; no summary.json, the summary read's run_output_records_unreadable
// at the start (not EndedError), naming summary.json, nothing shown.
func TestFollowGatherJudgedBySummary(t *testing.T) {
	jobID, lines, want := labJob(t, filepath.Join(t.TempDir(), "commands.jsonl"))
	folder := func(summary func(dir string), file func(path string)) string {
		dir := t.TempDir()
		if summary != nil {
			summary(dir)
		}
		if file != nil {
			file(filepath.Join(dir, "commands.jsonl"))
		}
		return dir
	}
	naming := func(dir string) { summaryOfDevices(t, dir, 8, "") }
	none := func(dir string) { summaryNamingNone(t, dir) }
	lab := func(path string) { labJob(t, path) }
	link := func(path string) {
		if err := os.Symlink("nowhere", path); err != nil {
			t.Fatal(err)
		}
	}
	const (
		refused  = "refused"
		gathered = "gathered"
		today    = "today's"
		readBack = "read-back"
		summary  = "summary"
	)
	for _, tc := range []struct {
		name          string
		dir           string
		gather, alone string
	}{
		{"no folder", "", refused, today},
		{"a summary naming none, a file at the name", folder(none, lab), refused, today},
		{"a summary naming it, the file", folder(naming, lab), gathered, gathered},
		{"a summary naming it, a link at the name", folder(naming, link), readBack, readBack},
		{"a summary naming it, the file gone", folder(naming, nil), readBack, readBack},
		{"no summary, the file", folder(nil, lab), summary, summary},
	} {
		for _, option := range []bool{true, false} {
			wantOutcome := tc.gather
			if !option {
				wantOutcome = tc.alone
			}
			var stderr bytes.Buffer
			stdout, _, err := followGather(t, followDaemon(t, jobID, tc.dir, lines, nil), jobID, option, &stderr, gatherSets...)
			var ended *EndedError
			var ok bool
			switch wantOutcome {
			case refused:
				key := "output.persist-command"
				if tc.dir != "" {
					key = "output.files.commands-jsonl"
				}
				ok = errorcodes.Message(err) == notKeptMessage(jobID, key) && stdout == ""
			case gathered:
				ok = err == nil && stdout == want
			case today:
				ok = err == nil && stdout != want && strings.Contains(stdout, "! router01 [127.0.0.1]")
			case readBack:
				ok = errorcodes.Of(err) == "run_output_records_unreadable" && errors.As(err, &ended) && strings.Contains(err.Error(), filepath.Join(tc.dir, "commands.jsonl")) && stdout == ""
			case summary:
				ok = errorcodes.Of(err) == "run_output_records_unreadable" && !errors.As(err, &ended) && strings.Contains(err.Error(), filepath.Join(tc.dir, "summary.json")) && stdout == ""
			}
			if !ok {
				t.Errorf("%s, --gather %v: want %s; %v\nstdout: %.80q", tc.name, option, wantOutcome, err, stdout)
			}
		}
	}
}

// TestFollowGatherUnsearchableFolder: a folder the follower cannot search
// (mode 0), or a regular file where the start names a folder, gives the
// summary read's run_output_records_unreadable at the start, naming
// summary.json, under --gather with the key false or true, never "kept
// none (... was false)", which the follower does not know; nothing shown,
// and the folder learned, so job follow's failure line names it.
func TestFollowGatherUnsearchableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root searches any folder")
	}
	jobID, lines, _ := labJob(t, filepath.Join(t.TempDir(), "commands.jsonl"))
	shut := t.TempDir()
	labJob(t, filepath.Join(shut, "commands.jsonl"))
	summaryOfDevices(t, shut, 8, "")
	if err := os.Chmod(shut, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{shut, notDir} {
		for _, key := range []string{"display.run.gather=false", "display.run.gather=true"} {
			var stderr bytes.Buffer
			stdout, learned, err := followGather(t, followDaemon(t, jobID, dir, lines, nil), jobID, true, &stderr, "display.color=never", key)
			var ended *EndedError
			if errorcodes.Of(err) != "run_output_records_unreadable" || errors.As(err, &ended) || !strings.Contains(err.Error(), filepath.Join(dir, "summary.json")) || stdout != "" || learned != dir {
				t.Errorf("%s, %s: %v; stdout %.80q; learned %q", dir, key, err, stdout, learned)
			}
		}
	}
}

// atFirstWrite is a follower's standard error that runs do at its first
// write (the start's admission warning, the follower's first line): after
// the start was judged under --gather and before gathering began.
type atFirstWrite struct {
	do   func()
	done bool
	bytes.Buffer
}

func (w *atFirstWrite) Write(p []byte) (int, error) {
	if !w.done {
		w.done = true
		w.do()
	}
	return w.Buffer.Write(p)
}

// writeSummaryNamingNone is a do for atFirstWrite or a scriptedDaemon's
// beforeStart: dir's summary.json replaced by one naming no
// commands.jsonl.
func writeSummaryNamingNone(t *testing.T, dir string) func() {
	return func() { summaryNamingNone(t, dir) }
}

// followGatherOnto is followGather onto a given standard error writer.
func followGatherOnto(t *testing.T, socket, jobID string, gather bool, stderr io.Writer, sets ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append([]string{`basedir="` + home + `"`, `sharedroot="none"`, `timezone="UTC"`}, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	_, _, err = FollowJobRendering(ctx, CommonOptions{Config: cfg, Operator: credentials.Operator{Home: home}}, DaemonRuntime{Config: cfg, Socket: socket, MaxFrame: 1 << 24}, jobID, FollowRenderOptions{Format: "text", Gather: gather}, &stdout, stderr)
	return stdout.String(), err
}

// TestFollowJudgedOnce: the start is judged once, the judgement deciding
// the refusal and the gathering: commands.jsonl removed just after the
// start (at the admission warning, the follower's first line) leaves the
// follow gathering, under --gather and under the key alone, and the
// read-back at the end fails, run_output_records_unreadable naming the
// file (the job ended: EndedError), nothing shown; never today's display.
func TestFollowJudgedOnce(t *testing.T) {
	for _, option := range []bool{true, false} {
		dir := t.TempDir()
		path := filepath.Join(dir, "commands.jsonl")
		jobID, lines, _ := labJob(t, path)
		summaryOfDevices(t, dir, 8, "")
		socket := scriptedDaemon(t, jobID, dir, lines, nil, followEnd{exit: 0, final: "complete", warnings: []string{"an admission warning"}})
		stderr := &atFirstWrite{do: func() { _ = os.Remove(path) }}
		stdout, err := followGatherOnto(t, socket, jobID, option, stderr, gatherSets...)
		var ended *EndedError
		if errorcodes.Of(err) != "run_output_records_unreadable" || !errors.As(err, &ended) || !strings.Contains(err.Error(), path) || stdout != "" || !stderr.done {
			t.Errorf("--gather %v: %v; stderr %q\nstdout: %.80q", option, err, stderr.String(), stdout)
		}
	}
}

// TestFollowJudgedOnceBySummary: under --gather the job's records are
// judged once, at the first start, through its summary.json, and that
// judgement decides the gathering too (gatherInto never judges again):
// the summary removed just after the start (at the admission warning), or
// replaced there by one naming no commands.jsonl, leaves the follow
// gathering by the first judgement, the lab job's gathered layout, no
// error (DECISIONS point 4, the third "Settled at S6").
func TestFollowJudgedOnceBySummary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, dir string) func()
	}{
		{"removed", func(t *testing.T, dir string) func() {
			return func() { _ = os.Remove(filepath.Join(dir, "summary.json")) }
		}},
		{"replaced by one naming none", writeSummaryNamingNone},
	} {
		dir := t.TempDir()
		jobID, lines, want := labJob(t, filepath.Join(dir, "commands.jsonl"))
		summaryOfDevices(t, dir, 8, "")
		socket := scriptedDaemon(t, jobID, dir, lines, nil, followEnd{exit: 0, final: "complete", warnings: []string{"an admission warning"}})
		stderr := &atFirstWrite{do: tc.change(t, dir)}
		stdout, err := followGatherOnto(t, socket, jobID, true, stderr, gatherSets...)
		if err != nil || stdout != want || !stderr.done {
			t.Errorf("the summary %s at the warning: %v; stderr %q\nstdout:\n%s\nwant:\n%s", tc.name, err, stderr.String(), stdout, want)
		}
	}
}

// TestFollowResumeNeverRefused: job follow --gather is judged at the
// follow's first start alone: a stream lost after two records, whose
// commands.jsonl is gone when the resume's request comes, resumes (not
// refused "kept none"), and the read-back at the end fails,
// run_output_records_unreadable (the job ended: EndedError); two follows.
// A summary replaced at the resume by one naming no commands.jsonl is
// not read again: the follow gathers by the first judgement, the lab
// job's gathered layout.
func TestFollowResumeNeverRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.jsonl")
	jobID, lines, _ := labJob(t, path)
	summaryOfDevices(t, dir, 8, "")
	served := new(atomic.Int32)
	gone := func(conn int) {
		if conn == 1 {
			_ = os.Remove(path)
		}
	}
	socket := scriptedDaemon(t, jobID, dir, lines, []followConn{{1, 0, 2}, {3, 2, len(lines)}}, followEnd{exit: 0, final: "complete", beforeStart: gone, served: served})
	var stderr bytes.Buffer
	stdout, _, err := followGather(t, socket, jobID, true, &stderr, gatherSets...)
	var ended *EndedError
	if errorcodes.Of(err) != "run_output_records_unreadable" || !errors.As(err, &ended) || stdout != "" || served.Load() != 2 {
		t.Fatalf("resumed with the file gone: %v; follows %d; stdout %.80q stderr %q", err, served.Load(), stdout, stderr.String())
	}

	dir = t.TempDir()
	jobID, lines, want := labJob(t, filepath.Join(dir, "commands.jsonl"))
	summaryOfDevices(t, dir, 8, "")
	served = new(atomic.Int32)
	replaced := func(conn int) {
		if conn == 1 {
			summaryNamingNone(t, dir)
		}
	}
	socket = scriptedDaemon(t, jobID, dir, lines, []followConn{{1, 0, 2}, {3, 2, len(lines)}}, followEnd{exit: 0, final: "complete", beforeStart: replaced, served: served})
	stderr.Reset()
	if stdout, _, err := followGather(t, socket, jobID, true, &stderr, gatherSets...); err != nil || stdout != want || served.Load() != 2 {
		t.Fatalf("resumed with the summary naming none: %v; follows %d; stderr %q\nstdout:\n%s\nwant:\n%s", err, served.Load(), stderr.String(), stdout, want)
	}
}

// TestFollowGatherNamedPathNeverRead: the judgement asks whether the
// summary names a commands_jsonl, never the path it names; the read-back
// opens ARTIFACTDIR/commands.jsonl (a folder reached by another path than
// the job wrote: a moved or mounted tree). A summary naming a path
// elsewhere: the follow under --gather gathers the folder's file, the lab
// job's gathered layout.
func TestFollowGatherNamedPathNeverRead(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, want := labJob(t, filepath.Join(dir, "commands.jsonl"))
	body := `{"schema_version":5,"job_id":"x","kind":"run","devices":[{"name":"router01"}],"device_counts":{"total":1},"paths":{"commands_jsonl":"/nonexistent/elsewhere/commands.jsonl"}}`
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if stdout, _, err := followGather(t, followDaemon(t, jobID, dir, lines, nil), jobID, true, &stderr, gatherSets...); stdout != want {
		t.Fatalf("%v\nstderr %q\nstdout:\n%s\nwant:\n%s", err, stderr.String(), stdout, want)
	}
}

// TestFollowRecordsNeverReadTheSummary: under --format jsonl or json the
// display never gathers, so the follow never reads the job's summary.json,
// the key true or not: the records pass through though the folder has
// none, standard error not a terminal (TestFollowRecordsAtATerminal-
// NeverReadTheSummary has it a terminal), and a site's key never breaks a
// script.
func TestFollowRecordsNeverReadTheSummary(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, _ := labJob(t, filepath.Join(dir, "commands.jsonl"))
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append([]string{`basedir="` + home + `"`, `sharedroot="none"`, `timezone="UTC"`}, gatherSets...)})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"jsonl", "json"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var stdout, stderr bytes.Buffer
		rt := DaemonRuntime{Config: cfg, Socket: followDaemon(t, jobID, dir, lines, nil), MaxFrame: 1 << 24}
		_, _, err := FollowJobRendering(ctx, CommonOptions{Config: cfg, Operator: credentials.Operator{Home: home}}, rt, jobID, FollowRenderOptions{Format: format}, &stdout, &stderr)
		cancel()
		if err != nil || strings.Count(stdout.String(), jobID) < len(lines) {
			t.Errorf("--format %s: %v; %d bytes shown; stderr %q", format, err, stdout.Len(), stderr.String())
		}
	}
}
