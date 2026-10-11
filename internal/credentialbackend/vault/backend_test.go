package vault

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/storetls/tlstest"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

// store is a KV-v2 read: records maps a request path to its data; each
// request's token, namespace, and client chain are counted or kept.
type store struct {
	records map[string]map[string]any
	reads   atomic.Int32
	mu      sync.Mutex
	token   string
	ns      string
	chain   int
}

func (s *store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.reads.Add(1)
	s.mu.Lock()
	s.token, s.ns = r.Header.Get("X-Vault-Token"), r.Header.Get("X-Vault-Namespace")
	if r.TLS != nil {
		s.chain = len(r.TLS.PeerCertificates)
	}
	s.mu.Unlock()
	data, ok := s.records[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data}})
}

func request(env map[string]string) credentials.ResolveRequest {
	return credentials.ResolveRequest{Operator: credentials.Operator{Username: "netops"}, Policy: "default", LookupEnv: func(name string) (string, bool) { v, ok := env[name]; return v, ok }}
}

// backend is v as the resolver builds it with its defaults.
func backend(addr string) *Backend {
	return &Backend{BackendName: "v", Address: addr, Mount: "secret", PathTemplate: "karvi/" + credentials.OperatorMacro, TokenEnv: "VAULT_TOKEN", UsernameField: "username", PasswordField: "password", EnableField: "enable_password", TLSVerify: true, Timeout: 5 * time.Second}
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

var vtok = map[string]string{"VAULT_TOKEN": "vtok"}

// TestToken is EXAMPLES chapter 56, issue 1: the token is read through the
// request's lookup alone, before any request; unset or empty is
// credential_env_missing naming the backend, the key, and the variable.
func TestToken(t *testing.T) {
	t.Setenv("VAULT_TOKEN", "from-the-process")
	s := &store{records: map[string]map[string]any{"/v1/secret/data/karvi/netops": {"username": "netops", "password": "S3cret"}}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	for env, says := range map[string]string{"": "VAULT_TOKEN not set", "empty": "VAULT_TOKEN empty"} {
		lookup := map[string]string{}
		if env == "empty" {
			lookup["VAULT_TOKEN"] = ""
		}
		r := backend(srv.URL).Resolve(context.Background(), request(lookup))
		if r.ErrorCode != "credential_env_missing" || r.Message != "credential backend v: token-env: "+says {
			t.Errorf("%q: %s %q", env, r.ErrorCode, r.Message)
		}
	}
	if n := s.reads.Load(); n != 0 {
		t.Errorf("a missing token reached the store: %d reads", n)
	}
	b := backend(srv.URL)
	b.Namespace = "team"
	if got := fields(t, b.Resolve(context.Background(), request(vtok))); got[1] != "S3cret" || s.token != "vtok" || s.ns != "team" {
		t.Errorf("read: %q, token %q, namespace %q", got, s.token, s.ns)
	}
}

// TestRecordReferences is EXAMPLES chapter 56, issues 2 and 5: a configured
// field whose value begins with env: is the variable's value through the
// lookup; unset is credential_env_missing naming the path, the field, and
// the variable; a reference to no variable name is vault_response_malformed,
// never quoting the field; the credential's source is the path read.
func TestRecordReferences(t *testing.T) {
	s := &store{records: map[string]map[string]any{
		"/v1/kv/data/sites/netops": {"login": "USER", "secret": "env:NETPASS_NYC", "en": "Env:NETENABLE"},
		"/v1/kv/data/sites/unset":  {"login": "netops", "secret": "env:NETPASS_UNSET"},
		"/v1/kv/data/sites/bad":    {"login": "netops", "secret": "env:p$ss-word"},
	}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	b := backend(srv.URL)
	b.Mount, b.PathTemplate, b.UsernameField, b.PasswordField, b.EnableField = "kv", "sites/"+credentials.OperatorMacro, "login", "secret", "en"
	built := *b
	env := map[string]string{"VAULT_TOKEN": "vtok", "USER": "not-read", "NETPASS_NYC": "S3cret", "NETENABLE": "en"}
	r := b.Resolve(context.Background(), request(env))
	if src := r.Credential.MatchedOn.Source; src != "v1/kv/data/sites/netops" {
		t.Errorf("source %q", src)
	}
	if got := fields(t, r); got != [3]string{"USER", "S3cret", "en"} {
		t.Errorf("references: %q", got)
	}
	req := request(env)
	req.Operator.Username = "unset"
	if r := b.Resolve(context.Background(), req); r.ErrorCode != "credential_env_missing" || r.Message != "credential backend v: v1/kv/data/sites/unset secret: NETPASS_UNSET not set" {
		t.Errorf("unset: %s %q", r.ErrorCode, r.Message)
	}
	req.Operator.Username = "bad"
	if r := b.Resolve(context.Background(), req); r.ErrorCode != "vault_response_malformed" || !strings.HasPrefix(r.Message, "credential backend v: v1/kv/data/sites/bad secret: env: not followed by a variable name") || strings.Contains(r.Message, "p$ss") {
		t.Errorf("bad: %s %q", r.ErrorCode, r.Message)
	}
	req.Operator.Username = "absent"
	if r := b.Resolve(context.Background(), req); r.Outcome != credentials.NotFound {
		t.Errorf("no record: %s", r.Outcome)
	}
	if *b != built {
		t.Errorf("Resolve changed its backend: %+v", *b)
	}
}

// TestTLS is EXAMPLES chapter 56, issue 16: a self-signed store verified by
// naming its own certificate in ca-file and refused without it; a server
// another CA signed refused under ca-file; tls-verify = false reads it; a
// client pair presented with its chain.
func TestTLS(t *testing.T) {
	s := &store{records: map[string]map[string]any{"/v1/secret/data/karvi/netops": {"username": "netops", "password": "S3cret"}}}
	site := tlstest.New(t, "site CA")
	srv := httptest.NewUnstartedServer(s)
	srv.TLS = &tls.Config{ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: site.Pool}
	srv.StartTLS()
	defer srv.Close()
	self := filepath.Join(t.TempDir(), "store.pem")
	if err := os.WriteFile(self, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o644); err != nil {
		t.Fatal(err)
	}
	resolve := func(ca string, verify, client bool) credentials.BackendResult {
		b := backend(srv.URL)
		b.CAFile, b.TLSVerify = ca, verify
		if client {
			b.ClientCertFile, b.ClientKeyFile = site.ClientCert, site.ClientKey
		}
		return b.Resolve(context.Background(), request(vtok))
	}
	if r := resolve(self, true, false); r.Outcome != credentials.Success {
		t.Errorf("ca-file the store's own certificate: %s %s", r.ErrorCode, r.Message)
	}
	for _, ca := range []string{"", site.CA} {
		if r := resolve(ca, true, false); r.ErrorCode != "vault_unavailable" || !strings.Contains(r.Message, "certificate") {
			t.Errorf("ca-file %q: %s %s", ca, r.ErrorCode, r.Message)
		}
	}
	if r := resolve("", false, false); r.Outcome != credentials.Success {
		t.Errorf("tls-verify false: %s %s", r.ErrorCode, r.Message)
	}
	if r := resolve(self, true, true); r.Outcome != credentials.Success || s.chain != 2 {
		t.Errorf("a client pair: %s %s, chain %d", r.ErrorCode, r.Message, s.chain)
	}
}
