package redis

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/redis/redistest"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/storetls/tlstest"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

// request is the operator netops's request, its variables env.
func request(env map[string]string) credentials.ResolveRequest {
	return credentials.ResolveRequest{Operator: credentials.Operator{Username: "netops"}, Policy: "default", LookupEnv: func(name string) (string, bool) { v, ok := env[name]; return v, ok }}
}

// backend is r as the resolver builds it with its defaults, over plain TCP.
func backend(addr string) *Backend {
	return &Backend{BackendName: "r", Address: addr, KeyTemplate: "karvi:credential:" + credentials.OperatorMacro, TLSVerify: true, Timeout: 2 * time.Second}
}

func fields(t *testing.T, r credentials.BackendResult) [3]string {
	t.Helper()
	if r.Outcome != credentials.Success {
		t.Fatalf("%s: %s %s", r.Outcome, r.ErrorCode, r.Message)
	}
	m := r.Credential.Material.(*secrets.Material)
	var out [3]string
	for i, v := range []*secrets.Value{m.Username, m.Password, m.EnablePassword} {
		_ = v.WithBytes(func(b []byte) error { out[i] = string(b); return nil })
	}
	m.Destroy()
	return out
}

// TestLoginVariables is EXAMPLES chapter 56, issue 1: auth-password-env, and
// auth-username-env beside it, are read through the request's lookup alone,
// before any connection; unset or empty is credential_env_missing naming the
// backend, the key, and the variable; "" names none and no AUTH is sent.
func TestLoginVariables(t *testing.T) {
	t.Setenv("REDIS_PASS", "from-the-process")
	s := redistest.Start(t, &redistest.Server{Password: "rpass", Hashes: map[string]map[string]string{"karvi:credential:netops": {"username": "netops", "password": "S3cret"}}}, nil)
	for _, tc := range []struct {
		user, pass string
		env        map[string]string
		says       string
	}{
		{"", "REDIS_PASS", map[string]string{}, "credential backend r: auth-password-env: REDIS_PASS not set"},
		{"", "REDIS_PASS", map[string]string{"REDIS_PASS": ""}, "credential backend r: auth-password-env: REDIS_PASS empty"},
		{"REDIS_USER", "REDIS_PASS", map[string]string{"REDIS_PASS": "rpass"}, "credential backend r: auth-username-env: REDIS_USER not set"},
	} {
		b := backend(s.Addr)
		b.AuthUsernameEnv, b.AuthPasswordEnv = tc.user, tc.pass
		r := b.Resolve(context.Background(), request(tc.env))
		if r.ErrorCode != "credential_env_missing" || r.Message != tc.says {
			t.Errorf("%v: %s %q, want credential_env_missing %q", tc.env, r.ErrorCode, r.Message, tc.says)
		}
	}
	if n := s.Conns(); n != 0 {
		t.Errorf("a missing variable reached the store: %d connections", n)
	}
	for _, tc := range []struct {
		user, pass string
		env        map[string]string
		auth       string
	}{
		{"", "", map[string]string{}, ""},
		{"", "REDIS_PASS", map[string]string{"REDIS_PASS": "rpass"}, "AUTH rpass"},
		{"REDIS_USER", "REDIS_PASS", map[string]string{"REDIS_USER": "karvi", "REDIS_PASS": "rpass"}, "AUTH karvi rpass"},
	} {
		before := len(s.Commands())
		b := backend(s.Addr)
		b.AuthUsernameEnv, b.AuthPasswordEnv = tc.user, tc.pass
		if got := fields(t, b.Resolve(context.Background(), request(tc.env))); got[1] != "S3cret" {
			t.Errorf("%v: %q", tc.env, got)
		}
		auth := ""
		for _, c := range s.Commands()[before:] {
			if c[0] == "AUTH" {
				auth = strings.Join(c, " ")
			}
		}
		if auth != tc.auth {
			t.Errorf("%v: sent %q, want %q", tc.env, auth, tc.auth)
		}
	}
}

// TestRecordReferences is EXAMPLES chapter 56, issue 2: a record's field
// whose value, trimmed, begins with env: in any case is the value of the
// variable it names, read through the request's lookup; unset or empty is
// credential_env_missing naming the key, the field, and the variable; a
// reference to no variable name is redis_malformed, never quoting the field;
// any other value is a literal, a variable's name included.
func TestRecordReferences(t *testing.T) {
	hashes := map[string]map[string]string{
		"karvi:credential:netops": {"username": "USER", "password": " ENV:NETPASS_NYC ", "enable_password": "env:NETENABLE"},
		"karvi:credential:unset":  {"username": "netops", "password": "env:NETPASS_UNSET"},
		"karvi:credential:bad":    {"username": "netops", "password": "env:p$ss-word"},
	}
	s := redistest.Start(t, &redistest.Server{Hashes: hashes}, nil)
	env := map[string]string{"USER": "not-read", "NETPASS_NYC": "S3cret", "NETENABLE": "en"}
	b := backend(s.Addr)
	if got := fields(t, b.Resolve(context.Background(), request(env))); got != [3]string{"USER", "S3cret", "en"} {
		t.Errorf("references: %q", got)
	}
	req := request(env)
	req.Operator.Username = "unset"
	if r := b.Resolve(context.Background(), req); r.ErrorCode != "credential_env_missing" || r.Message != "credential backend r: karvi:credential:unset password: NETPASS_UNSET not set" {
		t.Errorf("unset: %s %q", r.ErrorCode, r.Message)
	}
	req.Operator.Username = "bad"
	if r := b.Resolve(context.Background(), req); r.ErrorCode != "redis_malformed" || !strings.HasPrefix(r.Message, "credential backend r: karvi:credential:bad password: env: not followed by a variable name") || strings.Contains(r.Message, "p$ss") {
		t.Errorf("bad: %s %q", r.ErrorCode, r.Message)
	}
}

// TestSourceIsTheKey is EXAMPLES chapter 56, issues 1, 5, and 6: the key is
// the key-template with %operator% filled as text, %s left as written; the
// credential's source is the key read; Resolve leaves its backend as built.
func TestSourceIsTheKey(t *testing.T) {
	s := redistest.Start(t, &redistest.Server{Hashes: map[string]map[string]string{
		"karvi:credential:netops": {"username": "netops", "password": "a"},
		"karvi:shared":            {"username": "svc", "password": "b"},
		"karvi:%s":                {"username": "pct", "password": "c"},
	}}, nil)
	for template, source := range map[string]string{"karvi:credential:%operator%": "karvi:credential:netops", "karvi:shared": "karvi:shared", "karvi:%s": "karvi:%s"} {
		b := backend(s.Addr)
		b.KeyTemplate = template
		built := *b
		r := b.Resolve(context.Background(), request(nil))
		if r.Outcome != credentials.Success || r.Credential.MatchedOn.Source != source {
			t.Errorf("%s: %s %s, source %q", template, r.Outcome, r.Message, r.Credential.MatchedOn.Source)
		}
		r.Credential.Material.Destroy()
		if *b != built {
			t.Errorf("%s: Resolve changed its backend: %+v", template, *b)
		}
	}
	b := backend(s.Addr)
	b.KeyTemplate = "karvi:none"
	if r := b.Resolve(context.Background(), request(nil)); r.Outcome != credentials.NotFound {
		t.Errorf("no record: %s", r.Outcome)
	}
}

// TestTLS is EXAMPLES chapter 56, issue 16: under tls-verify the server is
// verified against ca-file alone, a server another CA signed refused; with
// tls-verify = false the same server is read; a client pair is presented
// with its chain.
func TestTLS(t *testing.T) {
	site, other := tlstest.New(t, "site CA"), tlstest.New(t, "other CA")
	var chain int
	srv := &tls.Config{Certificates: []tls.Certificate{site.Server}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: site.Pool, VerifyConnection: func(cs tls.ConnectionState) error {
		chain = len(cs.PeerCertificates)
		return nil
	}}
	s := redistest.Start(t, &redistest.Server{Hashes: map[string]map[string]string{"karvi:credential:netops": {"username": "netops", "password": "a"}}}, srv)
	resolve := func(ca string, verify bool, client bool) credentials.BackendResult {
		b := backend(s.Addr)
		b.TLS, b.TLSVerify, b.CAFile = true, verify, ca
		if client {
			b.ClientCertFile, b.ClientKeyFile = site.ClientCert, site.ClientKey
		}
		return b.Resolve(context.Background(), request(nil))
	}
	if r := resolve(site.CA, true, false); r.Outcome != credentials.Success {
		t.Errorf("ca-file the site CA: %s %s", r.ErrorCode, r.Message)
	}
	for _, ca := range []string{other.CA, ""} {
		if r := resolve(ca, true, false); r.ErrorCode != "redis_unavailable" || !strings.Contains(r.Message, "certificate signed by unknown authority") {
			t.Errorf("ca-file %q: %s %s", ca, r.ErrorCode, r.Message)
		}
	}
	if r := resolve("", false, false); r.Outcome != credentials.Success {
		t.Errorf("tls-verify false: %s %s", r.ErrorCode, r.Message)
	}
	if r := resolve(site.CA, true, true); r.Outcome != credentials.Success || chain != 2 {
		t.Errorf("a client pair: %s %s, chain %d", r.ErrorCode, r.Message, chain)
	}
}

// TestTLSHandshakeEndsByTheTimeout: a store that accepts the connection and
// never answers the ClientHello ends the handshake at connect-timeout, under
// a context with no deadline, as redis_unavailable.
func TestTLSHandshakeEndsByTheTimeout(t *testing.T) {
	s := redistest.Start(t, &redistest.Server{Silent: true}, nil)
	b := backend(s.Addr)
	b.TLS, b.Timeout = true, 200*time.Millisecond
	done := make(chan credentials.BackendResult, 1)
	go func() { done <- b.Resolve(context.Background(), request(nil)) }()
	select {
	case r := <-done:
		if r.ErrorCode != "redis_unavailable" || !strings.Contains(r.Message, "timeout") {
			t.Errorf("an unanswered handshake: %s %s", r.ErrorCode, r.Message)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the handshake was still waiting 10s after a %s connect-timeout", b.Timeout)
	}
}

// authStore listens on 127.0.0.1 and answers each connection's first
// command, the AUTH, with answer; it returns the address.
func authStore(t *testing.T, answer func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				if _, err := readReply(bufio.NewReader(c)); err == nil {
					answer(c)
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// TestAuthFailureNamesItsCause: only the server's error reply to AUTH is
// redis_auth_failed; a connection that ends or goes silent under AUTH is
// redis_unavailable, retryable; a reply that breaks RESP is
// redis_protocol_error.
func TestAuthFailureNamesItsCause(t *testing.T) {
	for _, c := range []struct {
		what    string
		answer  func(net.Conn)
		code    string
		outcome credentials.Outcome
		retry   bool
	}{
		{"an error reply", func(c net.Conn) { _, _ = io.WriteString(c, "-WRONGPASS invalid username-password pair\r\n") }, "redis_auth_failed", credentials.PermissionDenied, false},
		{"a closed connection", func(net.Conn) {}, "redis_unavailable", credentials.Unavailable, true},
		{"no answer", func(c net.Conn) { _, _ = io.Copy(io.Discard, c) }, "redis_unavailable", credentials.Unavailable, true},
		{"a malformed reply", func(c net.Conn) { _, _ = io.WriteString(c, "+OK\n") }, "redis_protocol_error", credentials.Malformed, false},
	} {
		b := backend(authStore(t, c.answer))
		b.AuthPasswordEnv, b.Timeout = "REDIS_PASS", 200*time.Millisecond
		r := b.Resolve(context.Background(), request(map[string]string{"REDIS_PASS": "rpass"}))
		if r.ErrorCode != c.code || r.Outcome != c.outcome || r.Retryable != c.retry {
			t.Errorf("%s: %s %s retryable %t (%s), want %s %s retryable %t", c.what, r.Outcome, r.ErrorCode, r.Retryable, r.Message, c.outcome, c.code, c.retry)
		}
	}
}

// TestCodedFailureMessageHoldsNoCode: a reply that breaks RESP, or the
// server's error reply to HGETALL, carries its code in ErrorCode alone; the
// message is the text after the code, so a result's text writes it once.
func TestCodedFailureMessageHoldsNoCode(t *testing.T) {
	for _, c := range []struct {
		what, answer, code, message string
		auth                        bool
	}{
		{"a malformed reply to AUTH", "+OK\n", "redis_protocol_error", "invalid RESP line", true},
		{"an error reply to HGETALL", "-ERR no such key\r\n", "redis_command_rejected", "redis error: ERR no such key", false},
	} {
		b := backend(authStore(t, func(conn net.Conn) { _, _ = io.WriteString(conn, c.answer) }))
		env := map[string]string{}
		if c.auth {
			b.AuthPasswordEnv, env["REDIS_PASS"] = "REDIS_PASS", "rpass"
		}
		if r := b.Resolve(context.Background(), request(env)); r.ErrorCode != c.code || r.Message != c.message {
			t.Errorf("%s: code %q, message %q; want %q, %q", c.what, r.ErrorCode, r.Message, c.code, c.message)
		}
	}
}
