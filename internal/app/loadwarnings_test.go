package app

import (
	"bytes"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
)

// TestLoadWarningsSayEachOnce: a reporter says each warning at the first
// load that raised it and never again, a warning first raised at a later
// load then; a nil reporter says nothing.
func TestLoadWarningsSayEachOnce(t *testing.T) {
	var out bytes.Buffer
	l := NewLoadWarnings(WarningLines(&out))
	l.Say(configload.Snapshot{Warnings: []string{"a", "b"}})
	l.Say(configload.Snapshot{Warnings: []string{"a", "b"}})
	l.Say(configload.Snapshot{Warnings: []string{"b", "c"}})
	if got, want := out.String(), "warning: a\nwarning: b\nwarning: c\n"; got != want {
		t.Errorf("said %q, want %q", got, want)
	}
	var muted *LoadWarnings
	muted.Say(configload.Snapshot{Warnings: []string{"a"}})
}
