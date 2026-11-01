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
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
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
		trim := strings.TrimSpace(raw)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if endsContinuation(raw) {
			return nil, fmt.Errorf("%s:%d: multiline continuation is unsupported", canonical, line)
		}
		tokens, err := tokenize(raw)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", canonical, line, err)
		}
		if len(tokens) == 0 {
			continue
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
				if code := errorcodes.Of(err); code != "" {
					return nil, errorcodes.Errorf(code, "%s:%d include: %s", canonical, line, errorcodes.Message(err))
				}
				return nil, fmt.Errorf("%s:%d include: %w", canonical, line, err)
			}
			out = append(out, nested...)
			continue
		}
		if !strings.EqualFold(tokens[0], "add") || len(tokens) < 4 {
			return nil, fmt.Errorf("unsupported statement")
		}
		d := strings.ToLower(tokens[1])
		pattern := tokens[2]
		values := tokens[3:]
		switch d {
		case "user", "userpassword":
			if len(values) != 1 {
				return nil, fmt.Errorf("add %s requires exactly one value", d)
			}
		case "password":
			if len(values) < 1 || len(values) > 2 {
				return nil, fmt.Errorf("add password requires one or two values")
			}
		default:
			if !ignored[d] {
				return nil, fmt.Errorf("cloginrc_unknown_directive: %s", d)
			}
			if len(values) < 1 {
				return nil, fmt.Errorf("ignored directive %s requires a value", d)
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
		return nil, err
	}
	return out, nil
}
func endsContinuation(s string) bool {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}
func tokenize(s string) ([]string, error) {
	out := []string{}
	i := 0
	for {
		i = skipSpace(s, i)
		if i >= len(s) {
			break
		}
		if s[i] == '#' {
			break
		}
		var v string
		var err error
		if s[i] == '{' {
			v, i, err = parseBraced(s, i)
		} else {
			v, i, err = parseBare(s, i)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func skipSpace(s string, i int) int {
	for i < len(s) && unicode.IsSpace(rune(s[i])) {
		i++
	}
	return i
}

// parseBraced reads a braced word as Tcl does: no substitution inside the
// braces, so a backslash and the byte after it are kept as they stand, and
// an escaped brace does not count toward the nesting.
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
				return b.String(), i, nil
			}
			b.WriteByte(c)
			continue
		}
		if c == '$' || c == '[' || c == ']' || c == ';' {
			return "", i, errorcodes.Errorf("cloginrc_tcl_substitution_forbidden", "Tcl substitution/chaining syntax is forbidden")
		}
		b.WriteByte(c)
		i++
	}
	return "", i, fmt.Errorf("unterminated brace")
}
func parseBare(s string, i int) (string, int, error) {
	var b strings.Builder
	for i < len(s) && !unicode.IsSpace(rune(s[i])) {
		c := s[i]
		if c == '\\' {
			if i+1 >= len(s) {
				return "", i, fmt.Errorf("unfinished escape")
			}
			b.WriteByte(s[i+1])
			i += 2
			continue
		}
		if c == '$' || c == '[' || c == ']' || c == ';' {
			return "", i, errorcodes.Errorf("cloginrc_tcl_substitution_forbidden", "Tcl substitution/chaining syntax is forbidden")
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), i, nil
}
