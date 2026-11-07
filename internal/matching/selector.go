// Package matching holds karvi's SELECTOR profile: the one glob grammar of
// the command line's selectors, the credential policy map, and the
// session-init map, and the rule selection the two maps share. The
// configuration-lock and .cloginrc matchers keep their own profiles.
//
// A pattern matches the whole field without regard to case. "*" matches any
// run of runes, "/" and "." included; "?" matches one rune; "[a-c]" and
// "[!a-c]" are a class and its negation; a backslash makes the next rune
// literal. An unescaped "^" or "$" is refused: a selector already matches the
// whole field, and the two characters are reserved for regular-expression
// style starts-with and ends-with matches.
//
// A bracket holding only digits, hyphens, and commas is a numeric range:
// items separated by ",", each a number or two numbers joined by "-"
// ("[1-12]", "[01-12]", "[1,4,7-9]"). It matches a run of digits by number
// and width, the width the item's lower bound is written in, so a pattern
// matches a value exactly when the value is one of the names the pattern
// spells: "[01-12]" is 01 to 12, "[1-12]" is 1 to 12 unpadded, "[05]" is 05.
// Any other bracket of those characters ("[1-]", "[1,,3]") is refused, and
// so is one that also holds white space beside a digit ("[10, 12]"); a
// backslash keeps a class ("[1\-]" is 1 or "-", "[1\ 0]" 1, space, or 0). A pattern holding a range
// also spells its words (Spell): the pattern with each range written as one
// of its numbers.
package matching

import (
	"fmt"
	"strings"
	"unicode"
)

// PatternError is a malformed selector. It carries no code: the caller names
// the rule or the option and supplies its own.
type PatternError struct {
	Value  string
	Reason string
	// Unquoted is Reason without the pattern's own text, for a caller that
	// must not quote it (a credential row's cell); "" when Reason quotes
	// none of it.
	Unquoted string
}

func (e *PatternError) Error() string {
	return fmt.Sprintf("selector %q is invalid: %s", e.Value, e.Reason)
}

// Selector is one decoded selector value: a pattern and whether a leading
// unescaped "!" negated it. "\!" is a literal "!".
type Selector struct {
	Negated bool
	Pattern Pattern
}

// ParseSelector splits a leading unescaped "!" from value and compiles the
// rest. A bare "!" is refused.
func ParseSelector(value string) (Selector, error) {
	if strings.HasPrefix(value, "!") {
		rest := value[1:]
		if rest == "" {
			return Selector{}, &PatternError{Value: value, Reason: "a negation needs a pattern after the \"!\""}
		}
		p, err := compile(value, rest)
		if err != nil {
			return Selector{}, err
		}
		return Selector{Negated: true, Pattern: p}, nil
	}
	p, err := compile(value, value)
	if err != nil {
		return Selector{}, err
	}
	return Selector{Pattern: p}, nil
}

// Compile compiles a pattern with no negation step: a leading "!" is an
// ordinary rune.
func Compile(pattern string) (Pattern, error) { return compile(pattern, pattern) }

// HasMeta reports whether value holds a rune the grammar treats specially
// ("*", "?", "[", or a backslash), so a caller can tell a name from a glob.
func HasMeta(value string) bool { return strings.ContainsAny(value, `*?[\`) }

// Pattern is a compiled selector pattern.
type Pattern struct {
	source string
	tokens []token
	ranged bool        // a numeric token among the tokens
	spans  []rangeSpan // every numeric range as written, for Spell
}

// rangeSpan is a numeric range's place in the pattern as written, the rune
// indexes of its "[" and "]", and its items: kept for Spell whether the
// range compiled to a numeric token or, its items one digit each, to the
// class of those digits.
type rangeSpan struct {
	start, end int
	items      []numberRange
}

// String is the pattern as written.
func (p Pattern) String() string { return p.source }

type tokenKind int

const (
	literal tokenKind = iota
	anyOne
	anyRun
	class
	numeric
)

type token struct {
	kind    tokenKind
	r       rune
	negated bool
	ranges  []runeRange
	items   []numberRange
}

type runeRange struct{ lo, hi rune }

// numberRange is one item of a numeric range: the numbers lo to hi, each
// written as decimal digits with no leading zero ("0" for zero), spelled
// zero-padded to width digits, the lower bound's width as written.
type numberRange struct {
	lo, hi []rune
	width  int
}

// rangeItems is what a malformed numeric range's message says a range holds.
const rangeItems = `each item is a number or two numbers joined by "-", separated by "," with no space`

const reservedReason = "an unescaped %q is not allowed: a selector already matches the whole field, \"[!...]\" excludes characters, and \\^ and \\$ are the literal characters"

func compile(value, pattern string) (Pattern, error) {
	runes := []rune(pattern)
	p := Pattern{source: pattern}
	bad := func(format string, args ...any) *PatternError {
		return &PatternError{Value: value, Reason: fmt.Sprintf(format, args...)}
	}
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '*':
			if n := len(p.tokens); n == 0 || p.tokens[n-1].kind != anyRun {
				p.tokens = append(p.tokens, token{kind: anyRun})
			}
		case '?':
			p.tokens = append(p.tokens, token{kind: anyOne})
		case '\\':
			if i+1 == len(runes) {
				return Pattern{}, bad("a trailing backslash escapes nothing")
			}
			i++
			p.tokens = append(p.tokens, token{kind: literal, r: runes[i]})
		case '^', '$':
			return Pattern{}, bad(reservedReason, string(r))
		case '[':
			end, ok, spaced := numericBracket(runes, i+1)
			if spaced {
				return Pattern{}, malformedRange(string(runes[i+1:end]), bad)
			}
			if ok {
				t, items, err := compileNumeric(string(runes[i+1:end]), bad)
				if err != nil {
					return Pattern{}, err
				}
				p.tokens = append(p.tokens, t)
				p.ranged = p.ranged || t.kind == numeric
				p.spans = append(p.spans, rangeSpan{start: i, end: end, items: items})
				i = end
				break
			}
			t, next, err := compileClass(runes, i+1, bad)
			if err != nil {
				return Pattern{}, err
			}
			p.tokens = append(p.tokens, t)
			i = next
		default:
			p.tokens = append(p.tokens, token{kind: literal, r: r})
		}
	}
	return p, nil
}

// numericBracket reads the bracket whose "[" is at start-1: ok when it
// holds only digits, hyphens, and commas, spaced when it holds those and
// white space, a digit and a space among them, which is a malformed range;
// end is the index of its closing "]" in either case.
func numericBracket(runes []rune, start int) (end int, ok, spaced bool) {
	i := start
	digit, space := false, false
	for ; i < len(runes); i++ {
		r := runes[i]
		if isDigit(r) {
			digit = true
		} else if unicode.IsSpace(r) {
			space = true
		} else if r != '-' && r != ',' {
			break
		}
	}
	if i == start || i == len(runes) || runes[i] != ']' {
		return i, false, false
	}
	if space {
		return i, false, digit
	}
	return i, true, false
}

// malformedRange is the refusal of a numeric range's body that is no list
// of numbers and number-number items.
func malformedRange(body string, bad func(string, ...any) *PatternError) *PatternError {
	e := bad("the numeric range %q is malformed: %s", "["+body+"]", rangeItems)
	e.Unquoted = "a numeric range is malformed: " + rangeItems
	return e
}

// compileNumeric reads a numeric range's body, the text between its
// brackets: items separated by ",", each a number or two numbers joined by
// "-", the lower not above the upper. A range whose every item is one digit
// spells one character, so it is the class of those digits. The items are
// returned with the token, for Spell.
func compileNumeric(body string, bad func(string, ...any) *PatternError) (token, []numberRange, error) {
	t := token{kind: numeric}
	digit := token{kind: class}
	for _, item := range strings.Split(body, ",") {
		lo, hi, isRange := strings.Cut(item, "-")
		if !isRange {
			hi = lo
		}
		if lo == "" || hi == "" || strings.Contains(hi, "-") {
			return token{}, nil, malformedRange(body, bad)
		}
		n := numberRange{lo: number([]rune(lo)), hi: number([]rune(hi)), width: len(lo)}
		if compareNumbers(n.lo, n.hi) > 0 {
			e := bad("the range %q-%q is reversed", lo, hi)
			e.Unquoted = "a numeric range is reversed"
			return token{}, nil, e
		}
		t.items = append(t.items, n)
		if n.width == 1 && len(n.hi) == 1 {
			digit.ranges = append(digit.ranges, runeRange{n.lo[0], n.hi[0]})
		}
	}
	if len(digit.ranges) == len(t.items) {
		return digit, t.items, nil
	}
	return t, t.items, nil
}

// compileClass reads a class whose "[" is at start-1 and returns the index of
// its closing "]". A "-" first or last in the class is literal; "]" inside a
// class is written "\]".
func compileClass(runes []rune, start int, bad func(string, ...any) *PatternError) (token, int, error) {
	t := token{kind: class}
	i := start
	if i < len(runes) && runes[i] == '!' {
		t.negated = true
		i++
	}
	first := i
	for {
		if i >= len(runes) {
			return token{}, 0, bad("a character class is not closed with \"]\"")
		}
		r := runes[i]
		if r == ']' {
			if i == first {
				if t.negated {
					return token{}, 0, bad("a negated character class \"[!]\" is empty")
				}
				return token{}, 0, bad("a character class \"[]\" is empty")
			}
			return t, i, nil
		}
		lo, next, err := classRune(runes, i, bad)
		if err != nil {
			return token{}, 0, err
		}
		hi := lo
		if next+1 < len(runes) && runes[next] == '-' && runes[next+1] != ']' {
			hi, next, err = classRune(runes, next+1, bad)
			if err != nil {
				return token{}, 0, err
			}
			if hi < lo {
				e := bad("the range %q-%q is reversed", string(lo), string(hi))
				e.Unquoted = "a character range is reversed"
				return token{}, 0, e
			}
		}
		t.ranges = append(t.ranges, runeRange{lo, hi})
		i = next
	}
}

func classRune(runes []rune, i int, bad func(string, ...any) *PatternError) (rune, int, error) {
	switch r := runes[i]; r {
	case '\\':
		if i+1 == len(runes) {
			return 0, 0, bad("a trailing backslash escapes nothing")
		}
		return runes[i+1], i + 2, nil
	case '^', '$':
		return 0, 0, bad(reservedReason, string(r))
	default:
		return r, i + 1, nil
	}
}

// Match reports whether the whole of value matches, without regard to case:
// whether value is one of the names the pattern spells.
func (p Pattern) Match(value string) bool {
	v := []rune(value)
	if p.ranged {
		return p.matchPositions(v)
	}
	return p.matchRunes(v)
}

// matchRunes matches a pattern whose every token but "*" takes one rune: on
// a mismatch it retries from the last "*", taking one rune more, which is
// enough when every other token's width is fixed.
func (p Pattern) matchRunes(v []rune) bool {
	ti, vi := 0, 0
	starT, starV := -1, 0
	for vi < len(v) {
		if ti < len(p.tokens) {
			switch t := p.tokens[ti]; t.kind {
			case anyRun:
				starT, starV = ti, vi
				ti++
				continue
			case anyOne:
				ti++
				vi++
				continue
			case literal:
				if equalFold(t.r, v[vi]) {
					ti++
					vi++
					continue
				}
			case class:
				if t.matches(v[vi]) {
					ti++
					vi++
					continue
				}
			}
		}
		if starT < 0 {
			return false
		}
		starV++
		ti, vi = starT+1, starV
	}
	for ti < len(p.tokens) && p.tokens[ti].kind == anyRun {
		ti++
	}
	return ti == len(p.tokens)
}

// matchPositions matches any pattern, a numeric range's among them: it
// follows every position of v the tokens can reach, since a range takes a
// run of digits of more than one length and a later mismatch can need a
// shorter one.
func (p Pattern) matchPositions(v []rune) bool {
	at := make([]bool, len(v)+1)
	next := make([]bool, len(v)+1)
	at[0] = true
	for _, t := range p.tokens {
		clear(next)
		reached := false
		for i := range at {
			if !at[i] {
				continue
			}
			if t.kind == anyRun {
				for j := i; j <= len(v); j++ {
					next[j] = true
				}
				reached = true
				break
			}
			if t.step(v, i, next) {
				reached = true
			}
		}
		if !reached {
			return false
		}
		at, next = next, at
	}
	return at[len(v)]
}

// step marks in next each position of v the token can end at when it
// starts at i, and reports whether it marked any: one rune on for a rune
// token that matches, each end of a digit run the range spells for a
// numeric range.
func (t token) step(v []rune, i int, next []bool) bool {
	if i == len(v) {
		return false
	}
	switch t.kind {
	case anyOne:
	case literal:
		if !equalFold(t.r, v[i]) {
			return false
		}
	case class:
		if !t.matches(v[i]) {
			return false
		}
	case numeric:
		marked := false
		for j := i; j < len(v) && isDigit(v[j]); j++ {
			if t.spells(v[i : j+1]) {
				next[j+1] = true
				marked = true
			}
		}
		return marked
	}
	next[i+1] = true
	return true
}

// spells reports whether digits is a name the numeric range spells: a
// number within an item's bounds, zero-padded to the item's width and no
// further.
func (t token) spells(digits []rune) bool {
	n := number(digits)
	for _, it := range t.items {
		if len(digits) < it.width || len(digits) > it.width && digits[0] == '0' {
			continue
		}
		if compareNumbers(n, it.lo) >= 0 && compareNumbers(n, it.hi) <= 0 {
			return true
		}
	}
	return false
}

// number is a run of decimal digits without its leading zeros, "0" for
// zero, so two numbers of any size compare by compareNumbers.
func number(digits []rune) []rune {
	z := 0
	for z < len(digits)-1 && digits[z] == '0' {
		z++
	}
	return digits[z:]
}

// compareNumbers orders two numbers as number writes them: the longer is
// larger, and two of one length compare digit by digit.
func compareNumbers(a, b []rune) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	for i := range a {
		if a[i] != b[i] {
			return int(a[i]) - int(b[i])
		}
	}
	return 0
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// Match reports whether s matches value, a negated selector matching when its
// pattern matches (the caller decides what a negation does).
func (s Selector) Match(value string) bool { return s.Pattern.Match(value) }

func (t token) matches(r rune) bool {
	in := false
	for _, f := range folds(r) {
		for _, rg := range t.ranges {
			if f >= rg.lo && f <= rg.hi {
				in = true
			}
		}
	}
	return in != t.negated
}

func equalFold(a, b rune) bool {
	if a == b {
		return true
	}
	for f := unicode.SimpleFold(a); f != a; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
	}
	return false
}

// folds is r and every rune simple case folding makes equal to it.
func folds(r rune) []rune {
	out := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		out = append(out, f)
	}
	return out
}
