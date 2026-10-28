package credentialbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/redis"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/redis/redistest"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/vault"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// storeResolver is a resolver over the configuration body, from a file, for
// the operator netops, whose input holds env.
func storeResolver(t *testing.T, body string, env map[string]string) (*Resolver, credentials.Operator) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "karvi.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{HomeDir: home, Operator: "netops", SkipAuto: true, Environment: []string{}, ExplicitRoots: []string{path}, Sets: []string{"creds.interactive-prompt=false"}})
	if err != nil {
		t.Fatal(err)
	}
	op := credentials.Operator{Username: "netops", UID: os.Getuid(), Home: home}
	r, err := New(cfg, op, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.SetInput(&scriptedInput{env: env})
	return r, op
}

// resolveAll resolves the devices named in parallel, as the planner's pool
// does.
func resolveAll(r *Resolver, op credentials.Operator, names ...string) ([]credentials.Resolved, []error) {
	got, errs := make([]credentials.Resolved, len(names)), make([]error, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], errs[i] = r.Resolve(context.Background(), op, inventory.Direct(name, "cisco_ios", "system", 22))
		}()
	}
	wg.Wait()
	return got, errs
}

// vaultStore is a KV-v2 read counting its requests.
func vaultStore(t *testing.T, records map[string]map[string]any) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var reads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		reads.Add(1)
		data, ok := records[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data}})
	}))
	t.Cleanup(srv.Close)
	return srv, &reads
}

// TestStoreAnswersHeld is EXAMPLES chapter 56, issue 15: an operator-keyed
// backend is asked once per resolver, its answer held for every device the
// policies send to it, a formula over it included; each device takes its
// own copy, so destroying one, as sealing a grant does, leaves the others
// whole; not found and a failure are held too, an unanswering store asked
// once.
func TestStoreAnswersHeld(t *testing.T) {
	rs := redistest.Start(t, &redistest.Server{Password: "rpass", Hashes: map[string]map[string]string{"karvi:credential:netops": {"username": "netops", "password": "S3cret"}}}, nil)
	vs, vaultReads := vaultStore(t, map[string]map[string]any{"/v1/secret/data/karvi/netops": {"username": "netops", "password": "V3cret"}})
	body := fmt.Sprintf(`
[credential-backend.r]
type = "redis"
address = %q
tls = false
auth-password-env = "REDIS_PASS"

[credential-backend.v]
type = "vault"
address = %q

[credential-backend.f]
type = "formula"
username-template = "svc-%%operator%%"
password-source = "r"

[credential-policy.pr]
backend-sequence = ["r"]

[credential-policy.pv]
backend-sequence = ["v"]

[credential-policy.pf]
backend-sequence = ["f"]

[[credential-policy-map]]
name = "r-*"
policy = "pr"

[[credential-policy-map]]
name = "v-*"
policy = "pv"

# f-1 and f-2, the formula's.
[[credential-policy-map]]
name = "*"
policy = "pf"
`, rs.Addr, vs.URL)
	r, op := storeResolver(t, body, map[string]string{"REDIS_PASS": "rpass", "VAULT_TOKEN": "vtok"})
	// The formula first, so its taking the held answer and destroying its
	// copy comes before every other device.
	first, errs := resolveAll(r, op, "f-1")
	if errs[0] != nil || first[0].DeviceUsername != "svc-netops" {
		t.Fatalf("the formula: %+v %v", first[0], errs[0])
	}
	first[0].Credential.Material.Destroy()
	got, errs := resolveAll(r, op, "r-1", "r-2", "r-3", "r-4", "v-1", "v-2", "v-3", "f-2")
	for i, err := range errs {
		if err != nil {
			t.Fatalf("device %d: %v", i, err)
		}
	}
	if n, a := rs.Count("HGETALL"), rs.Count("AUTH"); n != 1 || a != 1 {
		t.Errorf("redis asked %d HGETALL, %d AUTH; want one each for 5 devices and 2 formula devices", n, a)
	}
	if n := vaultReads.Load(); n != 1 {
		t.Errorf("vault read %d times for 3 devices", n)
	}
	got[0].Credential.Material.Destroy()
	for i, want := range map[int]string{1: "S3cret", 4: "V3cret", 7: "S3cret"} {
		if _, p, _, err := materialStrings(got[i].Credential.Material); err != nil || p != want {
			t.Errorf("device %d after another's copy was destroyed: %q %v", i, p, err)
		}
	}
	for _, g := range got[1:] {
		g.Credential.Material.Destroy()
	}

	// Not found, held: every device moves on to the fallback.
	empty := redistest.Start(t, &redistest.Server{Hashes: map[string]map[string]string{}}, nil)
	r, op = storeResolver(t, fmt.Sprintf("[credential-backend.r]\ntype = \"redis\"\naddress = %q\ntls = false\n\n[credential-policy.default]\nbackend-sequence = [\"r\"]\n", empty.Addr), map[string]string{"NETUSER": "netuser", "NETPASS": "netpass"})
	got, errs = resolveAll(r, op, "d1", "d2", "d3", "d4")
	for i := range got {
		if errs[i] != nil || got[i].Credential.Backend != "builtin-env-fallback" {
			t.Errorf("not found, device %d: %+v %v", i, got[i].Credential.Backend, errs[i])
			continue
		}
		got[i].Credential.Material.Destroy()
	}
	if n := empty.Count("HGETALL"); n != 1 {
		t.Errorf("not found: redis asked %d times for 4 devices", n)
	}

	// A failure, held: an unanswering store costs one timeout.
	silent := redistest.Start(t, &redistest.Server{Silent: true}, nil)
	r, op = storeResolver(t, fmt.Sprintf("[credential-backend.r]\ntype = \"redis\"\naddress = %q\ntls = false\nconnect-timeout = \"200ms\"\n\n[credential-policy.default]\nbackend-sequence = [\"r\"]\n", silent.Addr), nil)
	_, errs = resolveAll(r, op, "d1", "d2", "d3", "d4", "d5", "d6")
	for i, err := range errs {
		if errorcodes.Of(err) != "redis_unavailable" {
			t.Errorf("unanswering, device %d: %v", i, err)
		}
	}
	if n := silent.Conns(); n != 1 {
		t.Errorf("an unanswering store was asked %d times for 6 devices", n)
	}
}

// TestStoresBuiltWithTheirDefaults is EXAMPLES chapter 56, issue 6: the
// resolver sets every default where it builds a Redis or Vault backend, and
// Resolve leaves the backend as built, through the resolver and when eight
// devices ask the backend itself at once, as the planner's pool asked it
// before answers were held; run under -race.
func TestStoresBuiltWithTheirDefaults(t *testing.T) {
	rs := redistest.Start(t, &redistest.Server{Hashes: map[string]map[string]string{"karvi:credential:netops": {"username": "netops", "password": "S3cret"}}}, nil)
	vs, _ := vaultStore(t, map[string]map[string]any{"/v1/secret/data/karvi/netops": {"username": "netops", "password": "V3cret"}})
	body := fmt.Sprintf(`
[credential-backend.r]
type = "redis"
address = %q
tls = false

[credential-backend.v]
type = "vault"
address = %q

[credential-policy.pr]
backend-sequence = ["r"]

[credential-policy.pv]
backend-sequence = ["v"]

[[credential-policy-map]]
name = "r-*"
policy = "pr"

[[credential-policy-map]]
name = "*"
policy = "pv"
`, rs.Addr, vs.URL)
	r, op := storeResolver(t, body, map[string]string{"VAULT_TOKEN": "vtok"})
	rb, vb := r.backends["r"].(*redis.Backend), r.backends["v"].(*vault.Backend)
	builtR, builtV := *rb, *vb
	if builtR.KeyTemplate != "karvi:credential:%operator%" || builtR.Timeout.String() != "2s" || !builtR.TLSVerify {
		t.Errorf("redis as built: %+v", builtR)
	}
	if builtV.Mount != "secret" || builtV.PathTemplate != "karvi/%operator%" || builtV.TokenEnv != "VAULT_TOKEN" || builtV.UsernameField != "username" || builtV.PasswordField != "password" || builtV.EnableField != "enable_password" || builtV.Timeout.String() != "5s" || !builtV.TLSVerify {
		t.Errorf("vault as built: %+v", builtV)
	}
	var wg sync.WaitGroup
	for range 8 {
		for _, b := range []credentials.Backend{rb, vb} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res := b.Resolve(context.Background(), credentials.ResolveRequest{Operator: op, Policy: "default", LookupEnv: r.lookupEnv})
				if res.Outcome != credentials.Success {
					t.Errorf("%s: %s %s", b.Name(), res.ErrorCode, res.Message)
					return
				}
				res.Credential.Material.Destroy()
			}()
		}
	}
	wg.Wait()
	got, errs := resolveAll(r, op, "r-1", "v-1")
	for i, err := range errs {
		if err != nil {
			t.Fatalf("device %d: %v", i, err)
		}
		got[i].Credential.Material.Destroy()
	}
	if *rb != builtR || *vb != builtV {
		t.Errorf("Resolve changed its backend: %+v %+v", *rb, *vb)
	}
}
