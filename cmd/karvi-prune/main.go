// Command karvi-prune is the retention helper: the units and the cron script
// run it daily, and an operator runs it by hand, `karvi-prune --dry-run`
// first. It reads no configuration: its flags carry the settings' words
// (basedir, sharedroot) and the unit's line holds a site's values; the flags
// are defined once in the prune package, which the helper's tab completion
// reads too. The report is one line per item on standard output with the
// summary last (`--format jsonl` the same lines as JSON documents),
// `--verbose` adds why an item stays, and the exit is 0 when everything
// eligible went, 1 when a removal failed or a tree could not be walked, 2 for
// a usage error.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/robert-patrick-texas/karvi/internal/completion"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/helplayout"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/prune"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	// The hidden word stands outside the flags: it
	// is taken before parsing, since the words it carries are the line being
	// completed, not this invocation's.
	if len(args) > 0 && args[0] == completion.Word {
		return prune.Complete(args[1:], stdout)
	}
	fs := flag.NewFlagSet("karvi-prune", flag.ContinueOnError)
	fs.SetOutput(stderr)
	f := prune.DefineFlags(fs)
	// The help is printed here, not by the flag package: -h and --help to
	// standard output, a usage error's text after its message on standard
	// error, laid out as karvi's help is.
	fs.Usage = func() {}
	help := func(w *os.File) { fmt.Fprint(w, helplayout.Layout(prune.Help(fs), helpStyle(w))) }
	usageError := func(format string, a ...any) int {
		if format != "" {
			fmt.Fprintf(stderr, "karvi-prune: "+format+"\n", a...)
		}
		help(stderr)
		return 2
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			help(stdout)
			return 0
		}
		// The flag package has said what was wrong.
		return usageError("")
	}
	if len(fs.Args()) != 0 {
		return usageError("no positional argument is taken")
	}
	if *f.Days < 1 {
		return usageError("--days takes one or more, not %d", *f.Days)
	}
	if *f.MinFree < 0 || *f.MinFree > 100 {
		return usageError("--minfree takes a percentage from 0 to 100, not %g", *f.MinFree)
	}
	if *f.Format != prune.FormatText && *f.Format != prune.FormatJSONL {
		return usageError("--format takes text or jsonl, not %q", *f.Format)
	}
	root := *f.Basedir
	var siteRoots []string
	uid := os.Getuid()
	switch {
	case root == "auto" && uid == 0:
		// Root's own basedir holds no operator's jobs (and resolving it
		// would create one under /root): a root run walks the private roots
		// the site provisioned under the system roots instead, then the
		// shared trees (docs/PRUNE.md). A root under
		// an operator's home is that operator's own run's.
		root = ""
		siteRoots = osutil.SiteUserRoots()
	case root == "auto":
		op, err := osutil.CurrentOperator()
		if err != nil {
			fmt.Fprintln(stderr, "karvi-prune:", err)
			return 1
		}
		root, err = osutil.ResolveBaseDir("auto", op.Home, op.Username)
		if err != nil {
			fmt.Fprintln(stderr, "karvi-prune:", err)
			return 1
		}
	}
	// The run removes what the invoking user owns; root removes everything
	// eligible.
	opts := prune.Options{UserRoot: root, SiteRoots: siteRoots, SharedRoot: *f.Sharedroot, ScoreboardRoot: *f.Scoreboards, Days: *f.Days, MinFreePercent: *f.MinFree, DryRun: *f.DryRun, Verbose: *f.Verbose, Format: *f.Format, UID: uid}
	result, err := prune.Run(opts, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "karvi-prune:", err)
		return 1
	}
	prune.Summary(stdout, opts, result)
	// A removal that failed is in its line and here; the run went on past
	// it, and the exit says the tree is not as the policy wants it.
	if result.Failed > 0 {
		return 1
	}
	return 0
}

// helpStyle is the help's colour, decided without configuration, which the
// helper never reads: display.color's default, auto (colour when w is a
// terminal), and display.theme's default, dark, for the roles.
func helpStyle(w *os.File) helplayout.Style {
	const theme = "dark"
	return helplayout.Style{
		Enabled: display.ColorEnabled("auto", theme, osutil.IsTerminal(w)),
		Accent:  display.RoleColor(theme, "accent", "default"),
		Action:  display.RoleColor(theme, "success", "default"),
		Label:   display.RoleColor(theme, "label", "default"),
	}
}
