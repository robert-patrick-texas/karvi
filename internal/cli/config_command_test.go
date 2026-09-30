package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// TestConfigColors: the colour test lists
// every role under the configured theme first and then the other, each key
// rendered as the display renders that role (target and address bold), with
// the resolved colour, default or configured, and the escape; a configured
// override holds under both themes; a pipe under auto prints the words alone
// with the escape column still filled.
func TestConfigColors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct {
		args    []string
		contain []string
		absent  []string
	}{
		{[]string{"--set", "display.color=always", "config", "colors"},
			[]string{"theme dark (display.theme = \"dark\")\n", "\ntheme light\n",
				"  \x1b[36mdisplay.colors.accent\x1b[0m          cyan     default    \\x1b[36m\n",
				"  \x1b[1;33mdisplay.colors.target\x1b[0m          yellow   default    \\x1b[1;33m\n",
				"  \x1b[90mdisplay.colors.dynamic-border\x1b[0m  gray     default    \\x1b[90m\n",
				"  \x1b[1;35mdisplay.colors.address\x1b[0m         magenta  default    \\x1b[1;35m\n"},
			nil},
		{[]string{"--set", "display.color=auto", "--set", "display.theme=light", "--set", "display.colors.accent=magenta", "config", "colors"},
			[]string{"theme light (display.theme = \"light\")\n", "\ntheme dark\n",
				"  display.colors.accent          magenta  configured \\x1b[35m\n",
				"  display.colors.target          blue     default    \\x1b[1;34m\n",
				"  display.colors.target          yellow   default    \\x1b[1;33m\n"},
			[]string{"\x1b["}},
	} {
		var stdout, stderr bytes.Buffer
		if got := Main(tc.args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitSuccess || stderr.Len() != 0 {
			t.Fatalf("%q: exit=%d stderr=%q", tc.args, got, stderr.String())
		}
		out := stdout.String()
		if n := strings.Count(out, "display.colors."); n != 24 {
			t.Errorf("%q: %d role lines, want 24:\n%s", tc.args, n, out)
		}
		for _, s := range tc.contain {
			if !strings.Contains(out, s) {
				t.Errorf("%q: the output lacks %q:\n%s", tc.args, s, out)
			}
		}
		for _, s := range tc.absent {
			if strings.Contains(out, s) {
				t.Errorf("%q: the output holds %q:\n%s", tc.args, s, out)
			}
		}
	}
}
