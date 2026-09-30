package watchui

import (
	"io"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/display"
)

// The screen model. A frame is the
// whole screen as lines in memory, one per terminal row, no newline in any
// of them. Screen keeps the frame the terminal shows and, at each Draw,
// writes only the lines that differ, each addressed by its row and erased
// to the end of the line, in one write: over SSH a two-second refresh moves
// a few cells, not the screen. Enter takes the alternate screen and hides
// the cursor; Leave gives both back, so the shell is as it was. Nothing
// here reads the terminal: the size and the keys come from the caller
// (the tty on a live run, the test's values against a buffer).

// The control sequences the screen writes, named once.
const (
	altScreenOn  = "\x1b[?1049h" // the alternate screen, saved cursor
	altScreenOff = "\x1b[?1049l"
	cursorHide   = "\x1b[?25l"
	cursorShow   = "\x1b[?25h"
	clearScreen  = "\x1b[H\x1b[2J" // home, then erase the display
	eraseToEnd   = "\x1b[K"        // from the cursor to the end of the line
	resetStyle   = "\x1b[0m"
)

// cursorTo addresses row and column, both counted from 1.
func cursorTo(row, col int) string {
	return "\x1b[" + strconv.Itoa(row) + ";" + strconv.Itoa(col) + "H"
}

// Screen holds what the terminal shows and writes the difference.
type Screen struct {
	out       io.Writer
	rows      int
	cols      int
	shown     []string // the lines on the terminal after the last Draw
	redrawAll bool     // the next Draw writes every row: first draw, resize
}

// NewScreen makes a screen of rows by cols over out, nothing drawn yet.
func NewScreen(out io.Writer, rows, cols int) *Screen {
	return &Screen{out: out, rows: rows, cols: cols, redrawAll: true}
}

// Size is the screen's rows and columns as last set.
func (s *Screen) Size() (rows, cols int) { return s.rows, s.cols }

// Resize takes the terminal's new size; the next Draw rewrites every row,
// since the terminal reflowed what it showed.
func (s *Screen) Resize(rows, cols int) {
	s.rows, s.cols = rows, cols
	s.shown = nil
	s.redrawAll = true
}

// Enter takes the alternate screen, hides the cursor and clears the screen.
func (s *Screen) Enter() error {
	s.shown = nil
	s.redrawAll = true
	_, err := io.WriteString(s.out, altScreenOn+cursorHide+clearScreen)
	return err
}

// Leave resets the style, shows the cursor and drops the alternate screen:
// the shell's screen returns as it was, the cursor where it stood.
func (s *Screen) Leave() error {
	_, err := io.WriteString(s.out, resetStyle+cursorShow+altScreenOff)
	return err
}

// Draw shows a frame: the lines are fitted to the screen (rows past the
// height dropped, each line cut at the width by its visible cells, an
// escape sequence counted at zero), then every fitted line that differs
// from the one shown is written at its row and erased to the line's end. A
// frame shorter than the last leaves its missing rows erased. The whole
// difference goes to the writer in one call.
func (s *Screen) Draw(frame []string) error {
	if s.rows <= 0 || s.cols <= 0 {
		return nil
	}
	fitted := Fit(frame, s.rows, s.cols)
	var b strings.Builder
	for i, line := range fitted {
		if !s.redrawAll && i < len(s.shown) && s.shown[i] == line {
			continue
		}
		b.WriteString(cursorTo(i+1, 1))
		b.WriteString(line)
		b.WriteString(eraseToEnd)
	}
	s.shown = fitted
	s.redrawAll = false
	if b.Len() == 0 {
		return nil
	}
	_, err := io.WriteString(s.out, b.String())
	return err
}

// Fit is the frame the terminal can show: exactly rows lines (the frame
// padded with empty lines or cut), each at most cols visible cells wide.
func Fit(frame []string, rows, cols int) []string {
	fitted := make([]string, rows)
	for i := range fitted {
		if i < len(frame) {
			fitted[i] = display.CropLines(frame[i], cols)
		}
	}
	return fitted
}
