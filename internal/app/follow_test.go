package app

import "testing"

// TestNotKeptLine is the not-kept sentence: named
// from the cursor and the start's first_sequence, for the first follow of
// a job without commands.jsonl and for a resume; empty when the stream
// starts right after the cursor.
func TestNotKeptLine(t *testing.T) {
	for _, c := range []struct {
		cursor, first int64
		want          string
	}{
		{0, 1, ""},
		{4, 5, ""},
		{0, 5, "records 1 to 4 were before this follow and are not kept (output.files.commands-jsonl is false)"},
		{2, 8, "records 3 to 7 were before this follow and are not kept (output.files.commands-jsonl is false)"},
	} {
		if got := NotKeptLine(c.cursor, c.first); got != c.want {
			t.Errorf("cursor %d first %d: %q", c.cursor, c.first, got)
		}
	}
}
