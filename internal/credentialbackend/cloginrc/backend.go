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
	"strings"
	"unicode"

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
	path = b.Rules.ExpandPath(path)
	return b.cache.Load(ctx,
		func() ([]record, error) { return b.flatten(ctx, path, true, nil, map[string]string{}) },
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
	cred := credentials.Credential{Material: secrets.NewMaterial(username, password, enable), Backend: b.Rules.BackendName, Policy: req.Policy, MatchedOn: credentials.Match{Category: "device_glob", Pattern: matched.Pattern, Source: matched.Path, Line: matched.Line}, FieldSources: map[string]credentials.FieldSource{}}
	if userRec != nil {
		cred.FieldSources["username"] = credentials.FieldSource{Backend: b.Rules.BackendName, Path: fmt.Sprintf("%s:%d user %s", userRec.Path, userRec.Line, userRec.Pattern)}
	} else {
		cred.FieldSources["username"] = credentials.FieldSource{Backend: b.Rules.BackendName, Path: "operator-default"}
	}
	if userPass != nil {
		cred.FieldSources["password"] = credentials.FieldSource{Backend: b.Rules.BackendName, Path: fmt.Sprintf("%s:%d userpassword %s", userPass.Path, userPass.Line, userPass.Pattern)}
	} else if passRec != nil {
		cred.FieldSources["password"] = credentials.FieldSource{Backend: b.Rules.BackendName, Path: fmt.Sprintf("%s:%d password %s", passRec.Path, passRec.Line, passRec.Pattern)}
	}
	if passRec != nil && len(passRec.Values) == 2 {
		cred.FieldSources["enable_password"] = credentials.FieldSource{Backend: b.Rules.BackendName, Path: fmt.Sprintf("%s:%d password %s", passRec.Path, passRec.Line, passRec.Pattern)}
	}
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
func match(pattern, name string) bool {
	pattern = normalizeNegatedClass(strings.ToLower(pattern))
	name = strings.ToLower(name)
	ok, _ := filepath.Match(pattern, name)
	return ok
}
func normalizeNegatedClass(s string) string { return strings.ReplaceAll(s, "[!", "[^") }

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
			b.WriteByte(s[i+1])
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
