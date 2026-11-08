package cli

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/matching"
)

// TestCondenseReadsBackAsOneTl: a gathered display's header is one --tl
// value (matching.Condense; gather-2026-11 DECISIONS point 5). Its text split
// as --tl splits it (targetListNames), each item a selector that selects (no
// "!" negation, no "-" read from standard input, nothing white space trims),
// spelled by the grammar (Spell), each word with its escapes removed: the
// words are exactly the names condensed, once each, case as written. Over
// the grammar's own characters and generated sets of names: widths and
// gaps, one to three runs of digits, addresses, numbers past 64 bits.
func TestCondenseReadsBackAsOneTl(t *testing.T) {
	check := func(names []string) {
		t.Helper()
		text := matching.Condense(names)
		var got []string
		for _, item := range targetListNames(text) {
			if stdinDevice(item) || strings.TrimSpace(item) != item {
				t.Fatalf("Condense(%q) = %q: item %q is read as standard input or trimmed", names, text, item)
			}
			sel, err := matching.ParseSelector(item)
			if err != nil || sel.Negated {
				t.Fatalf("Condense(%q) = %q: item %q negated %v or refused: %v", names, text, item, sel.Negated, err)
			}
			words := []string{item}
			if sel.Pattern.Spells() {
				var ok bool
				if words, ok = sel.Pattern.Spell(len(names)); !ok {
					t.Fatalf("Condense(%q) = %q: item %q spells %s words, above the %d names", names, text, item, sel.Pattern.Words(), len(names))
				}
			}
			for _, w := range words {
				got = append(got, unescape(w))
			}
		}
		var want []string
		for _, n := range names {
			if n != "" && !slices.Contains(want, n) {
				want = append(want, n)
			}
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("Condense(%q) = %q reads back as %q", names, text, got)
		}
	}
	for _, names := range [][]string{
		{"lab*1", "lab*2", "labx1"},
		{"a,b", "a", "b"},
		{"!edge1", "edge1", "!edge2"},
		{"x[1]", "x[2]", "x1", "x[1,2]"},
		{"^core", "core$", "core"},
		{`back\slash1`, "backslash1", `end\`},
		{"-", "-1", "--2"},
		{"core 1", "core 2", "a\tb"},
		{"r?1", "r?2", "[1-2]", "r[a-c]"},
		{"r00", "r01", "r1", "r09", "r9", "r10", "r099", "r100", "r005", "r6"},
		{"Austin-rtr1", "austin-rtr2", "Router1", "router1"},
	} {
		check(names)
	}
	rng := rand.New(rand.NewSource(5))
	for range 3000 {
		check(generatedNames(rng))
	}
}

// generatedNames is a set of up to 40 names of one to three shapes: text
// from letters of both cases, separators, and the grammar's characters,
// between one to three runs of digits written at random widths with gaps;
// now and then an address, a name without digits, or a number past 64 bits.
func generatedNames(rng *rand.Rand) []string {
	const alphabet = `abAB-._:/!*?[],^$\ `
	text := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}
		return b.String()
	}
	number := func() string {
		switch rng.Intn(20) {
		case 0:
			return fmt.Sprintf("1844674407370955161%d", rng.Intn(10))
		case 1, 2: // about a width's edge
			return []string{"9", "09", "009", "10", "010", "99", "099", "0099", "100", "0100"}[rng.Intn(10)]
		}
		v := rng.Intn(30)
		if rng.Intn(3) == 0 {
			v = 95 + rng.Intn(10)
		}
		return fmt.Sprintf("%0*d", rng.Intn(4), v)
	}
	var names []string
	for range 1 + rng.Intn(3) {
		switch rng.Intn(8) {
		case 0: // IPv4
			for range 1 + rng.Intn(20) {
				names = append(names, fmt.Sprintf("192.0.%d.%d", rng.Intn(3), rng.Intn(20)))
			}
			continue
		case 1: // IPv6
			for range 1 + rng.Intn(20) {
				names = append(names, fmt.Sprintf("2001:db8::%s%d", []string{"", "a", "b0"}[rng.Intn(3)], rng.Intn(15)))
			}
			continue
		}
		runs := 1 + rng.Intn(3)
		parts := make([]string, runs+1)
		for i := range parts {
			parts[i] = text(rng.Intn(3))
			if i > 0 && i < runs && parts[i] == "" {
				parts[i] = "-" // between two runs, so they stay two
			}
		}
		for range 1 + rng.Intn(20) {
			var b strings.Builder
			b.WriteString(parts[0])
			for i := 1; i <= runs; i++ {
				b.WriteString(number())
				b.WriteString(parts[i])
			}
			n := b.String()
			if rng.Intn(15) == 0 {
				n = text(1 + rng.Intn(4)) // no digits
			}
			n = strings.TrimSpace(n) // as the inventory keeps a name
			if n != "" {
				names = append(names, n)
			}
		}
	}
	return names
}

// unescape removes a word's escapes: each backslash dropped, the rune after
// it kept. Condense writes no glob, so a spelled word holds only literal
// runes and escapes.
func unescape(word string) string {
	var b strings.Builder
	escaped := false
	for _, r := range word {
		if r == '\\' && !escaped {
			escaped = true
			continue
		}
		escaped = false
		b.WriteRune(r)
	}
	return b.String()
}
