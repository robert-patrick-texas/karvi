package prune

import (
	"bytes"
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
// to the line the helper printed when it was a constant, and -h to the
// double dash, the placeholder, and an unquoted default; a switch shows
// none.
func TestUsage(t *testing.T) {
	const want = "usage: karvi-prune [--basedir auto|PATH] [--sharedroot auto|none|PATH] [--scoreboards PATH] [--days N] [--minfree PERCENT] [--dry-run] [--verbose] [--format text|jsonl]"
	if got := Usage(); got != want {
		t.Fatalf("Usage()\n got %q\nwant %q", got, want)
	}
	fs := flag.NewFlagSet("karvi-prune", flag.ContinueOnError)
	DefineFlags(fs)
	var b bytes.Buffer
	PrintUsage(&b, fs)
	out := b.String()
	for _, line := range []string{
		want + "\n",
		"  --basedir auto|PATH\n        the private root whose jobs and transcripts trees are pruned: auto (the operator's own, as karvi resolves it) or a path (default auto)\n",
		"  --days N\n        the retention age in days, one or more (default 31)\n",
		"  --minfree PERCENT\n",
		"  --dry-run\n        report what would go and remove nothing\n",
		"  --format text|jsonl\n",
		"(default /dev/shm/karvi/scoreboards)\n",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("-h lacks %q:\n%s", line, out)
		}
	}
	if strings.Contains(out, "\n  -basedir") || strings.Contains(out, " string\n") || strings.Contains(out, `"auto"`) {
		t.Errorf("-h keeps the flag package's form:\n%s", out)
	}
	if strings.Index(out, "--basedir") > strings.Index(out, "--days") || strings.Index(out, "--verbose") > strings.Index(out, "--format") {
		t.Errorf("-h is not in the synopsis's order:\n%s", out)
	}
}
