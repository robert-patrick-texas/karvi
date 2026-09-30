package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// TestHelpLayoutShapes: the layout rules on a text of every shape a help
// text has: the title line's tag in
// the label colour with the name plain, a heading in bold, an
// entry's words in the accent colour with the indent left plain, a Usage
// line's action words in the action colour (the command word, the
// subcommand word after it, or the <command> placeholder past a leading
// group; a continuation line of the invocation has none; a line that runs
// as root keeps its sudo plain before the coloured words), a command entry
// too long for its column told from prose by the continuation under it, a
// blank line around a prose paragraph inside a section and after an entry
// of three lines or more, none after a heading, and an existing blank line
// never doubled.
func TestHelpLayoutShapes(t *testing.T) {
	text := `karvi - a title line

Usage:
  karvi thing [options]
  karvi thing sub JOB-ID [--format text|json]
  karvi [global-options] <command> [options]
  karvi thing [--one VALUE] [--two VALUE]
              [--three VALUE]
  sudo karvi thing sub [--group NAME]

Two-line heading (with a
parenthesis):
  DEVICE                         A placeholder entry
  --one VALUE, --o               A one-line entry
  --two                          A two-line entry, whose second line
                                 is here
  --three                        A three-line entry, whose lines
                                 run to
                                 here
  --four                         After three lines a blank line; this
                                 entry is short
  --site GLOB, --device-group GLOB, --select-platform GLOB, --all
                                 A dash line too long for the column
  A prose paragraph inside the section, which the eye needs a blank
  line around.
  --five                         The entry after the prose
  daemon start|stop|restart      A command entry
  daemon start|stop|restart|status|serve
                                 A command entry too long for the column

A closing paragraph at the margin, not a heading.
`
	want := `karvi - a title line

Usage:
  karvi thing [options]
  karvi thing sub JOB-ID [--format text|json]
  karvi [global-options] <command> [options]
  karvi thing [--one VALUE] [--two VALUE]
              [--three VALUE]
  sudo karvi thing sub [--group NAME]

Two-line heading (with a
parenthesis):
  DEVICE                         A placeholder entry
  --one VALUE, --o               A one-line entry
  --two                          A two-line entry, whose second line
                                 is here
  --three                        A three-line entry, whose lines
                                 run to
                                 here

  --four                         After three lines a blank line; this
                                 entry is short
  --site GLOB, --device-group GLOB, --select-platform GLOB, --all
                                 A dash line too long for the column

  A prose paragraph inside the section, which the eye needs a blank
  line around.

  --five                         The entry after the prose
  daemon start|stop|restart      A command entry
  daemon start|stop|restart|status|serve
                                 A command entry too long for the column

A closing paragraph at the margin, not a heading.
`
	if got := layoutHelp(text, helpStyle{}); got != want {
		t.Fatalf("plain layout:\n%s\nwant:\n%s", got, want)
	}
	styled := layoutHelp(text, helpStyle{enabled: true, accent: "cyan", action: "green", label: "gray"})
	if got := ansiEscape.ReplaceAllString(styled, ""); got != want {
		t.Fatalf("the styled layout's words differ from the plain layout:\n%s", got)
	}
	for _, line := range []string{
		"\x1b[1mUsage:\x1b[0m\n",
		"  karvi \x1b[32mthing\x1b[0m [options]\n",
		"  karvi \x1b[32mthing sub\x1b[0m JOB-ID [--format text|json]\n",
		"  karvi [global-options] \x1b[32m<command>\x1b[0m [options]\n",
		"  karvi \x1b[32mthing\x1b[0m [--one VALUE] [--two VALUE]\n              [--three VALUE]\n",
		"  sudo karvi \x1b[32mthing sub\x1b[0m [--group NAME]\n",
		"\x1b[1mTwo-line heading (with a\x1b[0m\n\x1b[1mparenthesis):\x1b[0m\n",
		"  \x1b[36mDEVICE\x1b[0m                         A placeholder entry\n",
		"  \x1b[36m--one VALUE, --o\x1b[0m               A one-line entry\n",
		"  \x1b[36m--site GLOB, --device-group GLOB, --select-platform GLOB, --all\x1b[0m\n",
		"  \x1b[36mdaemon start|stop|restart|status|serve\x1b[0m\n",
		"  A prose paragraph inside the section, which the eye needs a blank\n",
		"A closing paragraph at the margin, not a heading.\n",
		"karvi \x1b[90m- a title line\x1b[0m\n",
	} {
		if !strings.Contains(styled, line) {
			t.Errorf("the styled layout lacks %q:\n%s", line, styled)
		}
	}
}

// TestHelpLayoutKeepsEveryHelpText: laid out, every help text of the parser
// table holds the same words in the same order (only blank lines and
// escapes are added), so `--help | grep` and the documents' copies of the
// texts read the same; and the styled form less its escapes is the plain
// form.
func TestHelpLayoutKeepsEveryHelpText(t *testing.T) {
	texts := map[string]string{"top": topHelp}
	var add func(c *command)
	add = func(c *command) {
		texts[c.path] = c.help()
		for _, s := range c.subs {
			add(s)
		}
	}
	for _, c := range commandTable {
		add(c)
	}
	words := func(s string) string { return strings.ReplaceAll(s, "\n\n", "\n") }
	for name, text := range texts {
		plain := layoutHelp(text, helpStyle{})
		if strings.Contains(plain, "\n\n\n") {
			t.Errorf("%s: the layout doubled a blank line", name)
		}
		if words(plain) != words(text) {
			t.Errorf("%s: the layout changed the words:\n%s", name, plain)
		}
		styled := layoutHelp(text, helpStyle{enabled: true, accent: "cyan"})
		if ansiEscape.ReplaceAllString(styled, "") != plain {
			t.Errorf("%s: the styled layout is not the plain layout plus escapes", name)
		}
	}
}

// TestHelpStyleDecision: colour under display.color as the display's own
// lines have it (always colours a pipe, never and --ansi strip do not), a
// --config or --set before a global --help is read, the action words take
// the success role (green under both themes, display.colors.success
// overriding), and a configuration that does not load gives plain help at
// exit 0.
func TestHelpStyleDecision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := filepath.Join(home, "always.toml")
	if err := os.WriteFile(cfg, []byte("[display]\ncolor = \"always\"\ncolors = {accent = \"magenta\", success = \"blue\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args    []string
		styled  bool
		contain string
	}{
		{[]string{"--set", "display.color=always", "job", "--help"}, true, "\x1b[1mUsage:\x1b[0m"},
		{[]string{"--set", "display.color=always", "cmd", "--help"}, true, "  \x1b[36m--target DEVICE, --host, --t\x1b[0m   "},
		{[]string{"--set", "display.color=always", "--set", "display.theme=light", "cmd", "--help"}, true, "  \x1b[34m--target DEVICE, --host, --t\x1b[0m   "},
		{[]string{"--config", cfg, "--help"}, true, "  \x1b[35mlogin\x1b[0m                          "},
		{[]string{"--set", "display.color=always", "run", "--help"}, true, "  karvi \x1b[32mrun\x1b[0m [target inputs] "},
		{[]string{"--set", "display.color=always", "stream", "--help"}, true, "  karvi \x1b[32mstream\x1b[0m\n  karvi \x1b[32m-\x1b[0m\n"},
		{[]string{"--set", "display.color=always", "--set", "display.theme=light", "daemon", "--help"}, true, "  karvi \x1b[32mdaemon start\x1b[0m [--foreground]\n"},
		{[]string{"--set", "display.color=always", "--help"}, true, "  karvi [global-options] \x1b[32m<command>\x1b[0m [options] [payload]\n"},
		{[]string{"--set", "display.color=always", "setup", "shared", "--help"}, true, "  sudo karvi \x1b[32msetup shared\x1b[0m [--group NAME] [--mode 2770|2775]\n"},
		{[]string{"--set", "display.color=always", "--help"}, true, "karvi \x1b[34m- run the fleet, gather the output\x1b[0m\n"},
		{[]string{"--set", "display.color=always", "--set", "display.colors.label=cyan", "--help"}, true, "karvi \x1b[36m- run the fleet, gather the output\x1b[0m\n"},
		{[]string{"--config", cfg, "version", "--help"}, true, "\x1b[1mUsage:\x1b[0m\n  karvi \x1b[34mversion\x1b[0m [--format text|json] [--help]\n"},
		{[]string{"--config", cfg, "--ansi", "strip", "--help"}, false, "  login                          "},
		{[]string{"--set", "display.color=never", "cmd", "--help"}, false, ""},
		{[]string{"cmd", "--help"}, false, ""}, // a pipe under auto
		{[]string{"--config", "/nonexistent/karvi", "--set", "display.color=always", "cmd", "--help"}, false, "Usage:\n"},
		{[]string{"--set", "display.timestamp=\"\"", "--set", "display.color=always", "cmd", "--help"}, false, "Usage:\n"},
	} {
		var stdout, stderr bytes.Buffer
		if got := Main(tc.args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitSuccess || stderr.Len() != 0 {
			t.Errorf("%q: exit=%d stderr=%q", tc.args, got, stderr.String())
			continue
		}
		if styled := strings.Contains(stdout.String(), "\x1b["); styled != tc.styled {
			t.Errorf("%q: styled=%v, want %v", tc.args, styled, tc.styled)
		}
		if !strings.Contains(stdout.String(), tc.contain) {
			t.Errorf("%q: the help lacks %q", tc.args, tc.contain)
		}
	}
}
