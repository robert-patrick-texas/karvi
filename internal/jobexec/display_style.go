package jobexec

import (
	"io"
	"os"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// DisplayTerminal reports whether the original operator output stream is a
// terminal. Callers must evaluate this before wrapping the stream in a
// transcript MultiWriter so color=auto continues to reflect the real terminal.
func DisplayTerminal(out io.Writer) bool {
	return terminalFile(out) != nil
}

// terminalFile is the terminal out writes to: out itself at a terminal, or
// the terminal a writer that wraps one names (osutil.RawTerminalLines, a
// recorded login's child writing to its wrapper's terminal); nil otherwise.
func terminalFile(out io.Writer) *os.File {
	if t, ok := out.(interface{ Terminal() *os.File }); ok {
		return t.Terminal()
	}
	if f, ok := out.(*os.File); ok && osutil.IsTerminal(f) {
		return f
	}
	return nil
}

// DisplayLineStyle resolves all semantic colors once per invocation. Templates
// remain plain text in configuration; operators never need to embed ANSI
// escape sequences to distinguish identity, address, labels, and values.
func DisplayLineStyle(cfg configload.Snapshot, terminal bool) display.LineStyle {
	theme := cfg.String("display.theme")
	return display.LineStyle{
		Enabled:   display.ColorEnabled(cfg.String("display.color"), theme, terminal),
		Target:    display.RoleColor(theme, "target", cfg.String("display.colors.target")),
		Address:   display.RoleColor(theme, "address", cfg.String("display.colors.address")),
		Label:     display.RoleColor(theme, "label", cfg.String("display.colors.label")),
		Value:     display.RoleColor(theme, "value", cfg.String("display.colors.value")),
		Timestamp: display.RoleColor(theme, "timestamp", cfg.String("display.colors.timestamp")),
		Accent:    display.RoleColor(theme, "accent", cfg.String("display.colors.accent")),
		Success:   display.RoleColor(theme, "success", cfg.String("display.colors.success")),
		Warning:   display.RoleColor(theme, "warning", cfg.String("display.colors.warning")),
		Error:     display.RoleColor(theme, "error", cfg.String("display.colors.error")),
		Muted:     display.RoleColor(theme, "muted", cfg.String("display.colors.muted")),
	}
}

func DisplayBorderColor(cfg configload.Snapshot) string {
	return display.RoleColor(cfg.String("display.theme"), "border", cfg.String("display.colors.border"))
}

func DisplayDynamicBorderColor(cfg configload.Snapshot) string {
	return display.RoleColor(cfg.String("display.theme"), "dynamic-border", cfg.String("display.colors.dynamic-border"))
}

func DisplayTerminalWidth(out io.Writer) int {
	f := terminalFile(out)
	if f == nil {
		return 0
	}
	return osutil.TerminalWidth(f)
}
