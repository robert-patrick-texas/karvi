package completion

import (
	"bytes"
	"reflect"
	"testing"
)

// TestLine covers the hidden word's arguments: the cursor on a word, one
// past the last word (an empty current word), and the shapes that are not a
// line (no words, a cursor that is not a number, zero, or past the end).
func TestLine(t *testing.T) {
	line, current, ok := Line([]string{"2", "karvi", "run", "--tar"})
	if !ok || !reflect.DeepEqual(line, []string{"run"}) || current != "--tar" {
		t.Errorf("on a word: %v %q %v", line, current, ok)
	}
	line, current, ok = Line([]string{"3", "karvi", "run", "--target"})
	if !ok || !reflect.DeepEqual(line, []string{"run", "--target"}) || current != "" {
		t.Errorf("past the last word: %v %q %v", line, current, ok)
	}
	if line, current, ok = Line([]string{"1", "karvi"}); !ok || len(line) != 0 || current != "" {
		t.Errorf("the program name alone: %v %q %v", line, current, ok)
	}
	for _, bad := range [][]string{nil, {"1"}, {"x", "karvi"}, {"0", "karvi"}, {"3", "karvi"}} {
		if _, _, ok := Line(bad); ok {
			t.Errorf("%q read as a line", bad)
		}
	}
}

// TestFilterAndPrint: the filter keeps the prefix's candidates once and
// sorted, and the printer writes them one per line with the directive last.
func TestFilterAndPrint(t *testing.T) {
	got := Filter([]string{"--verbose", "--days", "--dry-run", "--days"}, "--d")
	if !reflect.DeepEqual(got, []string{"--days", "--dry-run"}) {
		t.Errorf("filter: %q", got)
	}
	if got := Filter([]string{"a"}, "abc"); got != nil {
		t.Errorf("a current word longer than the candidate: %q", got)
	}
	var buf bytes.Buffer
	Print(&buf, []string{"auto"}, true)
	if buf.String() != "auto\n:files\n" {
		t.Errorf("print: %q", buf.String())
	}
	buf.Reset()
	Print(&buf, nil, false)
	if buf.Len() != 0 {
		t.Errorf("nothing: %q", buf.String())
	}
}
