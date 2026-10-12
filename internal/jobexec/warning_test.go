package jobexec

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/display"
)

// namedTerminal is a writer naming the terminal it writes to, as a recorded
// login's child's is (osutil.RawTerminalLines).
type namedTerminal struct {
	io.Writer
	f *os.File
}

func (n namedTerminal) Terminal() *os.File { return n.f }

// TestDisplayDestinationSeesThroughAWriterNamingItsTerminal: a writer that
// names the terminal it writes to is that terminal to the display's
// questions; one naming none, a buffer, and a pipe are elsewhere, and a
// regular file is a file.
func TestDisplayDestinationSeesThroughAWriterNamingItsTerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "terminal")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if DisplayDestination(namedTerminal{io.Discard, f}) != display.Terminal {
		t.Errorf("a writer naming its terminal is not one")
	}
	if DisplayDestination(namedTerminal{io.Discard, nil}) != display.Elsewhere || DisplayDestination(&bytes.Buffer{}) != display.Elsewhere {
		t.Errorf("a writer naming no terminal, or a buffer, taken for a terminal or a file")
	}
	if DisplayDestination(f) != display.File {
		t.Errorf("a regular file is not a file")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if DisplayDestination(w) != display.Elsewhere {
		t.Errorf("a pipe is not elsewhere")
	}
}

// TestWarningText: a warning's line is "warning: " and the message, the whole
// line in the warning colour when the style is on, plain when it is off.
func TestWarningText(t *testing.T) {
	if got := WarningText("x y", display.LineStyle{}); got != "warning: x y" {
		t.Errorf("off: %q", got)
	}
	on := display.LineStyle{Enabled: true, Warning: display.RoleColor("dark", "warning", "default")}
	got := WarningText("x y", on)
	if got != display.ANSIStyle("warning: x y", on.Warning, true, true) || !strings.HasPrefix(got, "\x1b[") || !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("on: %q", got)
	}
}
