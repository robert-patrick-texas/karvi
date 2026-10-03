package termline

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// chunks is a reader that delivers each of its strings as one read, as a
// terminal delivers what one keypress or one paste sent.
type chunks struct{ parts []string }

func (c *chunks) Read(b []byte) (int, error) {
	if len(c.parts) == 0 {
		return 0, io.EOF
	}
	n := copy(b, c.parts[0])
	c.parts = c.parts[1:]
	return n, nil
}

// TestReadLineEditing: the keys an operator types at a prompt edit the
// answer, Backspace as DEL or Ctrl-H, the Delete key forward, the arrows,
// Ctrl-A, Ctrl-E, Ctrl-U, and Ctrl-W, and Enter as \r or \n.
func TestReadLineEditing(t *testing.T) {
	for _, c := range []struct {
		name  string
		typed []string
		want  string
	}{
		{"backspace DEL", []string{"abc\x7fd\r"}, "abd"},
		{"backspace Ctrl-H", []string{"abc\x08d\r"}, "abd"},
		{"Delete key", []string{"abd\x1b[D\x1b[D\x1b[3~\r"}, "ad"},
		{"Delete key at the end", []string{"ab\x1b[3~\r"}, "ab"},
		{"Delete key split across reads", []string{"abd\x1b[D\x1b[D\x1b[", "3~\r"}, "ad"},
		{"Ctrl-A and Ctrl-E", []string{"bc\x01a\x05d\r"}, "abcd"},
		{"Ctrl-U", []string{"wrong\x15right\r"}, "right"},
		{"Ctrl-W", []string{"one two\x17three\r"}, "one three"},
		{"Enter as newline", []string{"abd\n"}, "abd"},
		{"a multi-byte character deleted forward", []string{"aé\x1b[D\x1b[3~b\r"}, "ab"},
	} {
		l := FromReader(&chunks{parts: c.typed}, io.Discard)
		got, err := l.ReadLine("Username: ")
		if err != nil || got != c.want {
			t.Errorf("%s: %q %v, want %q", c.name, got, err, c.want)
		}
	}
}

// TestReadPasswordEditsWithoutEcho: the password is edited as the
// username is, and nothing typed reaches the terminal.
func TestReadPasswordEditsWithoutEcho(t *testing.T) {
	var echo bytes.Buffer
	l := FromReader(&chunks{parts: []string{"sec\x7fcret\x1b[D\x1b[3~\r"}}, &echo)
	got, err := l.ReadPassword("Password: ")
	if err != nil || got != "secre" {
		t.Fatalf("%q %v", got, err)
	}
	if strings.Contains(echo.String(), "sec") {
		t.Fatalf("the password was echoed: %q", echo.String())
	}
}

// TestInterruptAndEnd: Ctrl-C ends the read with ErrInterrupt, at an empty
// line or a typed one; Ctrl-D on an empty line and the input's end are
// io.EOF; Ctrl-D inside a line deletes forward, and a line typed ahead of
// the next read is that read's answer.
func TestInterruptAndEnd(t *testing.T) {
	read := func(parts ...string) (*Line, string, error) {
		l := FromReader(&chunks{parts: parts}, io.Discard)
		got, err := l.ReadLine("")
		return l, got, err
	}
	if _, _, err := read("\x03"); !errors.Is(err, ErrInterrupt) {
		t.Errorf("Ctrl-C at an empty line: %v", err)
	}
	if _, _, err := read("half\x03"); !errors.Is(err, ErrInterrupt) {
		t.Errorf("Ctrl-C in a typed line: %v", err)
	}
	if _, _, err := read("\x04"); err != io.EOF {
		t.Errorf("Ctrl-D at an empty line: %v", err)
	}
	if _, _, err := read(); err != io.EOF {
		t.Errorf("the input's end: %v", err)
	}
	if _, got, err := read("ab\x1b[D\x04\r"); err != nil || got != "a" {
		t.Errorf("Ctrl-D in a line: %q %v", got, err)
	}
	l, got, err := read("user\rpass\r\x03")
	if err != nil || got != "user" {
		t.Fatalf("first line: %q %v", got, err)
	}
	if got, err := l.ReadPassword(""); err != nil || got != "pass" {
		t.Errorf("the line typed ahead: %q %v", got, err)
	}
	if _, err := l.ReadLine(""); !errors.Is(err, ErrInterrupt) {
		t.Errorf("the Ctrl-C typed ahead: %v", err)
	}
}
