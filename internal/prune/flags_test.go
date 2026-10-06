package prune

import (
	"flag"
	"strings"
	"testing"
)

// TestFlagOrder holds FlagOrder to the defined flags (every one, once, no
// other) and the value names to the kinds: every flag that takes a value
// has a placeholder, and a switch has none.
func TestFlagOrder(t *testing.T) {
	fs := flag.NewFlagSet("karvi-prune", flag.ContinueOnError)
	DefineFlags(fs)
	seen := map[string]bool{}
	for _, name := range FlagOrder {
		f := fs.Lookup(name)
		if f == nil || seen[name] {
			t.Fatalf("FlagOrder names %q: undefined or twice", name)
		}
		seen[name] = true
		if switched := isBoolFlag(f); switched == (Placeholder(name) != "") {
			t.Errorf("--%s: switch %v, placeholder %q", name, switched, Placeholder(name))
		}
	}
	fs.VisitAll(func(f *flag.Flag) {
		if !seen[f.Name] {
			t.Errorf("--%s is not in FlagOrder", f.Name)
		}
	})
}

// TestUsage pins the synopsis built from the order and the placeholders
// to the line the helper printed when it was a constant.
func TestUsage(t *testing.T) {
	const want = "usage: karvi-prune [--basedir auto|PATH] [--sharedroot auto|none|PATH] [--scoreboards PATH] [--days N] [--minfree PERCENT] [--dry-run] [--verbose] [--format text|jsonl]"
	if got := Usage(); got != want {
		t.Fatalf("Usage()\n got %q\nwant %q", got, want)
	}
}

// TestHelp: the help in karvi's shape, as helplayout reads it: the title,
// the Usage section's synopsis wrapped under its first bracket at 79, the
// Options section with each flag's words in the field and its description
// from column 34, wrapped at 79, the default after, in FlagOrder.
func TestHelp(t *testing.T) {
	fs := flag.NewFlagSet("karvi-prune", flag.ContinueOnError)
	DefineFlags(fs)
	got := Help(fs)
	want := `karvi-prune - remove finished karvi work older than the retention age

Usage:
  karvi-prune [--basedir auto|PATH] [--sharedroot auto|none|PATH]
              [--scoreboards PATH] [--days N] [--minfree PERCENT] [--dry-run]
              [--verbose] [--format text|jsonl]

Options:
  --basedir auto|PATH            The private roots whose jobs, transcripts, and
                                 scoreboards are pruned: auto (every root of
                                 the operator's that exists; as root, every
                                 site user root) or a path, that one alone
                                 (default auto)
  --sharedroot auto|none|PATH    The site's shared root whose jobs and
                                 transcripts trees are pruned too: auto, none,
                                 or a path (default auto)
  --scoreboards PATH             The shared scoreboards, pruned beside each
                                 private root's state/scoreboards (default
                                 /dev/shm/karvi/scoreboards)
  --days N                       The retention age in days, one or more
                                 (default 31)
  --minfree PERCENT              The free-space floor in percent, under which
                                 the oldest eligible items go before their age;
                                 0 turns pressure off (default 5)
  --dry-run                      Report what would go and remove nothing
  --verbose                      Add one kept line per examined item that
                                 stays, with the reason
  --format text|jsonl            The report's form: text (an event word and
                                 key=value fields per line) or jsonl (one JSON
                                 document per line, the same fields, the
                                 summary last) (default text)
`
	if got != want {
		t.Fatalf("Help\n%s\nwant\n%s", got, want)
	}
	for _, l := range strings.Split(got, "\n") {
		if len(l) > 79 {
			t.Errorf("over 79: %q", l)
		}
	}
}
