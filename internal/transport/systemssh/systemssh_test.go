package systemssh

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
)

func TestManagedOptionsPrecedeUserInclude(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host bastion\n  ProxyJump jump.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := (Factory{Config: cfg, Home: home, BaseDir: filepath.Join(home, ".local", "share", "karvi"), ControlRoot: filepath.Join(home, "ctl")}).renderConfig()
	if err != nil {
		t.Fatal(err)
	}
	managed := strings.Index(text, "Host *")
	include := strings.Index(text, "Include ")
	if managed < 0 || include < 0 || managed >= include {
		t.Fatalf("managed block must precede user Include:\n%s", text)
	}
	if !strings.Contains(text, "StrictHostKeyChecking accept-new") {
		t.Fatalf("missing managed host-key policy:\n%s", text)
	}
}

// TestManagedAuthentication: a credential without keys offers none; one with
// keys offers them alone, in order, with no agent; one without a password
// turns the password methods off. OpenSSH, where present, reads the file so
// (ssh -G), the included ~/.ssh/config's IdentityFile lines after karvi's.
func TestManagedAuthentication(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host *\n  IdentityFile ~/.ssh/id_user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	base := Factory{Config: cfg, Home: home, BaseDir: filepath.Join(home, ".local", "share", "karvi")}
	render := func(keys []string, passwordless bool) string {
		t.Helper()
		f := base
		f.identities, f.passwordless = keys, passwordless
		text, err := f.renderConfig()
		if err != nil {
			t.Fatal(err)
		}
		return text
	}
	keys := []string{"/keys/first key", "/keys/second"}
	for _, tc := range []struct {
		name         string
		keys         []string
		passwordless bool
		want, absent []string
	}{
		{"no keys", nil, false, []string{"PubkeyAuthentication no", "PasswordAuthentication yes", "KbdInteractiveAuthentication yes"}, []string{"IdentityFile", "IdentitiesOnly"}},
		{"keys and a password", keys, false, []string{"PubkeyAuthentication yes\n  IdentitiesOnly yes\n  IdentityAgent none\n  IdentityFile \"/keys/first key\"\n  IdentityFile \"/keys/second\"\n", "PasswordAuthentication yes"}, nil},
		{"keys alone", keys, true, []string{"PubkeyAuthentication yes", "PasswordAuthentication no", "KbdInteractiveAuthentication no"}, nil},
	} {
		text := render(tc.keys, tc.passwordless)
		for _, w := range tc.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: missing %q in\n%s", tc.name, w, text)
			}
		}
		for _, a := range tc.absent {
			if strings.Contains(text, a) {
				t.Errorf("%s: unexpected %q in\n%s", tc.name, a, text)
			}
		}
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no OpenSSH client to read the file")
	}
	// The lists this client implements, as Open writes them.
	if base.offered, err = base.offeredAlgorithms(binaryCapabilities(cfg, ssh).implements); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "managed.conf")
	if err := os.WriteFile(path, []byte(render(keys, true)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(ssh, "-G", "-F", path, "192.0.2.10").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 && (f[0] == "identityfile" || f[0] == "identitiesonly" || f[0] == "identityagent" || f[0] == "passwordauthentication") {
			got = append(got, line)
		}
	}
	want := "identitiesonly yes|passwordauthentication no|identityagent none|identityfile /keys/first key|identityfile /keys/second|identityfile ~/.ssh/id_user"
	if strings.Join(got, "|") != want {
		t.Fatalf("ssh -G:\n%s\nwant %s", strings.Join(got, "\n"), want)
	}
}
