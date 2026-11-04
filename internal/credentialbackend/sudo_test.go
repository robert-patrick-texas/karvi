package credentialbackend

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey/sshkeytest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// TestSudoPassword is the rule for a --sudo command's password (EXAMPLES
// chapter 96, issue 3): on an exec channel the credential's enable field is
// sudo's password, chosen by the source that answered, and resolved only for
// a device with a --sudo command. A backend's enable field is its own (blank
// is none, never filled by NETSUDO or NETENABLE); under the fallback NETSUDO,
// else NETENABLE where the fallback holds netvars, else none; the terminal is
// never asked for it; requires-enable requires it of a --sudo device alone.
// A shell device is as before.
func TestSudoPassword(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	key, _ := sshkeytest.Ed25519(t, "")
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	csv := filepath.Join(home, "credentials.csv")
	body := "device_name,username,password,enable_password\n" +
		"blank-*,svc,pw,\n" +
		"cell-*,svc,pw,env:NETSUDO\n" +
		"lit-*,svc,pw,en-row\n"
	if err := os.WriteFile(csv, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	creds := []string{`credential-backend.creds.type="csv"`, `credential-backend.creds.scope="user"`, `credential-backend.creds.path="` + csv + `"`, `creds.backend-sequence=["creds"]`}
	alias := []string{`platform.srv.driver="linux"`, "platform.srv.requires-enable=true", `platform.srv.fallback=["netvars", "prompt"]`}
	netvars := `platform.linux.fallback=["netvars"]`
	op := credentials.Operator{Username: "jsmith", UID: os.Getuid(), Home: home}
	type got struct {
		backend, enable, sudo string
		input                 *scriptedInput
	}
	resolve := func(t *testing.T, d inventory.Device, input *scriptedInput, sets ...string) (got, error) {
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
		res, err := r.Resolve(context.Background(), op, d)
		if err != nil {
			return got{input: input}, err
		}
		defer res.Credential.Material.Destroy()
		_, _, enable, err := materialStrings(res.Credential.Material)
		if err != nil {
			t.Fatal(err)
		}
		return got{backend: res.Credential.Backend, enable: enable, sudo: res.Sudo, input: input}, nil
	}
	env := func(vars ...string) *scriptedInput {
		in := &scriptedInput{env: map[string]string{}}
		for _, v := range vars {
			name, value, _ := strings.Cut(v, "=")
			in.env[name] = value
		}
		return in
	}
	device := func(name, platform string, sudo bool) inventory.Device {
		d := inventory.Direct(name, platform, "native", 22)
		d.Sudo = sudo
		return d
	}
	server := func(sudo bool) inventory.Device { return device("srv1", "linux", sudo) }
	user := []string{"NETUSER=netuser", "NETPASS=netpass"}

	for _, tc := range []struct {
		name   string
		d      inventory.Device
		input  *scriptedInput
		sets   []string
		want   got
		absent []string // variables never looked up
	}{
		// The fallback answered.
		{"linux's keys, NETSUDO", server(true), env("NETSUDO=s-pw", "NETENABLE=en"), nil, got{backend: "builtin-operator-keys", enable: "s-pw", sudo: credentials.SudoNETSUDO}, []string{"NETENABLE"}},
		{"linux's keys, NETENABLE alone", server(true), env("NETENABLE=en"), nil, got{backend: "builtin-operator-keys", sudo: credentials.SudoNone}, []string{"NETENABLE"}},
		{"netvars, NETENABLE", server(true), env(append(user, "NETENABLE=en")...), []string{netvars}, got{backend: "builtin-env-fallback", enable: "en", sudo: credentials.SudoNETENABLE}, nil},
		{"netvars, NETSUDO before NETENABLE", server(true), env(append(user, "NETENABLE=en", "NETSUDO=s-pw")...), []string{netvars}, got{backend: "builtin-env-fallback", enable: "s-pw", sudo: credentials.SudoNETSUDO}, nil},
		{"netvars, an empty NETSUDO is unset", server(true), env(append(user, "NETENABLE=en", "NETSUDO=")...), []string{netvars}, got{backend: "builtin-env-fallback", enable: "en", sudo: credentials.SudoNETENABLE}, nil},
		{"netvars, neither", server(true), env(user...), []string{netvars}, got{backend: "builtin-env-fallback", sudo: credentials.SudoNone}, nil},
		{"netvars then keys, the keys answer", server(true), env("NETUSER=netuser", "NETENABLE=en"), []string{`platform.linux.fallback=["netvars", "keys"]`}, got{backend: "builtin-operator-keys", enable: "en", sudo: credentials.SudoNETENABLE}, nil},
		{"no --sudo command: none read", server(false), env(append(user, "NETENABLE=en", "NETSUDO=s-pw")...), []string{netvars}, got{backend: "builtin-env-fallback"}, []string{"NETSUDO", "NETENABLE"}},
		{"a shell device: NETENABLE, never NETSUDO", device("rtr1", "cisco_ios", true), env(append(user, "NETENABLE=en", "NETSUDO=s-pw")...), nil, got{backend: "builtin-env-fallback", enable: "en"}, []string{"NETSUDO"}},
		// requires-enable on exec requires sudo's password of a --sudo
		// device alone, and the terminal is not asked for it.
		{"requires-enable, NETSUDO", device("app1", "srv", true), env(append(user, "NETSUDO=s-pw")...), alias, got{backend: "builtin-env-fallback", enable: "s-pw", sudo: credentials.SudoNETSUDO}, nil},
		{"requires-enable, no --sudo command", device("app1", "srv", false), env(user...), alias, got{backend: "builtin-env-fallback"}, []string{"NETSUDO", "NETENABLE"}},
		// A backend answered: its enable field, blank or not.
		{"a blank enable cell is none", device("blank-1", "linux", true), env("NETSUDO=s-pw", "NETENABLE=en"), creds, got{backend: "creds", sudo: credentials.SudoNone}, []string{"NETSUDO", "NETENABLE"}},
		{"an env:NETSUDO cell", device("cell-1", "linux", true), env("NETSUDO=s-pw"), creds, got{backend: "creds", enable: "s-pw", sudo: credentials.SudoEnable}, nil},
		{"the row's enable", device("lit-1", "linux", true), env(), creds, got{backend: "creds", enable: "en-row", sudo: credentials.SudoEnable}, nil},
		{"the row's enable, no --sudo command", device("lit-1", "linux", false), env(), creds, got{backend: "creds"}, nil},
		{"an env backend naming NETSUDO", server(true), env(append(user, "NETSUDO=s-pw")...), []string{`credential-backend.e.type="env"`, `credential-backend.e.username-var-template="NETUSER"`, `credential-backend.e.password-var-template="NETPASS"`, `credential-backend.e.enable-var-template="NETSUDO"`, `creds.backend-sequence=["e"]`}, got{backend: "e", enable: "s-pw", sudo: credentials.SudoEnable}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := resolve(t, tc.d, tc.input, tc.sets...)
			if err != nil {
				t.Fatal(err)
			}
			if g.backend != tc.want.backend || g.enable != tc.want.enable || g.sudo != tc.want.sudo {
				t.Errorf("backend %q enable %q sudo %q, want %q %q %q", g.backend, g.enable, g.sudo, tc.want.backend, tc.want.enable, tc.want.sudo)
			}
			if len(g.input.asked) != 0 {
				t.Errorf("prompts asked: %v", g.input.asked)
			}
			for _, name := range tc.absent {
				if slices.Contains(g.input.looked, name) {
					t.Errorf("%s looked up: %v", name, g.input.looked)
				}
			}
		})
	}

	// requires-enable with no password: credential_enable_missing at
	// planning, naming sudo; the terminal is asked for the password the
	// variables left, never for sudo's.
	asked := &scriptedInput{env: map[string]string{"NETUSER": "netuser"}, answers: map[string]string{FieldPassword: "typed"}}
	if _, err := resolve(t, device("app1", "srv", true), asked, alias...); errorcodes.Of(err) != "credential_enable_missing" || !strings.Contains(err.Error(), "sudo's for a --sudo command") || strings.Join(asked.asked, ",") != FieldPassword {
		t.Errorf("no password: %v, asked %v", err, asked.asked)
	}
	if _, err := resolve(t, device("blank-1", "srv", true), env(), append(alias, creds...)...); errorcodes.Of(err) != "credential_enable_missing" {
		t.Errorf("a blank cell under requires-enable: %v", err)
	}
	if g, err := resolve(t, device("blank-1", "srv", false), env(), append(alias, creds...)...); err != nil || g.enable != "" || g.sudo != "" {
		t.Errorf("a blank cell under requires-enable, no --sudo command: %+v %v", g, err)
	}
}
