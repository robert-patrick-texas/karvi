package prune

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// The helper's command line lives here once: the helper's main defines its
// flag set through DefineFlags and prints PrintUsage, the completer
// (complete.go) walks the same set, and the man page's SYNOPSIS and OPTIONS
// are generated from it (tools/mangen), so Tab cannot offer a flag the
// helper does not take and no text can name a value otherwise. The helper
// reads no configuration: the flags carry the settings' words, and a unit's
// line holds a site's values.

// FlagOrder is the flags in the synopsis's order; the flag package itself
// offers them alphabetically. TestFlagOrder holds it to the defined flags.
var FlagOrder = []string{"basedir", "sharedroot", "scoreboards", "days", "minfree", "dry-run", "verbose", "format"}

// Placeholder is the name of a flag's value, as the synopsis, -h, and the
// man page print it: its words joined with |, then PATH when it names a
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

// PrintUsage is -h: the synopsis, then each flag in FlagOrder with its
// placeholder, its usage string, and its default when it has one (not
// empty, not false), unquoted.
func PrintUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintln(w, Usage())
	for _, name := range FlagOrder {
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		line := "  --" + name
		if p := Placeholder(name); p != "" {
			line += " " + p
		}
		usage := f.Usage
		if d := Default(f); d != "" {
			usage += " (default " + d + ")"
		}
		fmt.Fprintf(w, "%s\n        %s\n", line, usage)
	}
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
		Basedir:     fs.String("basedir", "auto", "the private root whose jobs and transcripts trees are pruned: auto (the operator's own, as karvi resolves it) or a path"),
		Sharedroot:  fs.String("sharedroot", "auto", "the site's shared root whose jobs and transcripts trees are pruned too: auto, none, or a path"),
		Scoreboards: fs.String("scoreboards", "/dev/shm/karvi/scoreboards", "the scoreboard directory (watch.directory)"),
		Days:        fs.Int("days", 31, "the retention age in days, one or more"),
		MinFree:     fs.Float64("minfree", 5, "the free-space floor in percent, under which the oldest eligible items go before their age; 0 turns pressure off"),
		DryRun:      fs.Bool("dry-run", false, "report what would go and remove nothing"),
		Verbose:     fs.Bool("verbose", false, "add one kept line per examined item that stays, with the reason"),
		Format:      fs.String("format", FormatText, "the report's form: text (an event word and key=value fields per line) or jsonl (one JSON document per line, the same fields, the summary last)"),
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
