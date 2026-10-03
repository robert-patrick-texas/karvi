// Command mangen writes the generated regions of the manual pages in
// packaging/man. Each page of its table has its regions: karvi-prune.8 its
// SYNOPSIS and OPTIONS from the helper's one flag definition
// (internal/prune: FlagOrder, the placeholders, the usage strings, the
// defaults); karvi.1 and each karvi-WORD.1 their SYNOPSIS and DESCRIPTION
// from the word's help text (internal/cli.HelpPages, laid out by
// internal/helplayout.Roff), and karvi.1 its EXIT STATUS from
// internal/exitcode. Everything outside the regions is written by hand and
// kept byte for byte. A page whose marker pairs are missing, doubled, or
// out of order is refused, as is a command word without its page, and
// nothing is written. The output is deterministic, so make generated-clean
// and the bundle verifier compare it with the committed pages.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/cli"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/helplayout"
	"github.com/robert-patrick-texas/karvi/internal/prune"
)

func main() {
	src := flag.String("src", "packaging/man", "the directory of the committed pages")
	dir := flag.String("dir", "", "where to write the generated pages; the source directory itself when empty")
	flag.Parse()
	if *dir == "" {
		*dir = *src
	}
	out, err := render(*src)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mangen:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mangen:", err)
		os.Exit(1)
	}
	for _, p := range pages() {
		if err := os.WriteFile(filepath.Join(*dir, p.file), []byte(out[p.file]), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "mangen:", err)
			os.Exit(1)
		}
	}
}

// page is one manual page of the table: its file in packaging/man and its
// generated regions.
type page struct {
	file    string
	regions []region
}

// region is one generated part of a page: its name in the markers, the
// package it is generated from, and its lines.
type region struct {
	name   string
	source string
	body   string
}

func pages() []page {
	ps := []page{{"karvi-prune.8", pruneRegions()}}
	for _, h := range cli.HelpPages() {
		file := "karvi.1"
		if h.Word != "" {
			file = "karvi-" + h.Word + ".1"
		}
		syn, desc := helplayout.Roff(h.Text)
		regions := []region{{"SYNOPSIS", "internal/cli", syn}, {"DESCRIPTION", "internal/cli", desc}}
		if h.Word == "" {
			regions = append(regions, region{"EXIT STATUS", "internal/exitcode", exitRegion()})
		}
		ps = append(ps, page{file, regions})
	}
	return ps
}

// render reads every page of the table from src and returns each with its
// regions written, or the first refusal; a command word without its page
// is refused by name.
func render(src string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range pages() {
		b, err := os.ReadFile(filepath.Join(src, p.file))
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s does not exist: every page of the table needs its hand-written frame", filepath.Join(src, p.file))
		}
		if err != nil {
			return nil, err
		}
		g, err := generate(string(b), p.regions)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.file, err)
		}
		out[p.file] = g
	}
	return out, nil
}

func begin(r region) string {
	return `.\" BEGIN GENERATED ` + r.name + ": tools/mangen from " + r.source + "; edit there"
}
func end(r region) string { return `.\" END GENERATED ` + r.name }

// generate replaces each region's lines, in the order given, between its
// markers; the markers stay. Each marker must stand on a line of its own,
// once, and the regions in order.
func generate(page string, regions []region) (string, error) {
	lines := strings.SplitAfter(page, "\n")
	var out strings.Builder
	i := 0
	for _, r := range regions {
		for _, m := range []string{begin(r), end(r)} {
			if n := count(lines, m); n != 1 {
				return "", fmt.Errorf("the marker %q appears %d times, not once", m, n)
			}
		}
		b, e := find(lines, begin(r)), find(lines, end(r))
		if b < i || e < b {
			return "", fmt.Errorf("the %s markers are out of order", r.name)
		}
		for ; i <= b; i++ {
			out.WriteString(lines[i])
		}
		out.WriteString(r.body)
		i = e
	}
	for ; i < len(lines); i++ {
		out.WriteString(lines[i])
	}
	return out.String(), nil
}

func count(lines []string, marker string) int {
	n := 0
	for _, l := range lines {
		if strings.TrimSuffix(l, "\n") == marker {
			n++
		}
	}
	return n
}

func find(lines []string, marker string) int {
	for i, l := range lines {
		if strings.TrimSuffix(l, "\n") == marker {
			return i
		}
	}
	return -1
}

// pruneRegions are karvi-prune's SYNOPSIS and OPTIONS from its flag set.
func pruneRegions() []region {
	fs := flag.NewFlagSet("karvi-prune", flag.ContinueOnError)
	prune.DefineFlags(fs)
	esc := helplayout.RoffEscape
	var syn strings.Builder
	syn.WriteString(".B karvi\\-prune\n")
	for _, name := range prune.FlagOrder {
		if p := prune.Placeholder(name); p != "" {
			fmt.Fprintf(&syn, ".RB [ \"\\-\\-%s \\fI%s\\fP\" ]\n", esc(name), esc(p))
		} else {
			fmt.Fprintf(&syn, ".RB [ \\-\\-%s ]\n", esc(name))
		}
	}
	var opt strings.Builder
	for _, name := range prune.FlagOrder {
		f := fs.Lookup(name)
		opt.WriteString(".TP\n")
		if p := prune.Placeholder(name); p != "" {
			fmt.Fprintf(&opt, ".BI \\-\\-%s \" %s\"\n", esc(name), esc(p))
		} else {
			fmt.Fprintf(&opt, ".B \\-\\-%s\n", esc(name))
		}
		opt.WriteString(helplayout.RoffLine(esc(f.Usage) + "."))
		if d := prune.Default(f); d != "" {
			opt.WriteString(helplayout.RoffLine("Default: \\fB" + esc(d) + "\\fP."))
		}
	}
	return []region{{"SYNOPSIS", "internal/prune", syn.String()}, {"OPTIONS", "internal/prune", opt.String()}}
}

// exitRegion is karvi.1's EXIT STATUS entries, one per status of
// internal/exitcode: the number in bold, the name, and the meaning.
func exitRegion() string {
	var b strings.Builder
	for _, s := range exitcode.Statuses {
		fmt.Fprintf(&b, ".TP\n.B %d\n", s.Code)
		b.WriteString(helplayout.RoffLine(`\fI` + helplayout.RoffEscape(s.Name) + `\fR: ` + helplayout.RoffEscape(s.Meaning)))
	}
	return b.String()
}
