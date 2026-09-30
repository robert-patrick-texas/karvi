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

func TestRejectCode(t *testing.T) {
	if _, err := tokenize("add user * $USER"); err == nil {
		t.Fatal("expected substitution rejection")
	}
}
