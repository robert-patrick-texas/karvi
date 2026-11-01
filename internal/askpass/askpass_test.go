package askpass

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/secrets"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

func TestBrokerReturnsOneRecognizedSecretAndThenCloses(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "enable-secret")
	defer material.Destroy()
	b, err := Start(testsocket.Dir(t), material, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := b.Environment()
	var socket, token string
	for _, value := range env {
		if strings.HasPrefix(value, SocketEnv+"=") {
			socket = strings.TrimPrefix(value, SocketEnv+"=")
		}
		if strings.HasPrefix(value, TokenEnv+"=") {
			token = strings.TrimPrefix(value, TokenEnv+"=")
		}
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(request{Token: token, Prompt: "Password:"}); err != nil {
		t.Fatal(err)
	}
	var got response
	if err := json.NewDecoder(conn).Decode(&got); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if got.Error != "" || string(got.Secret) != "login-secret" {
		t.Fatalf("unexpected response: %+v", got)
	}
	b.Wait(time.Second)
	if second, err := net.DialTimeout("unix", socket, 50*time.Millisecond); err == nil {
		second.Close()
		t.Fatal("single-use socket accepted a second connection")
	}
}

func TestBrokerRejectsUnknownPrompt(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "")
	defer material.Destroy()
	b, err := Start(testsocket.Dir(t), material, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got := ask(t, b, "", "Security question:"); got.Error != "askpass_prompt_unrecognized" || len(got.Secret) != 0 {
		t.Fatalf("unexpected response: %+v", got)
	}
}

// TestBrokerReportsARequest: requested is called for a request bearing the
// token, before its answer; not for one with another token.
func TestBrokerReportsARequest(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "")
	defer material.Destroy()
	for _, c := range []struct {
		token string
		want  bool
	}{{"", true}, {strings.Repeat("00", 32), false}} {
		called := make(chan struct{}, 1)
		b, err := Start(testsocket.Dir(t), material, func() { called <- struct{}{} })
		if err != nil {
			t.Fatal(err)
		}
		got := ask(t, b, c.token, "Password:")
		b.Close()
		if reported := len(called) == 1; reported != c.want {
			t.Errorf("token %q: reported %t, response %+v", c.token, reported, got)
		}
	}
}

// TestBrokerNeverServesTheEnableSecret: OpenSSH's password prompt carries
// the user and the HostKeyAlias, so "enable" in either is the login's
// password prompt still; a device's keyboard-interactive prompt naming an
// enable password never gets the enable secret, which a command session
// sends in-band.
func TestBrokerNeverServesTheEnableSecret(t *testing.T) {
	material := secrets.NewMaterial("jsmith", "login-secret", "enable-secret")
	defer material.Destroy()
	for _, prompt := range []string{
		"jsmith@enable-gw1.example.net's password: ",
		"svc-enable@router1's password: ",
		"(jsmith@router1) Enable password: ",
	} {
		b, err := Start(testsocket.Dir(t), material, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := ask(t, b, "", prompt)
		b.Close()
		if string(got.Secret) != "login-secret" || got.Error != "" {
			t.Errorf("%q: secret %q error %q, want the login password", prompt, got.Secret, got.Error)
		}
	}
}

// TestBrokerRefusesTheHostKeyQuestion: the broker answers the login's
// username and password alone; OpenSSH's host-key question, which karvi's
// StrictHostKeyChecking settings never let it ask, is refused.
func TestBrokerRefusesTheHostKeyQuestion(t *testing.T) {
	material := secrets.NewMaterial("jsmith", "login-secret", "")
	defer material.Destroy()
	for _, prompt := range []string{
		"The authenticity of host 'router1 (192.0.2.1)' can't be established.",
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? ",
	} {
		b, err := Start(testsocket.Dir(t), material, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := ask(t, b, "", prompt)
		b.Close()
		if len(got.Secret) != 0 || got.Error != "askpass_prompt_unrecognized" {
			t.Errorf("%q: secret %q error %q, want askpass_prompt_unrecognized", prompt, got.Secret, got.Error)
		}
	}
}

// TestBrokerJudgesThePromptsOwnWords: the user@host OpenSSH puts before a
// prompt, "user@host's " in its password prompt and "(user@host) " before a
// keyboard-interactive server's, never decides the field, whatever words the
// user or the host name holds.
func TestBrokerJudgesThePromptsOwnWords(t *testing.T) {
	material := secrets.NewMaterial("jsmith", "login-secret", "")
	defer material.Destroy()
	for _, c := range []struct {
		prompt      string
		secret, err string
	}{
		{"password@router1's password: ", "login-secret", ""},
		{"jsmith@username-gw1's password: ", "login-secret", ""},
		{"jsmith@corp@router1's password: ", "login-secret", ""},
		{"(password@router1) Username: ", "jsmith", ""},
		{"(jsmith@password-gw1) Token: ", "", "askpass_prompt_unrecognized"},
		{"(jsmith@username-gw1) Token: ", "", "askpass_prompt_unrecognized"},
		{"(jsmith@router1) Password: ", "login-secret", ""},
	} {
		b, err := Start(testsocket.Dir(t), material, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := ask(t, b, "", c.prompt)
		b.Close()
		if string(got.Secret) != c.secret || got.Error != c.err {
			t.Errorf("%q: secret %q error %q, want %q %q", c.prompt, got.Secret, got.Error, c.secret, c.err)
		}
	}
}

// ask sends one request to b, with b's own token when token is empty, and
// returns the answer.
func ask(t *testing.T, b *Broker, token, prompt string) response {
	t.Helper()
	var socket string
	for _, value := range b.Environment() {
		key, val, _ := strings.Cut(value, "=")
		switch key {
		case SocketEnv:
			socket = val
		case TokenEnv:
			if token == "" {
				token = val
			}
		}
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request{Token: token, Prompt: prompt}); err != nil {
		t.Fatal(err)
	}
	var got response
	if err := json.NewDecoder(conn).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// The broker's directory is as a rule the scratch directory and may be the
// operator's own: an existing one keeps its mode, a missing one is made 0700
// (an existing directory keeps its mode).
func TestStartLeavesAnExistingDirectoryAsItIs(t *testing.T) {
	existing := filepath.Join(testsocket.Dir(t), "mine")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(testsocket.Dir(t), "made")
	for dir, want := range map[string]os.FileMode{existing: 0o755, missing: 0o700} {
		material := secrets.NewMaterial("operator", "login-secret", "enable-secret")
		b, err := Start(dir, material, nil)
		if err != nil {
			t.Fatal(err)
		}
		b.Close()
		material.Destroy()
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s: mode %v err %v; want %04o", dir, info.Mode().Perm(), err, want)
		}
	}
}
