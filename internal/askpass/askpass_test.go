package askpass

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

func TestBrokerReturnsOneRecognizedSecretAndThenCloses(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "enable-secret")
	defer material.Destroy()
	b, err := Start(testsocket.Dir(t), material, Hooks{})
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
	if err := json.NewEncoder(conn).Encode(request{Token: token, Prompt: "operator@router1's password: "}); err != nil {
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
	b, err := Start(testsocket.Dir(t), material, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got := ask(t, b, "", "(operator@router1) Security question:"); got.Error != "askpass_prompt_unrecognized" || len(got.Secret) != 0 {
		t.Fatalf("unexpected response: %+v", got)
	}
}

// TestBrokerReportsARequest: Hooks.Requested is called for a request bearing
// the token, before its answer; not for one with another token.
func TestBrokerReportsARequest(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "")
	defer material.Destroy()
	for _, c := range []struct {
		token string
		want  bool
	}{{"", true}, {strings.Repeat("00", 32), false}} {
		called := make(chan struct{}, 1)
		b, err := Start(testsocket.Dir(t), material, Hooks{Requested: func() { called <- struct{}{} }})
		if err != nil {
			t.Fatal(err)
		}
		got := ask(t, b, c.token, "operator@router1's password: ")
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
		b, err := Start(testsocket.Dir(t), material, Hooks{})
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

// TestBrokerRefusesOpenSSHsOwnQuestions: the broker answers only a prompt
// beginning with the user@host OpenSSH puts before a device's; OpenSSH's own
// questions are refused, a key's passphrase, a FIDO or PKCS#11 PIN, user
// presence, a password change, and the host-key question (which karvi's
// StrictHostKeyChecking settings never let it ask) among them, and none is
// a change prompt.
func TestBrokerRefusesOpenSSHsOwnQuestions(t *testing.T) {
	material := secrets.NewMaterial("jsmith", "login-secret", "")
	defer material.Destroy()
	for _, prompt := range []string{
		"Enter passphrase for key '/home/jsmith/.ssh/id_ed25519': ",
		"Enter passphrase for /home/jsmith/.ssh/id_ed25519: ",
		"Enter PIN for ED25519-SK key /home/jsmith/.ssh/id_ed25519_sk: ",
		"Enter PIN for 'PIV Card Holder pin (PIV_II)': ",
		"Enter PIN for PIV_II: ",
		"Confirm user presence for key ED25519-SK SHA256:abc",
		"Enter jsmith@router1's old password: ",
		"Enter jsmith@router1's new password: ",
		"Retype jsmith@router1's new password: ",
		"The authenticity of host 'router1 (192.0.2.1)' can't be established.",
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? ",
		"Password: ",
		"",
	} {
		b, err := Start(testsocket.Dir(t), material, Hooks{Change: func() { t.Errorf("%q: the change hook was called", prompt) }})
		if err != nil {
			t.Fatal(err)
		}
		got := ask(t, b, "", prompt)
		b.Close()
		if len(got.Secret) != 0 || got.Error != "askpass_prompt_unrecognized" {
			t.Errorf("%q: secret %q error %q, want askpass_prompt_unrecognized", prompt, got.Secret, got.Error)
		}
		if err := b.Change(); err != nil {
			t.Errorf("%q: Change() = %v, want nil", prompt, err)
		}
	}
}

// TestBrokerEndsOpenSSHAtAChangePrompt: a change prompt calls the change
// hook before its refusal is sent, so the transport ends OpenSSH while it
// still waits on the helper; the broker then reports the prompt's own words
// as authentication_change_requested, and no secret leaves it.
func TestBrokerEndsOpenSSHAtAChangePrompt(t *testing.T) {
	material := secrets.NewMaterial("jsmith", "login-secret", "")
	defer material.Destroy()
	for _, c := range []struct{ prompt, words string }{
		{"(jsmith@router1) Enter new PIN: ", "Enter new PIN: "},
		{"(jsmith@router1) New password: ", "New password: "},
		{"(jsmith@router1) Do you want to enter your own pin (y/n) ", "Do you want to enter your own pin (y/n) "},
		{"(jsmith@router1) Enter Next PASSCODE: ", "Enter Next PASSCODE: "},
	} {
		var b *Broker
		hooked := 0
		b, err := Start(testsocket.Dir(t), material, Hooks{Change: func() {
			hooked++
			if b.Change() == nil {
				t.Errorf("%q: the change was not recorded before the hook", c.prompt)
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		got := ask(t, b, "", c.prompt)
		b.Close()
		if len(got.Secret) != 0 || got.Error != "authentication_change_requested" {
			t.Errorf("%q: secret %q error %q, want authentication_change_requested", c.prompt, got.Secret, got.Error)
		}
		if hooked != 1 {
			t.Errorf("%q: the change hook was called %d times, want once", c.prompt, hooked)
		}
		want := `authentication_change_requested: the device asks to change a secret: "` + c.words + `"`
		if err := b.Change(); err == nil || errorcodes.Message(err) != want {
			t.Errorf("%q: Change() = %v, want %s", c.prompt, err, want)
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
		{"(jsmith@password-gw1) Response: ", "", "askpass_prompt_unrecognized"},
		{"(jsmith@username-gw1) Response: ", "", "askpass_prompt_unrecognized"},
		{"(jsmith@router1) Token: ", "login-secret", ""},
		{"(jsmith@router1) Password: ", "login-secret", ""},
		{"(jsmith@router1) Username: ", "jsmith", ""},
		{"(jsmith@router1) New password: ", "", "authentication_change_requested"},
		{"(jsmith@new-rtr1) Password: ", "login-secret", ""},
		{"(jsmith@pin-gw1) Username for pin-gw1: ", "jsmith", ""},
	} {
		b, err := Start(testsocket.Dir(t), material, Hooks{})
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

// TestField: what a device's prompt asks for, by the first of the change,
// username, and password lists to match it, or none. Each pattern is a word
// of its own, so a device named in the prompt is not a field's word
// (pin-gw1, passau-rtr1, new-rtr1, new1, new_rtr1, repeater1, gw-change),
// a dot does not join (change.example.net), and a bracket or colon at a
// pattern's own end stands beside a word (PIN(y/n)?, Name:jsmith); any
// prompt naming user gets the username, the password prompts naming one
// among them.
func TestField(t *testing.T) {
	for _, c := range []struct{ prompt, want string }{
		{"Password: ", FieldPassword},
		{"password: ", FieldPassword},
		{"Passwd: ", FieldPassword},
		{"Passwort: ", FieldPassword},
		{"Pass: ", FieldPassword},
		{"Enter PASSCODE: ", FieldPassword},
		{"Passphrase: ", FieldPassword},
		{"PIN: ", FieldPassword},
		{"Enter PIN ", FieldPassword},
		{"Token: ", FieldPassword},
		{"Enter tokencode: ", FieldPassword},
		{"Verification code: ", FieldPassword},
		{"Enable password: ", FieldPassword},
		{"Password for pin-gw1: ", FieldPassword},
		{"Password for passau-rtr1: ", FieldPassword},
		{"Password for key-west-rtr1: ", FieldPassword},
		{"Password for new-rtr1: ", FieldPassword},
		{"Password for login-gw1: ", FieldPassword},
		{"Username: ", FieldUsername},
		{"User: ", FieldUsername},
		{"login as: ", FieldUsername},
		{"Login: ", FieldUsername},
		{"UserID: ", FieldUsername},
		{"User name: ", FieldUsername},
		{"Name: ", FieldUsername},
		{"Username for spine-01: ", FieldUsername},
		{"Username for passau-rtr1: ", FieldUsername},
		{"Username for passport-fw1: ", FieldUsername},
		{"Username for barcode-sw1: ", FieldUsername},
		{"Login for pin-gw1: ", FieldUsername},
		{"pin-gw1 login: ", FieldUsername},
		{"Login password: ", FieldUsername},
		{"Password for user jsmith: ", FieldUsername},
		{"Password for superuser: ", FieldUsername},
		{"Enter new PIN: ", FieldChange},
		{"Enter a new PIN having from 4 to 8 digits: ", FieldChange},
		{"Re-enter PIN: ", FieldChange},
		{"Reenter PIN: ", FieldChange},
		{"Retype new password: ", FieldChange},
		{"Confirm PIN: ", FieldChange},
		{"Verify PIN: ", FieldChange},
		{"Repeat password: ", FieldChange},
		{"Password again: ", FieldChange},
		{"PIN (again): ", FieldChange},
		{"Renew your PIN: ", FieldChange},
		{"Change password: ", FieldChange},
		{"Your password has expired; change it now: ", FieldChange},
		{"Do you want to enter your own pin (y/n) ", FieldChange},
		{"Are you prepared to have the system generate your PIN? (y/n) ", FieldChange},
		{"ACCEPT A SYSTEM GENERATED PIN ", FieldChange},
		{"Create a password: ", FieldChange},
		{"Accept the PIN (yes/no)? ", FieldChange},
		{"Enter Next PASSCODE: ", FieldChange},
		{"Wait for the token code to change, then enter the new passcode: ", FieldChange},
		{"New password: ", FieldChange},
		{"Re-type PIN: ", FieldChange},
		{"Re-type password: ", FieldChange},
		{"Reconfirm PIN: ", FieldChange},
		{"Password (new): ", FieldChange},
		{"Enter PIN once more: ", FieldChange},
		{"New\u00a0PIN: ", FieldChange},
		{"Do you want to enter your own PIN? [y/n] ", FieldChange},
		{"Your password must be changed: ", FieldChange},
		{"Confirmed PIN: ", FieldChange},
		{"Repeated password: ", FieldChange},
		{"Renewed PIN: ", FieldChange},
		{"Created PIN: ", FieldChange},
		{"Re-entered PIN: ", FieldChange},
		{"Retyped password: ", FieldChange},
		{"Changing password for jsmith: ", FieldChange},
		{"The system generates your PIN: ", FieldChange},
		{"Password confirmation: ", FieldPassword},
		{"Enter confirmation code: ", FieldPassword},
		{"Verifying PIN: ", FieldPassword},
		{"Password for changes-gw1: ", FieldPassword},
		{"Enter your PIN.", FieldPassword},
		{"PIN?", FieldPassword},
		{"OTP: ", FieldPassword},
		{"Enter OTP: ", FieldPassword},
		{"Current password: ", FieldPassword},
		{"Password for repeater1: ", FieldPassword},
		{"Password for change-rtr1: ", FieldPassword},
		{"Password for confirm-gw1: ", FieldPassword},
		{"Password for verify-lab1: ", FieldPassword},
		{"Password for gw-change: ", FieldPassword},
		{"Password for generator-rtr1: ", FieldPassword},
		{"Password for create-sw1: ", FieldPassword},
		{"Password for renew-fw1: ", FieldPassword},
		{"Password for again-gw: ", FieldPassword},
		{"Password for next-hop1: ", FieldPassword},
		{"Password for newark-rtr1: ", FieldPassword},
		{"Password for new1: ", FieldPassword},
		{"Password for new_rtr1: ", FieldPassword},
		{"Password for change.example.net: ", FieldChange},
		{"Accept PIN(y/n)? ", FieldChange},
		{"Accept system PIN[y/n] ", FieldChange},
		{"Continue(y/n): ", FieldChange},
		{"ACCEPT A SYSTEM GENERATED PIN(y/n)", FieldChange},
		{"Name:jsmith", FieldUsername},
		{"Pass:", FieldPassword},
		{"Password for user-gw1: ", FieldUsername},
		{"Hostname: ", ""},
		{"New-PIN: ", ""},
		{"Response: ", ""},
		{"Challenge: ", ""},
		{"Security question: ", ""},
		{"", ""},
	} {
		if got := Field(c.prompt); got != c.want {
			t.Errorf("Field(%q) = %q, want %q", c.prompt, got, c.want)
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
		b, err := Start(dir, material, Hooks{})
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
