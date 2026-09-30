package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
)

// liveArgs is a live run over the dry-run test's fake device, which fails
// every session at once, so the job reaches a terminal quickly.
func liveArgs(sets []string, extra ...string) []string {
	args := append([]string{}, sets...)
	args = append(args, "run", "--target", "127.0.0.1", "--transport", "system")
	args = append(args, extra...)
	return append(args, "show", "clock")
}

// compactJSON is a .json file's content with the layout taken out, so a test
// that asserts how a field is written (`"expectations":[]`, not null) does
// not also assert the file's indentation. A test that only needs a value
// decodes the file instead.
func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, raw)
	}
	return b.String()
}

// waitSummary returns the job's summary.json, compacted, once it exists.
func waitSummary(t *testing.T, base, jobID string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m, _ := filepath.Glob(filepath.Join(base, "jobs", "*", jobID, "summary.json")); len(m) == 1 {
			b, _ := os.ReadFile(m[0])
			return compactJSON(t, b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no summary for job %s", jobID)
	return ""
}

// TestRunDetachReturnsTheReceipt: --detach prints
// the job ID and artifact directory (the receipt under json), exits 0 at
// acceptance, and the job reaches its own terminal afterwards.
func TestRunDetachReturnsTheReceipt(t *testing.T) {
	base, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	var stdout, stderr bytes.Buffer
	if got := Main(liveArgs(sets, "--detach"), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "job_id: ") || !strings.HasPrefix(lines[1], "artifact_dir: ") {
		t.Fatalf("stdout: %q", stdout.String())
	}
	jobID := strings.TrimPrefix(lines[0], "job_id: ")
	if !strings.Contains(stderr.String(), "run "+jobID+" accepted artifacts=") {
		t.Fatalf("stderr: %q", stderr.String())
	}
	summary := waitSummary(t, base, jobID)
	if !strings.Contains(summary, `"mode":"live"`) {
		t.Fatalf("summary: %s", summary)
	}
	stdout.Reset()
	stderr.Reset()
	if got := Main(liveArgs(sets, "--detach", "--format", "json"), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("json exit=%d stderr=%q", got, stderr.String())
	}
	var receipt ipc.JobReceipt
	if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil || receipt.JobID == "" || receipt.ArtifactDir == "" || receipt.Mode != "live" {
		t.Fatalf("receipt: err=%v %+v", err, receipt)
	}
	waitSummary(t, base, receipt.JobID)
	t.Logf("detached %s and %s; both reached a summary", jobID, receipt.JobID)
}

// TestRunFollowFalseWaitsWithoutRendering: --follow=false
// renders no record and exits with the job's own result.
func TestRunFollowFalseWaitsWithoutRendering(t *testing.T) {
	base, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	var stdout, stderr bytes.Buffer
	got := Main(liveArgs(sets, "--follow=false", "--format", "jsonl"), strings.NewReader(""), &stdout, &stderr)
	if stdout.Len() != 0 {
		t.Fatalf("records were rendered: %s", stdout.String())
	}
	// The fake device fails every session, so the job's result is a
	// partial failure; the client's exit is exactly that, and it prints
	// nothing: a run has no result line.
	if got != exitcode.ExitPartialFailure || strings.Contains(stderr.String(), "exit=") {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	// No line names the job (28.4): its folder is the one under the base.
	jobs, _ := filepath.Glob(filepath.Join(base, "jobs", "*", "*"))
	if len(jobs) != 1 {
		t.Fatalf("job folders: %v", jobs)
	}
	jobID := filepath.Base(jobs[0])
	summary := waitSummary(t, base, jobID)
	if !strings.Contains(summary, `"mode":"live"`) || !strings.Contains(summary, `"primary_exit_code":101`) {
		t.Fatalf("summary: %s", summary)
	}
}

// TestRunDetachConflicts: the detach pairs are run_mode_conflict and
// --detach is built.
func TestRunDetachConflicts(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	for _, extra := range [][]string{{"--detach", "--follow"}, {"--detach", "--no-daemon"}, {"--detach", "--dry-run"}, {"--detach", "--exercise"}} {
		var stdout, stderr bytes.Buffer
		if got := Main(liveArgs(sets, extra...), strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitUsageError || !strings.HasPrefix(stderr.String(), "run_mode_conflict: ") {
			t.Errorf("%v: exit=%d stderr=%q", extra, got, stderr.String())
		}
	}
	inv := mustParse(t, "run", "--detach", "--target", "r1", "show", "clock")
	if !inv.Flag(optDetach) || len(inv.Pending) != 0 {
		t.Fatalf("--detach must be built: pending=%v", inv.Pending)
	}
	inv = mustParse(t, "run", "--detach", "--follow=false", "--target", "r1", "show", "clock")
	if !inv.Flag(optDetach) || inv.Flag(optFollow) {
		t.Fatal("--detach --follow=false is redundant, not a conflict")
	}
}
