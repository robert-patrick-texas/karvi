package scrapligov1

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
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

func trustStore(t *testing.T, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "karvi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// entry is a known_hosts line for the fake under its host-key identity.
func entry(srv *fakedevice.Server, authorizedKey string) string {
	return fmt.Sprintf("[fake-iosxe]:%d %s\n", srv.Port(), authorizedKey)
}

func dialRequest(srv *fakedevice.Server, mode hostkey.Mode, file string) DialRequest {
	return DialRequest{
		Host: "fake-iosxe", Address: "127.0.0.1", Port: srv.Port(), Username: "netops",
		Password:       func(f func([]byte) error) error { return f([]byte("pw")) },
		Policy:         hostkey.Policy{Mode: mode, KnownHostsFile: file},
		ConnectTimeout: 5 * time.Second, HandshakeTimeout: 5 * time.Second,
		Term: "xterm",
	}
}

func openSession(t *testing.T, s devsession.Stream, name string) *devsession.Session {
	t.Helper()
	def, _ := platform.Builtin(name)
	session, err := devsession.Open(context.Background(), s, devsession.Options{
		Definition:   def,
		EnableSecret: func(f func([]byte) error) error { return f([]byte("en")) },
		LoginTimeout: 5 * time.Second, PromptTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("session open: %v", err)
	}
	if err := session.Prepare(context.Background()); err != nil {
		t.Fatalf("session prepare: %v", err)
	}
	return session
}

func TestDialSessionOneConnectionOneShell(t *testing.T) {
	srv := startFake(t, fakedevice.Options{Enable: "en"})
	s, err := Dial(context.Background(), dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent")))
	if err != nil {
		t.Fatal(err)
	}
	session := openSession(t, s, "cisco_iosxe")
	clock := session.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 5 * time.Second})
	if clock.Err != nil || string(clock.Output) != "*10:00:00.000 UTC Tue Sep 15 2026\n" || clock.Prompt != "Router#" {
		t.Fatalf("show clock: %+v", clock)
	}
	bogus := session.Execute(context.Background(), platform.Command{Text: "show bogus", Timeout: 5 * time.Second})
	if bogus.ErrorCode != "device_command_error" || !session.Usable() {
		t.Fatalf("show bogus: %+v", bogus)
	}
	started := time.Now()
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("close after exit took %s", elapsed)
	}
	time.Sleep(100 * time.Millisecond)
	want := []string{"enable", "<secret>", "terminal length 0", "terminal width 512", "show clock", "show bogus", "exit"}
	if got := srv.Lines(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", got, want)
	}
	if srv.Connections() != 1 || srv.Sessions() != 1 {
		t.Fatalf("connections=%d sessions=%d", srv.Connections(), srv.Sessions())
	}
	if ptys := srv.PTYRequests(); len(ptys) != 1 || ptys[0] != (fakedevice.PTYRequest{Term: "xterm", ModeBytes: 1}) {
		t.Fatalf("pty requests %+v", ptys)
	}
}

func TestDialAbortAfterTimeoutReturnsAtOnce(t *testing.T) {
	srv := startFake(t, fakedevice.Options{Delay: map[string]time.Duration{"show slow": 10 * time.Second}})
	s, err := Dial(context.Background(), dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent")))
	if err != nil {
		t.Fatal(err)
	}
	session := openSession(t, s, "cisco_iosxe")
	started := time.Now()
	r := session.Execute(context.Background(), platform.Command{Text: "show slow", Timeout: 300 * time.Millisecond})
	if r.ErrorCode != "command_timeout" || session.Usable() {
		t.Fatalf("show slow: %+v usable=%t", r, session.Usable())
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("timeout and close took %s", elapsed)
	}
}

// TestKeepalives: requests on the
// interval that the device answers keep a slow command's session; a device
// gone silent ends the read under session_keepalive_timeout after the count
// of unanswered ones, well before the command timeout; interval 0 sends none.
func TestKeepalives(t *testing.T) {
	dial := func(t *testing.T, srv *fakedevice.Server, interval time.Duration) *devsession.Session {
		t.Helper()
		req := dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent"))
		req.KeepaliveInterval, req.KeepaliveCountMax = interval, 2
		s, err := Dial(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		session := openSession(t, s, "cisco_iosxe")
		t.Cleanup(func() { session.Close() })
		return session
	}
	t.Run("answered", func(t *testing.T) {
		srv := startFake(t, fakedevice.Options{Enable: "en", Delay: map[string]time.Duration{"show slow": 1500 * time.Millisecond}})
		session := dial(t, srv, 200*time.Millisecond)
		r := session.Execute(context.Background(), platform.Command{Text: "show slow", Timeout: 5 * time.Second})
		if r.Err != nil || !session.Usable() {
			t.Fatalf("show slow: %+v", r)
		}
		if n := srv.Keepalives(); n < 4 {
			t.Fatalf("the device received %d keepalives in 1.5s at 200ms", n)
		}
	})
	t.Run("unanswered", func(t *testing.T) {
		srv := startFake(t, fakedevice.Options{Enable: "en"})
		session := dial(t, srv, 200*time.Millisecond)
		started := time.Now()
		r := session.Execute(context.Background(), platform.Command{Text: "show mute", Timeout: 10 * time.Second})
		if r.ErrorCode != "session_keepalive_timeout" || r.ErrorCategory != "connection" || !r.Retryable || session.Usable() {
			t.Fatalf("show mute: %+v usable=%t", r, session.Usable())
		}
		if !strings.Contains(r.Err.Error(), "none of 2 keepalives sent every 200ms") {
			t.Fatalf("message: %v", r.Err)
		}
		// (count + 1) intervals from the last output, as OpenSSH counts.
		if elapsed := time.Since(started); elapsed < 550*time.Millisecond || elapsed > 1500*time.Millisecond {
			t.Fatalf("the silent device held the command %s, want about 600ms", elapsed)
		}
	})
	t.Run("off", func(t *testing.T) {
		srv := startFake(t, fakedevice.Options{Enable: "en", Delay: map[string]time.Duration{"show slow": 500 * time.Millisecond}})
		session := dial(t, srv, 0)
		if r := session.Execute(context.Background(), platform.Command{Text: "show slow", Timeout: 5 * time.Second}); r.Err != nil {
			t.Fatalf("show slow: %+v", r)
		}
		if n := srv.Keepalives(); n != 0 {
			t.Fatalf("the device received %d keepalives with the interval 0", n)
		}
	})
}

func TestDialHostKeyPolicies(t *testing.T) {
	first := startFake(t, fakedevice.Options{})
	file := trustStore(t, "")

	// accept-new: enrolled on the first open, matched on the next; the
	// enrollment is said through HostKeyNotice, once, with OpenSSH's label.
	var notices []platform.HostKeyNotice
	req := dialRequest(first, hostkey.AcceptNew, file)
	req.HostKeyNotice = func(n platform.HostKeyNotice) { notices = append(notices, n) }
	s, err := Dial(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	s.(*stream).Abort()
	content, _ := os.ReadFile(file)
	if got := string(content); got != strings.TrimSuffix(entry(first, first.HostKeys()[0]), "\n")+" karvi-auto-enrolled\n" {
		t.Fatalf("store after the first open: %q", got)
	}
	if len(notices) != 1 || notices[0].Code != platform.HostKeyEnrolled || notices[0].Label != "ED25519" {
		t.Fatalf("notices %+v", notices)
	}
	s, err = Dial(context.Background(), req)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	if len(notices) != 1 {
		t.Fatalf("the known key said enrolled again: %+v", notices)
	}
	s.(*stream).Abort()
	if first.Connections() != 2 {
		t.Fatalf("connections=%d, want one per open", first.Connections())
	}

	// Another key under the same identity.
	changed := startFake(t, fakedevice.Options{})
	store := trustStore(t, entry(changed, first.HostKeys()[0]))
	for _, mode := range []hostkey.Mode{hostkey.AcceptNew, hostkey.Secure} {
		_, err := Dial(context.Background(), dialRequest(changed, mode, store))
		if errorcodes.Of(err) != "host_key_changed" || !strings.Contains(err.Error(), "enrolled=ssh-ed25519 SHA256:") || !strings.Contains(err.Error(), "presented=ssh-ed25519 SHA256:") {
			t.Fatalf("%s, changed key: %v", mode, err)
		}
	}
	if changed.Sessions() != 0 {
		t.Fatalf("a shell started after a changed key")
	}
	// insecure: accepted, nothing stored; the difference is a
	// host_key_mismatch_accepted notice with both fingerprints.
	notices = nil
	insecure := dialRequest(changed, hostkey.Insecure, store)
	insecure.HostKeyNotice = func(n platform.HostKeyNotice) { notices = append(notices, n) }
	s, err = Dial(context.Background(), insecure)
	if err != nil {
		t.Fatalf("insecure, changed key: %v", err)
	}
	s.(*stream).Abort()
	if len(notices) != 1 || notices[0].Code != platform.HostKeyMismatchAccepted ||
		len(notices[0].Enrolled) != 1 || len(notices[0].Presented) != 1 || notices[0].Enrolled[0] == notices[0].Presented[0] {
		t.Fatalf("insecure notices %+v", notices)
	}
	if after, _ := os.ReadFile(store); string(after) != entry(changed, first.HostKeys()[0]) {
		t.Fatalf("store changed: %q", after)
	}

	_, err = Dial(context.Background(), dialRequest(changed, hostkey.Secure, trustStore(t, "")))
	if errorcodes.Of(err) != "host_key_not_enrolled" {
		t.Fatalf("secure, unknown host: %v", err)
	}
}

func TestDialHostKeyAlgorithmChoice(t *testing.T) {
	srv := startFake(t, fakedevice.Options{ExtraHostKeys: []string{"ecdsa256", "rsa"}})
	keys := srv.HostKeys() // ed25519, ecdsa256, rsa

	// No entry: the strongest key is negotiated and enrolled.
	file := trustStore(t, "")
	s, err := Dial(context.Background(), dialRequest(srv, hostkey.AcceptNew, file))
	if err != nil {
		t.Fatal(err)
	}
	s.(*stream).Abort()
	if content, _ := os.ReadFile(file); !strings.Contains(string(content), " ssh-ed25519 ") {
		t.Fatalf("enrolled %q", content)
	}
	// An entry of another type: offering only that type matches it.
	for _, key := range keys[1:] {
		s, err := Dial(context.Background(), dialRequest(srv, hostkey.Secure, trustStore(t, entry(srv, key))))
		if err != nil {
			t.Fatalf("secure with %s enrolled: %v", strings.Fields(key)[0], err)
		}
		s.(*stream).Abort()
	}

	// A legacy device signing only with ssh-rsa is reachable and enrolled.
	legacy := startFake(t, fakedevice.Options{RSASHA1Only: true})
	file = trustStore(t, "")
	s, err = Dial(context.Background(), dialRequest(legacy, hostkey.AcceptNew, file))
	if err != nil {
		t.Fatalf("legacy device: %v", err)
	}
	session := openSession(t, s, "cisco_iosxe")
	if r := session.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 5 * time.Second}); r.Err != nil {
		t.Fatalf("legacy show clock: %+v", r)
	}
	session.Close()
	if content, _ := os.ReadFile(file); !strings.Contains(string(content), " ssh-rsa ") {
		t.Fatalf("legacy enrolled %q", content)
	}

	// The store holds a key type the device no longer offers.
	onlyEd25519 := startFake(t, fakedevice.Options{})
	_, err = Dial(context.Background(), dialRequest(onlyEd25519, hostkey.AcceptNew, trustStore(t, entry(onlyEd25519, keys[2]))))
	if errorcodes.Of(err) != "host_key_changed" || !strings.Contains(err.Error(), "enrolled=ssh-rsa SHA256:") || !strings.Contains(err.Error(), "offered: ssh-ed25519") {
		t.Fatalf("enrolled type not offered: %v", err)
	}
}

func TestDialOpenFailures(t *testing.T) {
	srv := startFake(t, fakedevice.Options{Password: "other"})
	absent := filepath.Join(t.TempDir(), "absent")

	_, err := Dial(context.Background(), dialRequest(srv, hostkey.Insecure, absent))
	if errorcodes.Of(err) != "authentication_failed" || strings.Contains(err.Error(), "pw") {
		t.Fatalf("wrong password: %v", err)
	}

	refused := dialRequest(srv, hostkey.Insecure, absent)
	refused.Address = "127.0.0.2"
	_, err = Dial(context.Background(), refused)
	if errorcodes.Of(err) != "native_session_open_failed" || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("refused: %v", err)
	}

	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	go func() {
		for {
			conn, err := silent.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { conn.Close() })
		}
	}()
	hung := dialRequest(srv, hostkey.Insecure, absent)
	hung.Port = silent.Addr().(*net.TCPAddr).Port
	hung.HandshakeTimeout = 500 * time.Millisecond
	started := time.Now()
	_, err = Dial(context.Background(), hung)
	if errorcodes.Of(err) != "native_session_open_failed" || !strings.Contains(err.Error(), "did not complete within 500ms") || time.Since(started) > 2*time.Second {
		t.Fatalf("silent server: %v after %s", err, time.Since(started))
	}

	hung.HandshakeTimeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started = time.Now()
	_, err = Dial(ctx, hung)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatalf("cancelled handshake: %v after %s", err, time.Since(started))
	}
}

func TestFakeRefusesExec(t *testing.T) {
	srv := startFake(t, fakedevice.Options{})
	s, err := Dial(context.Background(), dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent")))
	if err != nil {
		t.Fatal(err)
	}
	defer s.(*stream).Abort()
	client := s.(*stream).t.Impl.(*connection).client
	session, err := client.NewSession()
	if err != nil {
		t.Skipf("second channel refused before exec: %v", err)
	}
	defer session.Close()
	if err := session.Start("show version"); err == nil {
		t.Fatal("the fake accepted an exec request")
	}
	if lines := srv.Lines(); len(lines) != 1 || lines[0] != "exec: show version" {
		t.Fatalf("lines %q", lines)
	}
}

func TestXCryptoImplements(t *testing.T) {
	for name, want := range map[string]bool{"mlkem768x25519-sha256": false, "sntrup761x25519-sha512@openssh.com": false, "curve25519-sha256": true, "diffie-hellman-group1-sha1": true} {
		if got := xcryptoImplements(sshalgorithms.Kex, name); got != want {
			t.Errorf("kex %s: %t", name, got)
		}
	}
	for name, want := range map[string]bool{"aes256-cbc": false, "aes192-cbc": false, "aes128-cbc": true, "aes256-gcm@openssh.com": true, "chacha20-poly1305@openssh.com": true} {
		if got := xcryptoImplements(sshalgorithms.Ciphers, name); got != want {
			t.Errorf("cipher %s: %t", name, got)
		}
	}
	if !xcryptoImplements(sshalgorithms.MACs, "hmac-sha2-512-etm@openssh.com") || xcryptoImplements(sshalgorithms.MACs, "umac-128@openssh.com") {
		t.Error("macs")
	}
	offered, dropped, err := offer(nil)
	if err != nil || dropped || strings.Join(offered[sshalgorithms.Ciphers], ",") != "aes256-gcm@openssh.com,chacha20-poly1305@openssh.com,aes256-ctr" || strings.Join(offered[sshalgorithms.Kex], ",")[:17] != "curve25519-sha256" {
		t.Fatalf("defaults offered %v dropped=%t %v", offered, dropped, err)
	}
	offered, dropped, err = offer(sshalgorithms.Apply(sshalgorithms.Defaults(), map[string]any{"ciphers-append": []any{"aes128-cbc"}}))
	if err != nil || !dropped || strings.Join(offered[sshalgorithms.MACs], ",") != "hmac-sha2-512,hmac-sha2-256,hmac-sha1" {
		t.Fatalf("aes128-cbc offered %v dropped=%t %v", offered, dropped, err)
	}
	_, _, err = offer(sshalgorithms.Apply(sshalgorithms.Defaults(), map[string]any{"ciphers": []any{"aes256-cbc"}}))
	if errorcodes.Of(err) != "ssh_algorithms_unavailable" {
		t.Fatalf("aes256-cbc only: %v", err)
	}
}

func TestDialAlgorithmLists(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent")
	profile := func(p map[string]any) sshalgorithms.Lists { return sshalgorithms.Apply(sshalgorithms.Defaults(), p) }
	// ssh-algorithms.source "transport": the host-key list alone, x/crypto's
	// own key exchanges, ciphers, and MACs.
	transport := sshalgorithms.Lists{sshalgorithms.HostKey: sshalgorithms.Defaults()[sshalgorithms.HostKey]}
	cases := []struct {
		name   string
		device fakedevice.Options
		lists  sshalgorithms.Lists
		code   string
		detail string
	}{
		{"legacy device, defaults", fakedevice.Options{RSASHA1Only: true, KeyExchanges: []string{"diffie-hellman-group14-sha1"}, Ciphers: []string{"aes128-ctr"}, MACs: []string{"hmac-sha1"}}, nil, "ssh_algorithm_negotiation_failed", "no cipher algorithm in common: karvi offered aes256-gcm@openssh.com,chacha20-poly1305@openssh.com,aes256-ctr; the device offered aes128-ctr"},
		{"legacy device, a profile appending aes128-ctr", fakedevice.Options{RSASHA1Only: true, KeyExchanges: []string{"diffie-hellman-group14-sha1"}, Ciphers: []string{"aes128-ctr"}, MACs: []string{"hmac-sha1"}}, profile(map[string]any{"ciphers-append": []any{"aes128-ctr"}}), "", ""},
		{"group1-only device, defaults", fakedevice.Options{KeyExchanges: []string{"diffie-hellman-group1-sha1"}}, nil, "ssh_algorithm_negotiation_failed", "no key exchange algorithm in common"},
		{"group1-only device, a profile appending group1", fakedevice.Options{KeyExchanges: []string{"diffie-hellman-group1-sha1"}}, profile(map[string]any{"kex-append": []any{"diffie-hellman-group1-sha1"}}), "", ""},
		{"aes128-cbc device with ETM MACs, a profile appending aes128-cbc", fakedevice.Options{Ciphers: []string{"aes128-cbc"}, MACs: []string{"hmac-sha2-512-etm@openssh.com", "hmac-sha2-512"}}, profile(map[string]any{"ciphers-append": []any{"aes128-cbc"}}), "", ""},
		{"a MAC the device lacks", fakedevice.Options{MACs: []string{"hmac-sha1-96"}, Ciphers: []string{"aes128-ctr"}}, profile(map[string]any{"ciphers-append": []any{"aes128-ctr"}}), "ssh_algorithm_negotiation_failed", "no MAC algorithm in common"},
		{"legacy device, the transport's lists", fakedevice.Options{RSASHA1Only: true, KeyExchanges: []string{"diffie-hellman-group14-sha1"}, Ciphers: []string{"aes128-ctr"}, MACs: []string{"hmac-sha1"}}, transport, "", ""},
		{"a device with only hmac-sha1-96, the transport's lists", fakedevice.Options{MACs: []string{"hmac-sha1-96"}, Ciphers: []string{"aes128-ctr"}}, transport, "", ""},
		{"an aes128-cbc device, the transport's lists", fakedevice.Options{Ciphers: []string{"aes128-cbc"}}, transport, "ssh_algorithm_negotiation_failed", "no cipher algorithm in common: native offered aes128-gcm@openssh.com,aes256-gcm@openssh.com,chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr; the device offered aes128-cbc"},
	}
	for _, c := range cases {
		srv := startFake(t, c.device)
		req := dialRequest(srv, hostkey.Insecure, absent)
		req.Algorithms = c.lists
		s, err := Dial(context.Background(), req)
		if c.code == "" {
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			session := openSession(t, s, "generic")
			if r := session.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 5 * time.Second}); r.Err != nil {
				t.Fatalf("%s: show clock %+v", c.name, r)
			}
			session.Close()
			continue
		}
		if errorcodes.Of(err) != c.code || !strings.Contains(err.Error(), c.detail) {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
}
