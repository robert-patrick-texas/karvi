package native

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey/sshkeytest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
	"github.com/robert-patrick-texas/karvi/platform"
)

func factory(t *testing.T, policy string, sets ...string) (Factory, string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "store")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(known, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: append([]string{
		fmt.Sprintf("ssh.host-key-policy=%q", policy), fmt.Sprintf("ssh.known-hosts-file=%q", known), `execution.prompt-timeout="5s"`,
	}, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	return Factory{Config: cfg, Home: home}, known
}

func request(srv *fakedevice.Server, platformName string, enable string) platform.OpenRequest {
	def, _ := platform.Builtin(platformName)
	def.Base = def.Name
	req := platform.OpenRequest{Address: "127.0.0.1", Port: uint16(srv.Port()), Username: "netops", Definition: def,
		Password: func(f func([]byte) error) error { return f([]byte("pw")) },
		Metadata: map[string]string{"canonical_name": "fake-iosxe"}}
	if enable != "" {
		req.EnablePassword = func(f func([]byte) error) error { return f([]byte(enable)) }
	}
	return req
}

func prepared(t *testing.T, f Factory, req platform.OpenRequest) platform.Driver {
	t.Helper()
	d, err := f.Open(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return d
}

// TestProviderRunsTheDeviceSession: the native transport is
// karvi's connection under karvi's device session: one connection and one
// shell, no extra returns, the definition's paging order, device errors
// from the platform's patterns, connection_reused, and a timeout that ends
// the session with nothing more sent.
func TestProviderRunsTheDeviceSession(t *testing.T) {
	srv := startFake(t, fakedevice.Options{Enable: "en", Delay: map[string]time.Duration{"show slow": 3 * time.Second}})
	f, known := factory(t, "accept-new")
	d := prepared(t, f, request(srv, "cisco_ios", "en"))
	ctx := context.Background()
	clock := d.Execute(ctx, platform.Command{Text: "show clock", Timeout: 5 * time.Second})
	bogus := d.Execute(ctx, platform.Command{Text: "show bogus", Timeout: 5 * time.Second})
	slow := d.Execute(ctx, platform.Command{Text: "show slow", Timeout: 300 * time.Millisecond})
	if clock.Err != nil || clock.Prompt != "Router#" || clock.ConnectionReused == nil || *clock.ConnectionReused {
		t.Fatalf("show clock: %+v", clock)
	}
	if bogus.ErrorCode != "device_command_error" || bogus.ConnectionReused == nil || !*bogus.ConnectionReused {
		t.Fatalf("show bogus: %+v", bogus)
	}
	if slow.ErrorCode != "command_timeout" || d.Usable() {
		t.Fatalf("show slow: %+v usable=%t", slow, d.Usable())
	}
	if r := d.Execute(ctx, platform.Command{Text: "show version", Timeout: time.Second}); r.ErrorCode != "command_session_lost" {
		t.Fatalf("after the timeout: %+v", r)
	}
	d.Close()
	time.Sleep(100 * time.Millisecond)
	want := "enable|<secret>|terminal length 0|terminal width 512|show clock|show bogus|show slow"
	if got := strings.Join(srv.Lines(), "|"); got != want {
		t.Fatalf("device saw %q, want %q", got, want)
	}
	if srv.Connections() != 1 || srv.Sessions() != 1 {
		t.Fatalf("connections=%d sessions=%d", srv.Connections(), srv.Sessions())
	}
	content, _ := os.ReadFile(known)
	if !strings.HasPrefix(string(content), fmt.Sprintf("[fake-iosxe]:%d ssh-ed25519 ", srv.Port())) {
		t.Fatalf("enrolled %q", content)
	}
}

// TestProviderEnableOptional: the enable secret is optional on the
// native transport.
func TestProviderEnableOptional(t *testing.T) {
	f, _ := factory(t, "insecure")
	srv := startFake(t, fakedevice.Options{StartPrivileged: true, Enable: "en"})
	d := prepared(t, f, request(srv, "cisco_ios", ""))
	if r := d.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 5 * time.Second}); r.Err != nil {
		t.Fatalf("privilege 15 at login: %+v", r)
	}
	d.Close()
	if got := strings.Join(srv.Lines(), "|"); !strings.HasPrefix(got, "terminal length 0|") {
		t.Fatalf("privilege 15 at login, device saw %q", got)
	}

	asks := startFake(t, fakedevice.Options{Enable: "en"})
	d, err := f.Open(context.Background(), request(asks, "cisco_ios", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Prepare(context.Background()); errorcodes.Of(err) != "privilege_failed" || !strings.Contains(err.Error(), "the credential has none") {
		t.Fatalf("a secret asked, none resolved: %v", err)
	}
}

// TestProviderAlgorithmsAndAdmission covers the device's algorithm lists on
// the native transport (a legacy device through a profile) and the
// admission table.
func TestProviderAlgorithmsAndAdmission(t *testing.T) {
	legacy := startFake(t, fakedevice.Options{RSASHA1Only: true, KeyExchanges: []string{"diffie-hellman-group14-sha1"}, Ciphers: []string{"aes128-ctr"}, MACs: []string{"hmac-sha1"}})
	f, _ := factory(t, "insecure")
	d, err := f.Open(context.Background(), request(legacy, "generic", ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Prepare(context.Background()); errorcodes.Of(err) != "ssh_algorithm_negotiation_failed" {
		t.Fatalf("defaults: %v", err)
	}
	d.Close()
	f.Algorithms = sshalgorithms.Apply(sshalgorithms.Defaults(), map[string]any{"ciphers-append": []any{"aes128-ctr"}})
	d = prepared(t, f, request(legacy, "generic", ""))
	if r := d.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 5 * time.Second}); r.Err != nil {
		t.Fatalf("profile: %+v", r)
	}

	for _, def := range platform.Builtins() {
		if err := Admits(def); err != nil {
			t.Fatalf("%s: %v", def.Name, err)
		}
	}
	err = Admits(platform.Definition{Name: "c9300", Driver: "cisco_ios", Base: "unqualified"})
	if errorcodes.Of(err) != "native_platform_not_qualified" || !strings.Contains(err.Error(), `platform "c9300" (base driver unqualified)`) {
		t.Fatalf("refusal: %v", err)
	}
}

// TestProviderOffersKeys: the credential's keys are offered first, in
// order, read at authentication: a file that no longer parses is passed
// over, a key the device does not hold is refused and the next tried; with
// keys and no password no password method is offered, so a refused key is
// authentication_failed; beside a password the password still logs in.
func TestProviderOffersKeys(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	held, heldFP := sshkeytest.Ed25519(t, "")
	other, _ := sshkeytest.Ed25519(t, "")
	locked, _ := sshkeytest.Ed25519(t, "lab passphrase")
	heldPath, otherPath, lockedPath := write("held", held), write("other", other), write("locked", locked)
	srv := startFake(t, fakedevice.Options{Enable: "en", AuthorizedKeys: sshkeytest.Authorized(t, held)})
	f, _ := factory(t, "accept-new")
	keyOnly := func(keys ...string) platform.OpenRequest {
		req := request(srv, "cisco_ios", "en")
		req.Password, req.Keys = nil, keys
		return req
	}
	d := prepared(t, f, keyOnly(lockedPath, otherPath, heldPath))
	if r := d.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 5 * time.Second}); r.Err != nil {
		t.Fatalf("show clock: %+v", r)
	}
	d.Close()
	if got := srv.KeyLogins(); len(got) != 1 || got[0] != heldFP {
		t.Fatalf("key logins %v, want %s", got, heldFP)
	}
	if m := d.(platform.AuthReporter).AuthMethod(); m != "publickey" {
		t.Fatalf("method %q", m)
	}
	refused, err := f.Open(context.Background(), keyOnly(otherPath))
	if err != nil {
		t.Fatal(err)
	}
	defer refused.Close()
	if err := refused.Prepare(context.Background()); errorcodes.Of(err) != "authentication_failed" {
		t.Fatalf("a refused key and no password: %v", err)
	}
	both := request(srv, "cisco_ios", "en")
	both.Keys = []string{otherPath}
	bd := prepared(t, f, both)
	if got := srv.KeyLogins(); len(got) != 1 {
		t.Fatalf("the password login took a key: %v", got)
	}
	if m := bd.(platform.AuthReporter).AuthMethod(); m != "keyboard-interactive" {
		t.Fatalf("after the refused key: method %q", m)
	}
	bd.Close()
}

// TestProviderKeyboardInteractivePrompts: each keyboard-interactive prompt
// is answered by what it asks for (askpass.Field), as the system
// transport's askpass broker answers the first: the password, the
// username, or an empty answer for a prompt no list matches; the password
// never goes to a prompt that does not ask for it.
func TestProviderKeyboardInteractivePrompts(t *testing.T) {
	for _, prompts := range [][]fakedevice.KbdPrompt{
		{{Prompt: "Username: ", Want: "username"}, {Prompt: "Password: ", Want: "password"}},
		{{Prompt: "Enter PASSCODE: ", Want: "password"}},
		{{Prompt: "Token: ", Want: "password"}},
		{{Prompt: "PIN: ", Want: "password"}},
		{{Prompt: "Response: ", Want: "empty"}},
	} {
		srv := startFake(t, fakedevice.Options{Enable: "en", KbdPrompts: prompts})
		f, _ := factory(t, "accept-new")
		d := prepared(t, f, request(srv, "cisco_ios", "en"))
		var want []string
		for _, p := range prompts {
			want = append(want, fmt.Sprintf("%q %s", p.Prompt, p.Want))
		}
		if got := srv.KbdAnswers(); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("answers %q, want %q", got, want)
		}
		if m := d.(platform.AuthReporter).AuthMethod(); m != "keyboard-interactive" {
			t.Errorf("%q: method %q", want, m)
		}
		d.Close()
	}
}

// TestProviderChangePromptEndsTheHandshake: a change prompt, alone or after
// one karvi would answer, ends the handshake with
// authentication_change_requested: the device gets no answer in the round,
// the password method is not tried after it (the password is never
// resolved), and no session opens.
func TestProviderChangePromptEndsTheHandshake(t *testing.T) {
	for _, prompts := range [][]fakedevice.KbdPrompt{
		{{Prompt: "Enter new PIN: ", Want: "empty"}},
		{{Prompt: "Username: ", Want: "username"}, {Prompt: "Confirm PIN: ", Want: "empty"}},
		{{Prompt: "Password: ", Want: "password"}, {Prompt: "New password: ", Want: "empty"}},
	} {
		srv := startFake(t, fakedevice.Options{Enable: "en", KbdPrompts: prompts})
		f, _ := factory(t, "accept-new")
		req := request(srv, "cisco_ios", "en")
		resolved := 0
		req.Password = func(f func([]byte) error) error { resolved++; return f([]byte("pw")) }
		d, err := f.Open(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		last := prompts[len(prompts)-1].Prompt
		want := fmt.Sprintf("authentication_change_requested: the device asks to change a secret: %q", last)
		if err := d.Prepare(context.Background()); errorcodes.Message(err) != want {
			t.Errorf("%q: %v, want %s", last, err, want)
		}
		d.Close()
		if got := srv.KbdAnswers(); len(got) != 0 {
			t.Errorf("%q: the device received %q, want nothing", last, got)
		}
		if resolved != 0 {
			t.Errorf("%q: the password was resolved %d times, want none", last, resolved)
		}
		if srv.Sessions() != 0 {
			t.Errorf("%q: sessions=%d", last, srv.Sessions())
		}
	}
}

// TestProviderPasswordMethods: keyboard-interactive (the fake's "Password: "
// prompt) is tried before the password method, both answered with the
// password; a server that refuses
// keyboard-interactive takes the password method. Each method is offered
// only where its setting leaves it on: with keyboard-interactive off the
// password method logs in, and with no method left the device opens no
// session and the failure is authentication_failed.
func TestProviderPasswordMethods(t *testing.T) {
	for _, tc := range []struct {
		noKbd bool
		sets  []string
		want  string
	}{
		{false, nil, "keyboard-interactive"},
		{true, nil, "password"},
		{false, []string{"ssh.keyboard-interactive-authentication=false"}, "password"},
		{false, []string{"ssh.password-authentication=false"}, "keyboard-interactive"},
		{true, []string{"ssh.password-authentication=false"}, ""},
		{false, []string{"ssh.keyboard-interactive-authentication=false", "ssh.password-authentication=false"}, ""},
	} {
		srv := startFake(t, fakedevice.Options{Enable: "en", NoKeyboardInteractive: tc.noKbd})
		f, _ := factory(t, "accept-new", tc.sets...)
		if tc.want == "" {
			d, err := f.Open(context.Background(), request(srv, "cisco_ios", "en"))
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Prepare(context.Background()); errorcodes.Of(err) != "authentication_failed" {
				t.Errorf("no keyboard-interactive %t, %v: %v, want authentication_failed", tc.noKbd, tc.sets, err)
			}
			d.Close()
			if srv.Sessions() != 0 {
				t.Errorf("no keyboard-interactive %t, %v: sessions=%d", tc.noKbd, tc.sets, srv.Sessions())
			}
			continue
		}
		d := prepared(t, f, request(srv, "cisco_ios", "en"))
		if m := d.(platform.AuthReporter).AuthMethod(); m != tc.want {
			t.Errorf("no keyboard-interactive %t, %v: method %q, want %q", tc.noKbd, tc.sets, m, tc.want)
		}
		d.Close()
	}
}
