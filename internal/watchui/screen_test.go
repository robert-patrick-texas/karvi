package watchui

import (
	"bytes"
	"strings"
	"testing"
)

// TestScreenDiff is the screen model as text against a buffer: Enter
// takes the alternate screen and hides the cursor; the
// first Draw writes every row, addressed and erased to its end; a Draw that
// changes one line writes that line alone; a shorter frame erases the rows
// it no longer fills; a frame that fits what is shown writes nothing; a
// Resize rewrites every row at the new size with the lines cut to the
// width; Leave shows the cursor and drops the alternate screen.
func TestScreenDiff(t *testing.T) {
	var out bytes.Buffer
	s := NewScreen(&out, 4, 10)
	if err := s.Enter(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "\x1b[?1049h\x1b[?25l\x1b[H\x1b[2J" {
		t.Fatalf("enter %q", got)
	}
	out.Reset()
	if err := s.Draw([]string{"JOB-ID", "row one"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "\x1b[1;1HJOB-ID\x1b[K\x1b[2;1Hrow one\x1b[K\x1b[3;1H\x1b[K\x1b[4;1H\x1b[K" {
		t.Fatalf("first draw %q", got)
	}
	out.Reset()
	if err := s.Draw([]string{"JOB-ID", "row two"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "\x1b[2;1Hrow two\x1b[K" {
		t.Fatalf("one line changed %q", got)
	}
	out.Reset()
	if err := s.Draw([]string{"JOB-ID", "row two"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("unchanged frame wrote %q", got)
	}
	if err := s.Draw([]string{"JOB-ID"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "\x1b[2;1H\x1b[K" {
		t.Fatalf("shorter frame %q", got)
	}
	out.Reset()
	s.Resize(2, 5)
	if err := s.Draw([]string{"JOB-ID", "row two", "a third row"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "\x1b[1;1HJOB-I\x1b[K\x1b[2;1Hrow t\x1b[K" {
		t.Fatalf("after resize %q", got)
	}
	out.Reset()
	if err := s.Leave(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "\x1b[0m\x1b[?25h\x1b[?1049l" {
		t.Fatalf("leave %q", got)
	}
	if rows, cols := s.Size(); rows != 2 || cols != 5 {
		t.Fatalf("size %d x %d", rows, cols)
	}
}

// TestFitKeepsColourWhole: a line cut at the width keeps an escape sequence
// whole and at zero cells, and a cut through a coloured cell ends with a
// reset so the colour cannot run into the next row.
func TestFitKeepsColourWhole(t *testing.T) {
	line := "  260924-134403-00  \x1b[36mrunning\x1b[0m  quinlan"
	fitted := Fit([]string{line}, 1, 26)
	if fitted[0] != "  260924-134403-00  \x1b[36mrunnin\x1b[0m" {
		t.Errorf("cut %q", fitted[0])
	}
	fitted = Fit([]string{line}, 1, 80)
	if fitted[0] != line {
		t.Errorf("uncut %q", fitted[0])
	}
	if got := Fit(nil, 3, 10); len(got) != 3 || strings.Join(got, "|") != "||" {
		t.Errorf("padded %q", got)
	}
	var out bytes.Buffer
	if err := NewScreen(&out, 0, 0).Draw([]string{"x"}); err != nil || out.Len() != 0 {
		t.Errorf("a screen of no size drew %q (%v)", out.String(), err)
	}
}
