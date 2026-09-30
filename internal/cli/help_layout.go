package cli

import (
	"io"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// The help texts (help.go) are written as plain columns and kept in step
// with the parser table by the drift test. What reaches the terminal is
// laid out here: the title line's
// tag in the label colour, a section heading in bold, an option's words in
// the accent colour, the action word of a Usage line in the success
// colour, and a blank line where the eye needs one inside a section. The text is not changed otherwise, so
// `--help | grep` and the documents' copies read the same words.
//
// Colour follows the display's own rule (display.color, display.theme, and
// whether standard output is a terminal), read from the configuration the
// invocation names; --ansi strip turns it off, as it strips every escape
// from the output. A configuration that does not load, or an operator
// identity that cannot be read, gives plain help: help never fails for a
// reason that is not the help's.

// helpStyle is the colour decision for one help text.
type helpStyle struct {
	enabled bool
	accent  string // the option words' colour
	action  string // the Usage lines' action words' colour (the success role)
	label   string // the title line's tag colour (the label role)
}

// helpStyleFor decides the style for a help request under the invocation's
// global options: the configuration is loaded as `config show` loads it
// (the same roots, --set values, and flag values), and nothing is created.
func helpStyleFor(g globalOptions, stdout io.Writer) helpStyle {
	if g.ansi == "strip" {
		return helpStyle{}
	}
	op, err := osutil.CurrentOperator()
	if err != nil {
		return helpStyle{}
	}
	cfg, err := configload.Load(configload.Options{ExplicitRoots: g.configs, Sets: g.sets, FlagValues: g.common().ConfigFlags, HomeDir: op.Home})
	if err != nil {
		return helpStyle{}
	}
	theme := cfg.String("display.theme")
	return helpStyle{
		enabled: display.ColorEnabled(cfg.String("display.color"), theme, jobexec.DisplayTerminal(stdout)),
		accent:  display.RoleColor(theme, "accent", cfg.String("display.colors.accent")),
		action:  display.RoleColor(theme, "success", cfg.String("display.colors.success")),
		label:   display.RoleColor(theme, "label", cfg.String("display.colors.label")),
	}
}

// titleTag is the tag of a text's title line, "karvi - what karvi is": the
// first line when it begins with the name and a dash. The tag, dash
// included, takes the label colour; the name stays plain.
const titleName = "karvi "

func titleTag(line string) (start int) {
	if strings.HasPrefix(line, titleName+"- ") {
		return len(titleName)
	}
	return -1
}

// usageHeading is the heading that opens a text's Usage section; the
// section's lines, up to the next blank line, are the invocations, and
// each takes the action colour on its action words.
const usageHeading = "Usage:"

// usagePrivileged is the one word a Usage line may carry before karvi: the
// invocation that runs as root keeps its sudo, plain, and its action words
// take the colour as every other line's do.
const usagePrivileged = "sudo"

// usageAction is the span of a Usage line's action: the command word after
// karvi and the subcommand word that follows it (run; cmd; daemon start;
// job follow; config generate; setup shared), the <command> placeholder
// standing where they stand (karvi [global-options] <command>), or the
// word -, stream's alias (karvi -). Before
// karvi the line holds its indent alone or the word sudo, the invocation
// that runs as root (sudo karvi setup shared). A bracket group before the
// action is passed over; the first word after the action that is not a
// bare lowercase word (a bracket, a dash, a placeholder in capitals) ends
// it. A line without karvi, or with no action after it, has start -1.
func usageAction(line string) (start, end int) {
	start, end = -1, -1
	i := strings.Index(line, "karvi ")
	if i < 0 {
		return start, end
	}
	if before := strings.TrimSpace(line[:i]); before != "" && before != usagePrivileged {
		return start, end
	}
	pos := i + len("karvi ")
	for pos < len(line) {
		for pos < len(line) && line[pos] == ' ' {
			pos++
		}
		e := pos
		for e < len(line) && line[e] != ' ' {
			e++
		}
		word := line[pos:e]
		switch {
		case word == "":
		case start < 0 && strings.HasPrefix(word, "["):
			// a group before the action, [global-options]
		case bareWord(word) || (start < 0 && (word == "<command>" || word == "-")):
			if start < 0 {
				start = pos
			}
			end = e
		default:
			return start, end
		}
		pos = e
	}
	return start, end
}

// bareWord is true of a word of lowercase letters only: a command or a
// subcommand word, never a placeholder, an option, or a group.
func bareWord(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// The shapes a help line takes. An entry is an option's line: two spaces,
// its words, then the description at the column every help text shares; a
// long option list has no description on its line and its description on
// the continuation lines under it. Prose is a paragraph inside a section,
// indented two spaces. A heading is a run of unindented lines ending in a
// colon; unindented text that does not end so is a paragraph of its own.
const helpDescriptionColumn = 33

type helpLineKind int

const (
	helpBlank helpLineKind = iota
	helpUnindented
	helpEntry
	helpContinuation
	helpProse
)

func classifyHelpLine(line string) helpLineKind {
	switch {
	case line == "":
		return helpBlank
	case line[0] != ' ':
		return helpUnindented
	case strings.HasPrefix(line, strings.Repeat(" ", helpDescriptionColumn)):
		return helpContinuation
	case strings.HasPrefix(line, "  ") && line[2] != ' ' && helpEntryWords(line) != "":
		return helpEntry
	}
	return helpProse
}

// helpEntryWords is the option words of an entry line, or "" for a line
// that is not one. An entry's words end two or more spaces before the
// description column, where the description begins; a line of options too
// long for that has no description on its line and is an entry when it
// begins with a dash. Before the column, a dash begins an option, and a
// word or a short list of words (login; command, cmd; DEVICE; daemon
// start|stop) is a command or placeholder entry; a sentence is prose.
func helpEntryWords(line string) string {
	body := line[2:]
	gap := len(line) > helpDescriptionColumn && line[helpDescriptionColumn-2:helpDescriptionColumn] == "  " && line[helpDescriptionColumn] != ' '
	if !gap {
		if body[0] == '-' {
			return line
		}
		return ""
	}
	words := strings.TrimRight(line[:helpDescriptionColumn], " ")
	if body[0] != '-' && len(strings.Fields(words)) > 3 {
		return ""
	}
	return words
}

// layoutHelp lays a help text out for the terminal: headings in bold, the
// entries' words in the accent colour, the Usage lines' action words in the
// action colour, and a blank line between a prose paragraph and its
// neighbours inside a section, and after an entry whose description ran to
// three lines or more, so that the next entry does not read as its
// continuation. Blank lines already in the text are kept and never doubled.
func layoutHelp(text string, st helpStyle) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	kinds := make([]helpLineKind, len(lines))
	words := make([]string, len(lines)) // an entry's words, coloured
	for i, l := range lines {
		kinds[i] = classifyHelpLine(l)
		if kinds[i] == helpEntry {
			words[i] = helpEntryWords(l)
		}
	}
	// A command entry too long for its column (daemon start|stop|restart)
	// has its description on the line under it, which is what tells it
	// from a line of prose: a short line over a continuation is an entry.
	for i := range lines {
		if kinds[i] == helpProse && i+1 < len(lines) && kinds[i+1] == helpContinuation && len(strings.Fields(lines[i])) <= 3 {
			kinds[i], words[i] = helpEntry, lines[i]
		}
	}
	// A run of unindented lines is a heading when its last line ends in a
	// colon; the run is marked as a whole.
	heading := make([]bool, len(lines))
	for i := 0; i < len(lines); {
		if kinds[i] != helpUnindented {
			i++
			continue
		}
		j := i
		for j < len(lines) && kinds[j] == helpUnindented {
			j++
		}
		if strings.HasSuffix(lines[j-1], ":") {
			for k := i; k < j; k++ {
				heading[k] = true
			}
		}
		i = j
	}
	var out strings.Builder
	entryLines := 0 // lines of the entry being written, 0 outside one
	prev := helpBlank
	prevHeading := false
	usage := false // inside the Usage section, up to its blank line
	for i, l := range lines {
		k := kinds[i]
		switch {
		case heading[i]:
			usage = l == usageHeading
		case k == helpBlank:
			usage = false
		}
		// Where a blank line is owed before this line.
		switch {
		case k == helpBlank || prev == helpBlank || prevHeading:
		case heading[i]:
		case k == helpProse && prev != helpProse:
			out.WriteByte('\n')
		case k == helpEntry && prev == helpProse:
			out.WriteByte('\n')
		case k == helpEntry && entryLines >= 3:
			out.WriteByte('\n')
		}
		switch k {
		case helpEntry:
			entryLines = 1
			out.WriteString("  ")
			out.WriteString(display.ANSI(words[i][2:], st.accent, st.enabled))
			out.WriteString(l[len(words[i]):])
		case helpContinuation:
			entryLines++
			out.WriteString(l)
		default:
			entryLines = 0
			switch {
			case heading[i]:
				out.WriteString(display.ANSIStyle(l, "default", st.enabled, true))
			case i == 0 && titleTag(l) >= 0:
				out.WriteString(l[:titleTag(l)])
				out.WriteString(display.ANSI(l[titleTag(l):], st.label, st.enabled))
			case usage:
				if s, e := usageAction(l); s >= 0 {
					out.WriteString(l[:s])
					out.WriteString(display.ANSI(l[s:e], st.action, st.enabled))
					out.WriteString(l[e:])
				} else {
					out.WriteString(l)
				}
			default:
				out.WriteString(l)
			}
		}
		out.WriteByte('\n')
		prev, prevHeading = k, heading[i]
	}
	return out.String()
}
