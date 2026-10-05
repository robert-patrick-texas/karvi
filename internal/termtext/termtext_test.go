package termtext

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

// The byte fixtures are logins to a host's OpenSSH captured through
// script(1), the banner's addresses replaced by documentation values:
// editing.raw the editing keys at 80 columns (Ctrl-L mid-line and at an
// empty prompt, Home and End, Ctrl-A, Ctrl-E, Ctrl-K, Ctrl-U, Ctrl-W, Delete,
// history, Ctrl-R, a line wrapped past 80 typed through, an insert in a
// wrapped line, an insert and a Delete in a wrapped line); helo.raw a
// coloured ls and a word corrected with two backspaces; resize.raw with
// resize.tim a wrapped line at 80 columns, a resize to 132, and an insert in
// a line wrapped at 132. Each .txt is the text the terminal showed.

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func renderFixture(t *testing.T, name string) []byte {
	t.Helper()
	if name != "resize" {
		return Render(fixture(t, name+".raw"), 80, nil)
	}
	columns, resizes, err := ParseTiming(bytes.NewReader(fixture(t, "resize.tim")))
	if err != nil {
		t.Fatal(err)
	}
	return Render(fixture(t, "resize.raw"), columns, resizes)
}

func TestFixtures(t *testing.T) {
	for _, name := range []string{"editing", "helo", "resize"} {
		t.Run(name, func(t *testing.T) {
			got, want := renderFixture(t, name), fixture(t, name+".txt")
			if !bytes.Equal(got, want) {
				t.Errorf("rendered:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// Every command in the fixtures is an echo, so the line after it, the
// shell's output, proves the command the shell received.
func TestEchoedCommands(t *testing.T) {
	const prompt = "netops@dev:~$ echo "
	for _, name := range []string{"editing", "helo", "resize"} {
		lines := strings.Split(string(renderFixture(t, name)), "\n")
		seen := 0
		for i, line := range lines {
			if args, ok := strings.CutPrefix(line, prompt); ok {
				seen++
				if i+1 >= len(lines) || lines[i+1] != args {
					t.Errorf("%s: %q is followed by %q", name, line, lines[i+1])
				}
			}
		}
		if seen == 0 {
			t.Errorf("%s: no echo command rendered", name)
		}
	}
}

// Without the resize the insert into a line wrapped at 132 columns is read
// in rows of 80, and the command recorded is not the one the shell ran.
func TestResizeNeeded(t *testing.T) {
	got := string(Render(fixture(t, "resize.raw"), 80, nil))
	if bytes.Equal([]byte(got), fixture(t, "resize.txt")) {
		t.Fatal("the resize fixture renders the same without its resize")
	}
	if strings.Contains(got, "$ echo Xq") {
		t.Errorf("the insert at 132 columns rendered right at 80:\n%s", got)
	}
}

// A write at a time, of any size, gives the same text: sequences and
// characters split between writes are joined.
func TestSplitWrites(t *testing.T) {
	for _, name := range []string{"editing", "helo"} {
		data := fixture(t, name+".raw")
		for _, size := range []int{1, 2, 3, 7, 64} {
			var out bytes.Buffer
			r := New(&out, 80)
			for i := 0; i < len(data); i += size {
				r.Write(data[i:min(i+size, len(data))])
			}
			r.Close()
			if !bytes.Equal(out.Bytes(), fixture(t, name+".txt")) {
				t.Errorf("%s in writes of %d differs:\n%s", name, size, out.Bytes())
			}
		}
	}
}

func TestRules(t *testing.T) {
	for _, tc := range []struct {
		name    string
		columns int
		in      string
		want    string
	}{
		{"a correction is applied, not deleted", 80, "$ echo helo\b\x1b[K\b\x1b[Klo\r\n", "$ echo helo\n"},
		{"a carriage return overwrites", 80, "abcdef\rXY\r\n", "XYcdef\n"},
		{"the line's end is the last newline's", 80, "one\r\ntwo", "one\ntwo\n"},
		{"blank lines are kept", 80, "a\r\n\r\n\r\nb\r\n", "a\n\n\nb\n"},
		{"trailing blanks are removed", 80, "a   \r\n", "a\n"},
		{"tabs are kept", 80, "a\tb\r\n", "a\tb\n"},
		{"colours and modes are dropped", 80, "\x1b[01;32mgreen\x1b[0m \x1b[?2004hx\x1b[?2004l\r\n", "green x\n"},
		{"a title to BEL is dropped", 80, "\x1b]0;user@host: ~\x07$ \r\n", "$\n"},
		{"a title to ST is dropped", 80, "\x1b]0;title\x1b\\$ x\r\n", "$ x\n"},
		{"a DCS string is dropped", 80, "a\x1bPq#0;1\x1b\\b\r\n", "ab\n"},
		{"a character set choice is dropped", 80, "\x1b(Ba\x1b)0b\r\n", "ab\n"},
		{"single escapes are dropped", 80, "\x1b=a\x1b>b\x1b7c\x1b8\r\n", "abc\n"},
		{"other controls are dropped", 80, "a\x07\x00\x0eb\x7f\r\n", "ab\n"},
		{"a C1 control is dropped", 80, "a\u0085b\r\n", "ab\n"},
		{"a control inside a sequence is executed", 80, "ab\x1b[\b2Kc\r\n", " c\n"},
		{"an intermediate drops the sequence", 80, "ab\x1b[2 qc\r\n", "abc\n"},
		{"text wraps at the width", 4, "abcdefg\r\n", "abcdefg\n"},
		{"a newline at the width does not add a row", 4, "abcd\r\nef\r\n", "abcd\nef\n"},
		{"a carriage return at the width stays in its row", 4, "abcd\rX\r\n", "Xbcd\n"},
		{"a backspace stops at the row's start", 4, "abcdef\b\b\b\bX\r\n", "abcdXf\n"},
		{"cursor up moves between the line's rows", 4, "abcdefgh\x1b[AX\r\n", "abcXefgh\n"},
		{"cursor up stops at the first row", 4, "ab\x1b[5AX\r\n", "abX\n"},
		{"a newline on an upper row moves down a row", 4, "abcdefgh\x1b[A\r\nX\r\n", "abcdXfgh\n"},
		{"cursor down moves to a lower row", 4, "abcdefgh\x1b[4D\x1b[A\x1b[BX\r\n", "abcdXfgh\n"},
		{"cursor right stops at the row's end", 4, "abcdefgh\x1b[A\r\x1b[9CX\r\n", "abcXefgh\n"},
		{"cursor left stops at the row's start", 4, "abcdefgh\x1b[9DX\r\n", "abcdXfgh\n"},
		{"a column move stays in the row", 4, "abcdefgh\x1b[2GX\r\n", "abcdeXgh\n"},
		{"erase to the end of the row", 4, "abcdefgh\x1b[A\x1b[2D\x1b[K\r\n", "a   efgh\n"},
		{"erase to the start of the row", 80, "abcdef\x1b[3D\x1b[1K\r\n", "    ef\n"},
		{"erase the row", 4, "abcdefgh\x1b[A\x1b[2K\r\n", "    efgh\n"},
		{"erase below ends the line at the cursor", 4, "abcdefgh\x1b[A\x1b[2D\x1b[J\r\n", "a\n"},
		{"erase above leaves the line", 80, "abc\x1b[1Jd\r\n", "abcd\n"},
		{"delete characters within the row", 4, "abcdefgh\x1b[A\x1b[2D\x1b[2P\r\n", "ad  efgh\n"},
		{"delete past the row's end", 80, "abcdef\x1b[3D\x1b[9P\r\n", "abc\n"},
		{"insert characters within the row, the overflow lost", 4, "abcdefgh\x1b[A\x1b[2D\x1b[2@X\r\n", "aX befgh\n"},
		{"insert at the line's end does nothing", 80, "ab\x1b[3@c\r\n", "abc\n"},
		{"the screen cleared discards the unfinished line", 80, "$ echo cl\x1b[H\x1b[2J$ echo cleared\r\n", "$ echo cleared\n"},
		{"the screen cleared with other sequences between", 80, "$ x\x1b[H\x1b[0m\x1b]0;t\x07\x1b[2J$ y\r\n", "$ y\n"},
		{"a clear alone discards the line", 80, "abc\x1b[2Jd\r\n", "d\n"},
		{"positioning ends the line", 80, "$ top\x1b[5;1Hrow five\r\n", "$ top\nrow five\n"},
		{"home ends the line when no clear follows", 80, "abc\x1b[Hdef\r\n", "abc\ndef\n"},
		{"the scrollback cleared is dropped", 80, "\x1b[H\x1b[2J\x1b[3J$ \r\n", "$\n"},
		{"positioning at the end keeps the line", 80, "abc\x1b[H", "abc\n"},
		{"UTF-8 is kept", 80, "caf\xc3\xa9 \xe2\x9c\x93\r\n", "café ✓\n"},
		{"a byte that is not UTF-8 is kept", 80, "a\xffb\xc3\r\n", "a\xffb\xc3\n"},
		{"an incomplete character at the end is kept", 80, "ab\xe2\x9c", "ab\xe2\x9c\n"},
		{"no width: nothing wraps", 0, "abcdefgh\x1b[AX\r\n", "abcdefghX\n"},
		{"no width: a column move", 0, "abcdefgh\x1b[3GX\r\n", "abXdefgh\n"},
		{"no width: insert", 0, "abcd\x1b[3D\x1b[2@X\r\n", "aX bcd\n"},
		{"an unfinished line is written at the close", 80, "$ ", "$\n"},
		{"nothing written, nothing out", 80, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(Render([]byte(tc.in), tc.columns, nil)); got != tc.want {
				t.Errorf("Render(%q, %d) = %q, want %q", tc.in, tc.columns, got, tc.want)
			}
		})
	}
}

func TestResize(t *testing.T) {
	// a line wrapped at 4 and widened to 8: up a row and left one is read
	// in rows of the width the terminal had
	in := []byte("abcdefghij\x1b[A\x1b[DX\r\n")
	if got := string(Render(in, 4, []Resize{{Offset: 10, Columns: 8}})); got != "aXcdefghij\n" {
		t.Errorf("resized: got %q", got)
	}
	if got := string(Render(in, 4, nil)); got != "abcdeXghij\n" {
		t.Errorf("not resized: got %q", got)
	}
	// a resize past the data's end is not reached
	if got := string(Render([]byte("ab\r\n"), 4, []Resize{{Offset: 99, Columns: 1}})); got != "ab\n" {
		t.Errorf("got %q", got)
	}
}

type failing struct{ n int }

func (f *failing) Write(p []byte) (int, error) {
	if f.n == 0 {
		return 0, errors.New("disk full")
	}
	f.n--
	return len(p), nil
}

func TestWriterError(t *testing.T) {
	r := New(&failing{n: 1}, 80)
	if _, err := r.Write([]byte("one\r\ntwo\r\nthree\r\n")); err == nil || err.Error() != "disk full" {
		t.Errorf("Write: %v", err)
	}
	if err := r.Close(); err == nil {
		t.Error("Close after a failed write: no error")
	}
}

func TestParseTiming(t *testing.T) {
	columns, resizes, err := ParseTiming(bytes.NewReader(fixture(t, "resize.tim")))
	if err != nil || columns != 80 || len(resizes) != 1 || resizes[0] != (Resize{Offset: 1702, Columns: 132}) {
		t.Fatalf("got %d %v %v", columns, resizes, err)
	}
	ok := "H 0.000000 COLUMNS 100\nI 0.1 5\nO 0.1 7\nS 0.1 SIGWINCH ROWS=1 COLS=50\n\nO 0.2 3\nS 0.1 SIGWINCH ROWS=2 COLS=60\nS 0.1 SIGTERM\n"
	columns, resizes, err = ParseTiming(strings.NewReader(ok))
	if err != nil || columns != 100 || len(resizes) != 2 || resizes[0] != (Resize{7, 50}) || resizes[1] != (Resize{10, 60}) {
		t.Fatalf("got %d %v %v", columns, resizes, err)
	}
	for _, bad := range []string{"O 0.1\n", "O 0.1 x\n", "O 0.1 -1\n", "S 0.1 SIGWINCH ROWS=1\n", "S 0.1 SIGWINCH COLS=x\n", "H 0.0 COLUMNS x\n", "Z 0.1 1\n"} {
		if _, _, err := ParseTiming(strings.NewReader(bad)); err == nil {
			t.Errorf("ParseTiming(%q): no error", bad)
		}
	}
}

// With KeepSpaces a line ends at the last cell written, a space the far end
// wrote included; a cell never written or blanked by an erase does not end
// it. Without, every trailing blank goes.
func TestKeepSpaces(t *testing.T) {
	for _, tc := range []struct{ in, keep, trim string }{
		{"desc \r\n", "desc \n", "desc\n"},
		{"abc\b \b\r\n", "ab \n", "ab\n"},
		{"abc\x1b[2D\x1b[K\r\n", "a\n", "a\n"},
		{"ab\x1b[5Gc \r\n", "ab  c \n", "ab  c\n"},
		{"abcdef\x1b[3D\x1b[9P\r\n", "abc\n", "abc\n"},
		{"ab\tc\t\r\n", "ab\tc\t\n", "ab\tc\t\n"},
		{"   \r\n", "   \n", "\n"},
	} {
		for _, keep := range []bool{true, false} {
			var out bytes.Buffer
			r := New(&out, 0)
			r.KeepSpaces = keep
			r.Write([]byte(tc.in))
			r.Close()
			want := tc.trim
			if keep {
				want = tc.keep
			}
			if out.String() != want {
				t.Errorf("%q keep=%t: %q, want %q", tc.in, keep, out.String(), want)
			}
		}
	}
}

// Pending is the unfinished line with its written spaces, whatever
// KeepSpaces says; Break writes it without a newline and starts afresh.
func TestPendingAndBreak(t *testing.T) {
	var out bytes.Buffer
	r := New(&out, 0)
	r.Write([]byte("one\r\n\x1b[01;32mSave?\x1b[0m [yes/no]: "))
	if got := r.Pending(); got != "Save? [yes/no]: " {
		t.Errorf("Pending %q", got)
	}
	r.Write([]byte("\x1b[K"))
	if got := r.Pending(); got != "Save? [yes/no]: " {
		t.Errorf("Pending after an erase at the end: %q", got)
	}
	if err := r.Break(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "one\nSave? [yes/no]: " {
		t.Errorf("after Break: %q", got)
	}
	if r.Pending() != "" {
		t.Errorf("Pending after Break: %q", r.Pending())
	}
	r.Write([]byte("no\r\n"))
	r.Close()
	if got := out.String(); got != "one\nSave? [yes/no]: no\n" {
		t.Errorf("at the end: %q", got)
	}
	f := New(&failing{}, 0)
	f.Write([]byte("abc"))
	if err := f.Break(); err == nil {
		t.Error("Break to a failing writer: no error")
	}
}

// Seeded text holds its columns and is never written out; what the far end
// writes over it is.
func TestSeed(t *testing.T) {
	for _, tc := range []struct{ seed, in, want, pending string }{
		{"router#", "show clock\r\n12:00\r\nrouter#", "show clock\n12:00\n", "router#"},
		{"router#", "\rxyz\r\n", "xyz\n", ""},
		{"router#", "\rrouter#show clock\r\n", "router#show clock\n", ""},
		{"router#", "\r\n", "\n", ""},
		{"router#", "\b\bX\r\n", "X\n", ""},
		{"router#", "abc", "", "abc"},
		{"$ ", "", "", ""},
	} {
		var out bytes.Buffer
		r := New(&out, 0)
		r.KeepSpaces = true
		r.Seed(tc.seed)
		r.Write([]byte(tc.in))
		if out.String() != tc.want || r.Pending() != tc.pending {
			t.Errorf("seed %q, %q: wrote %q, pending %q; want %q, %q", tc.seed, tc.in, out.String(), r.Pending(), tc.want, tc.pending)
		}
	}
}
