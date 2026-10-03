// Package termline reads one edited line from a terminal, for stream mode's
// lines and the credential prompts: golang.org/x/term's editor (Ctrl-A and
// Ctrl-E, Ctrl-K, Ctrl-U, Ctrl-W, Backspace and Ctrl-H, the arrows, and the
// up arrow recalling earlier lines), with the Delete key as a forward
// delete and Ctrl-C told apart from Ctrl-D. The terminal is in raw mode for
// one line's read alone and back in its own mode for everything else, so
// a message, a job's display, and a Ctrl-C during a job are as they were.
package termline

import (
	"bytes"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// ErrInterrupt is a read ended by Ctrl-C at the line; Ctrl-D on an empty
// line, and the input's end, are io.EOF.
var ErrInterrupt error = interrupted{}

type interrupted struct{}

func (interrupted) Error() string { return "interrupted (Ctrl-C)" }

// Line is one terminal's editor: in is read, and put into raw mode for each
// line; the editing is echoed on out.
type Line struct {
	in   *os.File // nil under test: no raw mode, no size
	keys *keyReader
	t    *term.Terminal
}

// New reads lines from in, echoing on out (the controlling terminal).
func New(in *os.File, out io.Writer) *Line {
	l := FromReader(in, out)
	l.in = in
	return l
}

// FromReader is New without a terminal to set, for a test: r is read as
// typed, with no raw mode and no window size.
func FromReader(r io.Reader, out io.Writer) *Line {
	k := &keyReader{r: r}
	t := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{k, out}, "")
	t.AutoCompleteCallback = forwardDelete
	return &Line{keys: k, t: t}
}

// ReadLine reads one line after prompt, echoed and kept in the history.
func (l *Line) ReadLine(prompt string) (string, error) {
	return l.read(func() (string, error) {
		l.t.SetPrompt(prompt)
		return l.t.ReadLine()
	})
}

// ReadPassword reads one line after prompt without echo and keeps it out
// of the history.
func (l *Line) ReadPassword(prompt string) (string, error) {
	return l.read(func() (string, error) { return l.t.ReadPassword(prompt) })
}

// read runs one line's read with the terminal in raw mode, sized to the
// window, and tells Ctrl-C from the input's end.
func (l *Line) read(fn func() (string, error)) (string, error) {
	if l.in != nil {
		if rows, columns := osutil.TerminalSize(l.in); columns > 0 {
			_ = l.t.SetSize(columns, rows)
		}
		restore, err := osutil.RawMode(l.in)
		if err != nil {
			return "", err
		}
		defer func() { _ = restore() }()
	}
	line, err := fn()
	if errors.Is(err, term.ErrPasteIndicator) {
		err = nil
	}
	if err == io.EOF && l.keys.interrupts > 0 {
		l.keys.interrupts--
		err = ErrInterrupt
	}
	return line, err
}

// deleteKey is the sequence the Delete key sends, which x/term does not
// know; deleteRune, a private-use character no keyboard sends, stands for
// it in front of the editor, which hands it to forwardDelete.
const deleteKey = "\x1b[3~"

const deleteRune = ''

// forwardDelete is the editor's callback for a key it does not handle: the
// Delete key removes the character under the cursor; every other key is
// the editor's.
func forwardDelete(line string, pos int, key rune) (string, int, bool) {
	if key != deleteRune {
		return "", 0, false
	}
	if pos >= len(line) {
		return line, pos, true
	}
	_, size := utf8.DecodeRuneInString(line[pos:])
	return line[:pos] + line[pos+size:], pos, true
}

// keyReader stands in front of the editor and translates what it reads:
// every \n becomes the \r the editor takes as Enter (a key typed while a
// job runs, or pasted, passes through the terminal's own mode, which turns
// Enter into \n; without the translation such a line would run into the
// next), and the Delete key becomes deleteRune, a sequence split across
// two reads waiting for its end. Each Ctrl-C is counted: the editor ends
// the line with io.EOF for Ctrl-C and Ctrl-D alike, and read takes one
// count for each io.EOF a Ctrl-C caused.
type keyReader struct {
	r          io.Reader
	out        []byte // translated, not yet delivered
	held       []byte // the start of a Delete key sequence, waiting
	err        error  // the read error behind out, delivered after it
	interrupts int
}

func (k *keyReader) Read(b []byte) (int, error) {
	for len(k.out) == 0 {
		if k.err != nil {
			err := k.err
			k.err = nil
			return 0, err
		}
		buf := make([]byte, 256)
		n, err := k.r.Read(buf)
		k.translate(buf[:n], err != nil)
		k.err = err
	}
	n := copy(b, k.out)
	k.out = k.out[n:]
	return n, nil
}

// translate appends data to out, translated; at the end of the input
// (last) a held sequence start is passed on as it is.
func (k *keyReader) translate(data []byte, last bool) {
	data = append(k.held, data...)
	k.held = nil
	for i := 0; i < len(data); {
		rest := data[i:]
		switch {
		case rest[0] == '\n':
			k.out = append(k.out, '\r')
			i++
		case rest[0] == 3:
			k.interrupts++
			k.out = append(k.out, 3)
			i++
		case bytes.HasPrefix(rest, []byte(deleteKey)):
			k.out = utf8.AppendRune(k.out, deleteRune)
			i += len(deleteKey)
		case !last && len(rest) < len(deleteKey) && bytes.HasPrefix([]byte(deleteKey), rest):
			k.held = append([]byte(nil), rest...)
			i = len(data)
		default:
			k.out = append(k.out, rest[0])
			i++
		}
	}
}
