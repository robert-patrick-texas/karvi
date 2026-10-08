package hostkey

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

func TestParseModes(t *testing.T) {
	cases := map[string]Mode{"": AcceptNew, "accept-new": AcceptNew, "ACCEPT-NEW": AcceptNew, "insecure": Insecure, "secure": Secure}
	for input, want := range cases {
		got, err := Parse(input)
		if err != nil || got != want {
			t.Fatalf("Parse(%q)=%q,%v want %q", input, got, err, want)
		}
	}
	for _, removed := range []string{"auto", "default", "ask"} {
		if _, err := Parse(removed); err == nil {
			t.Fatalf("Parse(%q) accepted a removed or invalid mode", removed)
		}
	}
}

func TestResolveAcceptNewCreatesTheStoreInTheBase(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(t.TempDir(), "users", "op")
	if err := os.MkdirAll(base, 0o750); err != nil {
		t.Fatal(err)
	}
	policy, err := Resolve("accept-new", "auto", home, base)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "known_hosts")
	if policy.Mode != AcceptNew || policy.KnownHostsFile != want {
		t.Fatalf("policy=%+v want path=%s", policy, want)
	}
	// The home is not the store's under "auto" when the base is elsewhere.
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("the home holds %v", entries)
	}
	fileInfo, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file mode=%04o", fileInfo.Mode().Perm())
	}
}

// StorePath names the store without making anything: "auto" is the base's
// known_hosts, an explicit path is the home's for ~ and the working
// directory's for a relative path, ~user refused.
func TestStorePath(t *testing.T) {
	home, base := "/home/op", "/opt/karvi/users/op"
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ configured, want string }{
		{"auto", "/opt/karvi/users/op/known_hosts"},
		{"", "/opt/karvi/users/op/known_hosts"},
		{"AUTO", "/opt/karvi/users/op/known_hosts"},
		{"~/kh", "/home/op/kh"},
		{"kh", filepath.Join(wd, "kh")},
		{"/srv/kh/../known_hosts", "/srv/known_hosts"},
	} {
		got, err := StorePath(c.configured, home, base)
		if err != nil || got != c.want {
			t.Fatalf("StorePath(%q) = %q, %v; want %q", c.configured, got, err, c.want)
		}
	}
	var hostErr *Error
	if _, err := StorePath("auto", home, ""); !errors.As(err, &hostErr) || hostErr.Code != "host_key_trust_store_unavailable" {
		t.Fatalf("auto without a base: %v", err)
	}
	if got, err := StorePath("/srv/kh", home, ""); err != nil || got != "/srv/kh" {
		t.Fatalf("an explicit path needs no base: %q %v", got, err)
	}
	if _, err := StorePath("~other/kh", home, base); errorcodes.Of(err) != "path_other_user_home_unsupported" {
		t.Fatalf("~other: %v", err)
	}
	if _, err := StorePath("~/kh", "", base); !errors.As(err, &hostErr) || hostErr.Code != "host_key_home_unavailable" {
		t.Fatalf("~ without a home: %v", err)
	}
}

func TestResolveRejectsRemovedModes(t *testing.T) {
	for _, removed := range []string{"auto", "default"} {
		_, err := Resolve(removed, "auto", t.TempDir(), t.TempDir())
		var hostErr *Error
		if !errors.As(err, &hostErr) || hostErr.Code != "host_key_policy_invalid" {
			t.Fatalf("Resolve(%q) expected host_key_policy_invalid, got %v", removed, err)
		}
	}
}

func TestResolveSecureRequiresEnrollment(t *testing.T) {
	_, err := Resolve("secure", "auto", t.TempDir(), t.TempDir())
	var hostErr *Error
	if !errors.As(err, &hostErr) || hostErr.Code != "host_key_not_enrolled" {
		t.Fatalf("expected host_key_not_enrolled, got %v", err)
	}
}

func TestOpenSSHSettings(t *testing.T) {
	p := Policy{Mode: AcceptNew, KnownHostsFile: "/tmp/karvi-known"}
	strict, user, global := p.OpenSSHSettings()
	if strict != "accept-new" || user != "/tmp/karvi-known" || global != "/dev/null" {
		t.Fatalf("accept-new=%q,%q,%q", strict, user, global)
	}
	p.Mode = Secure
	strict, _, _ = p.OpenSSHSettings()
	if strict != "yes" {
		t.Fatalf("secure strict=%q", strict)
	}
	p.Mode = Insecure
	strict, user, global = p.OpenSSHSettings()
	if strict != "no" || user != "/dev/null" || global != "/dev/null" {
		t.Fatalf("insecure=%q,%q,%q", strict, user, global)
	}
}

func TestExistingTrustFileRequiresPrivateParentDirectory(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "share", "karvi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(known, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Group or others able to write the directory is refused; 0750, the
	// mode the path resolver gives the operator's root, is accepted.
	for _, mode := range []os.FileMode{0o770, 0o707, 0o777, 0o720} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		_, err := Resolve("secure", known, home, "")
		want := fmt.Sprintf("directory mode is %04o; group or others must not write it", mode)
		if err == nil || !strings.Contains(err.Error(), "host_key_directory_permission") || !strings.Contains(err.Error(), want) {
			t.Fatalf("mode %04o: expected private-directory permission failure, got %v", mode, err)
		}
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("secure", known, home, ""); err != nil {
		t.Fatalf("mode 0750: %v", err)
	}
}

// storeOfMode makes a private directory holding a known_hosts file of mode,
// either where "auto" looks or at an explicit path, and returns the
// configured value and the file.
func storeOfMode(t *testing.T, home string, auto bool, mode os.FileMode) (configured, known string) {
	t.Helper()
	dir := filepath.Join(home, "store")
	configured = filepath.Join(dir, "known_hosts")
	if auto {
		dir = filepath.Join(home, ".local", "share", "karvi")
		configured = "auto"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	known = filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(known, []byte("r1 ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(known, mode); err != nil {
		t.Fatal(err)
	}
	return configured, known
}

// An existing store whose mode is not 0600: accept-new
// and secure refuse it, insecure continues, the same on an explicit path as on
// "auto", and no policy changes the file.
func TestExistingStoreModeIsRefusedOrIgnoredNeverRepaired(t *testing.T) {
	for _, auto := range []bool{false, true} {
		for _, mode := range []Mode{AcceptNew, Secure, Insecure} {
			home := t.TempDir()
			configured, known := storeOfMode(t, home, auto, 0o644)
			policy, err := Resolve(string(mode), configured, home, filepath.Dir(known))
			if mode == Insecure {
				if err != nil || policy.KnownHostsFile != known {
					t.Fatalf("auto=%v insecure: policy=%+v err=%v; want the store accepted whatever its mode", auto, policy, err)
				}
			} else {
				var hostErr *Error
				if !errors.As(err, &hostErr) || hostErr.Code != "host_key_trust_store_permission" {
					t.Fatalf("auto=%v %s: expected host_key_trust_store_permission, got %v", auto, mode, err)
				}
			}
			info, statErr := os.Stat(known)
			if statErr != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("auto=%v %s: the store's mode afterwards is %v (%v); want 0644 untouched", auto, mode, info.Mode().Perm(), statErr)
			}
		}
	}
}

// A 0600 store that exists is accepted by accept-new with its content kept.
func TestAcceptNewKeepsAnExistingPrivateStore(t *testing.T) {
	home := t.TempDir()
	configured, known := storeOfMode(t, home, false, 0o600)
	if _, err := Resolve("accept-new", configured, home, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(known); err != nil || string(data) != "r1 ssh-ed25519 AAAA\n" {
		t.Fatalf("content=%q err=%v", data, err)
	}
}

// accept-new creates a missing store 0600 on an explicit path, whatever the
// umask would have left.
func TestAcceptNewCreatesExplicitStorePrivate(t *testing.T) {
	home := t.TempDir()
	known := filepath.Join(home, "store", "known_hosts")
	if _, err := Resolve("accept-new", known, home, ""); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(known); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("created store: %v %v", info, err)
	}
}

// A symbolic link where the store should be is refused, and nothing is
// created through it.
func TestAcceptNewRefusesASymbolicLinkStore(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "store")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "elsewhere")
	known := filepath.Join(dir, "known_hosts")
	if err := os.Symlink(target, known); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve("accept-new", known, home, "")
	var hostErr *Error
	if !errors.As(err, &hostErr) || hostErr.Code != "host_key_trust_store_invalid" {
		t.Fatalf("expected host_key_trust_store_invalid, got %v", err)
	}
	if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the link's target was created: %v", statErr)
	}
}

// TestResolveAcceptNewInResolverRoot is the sequence a fresh host meets:
// the path resolver made the operator's
// root first, at 0750, and the first enrollment must place the store in it.
func TestResolveAcceptNewInResolverRoot(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "share", "karvi")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	policy, err := Resolve("accept-new", "auto", home, dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "known_hosts"); policy.KnownHostsFile != want {
		t.Fatalf("store %s, want %s", policy.KnownHostsFile, want)
	}
	if fi, err := os.Stat(policy.KnownHostsFile); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("store not created 0600: %v %v", fi, err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o750 {
		t.Fatalf("the root's mode changed to %04o", fi.Mode().Perm())
	}
}
