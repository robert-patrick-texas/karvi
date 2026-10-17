// Package auditview is karvi-audit: it reads the audit trail by running
// journalctl with the trail's matches and shows each event as one row of a
// table (docs/EXAMPLES.md, chapter 64). Following, reversing, the pattern,
// the window, and who reads whose events are journalctl's; the matches, the
// rows, the layout, the colours, and the run are this package's.
package auditview

import (
	"errors"
	"flag"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/flagspec"
)

// The reader's command line lives here once, as a flagspec.Spec: the
// reader's main defines its flag set through DefineFlags and prints the
// spec's Help, the completer walks the same set, and the man page's SYNOPSIS
// and OPTIONS are generated from it (tools/mangen). Each flag takes one or
// two dashes and its value after = or a space (Go's flag package), and all
// but --nocolor have a one-letter spelling. The reader reads no
// configuration.

// Spec is the reader's command line: the flags in the synopsis's order, the
// values' names, and the one-letter spellings. TestSpec holds them to the
// defined flags.
var Spec = flagspec.Spec{
	Program: "karvi-audit",
	Title:   "karvi-audit - show karvi's audit trail from journald, one event per row",
	Order:   []string{"follow", "reverse", "grep", "operator", "device", "lines", "since", "until", "nocolor"},
	Values: map[string]flagspec.Value{
		"grep":     {Name: "PATTERN"},
		"operator": {Name: "NAME", Repeat: true},
		"device":   {Name: "NAME", Repeat: true},
		"lines":    {Name: "N"},
		"since":    {Name: "TIME"},
		"until":    {Name: "TIME"},
	},
	Short:  short,
	Define: func(fs *flag.FlagSet) { DefineFlags(fs) },
}

// short is each flag's one-letter spelling, all but --nocolor's.
var short = map[string]string{"follow": "f", "reverse": "r", "grep": "g", "operator": "o", "device": "d", "lines": "n", "since": "S", "until": "U"}

// Flags holds the values of the reader's flags after the flag set parsed.
type Flags struct {
	Follow, Reverse, NoColor *bool
	Grep, Since, Until       *string
	Operators, Devices       *names
	Lines                    *count
}

// DefineFlags defines the reader's flags on fs, each long name and its
// one-letter spelling on the same value, and returns where their values land.
func DefineFlags(fs *flag.FlagSet) *Flags {
	f := &Flags{Follow: new(bool), Reverse: new(bool), NoColor: new(bool), Grep: new(string), Since: new(string), Until: new(string), Operators: &names{}, Devices: &names{}, Lines: &count{}}
	boolean := func(p *bool, long, usage string) {
		fs.BoolVar(p, long, false, usage)
		if s := short[long]; s != "" {
			fs.BoolVar(p, s, false, usage)
		}
	}
	text := func(p *string, long, usage string) {
		fs.StringVar(p, long, "", usage)
		fs.StringVar(p, short[long], "", usage)
	}
	value := func(v flag.Value, long, usage string) {
		fs.Var(v, long, usage)
		fs.Var(v, short[long], usage)
	}
	boolean(f.Follow, "follow", "Show the newest events, then each new one as it comes, until Ctrl-C (journalctl's --follow)")
	boolean(f.Reverse, "reverse", "The newest event first (journalctl's --reverse)")
	text(f.Grep, "grep", "The events whose line matches PATTERN, a job's ID or a statement's words among them (journalctl's --grep: a regular expression, its case ignored when it is all lowercase)")
	value(f.Operators, "operator", "The events of NAME, as the operator or as the login user behind sudo, matched whole; repeatable")
	value(f.Devices, "device", "The events at the device NAME, lowercased and matched whole; repeatable")
	value(f.Lines, "lines", "The newest N events (journalctl's --lines); every event when not given, ten under --follow")
	text(f.Since, "since", "The events at or after TIME (journalctl's --since: 2026-10-17 14:00, yesterday, -1h)")
	text(f.Until, "until", "The events at or before TIME (journalctl's --until)")
	boolean(f.NoColor, "nocolor", "No colour, at a terminal or through a pipe")
	return f
}

// names is a repeatable flag's values in the order given.
type names []string

func (n *names) String() string { return strings.Join(*n, ",") }
func (n *names) Set(v string) error {
	*n = append(*n, v)
	return nil
}

// count is --lines: a number, and whether it was given, journalctl's own
// default standing when it was not.
type count struct {
	n   int
	set bool
}

func (c *count) String() string {
	if !c.set {
		return ""
	}
	return strconv.Itoa(c.n)
}
func (c *count) Set(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		// The flag package's own words for a number it cannot parse, as
		// karvi-prune's --days x says them: `invalid value "x" for flag -n:
		// parse error`.
		return errors.New("parse error")
	}
	c.n, c.set = n, true
	return nil
}
