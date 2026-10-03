package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestCrunWordParses covers the crun word at the parser: crun is a word
// sharing run's options, --cd=PATH among them (run and command take it
// too), abbreviated cr, needing no command text; a bare --cd is refused by
// the handler; c stays ambiguous.
func TestCrunWordParses(t *testing.T) {
	inv, err := Parse([]string{"crun", "--all"})
	if err != nil || inv.Path != "crun" || len(inv.Commands) != 0 || len(inv.Targets) != 1 {
		t.Fatalf("crun --all: %v %+v", err, inv)
	}
	inv, err = Parse([]string{"cr", "--target", "r1", "--cd=/srv/karvi/crun", "--cmd", "show version"})
	if err != nil || inv.Path != "crun" || inv.String(optCd) != "/srv/karvi/crun" || len(inv.Commands) != 1 {
		t.Fatalf("cr with --cd=PATH: %v %+v", err, inv)
	}
	if inv, err := Parse([]string{"run", "--all", "--cd=/x", "show", "version"}); err != nil || inv.String(optCd) != "/x" {
		t.Fatalf("run --cd=PATH: %v", err)
	}
	if inv, err := Parse([]string{"command", "--cd=/x", "r1", "show", "version"}); err != nil || inv.String(optCd) != "/x" {
		t.Fatalf("command --cd=PATH: %v", err)
	}
	if _, err := Parse([]string{"run", "--all"}); errorcodes.Of(err) != "cli_command_text_missing" {
		t.Fatalf("run still needs its text: %v", err)
	}
	if _, err := Parse([]string{"c", "--all"}); errorcodes.Of(err) != "cli_command_ambiguous" {
		t.Fatalf("c: %v", err)
	}
	for _, c := range commandTable {
		if c.path != "crun" {
			continue
		}
		if len(c.options) != len(runOptions) {
			t.Fatalf("crun's options are run's: %d vs %d", len(c.options), len(runOptions))
		}
	}
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{{"crun", "--all", "--cd", "--no-daemon"}, {"run", "--all", "--cd", "--no-daemon", "--cmd", "show clock"}, {"command", "--cd", "--target", "r1", "show", "clock"}} {
		stderr.Reset()
		if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 4 || !strings.HasPrefix(stderr.String(), "cli_option_value_missing: ") || !strings.Contains(stderr.String(), "--cd=PATH") {
			t.Fatalf("%q, a bare --cd: exit=%d stderr=%q", args, got, stderr.String())
		}
	}
	if got := Main([]string{"crun", "--help"}, strings.NewReader(""), &stdout, &stderr); got != 0 || !strings.Contains(stdout.String(), "karvi crun [target inputs] [options]\n") || !strings.Contains(stdout.String(), "--cd=PATH") {
		t.Fatalf("crun --help: exit=%d\n%s", got, stdout.String())
	}
}

// TestCrunDryRunListsPerPlatform covers the platform lists from the
// command line: a crun with no command shows each device its
// platform's list and the collection directory in the dry run, refuses a
// platform without a list, and with --cmd sends the operator's list; the
// plan of a crun writes no output.NAME.txt.
func TestCrunDryRunListsPerPlatform(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	args := func(extra ...string) []string {
		a := append([]string{}, sets...)
		// --platform generic: a direct target runs as the shipped default,
		// cisco_iosxe, which has a list; the
		// test is about the generic table's list and its absence.
		a = append(a, "crun", "--dry-run", "--no-daemon", "--target", "127.0.0.1", "--transport", "system", "--platform", "generic")
		return append(a, extra...)
	}
	var stdout, stderr bytes.Buffer
	if got := Main(args(), strings.NewReader(""), &stdout, &stderr); got == 0 || !strings.Contains(stderr.String(), "crun_platform_commands_missing") {
		t.Fatalf("generic without a list: exit=%d stderr=%q", got, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	list := `--set=platform.generic.crun-commands=["show version", "show clock"]`
	if got := Main(append([]string{list}, args("--cd=configs")...), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	out := stdout.String()
	wd, _ := os.Getwd()
	for _, want := range []string{"outcome: planned", "commands: 2 (", "collection: " + filepath.Join(wd, "configs") + " (file mode 0660)", "- name:127.0.0.1: planned", "  commands: show version; show clock"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if got := Main(append([]string{list}, args("--cmd", "show users")...), strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("with --cmd: exit=%d stderr=%q", got, stderr.String())
	}
	if out := stdout.String(); strings.Contains(out, "  commands: show version") || !strings.Contains(out, "commands: 1 (") {
		t.Errorf("with --cmd the operator's list alone:\n%s", out)
	}
}
