package systemssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
)

func loadPolicyConfig(t *testing.T, home string, sets ...string) configload.Snapshot {
	t.Helper()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: sets})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestAutoPolicyUsesKarviTrustStore(t *testing.T) {
	home := t.TempDir()
	cfg := loadPolicyConfig(t, home)
	text, err := (Factory{Config: cfg, Home: home}).renderConfig()
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(home, ".local", "share", "karvi", "known_hosts")
	for _, want := range []string{
		"StrictHostKeyChecking accept-new",
		"UserKnownHostsFile \"" + expected + "\"",
		"GlobalKnownHostsFile \"/dev/null\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
}

func TestSecurePolicyRequiresAndUsesEnrolledStore(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "share", "karvi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(known, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := loadPolicyConfig(t, home, "ssh.host-key-policy=secure")
	text, err := (Factory{Config: cfg, Home: home}).renderConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "StrictHostKeyChecking yes") || !strings.Contains(text, known) {
		t.Fatalf("secure config:\n%s", text)
	}
}

func TestInsecurePolicyUsesNoTrustFileForConnection(t *testing.T) {
	home := t.TempDir()
	cfg := loadPolicyConfig(t, home, "ssh.host-key-policy=insecure")
	text, err := (Factory{Config: cfg, Home: home}).renderConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "StrictHostKeyChecking no") {
		t.Fatalf("insecure config:\n%s", text)
	}
	if strings.Count(text, "KnownHostsFile \"/dev/null\"") != 2 {
		t.Fatalf("insecure config should isolate user and global stores:\n%s", text)
	}
}

func TestHostKeyFailureClassification(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		code   string
	}{
		{name: "changed", stderr: "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!", code: "host_key_changed"},
		{name: "secure unknown", stderr: "No ED25519 host key is known for switch.example and you have requested strict checking. Host key verification failed.", code: "host_key_not_enrolled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, category, external, retryable := classify(tt.stderr, os.ErrPermission)
			if code != tt.code || category != "connection" || !external || retryable {
				t.Fatalf("classify() = %q %q %v %v", code, category, external, retryable)
			}
		})
	}
}

func TestHostKeyAlgorithmsStrongestFirstFilteredByAlias(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "share", "karvi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(dir, "known_hosts")
	entry := "switch1 ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBKdLeu+Gh6muvMCpEbo6wtbvWFqSXH4l4+pwLHHg5ZJF8QNOlquvLFKchmZSk2S3ZzpY1cKpkHQyBJTcBkTKaQE=\n"
	if err := os.WriteFile(known, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	whole := "  HostKeyAlgorithms ssh-ed25519,ecdsa-sha2-nistp521,ecdsa-sha2-nistp384,ecdsa-sha2-nistp256,rsa-sha2-512,rsa-sha2-256,ssh-rsa\n"
	for _, c := range []struct {
		name, policy string
		identity     string
		want         string
	}{
		{name: "unknown alias", policy: "accept-new", identity: "switch2", want: whole},
		{name: "enrolled alias", policy: "accept-new", identity: "switch1", want: "  HostKeyAlgorithms ecdsa-sha2-nistp256\n"},
		{name: "secure enrolled alias", policy: "secure", identity: "switch1", want: "  HostKeyAlgorithms ecdsa-sha2-nistp256\n"},
		{name: "insecure", policy: "insecure", identity: "switch1", want: whole},
	} {
		cfg := loadPolicyConfig(t, home, "ssh.host-key-policy="+c.policy)
		text, err := (Factory{Config: cfg, Home: home, hostKeyIdentity: c.identity}).renderConfig()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "Host *\n  StrictHostKeyChecking") || !strings.Contains(text, c.want) {
			t.Fatalf("%s: want %q in:\n%s", c.name, c.want, text)
		}
	}
}

func TestNegotiationDiagnostics(t *testing.T) {
	stderr := "Warning: Permanently added 'switch1' (ED25519) to the list of known hosts.\r\nUnable to negotiate with 192.0.2.10 port 22: no matching host key type found. Their offer: ssh-ed25519,ecdsa-sha2-nistp256\r\n"
	offered, ok := hostKeyTypesOffered(stderr)
	if !ok || strings.Join(offered, "|") != "ssh-ed25519|ecdsa-sha2-nistp256" {
		t.Fatalf("offered %q %t", offered, ok)
	}
	if _, ok := hostKeyTypesOffered("Permission denied (password)."); ok {
		t.Fatal("matched an unrelated diagnostic")
	}
	got := safeDiagnostic("Warning: Permanently added 'switch1' (ED25519) to the list of known hosts.\r\nnetops@192.0.2.10: Permission denied (password).\r\nConnection to 192.0.2.10 closed.\r\n", nil)
	if got != "netops@192.0.2.10: Permission denied (password)." {
		t.Fatalf("diagnostic %q", got)
	}
}
