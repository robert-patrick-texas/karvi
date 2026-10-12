package credhelper

import (
	"fmt"

	"github.com/robert-patrick-texas/karvi/internal/envname"
)

// The output's bounds: a helper's whole output, and one line without its
// newline.
const (
	maxOutput = 64 << 10
	maxLine   = 4 << 10
)

// assignment is one NAME=value the output set, on its line.
type assignment struct {
	name  string
	value []byte
	line  int
}

// outputError is a refusal of the output: the line it names (0 for the
// whole output) and the reason, which quotes nothing from the line, since
// any of it may be a secret.
type outputError struct {
	line   int
	reason string
}

func (e *outputError) Error() string {
	if e.line == 0 {
		return e.reason
	}
	return fmt.Sprintf("line %d: %s", e.line, e.reason)
}

// parse reads a helper's output as text, line by line, and never as a shell
// would run it (EXAMPLES chapter 56, issue 10): a line karvi accepts means
// what dash and bash give it under eval. A blank line is skipped, and so is
// a comment, its first non-blank character #. Any other line is an optional
// export and one or more assignments separated by blanks, each a portable
// variable name, = with no blank on either side, and a value: unquoted, of
// letters, digits, and _ . , : / @ % + = - alone; double-quoted, where \",
// \\, \$, and \` are the four escapes, any other backslash is kept, and a
// bare $ or ` is refused; single-quoted, every character as written but ';
// or empty. A trailing comment after blanks is ignored. No value holds a
// control character. A name set twice is refused naming both lines, as is
// an output past 64 KiB or a line past 4 KiB. Any refusal refuses the whole
// output, so a line karvi cannot read never lets an exported value through
// in its place. The values returned are copies; the caller clears out.
func parse(out []byte) ([]assignment, error) {
	if len(out) > maxOutput {
		return nil, &outputError{reason: "output past 64 KiB"}
	}
	var all []assignment
	seen := map[string]int{}
	line := 0
	for start := 0; start < len(out); {
		end := start
		for end < len(out) && out[end] != '\n' {
			end++
		}
		line++
		text := out[start:end]
		start = end + 1
		if len(text) > maxLine {
			return nil, &outputError{line: line, reason: "line past 4 KiB"}
		}
		set, reason := parseLine(text)
		if reason != "" {
			clearAssignments(all)
			clearAssignments(set)
			return nil, &outputError{line: line, reason: reason}
		}
		for _, a := range set {
			if first, ok := seen[a.name]; ok {
				clearAssignments(all)
				clearAssignments(set)
				if first == line {
					return nil, &outputError{line: line, reason: a.name + " set twice"}
				}
				return nil, &outputError{reason: fmt.Sprintf("%s set on lines %d and %d", a.name, first, line)}
			}
			seen[a.name] = line
			a.line = line
			all = append(all, a)
		}
	}
	return all, nil
}

// clearAssignments overwrites the values read so far.
func clearAssignments(set []assignment) {
	for _, a := range set {
		clear(a.value)
	}
}

func blank(c byte) bool { return c == ' ' || c == '\t' }

func control(c byte) bool { return c < 0x20 || c == 0x7f }

// unquoted reports a byte an unquoted value may hold.
func unquoted(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '_', '.', ',', ':', '/', '@', '%', '+', '=', '-':
		return true
	}
	return false
}

// nameByte reports a byte a name may hold.
func nameByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// parseLine reads one line: its assignments, or the reason it is refused.
func parseLine(s []byte) ([]assignment, string) {
	i := skipBlanks(s, 0)
	if i == len(s) || s[i] == '#' {
		return nil, ""
	}
	if j := i + len("export"); j <= len(s) && string(s[i:j]) == "export" && (j == len(s) || blank(s[j])) {
		i = skipBlanks(s, j)
		if i == len(s) || s[i] == '#' {
			return nil, "export without an assignment"
		}
	}
	var set []assignment
	emptyBefore := false // the assignment before had nothing after its =
	for {
		j := i
		for j < len(s) && nameByte(s[j]) {
			j++
		}
		if j == len(s) || s[j] != '=' || !envname.Portable(string(s[i:j])) {
			clearAssignments(set)
			return nil, wordReason(s, i, j, emptyBefore)
		}
		name := string(s[i:j])
		value, next, reason := parseValue(s, j+1)
		if reason != "" {
			clearAssignments(set)
			return nil, reason
		}
		set = append(set, assignment{name: name, value: value})
		emptyBefore = next == j+1
		i = skipBlanks(s, next)
		if i == len(s) || s[i] == '#' {
			return set, ""
		}
	}
}

func skipBlanks(s []byte, i int) int {
	for i < len(s) && blank(s[i]) {
		i++
	}
	return i
}

// wordReason says why the word at i, its name part ending at j, is no
// assignment: a blank before or after = (a shell would run a command), a
// control character, or a word of any other kind.
func wordReason(s []byte, i, j int, emptyBefore bool) string {
	for k := i; k < len(s) && !blank(s[k]); k++ {
		if control(s[k]) {
			return "a control character"
		}
	}
	if j > i && envname.Portable(string(s[i:j])) && j < len(s) && blank(s[j]) {
		if k := skipBlanks(s, j); k < len(s) && s[k] == '=' {
			return "a blank before ="
		}
	}
	if emptyBefore {
		return "a blank after ="
	}
	return "a word that is no assignment"
}

// parseValue reads the value that begins at i, after its =: the value, the
// index after it, or the reason it is refused.
func parseValue(s []byte, i int) ([]byte, int, string) {
	if i == len(s) || blank(s[i]) {
		return []byte{}, i, ""
	}
	var v []byte
	switch s[i] {
	case '"':
		k := i + 1
		for ; k < len(s) && s[k] != '"'; k++ {
			c := s[k]
			switch {
			case control(c):
				clear(v)
				return nil, 0, "a control character"
			case c == '$' || c == '`':
				clear(v)
				return nil, 0, "a bare $ or ` in double quotes"
			case c == '\\' && k+1 < len(s) && (s[k+1] == '"' || s[k+1] == '\\' || s[k+1] == '$' || s[k+1] == '`'):
				k++
				v = append(v, s[k])
			default:
				v = append(v, c)
			}
		}
		if k == len(s) {
			clear(v)
			return nil, 0, "an unterminated double quote"
		}
		return closed(s, k+1, v)
	case '\'':
		k := i + 1
		for ; k < len(s) && s[k] != '\''; k++ {
			if control(s[k]) {
				clear(v)
				return nil, 0, "a control character"
			}
			v = append(v, s[k])
		}
		if k == len(s) {
			clear(v)
			return nil, 0, "an unterminated single quote"
		}
		return closed(s, k+1, v)
	}
	k := i
	for ; k < len(s) && !blank(s[k]); k++ {
		c := s[k]
		if !unquoted(c) {
			clear(v)
			if control(c) {
				return nil, 0, "a control character"
			}
			return nil, 0, "an unquoted special character"
		}
		v = append(v, c)
	}
	return v, k, ""
}

// closed ends a quoted value at k, after its closing quote: the line ends
// there or a blank follows.
func closed(s []byte, k int, v []byte) ([]byte, int, string) {
	if k < len(s) && !blank(s[k]) {
		clear(v)
		if control(s[k]) {
			return nil, 0, "a control character"
		}
		return nil, 0, "text after a closing quote"
	}
	if v == nil {
		v = []byte{}
	}
	return v, k, ""
}
