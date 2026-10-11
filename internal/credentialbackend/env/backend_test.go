package env

import (
	"context"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

func request(operator string, env map[string]string) credentials.ResolveRequest {
	req := credentials.ResolveRequest{Operator: credentials.Operator{Username: operator}, Policy: "default"}
	if env != nil {
		req.LookupEnv = func(name string) (string, bool) { v, ok := env[name]; return v, ok }
	}
	return req
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

// TestResolve is EXAMPLES chapter 56, issues 1, 2, and 4: the templates'
// variables are read through the request's lookup alone, each %operator%
// filled as text and a fixed name used as written; a template "" names
// none; a value is the value, env:X the text; none set is not found, as is a
// request without a lookup; a filled name that is no variable name stops the
// device, naming the variable and the remedy.
func TestResolve(t *testing.T) {
	t.Setenv("KARVI_netops_PASSWORD", "from-the-process")
	b := &Backend{BackendName: "e", UsernameTemplate: "SRV_USER", PasswordTemplate: "KARVI_%operator%_PASSWORD", EnableTemplate: ""}
	env := map[string]string{"SRV_USER": "svc", "KARVI_netops_PASSWORD": "env:NETPASS", "NETPASS": "not-read", "KARVI_%s_PASSWORD": "not-read"}
	if got := fields(t, b.Resolve(context.Background(), request("netops", env))); got != [3]string{"svc", "env:NETPASS", ""} {
		t.Errorf("filled: %q", got)
	}
	if r := b.Resolve(context.Background(), request("netops", map[string]string{})); r.Outcome != credentials.NotFound {
		t.Errorf("none set: %s", r.Outcome)
	}
	if r := b.Resolve(context.Background(), request("netops", nil)); r.Outcome != credentials.NotFound {
		t.Errorf("no lookup: %s", r.Outcome)
	}
	r := b.Resolve(context.Background(), request("j.smith", env))
	want := `credential backend e: password-var-template gives "KARVI_j.smith_PASSWORD", not a variable name (letters, digits, underscores; no leading digit); remedy: replace-chars in the backend's operator transform`
	if r.ErrorCode != "config_credential_backend_variable_invalid" || r.Message != want {
		t.Errorf("j.smith: %s %q", r.ErrorCode, r.Message)
	}
}
