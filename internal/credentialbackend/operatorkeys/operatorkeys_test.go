package operatorkeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey"
	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey/sshkeytest"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credfile"
)

// TestJudge: the listed files in order, ~/ the home: an absent file passed
// over, a usable key at 0600 or 0400 kept with its fingerprint, and every
// other skipped with its reason and never its contents.
func TestJudge(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(ssh, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	good, goodFP := sshkeytest.Ed25519(t, "")
	readOnly, readOnlyFP := sshkeytest.Ed25519(t, "")
	locked, _ := sshkeytest.Ed25519(t, "lab passphrase")
	write("id_ed25519", good, 0o600)
	write("id_ro", readOnly, 0o400)
	write("id_locked", locked, 0o600)
	write("id_open", good, 0o644)
	write("id_ed25519_sk", sshkeytest.SK(t), 0o600)
	write("id_junk", []byte("not a key\n"), 0o600)
	if err := os.Symlink(filepath.Join(ssh, "id_ed25519"), filepath.Join(ssh, "id_link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(ssh, "id_dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	rules := credfile.Rules{Home: home, UID: os.Getuid()}
	got := Judge(rules, []string{"~/.ssh/id_missing", "~/.ssh/id_locked", "~/.ssh/id_ed25519", "~/.ssh/id_open", "~/.ssh/id_ro", "~/.ssh/id_ed25519_sk", "~/.ssh/id_link", "~/.ssh/id_dir", "~/.ssh/id_junk"})
	if len(got.Keys) != 2 || got.Keys[0].Path != filepath.Join(ssh, "id_ed25519") || got.Keys[0].Fingerprint != goodFP || got.Keys[1].Path != filepath.Join(ssh, "id_ro") || got.Keys[1].Fingerprint != readOnlyFP {
		t.Fatalf("keys: %+v", got.Keys)
	}
	want := map[string]string{
		"id_locked":     sshkey.ReasonPassphrase,
		"id_open":       "mode 0644, not 0600 or 0400",
		"id_ed25519_sk": sshkey.ReasonUnsupported,
		"id_link":       "a symbolic link, and security.allow-credential-symlinks is false",
		"id_dir":        "not a regular file",
		"id_junk":       sshkey.ReasonUnreadable,
	}
	if len(got.Skipped) != len(want) {
		t.Fatalf("skipped: %+v", got.Skipped)
	}
	for _, s := range got.Skipped {
		if want[filepath.Base(s.Path)] != s.Reason {
			t.Errorf("%s: %q, want %q", s.Path, s.Reason, want[filepath.Base(s.Path)])
		}
	}
	if len(got.Examined) != 9 || got.Examined[0] != filepath.Join(ssh, "id_missing")+" (absent)" || got.Examined[2] != filepath.Join(ssh, "id_ed25519") {
		t.Fatalf("examined: %q", got.Examined)
	}
	for _, line := range got.Examined {
		if strings.Contains(line, "PRIVATE") {
			t.Fatalf("contents in %q", line)
		}
	}
	// A symbolic link is followed where the credential file rule allows it.
	rules.AllowSymlink = true
	if got := Judge(rules, []string{"~/.ssh/id_link"}); len(got.Keys) != 1 || got.Keys[0].Path != filepath.Join(ssh, "id_link") || got.Keys[0].Fingerprint != goodFP {
		t.Fatalf("allowed symlink: %+v", got)
	}
	// A file another account owns is not the operator's.
	rules.UID = os.Getuid() + 1
	if got := Judge(rules, []string{"~/.ssh/id_ed25519"}); len(got.Skipped) != 1 || got.Skipped[0].Reason != "not owned by the operator" {
		t.Fatalf("owner: %+v", got)
	}
}
