package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// TestFsOptionChecks: --fs=SUFFIX on run, command, and crun; a bare --fs
// or --fs= is cli_option_value_missing, a path after a bare --fs is
// cli_option_value_detached, and a suffix holding /, NUL, or a control
// character is crun_suffix_invalid, all before any device is planned.
func TestFsOptionChecks(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--target", "r1", "--fs=.cfg", "--cmd", "show clock"},
		{"command", "--fs=.cfg", "r1", "show", "clock"},
		{"crun", "--all", "--fs=.cfg"},
	} {
		if inv, err := Parse(args); err != nil || inv.String(optFs) != ".cfg" {
			t.Fatalf("%q: %v", args, err)
		}
	}
	if _, err := Parse([]string{"crun", "--all", "--fs", "/x"}); errorcodes.Of(err) != "cli_option_value_detached" || !strings.Contains(err.Error(), "--fs takes its SUFFIX with =: --fs=/x") {
		t.Fatalf("a detached path: %v", err)
	}
	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"run", "--no-daemon", "--target", "r1", "--fs", "--cmd", "show clock"}, "cli_option_value_missing"},
		{[]string{"run", "--no-daemon", "--target", "r1", "--fs=", "--cmd", "show clock"}, "cli_option_value_missing"},
		{[]string{"command", "--fs", "r1", "show", "clock"}, "cli_option_value_missing"},
		{[]string{"run", "--no-daemon", "--target", "r1", "--fs=a/b", "--cmd", "show clock"}, "crun_suffix_invalid"},
		{[]string{"command", "--fs=x\ny", "r1", "show", "clock"}, "crun_suffix_invalid"},
		{[]string{"crun", "--all", "--no-daemon", "--fs=\x00"}, "crun_suffix_invalid"},
	} {
		var stdout, stderr bytes.Buffer
		if got := Main(c.args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitUsageError || !strings.HasPrefix(stderr.String(), c.code+": ") {
			t.Errorf("%q: exit=%d stderr=%q, want %s", c.args, got, stderr.String(), c.code)
		}
	}
}

// TestFsImpliesCdOnRunAndCommand: on run and command --fs without --cd is
// --cd=. (crun.directory "." as a flag-origin value, named as implied); with
// --cd the PATH stands; on crun --fs alone keeps crun.directory.
func TestFsImpliesCdOnRunAndCommand(t *testing.T) {
	for _, c := range []struct {
		args          []string
		word, dir     string
		want          string
		implied, none bool
	}{
		{[]string{"run", "--target", "r1", "--fs=.cfg", "--cmd", "x"}, "run", ".", "run", true, false},
		{[]string{"command", "--fs=.cfg", "r1", "x"}, "command", ".", "command", true, false},
		{[]string{"run", "--target", "r1", "--cd=/srv/c", "--fs=.cfg", "--cmd", "x"}, "run", "/srv/c", "run", false, false},
		{[]string{"crun", "--all", "--fs=.cfg"}, "crun", "", "crun", false, true},
		{[]string{"run", "--target", "r1", "--cmd", "x"}, "run", "", "", false, true},
	} {
		inv := mustParse(t, c.args...)
		flags := map[string]configload.FlagValue{}
		got := collectionOptions(inv, c.word, flags)
		dir, set := flags["crun.directory"]
		source := "--cd"
		if c.implied {
			source = "--fs"
		}
		if got.word != c.want || got.implied != c.implied || set == c.none || (set && (dir.Value != c.dir || dir.Option != source)) {
			t.Errorf("%q: %+v crun.directory=%+v (%v)", c.args, got, dir, set)
		}
	}
}

// TestFsDryRunAndStream: a dry run of run --fs=.cfg names the working
// directory and the suffix; a stream line with a bad suffix is dropped when
// read.
func TestFsDryRunAndStream(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	var stdout, stderr bytes.Buffer
	args := append(append([]string{}, sets...), "run", "--dry-run", "--no-daemon", "--target", "127.0.0.1", "--transport", "system", "--fs=.cfg", "--cmd", "show clock")
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	wd, _ := os.Getwd()
	if want := "collection: " + filepath.Clean(wd) + " (file mode 0660, suffix .cfg)"; !strings.Contains(stdout.String(), want) {
		t.Errorf("report lacks %q:\n%s", want, stdout.String())
	}
	stderr.Reset()
	in := strings.NewReader("--target r1\n--fs=a/b\n--fs\n--end\n")
	streamLoop(context.Background(), streamScanner(in), &stderr, func(int, []string) int { return 0 }, restartNone)
	for _, want := range []string{"stream line 2 dropped: crun_suffix_invalid: ", "stream line 3 dropped: cli_option_value_missing: --fs takes its SUFFIX with ="} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr.String())
		}
	}
}
