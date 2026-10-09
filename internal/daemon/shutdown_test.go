package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/records"
)

// heldJob is a live job over the Go fake device holding every session
// open for the given duration: three targets, two commands, serial
// dispatch, and a follower attached through whatever ends it.
type heldJob struct {
	f        *v5Fixture
	jobID    string
	dir      string
	fakeDir  string
	devices  []inventory.Device
	commands []string
	followed chan followed
}

func startHeldJob(t *testing.T, hold string, signals chan os.Signal, sets ...string) *heldJob {
	t.Helper()
	return startHeldJobWith(t, hold, signals, nil, sets...)
}

// startHeldJobWith is startHeldJob with client, when set, changing the
// fixture's configuration after the daemon has it and before the job is
// drafted: a client whose configuration differs from the daemon's.
func startHeldJobWith(t *testing.T, hold string, signals chan os.Signal, client func(*v5Fixture), sets ...string) *heldJob {
	t.Helper()
	fakeDir := t.TempDir()
	t.Setenv("KARVI_TEST_FAKE_DIR", fakeDir)
	t.Setenv("KARVI_TEST_FAKE_HOLD", hold)
	// Both barrier markers exist, so a session for either command proceeds
	// at once.
	for _, m := range []string{"ready-a", "ready-b"} {
		if err := os.WriteFile(filepath.Join(fakeDir, m), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true, Signals: signals, Sets: append([]string{
		`security.child-environment-allowlist=["KARVI_TEST_FAKE_DIR","KARVI_TEST_FAKE_HOLD"]`,
		`dispatch.default="serial"`,
	}, sets...)})
	if client != nil {
		client(f)
	}
	h := &heldJob{f: f, fakeDir: fakeDir, devices: []inventory.Device{direct("127.0.0.1"), direct("127.0.0.2"), direct("127.0.0.3")}, commands: []string{"show a", "show b"}, followed: make(chan followed, 1)}
	sub := f.prepareAndPackageWith(mustID(t), h.commands, h.devices, fixedInput{"alice", "pw"})
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	result, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if err != nil {
		t.Fatal(err)
	}
	h.jobID, h.dir = result.Receipt.JobID, result.Receipt.ArtifactDir
	go func() { h.followed <- f.follow(h.jobID, 0, 1<<20) }()
	// The first command reaches the device and is held there.
	deadline := time.Now().Add(10 * time.Second)
	for h.sent() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("the first command never reached the fake device")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return h
}

// sent is the number of commands the fake device has received.
func (h *heldJob) sent() int {
	log, _ := os.ReadFile(filepath.Join(h.fakeDir, "log"))
	return strings.Count(string(log), "\n")
}

// waitStopped waits for the socket to go, which happens only when Serve
// returns after the accounting, and reports how long that took.
func (h *heldJob) waitStopped(t *testing.T, within time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	for {
		if _, err := os.Stat(h.f.socket); os.IsNotExist(err) {
			return time.Since(start)
		}
		if time.Since(start) > within {
			t.Fatalf("the daemon did not stop within %s", within)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// assertAccounted checks every unit of every target has one terminal
// record with the wanted status and code, the summary partitions the
// targets under the wanted final status and exit, the scoreboard agrees,
// the audit record names the reason, and the follower saw the terminal.
func (h *heldJob) assertAccounted(t *testing.T, status, code, finalStatus string, exit int, cause, reason string) {
	t.Helper()
	assertAccounting(t, h.dir, len(h.devices), len(h.commands))
	recs := readRecords(t, h.dir)
	for _, r := range recs {
		if r.Status != status || (code != "" && (r.Error == nil || r.Error.Code != code)) || (code == "" && r.Error != nil) {
			t.Errorf("record %s/%d: status=%s error=%+v, want %s with code %q", r.SelectedAddress, r.CommandIndex, r.Status, r.Error, status, code)
		}
	}
	s := readSummary(t, h.dir)
	c := s.DeviceCounts
	if s.FinalStatus != finalStatus || s.ExitCode != exit {
		t.Errorf("summary final_status=%s exit=%d, want %s at %d", s.FinalStatus, s.ExitCode, finalStatus, exit)
	}
	if cause != "" && (len(s.TerminalCauses) != 1 || s.TerminalCauses[0] != cause || s.Halt.(map[string]any)["run_wide"] != reason) {
		t.Errorf("summary causes=%v halt=%v, want %s", s.TerminalCauses, s.Halt, cause)
	}
	sb := readScoreboard(t, h.f.cfg.String("scoreboards"))
	if sb.Status != finalStatus || sb.Counts.Total != len(h.devices) || sb.Counts.Completed+sb.Counts.NotStarted+sb.Counts.Incomplete+sb.Counts.Cancelled != sb.Counts.Total || sb.Counts.Incomplete != c["incomplete"] || sb.Counts.Cancelled != c["cancelled"] {
		t.Errorf("scoreboard status=%s counts=%+v", sb.Status, sb.Counts)
	}
	// The targets' states agree with the counts: a device the shutdown
	// left is incomplete, a cancelled one
	// cancelled, and no device is still running or queued at the end.
	states := map[string]int{}
	for _, tg := range sb.Targets {
		states[tg.State]++
	}
	if len(sb.Targets) != len(h.devices) || states[records.TargetIncomplete] != sb.Counts.Incomplete || states[records.TargetCancelled] != sb.Counts.Cancelled || states[records.TargetRunning] != 0 || states[records.TargetQueued] != sb.Counts.NotStarted {
		t.Errorf("scoreboard target states %v against counts %+v", states, sb.Counts)
	}
	audit, _ := os.ReadFile(h.f.cfg.String("audit.file"))
	completed := ""
	for _, line := range strings.Split(string(audit), "\n") {
		if strings.Contains(line, `"event_name":"run.completed"`) {
			completed = line
		}
	}
	if completed == "" || !strings.Contains(completed, `"outcome":"`+finalStatus+`"`) || !strings.Contains(completed, `"reason":"`+reason+`"`) {
		t.Errorf("run.completed audit record: %s", completed)
	}
	select {
	case fw := <-h.followed:
		if fw.err != nil || fw.terminal.Outcome.ExitCode != exit || fw.terminal.Outcome.Summary.FinalStatus != finalStatus || len(fw.records) != len(recs) {
			t.Errorf("follow: err=%v outcome exit=%d status=%s notices=%d", fw.err, fw.terminal.Outcome.ExitCode, fw.terminal.Outcome.Summary.FinalStatus, len(fw.records))
		}
	case <-time.After(5 * time.Second):
		t.Error("the follower never received the terminal")
	}
}

// readScoreboard reads the one scoreboard the fixture's job wrote.
func readScoreboard(t *testing.T, dir string) records.ScoreboardSnapshot {
	t.Helper()
	var out records.ScoreboardSnapshot
	matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(matches) != 1 {
		t.Fatalf("scoreboards in %s: %v", dir, matches)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestForcedStopMidJobStillAccountsEveryUnit: a forced stop while the
// first command is in
// flight. Every unit is incomplete_shutdown with shutdown_incomplete, the
// summary and scoreboard are incomplete at exit 106 under the cause
// shutdown_forced, the audit record names it, and the follower receives
// the terminal. The server returns only after the accounting, and the held
// session never delays it.
func TestForcedStopMidJobStillAccountsEveryUnit(t *testing.T) {
	h := startHeldJob(t, "20s", nil, "daemon.forced-grace-seconds=5")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := StopCompatible(ctx, h.f.socket, 1<<20, StopForce); err != nil {
		t.Fatal(err)
	}
	// The hold is 20 s; a bound well below it still proves the accounting
	// never waits for the session, and holds under the race detector.
	if took := h.waitStopped(t, 20*time.Second); took > 8*time.Second {
		t.Errorf("the forced stop took %s; the accounting must not wait for the held session", took)
	}
	if n := h.sent(); n != 1 {
		t.Errorf("the fake device received %d commands, want the one in flight", n)
	}
	h.assertAccounted(t, "incomplete_shutdown", "shutdown_incomplete", "incomplete", exitcode.ExitShutdownIncomplete, "shutdown:shutdown_forced", "shutdown_forced")
}

// TestSignalDrainsAndStopsWhenTheJobsFinish:
// the first signal drains at once, so a new submission is refused, and
// the job finishes within the grace; the daemon then stops with the job
// complete.
func TestSignalDrainsAndStopsWhenTheJobsFinish(t *testing.T) {
	signals := make(chan os.Signal, 2)
	h := startHeldJob(t, "300ms", signals, "daemon.shutdown-grace-seconds=30")
	signals <- syscall.SIGTERM
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err := Ping(h.f.ctx(), h.f.socket, 1<<20)
		if err == nil && status.Status == "draining" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon never reported draining: %+v %v", status, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	draft, header, _, _ := h.f.draftWith(mustID(t), []string{"show b"}, []inventory.Device{direct("127.0.0.1")}, fixedInput{"alice", "pw"})
	if _, err := PrepareJob(h.f.ctx(), h.f.socket, 1<<20, ipc.PrepareRequest{Header: header, Draft: draft}); err == nil || !strings.Contains(err.Error(), "daemon_draining") {
		t.Errorf("prepare on a draining daemon: %v", err)
	}
	// Six race-instrumented fake sessions under load can take a while; the
	// claim is that the daemon waits for the job, not how long that takes.
	took := h.waitStopped(t, 60*time.Second)
	t.Logf("the drain took %s", took)
	if n := h.sent(); n != len(h.devices)*len(h.commands) {
		t.Errorf("the fake device received %d commands, want all %d", n, len(h.devices)*len(h.commands))
	}
	h.assertAccounted(t, "succeeded", "", "completed", exitcode.ExitSuccess, "", "")
}

// TestSignalGraceExpiryAccountsIncomplete: the grace expires under a
// held session; every unfinished
// unit is incomplete_shutdown under the cause shutdown_grace_expired at
// exit 106.
func TestSignalGraceExpiryAccountsIncomplete(t *testing.T) {
	signals := make(chan os.Signal, 2)
	h := startHeldJob(t, "20s", signals, "daemon.shutdown-grace-seconds=1", "daemon.forced-grace-seconds=1")
	signals <- syscall.SIGTERM
	if took := h.waitStopped(t, 20*time.Second); took > 8*time.Second {
		t.Errorf("grace expiry took %s", took)
	}
	h.assertAccounted(t, "incomplete_shutdown", "shutdown_incomplete", "incomplete", exitcode.ExitShutdownIncomplete, "shutdown:shutdown_grace_expired", "shutdown_grace_expired")
}

// TestSecondSignalShortensTheGrace: a second
// signal cuts a long grace to the forced grace, and the terminal writes
// still happen.
func TestSecondSignalShortensTheGrace(t *testing.T) {
	signals := make(chan os.Signal, 2)
	h := startHeldJob(t, "20s", signals, "daemon.shutdown-grace-seconds=60", "daemon.forced-grace-seconds=1")
	signals <- syscall.SIGTERM
	time.Sleep(200 * time.Millisecond)
	signals <- syscall.SIGINT
	if took := h.waitStopped(t, 20*time.Second); took > 8*time.Second {
		t.Errorf("the shortened grace took %s", took)
	}
	h.assertAccounted(t, "incomplete_shutdown", "shutdown_incomplete", "incomplete", exitcode.ExitShutdownIncomplete, "shutdown:shutdown_grace_expired", "shutdown_grace_expired")
}
