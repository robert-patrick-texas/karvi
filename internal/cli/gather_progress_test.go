package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/testpty"
	"github.com/robert-patrick-texas/karvi/records"
)

// The gathered display's progress line and the operator's Ctrl-C, end to
// end (DECISIONS point 4, "Settled at S4"): at a terminal the line on
// standard error over the job's devices, from done=0/M to the job's own
// counts (its summary's device_counts), erased before the blocks; and the
// invocation's interrupt, SIGINT sent to this process while Main runs, so
// Main's own interrupt context takes it, stopping the gathered display at
// once.

// guardInterrupt keeps a SIGINT from ending the test binary should one
// come when no Main is running; Main's context takes it too.
func guardInterrupt(t *testing.T) {
	t.Helper()
	guard := make(chan os.Signal, 4)
	signal.Notify(guard, os.Interrupt)
	t.Cleanup(func() { signal.Stop(guard) })
}

// interruptOnWrite is a standard output that sends this process SIGINT
// at its first write, the operator's Ctrl-C as the gathered display
// starts its read-back, and holds each later write a moment, so the
// signal is taken before the answer's end.
type interruptOnWrite struct {
	bytes.Buffer
	sent bool
}

func (w *interruptOnWrite) Write(p []byte) (int, error) {
	if !w.sent {
		w.sent = true
		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			return 0, err
		}
	} else {
		time.Sleep(20 * time.Millisecond)
	}
	return w.Buffer.Write(p)
}

// atTerminal runs Main with standard error a terminal, and returns the
// exit, standard output, and what the terminal was sent.
func atTerminal(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	var stdout bytes.Buffer
	code := Main(args, strings.NewReader(""), &stdout, slave)
	return code, stdout.String(), term.Wait(t, slave)
}

// progressLines is the progress lines the terminal was sent, in order, a
// line drawn again unchanged once.
func progressLines(term string) []string {
	var lines []string
	for _, piece := range strings.Split(term, "\rgather: ")[1:] {
		line, _, _ := strings.Cut(piece, "\x1b[K")
		if line = "gather: " + line; len(lines) == 0 || lines[len(lines)-1] != line {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestRunGatheredProgressAtATerminal: in process the line is drawn at the
// display's start over the plan's devices and ends at the job's own counts
// (the summary's device_counts, job list's DONE and FAIL), two devices
// answering and two failing, erased before the blocks; command's one
// device shows done=0/1 through the wait; --quiet shows none.
func TestRunGatheredProgressAtATerminal(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	device := filepath.Join(base, "fake-device")
	raw, err := os.ReadFile(device)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(raw), "    exit) exit 0;;\n", "    exit) exit 0;;\n    'show bad') exit 0;;\n", 1)
	if err := os.WriteFile(device, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(command string, extra ...string) []string {
		args := append(append([]string{}, sets...), "--set", "display.run.gather=true", "run", "--no-daemon")
		return append(append(args, extra...), "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", command)
	}
	for _, command := range []string{"show version", "show bad"} {
		if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
			t.Fatal(err)
		}
		code, stdout, term := atTerminal(t, run(command))
		summary, _ := jobSummary(t, base)
		counts := summary.DeviceCounts
		last := fmt.Sprintf("gather: done=%d/%d fail=%d", counts["done"], counts["total"], counts["fail"])
		lines := progressLines(term)
		if len(lines) < 2 || lines[0] != "gather: done=0/2 fail=0" || lines[len(lines)-1] != last || !strings.HasSuffix(term, "\x1b[K\r\x1b[K") || !strings.HasPrefix(stdout, "! "+command+"\n") {
			t.Fatalf("%s: exit=%d drawn %q, want from done=0/2 to %q\nterminal %q\nstdout:\n%s", command, code, lines, last, term, stdout)
		}
		if command == "show bad" && counts["fail"] != 2 {
			t.Fatalf("show bad: device_counts %v", counts)
		}
	}

	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	args := append(append([]string{}, sets...), "--set", "display.command.gather=true", "--set", "display.command.footer=", "command", "--format", "text", "--transport", "system", "127.0.0.1", "show", "version")
	code, stdout, term := atTerminal(t, args)
	if lines := progressLines(term); code != 0 || len(lines) != 2 || lines[0] != "gather: done=0/1 fail=0" || lines[1] != "gather: done=1/1 fail=0" || stdout != "! show version\n! 127.0.0.1 (1)\nok\n" {
		t.Fatalf("command: exit=%d drawn %q\nterminal %q\nstdout:\n%s", code, lines, term, stdout)
	}

	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	quiet := append(append([]string{}, sets...), "--quiet", "--set", "display.run.gather=true", "run", "--no-daemon", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version")
	if code, _, term := atTerminal(t, quiet); code != 0 || strings.Contains(term, "gather:") || strings.Contains(term, "\x1b[K") {
		t.Fatalf("--quiet: exit=%d terminal %q", code, term)
	}
}

// TestRunNoDaemonGatheredInterrupted: in process a Ctrl-C while the job
// runs (the first device between its two commands, serial) cancels the
// job as today and shows nothing gathered, only the footer with exit 113;
// a Ctrl-C as the read-back starts stops it between the answer's pieces:
// what was written stands, the cut line ended, the footer after, exit 113;
// a crun's the same, its job ended with its collection summary, and its
// crun.after not started: no hook, no warning, no audit event, as through
// the daemon.
func TestRunNoDaemonGatheredInterrupted(t *testing.T) {
	guardInterrupt(t)
	base, _, _ := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.run.gather=true", "--set", "display.run.footer=! exit=<exit-code>")
	args := append(append([]string{}, sets...), "run", "--no-daemon", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--dispatch", "serial", "--cmd", "show version", "--sleep", "3s", "--cmd", "show version")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go func() {
		// SIGINT once the first device's first record is in its job's
		// commands.jsonl: the device sleeps 3 s before its second.
		for ctx.Err() == nil {
			m, _ := filepath.Glob(filepath.Join(base, "jobs", "*", "*", "commands.jsonl"))
			if len(m) == 1 {
				if fi, err := os.Stat(m[0]); err == nil && fi.Size() > 0 {
					_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	var stdout, stderr bytes.Buffer
	code := Main(args, strings.NewReader(""), &stdout, &stderr)
	cancel()
	if code != exitcode.ExitCancelled || stdout.String() != "! exit=113\n" {
		t.Fatalf("while the job ran: exit=%d stderr=%q\nstdout:\n%s", code, stderr.String(), stdout.String())
	}

	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	out := &interruptOnWrite{}
	stderr.Reset()
	code = Main(append(append([]string{}, sets...), "run", "--no-daemon", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show big"), strings.NewReader(""), out, &stderr)
	got := out.String()
	if code != exitcode.ExitCancelled || !strings.HasPrefix(got, "! show big\n! 127.0.0.[1-2] (2)\n") || !strings.HasSuffix(got, "\n! exit=113\n") || len(got) > 750_000 {
		t.Fatalf("during the read-back: exit=%d stderr=%q\n%d bytes: %q ... %q", code, stderr.String(), len(got), got[:min(len(got), 60)], got[max(0, len(got)-60):])
	}

	// The daemon test's crun case in process: the job has ended, so the
	// hook is asked for under the interrupted invocation's context.
	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	hookOut := filepath.Join(base, "hook.out")
	hook := filepath.Join(base, "hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho ran >"+hookOut+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out = &interruptOnWrite{}
	stderr.Reset()
	code = Main(append(append([]string{}, sets...), "--set", `platform.cisco_ios.crun-commands=["show big"]`, "--set", fmt.Sprintf("crun.after=%q", hook), "crun", "--no-daemon", "--cd="+filepath.Join(base, "crun"), "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system"), strings.NewReader(""), out, &stderr)
	summary, _ := jobSummary(t, base)
	if code != exitcode.ExitCancelled || summary.Collection == nil || !strings.HasPrefix(out.String(), "! show big\n") || out.Len() > 750_000 {
		t.Fatalf("crun during the read-back: exit=%d collection %+v stderr=%q\n%d bytes shown", code, summary.Collection, stderr.String(), out.Len())
	}
	if _, err := os.Stat(hookOut); err == nil {
		t.Fatal("crun.after ran after the display was interrupted")
	}
	if events := crunAfterEvents(t, base); strings.Contains(stderr.String(), "crun_after") || len(events) != 0 {
		t.Fatalf("crun.after was started after the display was interrupted: stderr=%q audit %q", stderr.String(), events)
	}
}

// crunAfterEvents is the crun.after events of the audit under base,
// EVENT:OUTCOME.
func crunAfterEvents(t *testing.T, base string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(base, "audit.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var events []string
	for line := range strings.Lines(string(data)) {
		var r records.AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		if r.EventName == "crun.after" {
			events = append(events, r.EventName+":"+r.Outcome)
		}
	}
	return events
}

// TestRunThroughTheDaemonGatheredInterrupted: through the daemon the
// line's devices are the submitted plan's, the line first drawn after the
// receipt's warnings; a Ctrl-C during the read-back at the job's end stops
// the display: the job ended, so the client says "job ID ended exit=N;
// artifacts DIR" and directly after it "cancelled: the display of job ID
// was interrupted", exit 113, no footer, the job's summary keeping its
// exit; job follow of the job the same; a crun's crun.after is not asked
// for, as for a Ctrl-C during the follow.
func TestRunThroughTheDaemonGatheredInterrupted(t *testing.T) {
	guardInterrupt(t)
	base, _, socket := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.run.gather=true", "--set", "display.run.footer=! exit=<exit-code>")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var launch bytes.Buffer
	rt, err := ensureTestDaemon(ctx, globalsFor(sets), &launch)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (%s)", err, launch.String())
	}
	defer stopTestDaemon(t, socket, rt.MaxFrame)
	run := func(command string) []string {
		return append(append([]string{}, sets...), "run", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", command)
	}

	code, stdout, term := atTerminal(t, run("show version"))
	if lines := progressLines(term); code != 0 || len(lines) < 2 || lines[0] != "gather: done=0/2 fail=0" || lines[len(lines)-1] != "gather: done=2/2 fail=0" || !strings.HasSuffix(term, "\x1b[K\r\x1b[K") || !strings.HasPrefix(stdout, "! show version\n") {
		t.Fatalf("the line through the daemon: exit=%d drawn %q\nterminal %q\nstdout:\n%s", code, lines, term, stdout)
	}
	if before, ok := beforeTheLine(term); !ok || !strings.Contains(before, insecureWarning) {
		t.Fatalf("through the daemon the receipt's warnings are not before the line: %q", term)
	}

	interrupted := func(args []string, jobID, dir string) string {
		t.Helper()
		out := &interruptOnWrite{}
		var stderr bytes.Buffer
		code := Main(args, strings.NewReader(""), out, &stderr)
		if jobID == "" {
			summary, d := jobSummary(t, base)
			jobID, dir = summary.JobID, d
			if summary.ExitCode != 0 {
				t.Errorf("the job's own exit %d, want 0", summary.ExitCode)
			}
		}
		// The ended line, then the cancelled line directly after it.
		want := "job " + jobID + " ended exit=0; artifacts " + dir + "\ncancelled: the display of job " + jobID + " was interrupted\n"
		if code != exitcode.ExitCancelled || !strings.Contains(stderr.String(), want) || strings.Contains(stderr.String(), "continues") || strings.Contains(out.String(), "! exit=") || !strings.HasPrefix(out.String(), "! show big\n") || out.Len() > 750_000 {
			t.Fatalf("%v: exit=%d stderr=%q\nwant the lines %q\n%d bytes shown", args[len(sets):], code, stderr.String(), want, out.Len())
		}
		return stderr.String()
	}
	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	interrupted(run("show big"), "", "")
	summary, dir := jobSummary(t, base)
	interrupted(append(append([]string{}, sets...), "job", "follow", summary.JobID), summary.JobID, dir)

	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	hookOut := filepath.Join(base, "hook.out")
	hook := filepath.Join(base, "hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho ran >"+hookOut+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stderr := interrupted(append(append([]string{}, sets...), "--set", `platform.cisco_ios.crun-commands=["show big"]`, "--set", fmt.Sprintf("crun.after=%q", hook), "crun", "--cd="+filepath.Join(base, "crun"), "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system"), "", "")
	if _, err := os.Stat(hookOut); err == nil {
		t.Fatal("crun.after ran after the display was interrupted")
	}
	// Nor a warning or an audit event: the interrupted display hands the
	// hook no summary, and one asked for under the interrupted context
	// would not start either, silent in process too
	// (TestRunNoDaemonGatheredInterrupted).
	if events := crunAfterEvents(t, base); strings.Contains(stderr, "crun_after") || len(events) != 0 {
		t.Fatalf("crun.after was asked for after the display was interrupted: stderr=%q audit %q", stderr, events)
	}
}

// TestRunGatheredProgressHalted: a serial run of three devices under
// dispatch.halt-on-error-count=1, the first failing, so dispatch never
// starts the other two (their records not_started_halt): the line's last
// counts are still the summary's device_counts, job list's DONE and FAIL,
// done=1/3 fail=1, the devices never started counted neither done nor
// failed.
func TestRunGatheredProgressHalted(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	device := filepath.Join(base, "fake-device")
	raw, err := os.ReadFile(device)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(raw), "    exit) exit 0;;\n", "    exit) exit 0;;\n    'show bad') exit 0;;\n", 1)
	if err := os.WriteFile(device, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	args := append(append([]string{}, sets...), "--set", "display.run.gather=true", "--set", "dispatch.halt-on-error-count=1",
		"run", "--no-daemon", "--dispatch", "serial", "--format", "text",
		"--target", "127.0.0.1", "--target", "127.0.0.2", "--target", "127.0.0.3", "--transport", "system", "--cmd", "show bad")
	code, stdout, term := atTerminal(t, args)
	summary, _ := jobSummary(t, base)
	counts := summary.DeviceCounts
	lines := progressLines(term)
	if counts["done"] != 1 || counts["fail"] != 1 || counts["total"] != 3 || len(lines) == 0 || lines[len(lines)-1] != "gather: done=1/3 fail=1" {
		t.Fatalf("exit=%d device_counts %v; drawn %q, want the last gather: done=1/3 fail=1\nstdout:\n%s", code, counts, lines, stdout)
	}
}

// beforeTheLine is what the terminal was sent before the line was first
// drawn; ok when it was drawn, nothing before it drew or erased a line,
// and it ends a line, so the first draw starts on a line of its own.
func beforeTheLine(term string) (string, bool) {
	first := strings.Index(term, "\rgather: ")
	if first < 0 {
		return term, false
	}
	before := term[:first]
	return before, !strings.Contains(before, clearToTheEnd) && (before == "" || strings.HasSuffix(before, "\n"))
}

// clearToTheEnd is the escape that ends every draw and erase of the line;
// insecureWarning begins the admission warning of the insecure host-key
// policy, which gatherSets runs under.
const (
	clearToTheEnd   = "\x1b[K"
	insecureWarning = "ssh host-key policy insecure: "
)

// halfLineWrites is each place the terminal was sent something other than
// a carriage return right after a drawn line (a write after a half line),
// with a little of what came after.
func halfLineWrites(term string) []string {
	var bad []string
	for i := 0; ; {
		j := strings.Index(term[i:], "\rgather: ")
		if j < 0 {
			return bad
		}
		start := i + j
		end := strings.Index(term[start:], clearToTheEnd)
		if end < 0 {
			return append(bad, term[start:])
		}
		next := start + end + len(clearToTheEnd)
		if next < len(term) && term[next] != '\r' {
			bad = append(bad, term[start:min(len(term), next+60)])
		}
		i = next
	}
}

// TestRunGatheredBlocksAfterTheErasedLine: in process, standard output and
// standard error one terminal as on an operator's screen, the line is
// erased before the blocks print, run's and command's alike: nothing is
// written after a half line, and the first block's header starts where
// the line was.
func TestRunGatheredBlocksAfterTheErasedLine(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	oneTerminal := func(args []string) (int, string) {
		t.Helper()
		master, slave := testpty.Open(t)
		term := testpty.Record(master)
		code := Main(args, strings.NewReader(""), slave, slave)
		return code, term.Wait(t, slave)
	}
	run := append(append([]string{}, sets...), "--set", "display.run.gather=true", "run", "--no-daemon", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version")
	code, term := oneTerminal(run)
	if bad := halfLineWrites(term); code != 0 || len(bad) > 0 || !strings.Contains(term, "\rgather: done=2/2 fail=0\x1b[K\r\x1b[K"+gatheredOK("show version")) {
		t.Fatalf("run: exit=%d, %d writes after a half line: %q\nterminal %q", code, len(bad), bad, term)
	}
	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	command := append(append([]string{}, sets...), "--set", "display.command.gather=true", "--set", "display.command.footer=", "command", "--format", "text", "--transport", "system", "127.0.0.1", "show", "version")
	code, term = oneTerminal(command)
	if bad := halfLineWrites(term); code != 0 || len(bad) > 0 || !strings.Contains(term, "\rgather: done=1/1 fail=0\x1b[K\r\x1b[K! show version\n! 127.0.0.1 (1)\nok\n") {
		t.Fatalf("command: exit=%d, %d writes after a half line: %q\nterminal %q", code, len(bad), bad, term)
	}
}

// TestRunGatheredDebugLinesAroundTheLine: in process the line is the job's
// standard error, so the --debug lines written there while the job runs
// each erase it first and draw it again after: debug lines come after the
// first draw, and nothing is written after a half line.
func TestRunGatheredDebugLinesAroundTheLine(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	args := append(append([]string{}, sets...), "--debug", "--set", "display.run.gather=true", "run", "--no-daemon", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version")
	code, _, term := atTerminal(t, args)
	first := strings.Index(term, "\rgather: ")
	lines := progressLines(term)
	if code != 0 || first < 0 || !strings.Contains(term[first:], " DEBUG ") || len(lines) == 0 || lines[len(lines)-1] != "gather: done=2/2 fail=0" {
		t.Fatalf("exit=%d drawn %q, want debug lines after the first draw\nterminal %q", code, lines, term)
	}
	if bad := halfLineWrites(term); len(bad) > 0 {
		t.Fatalf("%d writes after a half line: %q", len(bad), bad)
	}
}

// TestJobFollowFromTheFolderGatheredInterrupted: job follow with no daemon
// (from the job's folder) of a finished job, a Ctrl-C during the
// read-back: the display stops, the job ended, so the ended line and
// directly after it the cancelled line, exit 113, no footer, never the
// line that says the job continues.
func TestJobFollowFromTheFolderGatheredInterrupted(t *testing.T) {
	guardInterrupt(t)
	base, _, _ := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.run.footer=! exit=<exit-code>")
	var stdout, stderr bytes.Buffer
	if code := Main(append(append([]string{}, sets...), "run", "--no-daemon", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show big"), strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("the job: exit=%d stderr=%q", code, stderr.String())
	}
	summary, dir := jobSummary(t, base)
	out := &interruptOnWrite{}
	stderr.Reset()
	code := Main(append(append([]string{}, sets...), "--set", "display.run.gather=true", "job", "follow", summary.JobID), strings.NewReader(""), out, &stderr)
	want := "job " + summary.JobID + " ended exit=0; artifacts " + dir + "\ncancelled: the display of job " + summary.JobID + " was interrupted\n"
	if code != exitcode.ExitCancelled || !strings.Contains(stderr.String(), want) || strings.Contains(stderr.String(), "continues") || strings.Contains(out.String(), "! exit=") || !strings.HasPrefix(out.String(), "! show big\n") || out.Len() > 750_000 {
		t.Fatalf("exit=%d stderr=%q\nwant the lines %q\n%d bytes shown", code, stderr.String(), want, out.Len())
	}
}

// TestRunGatheredProgressInStandardErrorsColours: in process the line is
// styled by standard error's own colour policy, never standard output's:
// under display.color=always, standard output a file (`> out.txt`, which
// is written plain whatever the mode) and standard error a terminal, the
// line is coloured like the footer, its words in the label colour and its
// counts in the value colour, while the blocks in the file are plain.
func TestRunGatheredProgressInStandardErrorsColours(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.color=always", "--set", "display.theme=dark")
	args := append(append([]string{}, sets...), "--set", "display.run.gather=true", "run", "--no-daemon", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version")
	file, err := os.Create(filepath.Join(t.TempDir(), "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	code := Main(args, strings.NewReader(""), file, slave)
	got := term.Wait(t, slave)
	blocks, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	label, value := display.RoleColor("dark", "label", "default"), display.RoleColor("dark", "value", "default")
	styled := func(done, fail string) string {
		return "\r" + display.ANSIStyle("gather: done=", label, true, false) + display.ANSIStyle(done, value, true, false) +
			display.ANSIStyle(" fail=", label, true, false) + display.ANSIStyle(fail, value, true, false) + clearToTheEnd
	}
	if code != 0 || !strings.Contains(got, styled("0/2", "0")) || !strings.Contains(got, styled("2/2", "0")) || string(blocks) != gatheredOK("show version") {
		t.Fatalf("exit=%d terminal %q\nwant the line drawn as %q\nout.txt %q", code, got, styled("0/2", "0"), blocks)
	}
}

// TestRunGatheredProgressAfterTheAdmissionWarnings: in process the line is
// first drawn after the job's admission warnings (here the insecure
// host-key policy's, said once for the job), so they stand on the
// terminal as written, no line drawn or erased before them.
func TestRunGatheredProgressAfterTheAdmissionWarnings(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	args := append(append([]string{}, sets...), "--set", "display.run.gather=true", "run", "--no-daemon", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version")
	code, _, term := atTerminal(t, args)
	if before, ok := beforeTheLine(term); code != 0 || !ok || !strings.Contains(before, insecureWarning) {
		t.Fatalf("exit=%d, the admission warnings not before the line\nterminal %q", code, term)
	}
}

// TestRunGatheredProgressNotToAFileOrAPipe: in process no line is written
// where standard error is not itself a terminal, whatever standard output
// is: to a file (`2> err.log`) with standard output at the terminal, and
// to a pipe under display.color=always, which colours the pipe's warnings
// as today and writes no line there.
func TestRunGatheredProgressNotToAFileOrAPipe(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	run := func(extra ...string) []string {
		args := append(append(append([]string{}, sets...), extra...), "--set", "display.run.gather=true", "run", "--no-daemon", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version")
		return args
	}
	errLog, err := os.Create(filepath.Join(t.TempDir(), "err.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer errLog.Close()
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	code := Main(run(), strings.NewReader(""), slave, errLog)
	shown := term.Wait(t, slave)
	logged, err := os.ReadFile(errLog.Name())
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || !strings.Contains(string(logged), insecureWarning) || strings.Contains(string(logged), "gather:") || strings.Contains(string(logged), clearToTheEnd) || shown != gatheredOK("show version") {
		t.Fatalf("2> err.log: exit=%d err.log %q\nterminal %q", code, logged, shown)
	}

	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	piped := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(r)
		piped <- b
	}()
	var stdout bytes.Buffer
	code = Main(run("--set", "display.color=always"), strings.NewReader(""), &stdout, w)
	w.Close()
	got := string(<-piped)
	if code != 0 || !strings.Contains(got, "\x1b[") || !strings.Contains(got, insecureWarning) || strings.Contains(got, "gather:") || strings.Contains(got, clearToTheEnd) || display.StripANSI(stdout.String()) != gatheredOK("show version") {
		t.Fatalf("a pipe: exit=%d the pipe %q\nstdout %q", code, got, stdout.String())
	}
}

// TestRunThroughTheDaemonGatheredQuiet: through the daemon --quiet shows
// no line at a terminal, on run and on job follow of its job, while the
// display still gathers: the two devices' alike answer shown once, as
// --quiet shows a group (its answer alone).
func TestRunThroughTheDaemonGatheredQuiet(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.run.gather=true")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var launch bytes.Buffer
	rt, err := ensureTestDaemon(ctx, globalsFor(sets), &launch)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (%s)", err, launch.String())
	}
	defer stopTestDaemon(t, socket, rt.MaxFrame)
	code, stdout, term := atTerminal(t, append(append([]string{}, sets...), "--quiet", "run", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version"))
	if code != 0 || strings.Contains(term, "gather:") || strings.Contains(term, clearToTheEnd) || stdout != "ok\n" {
		t.Fatalf("run --quiet: exit=%d terminal %q\nstdout:\n%s", code, term, stdout)
	}
	summary, _ := jobSummary(t, base)
	code, stdout, term = atTerminal(t, append(append([]string{}, sets...), "--quiet", "job", "follow", summary.JobID))
	if code != 0 || strings.Contains(term, "gather:") || strings.Contains(term, clearToTheEnd) || stdout != "ok\n" {
		t.Fatalf("job follow --quiet: exit=%d terminal %q\nstdout:\n%s", code, term, stdout)
	}
}

// TestCrunThroughTheDaemonGatheredProgress: a crun through the daemon at a
// terminal shows the line over its devices, from done=0/2 to the job's
// own counts, erased before its blocks.
func TestCrunThroughTheDaemonGatheredProgress(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.run.gather=true")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var launch bytes.Buffer
	rt, err := ensureTestDaemon(ctx, globalsFor(sets), &launch)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (%s)", err, launch.String())
	}
	defer stopTestDaemon(t, socket, rt.MaxFrame)
	args := append(append([]string{}, sets...), "--set", `platform.cisco_ios.crun-commands=["show version"]`, "crun", "--cd="+filepath.Join(base, "crun"), "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system")
	code, stdout, term := atTerminal(t, args)
	lines := progressLines(term)
	if code != 0 || len(lines) < 2 || lines[0] != "gather: done=0/2 fail=0" || lines[len(lines)-1] != "gather: done=2/2 fail=0" || !strings.HasSuffix(term, "\x1b[K\r\x1b[K") || !strings.HasPrefix(stdout, gatheredOK("show version")) {
		t.Fatalf("crun: exit=%d drawn %q\nterminal %q\nstdout:\n%s", code, lines, term, stdout)
	}
}

// TestJobFollowOfACommandJobGatheredProgress: job follow of a command's
// job under run's key shows the same wait as command did: done=0/1 over
// its one device (from its folder's summary.json), then done=1/1, erased
// before its one block.
func TestJobFollowOfACommandJobGatheredProgress(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.command.footer=")
	var stdout, stderr bytes.Buffer
	if code := Main(append(append([]string{}, sets...), "command", "--format", "text", "--transport", "system", "127.0.0.1", "show", "version"), strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("command: exit=%d stderr=%q", code, stderr.String())
	}
	summary, _ := jobSummary(t, base)
	code, shown, term := atTerminal(t, append(append([]string{}, sets...), "--set", "display.run.gather=true", "job", "follow", summary.JobID))
	if lines := progressLines(term); code != 0 || len(lines) != 2 || lines[0] != "gather: done=0/1 fail=0" || lines[1] != "gather: done=1/1 fail=0" || !strings.HasSuffix(term, "\x1b[K\r\x1b[K") || shown != "! show version\n! 127.0.0.1 (1)\nok\n" {
		t.Fatalf("job follow: exit=%d drawn %q\nterminal %q\nstdout:\n%s", code, lines, term, shown)
	}
}

// TestJobFollowGatheredSummaryUnreadable: job follow by job ID through the
// daemon of a running job whose summary.json cannot be read (its mode
// 0000), so neither the job's records nor the line's count of its devices
// can be judged: at a terminal and not, the summary read where the display
// gathers (DECISIONS point 4, the third "Settled at S6"), the follow ends
// with nothing shown and no line drawn, its line that the job continues
// in the daemon, then run_output_records_unreadable naming the file, exit
// 111; the job is unaffected: once it ends, its summary written again,
// keeping exit 0, a follow shows it whole, exit 0.
func TestJobFollowGatheredSummaryUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file of mode 0000")
	}
	base, _, socket := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.run.gather=true")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var launch bytes.Buffer
	rt, err := ensureTestDaemon(ctx, globalsFor(sets), &launch)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (%s)", err, launch.String())
	}
	defer stopTestDaemon(t, socket, rt.MaxFrame)
	var stdout, stderr bytes.Buffer
	detach := append(append([]string{}, sets...), "run", "--detach", "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version", "--sleep", "3s", "--cmd", "show clock")
	if code := Main(detach, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("run --detach: exit=%d stderr=%q", code, stderr.String())
	}
	summary, dir := jobSummary(t, base)
	path := filepath.Join(dir, "summary.json")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	code, shown, term := atTerminal(t, append(append([]string{}, sets...), "job", "follow", summary.JobID))
	t.Logf("job follow, summary.json unreadable: exit=%d stdout=%q\nterminal %q", code, shown, term)
	// The follow's line for a job it leaves running, then the error.
	want := "job " + summary.JobID + " continues in the daemon; artifacts " + dir + "\nrun_output_records_unreadable: open " + path + ": permission denied\n"
	if code != exitcode.ExitOutputFailure || shown != "" || !strings.HasSuffix(term, want) || strings.Contains(term, "gather:") || strings.Contains(term, clearToTheEnd) {
		t.Fatalf("job follow, summary.json unreadable: exit=%d stdout=%q\nterminal %q\nwant the lines %q", code, shown, term, want)
	}
	stdout.Reset()
	stderr.Reset()
	code = Main(append(append([]string{}, sets...), "job", "follow", summary.JobID), strings.NewReader(""), &stdout, &stderr)
	if code != exitcode.ExitOutputFailure || stdout.String() != "" || !strings.HasSuffix(stderr.String(), want) {
		t.Fatalf("job follow, no line: exit=%d stdout=%q\nstderr=%q\nwant the lines %q", code, stdout.String(), stderr.String(), want)
	}
	waitSummary(t, base, summary.JobID)
	stdout.Reset()
	stderr.Reset()
	code = Main(append(append([]string{}, sets...), "job", "follow", summary.JobID), strings.NewReader(""), &stdout, &stderr)
	want = gatheredOK("show version") + "\n" + gatheredOK("show clock")
	if code != 0 || stdout.String() != want {
		t.Fatalf("job follow after the job's end: exit=%d stderr=%q\nstdout:\n%s\nwant:\n%s", code, stderr.String(), stdout.String(), want)
	}
	if ended, _ := jobSummary(t, base); ended.ExitCode != 0 || ended.FinalStatus == "" {
		t.Fatalf("the job's end: exit %d status %q", ended.ExitCode, ended.FinalStatus)
	}
}
