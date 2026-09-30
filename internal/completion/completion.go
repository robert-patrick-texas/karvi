// Package completion holds what the executables' hidden words share: the
// word itself, the line the bash
// function hands over (`__complete CURSOR WORD...`), the directive that asks
// the shell for file names, the filter that keeps the candidates beginning
// with the word under the cursor, and the printer. karvi's completer walks its
// parser table over these, karvi-prune's walks its flag set, and the one bash
// function serves both, so the protocol lives here once.
package completion

import (
	"fmt"
	"io"
	"sort"
	"strconv"
)

const (
	// Word is the hidden word an executable takes before parsing anything
	// else: the words after it are the line being completed, not this
	// invocation's. It is in no help and abbreviates to nothing.
	Word = "__complete"
	// Files is the directive line the bash function reads as "complete file
	// names here"; it follows the candidates, when there are any, and the
	// function asks bash for file names only when no candidate matched.
	Files = ":files"
)

// Line reads the hidden word's arguments, CURSOR WORD...: CURSOR is the index
// of the word under the cursor in WORD..., whose first word is the program
// name; a cursor one past the last word is an empty current word (a Tab
// after a space, as bash passes it). line is the words after the program
// name and before the cursor, current the word under the cursor; ok is false
// when the arguments are not a line, on which the caller prints nothing.
func Line(args []string) (line []string, current string, ok bool) {
	if len(args) < 2 {
		return nil, "", false
	}
	cursor, err := strconv.Atoi(args[0])
	words := args[1:]
	if err != nil || cursor < 1 || cursor > len(words) {
		return nil, "", false
	}
	if cursor < len(words) {
		current = words[cursor]
	}
	return words[1:cursor], current, true
}

// Filter keeps the candidates that begin with current, each once, sorted.
func Filter(cands []string, current string) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cands {
		if len(c) >= len(current) && c[:len(current)] == current && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// Print writes the candidates one per line, then the Files directive when
// the shell should offer file names instead or as well.
func Print(w io.Writer, cands []string, files bool) {
	for _, c := range cands {
		fmt.Fprintln(w, c)
	}
	if files {
		fmt.Fprintln(w, Files)
	}
}
