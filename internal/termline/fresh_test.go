package termline

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/testpty"
)

// TestAReadAfterCtrlDOrCtrlCIsFresh: a read ended by Ctrl-D on an empty line
// or by Ctrl-C leaves the next read on the same editor as a first one: its
// prompt is shown and it reads the next line, as a login's operator meets
// "Re-enter PIN: " after a Ctrl-D at the new PIN.
func TestAReadAfterCtrlDOrCtrlCIsFresh(t *testing.T) {
	for _, c := range []struct {
		key  string
		want error
	}{{"\x04", io.EOF}, {"\x03", ErrInterrupt}} {
		var out bytes.Buffer
		l := FromReader(&chunks{parts: []string{c.key, "731904\r"}}, &out)
		if _, err := l.ReadPassword("A: "); !errors.Is(err, c.want) {
			t.Errorf("%q: the first read: %v, want %v", c.key, err, c.want)
		}
		out.Reset()
		got, err := l.ReadPassword("B: ")
		if err != nil || got != "731904" || !bytes.Contains(out.Bytes(), []byte("B: ")) {
			t.Errorf("%q: the next read: %q %v, the terminal sent %q; want the line, its prompt shown", c.key, got, err, out.String())
		}
	}
}

// TestAReadEndsAtItsDeadlineInRawMode: a read on a terminal, the window's
// size taken and raw mode set as each read does, ends at the file's read
// deadline, which is how termline.Prompt ends at its context's end (a
// signal): the terminal calls keep the file's deadlines working.
func TestAReadEndsAtItsDeadlineInRawMode(t *testing.T) {
	_, slave := testpty.Open(t)
	if !osutil.IsTerminal(slave) {
		t.Fatal("the pseudo-terminal is not a terminal")
	}
	l := New(slave, slave)
	done := make(chan error, 1)
	go func() { _, err := l.ReadPassword("x: "); done <- err }()
	time.Sleep(100 * time.Millisecond) // the read likely under way; either way it must end
	if err := slave.SetReadDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("the read ended without an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read did not end at its deadline")
	}
}
