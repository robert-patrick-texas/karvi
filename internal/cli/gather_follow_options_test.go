package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/testpty"
	"github.com/robert-patrick-texas/karvi/records"
)

// job follow's --gather and --prefix (DECISIONS points 1, 4, 6 and their
// "Settled at S6" bullets, gather-2026-11): each sets run's key,
// display.run.gather or display.run.prefix, for the invocation, through
// the lock-aware flag layer, so every job, a command's too, is shown by
// run's keys (settled at S3c); each is refused, as on run, beside what
// job follow has of its list (--gather: --format jsonl|json and --echo;
// --prefix: --format jsonl|json, --border, and --noborder), before the
// job ID's check and the read; and --gather of a job that kept no
// commands.jsonl is refused, gather_option_conflict, exit 4, the one line
// on standard error, from the job's folder and through the daemon alike,
// the key set true without the option keeping today's display.

// notKept is job follow --gather's refusal of job id, which kept no
// commands.jsonl, key being false when it ran.
func notKept(id, key string) string {
	return "gather_option_conflict: --gather needs the job's commands.jsonl; job " + id + " kept none (" + key + " was false)\n"
}

// followLine is job follow of id under sets: pre (the global options,
// --set, --config, and --quiet among them), then opts, then the ID.
func followLine(sets, pre []string, id string, opts ...string) []string {
	args := append(append(append([]string{}, sets...), pre...), "job", "follow")
	return append(append(args, opts...), id)
}

// jsonlJobID is the job ID of a run's --format jsonl display, whose last
// line is the job's summary.
func jsonlJobID(t *testing.T, stdout string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	var s records.Summary
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &s); err != nil || s.JobID == "" {
		t.Fatalf("no summary line with a job ID: %v\n%s", err, stdout)
	}
	return s.JobID
}

// TestJobFollowGatherAndPrefixRefused: each refusal job follow has of the
// options' lists, exit 4, the one line on stderr, nothing on stdout, each
// naming the first conflict in its own list's order (--format jsonl|json
// first), --gather's before --prefix's, by any unique prefix; both before
// the job ID's check (a malformed ID beside --gather --echo is gather's
// refusal) and before the read (a configuration that does not read is not
// reached). The guards: without the options the same lines are refused
// for the ID and for the configuration.
func TestJobFollowGatherAndPrefixRefused(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	bad := filepath.Join(base, "bad.toml")
	if err := os.WriteFile(bad, []byte("[dispatch]\nnope = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unread := []string{"--config", bad}
	gather := func(what string) string {
		return "gather_option_conflict: --gather and " + what + " are mutually exclusive\n"
	}
	prefix := func(what string) string {
		return "prefix_option_conflict: --prefix and " + what + " are mutually exclusive\n"
	}
	for _, tc := range []struct {
		pre  []string
		id   string
		opts []string
		want string
	}{
		{nil, fixtureJobID, []string{"--gather", "--format", "jsonl"}, gather("--format jsonl")},
		{nil, fixtureJobID, []string{"--gather", "--format", "json"}, gather("--format json")},
		{nil, fixtureJobID, []string{"--gather", "--echo"}, gather("--echo")},
		{nil, fixtureJobID, []string{"--g", "--echo"}, gather("--echo")},
		{nil, fixtureJobID, []string{"--prefix", "--format", "jsonl"}, prefix("--format jsonl")},
		{nil, fixtureJobID, []string{"--prefix", "--format", "json"}, prefix("--format json")},
		{nil, fixtureJobID, []string{"--prefix", "--border"}, prefix("--border")},
		{nil, fixtureJobID, []string{"--prefix", "--noborder"}, prefix("--noborder")},
		{nil, fixtureJobID, []string{"--p", "--noborder"}, prefix("--noborder")},
		// The first in each list's order, whatever the line's.
		{nil, fixtureJobID, []string{"--gather", "--echo", "--format", "json"}, gather("--format json")},
		{nil, fixtureJobID, []string{"--prefix", "--noborder", "--format", "json"}, prefix("--format json")},
		// --gather's before --prefix's.
		{nil, fixtureJobID, []string{"--prefix", "--gather", "--format", "jsonl"}, gather("--format jsonl")},
		{nil, fixtureJobID, []string{"--prefix", "--border", "--gather", "--echo"}, gather("--echo")},
		{nil, fixtureJobID, []string{"--gather", "--prefix", "--border"}, prefix("--border")},
		// Before the job ID's check.
		{nil, "not-a-job-id", []string{"--gather", "--echo"}, gather("--echo")},
		{nil, "not-a-job-id", []string{"--prefix", "--format", "json"}, prefix("--format json")},
		// Before the read.
		{unread, fixtureJobID, []string{"--gather", "--echo"}, gather("--echo")},
		{unread, fixtureJobID, []string{"--prefix", "--border"}, prefix("--border")},
		{unread, "not-a-job-id", []string{"--gather", "--format", "jsonl"}, gather("--format jsonl")},
	} {
		t.Run(strings.Join(append(append([]string{tc.id}, tc.pre...), tc.opts...), "_"), func(t *testing.T) {
			code, stdout, stderr := mainOutputs(followLine(sets, tc.pre, tc.id, tc.opts...), "")
			if code != exitcode.ExitUsageError || stderr != tc.want || stdout != "" {
				t.Fatalf("exit=%d stdout=%q\nstderr=%q\nwant  %q", code, stdout, stderr, tc.want)
			}
		})
	}
	code, stdout, stderr := mainOutputs(followLine(sets, nil, "not-a-job-id", "--echo", "--border"), "")
	if code != exitcode.ExitJobRejected || !strings.HasPrefix(stderr, "job_request_malformed: ") || stdout != "" {
		t.Errorf("a malformed ID without the options: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = mainOutputs(followLine(sets, unread, fixtureJobID, "--echo", "--format", "json"), "")
	if code != exitcode.ExitConfigValidationError || !strings.HasPrefix(stderr, "config_unknown_key: ") || stdout != "" {
		t.Errorf("a configuration that does not read, without the options: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

// TestJobFollowGatherAndPrefixAccepted: what the points allow reaches
// the follow of a finished job's folder and exits with the job's exit:
// --gather=false and --prefix=false beside every option the flags are
// refused beside (they ask for today's display), the flag given and then
// turned off, --format text, --gather beside a border option, --prefix
// beside --echo, both together, an option the flags are refused beside
// given false, either under --quiet, and the keys set true beside every
// option the flags are refused beside (the key shapes the text display
// alone).
func TestJobFollowGatherAndPrefixAccepted(t *testing.T) {
	_, g, _ := daemonTestRuntime(t)
	finishedDirectory(t, g, fixtureJobID, records.Summary{FinalStatus: "complete", ExitCode: 0, ExitName: "ExitSuccess"})
	for _, tc := range []struct {
		sets  []string
		quiet bool
		opts  []string
	}{
		{nil, false, []string{"--gather=false", "--format", "jsonl"}},
		{nil, false, []string{"--gather=false", "--format", "json"}},
		{nil, false, []string{"--gather=false", "--echo"}},
		{nil, false, []string{"--prefix=false", "--format", "jsonl"}},
		{nil, false, []string{"--prefix=false", "--border"}},
		{nil, false, []string{"--prefix=false", "--noborder"}},
		{nil, false, []string{"--gather", "--gather=false", "--echo"}},
		{nil, false, []string{"--prefix", "--prefix=false", "--border"}},
		{nil, false, []string{"--gather", "--format", "text"}},
		{nil, false, []string{"--prefix", "--format", "text"}},
		{nil, false, []string{"--gather", "--border"}},
		{nil, false, []string{"--gather", "--noborder"}},
		{nil, false, []string{"--prefix", "--echo"}},
		{nil, false, []string{"--gather", "--prefix"}},
		{nil, false, []string{"--gather", "--echo=false"}},
		{nil, false, []string{"--prefix", "--border=false", "--noborder=false"}},
		{nil, true, []string{"--gather"}},
		{nil, true, []string{"--prefix"}},
		{[]string{"display.run.gather=true"}, false, []string{"--format", "jsonl"}},
		{[]string{"display.run.gather=true"}, false, []string{"--echo"}},
		{[]string{"display.run.prefix=true"}, false, []string{"--format", "json"}},
		{[]string{"display.run.prefix=true"}, false, []string{"--border"}},
		{[]string{"display.run.prefix=true"}, false, []string{"--noborder"}},
	} {
		t.Run(strings.Join(append(append([]string{}, tc.sets...), tc.opts...), "_"), func(t *testing.T) {
			g := g
			g.sets = append(append([]string{}, g.sets...), tc.sets...)
			g.quiet = tc.quiet
			var stdout, stderr bytes.Buffer
			code := jobFollow(context.Background(), followInvocation(t, g, append(append([]string{}, tc.opts...), fixtureJobID)...), app.IO{Stdout: &stdout, Stderr: &stderr})
			if code != exitcode.ExitSuccess || strings.Contains(stderr.String(), "_conflict") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

// TestJobFollowGatherAndPrefixSetTheirKeys: job follow's options set run's
// keys, display.run.gather and display.run.prefix, for the invocation in
// the flag layer, sourced to the option by its long name, true or, by the
// =false form, false (never command's keys: job follow shows every job by
// run's); the last spelling wins; --set, applied after the layer, beats
// the option; an option not given writes nothing. The read is jobFollow's
// own (the invocation's shown snapshot); the job is unknown here, which
// comes after the read. A site's lock refuses either form at the read
// (internal/configload, TestGatherAndPrefixOptionsLock: the same keys
// written by the same options).
func TestJobFollowGatherAndPrefixSetTheirKeys(t *testing.T) {
	_, g, _ := daemonTestRuntime(t)
	setAt := fmt.Sprintf("--set[%d]", len(g.sets)+1)
	type value struct {
		key    string
		value  bool
		source string
	}
	for _, tc := range []struct {
		sets []string
		opts []string
		want []value
	}{
		{nil, []string{"--gather"}, []value{{"display.run.gather", true, "--gather"}, {"display.run.prefix", false, "<builtin>"}, {"display.command.gather", false, "<builtin>"}}},
		{nil, []string{"--gather=false"}, []value{{"display.run.gather", false, "--gather"}}},
		{nil, []string{"--prefix"}, []value{{"display.run.prefix", true, "--prefix"}, {"display.run.gather", false, "<builtin>"}, {"display.command.prefix", false, "<builtin>"}}},
		{nil, []string{"--prefix=false"}, []value{{"display.run.prefix", false, "--prefix"}}},
		{nil, []string{"--g", "--p"}, []value{{"display.run.gather", true, "--gather"}, {"display.run.prefix", true, "--prefix"}}},
		{nil, []string{"--gather", "--gather=false"}, []value{{"display.run.gather", false, "--gather"}}},
		{nil, []string{"--prefix=false", "--prefix"}, []value{{"display.run.prefix", true, "--prefix"}}},
		{[]string{"display.run.gather=false"}, []string{"--gather"}, []value{{"display.run.gather", false, setAt}}},
		{[]string{"display.run.gather=true"}, []string{"--gather=false"}, []value{{"display.run.gather", true, setAt}}},
		{[]string{"display.run.prefix=false"}, []string{"--prefix"}, []value{{"display.run.prefix", false, setAt}}},
		{nil, nil, []value{{"display.run.gather", false, "<builtin>"}, {"display.run.prefix", false, "<builtin>"}}},
	} {
		t.Run(strings.Join(append(append([]string{}, tc.sets...), tc.opts...), "_"), func(t *testing.T) {
			g := g
			g.sets = append(append([]string{}, g.sets...), tc.sets...)
			var shown configload.Snapshot
			g.shown = &shown
			var stdout, stderr bytes.Buffer
			if code := jobFollow(context.Background(), followInvocation(t, g, append(append([]string{}, tc.opts...), fixtureJobID)...), app.IO{Stdout: &stdout, Stderr: &stderr}); code != exitcode.ExitJobRejected || !strings.HasPrefix(stderr.String(), "job_unknown: ") {
				t.Fatalf("exit=%d stderr=%q", code, stderr.String())
			}
			for _, w := range tc.want {
				v, _ := shown.Provenance(w.key)
				if v.Data != w.value || v.Source.String() != w.source {
					t.Errorf("%s = %v at %s, want %v at %s", w.key, v.Data, v.Source, w.value, w.source)
				}
			}
		})
	}
}

// TestJobFollowGatherRefusedFromTheFolder: from the folder, a job whose
// summary names no commands.jsonl kept none (output.files.commands-jsonl
// false): job follow --gather is refused at the first readable summary,
// exit 4, the one line on stderr naming the job and the key, nothing on
// stdout, before the not-kept warning and the footer; a running job's at
// once, not at its end; under --quiet too (an error), and whatever the
// key's final value (--set display.run.gather=false beside --gather). The
// key set true without the option, and --gather=false beside the key
// true, keep today's warning and footer.
func TestJobFollowGatherRefusedFromTheFolder(t *testing.T) {
	_, g, _ := daemonTestRuntime(t)
	boards := t.TempDir()
	g.sets = append(g.sets, "scoreboards=\""+boards+"\"", "watch.refresh=\"250ms\"", "watch.stale-after=\"30s\"")
	accepted := records.Summary{AcceptedSummary: records.AcceptedSummary{SchemaVersion: records.JobSchemaVersion, JobID: fixtureJobID, Devices: []records.SummaryDevice{{Name: "r1"}}, Paths: map[string]string{"summary": "summary.json"}}}
	final := accepted
	final.FinalStatus, final.ExitCode, final.ExitName = "complete", 0, "ExitSuccess"
	dir := finishedDirectory(t, g, fixtureJobID, final)
	if err := os.Remove(filepath.Join(dir, "commands.jsonl")); err != nil {
		t.Fatal(err)
	}
	follow := func(ctx context.Context, sets []string, quiet bool, opts ...string) (int, string, string) {
		g := g
		g.sets = append(append([]string{}, g.sets...), sets...)
		g.quiet = quiet
		var stdout, stderr bytes.Buffer
		code := jobFollow(ctx, followInvocation(t, g, append(append([]string{}, opts...), fixtureJobID)...), app.IO{Stdout: &stdout, Stderr: &stderr})
		return code, stdout.String(), stderr.String()
	}
	want := notKept(fixtureJobID, "output.files.commands-jsonl")
	for _, tc := range []struct {
		sets  []string
		quiet bool
		opts  []string
	}{
		{nil, false, []string{"--gather"}},
		{nil, false, []string{"--g", "--border"}},
		{nil, false, []string{"--gather", "--prefix"}},
		{nil, true, []string{"--gather"}},
		{[]string{"display.run.gather=true"}, false, []string{"--gather"}},
		{[]string{"display.run.gather=false"}, false, []string{"--gather"}},
	} {
		t.Run(strings.Join(append(append([]string{}, tc.sets...), tc.opts...), "_"), func(t *testing.T) {
			if code, stdout, stderr := follow(context.Background(), tc.sets, tc.quiet, tc.opts...); code != exitcode.ExitUsageError || stderr != want || stdout != "" {
				t.Fatalf("exit=%d stdout=%q\nstderr=%q\nwant  %q", code, stdout, stderr, want)
			}
		})
	}
	const warning = "warning: the job's records are not kept (output.files.commands-jsonl is false)\n"
	for _, tc := range []struct {
		sets []string
		opts []string
	}{
		{[]string{"display.run.gather=true"}, nil},
		{[]string{"display.run.gather=true"}, []string{"--gather=false"}},
		{nil, []string{"--prefix"}},
	} {
		code, stdout, stderr := follow(context.Background(), tc.sets, false, tc.opts...)
		if code != 0 || stderr != warning || !strings.Contains(stdout, " artifacts="+dir+"\n") || strings.Count(stdout, "\n") != 1 {
			t.Errorf("%q %q: exit=%d stdout=%q stderr=%q", tc.sets, tc.opts, code, stdout, stderr)
		}
	}
	// A running job, its summary the acceptance's and its scoreboard
	// current: refused at once, its end never waited for.
	raw, _ := json.Marshal(accepted)
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), raw, 0o640); err != nil {
		t.Fatal(err)
	}
	writeBoard(t, boards, fixtureJobID, "running", time.Now())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if code, stdout, stderr := follow(ctx, nil, false, "--gather"); code != exitcode.ExitUsageError || stderr != want || stdout != "" {
		t.Fatalf("running: exit=%d stdout=%q\nstderr=%q\nwant  %q", code, stdout, stderr, want)
	}
}

// TestJobFollowShownThroughTheOptions: from the folder (no daemon), the
// options shape a finished job's display as their keys do: --gather the
// two devices' alike answers as one group, --prefix each line under its
// device's name, both together the group's folded names; --set beats the
// option (--gather under --set display.run.gather=false is today's
// display, a header per device), and --gather=false turns a site's true
// (a KARVI__ variable, below the option's layer) off; a command's job is
// shown by run's keys, so job follow --gather and --prefix shape it too.
func TestJobFollowShownThroughTheOptions(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.command.footer=")
	main := func(args ...string) (int, string, string) {
		return mainOutputs(append(append([]string{}, sets...), args...), "")
	}
	if code, stdout, stderr := main("run", "--no-daemon", "--dispatch", "serial", "--format", "jsonl", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version"); code != 0 {
		t.Fatalf("run: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	summary, _ := jobSummary(t, base)
	today := func(stdout string) bool {
		return strings.Count(stdout, "! 127.0.0.") == 2 && !strings.Contains(stdout, "! show version")
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"job", "follow", "--gather", summary.JobID}, gatheredOK("show version")},
		{[]string{"job", "follow", "--prefix", summary.JobID}, "127.0.0.1: ok\n127.0.0.2: ok\n"},
		{[]string{"job", "follow", "--gather", "--prefix", summary.JobID}, "! show version\n127.0.0.[1-2]: ok\n"},
	} {
		t.Run(strings.Join(tc.args[2:len(tc.args)-1], "_"), func(t *testing.T) {
			if code, stdout, stderr := main(tc.args...); code != 0 || stdout != tc.want {
				t.Fatalf("exit=%d stderr=%q\nstdout:\n%s\nwant:\n%s", code, stderr, stdout, tc.want)
			}
		})
	}
	if code, stdout, stderr := main("--set", "display.run.gather=false", "job", "follow", "--gather", summary.JobID); code != 0 || !today(stdout) {
		t.Errorf("--gather under --set false: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	t.Run("site's true", func(t *testing.T) {
		t.Setenv("KARVI__DISPLAY__RUN__GATHER", "true")
		if code, stdout, stderr := main("job", "follow", summary.JobID); code != 0 || stdout != gatheredOK("show version") {
			t.Fatalf("the site's true: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
		}
		if code, stdout, stderr := main("job", "follow", "--gather=false", summary.JobID); code != 0 || !today(stdout) {
			t.Fatalf("--gather=false under the site's true: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
		}
	})
	if err := os.RemoveAll(filepath.Join(base, "jobs")); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := main("command", "--format", "text", "--transport", "system", "127.0.0.1", "show", "version"); code != 0 {
		t.Fatalf("command: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	summary, _ = jobSummary(t, base)
	if code, stdout, stderr := main("job", "follow", "--gather", summary.JobID); code != 0 || stdout != "! show version\n! 127.0.0.1 (1)\nok\n" {
		t.Errorf("a command's job, --gather: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	if code, stdout, stderr := main("job", "follow", "--prefix", summary.JobID); code != 0 || stdout != "127.0.0.1: ok\n" {
		t.Errorf("a command's job, --prefix: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
}

// holdCommand gives gatherSets' fake device under base a command, `show
// held`, that marks it started (the file started) and holds its answer
// until the file release exists; the test's end writes release, so a held
// job is let go however the test ends.
func holdCommand(t *testing.T, base string) (release, started string) {
	t.Helper()
	device := filepath.Join(base, "fake-device")
	raw, err := os.ReadFile(device)
	if err != nil {
		t.Fatal(err)
	}
	release, started = filepath.Join(base, "release"), filepath.Join(base, "started")
	held := "    'show held') : >\"" + started + "\"; while [ ! -e \"" + release + "\" ]; do sleep 0.05; done; printf '%s\\r\\nok\\r\\ndev#' \"$line\";;\n"
	if err := os.WriteFile(device, []byte(strings.Replace(string(raw), "    exit) exit 0;;\n", "    exit) exit 0;;\n"+held, 1)), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })
	return release, started
}

// TestJobFollowGatherThroughTheDaemon: over a daemon the real launcher
// started and the fake device, job follow --gather and --prefix shape a
// kept job's display; --gather of a job that kept no commands.jsonl is
// refused at the follow's start, exit 4, the one line on stderr (no
// admission warning, no not-kept line, no "continues in the daemon"),
// nothing on stdout, under --quiet too: a job detached with
// output.files.commands-jsonl false, refused while it runs (the fake
// device holds its answer until the test lets it go) and after its end,
// the job itself unaffected (it completes, exit 0); and a job run with
// --nof, which has no folder (output.persist-command false). The key set
// true without the option, and --gather=false beside it, keep today's
// display and not-kept line, the line naming the key that was false
// (output.persist-command for the --nof job, which has no folder); a
// plain follow of either job still shows it with the job's own exit.
func TestJobFollowGatherThroughTheDaemon(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	release, _ := holdCommand(t, base)
	// A follow that is not refused while the job runs (it would wait for
	// the job's end) fails after 10 seconds rather than at the session's
	// own timeout; a refusal comes at once, never waiting.
	watchdog := time.AfterFunc(10*time.Second, func() { _ = os.WriteFile(release, nil, 0o600) })
	defer watchdog.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var launch bytes.Buffer
	rt, err := ensureTestDaemon(ctx, globalsFor(sets), &launch)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (%s)", err, launch.String())
	}
	defer stopTestDaemon(t, socket, rt.MaxFrame)
	main := func(args ...string) (int, string, string) {
		return mainOutputs(append(append([]string{}, sets...), args...), "")
	}
	run := func(pre []string, opts ...string) string {
		t.Helper()
		args := append(append(append([]string{}, pre...), "run", "--target", "127.0.0.1"), opts...)
		code, stdout, stderr := main(append(args, "--transport", "system")...)
		if code != 0 {
			t.Fatalf("run %q: exit=%d stderr=%q stdout=%q", opts, code, stderr, stdout)
		}
		return stdout
	}

	// A kept job, shown through the options.
	kept := jsonlJobID(t, run(nil, "--dispatch", "serial", "--format", "jsonl", "--target", "127.0.0.2", "--cmd", "show version"))
	for _, tc := range []struct {
		opts []string
		want string
	}{
		{[]string{"--gather"}, gatheredOK("show version")},
		{[]string{"--prefix"}, "127.0.0.1: ok\n127.0.0.2: ok\n"},
		{[]string{"--gather", "--prefix"}, "! show version\n127.0.0.[1-2]: ok\n"},
	} {
		if code, stdout, stderr := main(append(append([]string{"job", "follow"}, tc.opts...), kept)...); code != 0 || stdout != tc.want {
			t.Errorf("kept, %q: exit=%d stderr=%q\nstdout:\n%s\nwant:\n%s", tc.opts, code, stderr, stdout, tc.want)
		}
	}

	refused := func(what string, id, key string) {
		t.Helper()
		want := notKept(id, key)
		for _, pre := range [][]string{nil, {"--quiet"}, {"--set", "display.run.gather=false"}} {
			if code, stdout, stderr := main(append(append(append([]string{}, pre...), "job", "follow", "--gather"), id)...); code != exitcode.ExitUsageError || stderr != want || stdout != "" {
				t.Errorf("%s %q: exit=%d stdout=%q\nstderr=%q\nwant  %q", what, pre, code, stdout, stderr, want)
			}
		}
	}
	unrefused := func(what string, id, key string) {
		t.Helper()
		line := "are not kept (" + key + " is false)"
		for _, args := range [][]string{
			{"--set", "display.run.gather=true", "job", "follow", id},
			{"--set", "display.run.gather=true", "job", "follow", "--gather=false", id},
		} {
			if code, stdout, stderr := main(args...); code != 0 || strings.Contains(stderr, "_conflict") || !strings.Contains(stderr, line) || strings.Contains(stdout, "! show") {
				t.Errorf("%s %q: exit=%d stdout=%q stderr=%q", what, args, code, stdout, stderr)
			}
		}
		if code, stdout, stderr := main("job", "follow", id); code != 0 || strings.Contains(stderr, "_conflict") || !strings.Contains(stderr, line) {
			t.Errorf("%s, a plain follow: exit=%d stdout=%q stderr=%q", what, code, stdout, stderr)
		}
	}

	// A detached job that keeps no commands.jsonl: refused while it runs
	// and after; the job completes unaffected.
	out := run([]string{"--set", "output.files.commands-jsonl=false"}, "--detach", "--cmd", "show held")
	running := strings.TrimPrefix(strings.Split(out, "\n")[0], "job_id: ")
	refused("running, commands-jsonl false", running, "output.files.commands-jsonl")
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var s records.Summary
	if err := json.Unmarshal([]byte(waitSummary(t, base, running)), &s); err != nil || s.FinalStatus != "complete" || s.ExitCode != 0 {
		t.Fatalf("the refused follow's job: %v final %q exit %d", err, s.FinalStatus, s.ExitCode)
	}
	refused("ended, commands-jsonl false", running, "output.files.commands-jsonl")
	unrefused("commands-jsonl false", running, "output.files.commands-jsonl")

	// A job run with --nof: no folder.
	nof := jsonlJobID(t, run(nil, "--nof", "--format", "jsonl", "--cmd", "show version"))
	refused("--nof", nof, "output.persist-command")
	unrefused("--nof", nof, "output.persist-command")
}

// TestJobFollowHelpNamesGatherAndPrefix: job's help gives follow's usage
// with --gather and --prefix, every usage line within the width the
// text's other lines keep (78), and its prose names both among the
// options that mean what they mean for run, run's keys for every job, and
// the refusal of --gather for a job that kept no commands.jsonl;
// gather_option_conflict's registry text names that case.
func TestJobFollowHelpNamesGatherAndPrefix(t *testing.T) {
	text := lookupCommand("job follow").help()
	usage := text[:strings.Index(text, "\n\n")]
	i := strings.Index(usage, "karvi job follow ")
	j := strings.Index(usage, "karvi job cancel ")
	if i < 0 || j < i {
		t.Fatalf("no follow usage:\n%s", usage)
	}
	if follow := usage[i:j]; !strings.Contains(follow, "[--gather]") || !strings.Contains(follow, "[--prefix]") {
		t.Errorf("follow's usage lacks the options:\n%s", follow)
	}
	for _, line := range strings.Split(text, "\n") {
		if len(line) > 78 {
			t.Errorf("a help line of %d columns: %q", len(line), line)
		}
	}
	prose := strings.Join(strings.Fields(text[strings.Index(text, "\nfollow renders"):strings.Index(text, "\ncancel stops")]), " ")
	for _, want := range []string{"--gather, and --prefix mean what they mean for run", "display.run", "a command's", "--gather of a job that kept no commands.jsonl", "is refused, gather_option_conflict"} {
		if !strings.Contains(prose, want) {
			t.Errorf("follow's prose lacks %q:\n%s", want, prose)
		}
	}
	if e, _ := errorcodes.Lookup("gather_option_conflict"); !strings.Contains(e.Cause, "`job follow --gather` of a job that kept no `commands.jsonl`") {
		t.Errorf("gather_option_conflict's text: %s", e.Cause)
	}
}

// TestJobFollowGatherKeptFileRemoved: a kept job (its summary names its
// commands.jsonl) whose file was removed after its end, followed through
// the daemon: under --gather, and under the key alone, the follow gathers
// and the read-back at the end fails, run_output_records_unreadable, exit
// 111, naming the gone file, after the line "job ID ended exit=0;
// artifacts DIR" (the job ended, unaffected); nothing on standard output,
// the read-back failing at its open before the first group (and the
// footer, never written after it, is off here). Never "kept none", never
// today's display (DECISIONS point 4, the third "Settled at S6"). From
// the folder, the daemon gone, the same code and exit (as before).
func TestJobFollowGatherKeptFileRemoved(t *testing.T) {
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
	main := func(args ...string) (int, string, string) {
		return mainOutputs(append(append([]string{}, sets...), args...), "")
	}
	code, out, errs := main("run", "--target", "127.0.0.1", "--format", "jsonl", "--transport", "system", "--cmd", "show version")
	if code != 0 {
		t.Fatalf("run: exit=%d stderr=%q", code, errs)
	}
	id := jsonlJobID(t, out)
	summary, dir := jobSummary(t, base)
	if summary.JobID != id {
		t.Fatalf("summary %s, job %s", summary.JobID, id)
	}
	commands := filepath.Join(dir, "commands.jsonl")
	if err := os.Remove(commands); err != nil {
		t.Fatal(err)
	}
	unreadable := "run_output_records_unreadable: open " + commands + ": no such file or directory\n"
	for _, args := range [][]string{
		{"job", "follow", "--gather", id},
		{"--set", "display.run.gather=true", "job", "follow", id},
	} {
		code, stdout, stderr := main(args...)
		if want := "job " + id + " ended exit=0; artifacts " + dir + "\n" + unreadable; code != exitcode.ExitOutputFailure || stdout != "" || !strings.HasSuffix(stderr, want) || strings.Contains(stderr, "continues") {
			t.Errorf("through the daemon %q: exit=%d stdout=%q\nstderr=%q\nwant the end %q", args, code, stdout, stderr, want)
		}
	}
	stopTestDaemon(t, socket, rt.MaxFrame)
	stopped = true
	if code, stdout, stderr := main("job", "follow", "--gather", id); code != exitcode.ExitOutputFailure || stdout != "" || stderr != unreadable {
		t.Errorf("from the folder: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

// lateInterrupt is an invocation's context interrupted just after the
// client first looks: its first Err reads nil, every later one
// context.Canceled; Done stays nil (Background's), so nothing waits on it.
type lateInterrupt struct {
	context.Context
	reads atomic.Int32
}

func (c *lateInterrupt) Err() error {
	if c.reads.Add(1) > 1 {
		return context.Canceled
	}
	return nil
}

// TestJobFollowFolderRefusalBeforeTheInterrupt: from the folder (no
// daemon), job follow --gather of a job that kept no commands.jsonl
// reports the refusal alone, exit 4, even when an interrupt lands as the
// folder's follow returns it: jobFollow judges the refusal before the
// interrupt, as its daemon path does, never "job ID continues" and exit
// 113. The context's one quiet read is the probe's (no daemon); the
// folder's follow returns the refusal without reading it.
func TestJobFollowFolderRefusalBeforeTheInterrupt(t *testing.T) {
	_, g, _ := daemonTestRuntime(t)
	g.sets = append(g.sets, "scoreboards=\""+t.TempDir()+"\"")
	final := records.Summary{AcceptedSummary: records.AcceptedSummary{SchemaVersion: records.JobSchemaVersion, JobID: fixtureJobID, Devices: []records.SummaryDevice{{Name: "r1"}}, Paths: map[string]string{"summary": "summary.json"}}}
	final.FinalStatus, final.ExitCode, final.ExitName = "complete", 0, "ExitSuccess"
	dir := finishedDirectory(t, g, fixtureJobID, final)
	if err := os.Remove(filepath.Join(dir, "commands.jsonl")); err != nil {
		t.Fatal(err)
	}
	ctx := &lateInterrupt{Context: context.Background()}
	var stdout, stderr bytes.Buffer
	code := jobFollow(ctx, followInvocation(t, g, "--gather", fixtureJobID), app.IO{Stdout: &stdout, Stderr: &stderr})
	if want := notKept(fixtureJobID, "output.files.commands-jsonl"); code != exitcode.ExitUsageError || stderr.String() != want || stdout.String() != "" {
		t.Fatalf("exit=%d stdout=%q\nstderr=%q\nwant  %q", code, stdout.String(), stderr.String(), want)
	}
}

// interruptAtText is a terminal's standard error (Terminal, so the display
// takes it for one: its progress line is drawn there) that calls
// interrupt once, when it has been sent text: the progress line's first
// draw, the display gathering, as a Ctrl-C at that moment; String is all
// it was sent.
type interruptAtText struct {
	f         *os.File
	text      string
	interrupt func()
	mu        sync.Mutex
	seen      bytes.Buffer
	done      bool
}

func (w *interruptAtText) Terminal() *os.File { return w.f }

func (w *interruptAtText) Write(p []byte) (int, error) {
	n, err := w.f.Write(p)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seen.Write(p)
	if !w.done && strings.Contains(w.seen.String(), w.text) {
		w.done = true
		w.interrupt()
	}
	return n, err
}

func (w *interruptAtText) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seen.String()
}

// atText is an interruptAtText on a new terminal, interrupting once it has
// been sent text; wait waits for what the terminal was sent.
func atText(t *testing.T, text string, interrupt func()) (w *interruptAtText, wait func()) {
	t.Helper()
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	return &interruptAtText{f: slave, text: text, interrupt: interrupt}, func() { term.Wait(t, slave) }
}

// atGathering is atText at the progress line's first draw: the display
// gathering.
func atGathering(t *testing.T, interrupt func()) (w *interruptAtText, wait func()) {
	t.Helper()
	return atText(t, "\rgather: done=", interrupt)
}

// interruptedAt is the line of a follow of job id interrupted in state,
// its folder dir, naming --gather or not.
func interruptedAt(id, dir, state string, gather bool) string {
	again := "karvi job follow " + id
	if gather {
		again = "karvi job follow --gather " + id
	}
	return "interrupted: job " + id + " " + state + "; artifacts " + dir + "; run \"" + again + "\" again or read " + dir + "/summary.json when it ends\n"
}

// cancelOnFirstWrite is a standard output that cancels the invocation's
// context at its first write.
type cancelOnFirstWrite struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelOnFirstWrite) Write(p []byte) (int, error) {
	w.cancel()
	return w.Buffer.Write(p)
}

// TestInterruptedFollowNamesGather: over a daemon the real launcher
// started and the fake device holding its answer, the operator's Ctrl-C
// (SIGINT to this process, Main's interrupt context taking it) while the
// job runs ends the follow with the line naming the way back (DECISIONS
// point 4, the fourth "Settled at S6"): run --gather through the daemon,
// interrupted once its display gathers (the progress line drawn), names
// `karvi job follow --gather ID`; a run not gathering names the plain
// follow; job follow --gather of the detached job interrupted gathering
// names --gather, and job follow of it not gathering (the key false),
// interrupted at the start's admission warning, the plain follow; job
// cancel --follow, interrupted as it starts waiting, names the plain
// follow, the key set true or not (it shows no display). Each job is let
// go after, and its end waited for. A site's lock on the key keeps the
// line plain (internal/app, TestInterruptedLineNamesGather: the lock
// cannot be placed from here).
func TestInterruptedFollowNamesGather(t *testing.T) {
	guardInterrupt(t)
	base, _, socket := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	release, started := holdCommand(t, base)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var launch bytes.Buffer
	rt, err := ensureTestDaemon(ctx, globalsFor(sets), &launch)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (%s)", err, launch.String())
	}
	defer stopTestDaemon(t, socket, rt.MaxFrame)
	sigint := func() { _ = syscall.Kill(os.Getpid(), syscall.SIGINT) }
	run := func(opts ...string) []string {
		args := append(append(append([]string{}, sets...), "run", "--format", "text", "--target", "127.0.0.1", "--transport", "system"), opts...)
		return append(args, "--cmd", "show held")
	}
	// letGo releases the held job, waits for its end, and clears the
	// marks and the jobs for the next.
	letGo := func(id string) {
		t.Helper()
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		waitSummary(t, base, id)
		for _, path := range []string{release, started, filepath.Join(base, "jobs")} {
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
		}
	}
	detach := func() (string, string) {
		t.Helper()
		code, stdout, stderr := mainOutputs(run("--detach"), "")
		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		if code != 0 || len(lines) != 2 {
			t.Fatalf("detach: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		return strings.TrimPrefix(lines[0], "job_id: "), strings.TrimPrefix(lines[1], "artifact_dir: ")
	}

	w, wait := atGathering(t, sigint)
	var stdout bytes.Buffer
	code := Main(run("--gather"), strings.NewReader(""), &stdout, w)
	wait()
	summary, dir := jobSummary(t, base)
	if want := interruptedAt(summary.JobID, dir, "continues in the daemon", true); code != exitcode.ExitCancelled || !strings.Contains(w.String(), want) {
		t.Errorf("run --gather: exit=%d stderr=%q\nwant %q", code, w.String(), want)
	}
	letGo(summary.JobID)

	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(started); err == nil {
				sigint()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	code, _, stderr := mainOutputs(run(), "")
	summary, dir = jobSummary(t, base)
	if want := interruptedAt(summary.JobID, dir, "continues in the daemon", false); code != exitcode.ExitCancelled || !strings.Contains(stderr, want) {
		t.Errorf("run: exit=%d stderr=%q\nwant %q", code, stderr, want)
	}
	letGo(summary.JobID)

	id, dir := detach()
	w, wait = atGathering(t, sigint)
	code = Main(append(append([]string{}, sets...), "job", "follow", "--gather", id), strings.NewReader(""), &stdout, w)
	wait()
	if want := interruptedAt(id, dir, "continues in the daemon", true); code != exitcode.ExitCancelled || !strings.Contains(w.String(), want) {
		t.Errorf("job follow --gather: exit=%d stderr=%q\nwant %q", code, w.String(), want)
	}
	letGo(id)

	id, dir = detach()
	w, wait = atText(t, insecureWarning, sigint)
	code = Main(append(append([]string{}, sets...), "job", "follow", id), strings.NewReader(""), &stdout, w)
	wait()
	if want := interruptedAt(id, dir, "continues in the daemon", false); code != exitcode.ExitCancelled || !strings.Contains(w.String(), want) {
		t.Errorf("job follow, not gathering: exit=%d stderr=%q\nwant %q", code, w.String(), want)
	}
	letGo(id)

	for _, key := range []string{"display.run.gather=false", "display.run.gather=true"} {
		id, dir := detach()
		inv, err := Parse([]string{"job", "cancel", "--follow", id})
		if err != nil {
			t.Fatal(err)
		}
		inv.Global = globalsFor(append(append([]string{}, sets...), "--set", key))
		cctx, ccancel := context.WithCancel(context.Background())
		out := &cancelOnFirstWrite{cancel: ccancel}
		var errOut bytes.Buffer
		code := jobCancel(cctx, inv, app.IO{Stdout: out, Stderr: &errOut})
		ccancel()
		if want := interruptedAt(id, dir, "continues cancelling in the daemon", false); code != exitcode.ExitCancelled || errOut.String() != want {
			t.Errorf("job cancel --follow, %s: exit=%d stdout=%q stderr=%q\nwant %q", key, code, out.String(), errOut.String(), want)
		}
		letGo(id)
	}
}

// TestInterruptedFolderFollowNamesGather: from the folder (no daemon),
// job follow of a running job whose display gathers, by --gather or by
// the key alone, interrupted once it gathers (the progress line drawn),
// ends with the line naming `karvi job follow --gather ID`, the job
// continuing, exit 113; job follow of a running job that keeps no
// commands.jsonl, not gathering, interrupted at its not-kept warning,
// names the plain follow.
func TestInterruptedFolderFollowNamesGather(t *testing.T) {
	_, g, _ := daemonTestRuntime(t)
	boards := t.TempDir()
	g.sets = append(g.sets, "scoreboards=\""+boards+"\"", "watch.refresh=\"250ms\"", "watch.stale-after=\"30s\"", "display.color=never")
	accepted := records.Summary{AcceptedSummary: records.AcceptedSummary{SchemaVersion: records.JobSchemaVersion, JobID: fixtureJobID, Devices: []records.SummaryDevice{{Name: "r1"}}}}
	dir := finishedDirectory(t, g, fixtureJobID, accepted)
	writeBoard(t, boards, fixtureJobID, "running", time.Now())
	for _, tc := range []struct {
		sets []string
		opts []string
	}{
		{nil, []string{"--gather"}},
		{[]string{"display.run.gather=true"}, nil},
	} {
		g := g
		g.sets = append(append([]string{}, g.sets...), tc.sets...)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		w, wait := atGathering(t, cancel)
		var stdout bytes.Buffer
		code := jobFollow(ctx, followInvocation(t, g, append(append([]string{}, tc.opts...), fixtureJobID)...), app.IO{Stdout: &stdout, Stderr: w})
		cancel()
		wait()
		if want := interruptedAt(fixtureJobID, dir, "continues", true); code != exitcode.ExitCancelled || !strings.Contains(w.String(), want) || stdout.Len() != 0 {
			t.Errorf("%q %q: exit=%d stdout=%q stderr=%q\nwant %q", tc.sets, tc.opts, code, stdout.String(), w.String(), want)
		}
	}

	const notKeptID = "260915-170000-01"
	accepted.JobID, accepted.Paths = notKeptID, map[string]string{"summary": "summary.json"}
	dir = finishedDirectory(t, g, notKeptID, accepted)
	if err := os.Remove(filepath.Join(dir, "commands.jsonl")); err != nil {
		t.Fatal(err)
	}
	writeBoard(t, boards, notKeptID, "running", time.Now())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w, wait := atText(t, "the job's records are not kept", cancel)
	var stdout bytes.Buffer
	code := jobFollow(ctx, followInvocation(t, g, notKeptID), app.IO{Stdout: &stdout, Stderr: w})
	cancel()
	wait()
	if want := interruptedAt(notKeptID, dir, "continues", false); code != exitcode.ExitCancelled || !strings.Contains(w.String(), want) || stdout.Len() != 0 {
		t.Errorf("not gathering: exit=%d stdout=%q stderr=%q\nwant %q", code, stdout.String(), w.String(), want)
	}
}
