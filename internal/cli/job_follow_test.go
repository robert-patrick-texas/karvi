package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
// derives it: an empty canonical file and a summary.
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

// TestJobFollowDirectoryStates: a directory without a summary is
// job_orphaned (3.3); no directory is job_unknown naming the derived path
// (3.4); a malformed ID is job_request_malformed (3.1); a daemon that
// answers job_unknown yields to the directory (3.2).
func TestJobFollowDirectoryStates(t *testing.T) {
	_, g, socket := daemonTestRuntime(t)
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
	if got := jobFollow(context.Background(), followInvocation(t, g, fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr}); got != exitcode.ExitJobRejected || !strings.HasPrefix(stderr.String(), "job_orphaned: ") || !strings.Contains(stderr.String(), "holds no summary") {
		t.Fatalf("orphaned: exit=%d stderr=%q", got, stderr.String())
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
