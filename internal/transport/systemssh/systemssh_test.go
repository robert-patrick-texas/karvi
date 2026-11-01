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

// openSSHChangedKey is OpenSSH 10.2's standard error, verbatim but for the
// names, refusing a key that differs from the stored one.
const openSSHChangedKey = "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\r\n" +
	"@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\r\n" +
	"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\r\n" +
	"IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!\r\n" +
	"Someone could be eavesdropping on you right now (man-in-the-middle attack)!\r\n" +
	"It is also possible that a host key has just been changed.\r\n" +
	"The fingerprint for the ED25519 key sent by the remote host is\r\n" +
	"SHA256:7jTCG/CNq0DqFlsu1ZyOWI78LBfckJ48ZAIK0nQVQfs.\r\n" +
	"Please contact your system administrator.\r\n" +
	"Add correct host key in /home/jsmith/.local/share/karvi/known_hosts to get rid of this message.\r\n" +
	"Offending ED25519 key in /home/jsmith/.local/share/karvi/known_hosts:1\r\n" +
	"  remove with:\r\n" +
	"  ssh-keygen -f '/home/jsmith/.local/share/karvi/known_hosts' -R 'switch01'\r\n" +
	"Host key for switch01 has changed and you have requested strict checking.\r\n" +
	"Host key verification failed.\r\n"

// TestClassifyTakesOpenSSHsLastFailureLine: the code is that of the last
// line in OpenSSH's own form for a failure, each form matched to its whole
// line; the device text before it (a banner, OpenSSH writing it during
// authentication) and lines in no such form decide nothing.
func TestClassifyTakesOpenSSHsLastFailureLine(t *testing.T) {
	banner := "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! Report changes to the NOC.\r\n" +
		"@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\r\n" +
		"Host key verification failed.\r\n" +
		"Permission denied to unauthorised users; connection refused, no route to host.\r\n"
	for _, tc := range []struct{ name, stderr, code string }{
		{"changed key", openSSHChangedKey, "host_key_changed"},
		{"revoked key", openSSHRevokedKey, "host_key_revoked"},
		{"unknown key", "No ED25519 host key is known for switch01 and you have requested strict checking.\r\nHost key verification failed.\r\n", "host_key_not_enrolled"},
		{"bare verification failure", "Host key verification failed.\r\n", "host_key_not_enrolled"},
		{"host-key words in a banner, then refused", banner + "operator@192.0.2.10: Permission denied (publickey,password).\r\n", "authentication_failed"},
		{"too many failures", banner + "Received disconnect from 192.0.2.10 port 22:2: Too many authentication failures\r\nDisconnected from 192.0.2.10 port 22\r\n", "authentication_failed"},
		{"disconnect: authentication failed", "Received disconnect from 192.0.2.10 port 22:14: Authentication failed\r\nDisconnected from 192.0.2.10 port 22\r\n", "authentication_failed"},
		{"another disconnect", banner + "Received disconnect from 192.0.2.10 port 22:11: Idle timeout\r\nDisconnected from 192.0.2.10 port 22\r\n", "ssh_process_failed"},
		{"refused", "ssh: connect to host 192.0.2.10 port 22: Connection refused\r\n", "connection_refused"},
		{"refused beyond a jump host", "channel 0: open failed: connect failed: Connection refused\r\nstdio forwarding failed\r\nkex_exchange_identification: Connection closed by remote host\r\nConnection closed by UNKNOWN port 65535\r\n", "connection_refused"},
		{"timed out", "ssh: connect to host 192.0.2.10 port 22: Connection timed out\r\n", "connection_timeout"},
		{"banner exchange timed out", "Connection timed out during banner exchange\r\nConnection to 192.0.2.10 port 22 timed out\r\n", "connection_timeout"},
		{"no route", "ssh: connect to host 192.0.2.10 port 22: No route to host\r\n", "network_unreachable"},
		{"unreachable", "ssh: connect to host 2001:db8::10 port 22: Network is unreachable\r\n", "network_unreachable"},
		{"refused by this host", "ssh: connect to host 192.0.2.10 port 22: Permission denied\r\n", "network_unreachable"},
		{"unresolved", "ssh: Could not resolve hostname switch01.example.net: Name or service not known\r\n", "ssh_name_resolution_error"},
		{"keepalives", banner + "Timeout, server 192.0.2.10 not responding.\r\n", "session_keepalive_timeout"},
		{"master refused", "Master refused session request: Permission denied\r\n", "ssh_session_channel_refused"},
		{"channel prohibited", "channel 1: open failed: administratively prohibited: open failed\r\n", "ssh_session_channel_refused"},
		{"a key file unreadable, then refused", "Load key \"/keys/k\": Permission denied\r\noperator@192.0.2.10: Permission denied (publickey).\r\n", "authentication_failed"},
		{"GSSAPI", "Mutual authentication failed\r\n", "ssh_process_failed"},
	} {
		if code, _, _, _ := classify(tc.stderr, nil); code != tc.code {
			t.Errorf("%s: %s, want %s", tc.name, code, tc.code)
		}
	}
}
