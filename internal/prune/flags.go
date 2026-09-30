package prune

import "flag"

// The helper's command line lives here once: the
// helper's main defines its flag set through DefineFlags and prints Usage,
// and the completer (complete.go) walks the same set, so Tab cannot offer a
// flag the helper does not take, and a later reader of the flags (the
// man page) has one definition to read. The helper reads no configuration:
// the flags carry the settings' words, and a unit's line holds a site's
// values.

// Usage is the helper's synopsis line, printed before the flags' defaults.
const Usage = "usage: karvi-prune [--basedir auto|PATH] [--sharedroot auto|none|PATH] [--scoreboards PATH] [--days N] [--minfree PERCENT] [--dry-run] [--verbose] [--format text|jsonl]"

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

// flagValues says what a flag's value may be, for completion: the words it
// takes, and whether it also names a path the shell may complete. A flag
// with no row takes a value Tab cannot know (a number). TestDefineFlags
// holds every row to a defined flag.
var flagValues = map[string]struct {
	words []string
	path  bool
}{
	"basedir":     {words: []string{"auto"}, path: true},
	"sharedroot":  {words: []string{"auto", "none"}, path: true},
	"scoreboards": {path: true},
	"format":      {words: []string{FormatText, FormatJSONL}},
}
