package cloginrc

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
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

// TestBracedValueKeepsBackslashes: inside braces a backslash and the byte
// after it are kept, as Tcl keeps them (no backslash substitution inside
// braces); an escaped brace still does not count toward the nesting. A bare
// word drops the backslash, as Tcl's substitution does.
func TestBracedValueKeepsBackslashes(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cloginrc")
	write(t, p, "add password router-* {pa\\ss} {en\\}a\\{b\\\\}\nadd password switch-* pa\\ss\n", 0o600)
	b := newBackend(t, d, p, credfile.ScopeUser, false)
	if pw, en := secret(t, resolve(b, "router-1")); pw != `pa\ss` || en != `en\}a\{b\\` {
		t.Errorf("braced: password %q, enable %q, want %q and %q", pw, en, `pa\ss`, `en\}a\{b\\`)
	}
	if pw, _ := secret(t, resolve(b, "switch-1")); pw != "pass" {
		t.Errorf("bare: password %q, want %q", pw, "pass")
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

func TestRejectCode(t *testing.T) {
	if _, err := tokenize("add user * $USER"); err == nil {
		t.Fatal("expected substitution rejection")
	}
}
