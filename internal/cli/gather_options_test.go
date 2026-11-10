package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// The options --gather and --prefix (DECISIONS points 1, 3, 4, 6 and their
// "Settled at S6" bullets): on run, crun, a stream's lines, and command,
// each sets its key for the invocation through the lock-aware flag layer,
// --gather=false and --prefix=false setting it false; refused beside what
// leaves the display nothing to read back or nowhere to show it
// (gather_option_conflict), beside what line mode cannot take
// (prefix_option_conflict), and beside --live (live_option_conflict); the
// keys set true never refused. Every line here runs over the test
// runtime's fake transport (dryRunSets), never the host's configuration.

// optionLine is a line of word under sets: pre (the global options, --set
// among them), the word, opts, and one command for the test device.
func optionLine(sets, pre []string, word string, opts ...string) []string {
	args := append(append(append([]string{}, sets...), pre...), word)
	args = append(args, opts...)
	return append(args, "--transport", "system", "--target", "127.0.0.1", "--cmd", "show clock")
}

// mainOutputs runs Main over args and returns its exit and both streams.
func mainOutputs(args []string, stdin string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Main(args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// TestGatherAndPrefixRefusedBesideTheirOptions: each refusal before the
// read, on run, crun, and command as far as the word has the option,
// exit 4, the one line on stderr, nothing on stdout; and the order of
// the checks: --border with --noborder first, then --live's (the mode
// chosen, its own list first), then --gather's, then --prefix's, each
// naming the first conflict in its list (--format jsonl|json first), all
// before --of with --nof, the collection options' values, a --cf file's
// read, and the rehearsal flags' run_mode_conflict.
func TestGatherAndPrefixRefusedBesideTheirOptions(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	missing := filepath.Join(base, "no-such-commands-file")
	gather := func(what string) string {
		return "gather_option_conflict: --gather and " + what + " are mutually exclusive"
	}
	prefix := func(what string) string {
		return "prefix_option_conflict: --prefix and " + what + " are mutually exclusive"
	}
	live := func(what string) string {
		return "live_option_conflict: --live and " + what + " are mutually exclusive"
	}
	for _, tc := range []struct {
		word string
		opts []string
		want string
	}{
		// gather_option_conflict, in its list's order.
		{"run", []string{"--gather", "--format", "jsonl"}, gather("--format jsonl")},
		{"run", []string{"--gather", "--format", "json"}, gather("--format json")},
		{"run", []string{"--gather", "--echo"}, gather("--echo")},
		{"run", []string{"--gather", "--detach"}, gather("--detach")},
		{"run", []string{"--gather", "--follow=false"}, gather("--follow=false")},
		{"run", []string{"--gather", "--nof"}, gather("--nof")},
		{"crun", []string{"--gather", "--echo"}, gather("--echo")},
		{"crun", []string{"--ga", "--nof"}, gather("--nof")},
		{"command", []string{"--gather", "--format", "jsonl"}, gather("--format jsonl")},
		{"command", []string{"--gather", "--format", "json"}, gather("--format json")},
		{"command", []string{"--gather", "--echo"}, gather("--echo")},
		{"command", []string{"--gather", "--nof"}, gather("--nof")},
		// prefix_option_conflict, in its list's order.
		{"run", []string{"--prefix", "--format", "jsonl"}, prefix("--format jsonl")},
		{"run", []string{"--prefix", "--format", "json"}, prefix("--format json")},
		{"run", []string{"--prefix", "--border"}, prefix("--border")},
		{"run", []string{"--prefix", "--noborder"}, prefix("--noborder")},
		{"run", []string{"--prefix", "--detach"}, prefix("--detach")},
		{"run", []string{"--prefix", "--follow=false"}, prefix("--follow=false")},
		{"crun", []string{"--pre", "--border"}, prefix("--border")},
		{"command", []string{"--prefix", "--format", "json"}, prefix("--format json")},
		{"command", []string{"--prefix", "--border"}, prefix("--border")},
		{"command", []string{"--prefix", "--noborder"}, prefix("--noborder")},
		// live_option_conflict for both, checkLive first, ahead of
		// checkLiveRun's --no-daemon.
		{"command", []string{"--live", "--gather"}, live("--gather")},
		{"command", []string{"--live", "--prefix"}, live("--prefix")},
		{"run", []string{"--live", "--gather"}, live("--gather")},
		{"run", []string{"--no-daemon", "--live", "--prefix"}, live("--prefix")},
		{"run", []string{"--live", "--gather", "--echo"}, live("--echo")},
		{"run", []string{"--live", "--gather", "--nof"}, live("--gather")},
		// The order among the checks.
		{"run", []string{"--gather", "--border", "--noborder"}, "border_options_conflict: --border and --noborder are mutually exclusive"},
		{"command", []string{"--prefix", "--border", "--noborder"}, "border_options_conflict: --border and --noborder are mutually exclusive"},
		{"run", []string{"--gather", "--prefix", "--format", "jsonl"}, gather("--format jsonl")},
		{"run", []string{"--gather", "--prefix", "--border"}, prefix("--border")},
		{"run", []string{"--gather", "--format", "jsonl", "--echo"}, gather("--format jsonl")},
		{"run", []string{"--gather", "--echo", "--nof"}, gather("--echo")},
		{"run", []string{"--gather", "--detach", "--follow=false"}, gather("--detach")},
		{"run", []string{"--prefix", "--border", "--detach"}, prefix("--border")},
		{"run", []string{"--gather", "--nof", "--of"}, gather("--nof")},
		{"command", []string{"--gather", "--nof", "--of"}, gather("--nof")},
		{"run", []string{"--gather", "--dry-run", "--detach"}, gather("--detach")},
		{"run", []string{"--prefix", "--exercise", "--detach"}, prefix("--detach")},
		{"run", []string{"--gather", "--nof", "--exercise"}, gather("--nof")},
		{"run", []string{"--gather", "--echo", "--cd"}, gather("--echo")},
		{"run", []string{"--gather", "--echo", "--cf", missing}, gather("--echo")},
		{"command", []string{"--prefix", "--border", "--cf", missing}, prefix("--border")},
	} {
		t.Run(tc.word+"_"+strings.Join(tc.opts, "_"), func(t *testing.T) {
			code, stdout, stderr := mainOutputs(optionLine(sets, nil, tc.word, tc.opts...), "")
			if code != exitcode.ExitUsageError || stderr != tc.want+"\n" || stdout != "" {
				t.Fatalf("exit=%d stdout=%q\nstderr=%q\nwant  %q", code, stdout, stderr, tc.want+"\n")
			}
		})
	}
}

// TestGatherAndPrefixFalseNeverRefused: --gather=false and --prefix=false
// ask for today's display, so neither is refused beside any option the
// flag is refused beside: a run's dry run plans and exits 0, and beside
// --detach (which a rehearsal refuses) the next check, run_mode_conflict,
// is the one that answers.
func TestGatherAndPrefixFalseNeverRefused(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	for _, opts := range [][]string{
		{"--gather=false", "--format", "jsonl"},
		{"--gather=false", "--format", "json"},
		{"--gather=false", "--echo"},
		{"--gather=false", "--follow=false"},
		{"--gather=false", "--nof"},
		{"--prefix=false", "--format", "jsonl"},
		{"--prefix=false", "--border"},
		{"--prefix=false", "--noborder"},
		{"--prefix=false", "--follow=false"},
		{"--gather", "--gather=false", "--echo"},
	} {
		t.Run(strings.Join(opts, "_"), func(t *testing.T) {
			code, stdout, stderr := mainOutputs(optionLine(sets, nil, "run", append([]string{"--dry-run", "--no-daemon"}, opts...)...), "")
			if code != exitcode.ExitSuccess || stdout == "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
	for _, flag := range []string{"--gather=false", "--prefix=false"} {
		code, _, stderr := mainOutputs(optionLine(sets, nil, "run", flag, "--detach", "--no-daemon"), "")
		if want := "run_mode_conflict: --detach needs a daemon to leave the job with and cannot be combined with --no-daemon\n"; code != exitcode.ExitUsageError || stderr != want {
			t.Errorf("%s --detach: exit=%d stderr=%q, want %q", flag, code, stderr, want)
		}
	}
}

// TestGatherAndPrefixAccepted: what the points allow reaches past every
// check: a run's dry run plans and exits 0 with --prefix beside --echo or
// --nof, both options together, --gather beside a border option, either
// under --quiet (run's a global option, before the word) or an explicit
// --format text, under --dry-run itself; the
// keys set true by --set beside every option the flags are refused beside
// (the key shapes the text display alone); an exercise with either
// option.
func TestGatherAndPrefixAccepted(t *testing.T) {
	_, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	for _, tc := range []struct {
		pre  []string
		opts []string
	}{
		{nil, []string{"--prefix", "--echo"}},
		{nil, []string{"--prefix", "--nof"}},
		{nil, []string{"--gather", "--prefix"}},
		{nil, []string{"--gather", "--border"}},
		{nil, []string{"--gather", "--noborder"}},
		{[]string{"--quiet"}, []string{"--gather"}},
		{[]string{"--quiet"}, []string{"--prefix"}},
		{nil, []string{"--gather", "--format", "text"}},
		{nil, []string{"--prefix", "--format", "text"}},
		{[]string{"--set", "display.run.gather=true"}, []string{"--format", "jsonl"}},
		{[]string{"--set", "display.run.gather=true"}, []string{"--echo"}},
		{[]string{"--set", "display.run.gather=true"}, []string{"--nof"}},
		{[]string{"--set", "display.run.gather=true"}, []string{"--follow=false"}},
		{[]string{"--set", "display.run.gather=true", "--set", "output.persist-command=false"}, nil},
		{[]string{"--set", "display.run.gather=true", "--set", "output.files.commands-jsonl=false"}, nil},
		{[]string{"--set", "display.run.prefix=true"}, []string{"--format", "json"}},
		{[]string{"--set", "display.run.prefix=true"}, []string{"--border"}},
		{[]string{"--set", "display.run.prefix=true"}, []string{"--noborder"}},
		{[]string{"--set", "display.run.prefix=true", "--set", "display.run.gather=true"}, []string{"--echo", "--border"}},
	} {
		t.Run(strings.Join(append(append([]string{}, tc.pre...), tc.opts...), "_"), func(t *testing.T) {
			code, stdout, stderr := mainOutputs(optionLine(sets, tc.pre, "run", append([]string{"--dry-run", "--no-daemon"}, tc.opts...)...), "")
			if code != exitcode.ExitSuccess || stdout == "" || strings.Contains(stderr, "_conflict") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
	for _, flag := range []string{"--gather", "--prefix"} {
		code, stdout, stderr := mainOutputs(exerciseArgs(sets, flag), "")
		if code != exitcode.ExitSuccess || !strings.Contains(stdout, "\nstatus: exercise\n") {
			t.Errorf("--exercise %s: exit=%d stdout=%q stderr=%q", flag, code, stdout, stderr)
		}
	}
}

// TestCommandGatherAndPrefixAccepted: command, which has no rehearsal,
// runs over the fake device and exits 0 with what the points allow, and
// with its keys set true beside the options its flags are refused beside.
func TestCommandGatherAndPrefixAccepted(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	for _, tc := range []struct {
		pre  []string
		opts []string
	}{
		{nil, []string{"--prefix", "--echo"}},
		{nil, []string{"--prefix", "--nof"}},
		{nil, []string{"--gather", "--prefix"}},
		{nil, []string{"--gather", "--border"}},
		{nil, []string{"--gather", "--noborder"}},
		{nil, []string{"--gather", "--quiet"}},
		{nil, []string{"--gather", "--format", "text"}},
		{nil, []string{"--gather=false", "--format", "jsonl"}},
		{nil, []string{"--gather=false", "--nof"}},
		{nil, []string{"--prefix=false", "--border"}},
		{[]string{"--set", "display.command.gather=true"}, []string{"--format", "jsonl"}},
		{[]string{"--set", "display.command.gather=true"}, []string{"--echo", "--nof"}},
		{[]string{"--set", "display.command.prefix=true"}, []string{"--border"}},
	} {
		t.Run(strings.Join(append(append([]string{}, tc.pre...), tc.opts...), "_"), func(t *testing.T) {
			code, stdout, stderr := mainOutputs(optionLine(sets, tc.pre, "command", tc.opts...), "")
			if code != exitcode.ExitSuccess || stdout == "" || strings.Contains(stderr, "_conflict") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

// TestGatherAndPrefixSetTheirKeys: the option sets its key for the
// invocation in the lock-aware flag layer, sourced to the option by its
// long name, true or, by its =false form, false: display.run.gather and
// display.run.prefix on run and crun (and so on a stream's lines, run's
// handler), display.command.gather and display.command.prefix on command;
// the last spelling on the line wins; --set, applied after the layer,
// beats the option; an option not given writes nothing.
func TestGatherAndPrefixSetTheirKeys(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	setAt := fmt.Sprintf("--set[%d]", len(sets)/2+1)
	type value struct {
		key    string
		value  bool
		source string
	}
	for _, tc := range []struct {
		pre  []string
		word string
		opts []string
		want []value
	}{
		{nil, "run", []string{"--gather"}, []value{{"display.run.gather", true, "--gather"}, {"display.run.prefix", false, "<builtin>"}, {"display.command.gather", false, "<builtin>"}}},
		{nil, "run", []string{"--gather=false"}, []value{{"display.run.gather", false, "--gather"}}},
		{nil, "run", []string{"--prefix"}, []value{{"display.run.prefix", true, "--prefix"}, {"display.run.gather", false, "<builtin>"}, {"display.command.prefix", false, "<builtin>"}}},
		{nil, "run", []string{"--prefix=false"}, []value{{"display.run.prefix", false, "--prefix"}}},
		{nil, "run", []string{"--ga", "--pre"}, []value{{"display.run.gather", true, "--gather"}, {"display.run.prefix", true, "--prefix"}}},
		{nil, "run", []string{"--gather", "--gather=false"}, []value{{"display.run.gather", false, "--gather"}}},
		{nil, "crun", []string{"--gather", "--prefix=false"}, []value{{"display.run.gather", true, "--gather"}, {"display.run.prefix", false, "--prefix"}}},
		{nil, "command", []string{"--gather", "--prefix"}, []value{{"display.command.gather", true, "--gather"}, {"display.command.prefix", true, "--prefix"}, {"display.run.gather", false, "<builtin>"}, {"display.run.prefix", false, "<builtin>"}}},
		{nil, "command", []string{"--gather=false", "--prefix=false"}, []value{{"display.command.gather", false, "--gather"}, {"display.command.prefix", false, "--prefix"}}},
		{[]string{"--set", "display.run.gather=true"}, "run", []string{"--gather=false"}, []value{{"display.run.gather", true, setAt}}},
		{[]string{"--set", "display.run.gather=false"}, "run", []string{"--gather"}, []value{{"display.run.gather", false, setAt}}},
		{[]string{"--set", "display.command.prefix=false"}, "command", []string{"--prefix"}, []value{{"display.command.prefix", false, setAt}}},
		{[]string{"--set", "display.run.gather=true"}, "run", nil, []value{{"display.run.gather", true, setAt}}},
	} {
		t.Run(tc.word+"_"+strings.Join(append(append([]string{}, tc.pre...), tc.opts...), "_"), func(t *testing.T) {
			inv, err := Parse(optionLine(sets, tc.pre, tc.word, tc.opts...))
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			common, _, code, ok := workRead(inv, inv.Path, &stderr)
			if !ok {
				t.Fatalf("read: exit=%d %s", code, stderr.String())
			}
			for _, w := range tc.want {
				v, _ := common.Config.Provenance(w.key)
				if v.Data != w.value || v.Source.String() != w.source {
					t.Errorf("%s = %v at %s, want %v at %s", w.key, v.Data, v.Source, w.value, w.source)
				}
			}
		})
	}
}

// TestGatherRefusedWithoutTheJobsRecords: --gather reads the job's
// commands.jsonl back at its end, so a configuration that keeps none is
// refused after the read, gather_option_conflict, exit 4, naming the key
// that decided it (output.persist-command first, as the files' one rule
// reads it, then output.files.commands-jsonl) and where it was set: a
// --set, a file's line, a KARVI__ variable; "; --of" the remedy for the
// first where --of would take effect (a file's or a variable's, not a
// --set's, which beats --of: TestGatherRecordsOfHintOnlyWhereItWorks
// for a lock). On run, crun, and command, under --dry-run and --exercise as
// without, the exercise's own check first; never before a daemon or a
// device is reached. --of makes persistence true, so --gather --of under a
// file's persist-command false plans; --nof is the option's refusal,
// before the read, whatever --set says; --prefix needs no file, and the
// key set true is never refused (TestGatherAndPrefixAccepted).
func TestGatherRefusedWithoutTheJobsRecords(t *testing.T) {
	base, _, socket := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	setAt := fmt.Sprintf("--set[%d]", len(sets)/2+1)
	persistFile := filepath.Join(base, "persist-off.toml")
	jsonlFile := filepath.Join(base, "jsonl-off.toml")
	for path, text := range map[string]string{persistFile: "[output]\npersist-command = false\n", jsonlFile: "[output.files]\ncommands-jsonl = false\n"} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	persistOff := []string{"--set", "output.persist-command=false"}
	jsonlOff := []string{"--set", "output.files.commands-jsonl=false"}
	persist := func(at string, hint bool) string {
		line := "gather_option_conflict: --gather and output.persist-command = false at " + at + " are mutually exclusive"
		if hint {
			line += "; --of"
		}
		return line
	}
	jsonl := func(at string) string {
		return "gather_option_conflict: --gather and output.files.commands-jsonl = false at " + at + " are mutually exclusive"
	}
	for _, tc := range []struct {
		env  string
		pre  []string
		word string
		opts []string
		want string
	}{
		{"", persistOff, "run", []string{"--gather"}, persist(setAt, false)},
		{"", persistOff, "run", []string{"--gather", "--of"}, persist(setAt, false)},
		{"", []string{"--config", persistFile}, "run", []string{"--gather"}, persist(persistFile+":2", true)},
		{"false", nil, "run", []string{"--gather"}, persist("KARVI__OUTPUT__PERSIST_COMMAND", true)},
		{"", jsonlOff, "run", []string{"--gather"}, jsonl(setAt)},
		{"", []string{"--config", jsonlFile}, "crun", []string{"--gather"}, jsonl(jsonlFile + ":2")},
		{"", jsonlOff, "run", []string{"--gather", "--of"}, jsonl(setAt)},
		{"", persistOff, "command", []string{"--gather"}, persist(setAt, false)},
		{"", jsonlOff, "command", []string{"--gather"}, jsonl(setAt)},
		{"", persistOff, "run", []string{"--gather", "--dry-run", "--no-daemon"}, persist(setAt, false)},
		{"", jsonlOff, "run", []string{"--gather", "--exercise"}, jsonl(setAt)},
		{"", persistOff, "run", []string{"--gather", "--exercise"}, "run_mode_conflict: --exercise with output.persist-command = false: the exercise report is a file in the job folder; --of"},
		{"", []string{"--set", "output.persist-command=true"}, "run", []string{"--gather", "--nof"}, "gather_option_conflict: --gather and --nof are mutually exclusive"},
	} {
		t.Run(tc.word+"_"+strings.Join(append(append([]string{}, tc.pre...), tc.opts...), "_")+tc.env, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("KARVI__OUTPUT__PERSIST_COMMAND", tc.env)
			}
			code, stdout, stderr := mainOutputs(optionLine(sets, tc.pre, tc.word, tc.opts...), "")
			if code != exitcode.ExitUsageError || stderr != tc.want+"\n" || stdout != "" {
				t.Fatalf("exit=%d stdout=%q\nstderr=%q\nwant  %q", code, stdout, stderr, tc.want+"\n")
			}
			if _, err := os.Stat(socket); err == nil {
				t.Fatalf("a daemon was reached at %s", socket)
			}
		})
	}
	for _, tc := range []struct {
		pre  []string
		opts []string
	}{
		{[]string{"--config", persistFile}, []string{"--gather", "--of"}},
		{persistOff, []string{"--prefix"}},
		{persistOff, []string{"--gather=false"}},
		{jsonlOff, []string{"--prefix", "--gather=false"}},
	} {
		code, stdout, stderr := mainOutputs(optionLine(sets, tc.pre, "run", append([]string{"--dry-run", "--no-daemon"}, tc.opts...)...), "")
		if code != exitcode.ExitSuccess || stdout == "" {
			t.Errorf("%q %q: exit=%d stdout=%q stderr=%q", tc.pre, tc.opts, code, stdout, stderr)
		}
	}
}

// TestGatherAndPrefixOnStreamLines: a stream's lines take both options
// through run's table (an abbreviation as on a command line), and the
// draft's job is judged when --go runs it through run's handler: refused
// there, unprefixed, as --live beside --echo is; a later --gather=false
// line turns the option off and the job runs.
func TestGatherAndPrefixOnStreamLines(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := gatherSets(t, base)
	for _, tc := range []struct {
		lines []string
		want  string
	}{
		{[]string{"--gather", "--format jsonl"}, "gather_option_conflict: --gather and --format jsonl are mutually exclusive"},
		{[]string{"--ga", "--echo"}, "gather_option_conflict: --gather and --echo are mutually exclusive"},
		{[]string{"--gather", "--follow=false"}, "gather_option_conflict: --gather and --follow=false are mutually exclusive"},
		{[]string{"--prefix", "--border"}, "prefix_option_conflict: --prefix and --border are mutually exclusive"},
		{[]string{"--pre", "--format json"}, "prefix_option_conflict: --prefix and --format json are mutually exclusive"},
		{[]string{"--live", "--prefix"}, "live_option_conflict: --live and --prefix are mutually exclusive"},
	} {
		t.Run(strings.Join(tc.lines, "_"), func(t *testing.T) {
			in := "--no-daemon\n--target 127.0.0.1\n--transport system\n" + strings.Join(tc.lines, "\n") + "\nshow version\n--go\n--end\n"
			code, stdout, stderr := mainOutputs(append(append([]string{}, sets...), "stream"), in)
			if code != exitcode.ExitUsageError || stderr != tc.want+"\n" || stdout != "" {
				t.Fatalf("exit=%d stdout=%q\nstderr=%q\nwant  %q", code, stdout, stderr, tc.want+"\n")
			}
		})
	}
	in := "--no-daemon\n--target 127.0.0.1\n--transport system\n--gather\n--echo\n--gather=false\nshow version\n--go\n--end\n"
	code, stdout, stderr := mainOutputs(append(append([]string{}, sets...), "stream"), in)
	if code != exitcode.ExitSuccess || !strings.HasPrefix(stdout, "! 127.0.0.1 [127.0.0.1]") || strings.Contains(stderr, "_conflict") || strings.Contains(stderr, "dropped") {
		t.Fatalf("--gather then --gather=false: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
}

// TestRunAndCommandShownThroughTheOptions: end to end over the fake
// device, the options shape the display as their keys do: run --gather in
// process shows the two devices' alike answers as one group, crun's
// too, its collection line after; --gather=false under a site's
// display.run.gather true (a KARVI__ variable, below the option's layer)
// shows a header per device; --prefix streams each line under its device's name, and
// beside --gather under the group's folded names; command --gather and
// --prefix under command's keys.
func TestRunAndCommandShownThroughTheOptions(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := append(gatherSets(t, base), "--set", "display.command.footer=")
	main := func(args ...string) (int, string, string) {
		return mainOutputs(append(append([]string{}, sets...), args...), "")
	}
	two := []string{"--format", "text", "--target", "127.0.0.1", "--target", "127.0.0.2", "--transport", "system", "--cmd", "show version"}
	one := []string{"--format", "text", "--transport", "system", "127.0.0.1", "show", "version"}
	crun := filepath.Join(base, "crun")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{append([]string{"run", "--no-daemon", "--gather"}, two...), gatheredOK("show version")},
		{append([]string{"crun", "--no-daemon", "--gather", "--cd=" + crun}, two...), gatheredOK("show version") + "! collection=" + crun + " replace=2 keep=0\n"},
		{append([]string{"run", "--no-daemon", "--prefix", "--dispatch", "serial"}, two...), "127.0.0.1: ok\n127.0.0.2: ok\n"},
		{append([]string{"run", "--no-daemon", "--gather", "--prefix"}, two...), "! show version\n127.0.0.[1-2]: ok\n"},
		{append([]string{"command", "--gather"}, one...), "! show version\n! 127.0.0.1 (1)\nok\n"},
		{append([]string{"command", "--prefix"}, one...), "127.0.0.1: ok\n"},
	} {
		t.Run(strings.Join(tc.args[:3], "_"), func(t *testing.T) {
			if code, stdout, stderr := main(tc.args...); code != 0 || stdout != tc.want {
				t.Fatalf("exit=%d stderr=%q\nstdout:\n%s\nwant:\n%s", code, stderr, stdout, tc.want)
			}
		})
	}
	t.Setenv("KARVI__DISPLAY__RUN__GATHER", "true")
	code, stdout, stderr := main(append([]string{"run", "--no-daemon", "--gather=false"}, two...)...)
	if code != 0 || strings.Count(stdout, "! 127.0.0.") != 2 || strings.Contains(stdout, "! show version") {
		t.Fatalf("--gather=false under a site's key: exit=%d stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
}

// TestHelpNamesGatherAndPrefix: run's, crun's, and command's help name
// both options with their =false forms among the display's options, and
// each --live line names both among what --live is refused beside.
func TestHelpNamesGatherAndPrefix(t *testing.T) {
	for _, path := range []string{"run", "crun", "command"} {
		text := lookupCommand(path).help()
		for _, want := range []string{"\n  --gather, --gather=false ", "\n  --prefix, --prefix=false "} {
			if !strings.Contains(text, want) {
				t.Errorf("%s help lacks %q", path, want)
			}
		}
		i := strings.Index(text, "\n  --live ")
		if i < 0 {
			t.Fatalf("%s help has no --live line", path)
		}
		entry := text[i+1:]
		entry = entry[:strings.Index(entry[1:], "\n  --")+1]
		for _, want := range []string{"--gather", "--prefix"} {
			if !strings.Contains(entry, want) {
				t.Errorf("%s help's --live line lacks %s:\n%s", path, want, entry)
			}
		}
	}
}

// TestGatherAndPrefixCodesRegistered: the two codes are usage failures,
// exit 4, beside live_option_conflict.
func TestGatherAndPrefixCodesRegistered(t *testing.T) {
	for _, code := range []string{"gather_option_conflict", "prefix_option_conflict"} {
		e, ok := errorcodes.Lookup(code)
		if !ok || e.Category != "usage" || e.Exit != exitcode.ExitUsageError || e.Retryable {
			t.Errorf("%s: %+v (registered %v)", code, e, ok)
		}
	}
}

// TestGatherRecordsOfHintOnlyWhereItWorks: the refusal of --gather where
// output.persist-command is false ends "; --of" only where --of would
// take effect: the key set by a file or a KARVI__ variable and not
// locked; after a --set of the key, which beats --of, or under a site's
// lock, which refuses it, the message names the key and its source alone
// (settled at S6, gather-2026-11).
func TestGatherRecordsOfHintOnlyWhereItWorks(t *testing.T) {
	inv, err := Parse([]string{"run", "--all", "--gather", "show", "clock"})
	if err != nil {
		t.Fatal(err)
	}
	file := configload.SourceRef{Layer: "global", Path: "/etc/karvi/config.toml", Line: 12}
	env := configload.SourceRef{Layer: "environment", Path: "KARVI__OUTPUT__PERSIST_COMMAND"}
	set := configload.SourceRef{Layer: "set", Path: "--set[1]"}
	lock := []configload.LockDecl{{Pattern: "output.persist-command", Source: file}}
	for _, tc := range []struct {
		name  string
		src   configload.SourceRef
		locks []configload.LockDecl
		want  string
	}{
		{"file", file, nil, "/etc/karvi/config.toml:12 are mutually exclusive; --of"},
		{"environment", env, nil, "KARVI__OUTPUT__PERSIST_COMMAND are mutually exclusive; --of"},
		{"set", set, nil, "--set[1] are mutually exclusive"},
		{"file locked", file, lock, "/etc/karvi/config.toml:12 are mutually exclusive"},
		{"environment locked", env, lock, "KARVI__OUTPUT__PERSIST_COMMAND are mutually exclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := configload.Snapshot{Values: map[string]configload.Value{"output.persist-command": {Data: false, Source: tc.src}}, Locks: tc.locks}
			var stderr bytes.Buffer
			if code, ok := checkGatherRecords(inv, cfg, false, &stderr); ok || code != exitcode.ExitUsageError {
				t.Fatalf("ok=%v code=%d", ok, code)
			}
			if want := "gather_option_conflict: --gather and output.persist-command = false at " + tc.want + "\n"; stderr.String() != want {
				t.Fatalf("stderr=%q\nwant  %q", stderr.String(), want)
			}
		})
	}
}

// TestLiveBesideGatherAndPrefixFalse: --gather=false and --prefix=false
// ask for today's display, so --live beside them passes every check of
// the display's options on command and run, as beside --echo=false.
func TestLiveBesideGatherAndPrefixFalse(t *testing.T) {
	for _, args := range [][]string{
		{"command", "--live", "--gather=false", "--target", "r1", "--cmd", "x"},
		{"command", "--live", "--prefix=false", "--target", "r1", "--cmd", "x"},
		{"run", "--no-daemon", "--live", "--gather=false", "--target", "r1", "--cmd", "x"},
		{"run", "--no-daemon", "--live", "--prefix=false", "--target", "r1", "--cmd", "x"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			inv, err := Parse(args)
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			checks := []func(*Invocation, io.Writer) (int, bool){checkBorders, checkLive, checkGather, checkPrefix}
			if args[0] == "run" {
				// Run's own (command has no --no-daemon).
				checks = append(checks, checkLiveRun)
			}
			for _, check := range checks {
				if _, ok := check(inv, &stderr); !ok {
					t.Fatalf("refused: %q", stderr.String())
				}
			}
		})
	}
}

// TestGatherAndPrefixFirstConflictInListOrder: each refusal names the
// first conflict in its own list's order (--gather: --format, --echo,
// --detach, --follow=false, --nof; --prefix: --format, --border,
// --noborder, --detach, --follow=false), whatever order the line gives.
func TestGatherAndPrefixFirstConflictInListOrder(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	for _, tc := range []struct {
		opts []string
		want string
	}{
		{[]string{"--gather", "--detach", "--echo"}, "gather_option_conflict: --gather and --echo are mutually exclusive"},
		{[]string{"--gather", "--nof", "--follow=false"}, "gather_option_conflict: --gather and --follow=false are mutually exclusive"},
		{[]string{"--prefix", "--follow=false", "--detach"}, "prefix_option_conflict: --prefix and --detach are mutually exclusive"},
	} {
		t.Run(strings.Join(tc.opts, "_"), func(t *testing.T) {
			code, stdout, stderr := mainOutputs(optionLine(sets, nil, "run", tc.opts...), "")
			if code != exitcode.ExitUsageError || stderr != tc.want+"\n" || stdout != "" {
				t.Fatalf("exit=%d stdout=%q\nstderr=%q\nwant  %q", code, stdout, stderr, tc.want+"\n")
			}
		})
	}
}

// TestGatherAndPrefixBesideOptionsGivenFalse: an option the flag is
// refused beside, given =false, is not given: --gather and --prefix plan
// beside --echo=false, --nof=false, --detach=false, --border=false, and
// --noborder=false.
func TestGatherAndPrefixBesideOptionsGivenFalse(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	for _, opts := range [][]string{
		{"--gather", "--echo=false"},
		{"--gather", "--nof=false"},
		{"--gather", "--detach=false"},
		{"--prefix", "--border=false"},
		{"--prefix", "--noborder=false"},
	} {
		t.Run(strings.Join(opts, "_"), func(t *testing.T) {
			code, stdout, stderr := mainOutputs(optionLine(sets, nil, "run", append([]string{"--dry-run", "--no-daemon"}, opts...)...), "")
			if code != exitcode.ExitSuccess || stdout == "" || strings.Contains(stderr, "_conflict") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

// TestGatherRecordsNamePersistFirst: with output.persist-command and
// output.files.commands-jsonl both false, the refusal names
// persist-command, the key the files' one rule reads first, on run and
// command (a --set's, so no "; --of").
func TestGatherRecordsNamePersistFirst(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	setAt := fmt.Sprintf("--set[%d]", len(sets)/2+1)
	pre := []string{"--set", "output.persist-command=false", "--set", "output.files.commands-jsonl=false"}
	want := "gather_option_conflict: --gather and output.persist-command = false at " + setAt + " are mutually exclusive\n"
	for _, word := range []string{"run", "command"} {
		code, stdout, stderr := mainOutputs(optionLine(sets, pre, word, "--gather"), "")
		if code != exitcode.ExitUsageError || stderr != want || stdout != "" {
			t.Errorf("%s: exit=%d stdout=%q\nstderr=%q\nwant  %q", word, code, stdout, stderr, want)
		}
	}
}

// TestCommandChecksInRunsOrder: command's display checks come in run's
// order: --border with --noborder first, then --live's, then --gather's,
// then --prefix's, each before --of with --nof.
func TestCommandChecksInRunsOrder(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	for _, tc := range []struct {
		opts []string
		want string
	}{
		{[]string{"--gather", "--prefix", "--format", "json"}, "gather_option_conflict: --gather and --format json are mutually exclusive"},
		{[]string{"--live", "--gather", "--echo"}, "live_option_conflict: --live and --echo are mutually exclusive"},
		{[]string{"--prefix", "--border", "--of", "--nof"}, "prefix_option_conflict: --prefix and --border are mutually exclusive"},
		{[]string{"--gather", "--border", "--noborder", "--echo"}, "border_options_conflict: --border and --noborder are mutually exclusive"},
	} {
		t.Run(strings.Join(tc.opts, "_"), func(t *testing.T) {
			code, stdout, stderr := mainOutputs(optionLine(sets, nil, "command", tc.opts...), "")
			if code != exitcode.ExitUsageError || stderr != tc.want+"\n" || stdout != "" {
				t.Fatalf("exit=%d stdout=%q\nstderr=%q\nwant  %q", code, stdout, stderr, tc.want+"\n")
			}
		})
	}
}

// TestGatherRecordsBeforeTheTargets: run's refusal of --gather without
// commands.jsonl comes with the exercise's, right after the read, before
// the target inputs are read: a missing --tf file is not reached
// (target_source_missing, exit 5).
func TestGatherRecordsBeforeTheTargets(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	setAt := fmt.Sprintf("--set[%d]", len(sets)/2+1)
	missing := filepath.Join(base, "no-such-targets-file")
	pre := []string{"--set", "output.persist-command=false"}
	want := "gather_option_conflict: --gather and output.persist-command = false at " + setAt + " are mutually exclusive\n"
	code, stdout, stderr := mainOutputs(optionLine(sets, pre, "run", "--gather", "--tf", missing), "")
	if code != exitcode.ExitUsageError || stderr != want || stdout != "" {
		t.Fatalf("exit=%d stdout=%q\nstderr=%q\nwant  %q", code, stdout, stderr, want)
	}
}
