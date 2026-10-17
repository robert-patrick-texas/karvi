// Package flagspec holds the command line of an executable that parses with
// Go's flag package, karvi-prune's, once: its main defines its flag set
// through the Spec's Define and prints Help, the completer (complete.go)
// walks the same set, and the man page's SYNOPSIS and OPTIONS are generated
// from it (tools/mangen), so Tab cannot offer a flag the executable does not
// take and no text can name a value otherwise.
package flagspec

import (
	"flag"
	"strings"
)

// Spec is one executable's command line.
type Spec struct {
	// Program is the executable's name, as the synopsis and the help
	// print it.
	Program string
	// Title is the help's first line, the executable's name and what it
	// does.
	Title string
	// Order is the flags in the synopsis's order; the flag package itself
	// offers them alphabetically. Every defined flag is in it once
	// (TestSpec in each executable's package).
	Order []string
	// Values says what a flag's value may be. A switch has no row.
	Values map[string]Value
	// Define defines the flags on a flag set with their defaults and usage
	// texts.
	Define func(*flag.FlagSet)
}

// Value is what a flag's value may be: the words it takes, whether it also
// names a path the shell may complete, and, for a value that is neither,
// its name (a number Tab cannot know). Completion offers the words and the
// path; Placeholder names the value from all three.
type Value struct {
	Words []string
	Path  bool
	Name  string
}

// FlagSet is a new flag set of the spec's flags, for the completer and the
// man page, which parse nothing.
func (s Spec) FlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet(s.Program, flag.ContinueOnError)
	s.Define(fs)
	return fs
}

// Placeholder is the name of a flag's value, as the synopsis, the help,
// and the man page print it: its words joined with |, then PATH when it names a
// path, or its name; empty for a switch.
func (s Spec) Placeholder(name string) string {
	v := s.Values[name]
	parts := append([]string{}, v.Words...)
	if v.Path {
		parts = append(parts, "PATH")
	}
	if v.Name != "" {
		parts = append(parts, v.Name)
	}
	return strings.Join(parts, "|")
}

// Usage is the synopsis line: each flag in Order, bracketed, with its
// placeholder.
func (s Spec) Usage() string {
	var b strings.Builder
	b.WriteString("usage: " + s.Program)
	for _, name := range s.Order {
		b.WriteString(" " + s.group(name))
	}
	return b.String()
}

// group is one flag's bracketed synopsis word.
func (s Spec) group(name string) string {
	g := "[--" + name
	if p := s.Placeholder(name); p != "" {
		g += " " + p
	}
	return g + "]"
}

// The help's columns, karvi's: an option's words after two spaces, its
// description from helpColumn (two spaces at least between them), every
// line wrapped at helpWidth.
const (
	helpColumn = 33
	helpWidth  = 79
)

// Help is -h and --help, in karvi's shape for helplayout to lay out: the
// title, the Usage section with the synopsis wrapped under its first
// bracket, and the Options section, each flag in Order with its placeholder
// and its usage string beside it, the default after.
func (s Spec) Help(fs *flag.FlagSet) string {
	var b strings.Builder
	b.WriteString(s.Title + "\n\nUsage:\n")
	lead := "  " + s.Program + " "
	line := lead
	for i, name := range s.Order {
		group := s.group(name)
		if i > 0 && len(line)+1+len(group) > helpWidth {
			b.WriteString(line + "\n")
			line = strings.Repeat(" ", len(lead)) + group
			continue
		}
		if i > 0 {
			line += " "
		}
		line += group
	}
	b.WriteString(line + "\n\nOptions:\n")
	for _, name := range s.Order {
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		words := "  --" + name
		if p := s.Placeholder(name); p != "" {
			words += " " + p
		}
		text := f.Usage
		if d := Default(f); d != "" {
			text += " (default " + d + ")"
		}
		lines := wrap(text, helpWidth-helpColumn)
		if len(words)+2 > helpColumn {
			// Too long for the field: the description under it.
			b.WriteString(words + "\n")
		} else {
			b.WriteString(words + strings.Repeat(" ", helpColumn-len(words)) + lines[0] + "\n")
			lines = lines[1:]
		}
		for _, l := range lines {
			b.WriteString(strings.Repeat(" ", helpColumn) + l + "\n")
		}
	}
	return b.String()
}

// wrap breaks text into lines of at most width at spaces; a word longer
// than width has a line of its own.
func wrap(text string, width int) []string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(text) {
		switch {
		case line == "":
			line = w
		case len(line)+1+len(w) <= width:
			line += " " + w
		default:
			lines = append(lines, line)
			line = w
		}
	}
	return append(lines, line)
}

// Default is a flag's default as the texts print it: empty for an empty
// default or a switch that is off.
func Default(f *flag.Flag) string {
	if f.DefValue == "false" {
		return ""
	}
	return f.DefValue
}

// IsBool reports whether f takes no value word (a switch).
func IsBool(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}
