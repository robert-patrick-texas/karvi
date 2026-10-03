// Package helplayout lays a help text out for the terminal, the one layout
// of karvi's executables: the title line's tag in the label colour, a
// section heading in bold, an option's words in the accent colour, the
// action word of a Usage line in the success colour, and a blank line where
// the eye needs one inside a section. The text is not changed otherwise, so
// `--help | grep` and the documents' copies read the same words. Whether
// and in which colours is the caller's Style: karvi decides it from the
// display's configuration, karvi-prune from the defaults alone.
package helplayout

import (
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/display"
)

// Style is the colour decision for one help text.
type Style struct {
	Enabled bool
	Accent  string // the option words' colour
	Action  string // the Usage lines' action words' colour (the success role)
	Label   string // the title line's tag colour (the label role)
}

// titleTag is the tag of a text's title line, "karvi - what karvi is" or
// "karvi-prune - what it does": the first line when it begins with an
// executable's name, karvi or a name beginning karvi-, and a dash. The tag,
// dash included, takes the label colour; the name stays plain.
func titleTag(line string) (start int) {
	name, _, ok := strings.Cut(line, " - ")
	if ok && (name == "karvi" || strings.HasPrefix(name, "karvi-")) && !strings.Contains(name, " ") {
		return len(name) + 1
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

// Layout lays a help text out for the terminal: headings in bold, the
// entries' words in the accent colour, the Usage lines' action words in the
// action colour, and a blank line between a prose paragraph and its
// neighbours inside a section, and after an entry whose description ran to
// three lines or more, so that the next entry does not read as its
// continuation. Blank lines already in the text are kept and never doubled.
func Layout(text string, st Style) string {
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
			out.WriteString(display.ANSI(words[i][2:], st.Accent, st.Enabled))
			out.WriteString(l[len(words[i]):])
		case helpContinuation:
			entryLines++
			out.WriteString(l)
		default:
			entryLines = 0
			switch {
			case heading[i]:
				out.WriteString(display.ANSIStyle(l, "default", st.Enabled, true))
			case i == 0 && titleTag(l) >= 0:
				out.WriteString(l[:titleTag(l)])
				out.WriteString(display.ANSI(l[titleTag(l):], st.Label, st.Enabled))
			case usage:
				if s, e := usageAction(l); s >= 0 {
					out.WriteString(l[:s])
					out.WriteString(display.ANSI(l[s:e], st.Action, st.Enabled))
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
