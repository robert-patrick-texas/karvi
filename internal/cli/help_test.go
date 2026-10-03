package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/helplayout"
)

var helpOptionToken = regexp.MustCompile(`--([a-z0-9][a-z0-9-]*)`)

// TestHelpMatchesParserTable keeps the handwritten help texts and the parser
// table in step: every --word in a help text names an option or alias valid
// for a command that uses that text, and every option of every command appears
// in its help.
func TestHelpMatchesParserTable(t *testing.T) {
	type group struct {
		text    string
		allowed map[string]bool
		cmds    []*command
	}
	groups := map[string]*group{}
	add := func(c *command) {
		text := c.help()
		g, ok := groups[text]
		if !ok {
			g = &group{text: text, allowed: map[string]bool{}}
			groups[text] = g
		}
		g.cmds = append(g.cmds, c)
		for _, o := range c.options {
			g.allowed[o.name] = true
			for _, a := range o.aliases {
				g.allowed[a] = true
			}
		}
	}
	for _, c := range commandTable {
		if c.hidden {
			continue
		}
		add(c)
		for _, s := range c.subs {
			if !s.hidden {
				add(s)
			}
		}
	}
	// Top-level help explains global options and gives examples from every mode.
	top := &group{text: topHelp, allowed: map[string]bool{}}
	for _, o := range globalOptionTable {
		top.allowed[o.name] = true
		for _, a := range o.aliases {
			top.allowed[a] = true
		}
	}
	for _, g := range groups {
		for k := range g.allowed {
			top.allowed[k] = true
		}
	}
	// stream's lines carry run's options, and its directives are lines,
	// not options of the table.
	if g, ok := groups[streamHelp]; ok {
		for _, o := range runOptions {
			g.allowed[o.name] = true
			for _, a := range o.aliases {
				g.allowed[a] = true
			}
		}
		for d := range streamDirectives {
			g.allowed[d] = true
		}
		for _, o := range globalOptionTable { // named as going before the word
			g.allowed[o.name] = true
		}
	}
	groups["top"] = top

	for name, g := range groups {
		for _, m := range helpOptionToken.FindAllStringSubmatch(g.text, -1) {
			if !g.allowed[m[1]] {
				t.Errorf("help for %s mentions --%s, which the parser table does not accept there", label(name, g.cmds), m[1])
			}
		}
		for _, c := range g.cmds {
			for _, o := range c.options {
				if !strings.Contains(g.text, "--"+o.name) {
					t.Errorf("help for %s does not list --%s", c.path, o.name)
				}
			}
		}
	}
	for _, o := range globalOptionTable {
		if !strings.Contains(topHelp, "--"+o.name) {
			t.Errorf("top help does not list global option --%s", o.name)
		}
	}
}

func label(name string, cmds []*command) string {
	if len(cmds) == 0 {
		return name
	}
	var paths []string
	for _, c := range cmds {
		paths = append(paths, c.path)
	}
	return strings.Join(paths, "/")
}

// TestHelpShapesRegular: no help text holds a line in a shape the layout
// does not know (helplayout.Irregular), which the terminal would show as
// prose and the manual page would run into a paragraph.
func TestHelpShapesRegular(t *testing.T) {
	texts := []string{topHelp}
	for _, c := range commandTable {
		texts = append(texts, c.help())
	}
	for _, text := range texts {
		for _, l := range helplayout.Irregular(text) {
			t.Errorf("help line in no known shape: %q", l)
		}
	}
}
