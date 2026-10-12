package configload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// loadAs loads body as an explicit root, the operator named operator, with
// sets after it.
func loadAs(t *testing.T, operator, body string, sets ...string) (Snapshot, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "karvi.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(Options{HomeDir: t.TempDir(), Operator: operator, SkipAuto: true, Environment: []string{}, Sets: sets, ExplicitRoots: []string{path}})
}

// refused requires err to carry code at key with want in its message.
func refused(t *testing.T, label string, err error, code, key, want string) {
	t.Helper()
	if got := errorcodes.Of(err); got != code {
		t.Errorf("%s: code %q, want %s (%v)", label, got, code, err)
		return
	}
	if !strings.Contains(err.Error(), want+" for "+key) {
		t.Errorf("%s: %v, want %q for %s", label, err, want, key)
	}
}

// TestOperatorMacro is EXAMPLES chapter 56, issue 1: %operator% is the
// operator's login name, filled at the load in every value of every layer,
// the digest the filled values', and kept as written in the seven keys a
// backend fills, the key being filled deciding, so a site's macro holding it
// keeps it there and fills it elsewhere; [macros] operator is refused, and
// %Operator% is an ordinary macro.
func TestOperatorMacro(t *testing.T) {
	body := `[macros]
who = "%operator%"

[display.run]
header = "u=%operator%"
footer = "%who%"

[credential-backend.e]
type = "env"
username-var-template = "K_%operator%_U"
password-var-template = "K_%who%_P"
enable-var-template = "K_%operator%_E"

[credential-backend.r]
type = "redis"
address = "127.0.0.1:6379"
key-template = "k:%who%"

[credential-backend.v]
type = "vault"
address = "https://127.0.0.1:8200"
path-template = "karvi/%operator%"

[credential-backend.f]
type = "formula"
username-template = "%operator%-adm"
password-source = "e"

[credential-policy.p]
backend-sequence = ["e"]
username-template = "svc.%who%"
`
	snap, err := loadAs(t, "alice", body, `display.collection.footer="c=%operator%"`)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"display.run.header":                         "u=alice",
		"display.run.footer":                         "alice",
		"display.collection.footer":                  "c=alice",
		"credential-backend.e.username-var-template": "K_%operator%_U",
		"credential-backend.e.password-var-template": "K_%operator%_P",
		"credential-backend.e.enable-var-template":   "K_%operator%_E",
		"credential-backend.r.key-template":          "k:%operator%",
		"credential-backend.v.path-template":         "karvi/%operator%",
		"credential-backend.f.username-template":     "%operator%-adm",
		"credential-policy.p.username-template":      "svc.%operator%",
		"credential-backend.e.type":                  "env",
		"credential-backend.f.password-source":       "e",
	} {
		if got := snap.String(key); got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
	if trace := snap.Values["display.run.header"].MacroTrace; len(trace) != 1 || trace[0] != "operator" {
		t.Errorf("header's trace %v", trace)
	}
	// The environment layer fills it too.
	env, err := Load(Options{HomeDir: t.TempDir(), Operator: "alice", SkipAuto: true, Environment: []string{"KARVI__DISPLAY__RUN__HEADER=h=%operator%"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := env.String("display.run.header"); got != "h=alice" {
		t.Errorf("environment: %q", got)
	}
	// The digest is the filled values': a name in a kept key alone leaves
	// it the same for every operator, a filled one does not.
	digests := func(sets ...string) (string, string) {
		home := t.TempDir()
		as := func(operator string) string {
			snap, err := Load(Options{HomeDir: home, Operator: operator, SkipAuto: true, Environment: []string{}, Sets: sets})
			if err != nil {
				t.Fatal(err)
			}
			return snap.Digest
		}
		return as("alice"), as("bob")
	}
	if a, b := digests(`credential-backend.r.type="redis"`, `credential-backend.r.address="127.0.0.1:6379"`, `credential-backend.r.key-template="k:%operator%"`); a != b {
		t.Errorf("kept: digests %q and %q differ", a, b)
	}
	if a, b := digests(`display.run.header="%operator%"`); a == b {
		t.Error("filled: one digest for two operators")
	}
	// %s is text everywhere.
	snap, err = loadAs(t, "alice", "[display.run]\nheader = \"%s\"\n\n[credential-backend.f]\ntype = \"formula\"\nusername-template = \"%s-adm\"\npassword-source = \"e\"\n\n[credential-backend.e]\ntype = \"env\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if snap.String("display.run.header") != "%s" || snap.String("credential-backend.f.username-template") != "%s-adm" {
		t.Errorf("%%s: %q, %q", snap.String("display.run.header"), snap.String("credential-backend.f.username-template"))
	}
	// The name is reserved, from a file and from --set.
	_, err = loadAs(t, "alice", "[macros]\noperator = \"x\"\n")
	refused(t, "[macros] operator", err, "config_macro_name_reserved", "macros.operator", "%operator% built in (the operator's login name)")
	_, err = loadAs(t, "alice", "", `macros.operator="x"`)
	refused(t, "--set macros.operator", err, "config_macro_name_reserved", "macros.operator", "%operator% built in (the operator's login name)")
	// %Operator% is an ordinary macro: undefined, then a site's.
	_, err = loadAs(t, "alice", "[display.run]\nheader = \"%Operator%\"\n")
	refused(t, "%Operator% undefined", err, "config_macro_undefined", "display.run.header", "undefined macro %Operator%")
	snap, err = loadAs(t, "alice", "[macros]\nOperator = \"site\"\n\n[display.run]\nheader = \"%Operator%\"\n")
	if err != nil || snap.String("display.run.header") != "site" {
		t.Errorf("%%Operator%% defined: %q %v", snap.String("display.run.header"), err)
	}
}

// TestStoreBackendKinds is EXAMPLES chapter 56, issue 6: every key of the
// env, Redis, and Vault types has a kind, checked at the load: the values
// the lab found silently the default are refused.
func TestStoreBackendKinds(t *testing.T) {
	redis := []string{`credential-backend.r.type="redis"`, `credential-backend.r.address="127.0.0.1:6379"`}
	vault := []string{`credential-backend.v.type="vault"`, `credential-backend.v.address="https://127.0.0.1:8200"`}
	env := []string{`credential-backend.e.type="env"`}
	with := func(base []string, set string) []string { return append(append([]string(nil), base...), set) }
	for _, tc := range []struct {
		sets           []string
		code, key, msg string
	}{
		{with(redis, `credential-backend.r.tls="false"`), "config_type_error", "credential-backend.r.tls", "must be a boolean"},
		{with(redis, `credential-backend.r.tls-verify="yes"`), "config_type_error", "credential-backend.r.tls-verify", "must be a boolean"},
		{with(redis, `credential-backend.r.database="2"`), "config_type_error", "credential-backend.r.database", "must be an integer"},
		{with(redis, `credential-backend.r.database=-1`), "config_value_out_of_range", "credential-backend.r.database", "must be 0 or more"},
		{with(redis, `credential-backend.r.connect-timeout="abc"`), "config_duration_error", "credential-backend.r.connect-timeout", "invalid Go duration"},
		{with(redis, `credential-backend.r.connect-timeout=2`), "config_type_error", "credential-backend.r.connect-timeout", "must be a duration string"},
		{with(vault, `credential-backend.v.connect-timeout="0s"`), "config_value_out_of_range", "credential-backend.v.connect-timeout", "must be above 0"},
		{with(vault, `credential-backend.v.connect-timeout="-1s"`), "config_duration_negative", "credential-backend.v.connect-timeout", "duration cannot be negative"},
		{[]string{`credential-backend.r.type="redis"`, `credential-backend.r.address=6380`}, "config_type_error", "credential-backend.r.address", "must be a string"},
		{with(vault, `credential-backend.v.token-env=5`), "config_type_error", "credential-backend.v.token-env", "must be a string"},
		{with(vault, `credential-backend.v.mount=true`), "config_type_error", "credential-backend.v.mount", "must be a string"},
		{with(env, `credential-backend.e.password-var-template=1`), "config_type_error", "credential-backend.e.password-var-template", "must be a string"},
		{with(env, `credential-backend.e.transform=5`), "config_type_error", "credential-backend.e.transform", "must be a string"},
	} {
		_, err := Load(Options{HomeDir: t.TempDir(), Operator: "alice", SkipAuto: true, Environment: []string{}, Sets: tc.sets})
		refused(t, strings.Join(tc.sets, " "), err, tc.code, tc.key, tc.msg)
	}
	for _, sets := range [][]string{
		with(redis, `credential-backend.r.database=0`),
		with(redis, `credential-backend.r.connect-timeout="1ms"`),
		with(vault, `credential-backend.v.connect-timeout="30s"`),
	} {
		if _, err := Load(Options{HomeDir: t.TempDir(), Operator: "alice", SkipAuto: true, Environment: []string{}, Sets: sets}); err != nil {
			t.Errorf("%v: %v", sets, err)
		}
	}
}

// TestStoreBackendAddress is EXAMPLES chapter 56, issue 6: Redis's address
// is required and host:port, Vault's an http:// or https:// URL with a host.
func TestStoreBackendAddress(t *testing.T) {
	load := func(typ, addr string) error {
		sets := []string{`credential-backend.s.type="` + typ + `"`}
		if addr != "-" {
			sets = append(sets, `credential-backend.s.address="`+addr+`"`)
		}
		_, err := Load(Options{HomeDir: t.TempDir(), Operator: "alice", SkipAuto: true, Environment: []string{}, Sets: sets})
		return err
	}
	const key = "credential-backend.s.address"
	refused(t, "redis without an address", load("redis", "-"), "config_credential_backend_address_invalid", key, "a redis credential backend needs an address, host:port")
	refused(t, "vault without an address", load("vault", "-"), "config_credential_backend_address_invalid", key, "a vault credential backend needs an address, an http:// or https:// URL")
	for _, addr := range []string{"", "127.0.0.1", ":6379", "redis://127.0.0.1:6379", "h:0", "h:65536", "h:redis", "h:6379:1"} {
		refused(t, "redis "+addr, load("redis", addr), "config_credential_backend_address_invalid", key, "not host:port")
	}
	for _, addr := range []string{"", "127.0.0.1:8200", "vault.example.com", "ftp://h", "https://", "https://:8200", "//h:8200"} {
		refused(t, "vault "+addr, load("vault", addr), "config_credential_backend_address_invalid", key, "not an http:// or https:// URL with a host")
	}
	for _, tc := range [][2]string{
		{"redis", "127.0.0.1:6379"}, {"redis", "[::1]:6379"}, {"redis", "redis.example.com:6380"},
		{"vault", "http://127.0.0.1:8201"}, {"vault", "https://vault.example.com/prefix"}, {"vault", "HTTPS://vault.example.com"}, {"vault", "https://[::1]:8200"},
	} {
		if err := load(tc[0], tc[1]); err != nil {
			t.Errorf("%s %s: %v", tc[0], tc[1], err)
		}
	}
}

// TestStoreBackendVariables is EXAMPLES chapter 56, issue 4: the variables
// a Redis or Vault backend names are portable variable names; an env
// backend's templates are, with each %operator% standing for a letter; the
// refusal quotes the value. A name filled from %operator% at the load is
// judged as filled. "" is refused for token-env, whose default is a value,
// and is none for the rest (issue 17).
func TestStoreBackendVariables(t *testing.T) {
	load := func(operator string, sets ...string) error {
		_, err := Load(Options{HomeDir: t.TempDir(), Operator: operator, SkipAuto: true, Environment: []string{}, Sets: sets})
		return err
	}
	vault := func(set string) []string {
		return []string{`credential-backend.v.type="vault"`, `credential-backend.v.address="http://127.0.0.1:8201"`, "credential-backend.v." + set}
	}
	redis := func(sets ...string) []string {
		out := []string{`credential-backend.r.type="redis"`, `credential-backend.r.address="127.0.0.1:6379"`}
		for _, s := range sets {
			out = append(out, "credential-backend.r."+s)
		}
		return out
	}
	env := func(set string) []string {
		return []string{`credential-backend.e.type="env"`, "credential-backend.e." + set}
	}
	const code = "config_credential_backend_variable_invalid"
	const rule = " not a variable name (letters, digits, underscores; no leading digit)"
	for _, tc := range []struct {
		sets       []string
		key, value string
	}{
		{vault(`token-env="VAULT TOKEN"`), "credential-backend.v.token-env", `"VAULT TOKEN"`},
		{vault(`token-env="KARVI-TOKEN"`), "credential-backend.v.token-env", `"KARVI-TOKEN"`},
		{vault(`token-env=""`), "credential-backend.v.token-env", `""`},
		{vault(`token-env="1TOKEN"`), "credential-backend.v.token-env", `"1TOKEN"`},
		{redis(`auth-password-env="redis-pass"`), "credential-backend.r.auth-password-env", `"redis-pass"`},
		{redis(`auth-password-env="RP"`, `auth-username-env="redis user"`), "credential-backend.r.auth-username-env", `"redis user"`},
		{env(`password-var-template="KARVI-%operator%-PASSWORD"`), "credential-backend.e.password-var-template", `"KARVI-%operator%-PASSWORD"`},
		{env(`enable-var-template="KARVI_%s_ENABLE"`), "credential-backend.e.enable-var-template", `"KARVI_%s_ENABLE"`},
	} {
		refused(t, strings.Join(tc.sets, " "), load("alice", tc.sets...), code, tc.key, tc.value+rule)
	}
	for _, sets := range [][]string{
		vault(`token-env="VAULT_TOKEN"`), vault(`token-env="_T"`),
		redis(`auth-password-env="RP"`, `auth-username-env="RU"`),
		env(`password-var-template="KARVI_%operator%_PASSWORD"`), env(`password-var-template="%operator%"`), env(`password-var-template="%operator%_P"`), env(`enable-var-template="NETENABLE"`),
		env(`username-var-template=""`), redis(`auth-password-env=""`), redis(`auth-password-env="RP"`, `auth-username-env=""`),
	} {
		if err := load("alice", sets...); err != nil {
			t.Errorf("%v: %v", sets, err)
		}
	}
	// token-env keeps no %operator%: it is filled at the load, and judged
	// filled, so a name that makes it no variable name is refused for that
	// operator alone.
	sets := vault(`token-env="VT_%operator%"`)
	if err := load("alice", sets...); err != nil {
		t.Errorf("alice: %v", err)
	}
	refused(t, "j.smith", load("j.smith", sets...), code, "credential-backend.v.token-env", `"VT_j.smith"`+rule)
}

// TestStoreBackendUnreadKeys is EXAMPLES chapter 56, issues 3 and 16: a key
// the backend's other keys leave unread is refused, naming what it needs.
func TestStoreBackendUnreadKeys(t *testing.T) {
	load := func(typ, addr string, sets ...string) error {
		all := []string{`credential-backend.s.type="` + typ + `"`, `credential-backend.s.address="` + addr + `"`}
		for _, s := range sets {
			all = append(all, "credential-backend.s."+s)
		}
		_, err := Load(Options{HomeDir: t.TempDir(), Operator: "alice", SkipAuto: true, Environment: []string{}, Sets: all})
		return err
	}
	const redis, plain, https = "127.0.0.1:6379", "http://127.0.0.1:8201", "https://127.0.0.1:8200"
	const cert, key, ca = `client-cert-file="/etc/karvi/c.pem"`, `client-key-file="/etc/karvi/c.key"`, `ca-file="/etc/karvi/ca.pem"`
	for _, tc := range []struct {
		typ, addr string
		sets      []string
		key, msg  string
	}{
		{"redis", redis, []string{`auth-username-env="RU"`}, "auth-username-env", "auth-username-env read only beside auth-password-env"},
		{"redis", redis, []string{cert}, "client-cert-file", "client-cert-file read only beside client-key-file"},
		{"redis", redis, []string{key}, "client-key-file", "client-key-file read only beside client-cert-file"},
		{"vault", https, []string{cert}, "client-cert-file", "client-cert-file read only beside client-key-file"},
		{"vault", https, []string{key}, "client-key-file", "client-key-file read only beside client-cert-file"},
		{"redis", redis, []string{`tls=false`, ca}, "ca-file", "ca-file read only under tls = true"},
		{"redis", redis, []string{`tls=false`, cert, key}, "client-cert-file", "client-cert-file read only under tls = true"},
		{"redis", redis, []string{`tls=false`, `tls-verify=true`}, "tls-verify", "tls-verify read only under tls = true"},
		{"vault", plain, []string{ca}, "ca-file", "ca-file read only for an https address"},
		{"vault", plain, []string{cert, key}, "client-cert-file", "client-cert-file read only for an https address"},
		{"vault", plain, []string{`tls-verify=false`}, "tls-verify", "tls-verify read only for an https address"},
		{"redis", redis, []string{`tls-verify=false`, ca}, "ca-file", "ca-file read only under tls-verify = true"},
		{"vault", https, []string{`tls-verify=false`, ca}, "ca-file", "ca-file read only under tls-verify = true"},
	} {
		refused(t, tc.typ+" "+strings.Join(tc.sets, " "), load(tc.typ, tc.addr, tc.sets...), "config_credential_backend_key_unsupported", "credential-backend.s."+tc.key, tc.msg)
	}
	for _, tc := range []struct {
		typ, addr string
		sets      []string
	}{
		{"redis", redis, []string{`auth-username-env="RU"`, `auth-password-env="RP"`, ca, cert, key, `tls-verify=true`}},
		{"redis", redis, []string{`tls=false`, `auth-password-env="RP"`}},
		{"redis", redis, []string{`tls-verify=false`, cert, key}},
		{"vault", https, []string{ca, cert, key}},
		{"vault", https, []string{`tls-verify=false`}},
		{"vault", plain, nil},
	} {
		if err := load(tc.typ, tc.addr, tc.sets...); err != nil {
			t.Errorf("%s %v: %v", tc.typ, tc.sets, err)
		}
	}
}

// TestStoreBackendEmpty is EXAMPLES chapter 56, issue 17: a key whose
// default is a value refuses "", its value spelling the default; a key whose
// default is none takes "" as that none, the same as the key left out and
// never a key left unread, so a later layer says none over an earlier one's
// value, which it cannot remove.
func TestStoreBackendEmpty(t *testing.T) {
	load := func(typ, addr string, sets ...string) error {
		all := []string{`credential-backend.s.type="` + typ + `"`}
		if addr != "" {
			all = append(all, `credential-backend.s.address="`+addr+`"`)
		}
		for _, s := range sets {
			all = append(all, "credential-backend.s."+s)
		}
		_, err := Load(Options{HomeDir: t.TempDir(), Operator: "alice", SkipAuto: true, Environment: []string{}, Sets: all})
		return err
	}
	const redis, plain, https = "127.0.0.1:6379", "http://127.0.0.1:8201", "https://127.0.0.1:8200"
	for _, tc := range []struct{ typ, addr, key string }{
		{"vault", https, "mount"}, {"vault", https, "path-template"}, {"vault", https, "username-field"}, {"vault", https, "password-field"}, {"vault", https, "enable-field"},
		{"redis", redis, "key-template"}, {"redis", redis, "transform"}, {"vault", https, "transform"}, {"env", "", "transform"},
	} {
		refused(t, tc.typ+" "+tc.key, load(tc.typ, tc.addr, tc.key+`=""`), "config_type_error", "credential-backend.s."+tc.key, "must not be empty")
	}
	refused(t, "token-env", load("vault", https, `token-env=""`), "config_credential_backend_variable_invalid", "credential-backend.s.token-env", `"" not a variable name (letters, digits, underscores; no leading digit)`)
	for _, tc := range []struct {
		typ, addr string
		sets      []string
	}{
		{"vault", https, []string{`namespace=""`, `ca-file=""`, `client-cert-file=""`, `client-key-file=""`}},
		{"vault", plain, []string{`ca-file=""`, `client-cert-file=""`, `client-key-file=""`}},
		{"vault", https, []string{`tls-verify=false`, `ca-file=""`}},
		{"redis", redis, []string{`tls=false`, `ca-file=""`, `client-cert-file=""`, `client-key-file=""`}},
		{"redis", redis, []string{`auth-username-env=""`, `auth-password-env=""`}},
		{"redis", redis, []string{`auth-username-env=""`}},
		{"env", "", []string{`username-var-template=""`, `password-var-template=""`, `enable-var-template=""`}},
	} {
		if err := load(tc.typ, tc.addr, tc.sets...); err != nil {
			t.Errorf("%s %v: %v", tc.typ, tc.sets, err)
		}
	}
	// A client certificate beside an empty key has no key.
	refused(t, "cert beside key \"\"", load("vault", https, `client-cert-file="/etc/karvi/c.pem"`, `client-key-file=""`), "config_credential_backend_key_unsupported", "credential-backend.s.client-cert-file", "client-cert-file read only beside client-key-file")
	refused(t, "username beside password \"\"", load("redis", redis, `auth-username-env="RU"`, `auth-password-env=""`), "config_credential_backend_key_unsupported", "credential-backend.s.auth-username-env", "auth-username-env read only beside auth-password-env")
	// Two layers: the later sets "" over the earlier's values, which it
	// cannot remove, and the backend loads with none.
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.toml"), filepath.Join(dir, "second.toml")
	if err := os.WriteFile(first, []byte("[credential-backend.v]\ntype = \"vault\"\naddress = \"http://127.0.0.1:8201\"\nnamespace = \"team\"\nmount = \"kv\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("[credential-backend.v]\naddress = \"https://127.0.0.1:8200\"\nnamespace = \"\"\nmount = \"secret\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := Load(Options{HomeDir: t.TempDir(), Operator: "alice", SkipAuto: true, Environment: []string{}, ExplicitRoots: []string{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := snap.Values["credential-backend.v.namespace"]; !ok || v.Data != "" || !strings.HasSuffix(v.Source.Path, "second.toml") || snap.String("credential-backend.v.mount") != "secret" {
		t.Errorf("layers: namespace %+v, mount %q", v, snap.String("credential-backend.v.mount"))
	}
}

// TestCredentialHelperKeys is EXAMPLES chapter 56, issues 9 and 12: the
// helper's four keys, their defaults and bounds; creds.helper a bare name
// kept as written, any other value a place made absolute at the load.
func TestCredentialHelperKeys(t *testing.T) {
	home := t.TempDir()
	load := func(sets ...string) (Snapshot, error) {
		return Load(Options{HomeDir: home, Operator: "alice", SkipAuto: true, Environment: []string{}, Sets: sets})
	}
	snap, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.String("creds.helper") != "karvi-credh" || !snap.Bool("creds.helper-enabled") || snap.String("creds.helper-unavailable") != "skip" || snap.String("creds.helper-timeout") != "60s" {
		t.Errorf("defaults: %q %v %q %q", snap.String("creds.helper"), snap.Bool("creds.helper-enabled"), snap.String("creds.helper-unavailable"), snap.String("creds.helper-timeout"))
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for value, want := range map[string]string{
		"credh":           "credh",
		"./credh":         filepath.Join(wd, "credh"),
		"lib/credh":       filepath.Join(wd, "lib/credh"),
		"~/bin/credh":     filepath.Join(home, "bin/credh"),
		"/opt/karvi/../x": "/opt/x",
	} {
		snap, err := load(`creds.helper="` + value + `"`)
		if err != nil || snap.String("creds.helper") != want {
			t.Errorf("%s: %q %v, want %q", value, snap.String("creds.helper"), err, want)
		}
	}
	_, err = load(`creds.helper="~other/credh"`)
	if got := errorcodes.Of(err); got != "path_other_user_home_unsupported" {
		t.Errorf("~other: %q (%v)", got, err)
	}
	// The path has a value for a default; its off switch is
	// creds.helper-enabled (issue 17).
	_, err = load(`creds.helper=""`)
	refused(t, `creds.helper=""`, err, "config_type_error", "creds.helper", "must not be empty")
	for _, tc := range []struct{ set, code string }{
		{`creds.helper-unavailable="maybe"`, "config_enum_value_invalid"},
		{`creds.helper-enabled="yes"`, "config_type_error"},
		{`creds.helper-timeout="500ms"`, "config_value_out_of_range"},
		{`creds.helper-timeout="61m"`, "config_value_out_of_range"},
	} {
		if _, err := load(tc.set); errorcodes.Of(err) != tc.code {
			t.Errorf("%s: %v, want %s", tc.set, err, tc.code)
		}
	}
	if _, err := loadAs(t, "alice", "[creds]\nhelper = 5\n"); errorcodes.Of(err) != "config_type_error" {
		t.Errorf("helper = 5: %v", err)
	}
	for _, set := range []string{`creds.helper-unavailable="warn"`, `creds.helper-unavailable="fail"`, `creds.helper-timeout="1s"`, `creds.helper-timeout="1h"`, `creds.helper-enabled=false`} {
		if _, err := load(set); err != nil {
			t.Errorf("%s: %v", set, err)
		}
	}
}
