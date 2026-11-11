package matching

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/natural"
)

// Condense writes names as one --tl value that selects exactly them: the
// folded names of a gathered display's header ("edge[1-2],router[01-04,10]").
//
// Names alike but for their digits, a shape ("pod#-sw#"), fold on the run of
// digits that gives the fewest items, the last run on a tie
// ("pod1-sw1,pod2-sw1" is "pod[1-2]-sw1"); a name of no other's shape, or of
// no digits, is its own item. Only ASCII digits make a run, as only they
// make a range. Text is compared as written, so names alike but for case are
// two items.
//
// A range's numbers are the grammar's (Spell): consecutive numbers written
// at the start's width make one item ("[01-04]", "[9-10]", "[099-100]"), and
// a number written at another width is an item of its own ("[08,9]",
// "[1,01,001]"). A run of one number is written bare, without brackets.
//
// A backslash goes before each rune the grammar or --tl's split reads
// ("\", "*", "?", "[", "]", ",", "^", "$", and white space), before a "!" that
// begins an item, and before an item that is "-": an ordinary name is
// unchanged, and every item reads back as its names alone.
//
// Items are joined by "," with no space, in natural order of the first name
// each covers (natural.Compare, the order --order sorted dispatches in):
// runs of digits by value, then the fewer digits first; other runes
// lowercase, by code point; a name that is the start of another first;
// names alike but for case as written.
// A repeated name is one; an empty name is no item. Two calls given the same
// names in any order write the same text. The text is never cut.
func Condense(names []string) string {
	seen := map[string]bool{}
	var shapes [][]digitSplit
	shapeAt := map[string]int{}
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		s := splitDigits(n)
		key := s.shapeKey()
		i, ok := shapeAt[key]
		if !ok {
			i = len(shapes)
			shapeAt[key] = i
			shapes = append(shapes, nil)
		}
		shapes[i] = append(shapes[i], s)
	}
	var items []condensed
	for _, group := range shapes {
		items = append(items, condenseShape(group)...)
	}
	slices.SortFunc(items, func(a, b condensed) int { return natural.Compare(a.first, b.first) })
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.text
	}
	return strings.Join(texts, ",")
}

// condensed is one item of Condense's text and the first name, in natural
// order, it covers.
type condensed struct{ first, text string }

// digitSplit is a name cut at its runs of ASCII digits: parts are the text
// before, between, and after the runs, one more than the runs.
type digitSplit struct {
	name  string
	parts []string
	runs  []string
}

func splitDigits(name string) digitSplit {
	s := digitSplit{name: name}
	start, inRun := 0, false
	for i, r := range name {
		if natural.IsDigit(r) == inRun {
			continue
		}
		if inRun {
			s.runs = append(s.runs, name[start:i])
		} else {
			s.parts = append(s.parts, name[start:i])
		}
		start, inRun = i, !inRun
	}
	if inRun {
		s.runs = append(s.runs, name[start:])
		s.parts = append(s.parts, "")
	} else {
		s.parts = append(s.parts, name[start:])
	}
	return s
}

// shapeKey is the same for two names exactly when their parts are the same:
// each part written after its length, so no part's text can end another's.
func (s digitSplit) shapeKey() string {
	var b strings.Builder
	for _, p := range s.parts {
		b.WriteString(strconv.Itoa(len(p)))
		b.WriteByte(':')
		b.WriteString(p)
	}
	return b.String()
}

// condenseShape folds one shape's names on the run giving the fewest items,
// trying the last run first so that a tie keeps it.
func condenseShape(group []digitSplit) []condensed {
	if len(group[0].runs) == 0 {
		items := make([]condensed, len(group))
		for i, s := range group {
			items[i] = condensed{first: s.name, text: escapeItem(escapeText(s.name))}
		}
		return items
	}
	var best []condensed
	for axis := len(group[0].runs) - 1; axis >= 0; axis-- {
		if got := foldOn(group, axis); best == nil || len(got) < len(best) {
			best = got
		}
	}
	return best
}

// foldOn folds a shape's names on its run at axis: names alike in every
// other run are one item, their numbers at axis written as a range.
func foldOn(group []digitSplit, axis int) []condensed {
	type bucket struct {
		members []digitSplit
		numbers []string
	}
	var buckets []*bucket
	at := map[string]*bucket{}
	for _, s := range group {
		// The other runs, in order; a run holds only digits, so "," parts them.
		key := strings.Join(slices.Delete(slices.Clone(s.runs), axis, axis+1), ",")
		b := at[key]
		if b == nil {
			b = &bucket{}
			at[key] = b
			buckets = append(buckets, b)
		}
		b.members = append(b.members, s)
		b.numbers = append(b.numbers, s.runs[axis])
	}
	items := make([]condensed, len(buckets))
	for i, b := range buckets {
		s := b.members[0]
		var text strings.Builder
		for j, p := range s.parts {
			text.WriteString(escapeText(p))
			switch {
			case j == len(s.runs):
			case j == axis:
				text.WriteString(writeRange(b.numbers))
			default:
				text.WriteString(s.runs[j])
			}
		}
		first := s.name
		for _, m := range b.members[1:] {
			if natural.Compare(m.name, first) < 0 {
				first = m.name
			}
		}
		items[i] = condensed{first: first, text: escapeItem(text.String())}
	}
	return items
}

// writeRange writes distinct numbers, each as written, as the grammar's
// range spelling exactly them, in the fewest items: one number bare; more in
// brackets, each item a number or two joined by "-", ordered by the lower
// number's value and then its width.
//
// A number with a leading zero ("08") is written at its own width, so it
// joins only numbers of that width, consecutive; one without (8, 10) joins
// any range whose start is no wider. So the numbers fall into blocks of
// consecutive values, one list per padded width and one of the unpadded,
// and a padded block reaching the width's last value ("09", "099") takes on
// the unpadded block that starts at the next ("10", "100").
func writeRange(numbers []string) string {
	if len(numbers) == 1 {
		return numbers[0]
	}
	byWidth := map[int][]string{} // padded numbers by width; 0 the unpadded
	for _, n := range numbers {
		w := 0
		if len(natural.Number([]rune(n))) < len(n) {
			w = len(n)
		}
		byWidth[w] = append(byWidth[w], n)
	}
	type block struct{ lo, hi string }
	blocks := func(list []string) []block {
		slices.SortFunc(list, compareValue)
		var out []block
		for _, n := range list {
			if k := len(out) - 1; k >= 0 && compareValue(string(increment([]rune(natural.Number([]rune(out[k].hi))))), n) == 0 {
				out[k].hi = n
				continue
			}
			out = append(out, block{n, n})
		}
		return out
	}
	unpadded := blocks(byWidth[0])
	startsAt := map[string]int{} // an unpadded block's index by its lower number
	for i, b := range unpadded {
		startsAt[b.lo] = i
	}
	var items []block
	for w, list := range byWidth {
		if w == 0 {
			continue
		}
		padded := blocks(list)
		last := &padded[len(padded)-1]
		next := string(increment([]rune(natural.Number([]rune(last.hi)))))
		if i, ok := startsAt[next]; ok && len(next) == w {
			last.hi = unpadded[i].hi
			delete(startsAt, next)
		}
		items = append(items, padded...)
	}
	for _, i := range startsAt {
		items = append(items, unpadded[i])
	}
	slices.SortFunc(items, func(a, b block) int {
		return cmp.Or(compareValue(a.lo, b.lo), len(a.lo)-len(b.lo))
	})
	texts := make([]string, len(items))
	for i, b := range items {
		texts[i] = b.lo
		if b.hi != b.lo {
			texts[i] += "-" + b.hi
		}
	}
	return "[" + strings.Join(texts, ",") + "]"
}

// compareValue orders two runs of digits by the numbers they write.
func compareValue(a, b string) int {
	return natural.CompareNumbers(natural.Number([]rune(a)), natural.Number([]rune(b)))
}

// escapeText writes text with a backslash before each rune the grammar or
// --tl's split reads: a backslash, "*", "?", "[", "]", ",", "^", "$", and
// white space. Every other byte is copied as it is, a byte that is no
// UTF-8 among them.
func escapeText(text string) string {
	if !strings.ContainsFunc(text, special) {
		return text
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if special(r) {
			b.WriteByte('\\')
		}
		b.WriteString(text[i : i+size])
		i += size
	}
	return b.String()
}

func special(r rune) bool { return strings.ContainsRune(`\*?[],^$`, r) || unicode.IsSpace(r) }

// escapeItem escapes what only an item's start makes special: a leading "!"
// (a negation) and an item that is "-" (standard input).
func escapeItem(item string) string {
	if item == "-" || strings.HasPrefix(item, "!") {
		return `\` + item
	}
	return item
}
