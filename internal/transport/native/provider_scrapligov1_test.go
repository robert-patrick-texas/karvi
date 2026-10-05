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

func startFake(t *testing.T, opts fakedevice.Options) *fakedevice.Server {
	t.Helper()
	srv, err := fakedevice.Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

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
	return Factory{Implementation: "scrapligo-v1", Config: cfg, Home: home, Warn: func(m string) { t.Logf("warn: %s", m) }}, known
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

// TestProviderRunsTheDeviceSession: scrapligo-v1 is
// karvi's connection under karvi's device session: one connection and one
// shell, no extra returns, the definition's paging order, device errors
// from the platform's patterns, connection_reused, and a timeout that ends
// the session with nothing more sent.
func TestProviderRunsTheDeviceSession(t *testing.T) {
	srv := startFake(t, fakedevice.Options{Enable: "en", Delay: map[string]time.Duration{"show slow": 3 * time.Second}})
	f, known := factory(t, "accept-new")
	d := prepared(t, f, request(srv, "cisco_iosxe", "en"))
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

// TestProviderEnableOptional: the enable secret is optional on
// scrapligo-v1.
func TestProviderEnableOptional(t *testing.T) {
	f, _ := factory(t, "insecure")
	srv := startFake(t, fakedevice.Options{StartPrivileged: true, Enable: "en"})
	d := prepared(t, f, request(srv, "cisco_iosxe", ""))
	if r := d.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 5 * time.Second}); r.Err != nil {
		t.Fatalf("privilege 15 at login: %+v", r)
	}
	d.Close()
	if got := strings.Join(srv.Lines(), "|"); !strings.HasPrefix(got, "terminal length 0|") {
		t.Fatalf("privilege 15 at login, device saw %q", got)
	}

	asks := startFake(t, fakedevice.Options{Enable: "en"})
	d, err := f.Open(context.Background(), request(asks, "cisco_iosxe", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Prepare(context.Background()); errorcodes.Of(err) != "privilege_failed" || !strings.Contains(err.Error(), "the credential has none") {
		t.Fatalf("a secret asked, none resolved: %v", err)
	}
}

// TestProviderAlgorithmsAndAdmission covers the device's algorithm lists on
// scrapligo-v1 (a legacy device through a profile) and the admission table.
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
		if err := Admits("scrapligo-v1", def); err != nil {
			t.Fatalf("%s: %v", def.Name, err)
		}
	}
	err = Admits("scrapligo-v1", platform.Definition{Name: "c9300", Driver: "cisco_iosxe", Base: "scrapligo_v2_only"})
	if errorcodes.Of(err) != "native_platform_not_qualified" || !strings.Contains(err.Error(), `platform "c9300" (base driver scrapligo_v2_only)`) {
		t.Fatalf("refusal: %v", err)
	}
	if err := Admits("not-compiled-in", platform.Definition{Base: "generic"}); err != nil {
		t.Fatalf("unknown implementation: %v", err)
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
		req := request(srv, "cisco_iosxe", "en")
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
	both := request(srv, "cisco_iosxe", "en")
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

// TestProviderPasswordMethods: keyboard-interactive is tried before the
// password method, both answered with the password; a server that refuses
// keyboard-interactive takes the password method.
func TestProviderPasswordMethods(t *testing.T) {
	for _, tc := range []struct {
		noKbd bool
		want  string
	}{{false, "keyboard-interactive"}, {true, "password"}} {
		srv := startFake(t, fakedevice.Options{Enable: "en", NoKeyboardInteractive: tc.noKbd})
		f, _ := factory(t, "accept-new")
		d := prepared(t, f, request(srv, "cisco_iosxe", "en"))
		if m := d.(platform.AuthReporter).AuthMethod(); m != tc.want {
			t.Errorf("no keyboard-interactive %t: method %q, want %q", tc.noKbd, m, tc.want)
		}
		d.Close()
	}
}
