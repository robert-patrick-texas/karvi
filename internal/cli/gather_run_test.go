package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/records"
)

// The gathered display (display.run.gather) on run's paths, end to end over
// a fake device that answers every command "ok": in process (--no-daemon)
// and through a daemon the real launcher started, then job follow of the
// job through the daemon and from its folder. `show gone` has the device
// remove the job's commands.jsonl before it answers: the job runs on (the
// store appends to the file it holds open), and the display's end cannot
// read the file back. `show swap` puts a link at the name in one rename,
// so the name is there whenever a follower looks: the end's open refuses
// it. `show close` puts a folder at failed-devices.txt, so the store's
// close fails after every record is written. `show empty` empties the
// store's own file in place (its name opened, cut to nothing): the records
// before it are gone from the file. `show big` answers 20,000 lines
// (about 1.5 MB), a read-back of many pieces.

// gatherSets are the sets of a run over the fake device under base, the
// footer left out so a display is its blocks alone.
func gatherSets(t *testing.T, base string) []string {
	t.Helper()
	sets := dryRunSets(t, base)
	device := filepath.Join(base, "fake-device")
	body := "#!/bin/sh\ncase \" $* \" in *' -O '*) exit 1;; esac\nprintf 'dev#'\nwhile IFS= read -r line; do\n" +
		"  case \"$line\" in\n" +
		"    exit) exit 0;;\n" +
		"    'show gone') rm -f \"" + base + "\"/jobs/*/*/commands.jsonl; printf '%s\\r\\nok\\r\\ndev#' \"$line\";;\n" +
		"    'show swap') for d in \"" + base + "\"/jobs/*/*/; do ln -sf nowhere \"${d}.swap\" && mv -T \"${d}.swap\" \"${d}commands.jsonl\"; done; printf '%s\\r\\nok\\r\\ndev#' \"$line\";;\n" +
		"    'show empty') for d in \"" + base + "\"/jobs/*/*/; do : >\"${d}commands.jsonl\"; done; printf '%s\\r\\nok\\r\\ndev#' \"$line\";;\n" +
		"    'show close') for d in \"" + base + "\"/jobs/*/*/; do mkdir -p \"${d}failed-devices.txt/x\"; done; printf '%s\\r\\nok\\r\\ndev#' \"$line\";;\n" +
		"    'show big') printf '%s\\r\\n' \"$line\"; i=0; while [ $i -lt 20000 ]; do printf 'line %06d xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\\r\\n' $i; i=$((i+1)); done; printf 'dev#';;\n" +
		"    *) printf '%s\\r\\nok\\r\\ndev#' \"$line\";;\n" +
		"  esac\ndone\n"
	if err := os.WriteFile(device, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(base, "karvi-askpass")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KARVI_ASKPASS_PATH", helper)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	return append(sets,
		"--set", fmt.Sprintf("ssh.transports.system=%q", device),
		"--set", `ssh.host-key-policy="insecure"`,
		"--set", "display.run.footer=",
	)
}

// gatherRun runs `run` over the two fake devices with the extra arguments
// and the command, the gathered display on.
func gatherRun(sets []string, command string, extra ...string) (int, string, string) {
	args := append(append([]string{}, sets...), "--set", "display.run.gather=true", "run")
	args = append(append(args, extra...), "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", command)
	var stdout, stderr bytes.Buffer
	code := Main(args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// gatheredOK is the display of both devices answering one command alike.
func gatheredOK(command string) string {
	return "! " + command + "\n! 127.0.0.[1-2] (2)\nok\n"
}

// jobSummary is the one job's summary under base, and its folder.
func jobSummary(t *testing.T, base string) (records.Summary, string) {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(base, "jobs", "*", "*", "summary.json"))
	if len(m) != 1 {
		t.Fatalf("summaries %v", m)
	}
	var s records.Summary
	raw, err := os.ReadFile(m[0])
	if err != nil || json.Unmarshal(raw, &s) != nil {
		t.Fatalf("read %s: %v", m[0], err)
	}
	return s, filepath.Dir(m[0])
}

// TestRunNoDaemonGathered: in process the two devices' alike answers are
// one group; under --nof the key true shows today's display (a header per
// device). The answers are read back through the file the store holds
// open, never its name: the file removed or a link put at the name during
// the run changes nothing. The store's own file changed in place (`show
// empty` on the second device, serial, after the first's record) is
// run_output_records_unreadable, exit 111, nothing shown, and the job's
// summary carries the exit.
func TestRunNoDaemonGathered(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	code, stdout, stderr := gatherRun(sets, "show version", "--no-daemon")
	if code != 0 || stdout != gatheredOK("show version") {
		t.Fatalf("gathered: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	code, stdout, stderr = gatherRun(sets, "show version", "--no-daemon", "--nof")
	if code != 0 || strings.Count(stdout, "! 127.0.0.") != 2 || strings.Contains(stdout, "! show version") {
		t.Fatalf("--nof: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	for _, command := range []string{"show gone", "show swap"} {
		if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr = gatherRun(sets, command, "--no-daemon")
		summary, _ := jobSummary(t, base)
		if code != 0 || stdout != gatheredOK(command) || summary.ExitCode != 0 {
			t.Fatalf("%s: exit=%d summary exit=%d stderr=%q\nstdout:\n%s", command, code, summary.ExitCode, stderr, stdout)
		}
	}
	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = gatherRun(sets, "show empty", "--no-daemon", "--dispatch", "serial")
	summary, dir := jobSummary(t, base)
	want := "run_output_records_unreadable: " + filepath.Join(dir, "commands.jsonl") + " line 1 is not the record the job's stream carried\n  carried  record 1, 127.0.0.1 command 1"
	if code != exitcode.ExitOutputFailure || stdout != "" || !strings.Contains(stderr, want) {
		t.Fatalf("emptied: exit=%d stdout=%q\nstderr=%q\nwant %q", code, stdout, stderr, want)
	}
	if summary.ExitCode != exitcode.ExitOutputFailure {
		t.Fatalf("emptied: the summary's exit %d, want %d", summary.ExitCode, exitcode.ExitOutputFailure)
	}
}

// TestCommandGathered: command gathers under its own key,
// display.command.gather, never under run's; job follow shows the job by
// display.run.gather, as by the other display.run keys, so with both keys
// set alike the two displays are the same.
func TestCommandGathered(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.command.footer=")
	main := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Main(append(append([]string{}, sets...), args...), strings.NewReader(""), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	command := []string{"command", "--format", "text", "--transport", "system", "127.0.0.1", "show", "version"}
	gathered := "! show version\n! 127.0.0.1 (1)\nok\n"
	code, stdout, stderr := main(append([]string{"--set", "display.run.gather=true"}, command...)...)
	if code != 0 || stdout == gathered || !strings.HasPrefix(stdout, "! 127.0.0.1 [127.0.0.1]") {
		t.Fatalf("run's key: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = main(append([]string{"--set", "display.command.gather=true"}, command...)...)
	if code != 0 || stdout != gathered {
		t.Fatalf("command's key: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	summary, _ := jobSummary(t, base)
	if code, stdout, stderr := main("--set", "display.run.gather=true", "job", "follow", summary.JobID); code != 0 || stdout != gathered {
		t.Fatalf("job follow, run's key: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	if code, stdout, stderr := main("--set", "display.command.gather=true", "job", "follow", summary.JobID); code != 0 || stdout == gathered || !strings.HasPrefix(stdout, "! 127.0.0.1 [127.0.0.1]") {
		t.Fatalf("job follow, command's key: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
}

// TestRunNoDaemonGatheredUnshown: under --follow=false the display goes
// nowhere and nothing is gathered: nothing read back at the end, so the
// store's own file changed in place (`show empty`, serial, which a
// gathered display refuses at the end) leaves the job's exit 0 as with the
// key false.
func TestRunNoDaemonGatheredUnshown(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	code, stdout, stderr := gatherRun(sets, "show empty", "--no-daemon", "--follow=false", "--dispatch", "serial")
	summary, _ := jobSummary(t, base)
	if code != 0 || stdout != "" || strings.Contains(stderr, "run_output_records_unreadable") || summary.ExitCode != 0 {
		t.Fatalf("exit=%d summary exit=%d stdout=%q\nstderr=%q", code, summary.ExitCode, stdout, stderr)
	}
}

// TestRunNoDaemonGatheredOrdinaryDisplays: the key true is the ordinary
// display under --format jsonl and json (each record as JSON, no block)
// and where the job keeps no commands.jsonl (a header per device).
func TestRunNoDaemonGatheredOrdinaryDisplays(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	for _, format := range []string{"jsonl", "json"} {
		if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
			t.Fatal(err)
		}
		args := append(append([]string{}, sets...), "--set", "display.run.gather=true", "run", "--no-daemon", "--format", format, "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version")
		var stdout, stderr bytes.Buffer
		code := Main(args, strings.NewReader(""), &stdout, &stderr)
		if code != 0 || strings.Contains(stdout.String(), "! show version") || strings.Count(stdout.String(), `"canonical_name"`) < 2 {
			t.Fatalf("%s: exit=%d stderr=%q\nstdout:\n%s", format, code, stderr.String(), stdout.String())
		}
	}
	code, stdout, stderr := gatherRun(append(sets, "--set", "output.files.commands-jsonl=false"), "show version", "--no-daemon")
	if code != 0 || strings.Count(stdout, "! 127.0.0.") != 2 || strings.Contains(stdout, "! show version") {
		t.Fatalf("commands-jsonl false: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
}

// TestRunNoDaemonGatheredHaltedDevice: serial, halted at the first
// failure: the second device is never started, its record one of the
// job's, so it is a group of the block (not_started_halt), never today's
// line after the blocks.
func TestRunNoDaemonGatheredHaltedDevice(t *testing.T) {
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
	code, stdout, stderr := gatherRun(sets, "show bad", "--no-daemon", "--dispatch", "serial", "--halt-on-error-count", "1")
	if code == 0 || strings.Contains(stdout, "karvi: target=127.0.0.2") || !strings.Contains(stdout, "! 127.0.0.2 (1)\nkarvi: status=not_started_halt") {
		t.Fatalf("exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
}

// TestRunNoDaemonGatheredLive: --live is today's live view under the key
// (DECISIONS point 1): each device's session, never the gathered blocks.
func TestRunNoDaemonGatheredLive(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	code, stdout, stderr := gatherRun(sets, "show version", "--no-daemon", "--live")
	if code != 0 || strings.Contains(stdout, "! show version\n") || strings.Contains(stdout, "(2)\n") || !strings.Contains(stdout, "dev#show version\r\nok\r\n") {
		t.Fatalf("exit=%d stderr=%q\nstdout:\n%q", code, stderr, stdout)
	}
}

// TestRunNoDaemonGatheredBeforeTheStoresCloseFailure: the store's close
// failing after every record is written (failed-devices.txt cannot be
// put in place) follows the gathered blocks, as it follows today's
// records: the answers shown, then output_store_close_failed, exit 111.
func TestRunNoDaemonGatheredBeforeTheStoresCloseFailure(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	code, stdout, stderr := gatherRun(sets, "show close", "--no-daemon")
	if code != exitcode.ExitOutputFailure || stdout != gatheredOK("show close") || !strings.Contains(stderr, "output_store_close_failed: ") {
		t.Fatalf("exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
}

// TestRunNoDaemonGatheredBeforeTheJobsOwnFailure: a job whose own output
// fails after its records (output.max-job-bytes refuses the second
// device's record, the job's output_job_limit_exceeded) shows the gathered
// blocks of the records it kept, then the job's error, as today's display
// shows those records before it; the display is not dropped for the job's
// failure.
func TestRunNoDaemonGatheredBeforeTheJobsOwnFailure(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "output.max-command-bytes=1024", "--set", "output.spool-threshold-bytes=1024", "--set", "output.max-job-bytes=4096")
	code, stdout, stderr := gatherRun(sets, "show version", "--no-daemon")
	if code != exitcode.ExitOutputFailure || stdout != "! show version\n! 127.0.0.1 (1)\nok\n" || !strings.Contains(stderr, "output_job_limit_exceeded: ") {
		t.Fatalf("exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
}

// TestRunThroughTheDaemonGathered: through the daemon the client gathers
// what it follows and reads the answers back from the job's folder; job
// follow of the finished job gathers through the daemon, and from the
// folder once the daemon is gone. At the end a commands.jsonl that cannot
// be read is run_output_records_unreadable, exit 111, after the line
// "job ID ended exit=N; artifacts DIR" with the job's own exit, directly
// before the error: run with a link put at the name, job follow with the
// file's mode opened up; from the folder, a display whose standard output
// fails at the end says the same line before terminal_write_failed.
func TestRunThroughTheDaemonGathered(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var launch bytes.Buffer
	rt, err := ensureTestDaemon(ctx, globalsFor(sets), &launch)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (%s)", err, launch.String())
	}
	stopped := false
	defer func() {
		if !stopped {
			stopTestDaemon(t, socket, rt.MaxFrame)
		}
	}()
	code, stdout, stderr := gatherRun(sets, "show version")
	if code != 0 || stdout != gatheredOK("show version") {
		t.Fatalf("gathered: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	summary, dir := jobSummary(t, base)
	follow := func(gather string) (int, string, string) {
		var out, errOut bytes.Buffer
		args := append(append([]string{}, sets...), "--set", "display.run.gather="+gather, "job", "follow", summary.JobID)
		code := Main(args, strings.NewReader(""), &out, &errOut)
		return code, out.String(), errOut.String()
	}
	if code, stdout, stderr := follow("true"); code != 0 || stdout != gatheredOK("show version") {
		t.Fatalf("job follow: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	if code, stdout, stderr := follow("false"); code != 0 || strings.Count(stdout, "! 127.0.0.") != 2 {
		t.Fatalf("job follow, not gathered: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	commands := filepath.Join(dir, "commands.jsonl")
	if err := os.Chmod(commands, 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = follow("true")
	want := "job " + summary.JobID + " ended exit=0; artifacts " + dir + "\n"
	if code != exitcode.ExitOutputFailure || stdout != "" || !strings.Contains(stderr, want+"run_output_records_unreadable: "+commands+" has mode 0644") || strings.Contains(stderr, "continues") {
		t.Fatalf("job follow, mode 0644: exit=%d stdout=%q\nstderr=%q", code, stdout, stderr)
	}
	if err := os.Chmod(commands, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = gatherRun(sets, "show swap")
	summary, dir = jobSummary(t, base)
	want = "job " + summary.JobID + " ended exit=0; artifacts " + dir + "\n"
	if code != exitcode.ExitOutputFailure || stdout != "" || !strings.Contains(stderr, want+swappedAt(dir)) || strings.Count(stderr, "commands.jsonl") != 1 || strings.Contains(stderr, "continues") {
		t.Fatalf("a link at the name: exit=%d stdout=%q\nstderr=%q", code, stdout, stderr)
	}
	// The job is unaffected: its summary keeps its own exit.
	if summary.ExitCode != 0 {
		t.Fatalf("a link at the name: the job's exit %d, want 0", summary.ExitCode)
	}
	// crun through the daemon, its display failing at the end (a link put
	// at the name): the job ended, so its crun.after runs, as in process,
	// told the invocation's exit.
	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	hookOut := filepath.Join(base, "hook.out")
	hook := filepath.Join(base, "hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho \"exit=$KARVI_EXIT job=$KARVI_JOB_ID\" >"+hookOut+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	crunArgs := append(append([]string{}, sets...), "--set", "display.run.gather=true", "--set", `platform.cisco_ios.crun-commands=["show swap"]`, "--set", fmt.Sprintf("crun.after=%q", hook), "crun", "--cd="+filepath.Join(base, "crun"), "--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system")
	var crunOut, crunErr bytes.Buffer
	code = Main(crunArgs, strings.NewReader(""), &crunOut, &crunErr)
	summary, dir = jobSummary(t, base)
	ran, _ := os.ReadFile(hookOut)
	want = "job " + summary.JobID + " ended exit=0; artifacts " + dir + "\n"
	if code != exitcode.ExitOutputFailure || !strings.Contains(crunErr.String(), want+swappedAt(dir)) || string(ran) != fmt.Sprintf("exit=%d job=%s\n", exitcode.ExitOutputFailure, summary.JobID) {
		t.Fatalf("crun, its display failing at the end: exit=%d hook %q\nstderr=%q\nstdout=%q", code, ran, crunErr.String(), crunOut.String())
	}
	// From the folder, no daemon: a finished job gathered again (the file
	// put back).
	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := gatherRun(sets, "show version"); code != 0 {
		t.Fatalf("third run: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	summary, dir = jobSummary(t, base)
	stopTestDaemon(t, socket, rt.MaxFrame)
	stopped = true
	if code, stdout, stderr := follow("true"); code != 0 || stdout != gatheredOK("show version") {
		t.Fatalf("job follow from the folder: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	if code, stdout, stderr := follow("false"); code != 0 || strings.Count(stdout, "! 127.0.0.") != 2 || strings.Contains(stdout, "! show version\n") {
		t.Fatalf("job follow from the folder, not gathered: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	var errOut bytes.Buffer
	code = Main(append(append([]string{}, sets...), "--set", "display.run.gather=true", "job", "follow", summary.JobID), strings.NewReader(""), failWriter{}, &errOut)
	want = "job " + summary.JobID + " ended exit=0; artifacts " + dir + "\nterminal_write_failed: "
	if code != exitcode.ExitOutputFailure || !strings.Contains(errOut.String(), want) || strings.Contains(errOut.String(), "continues") {
		t.Fatalf("job follow from the folder, standard output failing: exit=%d stderr=%q", code, errOut.String())
	}
}

// swappedAt is the read-back's refusal of the link `show swap` put at the
// name of dir's commands.jsonl.
func swappedAt(dir string) string {
	return "run_output_records_unreadable: open " + filepath.Join(dir, "commands.jsonl") + ": too many levels of symbolic links"
}

// failWriter fails every write, a closed standard output.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write /dev/stdout: broken pipe") }
