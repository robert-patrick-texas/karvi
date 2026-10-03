package helplayout

import (
	"strings"
)

// RoffEscape makes text safe inside a roff line: a backslash as \e and a
// hyphen as \-, so an option reads and copies as typed.
func RoffEscape(s string) string {
	return strings.NewReplacer(`\`, `\e`, "-", `\-`).Replace(s)
}

// RoffLine is one text line of roff: a leading . or ' would be a request,
// so it is protected with \&.
func RoffLine(s string) string {
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "'") {
		s = `\&` + s
	}
	return s + "\n"
}

// roffArg is a quoted macro argument: escaped, a double quote doubled.
func roffArg(s string) string {
	return `"` + strings.ReplaceAll(RoffEscape(s), `"`, `""`) + `"`
}

// Roff renders a help text as the bodies of a manual page's SYNOPSIS and
// DESCRIPTION, from the analysis the terminal layout reads. The SYNOPSIS
// is the Usage lines, one invocation to a line, karvi and the action
// words in bold (the <command> placeholder in italic); a line of the Usage block that does not name karvi
// continues the invocation above it. The DESCRIPTION is everything after
// the Usage block: a heading as a subsection, an entry as a tagged
// paragraph whose option words are bold and whose values are italic, its
// description refilled, and a run of prose as a paragraph. A title line
// before the Usage block (karvi - ...) is the page's NAME, written by
// hand, and is left out. The words are kept byte for byte; groff breaks
// the lines.
func Roff(text string) (synopsis, description string) {
	lines, kinds, words, heading := analyze(text)
	i := 0
	for i < len(lines) && lines[i] != usageHeading {
		i++
	}
	var syn strings.Builder
	first := true
	for i++; i < len(lines) && lines[i] != ""; i++ {
		l := strings.TrimSpace(lines[i])
		start, end := usageAction(lines[i])
		k := strings.Index(l, "karvi")
		if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(l, usagePrivileged)), "karvi") {
			syn.WriteString(RoffLine(RoffEscape(l)))
			continue
		}
		if !first {
			syn.WriteString(".br\n")
		}
		first = false
		var b strings.Builder
		b.WriteString(RoffEscape(l[:k]))
		b.WriteString(`\fBkarvi\fR`)
		if start >= 0 {
			// usageAction measures the untrimmed line.
			off := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
			s, e := start-off, end-off
			b.WriteString(RoffEscape(l[k+len("karvi") : s]))
			font := `\fB`
			if strings.HasPrefix(l[s:], "<") {
				font = `\fI` // the <command> placeholder
			}
			b.WriteString(font + RoffEscape(l[s:e]) + `\fR`)
			b.WriteString(RoffEscape(l[e:]))
		} else {
			b.WriteString(RoffEscape(l[k+len("karvi"):]))
		}
		syn.WriteString(RoffLine(b.String()))
	}
	var desc strings.Builder
	paragraph := false // inside a run of prose, so a line continues it
	for ; i < len(lines); i++ {
		l := lines[i]
		switch {
		case kinds[i] == helpBlank:
			paragraph = false
		case heading[i]:
			j := i
			var run []string
			for j < len(lines) && heading[j] {
				run = append(run, lines[j])
				j++
			}
			desc.WriteString(".SS " + roffArg(strings.TrimSuffix(strings.Join(run, " "), ":")) + "\n")
			i, paragraph = j-1, false
		case kinds[i] == helpEntry:
			desc.WriteString(".TP\n")
			desc.WriteString(RoffLine(entryWords(strings.TrimSpace(words[i]))))
			if rest := strings.TrimSpace(l[len(words[i]):]); rest != "" {
				desc.WriteString(RoffLine(RoffEscape(rest)))
			}
			paragraph = false
		case kinds[i] == helpContinuation:
			desc.WriteString(RoffLine(RoffEscape(strings.TrimSpace(l))))
		default: // prose, indented or not
			if !paragraph {
				desc.WriteString(".PP\n")
			}
			desc.WriteString(RoffLine(RoffEscape(strings.TrimSpace(l))))
			paragraph = true
		}
	}
	return syn.String(), desc.String()
}

// entryWords sets an entry's words: an option (a word beginning with a
// dash) in bold and its value in italic (--target NAME, --host); a command
// entry's words (daemon start|stop) in bold; a placeholder entry (DEVICE)
// in italic. A comma between the words stays roman.
func entryWords(words string) string {
	command := words != "" && words[0] >= 'a' && words[0] <= 'z'
	var b strings.Builder
	for n, w := range strings.Fields(words) {
		if n > 0 {
			b.WriteByte(' ')
		}
		comma := strings.HasSuffix(w, ",")
		w = strings.TrimSuffix(w, ",")
		font := `\fI`
		if command || strings.HasPrefix(w, "-") {
			font = `\fB`
		}
		b.WriteString(font + RoffEscape(w) + `\fR`)
		if comma {
			b.WriteByte(',')
		}
	}
	return b.String()
}
