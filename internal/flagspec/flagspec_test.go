package flagspec

import (
	"flag"
	"testing"
)

// spec is a command line of every kind of flag: a word or a path, a word
// alone, a named value, and a switch whose words overflow the help's field.
var spec = Spec{
	Program: "tool",
	Title:   "tool - test the spec",
	Order:   []string{"root", "mode", "count", "a-switch-whose-name-overflows-the-field"},
	Values: map[string]Value{
		"root":  {Words: []string{"auto"}, Path: true},
		"mode":  {Words: []string{"a", "b"}},
		"count": {Name: "N"},
	},
	Define: func(fs *flag.FlagSet) {
		fs.String("root", "auto", "The root")
		fs.String("mode", "", "The mode, one of the two words")
		fs.Int("count", 3, "How many")
		fs.Bool("a-switch-whose-name-overflows-the-field", false, "A switch with a description long enough to wrap onto a second line under the field")
	},
}

// TestPlaceholder: the words, then PATH, or the name; a switch has none.
func TestPlaceholder(t *testing.T) {
	for name, want := range map[string]string{"root": "auto|PATH", "mode": "a|b", "count": "N", "a-switch-whose-name-overflows-the-field": ""} {
		if got := spec.Placeholder(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// TestHelpShapes: the synopsis wrapped under its first bracket, a word too
// long for the field with its description under it, an empty default and a
// switch's left out, a set default after the text.
func TestHelpShapes(t *testing.T) {
	got := spec.Help(spec.FlagSet())
	want := `tool - test the spec

Usage:
  tool [--root auto|PATH] [--mode a|b] [--count N]
       [--a-switch-whose-name-overflows-the-field]

Options:
  --root auto|PATH               The root (default auto)
  --mode a|b                     The mode, one of the two words
  --count N                      How many (default 3)
  --a-switch-whose-name-overflows-the-field
                                 A switch with a description long enough to
                                 wrap onto a second line under the field
`
	if got != want {
		t.Fatalf("Help\n%s\nwant\n%s", got, want)
	}
	if u := spec.Usage(); u != "usage: tool [--root auto|PATH] [--mode a|b] [--count N] [--a-switch-whose-name-overflows-the-field]" {
		t.Errorf("Usage: %q", u)
	}
}

// TestWrap: a word longer than the width has a line of its own.
func TestWrap(t *testing.T) {
	got := wrap("a bb ccccccccc d", 4)
	want := []string{"a bb", "ccccccccc", "d"}
	if len(got) != len(want) {
		t.Fatalf("%q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%q, want %q", got, want)
		}
	}
}
