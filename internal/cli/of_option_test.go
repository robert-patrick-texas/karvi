package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// TestOfOptionParses: --of[=PATH] takes its PATH with = only, so in command
// the word after a bare --of is the device, and in run it is device text;
// the parse records the option, and outputOptions turns it into flag-origin
// values: output.persist-command=true, and output.root when PATH is given,
// for command and run alike.
func TestOfOptionParses(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		target string
		cmd    string
		flags  map[string]any
	}{
		{[]string{"cmd", "--of", "xe-1", "show", "clock"}, "target=xe-1", "show clock", map[string]any{"output.persist-command": true}},
		{[]string{"cmd", "--of=out", "xe-1", "show", "clock"}, "target=xe-1", "show clock", map[string]any{"output.persist-command": true, "output.root": "out"}},
		{[]string{"cmd", "xe-1", "--of=/x/out", "show", "clock"}, "target=xe-1", "show clock", map[string]any{"output.persist-command": true, "output.root": "/x/out"}},
		{[]string{"cmd", "--nof", "xe-1", "show", "clock"}, "target=xe-1", "show clock", map[string]any{"output.persist-command": false}},
		{[]string{"run", "--no-daemon", "--of", "--target", "xe-1", "show", "clock"}, "target=xe-1", "show clock", map[string]any{"output.persist-command": true}},
		{[]string{"run", "--of=out", "--target", "xe-1", "show", "clock"}, "target=xe-1", "show clock", map[string]any{"output.persist-command": true, "output.root": "out"}},
		{[]string{"run", "--nof", "--target", "xe-1", "show", "clock"}, "target=xe-1", "show clock", map[string]any{"output.persist-command": false}},
	} {
		inv := mustParse(t, tc.args...)
		if got := targetValues(inv); len(got) != 1 || got[0] != tc.target {
			t.Errorf("%q: targets %q, want %q", tc.args, got, tc.target)
		}
		if len(inv.Commands) != 1 || inv.Commands[0] != tc.cmd {
			t.Errorf("%q: commands %q, want %q", tc.args, inv.Commands, tc.cmd)
		}
		flags := map[string]configload.FlagValue{}
		outputOptions(inv, flags)
		if len(flags) != len(tc.flags) {
			t.Errorf("%q: flags %v, want %v", tc.args, flags, tc.flags)
		}
		for k, v := range tc.flags {
			if flags[k].Value != v || (flags[k].Option != "--of" && flags[k].Option != "--nof") {
				t.Errorf("%q: %s=%+v, want %v from --of or --nof", tc.args, k, flags[k], v)
			}
		}
	}
}

// TestOfOptionRefusals: --of with --nof is output_options_conflict, as
// --border with --noborder is, on command and run; run --nof with --detach
// or --exercise is run_mode_conflict; a bare --of followed by a word of a
// path's form is cli_option_value_detached. All
// are usage errors before any device is planned.
func TestOfOptionRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"command", "--of", "--nof", "router1", "show", "clock"}, "output_options_conflict"},
		{[]string{"command", "--nof", "--of=out", "router1", "show", "clock"}, "output_options_conflict"},
		{[]string{"run", "--of", "--nof", "--target", "router1", "--cmd", "show clock"}, "output_options_conflict"},
		{[]string{"run", "--nof", "--detach", "--target", "router1", "--cmd", "show clock"}, "run_mode_conflict"},
		{[]string{"run", "--nof", "--exercise", "--target", "router1", "--cmd", "show clock"}, "run_mode_conflict"},
		// The path after a space was meant as the value, which attaches
		// with = alone: refused, never sent to the device or taken for one.
		{[]string{"run", "--of", "/tmp/x", "--target", "router1", "--cmd", "show clock"}, "cli_option_value_detached"},
		{[]string{"command", "--of", "../x", "router1", "show", "clock"}, "cli_option_value_detached"},
	} {
		var stdout, stderr bytes.Buffer
		got := Main(tc.args, strings.NewReader(""), &stdout, &stderr)
		if got != exitcode.ExitUsageError || !strings.HasPrefix(stderr.String(), tc.code+": ") {
			t.Errorf("%q: exit=%d stderr=%q, want %s", tc.args, got, stderr.String(), tc.code)
		}
	}
}
