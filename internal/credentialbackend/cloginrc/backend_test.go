package cloginrc

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credfile"
	"github.com/robert-patrick-texas/karvi/inventory"
)

const content = "add user router-* {svc.user}\nadd password router-* {pass one} {enable}\nadd userpassword special-* override\n"

func currentNames(t *testing.T) (string, string) {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	g, err := user.LookupGroupId(strconv.Itoa(os.Getgid()))
	if err != nil {
		t.Fatal(err)
	}
	return u.Username, g.Name
}

func newBackend(t *testing.T, home, path string, scope credfile.Scope, required bool) *Backend {
	t.Helper()
	username, group := currentNames(t)
	return &Backend{Path: path, Rules: credfile.Rules{BackendName: "rancid", Home: home, UID: os.Getuid(), Scope: scope, Required: required, ApprovedAdminUsers: []string{username}, SharedGroup: group}}
}

func resolve(b *Backend, name string) credentials.BackendResult {
	return b.Resolve(context.Background(), credentials.ResolveRequest{Operator: credentials.Operator{Username: "me"}, Device: inventory.Direct(name, "generic", "system", 0), Policy: "default"})
}

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func username(t *testing.T, r credentials.BackendResult) string {
	t.Helper()
	if r.Outcome != credentials.Success {
		t.Fatalf("%+v", r)
	}
	var u string
	_ = r.Credential.Material.WithUsername(func(v []byte) error { u = string(v); return nil })
	return u
}

func TestResolve(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	write(t, p, content, 0o600)
	b := newBackend(t, d, p, credfile.ScopeUser, false)
	if u := username(t, resolve(b, "router-1")); u != "svc.user" {
		t.Fatalf("user=%s", u)
	}
}

// TestEvidenceNeverEchoesTheDevice: two
// devices that take one line carry equal evidence (the pattern, the file,
// the line), with no safe_value naming either device.
func TestEvidenceNeverEchoesTheDevice(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	write(t, p, content, 0o600)
	b := newBackend(t, d, p, credfile.ScopeUser, false)
	one, two := resolve(b, "router-1").Credential.MatchedOn, resolve(b, "router-2").Credential.MatchedOn
	want := credentials.Match{Category: "device_glob", Pattern: "router-*", Source: p, Line: 2}
	if one != want || two != want {
		t.Errorf("evidence %+v and %+v, want %+v", one, two, want)
	}
}

// TestAvailabilityClasses: an absent,
// untraversable, or unreadable root file is a silent NotFound for an
// optional user backend and credential_file_unavailable, naming the
// backend and path, for a required user backend and for a shared one.
func TestAvailabilityClasses(t *testing.T) {
	d := t.TempDir()
	write(t, filepath.Join(d, "file"), "x", 0o600)
	locked := filepath.Join(d, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(locked, "x.rc"), content, 0o600)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	paths := []struct{ name, path string }{
		{"absent", filepath.Join(d, "missing.rc")},
		{"untraversable", filepath.Join(d, "file", "x.rc")},
		{"unreadable", filepath.Join(locked, "x.rc")},
	}
	for _, pc := range paths {
		if pc.name == "unreadable" && os.Geteuid() == 0 {
			continue
		}
		for _, bc := range []struct {
			name     string
			scope    credfile.Scope
			required bool
			want     credentials.Outcome
		}{
			{"user optional", credfile.ScopeUser, false, credentials.NotFound},
			{"user required", credfile.ScopeUser, true, credentials.Unavailable},
			{"shared", credfile.ScopeShared, true, credentials.Unavailable},
		} {
			r := resolve(newBackend(t, d, pc.path, bc.scope, bc.required), "router-1")
			if r.Outcome != bc.want {
				t.Errorf("%s, %s: outcome %s, want %s (%s)", pc.name, bc.name, r.Outcome, bc.want, r.Message)
				continue
			}
			if bc.want == credentials.NotFound {
				if r.ErrorCode != "" || r.Message != "" {
					t.Errorf("%s, %s: a silent NotFound carries %q %q", pc.name, bc.name, r.ErrorCode, r.Message)
				}
				continue
			}
			if r.ErrorCode != "credential_file_unavailable" || !strings.Contains(r.Message, "rancid") || !strings.Contains(r.Message, pc.path) || strings.Contains(r.Message, "svc.user") {
				t.Errorf("%s, %s: %q %q", pc.name, bc.name, r.ErrorCode, r.Message)
			}
			t.Logf("%s, %s: %s: %s", pc.name, bc.name, r.ErrorCode, r.Message)
		}
	}
}

// TestUnsafeFileFailsEvenWhenOptional: an unsafe file fails its check
// whether or not the backend is required.
func TestUnsafeFileFailsEvenWhenOptional(t *testing.T) {
	d := t.TempDir()
	wide := filepath.Join(d, "wide.rc")
	write(t, wide, content, 0o644)
	link := filepath.Join(d, "link.rc")
	if err := os.Symlink(wide, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, path, code string }{
		{"mode", wide, "credential_file_mode_unsafe"},
		{"symlink", link, "credential_file_symlink_rejected"},
		{"directory", d, "credential_file_not_regular"},
	} {
		r := resolve(newBackend(t, d, tc.path, credfile.ScopeUser, false), "router-1")
		if r.Outcome != credentials.Malformed || r.ErrorCode != tc.code {
			t.Errorf("%s: %+v", tc.name, r)
		}
	}
}

// TestSharedScopeIsDeclared: a file anywhere is checked as
// shared once declared, mode 0640, approved owner, the shared group.
func TestSharedScopeIsDeclared(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "shared.rc")
	write(t, p, content, 0o640)
	if u := username(t, resolve(newBackend(t, d, p, credfile.ScopeShared, true), "router-1")); u != "svc.user" {
		t.Fatalf("user=%s", u)
	}
	// The same file under user scope fails its mode check.
	if r := resolve(newBackend(t, d, p, credfile.ScopeUser, false), "router-1"); r.ErrorCode != "credential_file_mode_unsafe" {
		t.Errorf("user scope over a 0640 file: %+v", r)
	}
	unknown := newBackend(t, d, p, credfile.ScopeShared, true)
	unknown.Rules.SharedGroup = "karvi-no-such-group-xyz"
	if r := resolve(unknown, "router-1"); r.ErrorCode != "credential_file_shared_group_unknown" {
		t.Errorf("unknown group: %+v", r)
	}
	unapproved := newBackend(t, d, p, credfile.ScopeShared, true)
	unapproved.Rules.ApprovedAdminUsers = nil
	if r := resolve(unapproved, "router-1"); os.Geteuid() != 0 && r.ErrorCode != "credential_file_shared_owner_unapproved" {
		t.Errorf("unapproved owner: %+v", r)
	}
	tight := filepath.Join(d, "tight.rc")
	write(t, tight, content, 0o600)
	if r := resolve(newBackend(t, d, tight, credfile.ScopeShared, true), "router-1"); r.ErrorCode != "credential_file_shared_mode_invalid" {
		t.Errorf("0600 under shared: %+v", r)
	}
}

// TestIncludeLocality covers where an include resolves from and what the
// included file inherits.
func TestIncludeLocality(t *testing.T) {
	d := t.TempDir()
	home := filepath.Join(d, "home")
	creds := filepath.Join(d, "creds")
	for _, dir := range []string{home, creds} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// A relative include resolves beside the including file, not in HOME.
	root := filepath.Join(creds, "root.rc")
	write(t, root, "include sub.rc\n", 0o600)
	write(t, filepath.Join(creds, "sub.rc"), content, 0o600)
	r := resolve(newBackend(t, home, root, credfile.ScopeUser, false), "router-1")
	if u := username(t, r); u != "svc.user" || r.Credential.MatchedOn.Source != filepath.Join(creds, "sub.rc") {
		t.Fatalf("relative include: user=%s source=%s", u, r.Credential.MatchedOn.Source)
	}
	// ~/ resolves to the operator's home under user scope.
	write(t, filepath.Join(home, "home.rc"), content, 0o600)
	tilde := filepath.Join(creds, "tilde.rc")
	write(t, tilde, "include ~/home.rc\n", 0o600)
	if u := username(t, resolve(newBackend(t, home, tilde, credfile.ScopeUser, false), "router-1")); u != "svc.user" {
		t.Fatalf("~ include: user=%s", u)
	}
	// An included file inherits the scope and passes its checks on its own:
	// a 0640 file included from a user-scope root fails the user mode rule.
	write(t, filepath.Join(creds, "wide.rc"), content, 0o640)
	wideRoot := filepath.Join(creds, "wide-root.rc")
	write(t, wideRoot, "# comment\ninclude wide.rc\n", 0o600)
	if r := resolve(newBackend(t, home, wideRoot, credfile.ScopeUser, false), "router-1"); r.ErrorCode != "credential_file_mode_unsafe" || !strings.Contains(r.Message, "wide-root.rc:2 include") {
		t.Errorf("inherited scope: %+v", r)
	}
	// A missing include is never optional.
	missingRoot := filepath.Join(creds, "missing-root.rc")
	write(t, missingRoot, "include nowhere.rc\n", 0o600)
	if r := resolve(newBackend(t, home, missingRoot, credfile.ScopeUser, false), "router-1"); r.Outcome != credentials.Malformed || r.ErrorCode != "cloginrc_include_unavailable" || !strings.Contains(r.Message, "missing-root.rc:1 include") {
		t.Errorf("missing include: %+v", r)
	}
	// Under shared scope a ~ include is refused before any file is read.
	sharedRoot := filepath.Join(creds, "shared-root.rc")
	write(t, sharedRoot, "include ~/home.rc\n", 0o640)
	if r := resolve(newBackend(t, home, sharedRoot, credfile.ScopeShared, true), "router-1"); r.ErrorCode != "cloginrc_include_home_in_shared" || !strings.Contains(r.Message, "shared-root.rc:1") {
		t.Errorf("~ under shared: %+v", r)
	}
	// A cycle keeps its code through the include sites.
	a := filepath.Join(creds, "a.rc")
	write(t, a, "include b.rc\n", 0o600)
	write(t, filepath.Join(creds, "b.rc"), "include a.rc\n", 0o600)
	if r := resolve(newBackend(t, home, a, credfile.ScopeUser, false), "router-1"); r.ErrorCode != "cloginrc_include_cycle" {
		t.Errorf("cycle: %+v", r)
	}
	// ~otheruser stays forbidden.
	other := filepath.Join(creds, "other.rc")
	write(t, other, "include ~root/.cloginrc\n", 0o600)
	if r := resolve(newBackend(t, home, other, credfile.ScopeUser, false), "router-1"); r.ErrorCode != "cloginrc_include_other_user_forbidden" {
		t.Errorf("~otheruser: %+v", r)
	}
}

// TestReadOnce: the file is read on first use and the
// result serves every later resolution of the same backend.
func TestReadOnce(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-0 file")
	}
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	write(t, p, content, 0o600)
	b := newBackend(t, d, p, credfile.ScopeUser, false)
	if u := username(t, resolve(b, "router-1")); u != "svc.user" {
		t.Fatalf("user=%s", u)
	}
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
	if u := username(t, resolve(b, "router-2")); u != "svc.user" {
		t.Fatalf("second resolution did not use the cache: user=%s", u)
	}
	// A fresh backend sees the file as it is now: unsafe mode, not cached.
	if r := resolve(newBackend(t, d, p, credfile.ScopeUser, false), "router-1"); r.ErrorCode != "credential_file_mode_unsafe" {
		t.Errorf("fresh backend: %+v", r)
	}
	// A failure is cached as well.
	absent := newBackend(t, d, filepath.Join(d, "missing.rc"), credfile.ScopeUser, true)
	if r := resolve(absent, "router-1"); r.ErrorCode != "credential_file_unavailable" {
		t.Fatalf("%+v", r)
	}
	write(t, filepath.Join(d, "missing.rc"), content, 0o600)
	if r := resolve(absent, "router-1"); r.ErrorCode != "credential_file_unavailable" {
		t.Errorf("failure not cached: %+v", r)
	}
}

func secret(t *testing.T, r credentials.BackendResult) (password, enable string) {
	t.Helper()
	if r.Outcome != credentials.Success {
		t.Fatalf("%+v", r)
	}
	_ = r.Credential.Material.WithPassword(func(v []byte) error { password = string(v); return nil })
	_ = r.Credential.Material.WithEnablePassword(func(v []byte) error { enable = string(v); return nil })
	return password, enable
}

// TestPatternThatCannotMatchIsRefused: a pattern no name can match under
// Tcl's string match (a class that lists nothing: "[]", or a "[" or "[1-"
// that ends the pattern; a trailing backslash) fails the load with
// cloginrc_malformed, naming the file and the line and never a value,
// rather than passing every device on to a later line.
func TestPatternThatCannotMatchIsRefused(t *testing.T) {
	d := t.TempDir()
	for _, tc := range []struct{ name, line string }{
		{"a class that ends the pattern", `add password router\[ {pw-a}`},
		{"an empty class", `add password router\[\]1 {pw-a}`},
		{"a class whose only range has no end", `add password router\[1- {pw-a}`},
		{"a trailing backslash", `add password router\\ {pw-a}`},
		{"a class that ends the pattern after a star", `add password router*\[ {pw-a}`},
		{"in add user", `add user router\[ {svc.a}`},
	} {
		p := filepath.Join(d, "cloginrc")
		write(t, p, "# lab\n"+tc.line+"\nadd user * {svc.default}\nadd password * {pw-default}\n", 0o600)
		r := resolve(newBackend(t, d, p, credfile.ScopeUser, false), "router1")
		if r.Outcome != credentials.Malformed || r.ErrorCode != "cloginrc_malformed" {
			t.Errorf("%s: outcome %s, code %q, want cloginrc_malformed (%s)", tc.name, r.Outcome, r.ErrorCode, r.Message)
			continue
		}
		if !strings.Contains(r.Message, p+":2") || strings.Contains(r.Message, "pw-") || strings.Contains(r.Message, "svc.") || strings.Contains(r.Message, "router") {
			t.Errorf("%s: the message does not name the file and line alone: %s", tc.name, r.Message)
		}
		t.Logf("%s: %s: %s", tc.name, r.ErrorCode, r.Message)
	}
	// A class left open runs to the end of the pattern and matches one of
	// its characters; a complete class matches as well.
	for _, tc := range []struct{ name, line, device, want string }{
		{"an open class", `add password router\[12 {pw-a}`, "router1", "pw-a"},
		{"an open class, a character it does not list", `add password router\[12 {pw-a}`, "router3", "pw-default"},
		{"a complete class", `add password router\[12\] {pw-a}`, "router2", "pw-a"},
	} {
		p := filepath.Join(d, "class")
		write(t, p, tc.line+"\nadd password * {pw-default}\n", 0o600)
		if pw, _ := secret(t, resolve(newBackend(t, d, p, credfile.ScopeUser, false), tc.device)); pw != tc.want {
			t.Errorf("%s: %s takes password %q, want %q", tc.name, tc.device, pw, tc.want)
		}
	}
}

// TestClassHasNoNegation: a class matches only the characters it lists, as
// clogin's Tcl string match reads it: "[!x]" matches "!" and "x", "[^x]"
// matches "^" and "x", and neither matches any other character. The lines
// write the class in a bare word with escaped brackets, which the reader
// takes as Tcl does.
func TestClassHasNoNegation(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	write(t, p, "add password r\\[!x\\]* {pw-bang}\nadd password s\\[^x\\]* {pw-caret}\nadd password * {pw-default}\n", 0o600)
	b := newBackend(t, d, p, credfile.ScopeUser, false)
	for _, tc := range []struct{ device, want string }{
		{"r!1", "pw-bang"},
		{"rx1", "pw-bang"},
		{"ry1", "pw-default"},
		{"s^1", "pw-caret"},
		{"sx1", "pw-caret"},
		{"sy1", "pw-default"},
	} {
		if pw, _ := secret(t, resolve(b, tc.device)); pw != tc.want {
			t.Errorf("%s: password %q, want %q", tc.device, pw, tc.want)
		}
	}
}

// TestPatternMatchesAsTclStringMatch: a pattern matches a name exactly as
// clogin's [string match -nocase pattern name] does. The answers are
// tclsh8.6's (8.6.17), recorded here so that the test needs no Tcl.
func TestPatternMatchesAsTclStringMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{`r[!x]*`, `r!1`, true},
		{`r[!x]*`, `rx1`, true},
		{`r[!x]*`, `ry1`, false},
		{`r[^x]*`, `r^1`, true},
		{`r[^x]*`, `rx1`, true},
		{`r[^x]*`, `ry1`, false},
		{`[!]`, `!`, true},
		{`[^]`, `^`, true},
		{`[!a-c]`, `b`, true},
		{`[!a-c]`, `d`, false},
		{`r[0-9]`, `r5`, true},
		{`r[9-0]`, `r5`, true},
		{`r[z-a]`, `rm`, true},
		{`r[a-c]`, `rd`, false},
		{`r[Z-a]`, `rq`, true},
		{`r[Z-a]`, `r_`, false},
		{`[a-]`, `_`, true},
		{`[a-]`, `]`, true},
		{`[a-]`, `-`, false},
		{`[-a]`, `-`, true},
		{`[a-c-e]`, `d`, false},
		{`[a-c-e]`, `-`, true},
		{`[a-c-e]`, `e`, true},
		{`[]a]`, `a`, false},
		{`[]a]`, `]`, false},
		{`[]a]`, `]a]`, false},
		{`[]`, `]`, false},
		{`r[]x]`, `r]x]`, false},
		{`a]`, `a]`, true},
		{`router[12`, `router1`, true},
		{`router[12`, `router2`, true},
		{`router[12`, `router3`, false},
		{`router[12`, `router12`, false},
		{`r[a-`, `ra`, false},
		{`r[a-`, `r-`, false},
		{`r[`, `r[`, false},
		{`r[`, `r`, false},
		{`r[ab-`, `ra`, true},
		{`r[ab-`, `rb`, false},
		{`r[x*`, `r*`, true},
		{`r[x*`, `rx`, true},
		{`r[x*`, `rxy`, false},
		{`r\[1\]`, `r[1]`, true},
		{`r\[1\]`, `r1`, false},
		{`r\*`, `r*`, true},
		{`r\*`, `rx`, false},
		{`r\x`, `rx`, true},
		{`r\`, `r\`, false},
		{`r\`, `r`, false},
		{`r\\`, `r\`, true},
		{`r[\]]`, `r\]`, true},
		{`r[\]]`, `r]`, false},
		{`r[\]`, `r\`, true},
		{`r[\-a]`, `r_`, true},
		{`r[\-a]`, `r-`, false},
		{`r[a\`, `r\`, true},
		{`r[a\`, `ra`, true},
		{`*`, `a/b`, true},
		{`r*1`, `r/x/1`, true},
		{`r?1`, `r/1`, true},
		{`core/*`, `core/a/b`, true},
		{`?`, ``, false},
		{`*`, ``, true},
		{`ROUTER-*`, `router-1`, true},
		{`router-*`, `ROUTER-1`, true},
		{`R[A-C]1`, `rb1`, true},
		{`r[a-c]1`, `RB1`, true},
		{`r[!X]`, `rx`, true},
		{`[ab-][]`, `[`, true},
		{`[ab-][]`, `_`, true},
		{`[ab-][]`, `a`, false},
		{`[ab-]x]`, `ax]`, true},
		{`[ab-]x]`, `_`, true},
		{`[ab-]x]`, `ax`, false},
		{`r*[]`, `r`, false},
		{`*\`, `x\`, false},
		{`a**b`, `ab`, true},
		{`*a*b*`, `xxaxxbxx`, true},
		{`[*]`, `*`, true},
		{`[*]`, `x`, false},
		{`[?]`, `?`, true},
		{`[?]`, `a`, false},
		{`ÉCOLE-[1-3]`, `école-2`, true},
		// The SELECTOR profile's numeric range is not clogin's: a bracket
		// here stays Tcl's one-character class (tclsh8.6 agrees).
		{`router[10-12]`, `router1`, true},
		{`router[10-12]`, `router0`, true},
		{`router[10-12]`, `router10`, false},
		{`r[1,3]`, `r,`, true},
		{`r[1,3]`, `r2`, false},
	} {
		if got := match(tc.pattern, tc.name); got != tc.want {
			t.Errorf("match(%q, %q) = %v, Tcl says %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

// TestPatternRefusedOnlyWhenTclMatchesNothing: canMatch is false exactly for
// the patterns Tcl's string match -nocase accepts no name with, found by
// trying in tclsh8.6 every name of up to three characters (alone and after
// the pattern's leading plain characters); for the others, the name Tcl
// accepted matches here too.
func TestPatternRefusedOnlyWhenTclMatchesNothing(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		can     bool
		witness string
	}{
		{`\`, false, ``},
		{`[`, false, ``},
		{`[]`, false, ``},
		{`[]x]`, false, ``},
		{`[a-`, false, ``},
		{`[--`, false, ``},
		{`*\`, false, ``},
		{`*[`, false, ``},
		{`*[]*`, false, ``},
		{`[ab-]]\`, false, ``},
		{`router\`, false, ``},
		{`router[`, false, ``},
		{`router[]`, false, ``},
		{`router[]x]`, false, ``},
		{`router[a-`, false, ``},
		{`router*[`, false, ``},
		{`r[]*`, false, ``},
		{`router[12`, true, `router1`},
		{`router[ab-`, true, `routera`},
		{`r[a-]`, true, `ra`},
		{`r[a--`, true, `ra`},
		{`r[-`, true, `r-`},
		{`r[a\`, true, `ra`},
		{`r[\]`, true, `r\`},
		{`r\[1\]`, true, `r[1]`},
		{`r[!x]*`, true, `rx`},
		{`r[^x]*`, true, `rx`},
		{`[ab-][]`, true, `b`},
		{`[ab-][\`, true, `b`},
		{`*`, true, ``},
		{`?`, true, `a`},
	} {
		if got := canMatch(tc.pattern); got != tc.can {
			t.Errorf("canMatch(%q) = %v, Tcl says %v", tc.pattern, got, tc.can)
		}
		if tc.can && !match(tc.pattern, tc.witness) {
			t.Errorf("match(%q, %q) = false, Tcl says true", tc.pattern, tc.witness)
		}
	}
}

// TestBracedWordKeepsBackslashes: inside braces a backslash and the byte
// after it are kept, as Tcl keeps them (no backslash substitution inside
// braces); an escaped brace still does not count toward the nesting. A bare
// word's backslash sequence is substituted, as Tcl substitutes it ("\s" is
// "s"). The add user and add userpassword values show the words as read.
func TestBracedWordKeepsBackslashes(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	write(t, p, "add user router-* {svc\\user}\nadd userpassword router-* {en\\}a\\{b\\\\}\nadd userpassword switch-* pa\\ss\n", 0o600)
	b := newBackend(t, d, p, credfile.ScopeUser, false)
	r := resolve(b, "router-1")
	pw, _ := secret(t, r)
	if u := username(t, r); u != `svc\user` || pw != `en\}a\{b\\` {
		t.Errorf("braced: user %q, user password %q, want %q and %q", u, pw, `svc\user`, `en\}a\{b\\`)
	}
	if pw, _ := secret(t, resolve(b, "switch-1")); pw != "pass" {
		t.Errorf("bare: user password %q, want %q", pw, "pass")
	}
	// A braced pattern keeps its backslash too, so an escaped bracket is a
	// literal bracket for the match, as clogin's string match reads it.
	q := filepath.Join(d, "pattern")
	write(t, q, "add password {router\\[1\\]} {pw-a}\nadd password * {pw-default}\n", 0o600)
	b = newBackend(t, d, q, credfile.ScopeUser, false)
	if pw, _ := secret(t, resolve(b, "router[1]")); pw != "pw-a" {
		t.Errorf("router[1]: password %q, want pw-a", pw)
	}
	if pw, _ := secret(t, resolve(b, "router1")); pw != "pw-default" {
		t.Errorf("router1: password %q, want pw-default", pw)
	}
}

// TestBracedWordTakesSubstitutionCharactersLiterally: inside braces Tcl
// substitutes nothing, so "$", "[", "]", and ";" in a braced pattern or
// value are characters like any other. The lines and the values are the
// ones tclsh8.6 (8.6.17) gave through clogin's own add and find.
func TestBracedWordTakesSubstitutionCharactersLiterally(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	write(t, p, "add user {router$1} {svc$user}\n"+
		"add password {router$1} {pa$$w0rd} {en$able}\n"+
		"add password {router;1} {pa;ss} {en;able}\n"+
		"add password {router]1} {pa]ss} {en]able}\n"+
		"add password {r[!x]*} {pa[ss} {en[able]}\n"+
		"add userpassword {switch[12]} {up$[;]}\n"+
		"add password * {pw-default}\n", 0o600)
	b := newBackend(t, d, p, credfile.ScopeUser, false)
	for _, tc := range []struct{ device, user, password, enable string }{
		{"router$1", "svc$user", "pa$$w0rd", "en$able"},
		{"router;1", "me", "pa;ss", "en;able"},
		{"router]1", "me", "pa]ss", "en]able"},
		{"r!1", "me", "pa[ss", "en[able]"},
		{"switch2", "me", "up$[;]", ""},
		{"switch3", "me", "pw-default", ""},
	} {
		r := resolve(b, tc.device)
		if r.Outcome != credentials.Success {
			t.Fatalf("%s: outcome %s, code %q (%s)", tc.device, r.Outcome, r.ErrorCode, r.Message)
		}
		pw, en := secret(t, r)
		if u := username(t, r); u != tc.user || pw != tc.password || en != tc.enable {
			t.Errorf("%s: user %q, password %q, enable %q; want %q, %q, %q", tc.device, u, pw, en, tc.user, tc.password, tc.enable)
		}
	}
}

// TestBracedClassMatchesAsClogin: a class written inside braces is a class
// for the match, as clogin's Tcl reads it: {r[!x]*} matches "r!1" and
// "rx1", and "ry1" falls to the next line.
func TestBracedClassMatchesAsClogin(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	write(t, p, "add password {r[!x]*} {pw-braced}\nadd password * {pw-default}\n", 0o600)
	b := newBackend(t, d, p, credfile.ScopeUser, false)
	for _, tc := range []struct{ device, want string }{
		{"r!1", "pw-braced"},
		{"rx1", "pw-braced"},
		{"ry1", "pw-default"},
	} {
		r := resolve(b, tc.device)
		if r.Outcome != credentials.Success {
			t.Fatalf("%s: outcome %s, code %q (%s)", tc.device, r.Outcome, r.ErrorCode, r.Message)
		}
		if pw, _ := secret(t, r); pw != tc.want {
			t.Errorf("%s: password %q, want %q", tc.device, pw, tc.want)
		}
	}
}

// TestBareWordRefusesSubstitutionCharacters: in a bare word Tcl would
// substitute or end the command at "$", "[", "]", or ";", so the load is
// refused with cloginrc_tcl_substitution_forbidden. The reader's error
// holds the code once, leading, then the file and line (an included file's
// after the include's), the character, and the remedy, never the word.
func TestBareWordRefusesSubstitutionCharacters(t *testing.T) {
	const code = "cloginrc_tcl_substitution_forbidden"
	d := t.TempDir()
	root := filepath.Join(d, "cloginrc")
	sub := filepath.Join(d, "sub.rc")
	for _, tc := range []struct{ name, line, message string }{
		{"a dollar", "add password router1 pa$$w0rd", `'$' in a bare word; brace the word`},
		{"a semicolon", "add password router1 {pw-a} en;able", `';' in a bare word; brace the word`},
		{"an opening bracket", "add password r[!x]* {pw-a}", `'[' in a bare word; brace the word`},
		{"a closing bracket", "add password router1 pa]ss", `']' in a bare word; brace the word`},
	} {
		write(t, sub, tc.line+"\n", 0o600)
		for _, f := range []struct{ where, body, want string }{
			{"the file", "# lab\n" + tc.line + "\nadd password * {pw-default}\n", code + ": " + root + ":2: " + tc.message},
			{"an included file", "include sub.rc\n", code + ": " + root + ":1 include: " + sub + ":1: " + tc.message},
		} {
			write(t, root, f.body, 0o600)
			b := newBackend(t, d, root, credfile.ScopeUser, false)
			if r := resolve(b, "router1"); r.Outcome != credentials.Malformed || r.ErrorCode != code {
				t.Errorf("%s in %s: outcome %s, code %q, want %s (%s)", tc.name, f.where, r.Outcome, r.ErrorCode, code, r.Message)
			}
			_, err := b.flatten(context.Background(), root, true, nil, map[string]string{})
			if err == nil || err.Error() != f.want {
				t.Errorf("%s in %s: the reader's error is\n  %v\nwant\n  %s", tc.name, f.where, err, f.want)
			}
		}
	}
}

func TestRejectCode(t *testing.T) {
	if _, err := tokenize("add user * $USER"); err == nil {
		t.Fatal("expected substitution rejection")
	}
}

// wordCases are .cloginrc lines and what tclsh8.6 (8.6.17) reads from each
// through clogin's own add, the line evaluated on its own as clogin's
// source_password_file evaluates it: the pattern and the values clogin takes
// (an add password line's password and enable password joined, an add user
// or add userpassword value as the word reads), no record for a comment, or
// a refusal (the code and the text after the file and line) where tcl gives
// Tcl's reason for failing the line, or why karvi refuses a line Tcl reads.
// They are recorded here so that the test needs no Tcl.
var wordCases = []struct {
	line, pattern string
	values        []string
	code, message string
	tcl           string
}{
	// A bare word's backslash sequences are substituted.
	{line: `add userpassword router1 pa\tss`, pattern: "router1", values: []string{"pa\tss"}},
	{line: `add userpassword router1 a\ab\bc\fd\ne\rf\tg\vh`, pattern: "router1", values: []string{"a\ab\bc\fd\ne\rf\tg\vh"}},
	{line: `add userpassword router1 pa\\ss`, pattern: "router1", values: []string{`pa\ss`}},
	{line: `add userpassword router1 a\101b\477\08`, pattern: "router1", values: []string{"aAb'7\x008"}},
	{line: `add userpassword router1 a\x41b\x4g\xg`, pattern: "router1", values: []string{"aAb\x04gxg"}},
	{line: `add userpassword router1 a\` + `u00e9b\` + `ug`, pattern: "router1", values: []string{"a\xc3\xa9bug"}},
	{line: `add userpassword router1 a\U0001F600b\U41`, pattern: "router1", values: []string{"a\xef\xbf\xbdbA"}},
	{line: `add userpassword router1 a\` + `ud83d\` + `ude00b`, pattern: "router1", values: []string{"a\xf0\x9f\x98\x80b"}},
	{line: `add userpassword router1 pa\ss`, pattern: "router1", values: []string{"pass"}},
	{line: `add userpassword router1 pa\ ss`, pattern: "router1", values: []string{"pa ss"}},
	{line: `add userpassword router1 \{pass\}`, pattern: "router1", values: []string{"{pass}"}},
	// An escaped substitution character is a character.
	{line: `add userpassword router1 pa\$\[\]\;ss`, pattern: "router1", values: []string{"pa$[];ss"}},
	// A quote or a brace inside a bare word is a character.
	{line: `add userpassword router1 pa"ss"`, pattern: "router1", values: []string{`pa"ss"`}},
	{line: `add userpassword router1 pa{ss}`, pattern: "router1", values: []string{"pa{ss}"}},
	// Words are separated by Tcl's white space alone: space, tab, vertical
	// tab, form feed (a carriage return ends a line, as lineCases show); a
	// character whose UTF-8 holds the byte 0x85 or 0xa0 is part of its word.
	{line: "add password router1\vpa\fss\v", pattern: "router1", values: []string{"pa", "ss"}},
	{line: "add\tuserpassword\trouter1\tpass\f", pattern: "router1", values: []string{"pass"}},
	{line: "add userpassword router1 p\xc3\xa0ss", pattern: "router1", values: []string{"p\xc3\xa0ss"}},
	{line: "add userpassword router1 p\xc3\x85ss", pattern: "router1", values: []string{"p\xc3\x85ss"}},
	// A quoted word is one word: white space, ";", "]", and the braces are
	// characters in it, and its backslash sequences are substituted.
	{line: `add userpassword router1 "pa ss"`, pattern: "router1", values: []string{"pa ss"}},
	{line: `add userpassword router1 "a;b]c{d}e"`, pattern: "router1", values: []string{"a;b]c{d}e"}},
	{line: `add userpassword router1 "pa\tss\"q\$\101"`, pattern: "router1", values: []string{"pa\tss\"q$A"}},
	{line: `add userpassword router1 ""`, pattern: "router1", values: []string{""}},
	{line: `add user "router 1" bob`, pattern: "router 1", values: []string{"bob"}},
	{line: `add user router1 "svc user"`, pattern: "router1", values: []string{"svc user"}},
	// "#" begins a comment only as a line's first word.
	{line: `add password router1 pass #en`, pattern: "router1", values: []string{"pass", "#en"}},
	{line: `add userpassword router1 #x`, pattern: "router1", values: []string{"#x"}},
	{line: `add userpassword router1 "#x"`, pattern: "router1", values: []string{"#x"}},
	{line: `  # add password router1 pass`},
	{line: `#add password router1 pass`},
	{line: "\v# add password router1 pass"},
	{line: `add user router1 bob # the admin`, code: "cloginrc_malformed", message: "add user requires exactly one value", tcl: `clogin takes "bob#theadmin", the four words joined`},
	// An add password line's password and enable password are joined as
	// clogin joins them, after the words are read; a braced word keeps its
	// backslashes until the join.
	{line: `add password router1 "pa ss" "a b"`, pattern: "router1", values: []string{"pass", "ab"}},
	{line: `add password router1 pa\tss pa\x41ss`, pattern: "router1", values: []string{"pass", "paAss"}},
	{line: `add password router1 "{a b} c" {x\ty}`, pattern: "router1", values: []string{"a bc", "x\ty"}},
	{line: `add password router1 {pa\tss} "pa\\tss"`, pattern: "router1", values: []string{"pa\tss", "pa\tss"}},
	{line: `add password router1 "pa]ss"`, pattern: "router1", values: []string{"pa]ss"}},
	// Text right after a closing brace or quote fails the line.
	{line: `add password router1 {pa}ss`, code: "cloginrc_malformed", message: "text after a closing brace", tcl: "extra characters after close-brace"},
	{line: `add password router1 "pa"ss`, code: "cloginrc_malformed", message: "text after a closing quote", tcl: "extra characters after close-quote"},
	{line: `add password router1 "a"{b}`, code: "cloginrc_malformed", message: "text after a closing quote", tcl: "extra characters after close-quote"},
	{line: `add password router1 pass "en"x`, code: "cloginrc_malformed", message: "text after a closing quote", tcl: "extra characters after close-quote"},
	// A ";" right after one is refused as chaining, where Tcl ends the command.
	{line: `add password router1 {pa};`, code: "cloginrc_tcl_substitution_forbidden", message: "';' after a closing brace", tcl: `Tcl reads the password "pa", the ";" ending the command`},
	{line: `add password router1 "pa";`, code: "cloginrc_tcl_substitution_forbidden", message: "';' after a closing quote", tcl: `Tcl reads the password "pa", the ";" ending the command`},
	// A quote or a brace left open on its line fails the line.
	{line: `add password router1 "pass`, code: "cloginrc_malformed", message: "unterminated quote", tcl: `missing "`},
	{line: `add password router1 {pass`, code: "cloginrc_malformed", message: "unterminated brace", tcl: "missing close-brace"},
	// "$" and "[" are refused in a quoted word, and "$", "[", "]", and ";"
	// in a bare word, where Tcl would substitute or end the command.
	{line: `add password router1 "pa$ss"`, code: "cloginrc_tcl_substitution_forbidden", message: `'$' in a quoted word; brace the word`, tcl: `can't read "ss": no such variable`},
	{line: `add password router1 "pa[ss"`, code: "cloginrc_tcl_substitution_forbidden", message: `'[' in a quoted word; brace the word`, tcl: "missing close-bracket"},
	{line: `add password router1 pa$ss`, code: "cloginrc_tcl_substitution_forbidden", message: `'$' in a bare word; brace the word`, tcl: `can't read "ss": no such variable`},
}

// TestWordsReadAsTclsh: each line of wordCases is read as tclsh8.6 reads it
// through clogin's add, or refused with the code and message recorded.
func TestWordsReadAsTclsh(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	for _, tc := range wordCases {
		write(t, p, tc.line+"\n", 0o600)
		records, failed := newBackend(t, d, p, credfile.ScopeUser, false).load(context.Background())
		if tc.code != "" {
			if failed == nil {
				t.Errorf("%q: read as %+v; want %s, %s (Tcl: %s)", tc.line, records, tc.code, tc.message, tc.tcl)
			} else if failed.ErrorCode != tc.code || failed.Message != p+":1: "+tc.message {
				t.Errorf("%q: %s: %s\nwant %s: %s:1: %s (Tcl: %s)", tc.line, failed.ErrorCode, failed.Message, tc.code, p, tc.message, tc.tcl)
			}
			continue
		}
		if failed != nil {
			t.Errorf("%q: %s: %s; Tcl reads %q %q", tc.line, failed.ErrorCode, failed.Message, tc.pattern, tc.values)
			continue
		}
		if tc.values == nil {
			if len(records) != 0 {
				t.Errorf("%q: read as %+v; Tcl reads a comment", tc.line, records)
			}
			continue
		}
		if len(records) != 1 || records[0].Pattern != tc.pattern || !slices.Equal(records[0].Values, tc.values) {
			t.Errorf("%q: read as %+v; Tcl reads %q %q", tc.line, records, tc.pattern, tc.values)
		}
	}
}

// lineCases are .cloginrc files, byte for byte, and what clogin reads from
// each in tclsh8.6 (8.6.17): its source_password_file takes each line with
// Tcl's gets (a newline, a carriage return and a newline, or a carriage
// return alone ends a line, and the gets calls number the lines) and
// evaluates the line on its own through clogin's add and include. A case
// gives the records karvi reads (the line, the directive, the pattern, and
// the values clogin takes), or the refusal (the code, the line, and the text
// after the file and line) with tcl saying how clogin fails the line, or why
// karvi refuses a line clogin reads. sub.rc, beside the file, holds "add user
// switch1 alice". They are recorded here so that the test needs no Tcl.
var lineCases = []struct {
	name, file   string
	records      []record
	code         string
	line         int
	message, tcl string
}{
	// A newline, a carriage return and a newline, or a carriage return alone
	// ends a line.
	{name: "newlines", file: "add user router1 bob\nadd password router1 pw-a en-a\n",
		records: []record{{Line: 1, Directive: "user", Pattern: "router1", Values: []string{"bob"}}, {Line: 2, Directive: "password", Pattern: "router1", Values: []string{"pw-a", "en-a"}}}},
	{name: "carriage returns and newlines", file: "add user router1 bob\r\nadd password router1 pw-a en-a\r\nadd userpassword switch2 pw-b\r\nadd password switch3 pw-c\r\n",
		records: []record{{Line: 1, Directive: "user", Pattern: "router1", Values: []string{"bob"}}, {Line: 2, Directive: "password", Pattern: "router1", Values: []string{"pw-a", "en-a"}}, {Line: 3, Directive: "userpassword", Pattern: "switch2", Values: []string{"pw-b"}}, {Line: 4, Directive: "password", Pattern: "switch3", Values: []string{"pw-c"}}}},
	{name: "a carriage return alone mid-file", file: "add user router1 bob\radd password router1 pw-a en-a\n",
		records: []record{{Line: 1, Directive: "user", Pattern: "router1", Values: []string{"bob"}}, {Line: 2, Directive: "password", Pattern: "router1", Values: []string{"pw-a", "en-a"}}}},
	{name: "carriage returns alone, one ending the file", file: "add user router1 bob\radd password router1 pw-a\r",
		records: []record{{Line: 1, Directive: "user", Pattern: "router1", Values: []string{"bob"}}, {Line: 2, Directive: "password", Pattern: "router1", Values: []string{"pw-a"}}}},
	{name: "a carriage return alone before a carriage return and a newline", file: "add user router1 bob\r\r\nadd password router1 pw-a\n",
		records: []record{{Line: 1, Directive: "user", Pattern: "router1", Values: []string{"bob"}}, {Line: 3, Directive: "password", Pattern: "router1", Values: []string{"pw-a"}}}},
	{name: "a newline before a carriage return alone", file: "add user router1 bob\n\radd password router1 pw-a\n",
		records: []record{{Line: 1, Directive: "user", Pattern: "router1", Values: []string{"bob"}}, {Line: 3, Directive: "password", Pattern: "router1", Values: []string{"pw-a"}}}},
	{name: "a carriage return alone ending a comment", file: "# lab\radd password router1 pw-a\n",
		records: []record{{Line: 2, Directive: "password", Pattern: "router1", Values: []string{"pw-a"}}}},
	{name: "a carriage return alone inside a word", file: "add password router1 pw\r-a\n",
		code: "cloginrc_malformed", line: 2, message: "unsupported statement", tcl: `line 1 the password "pw"; line 2 invalid command name "-a"`},
	{name: "a refusal after carriage returns alone", file: "add user router1 bob\radd password router1 pw-a\rADD password router1 pw-b\n",
		code: "cloginrc_malformed", line: 3, message: "unsupported statement", tcl: `invalid command name "ADD"`},
	{name: "a refusal after a carriage return alone and a carriage return and a newline", file: "add user router1 bob\r\r\nadd password router1 {pw-a\n",
		code: "cloginrc_malformed", line: 3, message: "unterminated brace", tcl: "missing close-brace"},
	// A line has no continuation: a backslash that ends it is read as Tcl
	// reads the line alone.
	{name: "a backslash ending a bare word", file: "add userpassword router1 pass\\\n",
		records: []record{{Line: 1, Directive: "userpassword", Pattern: "router1", Values: []string{`pass\`}}}},
	{name: "a backslash ending a bare enable password", file: "add password router1 pass en\\\n",
		records: []record{{Line: 1, Directive: "password", Pattern: "router1", Values: []string{"pass", `en\`}}}},
	{name: "a backslash alone as the enable password", file: "add password router1 pass \\\n",
		records: []record{{Line: 1, Directive: "password", Pattern: "router1", Values: []string{"pass", `\`}}}},
	{name: "three backslashes ending a bare word", file: "add userpassword router1 pa\\\\\\\n",
		records: []record{{Line: 1, Directive: "userpassword", Pattern: "router1", Values: []string{`pa\\`}}}},
	{name: "three backslashes ending a bare password, joined", file: "add password router1 pa\\\\\\\n",
		records: []record{{Line: 1, Directive: "password", Pattern: "router1", Values: []string{`pa\`}}}},
	{name: "a backslash ending an add user value", file: "add user router1 bob\\\n",
		records: []record{{Line: 1, Directive: "user", Pattern: "router1", Values: []string{`bob\`}}}},
	{name: "a backslash before a carriage return and a newline", file: "add userpassword router1 pass\\\r\n",
		records: []record{{Line: 1, Directive: "userpassword", Pattern: "router1", Values: []string{`pass\`}}}},
	{name: "a backslash before a carriage return alone", file: "add userpassword router1 pass\\\radd user router1 bob\n",
		records: []record{{Line: 1, Directive: "userpassword", Pattern: "router1", Values: []string{`pass\`}}, {Line: 2, Directive: "user", Pattern: "router1", Values: []string{"bob"}}}},
	{name: "a backslash ending a comment", file: "# a comment ending in a backslash \\\nadd password router1 pw-a\n",
		records: []record{{Line: 2, Directive: "password", Pattern: "router1", Values: []string{"pw-a"}}}},
	{name: "a backslash in a quoted word left open", file: "add password router1 \"pass\\\n",
		code: "cloginrc_malformed", line: 1, message: "unterminated quote", tcl: `missing "`},
	{name: "a backslash in a braced word left open", file: "add password router1 {pass\\\n",
		code: "cloginrc_malformed", line: 1, message: "unterminated brace", tcl: "missing close-brace"},
	{name: "a backslash after a closing brace", file: "add password router1 {pa}\\\n",
		code: "cloginrc_malformed", line: 1, message: "text after a closing brace", tcl: "extra characters after close-brace"},
	{name: "a backslash after a closing quote", file: "add password router1 \"pa\"\\\n",
		code: "cloginrc_malformed", line: 1, message: "text after a closing quote", tcl: "extra characters after close-quote"},
	// add and include, the line's first word, and the directive are matched
	// as Tcl matches a command's name and clogin's find a directive: case
	// counts.
	{name: "ADD", file: "ADD password router1 pw-a\n",
		code: "cloginrc_malformed", line: 1, message: "unsupported statement", tcl: `invalid command name "ADD"`},
	{name: "Add", file: "Add user router1 bob\n",
		code: "cloginrc_malformed", line: 1, message: "unsupported statement", tcl: `invalid command name "Add"`},
	{name: "INCLUDE", file: "INCLUDE sub.rc\n",
		code: "cloginrc_malformed", line: 1, message: "unsupported statement", tcl: `invalid command name "INCLUDE"`},
	{name: "Include", file: "Include sub.rc\n",
		code: "cloginrc_malformed", line: 1, message: "unsupported statement", tcl: `invalid command name "Include"`},
	{name: "include", file: "include sub.rc\n",
		records: []record{{Line: 1, Directive: "user", Pattern: "switch1", Values: []string{"alice"}}}},
	{name: "add PASSWORD", file: "add PASSWORD router1 pw-a\n",
		code: "cloginrc_unknown_directive", line: 1, message: "PASSWORD", tcl: "clogin's add keeps the line under PASSWORD, which its find password never reads"},
	{name: "add Password", file: "add Password router1 pw-a\n",
		code: "cloginrc_unknown_directive", line: 1, message: "Password", tcl: "clogin's add keeps the line under Password, which its find password never reads"},
	{name: "add USER", file: "add USER router1 bob\n",
		code: "cloginrc_unknown_directive", line: 1, message: "USER", tcl: "clogin's add keeps the line under USER, which its find user never reads"},
	{name: "add UserPassword", file: "add UserPassword router1 pw-a\n",
		code: "cloginrc_unknown_directive", line: 1, message: "UserPassword", tcl: "clogin's add keeps the line under UserPassword, which its find userpassword never reads"},
	{name: "add METHOD", file: "add METHOD router1 ssh\n",
		code: "cloginrc_unknown_directive", line: 1, message: "METHOD", tcl: "clogin's add keeps the line under METHOD, which its find method never reads"},
	{name: "add method", file: "add method router1 ssh\n"},
}

// TestLinesReadAsClogin: each file of lineCases is read as clogin reads it,
// line by line, or refused with the code, line, and message recorded.
func TestLinesReadAsClogin(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	write(t, filepath.Join(d, "sub.rc"), "add user switch1 alice\n", 0o600)
	for _, tc := range lineCases {
		write(t, p, tc.file, 0o600)
		records, failed := newBackend(t, d, p, credfile.ScopeUser, false).load(context.Background())
		if tc.code != "" {
			want := p + ":" + strconv.Itoa(tc.line) + ": " + tc.message
			if failed == nil {
				t.Errorf("%s: read as %+v; want %s, %s (clogin: %s)", tc.name, records, tc.code, want, tc.tcl)
			} else if failed.ErrorCode != tc.code || failed.Message != want {
				t.Errorf("%s: %s: %s\nwant %s: %s (clogin: %s)", tc.name, failed.ErrorCode, failed.Message, tc.code, want, tc.tcl)
			}
			continue
		}
		if failed != nil {
			t.Errorf("%s: %s: %s; clogin reads %+v", tc.name, failed.ErrorCode, failed.Message, tc.records)
			continue
		}
		if !slices.EqualFunc(records, tc.records, func(a, b record) bool {
			return a.Line == b.Line && a.Directive == b.Directive && a.Pattern == b.Pattern && slices.Equal(a.Values, b.Values)
		}) {
			t.Errorf("%s: read as %+v; clogin reads %+v", tc.name, records, tc.records)
		}
	}
}

// TestLongLineIsRefusedNamingTheFileAndLine: a line of 1 MiB or more (the
// reader's bound, its line end aside) fails the load with cloginrc_malformed,
// the message naming the file and the line (an included file's after the
// include's); a line one byte shorter is read, whichever line end ends it.
func TestLongLineIsRefusedNamingTheFileAndLine(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "cloginrc")
	sub := filepath.Join(d, "sub.rc")
	const lead = "add userpassword router1 "
	fits := lead + strings.Repeat("a", 1<<20-1-len(lead))
	long := fits + "a"
	for _, end := range []string{"\n", "\r\n", "\r"} {
		write(t, root, "# lab\n"+fits+end+"add password * {pw-default}\n", 0o600)
		if records, failed := newBackend(t, d, root, credfile.ScopeUser, false).load(context.Background()); failed != nil || len(records) != 2 || len(records[0].Values[0]) != len(fits)-len(lead) || records[1].Line != 3 {
			t.Errorf("a line of 1 MiB less one byte, ended by %q: %d records, %v", end, len(records), failed)
		}
	}
	write(t, sub, "# lab\n"+long+"\n", 0o600)
	for _, f := range []struct{ where, body, want string }{
		{"the file", "# lab\n" + long + "\nadd password * {pw-default}\n", root + ":2: a line of 1 MiB or more"},
		{"the file, a carriage return ending the line", "# lab\n" + long + "\radd password * {pw-default}\n", root + ":2: a line of 1 MiB or more"},
		{"the file's last line", "# lab\n" + long, root + ":2: a line of 1 MiB or more"},
		{"an included file", "include sub.rc\n", root + ":1 include: " + sub + ":2: a line of 1 MiB or more"},
	} {
		write(t, root, f.body, 0o600)
		_, failed := newBackend(t, d, root, credfile.ScopeUser, false).load(context.Background())
		if failed == nil {
			t.Errorf("%s: read; want cloginrc_malformed, %s", f.where, f.want)
		} else if failed.Outcome != credentials.Malformed || failed.ErrorCode != "cloginrc_malformed" || failed.Message != f.want {
			t.Errorf("%s: %s %s: %s\nwant malformed cloginrc_malformed: %s", f.where, failed.Outcome, failed.ErrorCode, failed.Message, f.want)
		}
	}
}

// TestPasswordAndEnableJoinedAsClogin: an add password line's password and
// enable password are what clogin sends, [join [lindex $pswd n] ""]: each
// value read again as a Tcl list, its words joined with nothing between. An
// add user or add userpassword value is taken as written. The answers are
// clogin's own add and find in tclsh8.6 (8.6.17) over the same lines.
func TestPasswordAndEnableJoinedAsClogin(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	write(t, p, `add user router1 {svc user}
add user router2 {svc\user}
add password router1 {pass one} {en\}a\{b\\}
add password router2 {pa\ss} {a  b}
add password router3 {"a b"} {{}}
add password router4 {pa"ss} pa{ss
add password router5 {pass\ one} {{pass one}}
add password router6 {a\x41b} {a\101b}
add password router7 {a\tb} {a\nb}
add password router8 {"a\"b"} {a"b"c}
add password router9 {} { }
add password router10 {a\\b} {\{a}
add password router11 {{a b} c} {a\x414b}
add password router12 plain
add password router13 {a\477} {a\08}
add password router14 {\"pass}
add userpassword switch1 {up one}
add userpassword switch2 {up\ss}
`, 0o600)
	b := newBackend(t, d, p, credfile.ScopeUser, false)
	for _, tc := range []struct{ device, user, password, enable string }{
		{"router1", "svc user", "passone", `en}a{b\`},
		{"router2", `svc\user`, "pass", "ab"},
		{"router3", "me", "a b", ""},
		{"router4", "me", `pa"ss`, "pa{ss"},
		{"router5", "me", "pass one", "pass one"},
		{"router6", "me", "aAb", "aAb"},
		{"router7", "me", "a\tb", "a\nb"},
		{"router8", "me", `a"b`, `a"b"c`},
		{"router9", "me", "", ""},
		{"router10", "me", `a\b`, "{a"},
		{"router11", "me", "a bc", "aA4b"},
		{"router12", "me", "plain", ""},
		{"router13", "me", "a'7", "a\x008"},
		{"router14", "me", `"pass`, ""},
		{"switch1", "me", "up one", ""},
		{"switch2", "me", `up\ss`, ""},
	} {
		r := resolve(b, tc.device)
		if r.Outcome != credentials.Success {
			t.Errorf("%s: outcome %s, code %q (%s)", tc.device, r.Outcome, r.ErrorCode, r.Message)
			continue
		}
		pw, en := secret(t, r)
		if u := username(t, r); u != tc.user || pw != tc.password || en != tc.enable {
			t.Errorf("%s: user %q, password %q, enable %q; clogin sends %q, %q, %q", tc.device, u, pw, en, tc.user, tc.password, tc.enable)
		}
	}
}

// TestPasswordNotATclListIsRefused: a password or enable password that is
// not a Tcl list, where clogin's join stops with a Tcl error (an unmatched
// open quote or brace, or text after an element's closing brace or quote),
// fails the load with cloginrc_malformed, the message naming the file and
// the line (an included file's after the include's) and never the value.
func TestPasswordNotATclListIsRefused(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "cloginrc")
	sub := filepath.Join(d, "sub.rc")
	for _, tc := range []struct{ name, line, message string }{
		{"an unmatched open quote", `add password router1 {"pw-a}`, "the password is not a Tcl list: unmatched open quote"},
		{"an unmatched open quote, escaped in a bare word", `add password router1 \"pw-a`, "the password is not a Tcl list: unmatched open quote"},
		{"an unmatched open brace", `add password router1 \{pw-a`, "the password is not a Tcl list: unmatched open brace"},
		{"text after a closing brace", `add password router1 {{pw}-a}`, "the password is not a Tcl list: text after a closing brace"},
		{"text after a closing quote", `add password router1 {"pw"-a}`, "the password is not a Tcl list: text after a closing quote"},
		{"the enable password", `add password router1 {pw-a} {"en-a}`, "the enable password is not a Tcl list: unmatched open quote"},
	} {
		write(t, sub, tc.line+"\n", 0o600)
		for _, f := range []struct{ where, body, want string }{
			{"the file", "# lab\n" + tc.line + "\nadd password * {pw-default}\n", root + ":2: " + tc.message},
			{"an included file", "include sub.rc\n", root + ":1 include: " + sub + ":1: " + tc.message},
		} {
			write(t, root, f.body, 0o600)
			r := resolve(newBackend(t, d, root, credfile.ScopeUser, false), "router1")
			if r.Outcome != credentials.Malformed || r.ErrorCode != "cloginrc_malformed" || r.Message != f.want {
				t.Errorf("%s in %s: outcome %s, code %q, message\n  %s\nwant cloginrc_malformed,\n  %s", tc.name, f.where, r.Outcome, r.ErrorCode, r.Message, f.want)
			}
		}
	}
}

// TestStatementRefusalsNameTheFileAndLine: an unsupported statement, a
// directive with the wrong number of values, and an unknown directive fail
// the load naming the file and the line (an included file's after the
// include's), as the reader's other refusals do. The reader's error holds a
// code once, leading; the backend's result holds it in its code alone.
func TestStatementRefusalsNameTheFileAndLine(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "cloginrc")
	sub := filepath.Join(d, "sub.rc")
	for _, tc := range []struct{ name, line, code, message string }{
		{"an unsupported statement", "set password router1 {pw-a}", "", "unsupported statement"},
		{"a statement too short", "add password router1", "", "unsupported statement"},
		{"two users", "add user router1 {svc.a} {svc.b}", "", "add user requires exactly one value"},
		{"two user passwords", "add userpassword router1 {pw-a} {pw-b}", "", "add userpassword requires exactly one value"},
		{"three passwords", "add password router1 {pw-a} {pw-b} {pw-c}", "", "add password requires one or two values"},
		{"an unknown directive", "add frobnicate router1 {x}", "cloginrc_unknown_directive", "frobnicate"},
	} {
		write(t, sub, tc.line+"\n", 0o600)
		code, lead := tc.code, tc.code+": "
		if code == "" {
			code, lead = "cloginrc_malformed", ""
		}
		for _, f := range []struct{ where, body, at string }{
			{"the file", "# lab\n" + tc.line + "\nadd password * {pw-default}\n", root + ":2: "},
			{"an included file", "include sub.rc\n", root + ":1 include: " + sub + ":1: "},
		} {
			write(t, root, f.body, 0o600)
			b := newBackend(t, d, root, credfile.ScopeUser, false)
			if r := resolve(b, "router1"); r.Outcome != credentials.Malformed || r.ErrorCode != code || r.Message != f.at+tc.message {
				t.Errorf("%s in %s: outcome %s, code %q, message\n  %s\nwant %s,\n  %s", tc.name, f.where, r.Outcome, r.ErrorCode, r.Message, code, f.at+tc.message)
			}
			_, err := b.flatten(context.Background(), root, true, nil, map[string]string{})
			if want := lead + f.at + tc.message; err == nil || err.Error() != want {
				t.Errorf("%s in %s: the reader's error is\n  %v\nwant\n  %s", tc.name, f.where, err, want)
			}
		}
	}
}

// TestResultMessageHoldsNoCode: a failed load's result carries its code in
// ErrorCode alone; the message is the text after it, so the code is written
// once wherever the result is shown.
func TestResultMessageHoldsNoCode(t *testing.T) {
	d := t.TempDir()
	for _, tc := range []struct{ name, body string }{
		{"a bare word's substitution character", "add password router1 pa$$w0rd\n"},
		{"an unknown directive", "add frobnicate router1 {x}\n"},
		{"an include cycle", "include cloginrc\n"},
	} {
		p := filepath.Join(d, "cloginrc")
		write(t, p, tc.body, 0o600)
		r := resolve(newBackend(t, d, p, credfile.ScopeUser, false), "router1")
		if r.Outcome != credentials.Malformed || r.ErrorCode == "" || strings.Contains(r.Message, r.ErrorCode) {
			t.Errorf("%s: outcome %s, code %q, message %q", tc.name, r.Outcome, r.ErrorCode, r.Message)
		}
	}
	wide := filepath.Join(d, "wide.rc")
	write(t, wide, content, 0o644)
	if r := resolve(newBackend(t, d, wide, credfile.ScopeUser, false), "router1"); r.ErrorCode != "credential_file_mode_unsafe" || strings.Contains(r.Message, r.ErrorCode) {
		t.Errorf("an unsafe mode: code %q, message %q", r.ErrorCode, r.Message)
	}
}

// joinCases are values as a Tcl word holds them and what tclsh8.6
// (8.6.17) gives for [join $value ""]: the joined bytes, or the error's
// reason. They are recorded here so that the test needs no Tcl.
var joinCases = []struct{ value, want, err string }{
	{`pass one`, `passone`, ""},
	{`pa\ss`, `pass`, ""},
	{`a  b`, `ab`, ""},
	{`"a b"`, `a b`, ""},
	{`{}`, ``, ""},
	{`pa"ss`, `pa"ss`, ""},
	{`pa{ss`, `pa{ss`, ""},
	{`"pass`, "", "unmatched open quote"},
	{`{pass`, "", "unmatched open brace"},
	{`{ }`, ` `, ""},
	{` `, ``, ""},
	{``, ``, ""},
	{`""`, ``, ""},
	{`"" ""`, ``, ""},
	{`pass\ one`, `pass one`, ""},
	{`{pass one}`, `pass one`, ""},
	{`en\}a\{b\\`, `en}a{b\`, ""},
	{`{a b} c`, `a bc`, ""},
	{`{a{b}c}`, `a{b}c`, ""},
	{`{a\}b}`, `a\}b`, ""},
	{`"a{b"`, `a{b`, ""},
	{`"a\"b"`, `a"b`, ""},
	{`a"b"c`, `a"b"c`, ""},
	{`}a`, `}a`, ""},
	{"a\tb\nc\rd\ve\ff", "abcdef", ""},
	{"a\xc2\xa0b", "a\xc2\xa0b", ""},
	{`a\x41b`, `aAb`, ""},
	{`a\x414b`, `aA4b`, ""},
	{`a\x4g`, "a\x04g", ""},
	{`a\xg`, `axg`, ""},
	{`a\` + `u0041b`, `aAb`, ""},
	{`a\` + `u12345`, "a\xe1\x88\xb45", ""},
	{`a\` + `ug`, `aug`, ""},
	{`a\` + `ud83d\` + `ude00b`, "a\xf0\x9f\x98\x80b", ""},
	{`a\` + `ud83d`, "a\xed\xa0\xbd", ""},
	{`a\U0001F600b`, "a\xef\xbf\xbdb", ""},
	{`a\U110000`, "a\xef\xbf\xbd0", ""},
	{`a\U0000D800b`, "a\xed\xa0\x80b", ""},
	{`a\Ug`, `aUg`, ""},
	{`a\101b`, `aAb`, ""},
	{`a\377`, "a\xc3\xbf", ""},
	{`a\477`, `a'7`, ""},
	{`a\777`, `a?7`, ""},
	{`a\08`, "a\x008", ""},
	{`a\ab\bc\fd\ne\rf\tg\vh`, "a\ab\bc\fd\ne\rf\tg\vh", ""},
	{"a\\\n   b", `a b`, ""},
	{"\"a\\\n  b\"", `a b`, ""},
	{"{a\\\n  b}", "a\\\n  b", ""},
	{"a\\\r\nb", "a\rb", ""},
	{"a\\\xc3\xa9b", "a\xc3\xa9b", ""},
	{"a\\\xf0\x9f\x98\x80b", "a\xef\xbf\xbdb", ""},
	{"a\xf0\x9f\x98\x80b", "a\xf0\x9f\x98\x80b", ""},
	{`a\`, `a\`, ""},
	{`\`, `\`, ""},
	{`{a}b`, "", "text after a closing brace"},
	{`{a}}`, "", "text after a closing brace"},
	{`{}{}`, "", "text after a closing brace"},
	{`"a"b`, "", "text after a closing quote"},
	{`"a b""`, "", "text after a closing quote"},
	{`{{a}`, "", "unmatched open brace"},
	{`{a\}`, "", "unmatched open brace"},
	{`"a\"`, "", "unmatched open quote"},
}

// TestJoinListAsTclsh: joinList reads a value as Tcl 8.6 reads a list and
// joins its elements with nothing between, as tclsh8.6's [join $value ""]
// does, and fails where Tcl's list reading fails.
func TestJoinListAsTclsh(t *testing.T) {
	for _, tc := range joinCases {
		got, err := joinList(tc.value)
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Errorf("joinList(%q) = %q, %v; Tcl fails: %s", tc.value, got, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("joinList(%q) = %q, %v; Tcl gives %q", tc.value, got, err, tc.want)
		}
	}
}
