// Package cloginrc implements the non-executing RANCID credential subset
// under the credential-file rules: a declared user or
// shared scope, an optional or required root file, includes resolved from
// the including file's directory that inherit the scope, and one read per
// resolver. The file rules themselves live in the credfile package, shared
// with the csv backend.
package cloginrc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credfile"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

// Backend reads one .cloginrc file and its includes. The file rules (the
// scope, the checks on the opened descriptor, the availability classes,
// the read-once cache) are the shared credfile package's; the parser and
// the include logic are this package's own.
type Backend struct {
	Rules credfile.Rules
	Path  string // ~/.cloginrc when empty
	Warn  func(string)

	cache credfile.Cache[[]record]
}

// load reads and flattens the file once per backend and caches the records
// or the classified failure.
func (b *Backend) load(ctx context.Context) ([]record, *credentials.BackendResult) {
	path := b.Path
	if path == "" {
		path = "~/.cloginrc"
	}
	path, perr := b.Rules.ExpandPath(path)
	return b.cache.Load(ctx,
		func() ([]record, error) {
			if perr != nil {
				return nil, perr
			}
			return b.flatten(ctx, path, true, nil, map[string]string{})
		},
		func(err error) credentials.BackendResult { return b.Rules.Classify(path, err, "cloginrc_malformed") })
}

func (b *Backend) Name() string                { return b.Rules.BackendName }
func (*Backend) Mode() credentials.BackendMode { return credentials.DeviceKeyed }

type record struct {
	Directive, Pattern string
	Values             []string
	Path               string
	Line               int
}

var ignored = map[string]bool{"autoenable": true, "cyphertype": true, "enauser": true, "enableprompt": true, "identity": true, "method": true, "noenable": true, "passprompt": true, "sshcmd": true, "telnetcmd": true, "timeout": true, "userprompt": true}

func (b *Backend) Resolve(ctx context.Context, req credentials.ResolveRequest) credentials.BackendResult {
	records, failed := b.load(ctx)
	if failed != nil {
		return *failed
	}
	userRec := first(records, "user", req.Device.CanonicalName)
	passRec := first(records, "password", req.Device.CanonicalName)
	userPass := first(records, "userpassword", req.Device.CanonicalName)
	if passRec == nil && userPass == nil {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	username := req.Operator.Username
	if userRec != nil {
		username = userRec.Values[0]
	}
	password := ""
	if passRec != nil {
		password = passRec.Values[0]
	}
	if userPass != nil {
		password = userPass.Values[0]
	}
	enable := ""
	if passRec != nil && len(passRec.Values) == 2 {
		enable = passRec.Values[1]
	}
	matched := passRec
	if userPass != nil {
		matched = userPass
	}
	// The evidence describes what matched, the pattern, the file, and the
	// line, and never echoes the device being resolved: the planner merges
	// grants only when their evidence is equal, so
	// a device name here would give every device on one line its own sealed
	// copy of one secret. The record's device block names the device.
	cred := credentials.Credential{Material: secrets.NewMaterial(username, password, enable), Backend: b.Rules.BackendName, Policy: req.Policy, MatchedOn: credentials.Match{Category: "device_glob", Pattern: matched.Pattern, Source: matched.Path, Line: matched.Line}}
	return credentials.BackendResult{Outcome: credentials.Success, Credential: cred}
}
func first(rs []record, directive, name string) *record {
	for i := range rs {
		if rs[i].Directive == directive && match(rs[i].Pattern, name) {
			r := rs[i]
			return &r
		}
	}
	return nil
}

// match reports whether the device name matches a record's pattern as
// clogin matches it, with Tcl's string match -nocase: both folded to lower
// case, then compared by tclMatch.
func match(pattern, name string) bool {
	m := tclMatch{p: fold(pattern), s: fold(name)}
	return m.from(0, 0)
}

// fold is a pattern or a name as match compares it: lower case, in runes.
func fold(s string) []rune { return []rune(strings.ToLower(s)) }

// tclMatch is Tcl 8.6's string match (Tcl_StringCaseMatch) over runes. "*"
// matches any run of characters, "/" included; "?" matches one character;
// "\x" matches x; anything else matches itself. "[" opens a class, which
// matches one character and has no negation: see class.
type tclMatch struct {
	p, s []rune
	// failed holds the pattern and name positions after a star that were
	// tried and matched nothing, so that no position is tried twice.
	failed map[[2]int]bool
}

// from reports whether the pattern from p[pi] matches the name from s[si].
func (m *tclMatch) from(pi, si int) bool {
	for {
		if pi == len(m.p) {
			return si == len(m.s)
		}
		if si == len(m.s) && m.p[pi] != '*' {
			return false
		}
		switch m.p[pi] {
		case '*':
			for pi < len(m.p) && m.p[pi] == '*' {
				pi++
			}
			if pi == len(m.p) {
				return true
			}
			for ; si <= len(m.s); si++ {
				at := [2]int{pi, si}
				if m.failed[at] {
					continue
				}
				if m.from(pi, si) {
					return true
				}
				if m.failed == nil {
					m.failed = map[[2]int]bool{}
				}
				m.failed[at] = true
			}
			return false
		case '?':
			pi++
		case '[':
			next, ok := class(m.p, pi+1, m.s[si])
			if !ok {
				return false
			}
			pi = next
		case '\\':
			if pi++; pi == len(m.p) || m.p[pi] != m.s[si] {
				return false
			}
			pi++
		default:
			if m.p[pi] != m.s[si] {
				return false
			}
			pi++
		}
		si++
	}
}

// class matches the character c against the class whose items start at
// p[i], just past its "[", as Tcl reads one, and returns the position after
// the class. The items are characters and ranges ("a-z", read either way
// round), a backslash among the characters; the first item that holds c ends
// the class at the next "]" from there, or at the end of the pattern. The
// class does not match c once it reaches a "]" or the end of the pattern
// where an item would start, or a range whose dash ends the pattern.
func class(p []rune, i int, c rune) (int, bool) {
	for {
		if i == len(p) || p[i] == ']' {
			return 0, false
		}
		lo, hi := p[i], p[i]
		i++
		if i < len(p) && p[i] == '-' {
			if i++; i == len(p) {
				return 0, false
			}
			hi = p[i]
			i++
		}
		if min(lo, hi) <= c && c <= max(lo, hi) {
			break
		}
	}
	if end := slices.Index(p[i:], ']'); end >= 0 {
		return i + end + 1, true
	}
	return len(p), true
}

// canMatch reports whether any name matches the pattern. Tried in tclsh8.6,
// string match fails for every name once it reaches a backslash that ends
// the pattern, or a class that lists nothing: "[]", or a "[" or "[a-" that
// ends the pattern. Every other part of a pattern matches some character,
// or none for "*". A class with a range ending in "]" can end at more than
// one place, by which item matched, so canMatch follows every place a name
// can reach.
func canMatch(pattern string) bool {
	p := fold(pattern)
	reached := map[int]bool{}
	for todo := []int{0}; len(todo) > 0; {
		i := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		for i < len(p) && p[i] != '[' {
			if p[i] == '\\' {
				i++
			}
			i++
		}
		if i > len(p) {
			continue // a backslash that ends the pattern
		}
		if i == len(p) {
			return true
		}
		for _, c := range classChars(p, i+1) {
			if next, ok := class(p, i+1, c); ok && !reached[next] {
				reached[next] = true
				todo = append(todo, next)
			}
		}
	}
	return false
}

// classChars are characters a name can hold that, between them, reach
// every item of the class at p[i:] that can be the first to match: each
// item's lowest character and the first character a name can hold above
// each item. Without a range ending in "]", every item ends the class at
// the same place, and the first item's character alone reaches it.
func classChars(p []rune, i int) []rune {
	var cs []rune
	places := false
	for i < len(p) && p[i] != ']' {
		lo, hi := p[i], p[i]
		if i+1 < len(p) && p[i+1] == '-' {
			if i+2 == len(p) {
				break
			}
			hi = p[i+2]
			places = places || hi == ']'
			i += 2
		}
		i++
		cs = append(cs, min(lo, hi), nameChar(max(lo, hi)+1))
	}
	if !places && len(cs) > 0 {
		return cs[:1]
	}
	slices.Sort(cs)
	return slices.Compact(cs)
}

// nameChar is the first character from c up that a name folded to lower
// case can hold, or -1.
func nameChar(c rune) rune {
	for ; c <= unicode.MaxRune; c++ {
		if utf8.ValidRune(c) && unicode.ToLower(c) == c {
			return c
		}
	}
	return -1
}

// flatten reads one file and its includes in physical order. root marks
// the configured file, whose unavailability is classified by the caller;
// an unavailable include is always cloginrc_include_unavailable.
func (b *Backend) flatten(ctx context.Context, path string, root bool, active []string, seen map[string]string) ([]record, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	// Open first, then check what was opened; the
	// descriptor checked here is the one the scanner reads below.
	f, canonical, err := b.Rules.Open(path)
	if err != nil {
		if credfile.IsUnavailable(err) {
			if root {
				return nil, &credfile.UnavailableError{Err: err}
			}
			return nil, errorcodes.Errorf("cloginrc_include_unavailable", "%v", err)
		}
		return nil, err
	}
	defer f.Close()
	for _, p := range active {
		if p == canonical {
			return nil, errorcodes.Errorf("cloginrc_include_cycle", "cloginrc include cycle: %s -> %s", strings.Join(active, " -> "), canonical)
		}
	}
	if first, ok := seen[canonical]; ok {
		return nil, errorcodes.Errorf("cloginrc_include_duplicate", "cloginrc duplicate/diamond include %s (first from %s)", canonical, first)
	}
	seen[canonical] = path
	active = append(active, canonical)
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 1<<20)
	out := []record{}
	ignoredSeen := map[string]bool{}
	line := 0
	for s.Scan() {
		line++
		raw := strings.TrimSuffix(s.Text(), "\r")
		// A command begins at the line's first word, and a "#" there begins
		// a comment, as Tcl reads one.
		if at := skipTclSpace(raw, 0); at == len(raw) || raw[at] == '#' {
			continue
		}
		if endsContinuation(raw) {
			return nil, fmt.Errorf("%s:%d: multiline continuation is unsupported", canonical, line)
		}
		tokens, err := tokenize(raw)
		if err != nil {
			return nil, located(fmt.Sprintf("%s:%d: ", canonical, line), err)
		}
		if strings.EqualFold(tokens[0], "include") {
			if len(tokens) != 2 {
				return nil, fmt.Errorf("%s:%d: include requires exactly one value", canonical, line)
			}
			// Include locality: a relative
			// include resolves from the including file's directory; ~ forms
			// resolve to the operator's home under user scope and are
			// refused under shared scope; the included file inherits the
			// scope through the shared rules.
			inc := tokens[1]
			switch {
			case strings.HasPrefix(inc, "~") && inc != "~" && !strings.HasPrefix(inc, "~/"):
				return nil, errorcodes.Errorf("cloginrc_include_other_user_forbidden", "%s:%d: ~otheruser include is forbidden", canonical, line)
			case inc == "~" || strings.HasPrefix(inc, "~/"):
				if b.Rules.EffectiveScope() == credfile.ScopeShared {
					return nil, errorcodes.Errorf("cloginrc_include_home_in_shared", "%s:%d: a shared credential file cannot include from a home directory", canonical, line)
				}
				if inc == "~" {
					inc = b.Rules.Home
				} else {
					inc = filepath.Join(b.Rules.Home, inc[2:])
				}
			case !filepath.IsAbs(inc):
				inc = filepath.Join(filepath.Dir(canonical), inc)
			}
			nested, err := b.flatten(ctx, inc, false, active, seen)
			if err != nil {
				return nil, located(fmt.Sprintf("%s:%d include: ", canonical, line), err)
			}
			out = append(out, nested...)
			continue
		}
		if !strings.EqualFold(tokens[0], "add") || len(tokens) < 4 {
			return nil, fmt.Errorf("%s:%d: unsupported statement", canonical, line)
		}
		d := strings.ToLower(tokens[1])
		pattern := tokens[2]
		values := tokens[3:]
		switch d {
		case "user", "userpassword":
			if len(values) != 1 {
				return nil, fmt.Errorf("%s:%d: add %s requires exactly one value", canonical, line, d)
			}
		case "password":
			if len(values) > 2 {
				return nil, fmt.Errorf("%s:%d: add password requires one or two values", canonical, line)
			}
			// clogin sends the password and the enable password as
			// [join [lindex $pswd n] ""] gives them.
			for i, v := range values {
				joined, err := joinList(v)
				if err != nil {
					return nil, fmt.Errorf("%s:%d: the %s is not a Tcl list: %w", canonical, line, [2]string{"password", "enable password"}[i], err)
				}
				values[i] = joined
			}
		default:
			if !ignored[d] {
				return nil, errorcodes.Errorf("cloginrc_unknown_directive", "%s:%d: %s", canonical, line, d)
			}
			if !ignoredSeen[d] && b.Warn != nil {
				ignoredSeen[d] = true
				b.Warn(fmt.Sprintf("cloginrc_directive_ignored: %s at %s:%d", d, canonical, line))
			}
			continue
		}
		if !canMatch(pattern) {
			return nil, fmt.Errorf("%s:%d: add %s pattern cannot match", canonical, line, d)
		}
		out = append(out, record{Directive: d, Pattern: pattern, Values: values, Path: canonical, Line: line})
	}
	if err := s.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("%s:%d: a line of 1 MiB or more", canonical, line+1)
		}
		return nil, err
	}
	return out, nil
}

// located is err with where (a file and line) before its message. A coded
// error keeps its code once, leading the text; any other is wrapped.
func located(where string, err error) error {
	if code := errorcodes.Of(err); code != "" {
		return errorcodes.Errorf(code, "%s%s", where, strings.TrimPrefix(errorcodes.Message(err), code+": "))
	}
	return fmt.Errorf("%s%w", where, err)
}

func endsContinuation(s string) bool {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// tokenize reads a line's words as Tcl reads a command's words: white space
// (isTclSpace) between them; a word that starts with "{" is braced, one that
// starts with '"' quoted, and any other bare.
func tokenize(s string) ([]string, error) {
	out := []string{}
	for i := skipTclSpace(s, 0); i < len(s); i = skipTclSpace(s, i) {
		var v string
		var err error
		switch s[i] {
		case '{':
			v, i, err = parseBraced(s, i)
		case '"':
			v, i, err = parseQuoted(s, i)
		default:
			v, i, err = parseBare(s, i)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// parseBraced reads a braced word as Tcl does: no substitution inside the
// braces, so every character is kept as it stands, "$", "[", "]", and ";"
// among them, and a backslash with the byte after it; an escaped brace does
// not count toward the nesting. White space or the line's end follows the
// closing brace.
func parseBraced(s string, i int) (string, int, error) {
	i++
	depth := 1
	var b strings.Builder
	for i < len(s) {
		c := s[i]
		if c == '\\' {
			if i+1 >= len(s) {
				return "", i, fmt.Errorf("unfinished escape")
			}
			b.WriteString(s[i : i+2])
			i += 2
			continue
		}
		if c == '{' {
			depth++
			if depth > 1 {
				b.WriteByte(c)
			}
			i++
			continue
		}
		if c == '}' {
			depth--
			i++
			if depth == 0 {
				return b.String(), i, afterClose(s, i, "brace")
			}
			b.WriteByte(c)
			continue
		}
		b.WriteByte(c)
		i++
	}
	return "", i, fmt.Errorf("unterminated brace")
}

// parseQuoted reads a quoted word as Tcl does: it runs from its opening
// quote to the next quote not escaped, white space, ";", "]", and the braces
// among its characters, with its backslash sequences substituted
// (tclBackslash). "$" and "[" are refused, where Tcl would substitute. White
// space or the line's end follows the closing quote.
func parseQuoted(s string, i int) (string, int, error) {
	var b strings.Builder
	for i++; i < len(s); {
		switch c := s[i]; c {
		case '"':
			return pairSurrogates(b.String()), i + 1, afterClose(s, i+1, "quote")
		case '\\':
			v, n := tclBackslash(s[i:])
			b.WriteString(v)
			i += n
		case '$', '[':
			return "", i, substitution(c, "quoted")
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", i, fmt.Errorf("unterminated quote")
}

// parseBare reads a bare word as Tcl does: it runs to white space or the
// line's end, a quote or a brace in it a character like any other, with its
// backslash sequences substituted (tclBackslash). "$", "[", "]", and ";"
// are refused, where Tcl would substitute or end the command.
func parseBare(s string, i int) (string, int, error) {
	var b strings.Builder
	for i < len(s) && !isTclSpace(s[i]) {
		switch c := s[i]; c {
		case '\\':
			v, n := tclBackslash(s[i:])
			b.WriteString(v)
			i += n
		case '$', '[', ']', ';':
			return "", i, substitution(c, "bare")
		default:
			b.WriteByte(c)
			i++
		}
	}
	return pairSurrogates(b.String()), i, nil
}

// afterClose fails unless s[i], just after a braced or quoted word's closing
// brace or quote, is white space or the line's end, as Tcl requires. A ";"
// there is refused as chaining, where Tcl would end the command.
func afterClose(s string, i int, closing string) error {
	switch {
	case i >= len(s) || isTclSpace(s[i]):
		return nil
	case s[i] == ';':
		return errorcodes.Errorf("cloginrc_tcl_substitution_forbidden", "';' after a closing %s", closing)
	}
	return fmt.Errorf("text after a closing %s", closing)
}

// substitution refuses c in a word of the kind named, where Tcl would
// substitute or end the command at it.
func substitution(c byte, word string) error {
	return errorcodes.Errorf("cloginrc_tcl_substitution_forbidden", "%q in a %s word; brace the word", rune(c), word)
}

// joinList is value as clogin's [join $value ""] gives it, the way clogin
// takes an add password line's password and enable password: value read as
// a Tcl list, as Tcl 8.6 reads one, its elements joined with nothing
// between. White space (space, tab, newline, vertical tab, form feed,
// carriage return) separates the elements; listElement reads each one.
func joinList(value string) (string, error) {
	var b strings.Builder
	for i := skipTclSpace(value, 0); i < len(value); i = skipTclSpace(value, i) {
		end, err := listElement(value, i, &b)
		if err != nil {
			return "", err
		}
		i = end
	}
	return pairSurrogates(b.String()), nil
}

// isTclSpace reports whether c is white space as Tcl reads a command's words
// or a list's elements: space, tab, newline, vertical tab, form feed, or
// carriage return.
func isTclSpace(c byte) bool { return c == ' ' || '\t' <= c && c <= '\r' }

func skipTclSpace(s string, i int) int {
	for i < len(s) && isTclSpace(s[i]) {
		i++
	}
	return i
}

// listElement writes the list element at s[i] to b as Tcl 8.6's list
// parsing gives it and returns the position after it. A braced element is
// what stands between its braces, which nest, as written; a backslash
// sequence there is kept and its braces do not count. A quoted element
// runs to the next quote and a bare element to the next white space, each
// with its backslash sequences substituted (tclBackslash). An open brace or
// quote left unmatched, or text after an element's closing brace or quote,
// fails as it fails in Tcl.
func listElement(s string, i int, b *strings.Builder) (int, error) {
	switch s[i] {
	case '{':
		depth := 0
		for j := i; j < len(s); j++ {
			switch s[j] {
			case '\\':
				_, n := tclBackslash(s[j:])
				j += n - 1
			case '{':
				depth++
			case '}':
				if depth--; depth > 0 {
					continue
				}
				if j+1 < len(s) && !isTclSpace(s[j+1]) {
					return 0, fmt.Errorf("text after a closing brace")
				}
				b.WriteString(s[i+1 : j])
				return j + 1, nil
			}
		}
		return 0, fmt.Errorf("unmatched open brace")
	case '"':
		for j := i + 1; j < len(s); j++ {
			switch s[j] {
			case '\\':
				_, n := tclBackslash(s[j:])
				j += n - 1
			case '"':
				if j+1 < len(s) && !isTclSpace(s[j+1]) {
					return 0, fmt.Errorf("text after a closing quote")
				}
				collapse(s[i+1:j], b)
				return j + 1, nil
			}
		}
		return 0, fmt.Errorf("unmatched open quote")
	}
	j := i
	for j < len(s) && !isTclSpace(s[j]) {
		if s[j] == '\\' {
			_, n := tclBackslash(s[j:])
			j += n
			continue
		}
		j++
	}
	collapse(s[i:j], b)
	return j, nil
}

// collapse writes s to b with each backslash sequence replaced by the
// character it stands for.
func collapse(s string, b *strings.Builder) {
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			i++
			continue
		}
		c, n := tclBackslash(s[i:])
		b.WriteString(c)
		i += n
	}
}

// tclBackslash is the backslash sequence at the start of s as Tcl 8.6
// substitutes it (TclParseBackslash): the character it stands for and the
// bytes it takes. \a \b \f \n \r \t \v are the control characters; \x takes
// one or two hex digits, \u one to four, and \U one to eight, stopping
// before a digit that would pass U+10FFFF; one to three octal digits give a
// value up to 0377; a backslash, a newline, and the spaces and tabs after
// it are one space; a backslash before any other character is that
// character, and a backslash that ends s is a backslash.
func tclBackslash(s string) (string, int) {
	if len(s) < 2 {
		return `\`, len(s)
	}
	switch c := s[1]; c {
	case 'a':
		return "\a", 2
	case 'b':
		return "\b", 2
	case 'f':
		return "\f", 2
	case 'n':
		return "\n", 2
	case 'r':
		return "\r", 2
	case 't':
		return "\t", 2
	case 'v':
		return "\v", 2
	case 'x', 'u', 'U':
		digits := 2
		switch c {
		case 'u':
			digits = 4
		case 'U':
			digits = 8
		}
		v, n := tclHex(s[2:], digits)
		if n == 0 {
			return s[1:2], 2
		}
		return tclChar(v), 2 + n
	case '\n':
		n := 2
		for n < len(s) && (s[n] == ' ' || s[n] == '\t') {
			n++
		}
		return " ", n
	case '0', '1', '2', '3', '4', '5', '6', '7':
		v, n := rune(c-'0'), 2
		if n < len(s) && '0' <= s[n] && s[n] <= '7' {
			v, n = v<<3|rune(s[n]-'0'), n+1
			if n < len(s) && '0' <= s[n] && s[n] <= '7' && v < 0o40 {
				v, n = v<<3|rune(s[n]-'0'), n+1
			}
		}
		return tclChar(v), n
	}
	r, n := utf8.DecodeRuneInString(s[1:])
	if r == utf8.RuneError && n == 1 {
		return s[1:2], 2
	}
	return tclChar(r), 1 + n
}

// tclHex reads up to max hex digits at the start of s, as TclParseHex does:
// it stops before a digit once the value passes 0x10FFF.
func tclHex(s string, max int) (rune, int) {
	var v rune
	n := 0
	for ; n < max && n < len(s) && v <= 0x10fff; n++ {
		switch c := rune(s[n]); {
		case '0' <= c && c <= '9':
			v = v<<4 | (c - '0')
		case 'a' <= c && c <= 'f':
			v = v<<4 | (c - 'a' + 10)
		case 'A' <= c && c <= 'F':
			v = v<<4 | (c - 'A' + 10)
		default:
			return v, n
		}
	}
	return v, n
}

// tclChar is the character v in the UTF-8 tclsh8.6 (8.6.17) writes for it:
// a character past U+FFFF is U+FFFD, and a surrogate is written in three
// bytes of its own (pairSurrogates joins a high one followed by a low one).
func tclChar(v rune) string {
	switch {
	case v > 0xffff:
		return string(utf8.RuneError)
	case 0xd800 <= v && v <= 0xdfff:
		return string([]byte{0xe0 | byte(v>>12), 0x80 | byte(v>>6)&0x3f, 0x80 | byte(v)&0x3f})
	}
	return string(v)
}

// pairSurrogates is s with each high surrogate that comes just before a low
// one, three bytes each, written as the one character the pair stands for,
// as Tcl 8.6's UTF-8 encoding writes the pair.
func pairSurrogates(s string) string {
	if !strings.Contains(s, "\xed") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if hi, ok := surrogate(s[i:], 0xa0); ok {
			if lo, ok := surrogate(s[i+3:], 0xb0); ok {
				b.WriteRune(utf16.DecodeRune(hi, lo))
				i += 5
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// surrogate reads the three bytes of a surrogate at the start of s: a high
// one for half 0xa0, a low one for 0xb0.
func surrogate(s string, half byte) (rune, bool) {
	if len(s) < 3 || s[0] != 0xed || s[1]&0xf0 != half || s[2]&0xc0 != 0x80 {
		return 0, false
	}
	return 0xd000 | rune(s[1]&0x3f)<<6 | rune(s[2]&0x3f), true
}
