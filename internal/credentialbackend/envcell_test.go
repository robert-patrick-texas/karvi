package credentialbackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// TestEnvCellReadsTheResolversLookup is EXAMPLES chapter 55, issues 3 and 4:
// a credential CSV's env:NAME cell reads the resolver's one lookup, its input
// provider's, as the NET* fallback does, and not the process environment; a
// variable the provider does not hold stops the device under
// credential_env_missing, naming the variable, before a later backend or the
// fallback is asked; a resolver without a provider reads no variable.
func TestEnvCellReadsTheResolversLookup(t *testing.T) {
	t.Setenv("ROW_PASS", "from-the-process")
	home := t.TempDir()
	file, later := filepath.Join(home, "credentials.csv"), filepath.Join(home, "later.csv")
	for path, body := range map[string]string{
		file:  "device_name,username,password,enable_password\nsw-*,svc,env:ROW_PASS,\nrt-*,svc,env:ROW_UNSET,\n",
		later: "device_name,username,password\n*,later,later-pass\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{
		"creds.interactive-prompt=false", `creds.backend-sequence=["creds", "later"]`,
		`credential-backend.creds.type="csv"`, `credential-backend.creds.scope="user"`, `credential-backend.creds.path="` + file + `"`,
		`credential-backend.later.type="csv"`, `credential-backend.later.scope="user"`, `credential-backend.later.path="` + later + `"`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	op := credentials.Operator{Username: "netops", UID: os.Getuid(), Home: home}
	resolve := func(t *testing.T, input InputProvider, name string) (credentials.Resolved, error) {
		t.Helper()
		r, err := New(cfg, op, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.SetInput(input)
		return r.Resolve(context.Background(), op, inventory.Direct(name, "cisco_iosxe", "system", 22))
	}
	input := &scriptedInput{env: map[string]string{"ROW_PASS": "from-the-provider", "NETUSER": "netuser", "NETPASS": "netpass"}}

	got, err := resolve(t, input, "sw-1")
	if err != nil {
		t.Fatal(err)
	}
	_, password, _, err := materialStrings(got.Credential.Material)
	got.Credential.Material.Destroy()
	if err != nil || password != "from-the-provider" || got.Credential.Backend != "creds" {
		t.Errorf("the provider's value: %q via %s (%v)", password, got.Credential.Backend, err)
	}
	for _, tc := range []struct {
		input  InputProvider
		device string
		says   string
	}{
		{input, "rt-1", file + ":3 password: ROW_UNSET is not set"},
		{nil, "sw-1", file + ":2 password: ROW_PASS is not set"},
	} {
		_, err := resolve(t, tc.input, tc.device)
		if errorcodes.Of(err) != "credential_env_missing" || !strings.Contains(err.Error(), "credential backend creds: "+tc.says) {
			t.Errorf("%s: %v, want credential_env_missing saying %q", tc.device, err, tc.says)
		}
	}
}
