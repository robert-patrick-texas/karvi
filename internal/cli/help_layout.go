package cli

import (
	"io"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/helplayout"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// The help texts (help.go) are written as plain columns and kept in step
// with the parser table by the drift test; internal/helplayout lays them
// out for the terminal, and this file decides the colours.
//
// Colour follows the display's own rule (display.color, display.theme, and
// whether standard output is a terminal), read from the configuration the
// invocation names; --ansi strip turns it off, as it strips every escape
// from the output. A configuration that does not load, or an operator
// identity that cannot be read, gives plain help: help never fails for a
// reason that is not the help's.

// helpStyleFor decides the style for a help request under the invocation's
// global options: the configuration is loaded as `config show` loads it
// (the same roots, --set values, and flag values), and nothing is created.
func helpStyleFor(g globalOptions, stdout io.Writer) helplayout.Style {
	if g.ansi == "strip" {
		return helplayout.Style{}
	}
	op, err := osutil.CurrentOperator()
	if err != nil {
		return helplayout.Style{}
	}
	cfg, err := configload.Load(configload.Options{ExplicitRoots: g.configs, Sets: g.sets, FlagValues: g.flags(), HomeDir: op.Home})
	if err != nil {
		return helplayout.Style{}
	}
	theme := cfg.String("display.theme")
	return helplayout.Style{
		Enabled: display.ColorEnabled(cfg.String("display.color"), theme, jobexec.DisplayTerminal(stdout)),
		Accent:  display.RoleColor(theme, "accent", cfg.String("display.colors.accent")),
		Action:  display.RoleColor(theme, "success", cfg.String("display.colors.success")),
		Label:   display.RoleColor(theme, "label", cfg.String("display.colors.label")),
	}
}
