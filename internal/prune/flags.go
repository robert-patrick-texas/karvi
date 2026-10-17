package prune

import (
	"flag"

	"github.com/robert-patrick-texas/karvi/internal/flagspec"
)

// The helper's command line lives here once, as a flagspec.Spec: the
// helper's main defines its flag set through DefineFlags and prints the
// spec's Help, the completer walks the same set, and the man page's SYNOPSIS
// and OPTIONS are generated from it (tools/mangen). The helper reads no
// configuration: the flags carry the settings' words, and a unit's line
// holds a site's values.

// Spec is the helper's command line: the flags in the synopsis's order
// (TestFlagOrder holds it to the defined flags) and what each flag's value
// may be: the words it takes, whether it also names a path the shell may
// complete, and, for a value that is neither, its name. TestDefineFlags
// holds every value row to a defined flag, and TestFlagOrder every flag
// that takes a value to a row.
var Spec = flagspec.Spec{
	Program: "karvi-prune",
	Title:   "karvi-prune - remove finished karvi work older than the retention age",
	Order:   []string{"basedir", "sharedroot", "scoreboards", "days", "minfree", "dry-run", "verbose", "format"},
	Values: map[string]flagspec.Value{
		"basedir":     {Words: []string{"auto"}, Path: true},
		"sharedroot":  {Words: []string{"auto", "none"}, Path: true},
		"scoreboards": {Path: true},
		"days":        {Name: "N"},
		"minfree":     {Name: "PERCENT"},
		"format":      {Words: []string{FormatText, FormatJSONL}},
	},
	Define: func(fs *flag.FlagSet) { DefineFlags(fs) },
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
		Basedir:     fs.String("basedir", "auto", "The private roots whose jobs, transcripts, and scoreboards are pruned: auto (every root of the operator's that exists; as root, every site user root) or a path, that one alone"),
		Sharedroot:  fs.String("sharedroot", "auto", "The site's shared root whose jobs and transcripts trees are pruned too: auto, none, or a path"),
		Scoreboards: fs.String("scoreboards", "/dev/shm/karvi/scoreboards", "The shared scoreboards, pruned beside each private root's state/scoreboards"),
		Days:        fs.Int("days", 31, "The retention age in days, one or more"),
		MinFree:     fs.Float64("minfree", 5, "The free-space floor in percent, under which the oldest eligible items go before their age; 0 turns pressure off"),
		DryRun:      fs.Bool("dry-run", false, "Report what would go and remove nothing"),
		Verbose:     fs.Bool("verbose", false, "Add one kept line per examined item that stays, with the reason"),
		Format:      fs.String("format", FormatText, "The report's form: text (an event word and key=value fields per line) or jsonl (one JSON document per line, the same fields, the summary last)"),
	}
}
