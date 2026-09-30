package watchui

import (
	"io"
	"os"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// terminal is what the screen runs on: its size, a notice at each resize,
// its keys, and the way in and out of the screen's mode. The live one is
// the operator's tty; the fake one, for the tests, is a reader of keys and
// a fixed size against any writer.
type terminal struct {
	size    func() (rows, cols int)
	resized <-chan os.Signal // nil on the fake: a nil channel never delivers
	keys    <-chan Key
	enter   func() error // raw mode on the live terminal
	leave   func() error // its settings back, however the screen ended
}

// liveTerminal is the operator's terminal: the size and the resize signal
// from out, the keys from in, which goes into raw mode while the screen is
// up when it is a terminal (a pipe on stdin gives its bytes as they are).
func liveTerminal(in io.Reader, out *os.File) *terminal {
	changes, stop := osutil.NotifyResize()
	t := &terminal{size: func() (int, int) { return osutil.TerminalSize(out) }, resized: changes}
	var restore func() error
	t.enter = func() error {
		f, ok := in.(*os.File)
		if !ok || !osutil.IsTerminal(f) {
			t.keys = ReadKeys(in)
			return nil
		}
		r, err := osutil.RawMode(f)
		if err != nil {
			return err
		}
		restore = r
		t.keys = ReadKeys(f)
		return nil
	}
	t.leave = func() error {
		stop()
		if restore != nil {
			return restore()
		}
		return nil
	}
	return t
}

// fakeTerminal is the tests' terminal: rows by cols, the keys from in, no
// resize, no mode to enter or leave.
func fakeTerminal(in io.Reader, rows, cols int) *terminal {
	t := &terminal{size: func() (int, int) { return rows, cols }}
	t.enter = func() error { t.keys = ReadKeys(in); return nil }
	t.leave = func() error { return nil }
	return t
}
