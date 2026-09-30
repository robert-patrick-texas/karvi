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
}

// String is the pattern as written.
func (p Pattern) String() string { return p.source }

type tokenKind int

const (
	literal tokenKind = iota
	anyOne
	anyRun
	class
)

type token struct {
	kind    tokenKind
	r       rune
	negated bool
	ranges  []runeRange
}

type runeRange struct{ lo, hi rune }

const reservedReason = "an unescaped %q is not allowed: a selector already matches the whole field, \"[!...]\" excludes characters, and \\^ and \\$ are the literal characters"

func compile(value, pattern string) (Pattern, error) {
	runes := []rune(pattern)
	p := Pattern{source: pattern}
	bad := func(format string, args ...any) error {
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

// compileClass reads a class whose "[" is at start-1 and returns the index of
// its closing "]". A "-" first or last in the class is literal; "]" inside a
// class is written "\]".
func compileClass(runes []rune, start int, bad func(string, ...any) error) (token, int, error) {
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
				return token{}, 0, bad("the range %q-%q is reversed", string(lo), string(hi))
			}
		}
		t.ranges = append(t.ranges, runeRange{lo, hi})
		i = next
	}
}

func classRune(runes []rune, i int, bad func(string, ...any) error) (rune, int, error) {
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

// Match reports whether the whole of value matches, without regard to case.
func (p Pattern) Match(value string) bool {
	v := []rune(value)
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
