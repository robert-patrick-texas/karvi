package helplayout

import (
	"regexp"
	"strings"
	"testing"
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
	if got := Layout(text, Style{}); got != want {
		t.Fatalf("plain layout:\n%s\nwant:\n%s", got, want)
	}
	styled := Layout(text, Style{Enabled: true, Accent: "cyan", Action: "green", Label: "gray"})
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

// TestTitleTag: the tag follows karvi's name or a helper's, karvi-prune's
// among them, and nothing else: a sentence with a dash is not a title.
func TestTitleTag(t *testing.T) {
	for line, want := range map[string]int{
		"karvi - what karvi is":              6,
		"karvi-prune - remove finished work": 12,
		"karvi-askpass - a helper":           14,
		"run - not a title":                  -1,
		"the karvi - sentence":               -1,
		"karvi: no dash":                     -1,
	} {
		if got := titleTag(line); got != want {
			t.Errorf("%q: %d, want %d", line, got, want)
		}
	}
	got := Layout("karvi-prune - remove finished work\n", Style{Enabled: true, Label: "gray"})
	if !strings.HasPrefix(got, "karvi-prune \x1b[") || !strings.Contains(got, "- remove finished work\x1b[0m") {
		t.Errorf("styled title: %q", got)
	}
}

// TestIrregular: a line whose words are separated by two or more spaces
// anywhere but at the description column is irregular; an entry, its long
// form, a continuation, prose, and a gap at the column are not.
func TestIrregular(t *testing.T) {
	text := `Heading:
  --one VALUE                    An entry
  --a-long-option-list VALUE --other VALUE
                                 Its description under it
  A prose line with single spaces.
  Specified, not yet available:  --pending VALUE
Modes:
  first       A table of its own
  second      Another row
`
	got := Irregular(text)
	want := []string{"  first       A table of its own", "  second      Another row"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Irregular = %q, want %q", got, want)
	}
}
