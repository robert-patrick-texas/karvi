// Command mangen writes the generated regions of karvi-prune's man page,
// packaging/man/karvi-prune.8: the SYNOPSIS and the OPTIONS, from the
// helper's one flag definition (internal/prune: FlagOrder, the
// placeholders, the usage strings, the defaults). Everything outside the
// two regions is written by hand and kept byte for byte. A page whose
// marker pairs are missing, doubled, or out of order is refused and
// nothing is written. The output is deterministic, so make
// generated-clean and the bundle verifier compare it with the committed
// page.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/prune"
)

func main() {
	input := flag.String("input", "packaging/man/karvi-prune.8", "the page to read")
	output := flag.String("output", "", "where to write the page; the input itself when empty")
	flag.Parse()
	if *output == "" {
		*output = *input
	}
	page, err := os.ReadFile(*input)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mangen:", err)
		os.Exit(1)
	}
	out, err := generate(string(page), pruneRegions())
	if err != nil {
		fmt.Fprintf(os.Stderr, "mangen: %s: %v\n", *input, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, []byte(out), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "mangen:", err)
		os.Exit(1)
	}
}

// region is one generated part of a page: its name in the markers and its
// lines.
type region struct {
	name string
	body string
}

const markerSource = ": tools/mangen from internal/prune; edit there"

func begin(name string) string { return `.\" BEGIN GENERATED ` + name + markerSource }
func end(name string) string   { return `.\" END GENERATED ` + name }

// generate replaces each region's lines, in the order given, between its
// markers; the markers stay. Each marker must stand on a line of its own,
// once, and the regions in order.
func generate(page string, regions []region) (string, error) {
	lines := strings.SplitAfter(page, "\n")
	var out strings.Builder
	i := 0
	for _, r := range regions {
		for _, m := range []string{begin(r.name), end(r.name)} {
			if n := count(lines, m); n != 1 {
				return "", fmt.Errorf("the marker %q appears %d times, not once", m, n)
			}
		}
		b, e := find(lines, begin(r.name)), find(lines, end(r.name))
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
	var syn strings.Builder
	syn.WriteString(".B karvi\\-prune\n")
	for _, name := range prune.FlagOrder {
		if p := prune.Placeholder(name); p != "" {
			fmt.Fprintf(&syn, ".RB [ \"\\-\\-%s \\fI%s\\fP\" ]\n", escape(name), escape(p))
		} else {
			fmt.Fprintf(&syn, ".RB [ \\-\\-%s ]\n", escape(name))
		}
	}
	var opt strings.Builder
	for _, name := range prune.FlagOrder {
		f := fs.Lookup(name)
		opt.WriteString(".TP\n")
		if p := prune.Placeholder(name); p != "" {
			fmt.Fprintf(&opt, ".BI \\-\\-%s \" %s\"\n", escape(name), escape(p))
		} else {
			fmt.Fprintf(&opt, ".B \\-\\-%s\n", escape(name))
		}
		opt.WriteString(line(escape(f.Usage) + "."))
		if d := prune.Default(f); d != "" {
			opt.WriteString(line("Default: \\fB" + escape(d) + "\\fP."))
		}
	}
	return []region{{"SYNOPSIS", syn.String()}, {"OPTIONS", opt.String()}}
}

// escape makes text safe in roff: a backslash as \e and a hyphen as \-.
func escape(s string) string {
	return strings.NewReplacer(`\`, `\e`, "-", `\-`).Replace(s)
}

// line is one text line of roff: a leading . or ' would be a request, so
// it is protected with \&.
func line(s string) string {
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "'") {
		s = `\&` + s
	}
	return s + "\n"
}
