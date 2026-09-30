package watchui

import (
	"strings"
	"testing"
)

// TestDecodeKey: the bytes a terminal in raw mode sends for the screen's
// keys, in normal and application cursor mode, and the bytes the screen
// ignores.
func TestDecodeKey(t *testing.T) {
	cases := []struct {
		in   string
		kind KeyKind
		r    rune
		used int
	}{
		{"q", KeyRune, 'q', 1},
		{"j", KeyRune, 'j', 1},
		{"é", KeyRune, 'é', 2},
		{"\r", KeyEnter, 0, 1},
		{"\n", KeyEnter, 0, 1},
		{"\x03", KeyCtrlC, 0, 1},
		{"\x1b", KeyEscape, 0, 1},
		{"\x1b[A", KeyUp, 0, 3},
		{"\x1b[B", KeyDown, 0, 3},
		{"\x1b[C", KeyRight, 0, 3},
		{"\x1b[D", KeyLeft, 0, 3},
		{"\x1bOA", KeyUp, 0, 3},
		{"\x1bOB", KeyDown, 0, 3},
		{"\x1b[5~", KeyPageUp, 0, 4},
		{"\x1b[6~", KeyPageDown, 0, 4},
		{"\x1b[H", KeyHome, 0, 3},
		{"\x1b[F", KeyEnd, 0, 3},
		{"\x1bOH", KeyHome, 0, 3},
		{"\x1bOF", KeyEnd, 0, 3},
		{"\x1b[1~", KeyHome, 0, 4},
		{"\x1b[4~", KeyEnd, 0, 4},
		{"\x1b[7~", KeyHome, 0, 4},
		{"\x1b[8~", KeyEnd, 0, 4},
		{"\x1b[2~", KeyOther, 0, 4},   // Insert
		{"\x1b[1;5A", KeyOther, 0, 6}, // Ctrl-Up
		{"\x1b[15~", KeyOther, 0, 5},  // F5
		{"\x1bOP", KeyOther, 0, 3},    // F1
		{"\x1bq", KeyEscape, 0, 1},    // Alt-q: the Escape, then q
		{"\x1b[Aq", KeyUp, 0, 3},      // two keys in one read: the first
		{"\x1b[", KeyRune, 0, 0},      // unfinished: read more
		{"\x1bO", KeyRune, 0, 0},      // unfinished: read more
		{"\xc3", KeyRune, 0, 0},       // half a rune: read more
	}
	for _, c := range cases {
		key, used := DecodeKey([]byte(c.in))
		if used != c.used || (used > 0 && (key.Kind != c.kind || key.Rune != c.r)) {
			t.Errorf("DecodeKey(%q) = %+v used %d, want kind %d rune %q used %d", c.in, key, used, c.kind, c.r, c.used)
		}
	}
}

// TestReadKeys: the reader splits a stream into keys across read
// boundaries and closes its channel when the stream ends; an escape the
// stream ends inside is the Escape key.
func TestReadKeys(t *testing.T) {
	keys := ReadKeys(strings.NewReader("j\x1b[Bq\r\x1b[5~\x1b["))
	var got []KeyKind
	for k := range keys {
		got = append(got, k.Kind)
	}
	want := []KeyKind{KeyRune, KeyDown, KeyRune, KeyEnter, KeyPageUp, KeyEscape, KeyRune}
	if len(got) != len(want) {
		t.Fatalf("keys %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys %v, want %v", got, want)
		}
	}
}
