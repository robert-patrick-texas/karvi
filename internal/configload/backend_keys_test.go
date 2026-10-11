package configload

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestBackendKeysByType is EXAMPLES chapter 55, issues 6 and 7: each
// credential backend type loads with every key it reads; a key its type does
// not read is refused, naming the types that read it; the sqlite type is
// refused as unsupported. The env-indirection keys are removed on every type
// (chapter 56, issue 2; TestRemovedKeysRefused).
func TestBackendKeysByType(t *testing.T) {
	load := func(sets ...string) error {
		_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: sets})
		return err
	}
	b := func(typ string, sets ...string) []string {
		out := []string{`credential-backend.b.type="` + typ + `"`}
		for _, s := range sets {
			out = append(out, "credential-backend.b."+s)
		}
		return out
	}
	tlsFiles := []string{`tls-verify=true`, `ca-file="/etc/karvi/ca.pem"`, `client-cert-file="/etc/karvi/c.pem"`, `client-key-file="/etc/karvi/c.key"`}
	csvSource := []string{`credential-backend.c.type="csv"`, `credential-backend.c.scope="user"`, `credential-backend.c.path="~/c.csv"`}
	for _, sets := range [][]string{
		b("env", `username-var-template="U_%operator%"`, `password-var-template="P_%operator%"`, `enable-var-template="E_%operator%"`, `transform="default"`),
		b("cloginrc", `scope="user"`, `path="~/.cloginrc"`, `required=true`),
		b("csv", `scope="user"`, `path="~/c.csv"`, `required=true`, `mode="header"`, `delimiter=";"`, `mandatory-fields=["username"]`, `mappings.username="login"`),
		b("redis", append([]string{`address="127.0.0.1:6379"`, `database=1`, `key-template="k:%operator%"`, `tls=true`, `auth-username-env="RU"`, `auth-password-env="RP"`, `connect-timeout="2s"`}, tlsFiles...)...),
		b("vault", append([]string{`address="https://127.0.0.1:8200"`, `namespace="ns"`, `mount="secret"`, `path-template="karvi/%operator%"`, `token-env="VT"`, `connect-timeout="5s"`, `username-field="u"`, `password-field="p"`, `enable-field="e"`}, tlsFiles...)...),
		append(b("formula", `username-template="%operator%-adm"`, `password-source="c"`), csvSource...),
	} {
		if err := load(sets...); err != nil {
			t.Errorf("%v: %v", sets, err)
		}
	}
	for _, tc := range []struct {
		sets        []string
		key, reason string
	}{
		{b("redis", `address="127.0.0.1:6379"`, `mount="secret"`), "mount", "mount applies to the vault backend type only"},
		{append(b("formula", `username-template="%operator%-adm"`, `password-source="c"`, `path="~/other.csv"`), csvSource...), "path", "path applies to the cloginrc and csv backend types only"},
		{b("env", `password-var-template="P_%operator%"`, `address="127.0.0.1:6379"`), "address", "address applies to the redis and vault backend types only"},
		{b("vault", `address="https://127.0.0.1:8200"`, `tls=false`), "tls", "tls applies to the redis backend type only"},
		{b("csv", `scope="user"`, `path="~/c.csv"`, `tls-verify=true`), "tls-verify", "tls-verify applies to the redis and vault backend types only"},
		{b("csv", `scope="user"`, `path="~/c.csv"`, `token-env="VT"`), "token-env", "token-env applies to the vault backend type only"},
		{b("env", `scope="user"`), "scope", "scope applies to the cloginrc and csv backend types only"},
	} {
		err := load(tc.sets...)
		if got := errorcodes.Of(err); got != "config_credential_backend_key_unsupported" {
			t.Errorf("%v: code %q (%v)", tc.sets, got, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.reason+" for credential-backend.b."+tc.key) {
			t.Errorf("%v: %v, want %q for credential-backend.b.%s", tc.sets, err, tc.reason, tc.key)
		}
	}
	err := load(b("sqlite")...)
	if got := errorcodes.Of(err); got != "config_credential_backend_type_unsupported" {
		t.Errorf("sqlite: code %q (%v)", got, err)
	}
}
