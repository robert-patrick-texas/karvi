package prune

import (
	"flag"
	"strings"
)

// The helper's command line lives here once: the helper's main defines its
// flag set through DefineFlags and prints Help, the completer
// (complete.go) walks the same set, and the man page's SYNOPSIS and OPTIONS
// are generated from it (tools/mangen), so Tab cannot offer a flag the
// helper does not take and no text can name a value otherwise. The helper
// reads no configuration: the flags carry the settings' words, and a unit's
// line holds a site's values.

// FlagOrder is the flags in the synopsis's order; the flag package itself
// offers them alphabetically. TestFlagOrder holds it to the defined flags.
var FlagOrder = []string{"basedir", "sharedroot", "scoreboards", "days", "minfree", "dry-run", "verbose", "format"}

// Placeholder is the name of a flag's value, as the synopsis, the help,
// and the man page print it: its words joined with |, then PATH when it names a
// path, or its name; empty for a switch.
func Placeholder(name string) string {
	v := flagValues[name]
	parts := append([]string{}, v.words...)
	if v.path {
		parts = append(parts, "PATH")
	}
	if v.name != "" {
		parts = append(parts, v.name)
	}
	return strings.Join(parts, "|")
}

// Usage is the helper's synopsis line: each flag in FlagOrder, bracketed,
// with its placeholder.
func Usage() string {
	var b strings.Builder
	b.WriteString("usage: karvi-prune")
	for _, name := range FlagOrder {
		b.WriteString(" [--" + name)
		if p := Placeholder(name); p != "" {
			b.WriteString(" " + p)
		}
		b.WriteString("]")
	}
	return b.String()
}

// Title is the help's first line, the helper's name and what it does.
const Title = "karvi-prune - remove finished karvi work older than the retention age"

// The help's columns, karvi's: an option's words after two spaces, its
// description from helpColumn (two spaces at least between them), every
// line wrapped at helpWidth.
const (
	helpColumn = 33
	helpWidth  = 79
)

// Help is -h and --help, in karvi's shape for helplayout to lay out: the
// title, the Usage section with the synopsis wrapped under its first
// bracket, and the Options section, each flag in FlagOrder with its
// placeholder and its usage string beside it, the default after.
func Help(fs *flag.FlagSet) string {
	var b strings.Builder
	b.WriteString(Title + "\n\nUsage:\n")
	lead := "  karvi-prune "
	line := lead
	for i, name := range FlagOrder {
		group := "[--" + name
		if p := Placeholder(name); p != "" {
			group += " " + p
		}
		group += "]"
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
	for _, name := range FlagOrder {
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		words := "  --" + name
		if p := Placeholder(name); p != "" {
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

// Default is a flag's default as the helper's texts print it: empty for an
// empty default or a switch that is off.
func Default(f *flag.Flag) string {
	if f.DefValue == "false" {
		return ""
	}
	return f.DefValue
}

// Flags holds the values of the helper's flags after the flag set parsed.
type Flags struct {
	Basedir     *string
	Sharedroot  *string
	Scoreboards *string
	Days        *int
	MinFree     *float64
	DryRun      *bool
	Verbose     *bool
	Format      *string
}

// DefineFlags defines the helper's flags on fs with their defaults and usage
// texts, and returns where their values land.
func DefineFlags(fs *flag.FlagSet) *Flags {
	return &Flags{
		Basedir:     fs.String("basedir", "auto", "The private root whose jobs and transcripts trees are pruned: auto (the operator's own, as karvi resolves it) or a path"),
		Sharedroot:  fs.String("sharedroot", "auto", "The site's shared root whose jobs and transcripts trees are pruned too: auto, none, or a path"),
		Scoreboards: fs.String("scoreboards", "/dev/shm/karvi/scoreboards", "The scoreboard directory (watch.directory)"),
		Days:        fs.Int("days", 31, "The retention age in days, one or more"),
		MinFree:     fs.Float64("minfree", 5, "The free-space floor in percent, under which the oldest eligible items go before their age; 0 turns pressure off"),
		DryRun:      fs.Bool("dry-run", false, "Report what would go and remove nothing"),
		Verbose:     fs.Bool("verbose", false, "Add one kept line per examined item that stays, with the reason"),
		Format:      fs.String("format", FormatText, "The report's form: text (an event word and key=value fields per line) or jsonl (one JSON document per line, the same fields, the summary last)"),
	}
}

// flagValues says what a flag's value may be: the words it takes, whether
// it also names a path the shell may complete, and, for a value that is
// neither, its name (a number Tab cannot know). A switch has no row.
// Completion offers the words and the path; Placeholder names the value
// from all three. TestDefineFlags holds every row to a defined flag, and
// TestFlagOrder every flag that takes a value to a row.
var flagValues = map[string]struct {
	words []string
	path  bool
	name  string
}{
	"basedir":     {words: []string{"auto"}, path: true},
	"sharedroot":  {words: []string{"auto", "none"}, path: true},
	"scoreboards": {path: true},
	"days":        {name: "N"},
	"minfree":     {name: "PERCENT"},
	"format":      {words: []string{FormatText, FormatJSONL}},
}
