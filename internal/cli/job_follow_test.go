package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/records"
)

// job follow over the daemon path. The fixture answers a start and a
// terminal with no record between
// them; the live test below renders real records.

func followInvocation(t *testing.T, g globalOptions, args ...string) *Invocation {
	t.Helper()
	inv, err := Parse(append([]string{"job", "follow"}, args...))
	if err != nil {
		t.Fatal(err)
	}
	inv.Global = g
	return inv
}

func TestJobFollowRequiresOneJobID(t *testing.T) {
	for _, args := range [][]string{{"job", "follow"}, {"job", "follow", "a", "b"}, {"job", "follow", "--format"}} {
		if _, err := Parse(args); err == nil {
			t.Errorf("%q parsed", args)
		}
	}
	if _, msg := parseCode(t, "job", "follow"); !strings.Contains(msg, "cli_positional_missing") {
		t.Fatalf("message %q", msg)
	}
	if _, err := Parse([]string{"j", "f", fixtureJobID, "--format", "jsonl", "--echo", "--noborder"}); err != nil {
		t.Fatalf("abbreviated follow: %v", err)
	}
}

// TestJobFollowPrintsTheResultLineAndExitsWithTheJob: a cancelled terminal
// gives the cancelled line, the run line, and exit 113; a completed one the
// no result line, and exits with the job's exit.
func TestJobFollowPrintsTheCancelledLineAndExitsWithTheJob(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	f := startStopFixture(t, socket, 0, nil)
	var stdout, stderr bytes.Buffer
	got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr})
	if got != exitcode.ExitCancelled {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	want := "job " + fixtureJobID + " cancelled: fixture reason; artifacts /tmp/jobs/" + fixtureJobID + "\n"
	if stderr.String() != want {
		t.Fatalf("stderr=%q want %q", stderr.String(), want)
	}
	// The display of a job with no record is its footer alone (28.4).
	if !strings.HasPrefix(stdout.String(), "! exit=") || strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("stdout=%q for a job with no record", stdout.String())
	}
	f.terminal = &ipc.ActivityOutcome{ExitCode: 0, ExitName: "ExitSuccess", ActivityID: fixtureJobID, JobID: fixtureJobID, ArtifactDir: "/tmp/jobs/" + fixtureJobID, Summary: records.Summary{FinalStatus: "completed"}}
	stdout.Reset()
	stderr.Reset()
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID, "--format", "jsonl"), app.IO{Stdout: &stdout, Stderr: &stderr}); got != 0 {
		t.Fatalf("completed: exit=%d stderr=%q", got, stderr.String())
	}
	// jsonl: the summary line on stdout, nothing on stderr (28.4).
	if stderr.String() != "" || !strings.Contains(stdout.String(), `"final_status":"completed"`) {
		t.Fatalf("completed stderr=%q stdout=%q", stderr.String(), stdout.String())
	}
	f.terminal = &ipc.ActivityOutcome{ExitCode: exitcode.ExitShutdownIncomplete, ExitName: "ExitShutdownIncomplete", ActivityID: fixtureJobID, JobID: fixtureJobID, ArtifactDir: "/tmp/jobs/" + fixtureJobID, Summary: records.Summary{FinalStatus: "incomplete"}}
	stdout.Reset()
	stderr.Reset()
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitShutdownIncomplete || stderr.String() != "" || !strings.HasPrefix(stdout.String(), "! exit=106 ") {
		t.Fatalf("incomplete: exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	if ops := f.operations(); ops != "follow,follow,follow" {
		t.Fatalf("operations %q", ops)
	}
}

// TestJobFollowQuietAndJSON: --quiet suppresses the lines; --format json is
// accepted by the renderer.
func TestJobFollowQuietAndJSON(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	startStopFixture(t, socket, 0, nil)
	g.quiet = true
	var stdout, stderr bytes.Buffer
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID, "--format", "json"), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitCancelled || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
}

// TestJobFollowUnknownJobAndNoDaemon: job_unknown from the daemon at 112
// with no "continues" line; no daemon is daemon_unreachable (section B
// turns both toward the job directory).
func TestJobFollowUnknownJobAndNoDaemon(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	var stdout, stderr bytes.Buffer
	// No daemon and no directory: job_unknown
	// naming the derived path and the daemon reason, no "continues" line.
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitJobRejected || !strings.HasPrefix(stderr.String(), "job_unknown: ") || !strings.Contains(stderr.String(), "daemon_unreachable") || strings.Contains(stderr.String(), "continues") {
		t.Fatalf("no daemon: exit=%d stderr=%q", got, stderr.String())
	}
	startStopFixture(t, socket, 0, nil)
	stderr.Reset()
	unknown := strings.TrimSuffix(fixtureJobID, "00") + unknownJobSuffix
	if got := jobFollow(context.Background(), followInvocation(t, g, unknown), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitJobRejected || !strings.HasPrefix(stderr.String(), "job_unknown: ") || strings.Contains(stderr.String(), "continues") {
		t.Fatalf("unknown: exit=%d stderr=%q", got, stderr.String())
	}
}

// TestJobFollowRendersTheDetachedJob is the proof over a real
// daemon and the fake device: a detached job followed under jsonl prints
// commands.jsonl byte for byte, the run line, and the job's exit; text
// renders the same records through the record renderer.
func TestJobFollowRendersTheDetachedJob(t *testing.T) {
	base, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	var stdout, stderr bytes.Buffer
	if got := Main(liveArgs(sets, "--detach"), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("detach exit=%d stderr=%q", got, stderr.String())
	}
	jobID := strings.TrimPrefix(strings.Split(stdout.String(), "\n")[0], "job_id: ")
	waitSummary(t, base, jobID)
	m, _ := filepath.Glob(filepath.Join(base, "jobs", "*", jobID, "commands.jsonl"))
	if len(m) != 1 {
		t.Fatalf("commands.jsonl: %v", m)
	}
	file, err := os.ReadFile(m[0])
	if err != nil || len(file) == 0 {
		t.Fatalf("read %s: %v (%d bytes)", m[0], err, len(file))
	}
	stdout.Reset()
	stderr.Reset()
	got := Main(append(append([]string{}, sets...), "job", "follow", jobID, "--format", "jsonl"), strings.NewReader(""), &stdout, &stderr)
	// The fake device fails every session, so the job's exit is 101.
	if got != exitcode.ExitPartialFailure {
		t.Fatalf("follow exit=%d stderr=%q", got, stderr.String())
	}
	recordsThenSummary(t, stdout.Bytes(), file, jobID, "errored")
	if stderr.String() != "" {
		t.Fatalf("stderr=%q want nothing", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if got := Main(append(append([]string{}, sets...), "job", "follow", jobID), strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitPartialFailure || !strings.Contains(stdout.String(), "127.0.0.1") {
		t.Fatalf("text exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	t.Logf("followed %s: %d bytes of jsonl equal to the file; text rendered", jobID, len(file))
}

// TestJobFollowInterruptedBeforeTheStart: a cancelled context is the
// interrupted shape with exit 113 and no artifact path yet.
func TestJobFollowInterruptedBeforeTheStart(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	startStopFixture(t, socket, 0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	if got := jobFollow(ctx, followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitCancelled {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	if want := "interrupted: job " + fixtureJobID + " continues in the daemon; run \"karvi job follow " + fixtureJobID + "\" again or read its summary.json when it ends\n"; stderr.String() != want {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

// finishedDirectory writes a finished job's directory where JobDirectoryFor
// derives it: an empty canonical file and a summary naming it, as a job's
// does.
func finishedDirectory(t *testing.T, g globalOptions, jobID string, summary records.Summary) string {
	t.Helper()
	common, err := testCommon(g)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := app.JobDirectoryFor(common, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "commands.jsonl"), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if summary.Paths == nil {
		summary.Paths = map[string]string{"commands_jsonl": filepath.Join(dir, "commands.jsonl")}
	}
	raw, _ := json.Marshal(summary)
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), raw, 0o640); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestJobFollowReadsTheDirectoryWithoutADaemon: with no daemon the
// finished job's directory gives the result line and the
// summary's recorded exit; a cancellation block gives the cancelled line.
func TestJobFollowReadsTheDirectoryWithoutADaemon(t *testing.T) {
	_, g, _ := daemonTestRuntime(t)
	dir := finishedDirectory(t, g, fixtureJobID, records.Summary{FinalStatus: "completed", ExitCode: 0, ExitName: "ExitSuccess"})
	var stdout, stderr bytes.Buffer
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID, "--format", "jsonl"), app.IO{Stdout: &stdout, Stderr: &stderr}); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	// The directory's records (none in the fixture) then its summary as
	// the stream's last line; nothing on stderr (28.4).
	recordsThenSummary(t, stdout.Bytes(), nil, "", "completed")
	if stderr.String() != "" {
		t.Fatalf("stderr=%q stdout=%q", stderr.String(), stdout.String())
	}
	_ = dir
	finishedDirectory(t, g, fixtureJobID, records.Summary{FinalStatus: "cancelled", ExitCode: exitcode.ExitCancelled, ExitName: "ExitCancelled", Cancellation: &records.Cancellation{Reason: "wrong window"}})
	stderr.Reset()
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitCancelled {
		t.Fatalf("cancelled: exit=%d stderr=%q", got, stderr.String())
	}
	if want := "job " + fixtureJobID + " cancelled: wrong window; artifacts " + dir + "\n"; stderr.String() != want {
		t.Fatalf("cancelled stderr=%q", stderr.String())
	}
}

// TestJobFollowDirectoryStates: a directory without a summary, or whose
// summary names no final status, is job_orphaned when no scoreboard names
// the job or its scoreboard is stale (EXAMPLES chapter 61, issue 4); no
// directory is job_unknown naming the derived path
// (3.4); a malformed ID is job_request_malformed (3.1); a daemon that
// answers job_unknown yields to the directory (3.2).
func TestJobFollowDirectoryStates(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
	boards := t.TempDir()
	g.sets = append(g.sets, "scoreboards=\""+boards+"\"", "watch.refresh=\"250ms\"", "watch.stale-after=\"1s\"")
	var stdout, stderr bytes.Buffer
	if got := jobFollow(context.Background(), followInvocation(t, g, "not-a-job-id"), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitJobRejected || !strings.HasPrefix(stderr.String(), "job_request_malformed: ") {
		t.Fatalf("malformed: exit=%d stderr=%q", got, stderr.String())
	}
	stderr.Reset()
	common, _ := testCommon(g)
	dir, _ := app.JobDirectoryFor(common, fixtureJobID)
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitJobRejected || !strings.HasPrefix(stderr.String(), "job_unknown: ") || !strings.Contains(stderr.String(), dir+" does not exist") || !strings.Contains(stderr.String(), "daemon_unreachable") {
		t.Fatalf("absent: exit=%d stderr=%q", got, stderr.String())
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitJobRejected || stderr.String() != "job_orphaned: job "+fixtureJobID+" has no final summary and no scoreboard\n" {
		t.Fatalf("orphaned: exit=%d stderr=%q", got, stderr.String())
	}
	// The summary the acceptance wrote, without a final status, and a
	// scoreboard last written 90 seconds ago.
	finishedDirectory(t, g, fixtureJobID, records.Summary{AcceptedSummary: records.AcceptedSummary{SchemaVersion: records.JobSchemaVersion, Devices: []records.SummaryDevice{{Name: "r1"}}}})
	writeBoard(t, boards, fixtureJobID, "running", time.Now().Add(-90*time.Second))
	stderr.Reset()
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitJobRejected || !strings.HasPrefix(stderr.String(), "job_orphaned: job "+fixtureJobID+" has no final summary and a stale scoreboard\n  last update 1m30") || !strings.HasSuffix(stderr.String(), "s ago\n  stale after 1s\n") || stdout.String() != "" {
		t.Fatalf("not final: exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	// A scoreboard that names the job's end, the summary never made final.
	writeBoard(t, boards, fixtureJobID, "errored", time.Now().Add(-90*time.Second))
	stderr.Reset()
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitJobRejected || !strings.HasPrefix(stderr.String(), "job_orphaned: job "+fixtureJobID+" has no final summary and its scoreboard ended errored\n  last update 1m30") || !strings.HasSuffix(stderr.String(), "s ago\n  stale after 1s\n") || stdout.String() != "" {
		t.Fatalf("ended: exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	// The daemon does not hold the job (its fixture answers job_unknown for
	// the zzzz suffix); the finished directory answers instead.
	unknown := strings.TrimSuffix(fixtureJobID, "00") + unknownJobSuffix
	f := startStopFixture(t, socket, 0, nil)
	udir := finishedDirectory(t, g, unknown, records.Summary{FinalStatus: "completed", ExitCode: 0, ExitName: "ExitSuccess"})
	stderr.Reset()
	// Text mode: the directory's display ends with the footer naming its
	// folder, and nothing on stderr (28.4).
	stdout.Reset()
	if got := jobFollow(context.Background(), followInvocation(t, g, unknown), app.IO{Stdout: &stdout, Stderr: &stderr}); got != 0 || stderr.String() != "" || !strings.Contains(stdout.String(), " artifacts="+udir+"\n") {
		t.Fatalf("daemon then directory: exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	if ops := f.operations(); ops != "follow" {
		t.Fatalf("operations %q", ops)
	}
}

// TestJobFollowFollowsAnUnfinishedFolder: a job no daemon of the caller's
// holds, its summary not final and its scoreboard fresh, is followed from
// its folder (EXAMPLES chapter 61, issue 4): each record as it is written, a
// line in two writes rendered once whole, then the summary line once the
// summary names a final status, with the job's exit; a scoreboard that goes
// stale in the meantime ends the follow job_orphaned.
func TestJobFollowFollowsAnUnfinishedFolder(t *testing.T) {
	_, g, _ := daemonTestRuntime(t)
	boards := t.TempDir()
	g.sets = append(g.sets, "scoreboards=\""+boards+"\"", "watch.refresh=\"250ms\"", "watch.stale-after=\"1s\"")
	accepted := records.Summary{AcceptedSummary: records.AcceptedSummary{SchemaVersion: records.JobSchemaVersion, JobID: fixtureJobID, Devices: []records.SummaryDevice{{Name: "r1"}}, Paths: map[string]string{"commands_jsonl": "commands.jsonl"}}}
	dir := finishedDirectory(t, g, fixtureJobID, accepted)
	writeBoard(t, boards, fixtureJobID, "running", time.Now())
	type result struct {
		exit           int
		stdout, stderr string
	}
	follow := func() <-chan result {
		done := make(chan result, 1)
		go func() {
			var stdout, stderr bytes.Buffer
			exit := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID, "--format", "jsonl"), app.IO{Stdout: &stdout, Stderr: &stderr})
			done <- result{exit, stdout.String(), stderr.String()}
		}()
		return done
	}
	appendTo := func(text string) {
		f, err := os.OpenFile(filepath.Join(dir, "commands.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(text); err != nil {
			t.Fatal(err)
		}
	}
	done := follow()
	first, second := `{"sequence":1}`+"\n", `{"sequence":2}`+"\n"
	appendTo(first)
	time.Sleep(300 * time.Millisecond)
	appendTo(second[:6])
	time.Sleep(300 * time.Millisecond)
	appendTo(second[6:])
	// The job's end: the scoreboard's final status, then the summary's.
	writeBoard(t, boards, fixtureJobID, "errored", time.Now())
	time.Sleep(300 * time.Millisecond)
	final := accepted
	final.FinalStatus, final.ExitCode, final.ExitName = "errored", exitcode.ExitPartialFailure, "ExitPartialFailure"
	raw, _ := json.Marshal(final)
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), raw, 0o640); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.exit != exitcode.ExitPartialFailure || r.stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", r.exit, r.stderr, r.stdout)
	}
	recordsThenSummary(t, []byte(r.stdout), []byte(first+second), fixtureJobID, "errored")

	// The same job's records, its summary not final again, and a scoreboard
	// that stops being written: job_orphaned once it is stale.
	raw, _ = json.Marshal(accepted)
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), raw, 0o640); err != nil {
		t.Fatal(err)
	}
	writeBoard(t, boards, fixtureJobID, "running", time.Now())
	r = <-follow()
	if r.exit != exitcode.ExitJobRejected || !strings.HasPrefix(r.stderr, "job_orphaned: job "+fixtureJobID+" has no final summary and a stale scoreboard\n  last update ") || r.stdout != first+second {
		t.Fatalf("stale: exit=%d stderr=%q stdout=%q", r.exit, r.stderr, r.stdout)
	}
}

// TestJobFollowWithoutCommandsJSONL: a job whose summary names no
// commands.jsonl keeps none (output.files.commands-jsonl false); its follow
// from the folder says so once and shows the job's end alone, opening no
// file, live or finished (EXAMPLES chapter 61, issue 4).
func TestJobFollowWithoutCommandsJSONL(t *testing.T) {
	_, g, _ := daemonTestRuntime(t)
	boards := t.TempDir()
	g.sets = append(g.sets, "scoreboards=\""+boards+"\"", "watch.refresh=\"250ms\"", "watch.stale-after=\"1s\"")
	accepted := records.Summary{AcceptedSummary: records.AcceptedSummary{SchemaVersion: records.JobSchemaVersion, JobID: fixtureJobID, Devices: []records.SummaryDevice{{Name: "r1"}}, Paths: map[string]string{"summary": "summary.json"}}}
	dir := finishedDirectory(t, g, fixtureJobID, accepted)
	if err := os.Remove(filepath.Join(dir, "commands.jsonl")); err != nil {
		t.Fatal(err)
	}
	writeBoard(t, boards, fixtureJobID, "running", time.Now())
	const warning = "warning: the job's records are not kept (output.files.commands-jsonl is false)\n"
	type result struct {
		exit           int
		stdout, stderr string
	}
	done := make(chan result, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		exit := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID, "--format", "jsonl"), app.IO{Stdout: &stdout, Stderr: &stderr})
		done <- result{exit, stdout.String(), stderr.String()}
	}()
	time.Sleep(400 * time.Millisecond)
	final := accepted
	final.FinalStatus, final.ExitCode, final.ExitName = "completed", 0, "ExitSuccess"
	raw, _ := json.Marshal(final)
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), raw, 0o640); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.exit != 0 || r.stderr != warning {
		t.Fatalf("live: exit=%d stderr=%q stdout=%q", r.exit, r.stderr, r.stdout)
	}
	recordsThenSummary(t, []byte(r.stdout), nil, fixtureJobID, "completed")
	// Finished: the text display is the footer alone.
	var stdout, stderr bytes.Buffer
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != 0 || stderr.String() != warning || !strings.Contains(stdout.String(), " artifacts="+dir+"\n") || strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("finished: exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
}

// writeBoard writes the scoreboard of job id in dir with status, last
// updated at.
func writeBoard(t *testing.T, dir, id, status string, at time.Time) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"status": status, "last_updated_at": at})
	if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0o640); err != nil {
		t.Fatal(err)
	}
}

// TestJobFollowAfterTheDaemonStopped is the directory read's proof over a
// real daemon: the job followed from its directory prints the same jsonl bytes
// as the daemon-served follow and the job's recorded exit.
func TestJobFollowAfterTheDaemonStopped(t *testing.T) {
	base, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	var stdout, stderr bytes.Buffer
	if got := Main(liveArgs(sets, "--detach"), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("detach exit=%d stderr=%q", got, stderr.String())
	}
	jobID := strings.TrimPrefix(strings.Split(stdout.String(), "\n")[0], "job_id: ")
	waitSummary(t, base, jobID)
	m, _ := filepath.Glob(filepath.Join(base, "jobs", "*", jobID, "commands.jsonl"))
	if len(m) != 1 {
		t.Fatalf("commands.jsonl: %v", m)
	}
	file, _ := os.ReadFile(m[0])
	stdout.Reset()
	stderr.Reset()
	if got := Main(append(append([]string{}, sets...), "daemon", "stop", "--force"), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("stop exit=%d stderr=%q", got, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	got := Main(append(append([]string{}, sets...), "job", "follow", jobID, "--format", "jsonl"), strings.NewReader(""), &stdout, &stderr)
	if got != exitcode.ExitPartialFailure || stderr.String() != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	recordsThenSummary(t, stdout.Bytes(), file, jobID, "errored")
}

// recordLines are a run's jsonl stdout lines without the summary line
// that ends the stream (28.4), for a test that decodes the records.
func recordLines(stdout string) []string {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if n := len(lines); n > 0 && !strings.Contains(lines[n-1], `"record_id"`) {
		lines = lines[:n-1]
	}
	return lines
}

// recordsThenSummary asserts a jsonl stream of 28.4: the records byte for
// byte (file), then the summary document as the last line, naming the job
// (when jobID is set) and the final status.
func recordsThenSummary(t *testing.T, stdout, file []byte, jobID, finalStatus string) {
	t.Helper()
	if !bytes.HasPrefix(stdout, file) {
		t.Fatalf("jsonl stdout does not begin with commands.jsonl:\n%s\n---\n%s", stdout, file)
	}
	rest := bytes.TrimSuffix(stdout[len(file):], []byte("\n"))
	if bytes.Contains(rest, []byte("\n")) {
		t.Fatalf("more than one line after the records: %q", rest)
	}
	var s records.Summary
	if err := json.Unmarshal(rest, &s); err != nil {
		t.Fatalf("the last line is not a summary: %v: %q", err, rest)
	}
	if s.JobID != jobID || s.FinalStatus != finalStatus {
		t.Fatalf("summary line: job %q status %q, want %q %q", s.JobID, s.FinalStatus, jobID, finalStatus)
	}
}
