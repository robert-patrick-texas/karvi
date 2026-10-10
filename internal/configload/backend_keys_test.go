package configload

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestBackendKeysByType is EXAMPLES chapter 55, issues 6 and 7: each
// credential backend type loads with every key it reads; a key its type does
// not read is refused, naming the types that read it, a csv backend's
// env-indirection key saying what replaced it; the sqlite type is refused as
// unsupported.
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
	indirection := []string{`env-indirection.username=true`, `env-indirection.password=true`, `env-indirection.enable-password=true`}
	tlsFiles := []string{`ca-file="/etc/karvi/ca.pem"`, `client-cert-file="/etc/karvi/c.pem"`, `client-key-file="/etc/karvi/c.key"`}
	csvSource := []string{`credential-backend.c.type="csv"`, `credential-backend.c.scope="user"`, `credential-backend.c.path="~/c.csv"`}
	for _, sets := range [][]string{
		b("env", append([]string{`username-var-template="U_%s"`, `password-var-template="P_%s"`, `enable-var-template="E_%s"`, `transform="default"`}, indirection...)...),
		b("cloginrc", `scope="user"`, `path="~/.cloginrc"`, `required=true`),
		b("csv", `scope="user"`, `path="~/c.csv"`, `required=true`, `mode="header"`, `delimiter=";"`, `mandatory-fields=["username"]`, `mappings.username="login"`),
		b("redis", append(append([]string{`address="127.0.0.1:6379"`, `database=1`, `key-template="k:%s"`, `tls=true`, `auth-username-env="RU"`, `auth-password-env="RP"`, `connect-timeout="2s"`}, tlsFiles...), indirection...)...),
		b("vault", append(append([]string{`address="https://127.0.0.1:8200"`, `namespace="ns"`, `mount="secret"`, `path-template="karvi/%s"`, `token-env="VT"`, `connect-timeout="5s"`, `username-field="u"`, `password-field="p"`, `enable-field="e"`}, tlsFiles...), indirection...)...),
		append(b("formula", `username-template="%s-adm"`, `password-source="c"`), csvSource...),
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
		{append(b("formula", `username-template="%s-adm"`, `password-source="c"`, `path="~/other.csv"`), csvSource...), "path", "path applies to the cloginrc and csv backend types only"},
		{b("env", `password-var-template="P_%s"`, `address="127.0.0.1:6379"`), "address", "address applies to the redis and vault backend types only"},
		{b("vault", `address="https://127.0.0.1:8200"`, `tls=false`), "tls", "tls applies to the redis backend type only"},
		{b("cloginrc", `env-indirection.password=true`), "env-indirection.password", "env-indirection.password applies to the env, redis, and vault backend types only"},
		{b("csv", `scope="user"`, `path="~/c.csv"`, `token-env="VT"`), "token-env", "token-env applies to the vault backend type only"},
		{b("env", `scope="user"`), "scope", "scope applies to the cloginrc and csv backend types only"},
		{b("csv", `scope="user"`, `path="~/c.csv"`, `env-indirection.username=true`), "env-indirection.username", "a csv backend takes no env-indirection key: a cell names a variable as env:NAME"},
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
