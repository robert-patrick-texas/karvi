package cli

import (
	"errors"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// streamTerminal reads the stream's lines from a terminal with line
// editing and history (golang.org/x/term): Ctrl-A and Ctrl-E, Ctrl-K,
// Ctrl-U, Ctrl-W, the arrows, the up arrow recalling earlier lines. The
// terminal is in raw mode for one line's read alone and back in its own
// mode for everything else, so a notice, a dropped line's report, and a
// job's display print as they do from a pipe, and a Ctrl-C during a job is
// the signal it is today; at the line, Ctrl-C and Ctrl-D end the stream as
// the input's end does. The editing echoes on the controlling terminal, not
// on standard output, so a redirected standard output still shows what is
// typed.
type streamTerminal struct {
	in  *os.File // the terminal read, put into raw mode for each line; nil under test
	tty io.Closer
	t   *term.Terminal
}

// newStreamTerminal opens the controlling terminal for the echo; without one
// the reader is the scanner's.
func newStreamTerminal(in *os.File) (*streamTerminal, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	return &streamTerminal{in: in, tty: tty, t: term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{enterReader{in}, tty}, "")}, nil
}

// enterReader gives the terminal every line ending as the Enter it takes
// (\r). A key typed while a job runs, or pasted, passes through the
// terminal's own mode, which turns Enter into \n; without the translation
// such a line would run into the next one.
type enterReader struct{ r io.Reader }

func (e enterReader) Read(b []byte) (int, error) {
	n, err := e.r.Read(b)
	for i := range b[:n] {
		if b[i] == '\n' {
			b[i] = '\r'
		}
	}
	return n, err
}

// next reads one edited line; io.EOF ends the stream.
func (s *streamTerminal) next() (string, error) {
	if s.in != nil {
		if rows, columns := osutil.TerminalSize(s.in); columns > 0 {
			_ = s.t.SetSize(columns, rows)
		}
		restore, err := osutil.RawMode(s.in)
		if err != nil {
			return "", err
		}
		defer func() { _ = restore() }()
	}
	line, err := s.t.ReadLine()
	if errors.Is(err, term.ErrPasteIndicator) {
		err = nil
	}
	return line, err
}

// close releases the controlling terminal.
func (s *streamTerminal) close() {
	if s.tty != nil {
		_ = s.tty.Close()
	}
}
