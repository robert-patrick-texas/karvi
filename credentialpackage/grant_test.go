package credentialpackage

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
)

const fixtureGrantID = "20260914T120003.000000+0000-0123456789abcdefghjk"

// fixtureGrant mirrors the smoke harness credentials (NETUSER=u, NETPASS=p)
// bound to client-authority targets.
func fixtureGrant(password string) CredentialGrant {
	return CredentialGrant{
		CredentialID: fixtureGrantID, Method: MethodEmbeddedSecret,
		Username: credentials.NewSecretString("u"), Password: credentials.NewSecretString(password),
		Policy: "default", Backend: "env",
		MatchedOn: credentials.Match{Category: "operator", SafeValue: "netops"},
		Scope:     CredentialScope{TargetIDs: []string{"name:127.0.0.1", "name:edge-b"}, Transports: []string{"system"}, Ports: []uint16{22}},
		NotBefore: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC), NotAfter: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	}
}

func TestGrantValidatesAndProjects(t *testing.T) {
	g := fixtureGrant("p")
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	p, err := g.SafeProjection()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("safe projection:\n%s", data)
	if p.DeviceUsername != "u" || strings.Contains(string(data), `"p"`) || strings.Contains(string(data), "password") {
		t.Fatalf("projection: %s", data)
	}
	if !g.Scope.Covers("name:edge-b", "system", 22) || g.Scope.Covers("name:edge-b", "native", 22) || g.Scope.Covers("name:core-a", "system", 22) || g.Scope.Covers("name:edge-b", "system", 23) {
		t.Error("Covers")
	}
	if !g.Equivalent(fixtureGrant("p")) || g.Equivalent(fixtureGrant("q")) {
		t.Error("Equivalent")
	}
	keyed := fixtureGrant("p")
	keyed.Keys = []credentials.KeyRef{{Path: "/home/netops/.ssh/id_ed25519", Fingerprint: "SHA256:lCkD25f/uZQGbWYmns4BurmVr65NAa+wSHkq5Y/lnVk"}}
	if g.Equivalent(keyed) {
		t.Error("Equivalent must compare the keys")
	}
	if kp, _ := keyed.SafeProjection(); len(kp.Keys) != 1 || kp.Keys[0] != keyed.Keys[0] {
		t.Errorf("projection keys: %+v", kp.Keys)
	}
	other := fixtureGrant("p")
	other.Policy = "site-a"
	if !g.Equivalent(other) {
		t.Error("Equivalent must ignore non-credential metadata")
	}
	g.Destroy()
	if g.Password.IsSet() || g.Username.IsSet() {
		t.Error("Destroy left a secret set")
	}
	if _, err := g.SafeProjection(); err != nil {
		t.Errorf("projection after destroy must still work without the username: %v", err)
	}
}

func TestGrantValidationVectors(t *testing.T) {
	cases := []struct {
		name string
		edit func(*CredentialGrant)
		rule string
	}{
		{"id", func(g *CredentialGrant) { g.CredentialID = "cred-1" }, "grant_id"},
		{"method", func(g *CredentialGrant) { g.Method = "plaintext" }, "grant_method"},
		{"embedded without username", func(g *CredentialGrant) { g.Username = credentials.SecretString{} }, "grant_username"},
		{"embedded with reference", func(g *CredentialGrant) { g.Reference = SecretReference{Provider: "vault", Locator: "x"} }, "grant_reference"},
		{"reference without locator", func(g *CredentialGrant) {
			g.Method = MethodCredentialReference
			g.Username, g.Password = credentials.SecretString{}, credentials.SecretString{}
			g.Reference = SecretReference{Provider: "vault"}
		}, "grant_reference"},
		{"reference with embedded secret", func(g *CredentialGrant) {
			g.Method = MethodSigningAgentDelegation
			g.Reference = SecretReference{Provider: "agent", Locator: "key-1"}
		}, "grant_embedded"},
		{"no targets", func(g *CredentialGrant) { g.Scope.TargetIDs = nil }, "grant_scope"},
		{"unsorted targets", func(g *CredentialGrant) { g.Scope.TargetIDs = []string{"name:b", "name:a"} }, "grant_scope"},
		{"unknown transport", func(g *CredentialGrant) { g.Scope.Transports = []string{"serial"} }, "grant_scope"},
		{"zero port", func(g *CredentialGrant) { g.Scope.Ports = []uint16{0} }, "grant_scope"},
		{"window reversed", func(g *CredentialGrant) { g.NotAfter = g.NotBefore }, "grant_window"},
		{"window unset", func(g *CredentialGrant) { g.NotBefore = time.Time{} }, "grant_window"},
	}
	for _, c := range cases {
		g := fixtureGrant("p")
		c.edit(&g)
		err := g.Validate()
		if err == nil || !strings.HasPrefix(err.Error(), "credential_package_invalid: rule="+c.rule+":") {
			t.Errorf("%s: want rule %s, got %v", c.name, c.rule, err)
		}
	}
	// An empty password is valid for key-only transports.
	g := fixtureGrant("")
	if err := g.Validate(); err != nil {
		t.Errorf("empty password: %v", err)
	}
	// A reference grant is valid without any embedded secret.
	r := fixtureGrant("")
	r.Method, r.Username, r.Reference = MethodCredentialReference, credentials.SecretString{}, SecretReference{Provider: "vault", Locator: "secret/karvi/u", KeyID: "v3"}
	if err := r.Validate(); err != nil {
		t.Errorf("reference grant: %v", err)
	}
}

func TestGrantRefusesEverySink(t *testing.T) {
	seed := canarytest.Seed(t)
	g := fixtureGrant(seed.Raw)
	g.EnablePassword = credentials.NewSecretString(seed.Raw)
	canarytest.Exercise(t, g, canary.Refusing, seed)
	canarytest.Exercise(t, struct{ g CredentialGrant }{g}, canary.Hidden, seed)
	canarytest.Exercise(t, struct{ G CredentialGrant }{g}, canary.Refusing, seed)
	canarytest.Exercise(t, []CredentialGrant{g}, canary.Refusing, seed)
	canarytest.PanicOutput(t, "TestCanaryPanicHelper", seed)
}

// TestCanaryPanicHelper panics with a grant when PanicOutput re-executes the
// binary; otherwise it does nothing.
func TestCanaryPanicHelper(t *testing.T) {
	seed, ok := canarytest.PanicRequested()
	if !ok {
		t.Skip("only under PanicOutput")
	}
	g := fixtureGrant(seed.Raw)
	panic(struct{ G CredentialGrant }{g})
}

func TestSafeTypesAreStructurallyNonSecret(t *testing.T) {
	allow := canarytest.Allow{Leaves: []string{"time.Time", "github.com/robert-patrick-texas/karvi/credentials.Match"}}
	for _, rt := range []reflect.Type{reflect.TypeOf(GrantProjection{}), reflect.TypeOf(SecretReference{}), reflect.TypeOf(CredentialScope{})} {
		canarytest.Walk(t, rt, allow)
	}
}

func TestWalkerRejectsTheGrant(t *testing.T) {
	// The walker must recognise the grant as secret-bearing; a recorder
	// stands in for testing.TB.
	rec := &recorder{TB: t}
	canarytest.Walk(rec, reflect.TypeOf(CredentialGrant{}), canarytest.Allow{Leaves: []string{"time.Time", "github.com/robert-patrick-texas/karvi/credentials.Match"}})
	if rec.errors == 0 {
		t.Fatal("the walker accepted CredentialGrant")
	}
}

type recorder struct {
	testing.TB
	errors int
}

func (r *recorder) Errorf(string, ...any) { r.errors++ }
func (r *recorder) Error(...any)          { r.errors++ }
