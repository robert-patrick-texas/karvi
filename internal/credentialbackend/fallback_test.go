package credentialbackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey/sshkeytest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// scriptedInput answers the variables from a map and each prompt from a
// map by field, counting the prompts.
type scriptedInput struct {
	env     map[string]string
	answers map[string]string
	asked   []string
}

func (s *scriptedInput) LookupEnv(name string) (string, bool) {
	v, ok := s.env[name]
	return v, ok
}

func (s *scriptedInput) Prompt(_ context.Context, req PromptRequest) (string, error) {
	s.asked = append(s.asked, req.Field)
	return s.answers[req.Field], nil
}

// TestPlatformFallback is the walk after the policy's backends (none here):
// the platform's fallback in order, netvars, keys, and prompt.
func TestPlatformFallback(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	key, fingerprint := sshkeytest.Ed25519(t, "")
	locked, _ := sshkeytest.Ed25519(t, "lab passphrase")
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ecdsa"), locked, 0o600); err != nil {
		t.Fatal(err)
	}
	op := credentials.Operator{Username: "netops", UID: os.Getuid(), Home: home}
	resolve := func(t *testing.T, d inventory.Device, input *scriptedInput, sets ...string) (credentials.Resolved, error) {
		t.Helper()
		cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: sets})
		if err != nil {
			t.Fatal(err)
		}
		r, err := New(cfg, op, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.SetInput(input)
		got, err := r.Resolve(context.Background(), op, d)
		if err == nil {
			t.Cleanup(got.Credential.Material.Destroy)
		}
		return got, err
	}
	netvars := func() *scriptedInput {
		return &scriptedInput{env: map[string]string{"NETUSER": "netuser", "NETPASS": "netpass"}}
	}
	server := inventory.Direct("srv1", "linux", "native", 22)
	router := inventory.Direct("rtr1", "cisco_ios", "native", 22)

	// linux: keys alone; the variables never reach a server unasked, and a
	// key that fails is a notice on the credential.
	got, err := resolve(t, server, netvars())
	if err != nil {
		t.Fatal(err)
	}
	if got.Credential.Backend != "builtin-operator-keys" || got.DeviceUsername != "netops" || got.Credential.Material.PasswordSet() {
		t.Fatalf("keys: %+v %s", got.Credential, got.DeviceUsername)
	}
	if len(got.Credential.Keys) != 1 || got.Credential.Keys[0].Path != filepath.Join(home, ".ssh", "id_ed25519") || got.Credential.Keys[0].Fingerprint != fingerprint {
		t.Fatalf("keys: %+v", got.Credential.Keys)
	}
	if len(got.Notices) != 1 || got.Notices[0].Code != "operator_key_skipped" || got.Notices[0].Details["file"] != filepath.Join(home, ".ssh", "id_ecdsa") || got.Notices[0].Details["reason"] != "protected by a passphrase" {
		t.Fatalf("notices: %+v", got.Notices)
	}
	// linux_shell is linux's.
	if got, err := resolve(t, inventory.Direct("bast1", "linux_shell", "native", 22), netvars()); err != nil || got.Credential.Backend != "builtin-operator-keys" {
		t.Fatalf("linux_shell: %+v %v", got.Credential, err)
	}
	// No key left: the files examined are listed.
	_, err = resolve(t, server, netvars(), `ssh.identities=["~/.ssh/id_ecdsa", "~/.ssh/id_rsa"]`)
	if errorcodes.Of(err) != "credential_operator_keys_missing" || !strings.Contains(err.Error(), "id_ecdsa (skipped: protected by a passphrase), "+filepath.Join(home, ".ssh", "id_rsa")+" (absent)") {
		t.Fatalf("no key: %v", err)
	}
	// Telnet takes no key.
	if _, err := resolve(t, inventory.Direct("srv1", "linux", "telnet", 23), netvars()); errorcodes.Of(err) != "credential_password_missing" {
		t.Fatalf("telnet: %v", err)
	}
	// The site's option: the variables first, then the keys.
	opt := `platform.linux.fallback=["netvars", "keys"]`
	if got, err := resolve(t, server, netvars(), opt); err != nil || got.Credential.Backend != "builtin-env-fallback" || got.DeviceUsername != "netuser" || len(got.Credential.Keys) != 0 {
		t.Fatalf("netvars then keys: %+v %v", got.Credential, err)
	}
	// NETUSER alone makes no whole credential, and the keys answer.
	if got, err := resolve(t, server, &scriptedInput{env: map[string]string{"NETUSER": "netuser"}}, opt); err != nil || got.Credential.Backend != "builtin-operator-keys" {
		t.Fatalf("netuser alone: %+v %v", got.Credential, err)
	}
	// An alias takes its driver's fallback.
	if got, err := resolve(t, inventory.Direct("app1", "appliance", "native", 22), netvars(), `platform.appliance.driver="linux"`); err != nil || got.Credential.Backend != "builtin-operator-keys" {
		t.Fatalf("alias: %+v %v", got.Credential, err)
	}
	// An empty fallback is none.
	if _, err := resolve(t, server, netvars(), `platform.linux.fallback=[]`); errorcodes.Of(err) != "credential_username_missing" || !strings.Contains(err.Error(), "fallback (none)") {
		t.Fatalf("empty: %v", err)
	}

	// A router: the variables, then the prompt for what they leave.
	if got, err := resolve(t, router, netvars()); err != nil || got.Credential.Backend != "builtin-env-fallback" || got.DeviceUsername != "netuser" {
		t.Fatalf("router netvars: %+v %v", got.Credential, err)
	}
	input := &scriptedInput{env: map[string]string{"NETUSER": "netuser"}, answers: map[string]string{FieldPassword: "typed"}}
	if got, err := resolve(t, router, input); err != nil || got.Credential.Backend != "interactive-tty" || got.DeviceUsername != "netuser" || strings.Join(input.asked, ",") != FieldPassword {
		t.Fatalf("router prompt: %+v %v %v", got.Credential, err, input.asked)
	}
	// Without the prompt, the variables' missing field is the failure.
	if _, err := resolve(t, router, &scriptedInput{env: map[string]string{"NETUSER": "netuser"}}, "creds.interactive-prompt=false"); errorcodes.Of(err) != "credential_password_missing" {
		t.Fatalf("router no prompt: %v", err)
	}
	// NETPASS alone names no username, as before.
	if _, err := resolve(t, router, &scriptedInput{env: map[string]string{"NETPASS": "netpass"}}, "creds.interactive-prompt=false"); errorcodes.Of(err) != "credential_username_missing" {
		t.Fatalf("router netpass alone: %v", err)
	}
	// A router given the keys by its table.
	if got, err := resolve(t, router, &scriptedInput{}, `platform.cisco_ios.fallback=["keys"]`); err != nil || got.Credential.Backend != "builtin-operator-keys" {
		t.Fatalf("router keys: %+v %v", got.Credential, err)
	}
}

// TestOmitEnableResolvesNoEnableSecret: under OmitEnable, as for a login, a
// platform with requires-enable neither asks for the enable secret nor
// refuses a credential without one, NETENABLE is not read, and an enable
// secret a backend holds is dropped.
func TestOmitEnableResolvesNoEnableSecret(t *testing.T) {
	op := credentials.Operator{Username: "jsmith", UID: os.Getuid(), Home: t.TempDir()}
	router := inventory.Direct("router1", "cisco_ios", "system", 22)
	resolve := func(t *testing.T, input *scriptedInput, sets ...string) (credentials.Resolved, error) {
		t.Helper()
		cfg, err := configload.Load(configload.Options{HomeDir: op.Home, SkipAuto: true, Environment: []string{}, Sets: append([]string{"platform.cisco_ios.requires-enable=true"}, sets...)})
		if err != nil {
			t.Fatal(err)
		}
		r, err := New(cfg, op, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.SetInput(input)
		r.OmitEnable()
		got, err := r.Resolve(context.Background(), op, router)
		if err == nil {
			t.Cleanup(got.Credential.Material.Destroy)
		}
		return got, err
	}
	prompted := &scriptedInput{answers: map[string]string{FieldUsername: "jsmith", FieldPassword: "S3cret", FieldEnablePassword: "en-S3cret"}}
	if got, err := resolve(t, prompted); err != nil || got.Credential.Material.EnablePasswordSet() || strings.Join(prompted.asked, ",") != "username,password" {
		t.Errorf("the prompts: asked %v, err %v", prompted.asked, err)
	}
	if _, err := resolve(t, &scriptedInput{env: map[string]string{"NETUSER": "jsmith", "NETPASS": "S3cret"}}, "creds.interactive-prompt=false"); err != nil {
		t.Errorf("no enable secret: %v", err)
	}
	for backend, sets := range map[string][]string{
		"builtin-env-fallback": nil,
		"e":                    {`credential-backend.e.type="env"`, `credential-backend.e.username-var-template="NETUSER"`, `credential-backend.e.password-var-template="NETPASS"`, `credential-backend.e.enable-var-template="NETENABLE"`, `creds.backend-sequence=["e"]`},
	} {
		got, err := resolve(t, &scriptedInput{env: map[string]string{"NETUSER": "jsmith", "NETPASS": "S3cret", "NETENABLE": "en-S3cret"}}, append(sets, "creds.interactive-prompt=false")...)
		if err != nil || got.Credential.Backend != backend || got.Credential.Material.EnablePasswordSet() {
			t.Errorf("NETENABLE set, %s: backend %q, enable resolved %t, err %v", backend, got.Credential.Backend, err == nil && got.Credential.Material.EnablePasswordSet(), err)
		}
	}
}
