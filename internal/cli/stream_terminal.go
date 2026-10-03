package cli

import (
	"errors"
	"io"
	"os"

	"github.com/robert-patrick-texas/karvi/internal/termline"
)

// streamTerminal reads the stream's lines from a terminal through the line
// editor (termline): Ctrl-A and Ctrl-E, Ctrl-K, Ctrl-U, Ctrl-W, Backspace,
// the Delete key, the arrows, the up arrow recalling earlier lines. The
// terminal is in raw mode for one line's read alone, so a notice, a dropped
// line's report, and a job's display print as they do from a pipe, and a
// Ctrl-C during a job is the signal it is; at the line, Ctrl-C and Ctrl-D
// end the stream as the input's end does. The editing echoes on the
// controlling terminal, not on standard output, so a redirected standard
// output still shows what is typed.
type streamTerminal struct {
	line *termline.Line
	tty  io.Closer // nil under test
}

// newStreamTerminal opens the controlling terminal for the echo; without one
// the reader is the scanner's.
func newStreamTerminal(in *os.File) (*streamTerminal, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	return &streamTerminal{line: termline.New(in, tty), tty: tty}, nil
}

// next reads one edited line; io.EOF ends the stream, for Ctrl-C at the
// line as for Ctrl-D and the input's end.
func (s *streamTerminal) next() (string, error) {
	line, err := s.line.ReadLine("")
	if errors.Is(err, termline.ErrInterrupt) {
		err = io.EOF
	}
	return line, err
}

// close releases the controlling terminal.
func (s *streamTerminal) close() {
	if s.tty != nil {
		_ = s.tty.Close()
	}
}
