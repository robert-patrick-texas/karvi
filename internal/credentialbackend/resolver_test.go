package credentialbackend

import (
	"context"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
	"github.com/robert-patrick-texas/karvi/inventory"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinEnvironment(t *testing.T) {
	t.Setenv("NETUSER", "operator1")
	t.Setenv("NETPASS", "pw")
	cfg, err := configload.Load(configload.Options{SkipAuto: true, Environment: os.Environ(), Sets: []string{"ssh.pubkey-authentication=true", "creds.interactive-prompt=false"}})
	if err != nil {
		t.Fatal(err)
	}
	op := credentials.Operator{Username: "human", UID: os.Getuid(), Home: t.TempDir()}
	r, err := New(cfg, op, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := inventory.Direct("127.0.0.1", "linux", "system", 22)
	got, err := r.Resolve(context.Background(), op, d)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceUsername != "operator1" {
		t.Fatalf("user=%s", got.DeviceUsername)
	}
	got.Credential.Material.Destroy()
}
func TestRuleNegation(t *testing.T) {
	r := &Resolver{policies: map[string]policy{"default": {Name: "default"}, "prod": {Name: "prod"}}, rules: []map[string]any{
		{"name": []any{"*", "!lab*"}, "site": "prod", "policy": "prod"},
		{"name": "*", "policy": "default"},
	}}
	d := inventory.Direct("sw1", "generic", "system", 22)
	d.Site = "PROD"
	if p, err := r.selectPolicy(d); err != nil || p.Name != "prod" {
		t.Fatalf("site folds case: %+v %v", p, err)
	}
	d.CanonicalName = "lab1"
	if p, err := r.selectPolicy(d); err != nil || p.Name != "default" {
		t.Fatalf("negative should win: %+v %v", p, err)
	}
}

// TestSelectPolicyErrors: equal
// prefixes name the rules and their sources; a malformed pattern that reached
// the resolver is config_match_rule_pattern_invalid.
func TestSelectPolicyErrors(t *testing.T) {
	cfg := configload.Snapshot{Values: map[string]configload.Value{
		"credential-policy-map.0.policy": {Source: configload.SourceRef{Path: "/etc/karvi/karvi.toml", Line: 40}},
	}}
	d := inventory.Direct("sw1", "generic", "system", 22)
	d.ManagementAddress = netip.MustParseAddr("10.1.2.3")
	r := &Resolver{cfg: cfg, policies: map[string]policy{"default": {Name: "default"}}, rules: []map[string]any{
		{"address-cidr": "10.1.0.0/16", "policy": "default"},
		{"address-cidr": "10.1.0.0/16", "site": "*", "policy": "default"},
		{"name": "*", "policy": "default"},
	}}
	_, err := r.selectPolicy(d)
	if errorcodes.Of(err) != "credential_policy_prefix_ambiguous" || !strings.Contains(err.Error(), "credential-policy-map.0 (/etc/karvi/karvi.toml:40), credential-policy-map.1") {
		t.Fatalf("ambiguous: %v", err)
	}
	r.rules = []map[string]any{{"site": "ny[c", "policy": "default"}, {"name": "*", "policy": "default"}}
	if _, err := r.selectPolicy(d); errorcodes.Of(err) != "config_match_rule_pattern_invalid" {
		t.Fatalf("malformed: %v", err)
	}
}

// TestEnableSecretOptionalByDefault: no
// built-in or alias requires an enable secret; requires-enable = true on a
// platform or an alias does.
func TestEnableSecretOptionalByDefault(t *testing.T) {
	t.Setenv("NETUSER", "operator1")
	t.Setenv("NETPASS", "pw")
	os.Unsetenv("NETENABLE")
	resolve := func(t *testing.T, platform string, sets ...string) error {
		t.Helper()
		cfg, err := configload.Load(configload.Options{SkipAuto: true, Environment: os.Environ(), Sets: append([]string{"creds.interactive-prompt=false"}, sets...)})
		if err != nil {
			t.Fatal(err)
		}
		op := credentials.Operator{Username: "human", UID: os.Getuid(), Home: t.TempDir()}
		r, err := New(cfg, op, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := r.Resolve(context.Background(), op, inventory.Direct("127.0.0.1", platform, "system", 22))
		if err == nil {
			got.Credential.Material.Destroy()
		}
		return err
	}
	for _, p := range []string{"cisco_iosxe", "cisco_nxos", "arista_eos", "c9300"} {
		if err := resolve(t, p, `platform.c9300.driver="cisco_iosxe"`); err != nil {
			t.Fatalf("%s without an enable secret: %v", p, err)
		}
	}
	if err := resolve(t, "cisco_iosxe", "platform.cisco_iosxe.requires-enable=true"); errorcodes.Of(err) != "credential_enable_missing" {
		t.Fatalf("required on the built-in: %v", err)
	}
	if err := resolve(t, "c9300", `platform.c9300.driver="cisco_iosxe"`, "platform.c9300.requires-enable=true"); errorcodes.Of(err) != "credential_enable_missing" {
		t.Fatalf("required on the alias: %v", err)
	}
	if err := resolve(t, "c9300", `platform.c9300.driver="cisco_iosxe"`, "platform.cisco_iosxe.requires-enable=true"); err != nil {
		t.Fatalf("an alias does not read its built-in's table: %v", err)
	}
}

// TestEnableRuleReadsThePlatformUsed: the
// enable rule reads the device view's PlatformUsed, the plan's platform,
// so a not-set row under a default that requires an enable secret needs
// one, and a set platform that requires one does not when the platform
// used does not; with PlatformUsed empty the set platform stands in.
func TestEnableRuleReadsThePlatformUsed(t *testing.T) {
	cfg, err := configload.Load(configload.Options{SkipAuto: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_iosxe"`, "platform.c9300.requires-enable=true"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		set, used string
		want      bool
	}{
		{"", "c9300", true},           // a blank row under default = c9300
		{"", "generic", false},        // a blank row under no default
		{"cisco_iosx", "c9300", true}, // an unknown row fallen back to c9300 under warn
		{"c9300", "generic", false},   // the platform used decides, not the set one
		{"c9300", "", true},           // outside planning the set platform stands in
		{"", "", false},
	} {
		d := inventory.Direct("127.0.0.1", tc.set, "system", 22)
		d.PlatformUsed = tc.used
		if got := requiresEnable(d, cfg); got != tc.want {
			t.Errorf("set %q used %q: %v, want %v", tc.set, tc.used, got, tc.want)
		}
	}
}

// TestCredentialCSVInThePolicy covers the credential CSV through the resolver:
// the first matching row is the answer and the ordinary completeness rules
// judge it, never a later row; no matching row moves the policy to its next
// backend; a load failure stops the resolution under the file's code.
func TestCredentialCSVInThePolicy(t *testing.T) {
	t.Setenv("NETUSER", "fallback-user")
	t.Setenv("NETPASS", "fallback-pass")
	os.Unsetenv("NETENABLE")
	home := t.TempDir()
	file := filepath.Join(home, "credentials.csv")
	body := "device_name,username,password,enable_password\n" +
		"sw-nokey-*,svc.nopass,,\n" + // line 2: no password
		"sw-*,svc.switch,switch-pass,\n" + // line 3: no enable; also matches sw-nokey-*
		"rt-*,,router-pass,router-enable\n" // line 4: the operator's own name
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	resolve := func(t *testing.T, d inventory.Device, sets ...string) (credentials.Resolved, error) {
		t.Helper()
		base := []string{"creds.interactive-prompt=false", `credential-backend.creds.type="csv"`, `credential-backend.creds.scope="user"`, `credential-backend.creds.path="` + file + `"`, `creds.backend-sequence=["creds"]`}
		cfg, err := configload.Load(configload.Options{SkipAuto: true, Environment: os.Environ(), Sets: append(base, sets...)})
		if err != nil {
			t.Fatal(err)
		}
		op := credentials.Operator{Username: "human", UID: os.Getuid(), Home: home}
		r, err := New(cfg, op, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r.Resolve(context.Background(), op, d)
	}
	user := func(t *testing.T, got credentials.Resolved, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer got.Credential.Material.Destroy()
		return got.DeviceUsername + " via " + got.Credential.Backend
	}
	ssh := func(name string) inventory.Device { return inventory.Direct(name, "cisco_iosxe", "system", 22) }

	got, err := resolve(t, ssh("sw-nyc-01"))
	if u := user(t, got, err); u != "svc.switch via creds" {
		t.Errorf("a switch: %s", u)
	}
	got, err = resolve(t, ssh("rt-nyc-01"))
	if u := user(t, got, err); u != "human via creds" {
		t.Errorf("a blank username cell: %s", u)
	}
	// A username with no password is an answer where public-key login
	// needs none ...
	got, err = resolve(t, ssh("sw-nokey-01"), "ssh.pubkey-authentication=true")
	if u := user(t, got, err); u != "svc.nopass via creds" {
		t.Errorf("no password, public key allowed: %s", u)
	}
	// ... and credential_password_missing where a password is needed, as
	// it is by default (ssh.pubkey-authentication is false unless set). The
	// row on line 3 also matches and has a password: it is not consulted.
	if _, err = resolve(t, ssh("sw-nokey-01")); errorcodes.Of(err) != "credential_password_missing" {
		t.Errorf("no password, password login only: %v", err)
	}
	if _, err = resolve(t, ssh("sw-nyc-01"), "platform.cisco_iosxe.requires-enable=true"); errorcodes.Of(err) != "credential_enable_missing" {
		t.Errorf("no enable where the platform requires one: %v", err)
	}
	got, err = resolve(t, ssh("rt-nyc-01"), "platform.cisco_iosxe.requires-enable=true")
	if u := user(t, got, err); u != "human via creds" {
		t.Errorf("a row with an enable: %s", u)
	}
	// No matching row: the sequence ends and the built-in fallback answers.
	got, err = resolve(t, ssh("fw-nyc-01"))
	if u := user(t, got, err); u != "fallback-user via builtin-env-fallback" {
		t.Errorf("no matching row: %s", u)
	}
	// A present file that fails its check stops the resolution; it is not
	// a not-found.
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = resolve(t, ssh("fw-nyc-01")); errorcodes.Of(err) != "credential_file_mode_unsafe" {
		t.Errorf("mode 0644: %v", err)
	}
}

// TestCredKeyRefPin is the resolver's walk for a
// device pinned by the inventory's credkeyref: only backends that can
// honour a key are asked, a keyed backend without the key is a not-found
// and the walk goes on, and the end of the sequence is
// credkeyref_unresolved, before the built-in fallback and the prompt. The
// sequence here is an env backend (not keyed), a formula over the first
// CSV (not keyed), then two CSV files.
func TestCredKeyRefPin(t *testing.T) {
	t.Setenv("NETUSER", "fallback-user")
	t.Setenv("NETPASS", "fallback-pass")
	t.Setenv("KARVI_human_USERNAME", "env-user")
	t.Setenv("KARVI_human_PASSWORD", "env-pass")
	os.Unsetenv("NETENABLE")
	home := t.TempDir()
	first, second := filepath.Join(home, "first.csv"), filepath.Join(home, "second.csv")
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(first, "device_name,site,credkey,username,password\n"+
		",,core-admin,svc.core,core-pass\n"+ // line 2: a key-only row, pinned devices alone
		",nyc,nyc-ro,svc.nyc,nyc-pass\n"+ // line 3: a key and a site
		",,no-pass,svc.nopass,\n"+ // line 4: the key's row is incomplete
		"*,,,svc.general,general-pass\n") // line 5: the catch-all
	write(second, "device_name,credkey,username,password\n"+
		",edge-admin,svc.edge,edge-pass\n"+
		"*,,svc.second,second-pass\n")
	resolve := func(t *testing.T, d inventory.Device, sequence string) (credentials.Resolved, error) {
		t.Helper()
		sets := []string{"creds.interactive-prompt=false",
			`credential-backend.operator-env.type="env"`, `credential-backend.operator-env.username-var-template="KARVI_%s_USERNAME"`, `credential-backend.operator-env.password-var-template="KARVI_%s_PASSWORD"`,
			`credential-backend.first.type="csv"`, `credential-backend.first.scope="user"`, `credential-backend.first.path="` + first + `"`,
			`credential-backend.second.type="csv"`, `credential-backend.second.scope="user"`, `credential-backend.second.path="` + second + `"`,
			`credential-backend.shaped.type="formula"`, `credential-backend.shaped.username-template="%s-adm"`, `credential-backend.shaped.password-source="first"`,
			`creds.backend-sequence=` + sequence}
		cfg, err := configload.Load(configload.Options{SkipAuto: true, Environment: os.Environ(), Sets: sets})
		if err != nil {
			t.Fatal(err)
		}
		op := credentials.Operator{Username: "human", UID: os.Getuid(), Home: home}
		r, err := New(cfg, op, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r.Resolve(context.Background(), op, d)
	}
	answer := func(t *testing.T, got credentials.Resolved, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer got.Credential.Material.Destroy()
		return got.DeviceUsername + " via " + got.Credential.Backend + " key " + got.Credential.MatchedOn.CredKey
	}
	device := func(name, site, ref string) inventory.Device {
		d := inventory.Direct(name, "cisco_iosxe", "system", 22)
		d.Site, d.CredKeyRef = site, ref
		return d
	}
	const all = `["operator-env", "shaped", "first", "second"]`

	// An unpinned device is untouched by all this: the env backend, first
	// in the sequence, answers it.
	got, err := resolve(t, device("sw-nyc-01", "nyc", ""), all)
	if a := answer(t, got, err); a != "env-user via operator-env key " {
		t.Errorf("unpinned: %s", a)
	}
	// A pinned device skips the env backend and the formula, and takes the
	// key's row; the key folds case.
	got, err = resolve(t, device("sw-nyc-01", "nyc", "Core-Admin"), all)
	if a := answer(t, got, err); a != "svc.core via first key core-admin" {
		t.Errorf("pinned to a key-only row: %s", a)
	}
	// The first keyed backend lacks the key: not found, and the walk goes
	// on to the next keyed backend, still looking for the key.
	got, err = resolve(t, device("sw-nyc-01", "nyc", "edge-admin"), all)
	if a := answer(t, got, err); a != "svc.edge via second key edge-admin" {
		t.Errorf("the key in the second file: %s", a)
	}
	// No backend holds the key: an error naming the device, the key, and
	// where it was looked for. Neither catch-all row, nor the env backend,
	// nor the built-in fallback (NETUSER and NETPASS are set) answers.
	_, err = resolve(t, device("sw-nyc-01", "nyc", "egde-admin"), all)
	if errorcodes.Of(err) != "credkeyref_unresolved" {
		t.Fatalf("a mistyped key: %v", err)
	}
	for _, want := range []string{"sw-nyc-01", `"egde-admin"`, "asked: first, second", "cannot honour a key: operator-env, shaped", "policy=default"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message lacks %q: %v", want, err)
		}
	}
	// The key's row still applies its other cells (line 3 wants site nyc):
	// a pinned device elsewhere matches nothing and is unresolved.
	got, err = resolve(t, device("sw-nyc-01", "nyc", "nyc-ro"), all)
	if a := answer(t, got, err); a != "svc.nyc via first key nyc-ro" {
		t.Errorf("a key and a matching site: %s", a)
	}
	if _, err = resolve(t, device("sw-bos-01", "bos", "nyc-ro"), all); errorcodes.Of(err) != "credkeyref_unresolved" {
		t.Errorf("a key whose row's site disagrees: %v", err)
	}
	// The key's row is judged by the ordinary completeness rules and is
	// never passed over for another row or another backend.
	if _, err = resolve(t, device("sw-nyc-01", "nyc", "no-pass"), all); errorcodes.Of(err) != "credential_password_missing" {
		t.Errorf("the key's row has no password: %v", err)
	}
	// A sequence with no keyed backend at all: unresolved, asked none.
	_, err = resolve(t, device("sw-nyc-01", "nyc", "core-admin"), `["operator-env"]`)
	if errorcodes.Of(err) != "credkeyref_unresolved" || !strings.Contains(err.Error(), "asked: none") {
		t.Errorf("no keyed backend in the sequence: %v", err)
	}
	// A keyed backend's failure stops a pinned device as it stops any
	// other: a present file that fails its check is not a not-found.
	if err := os.Chmod(first, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = resolve(t, device("sw-nyc-01", "nyc", "edge-admin"), all); errorcodes.Of(err) != "credential_file_mode_unsafe" {
		t.Errorf("an unsafe first file: %v", err)
	}
}

// TestKeyedIsACapability: the resolver asks whatever declares
// credentials.Keyed, not a backend type (a later keyed store
// joins by declaring it), and believes a backend that declares false.
func TestKeyedIsACapability(t *testing.T) {
	cfg, err := configload.Load(configload.Options{SkipAuto: true, Environment: []string{}, Sets: []string{"creds.interactive-prompt=false", `creds.backend-sequence=["plain", "unkeyed", "store"]`}})
	if err != nil {
		t.Fatal(err)
	}
	op := credentials.Operator{Username: "human", UID: os.Getuid(), Home: t.TempDir()}
	r, err := New(cfg, op, nil)
	if err != nil {
		t.Fatal(err)
	}
	plain := &stubBackend{name: "plain"}
	unkeyed := &stubKeyed{stubBackend: stubBackend{name: "unkeyed"}, keyed: false}
	store := &stubKeyed{stubBackend: stubBackend{name: "store"}, keyed: true}
	r.backends["plain"], r.backends["unkeyed"], r.backends["store"] = plain, unkeyed, store
	for _, name := range []string{"plain", "unkeyed", "store"} {
		r.backendProfile[name] = "default"
	}
	d := inventory.Direct("sw-nyc-01", "cisco_iosxe", "system", 22)
	d.CredKeyRef = "core-admin"
	got, err := r.Resolve(context.Background(), op, d)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Credential.Material.Destroy()
	if got.Credential.Backend != "store" || plain.asked != 0 || unkeyed.asked != 0 || store.asked != 1 {
		t.Fatalf("backend=%s asked plain=%d unkeyed=%d store=%d", got.Credential.Backend, plain.asked, unkeyed.asked, store.asked)
	}
	// Unpinned, the sequence is walked in order and the first answers.
	d.CredKeyRef = ""
	got2, err := r.Resolve(context.Background(), op, d)
	if err != nil {
		t.Fatal(err)
	}
	defer got2.Credential.Material.Destroy()
	if got2.Credential.Backend != "plain" {
		t.Fatalf("unpinned: backend=%s", got2.Credential.Backend)
	}
}

// stubBackend answers every request with one credential and counts.
type stubBackend struct {
	name  string
	asked int
}

func (s *stubBackend) Name() string                  { return s.name }
func (s *stubBackend) Mode() credentials.BackendMode { return credentials.DeviceKeyed }
func (s *stubBackend) Resolve(context.Context, credentials.ResolveRequest) credentials.BackendResult {
	s.asked++
	return credentials.BackendResult{Outcome: credentials.Success, Credential: credentials.Credential{Material: secrets.NewMaterial("svc."+s.name, "pass", ""), Backend: s.name}}
}

// stubKeyed declares the capability, true or false.
type stubKeyed struct {
	stubBackend
	keyed bool
}

func (s *stubKeyed) HonoursCredKey() bool { return s.keyed }
