package credentialpackage

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
)

const (
	fixturePackageID = "20260914T120005.000000+0000-0123456789abcdefghjk"
	audienceLocal    = "daemon:ops01:1000"
	envelopeSchema   = "../schema/credential-package-envelope.schema.json"
)

var now = time.Date(2026, 9, 14, 12, 0, 10, 0, time.UTC)

// fixturePackage binds the two client-authority targets to grant A (u/p)
// and core-a to grant B (admin/q).
func fixturePackage(passwordA string) CredentialPackage {
	a := CredentialGrant{
		CredentialID: plantest.GrantA, Method: MethodEmbeddedSecret,
		Username: credentials.NewSecretString("u"), Password: credentials.NewSecretString(passwordA),
		Policy: "default", Backend: "env", MatchedOn: credentials.Match{Category: "operator", SafeValue: "netops"},
		Scope:     CredentialScope{TargetIDs: []string{"name:127.0.0.1", "name:edge-b"}, Transports: []string{"system"}, Ports: []uint16{22}},
		NotBefore: plantest.DraftedAt, NotAfter: plantest.DraftedAt.Add(12 * time.Hour),
	}
	b := CredentialGrant{
		CredentialID: plantest.GrantB, Method: MethodEmbeddedSecret,
		Username: credentials.NewSecretString("admin"), Password: credentials.NewSecretString("q"), EnablePassword: credentials.NewSecretString("en"),
		Policy: "core", Backend: "cloginrc", MatchedOn: credentials.Match{Category: "device", Pattern: "core-*", CredKey: "core-admin"},
		Scope:     CredentialScope{TargetIDs: []string{"name:core-a"}, Transports: []string{"system"}, Ports: []uint16{22}},
		NotBefore: plantest.DraftedAt, NotAfter: plantest.DraftedAt.Add(12 * time.Hour),
	}
	final := plantest.FinalPlan()
	return CredentialPackage{
		SchemaVersion: SchemaVersion, PackageID: fixturePackageID, JobID: plantest.JobID, PlanDigest: final.PlanDigest,
		Issuer:   Principal{Kind: IssuerOperator, Username: "netops", UID: 1000, Hostname: "ops01"},
		Audience: []string{audienceLocal}, Protection: ProtectionLocalPeer,
		IssuedAt: plantest.FinalizedAt, ExpiresAt: plantest.FinalizedAt.Add(MaxPackageWindow),
		Grants:   []CredentialGrant{a, b},
		Bindings: []TargetCredentialBinding{{"name:127.0.0.1", plantest.GrantA}, {"name:edge-b", plantest.GrantA}, {"name:core-a", plantest.GrantB}},
	}
}

func TestPackageValidatesProjectsAndDigestIgnoresSecrets(t *testing.T) {
	plan := plantest.FinalPlan()
	header := plantest.Header(plan, executionplan.Committed)
	p := fixturePackage("p")
	if err := p.Validate(&plan, header, audienceLocal, now); err != nil {
		t.Fatal(err)
	}
	proj, err := p.SafeProjection()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(proj, "", "  ")
	t.Logf("safe projection:\n%s", data)
	env := proj.Envelope()
	if err := env.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := env.Matches(proj); err != nil {
		t.Fatal(err)
	}
	edata, _ := json.MarshalIndent(env, "", "  ")
	t.Logf("local-peer envelope:\n%s", edata)
	for _, forbidden := range []string{`"p"`, `"q"`, `"en"`, "password", "ciphertext"} {
		if strings.Contains(string(data), forbidden) || strings.Contains(string(edata), forbidden) {
			t.Errorf("projection or envelope contains %s", forbidden)
		}
	}
	// The digest never covers secret bytes: a password change leaves it
	// unchanged, any projection field change moves it.
	mutated := fixturePackage("different-password")
	mp, _ := mutated.SafeProjection()
	if mp.PackageDigest != proj.PackageDigest {
		t.Errorf("digest moved with a password change: %s vs %s", mp.PackageDigest, proj.PackageDigest)
	}
	t.Logf("digest before password change %s\ndigest after  password change %s", proj.PackageDigest, mp.PackageDigest)
	renamed := fixturePackage("p")
	renamed.Grants[0].Policy = "other"
	rp, _ := renamed.SafeProjection()
	if rp.PackageDigest == proj.PackageDigest {
		t.Error("digest did not move with a projection field change")
	}
	other := fixturePackage("p")
	other.PackageID = "20260914T120006.000000+0000-0123456789abcdefghjk"
	op, _ := other.SafeProjection()
	if op.PackageDigest == proj.PackageDigest {
		t.Error("digest did not move with the package id")
	}
	// The projection re-hashes to its own digest after a JSON round trip.
	var back SafePackageProjection
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	sum, _ := back.Sum()
	if sum != proj.PackageDigest {
		t.Error("projection digest differs after a JSON round trip")
	}
	if g, ok := p.GrantForTarget("name:core-a"); !ok || g.CredentialID != plantest.GrantB {
		t.Error("GrantForTarget")
	}
}

func TestEncodeDecodeRoundTripAndWipe(t *testing.T) {
	seed := canarytest.Seed(t)
	p := fixturePackage(seed.Raw)
	// A key credential's keys cross by path and fingerprint.
	p.Grants[1].Keys = []credentials.KeyRef{{Path: "/home/netops/.ssh/id_ed25519", Fingerprint: "SHA256:lCkD25f/uZQGbWYmns4BurmVr65NAa+wSHkq5Y/lnVk"}}
	encoded, intermediate, err := encode(p)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range intermediate {
		if b != 0 {
			t.Fatalf("intermediate buffer byte %d is %d after encode", i, b)
		}
	}
	if !encoded.IsSet() || encoded.Len() == 0 {
		t.Fatal("encoded bytes are unset")
	}
	back, err := decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Grants) != 2 || len(back.Bindings) != 3 {
		t.Fatalf("decoded shape: %d grants %d bindings", len(back.Grants), len(back.Bindings))
	}
	for i := range p.Grants {
		if !p.Grants[i].Equivalent(back.Grants[i]) {
			t.Errorf("grant %d differs after the round trip", i)
		}
		// The evidence crosses the wire whole, the credential CSV's credkey
		// with it.
		if p.Grants[i].MatchedOn != back.Grants[i].MatchedOn {
			t.Fatalf("grant %d evidence %+v came back %+v", i, p.Grants[i].MatchedOn, back.Grants[i].MatchedOn)
		}
		if !reflect.DeepEqual(p.Grants[i].Keys, back.Grants[i].Keys) {
			t.Fatalf("grant %d keys %+v came back %+v", i, p.Grants[i].Keys, back.Grants[i].Keys)
		}
		if !p.Grants[i].Username.Equal(back.Grants[i].Username) {
			t.Errorf("grant %d username differs", i)
		}
	}
	bp, _ := back.SafeProjection()
	pp, _ := p.SafeProjection()
	if bp.PackageDigest != pp.PackageDigest {
		t.Error("package digest differs after the round trip")
	}
	// Every plain field survives, checked through the projections.
	if bj, pj := mustJSON(t, bp), mustJSON(t, pp); bj != pj {
		t.Errorf("projections differ:\n%s\n%s", bj, pj)
	}
	// Unknown fields and garbage are refused with the package code.
	if _, err := decode(credentials.NewSecretBytes([]byte(`{"schema_version":1,"extra":true}`))); err == nil || !strings.HasPrefix(err.Error(), "credential_package_invalid: rule=package_malformed") {
		t.Errorf("unknown field: %v", err)
	}
	if _, err := decode(credentials.NewSecretBytes([]byte("not json"))); err == nil {
		t.Error("garbage decoded")
	}
	// The encoded bytes refuse every sink like any other secret.
	canarytest.Exercise(t, encoded, canary.Refusing, seed)
}

func TestPackageRefusesEverySink(t *testing.T) {
	seed := canarytest.Seed(t)
	p := fixturePackage(seed.Raw)
	canarytest.Exercise(t, p, canary.Refusing, seed)
	canarytest.Exercise(t, struct{ P CredentialPackage }{p}, canary.Refusing, seed)
	canarytest.Exercise(t, struct{ p CredentialPackage }{p}, canary.Hidden, seed)
	canarytest.Exercise(t, []CredentialPackage{p}, canary.Refusing, seed)
	p.Destroy()
	if p.Grants[0].Password.IsSet() || p.Grants[1].EnablePassword.IsSet() {
		t.Error("Destroy left a secret set")
	}
}

func TestSafeTypesMatchSchemaAndAreNonSecret(t *testing.T) {
	p := fixturePackage("p")
	proj, err := p.SafeProjection()
	if err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParity(t, envelopeSchema, proj.Envelope())
	canarytest.SchemaParityAt(t, envelopeSchema, "#/$defs/safe_package_projection", proj)
	sealed := proj.Envelope()
	sealed.Protection = ProtectionSealed
	sealed.Sealed = &SealedPayload{KeyID: "k1", Nonce: []byte("nonce-nonce-"), Ciphertext: []byte("ciphertext")}
	sealed.PackageDigest = executionplan.Sum(sealed.Sealed.Ciphertext)
	if err := sealed.Validate(); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParity(t, envelopeSchema, sealed)
	allow := canarytest.Allow{
		Leaves: []string{"time.Time", "github.com/robert-patrick-texas/karvi/executionplan.Digest", "github.com/robert-patrick-texas/karvi/credentials.Match"},
		Fields: []string{"SealedPayload.Nonce", "SealedPayload.Ciphertext"},
	}
	for _, rt := range []reflect.Type{reflect.TypeOf(SafePackageProjection{}), reflect.TypeOf(Envelope{}), reflect.TypeOf(TargetCredentialBinding{}), reflect.TypeOf(Principal{})} {
		canarytest.Walk(t, rt, allow)
	}
	rec := &recorder{TB: t}
	canarytest.Walk(rec, reflect.TypeOf(CredentialPackage{}), allow)
	if rec.errors == 0 {
		t.Error("the walker accepted CredentialPackage")
	}
}

func TestEnvelopeValidationVectors(t *testing.T) {
	p := fixturePackage("p")
	proj, _ := p.SafeProjection()
	cases := []struct {
		name string
		edit func(*Envelope)
		rule string
	}{
		{"schema", func(e *Envelope) { e.SchemaVersion = SchemaVersion + 1 }, "envelope_schema"},
		{"content type", func(e *Envelope) { e.ContentType = "text/plain" }, "envelope_content_type"},
		{"id", func(e *Envelope) { e.PackageID = "pkg" }, "envelope_id"},
		{"digest", func(e *Envelope) { e.PackageDigest = executionplan.Digest{} }, "envelope_digest"},
		{"local-peer with sealed", func(e *Envelope) { e.Sealed = &SealedPayload{KeyID: "k", Nonce: []byte("n"), Ciphertext: []byte("c")} }, "envelope_protection"},
		{"sealed without payload", func(e *Envelope) { e.Protection = ProtectionSealed }, "envelope_protection"},
		{"sealed wrong digest", func(e *Envelope) {
			e.Protection = ProtectionSealed
			e.Sealed = &SealedPayload{KeyID: "k", Nonce: []byte("n"), Ciphertext: []byte("c")}
		}, "envelope_digest"},
		{"unknown protection", func(e *Envelope) { e.Protection = "none" }, "envelope_protection"},
	}
	for _, c := range cases {
		e := proj.Envelope()
		c.edit(&e)
		err := e.Validate()
		if err == nil || !strings.HasPrefix(err.Error(), "credential_package_invalid: rule="+c.rule) {
			t.Errorf("%s: want %s, got %v", c.name, c.rule, err)
		}
	}
	e := proj.Envelope()
	e.JobID = plantest.PlanID
	if err := e.Matches(proj); err == nil {
		t.Error("mismatched envelope accepted")
	}
}

type validationVector struct {
	name string
	edit func(*CredentialPackage)
	code string
	rule string
}

// validationVectors holds one vector per rejection rule, shared
// by the committed and provided stages.
func validationVectors() []validationVector {
	return []validationVector{
		{"schema", func(p *CredentialPackage) { p.SchemaVersion = 0 }, "credential_package_invalid", "schema_version"},
		{"package id", func(p *CredentialPackage) { p.PackageID = "pkg" }, "credential_package_invalid", "package_id"},
		{"job id", func(p *CredentialPackage) { p.JobID = plantest.PlanID }, "credential_package_invalid", "job_id"},
		{"plan digest", func(p *CredentialPackage) { p.PlanDigest = executionplan.Sum([]byte("x")) }, "credential_package_invalid", "plan_digest"},
		{"issuer kind", func(p *CredentialPackage) { p.Issuer.Kind = IssuerCredentialAuthority }, "credential_package_invalid", "issuer_kind"},
		{"issuer uid", func(p *CredentialPackage) { p.Issuer.UID = 1001 }, "credential_package_invalid", "issuer"},
		{"wrong audience", func(p *CredentialPackage) { p.Audience = []string{"daemon:other:1000"} }, "credential_package_invalid", "audience"},
		{"empty audience entry", func(p *CredentialPackage) { p.Audience = []string{audienceLocal, ""} }, "credential_package_invalid", "audience"},
		{"sealed", func(p *CredentialPackage) { p.Protection = ProtectionSealed }, "credential_package_invalid", "protection_unsupported"},
		{"unknown protection", func(p *CredentialPackage) { p.Protection = "none" }, "credential_package_invalid", "protection"},
		{"window reversed", func(p *CredentialPackage) { p.ExpiresAt = p.IssuedAt }, "credential_package_invalid", "package_window"},
		{"window too long", func(p *CredentialPackage) { p.ExpiresAt = p.IssuedAt.Add(MaxPackageWindow + time.Second) }, "credential_package_invalid", "package_window"},
		{"not yet valid", func(p *CredentialPackage) { p.IssuedAt = now.Add(time.Minute); p.ExpiresAt = now.Add(2 * time.Minute) }, "credential_package_invalid", "package_not_yet_valid"},
		{"expired", func(p *CredentialPackage) {
			p.IssuedAt = now.Add(-3 * time.Minute)
			p.ExpiresAt = now.Add(-time.Minute)
		}, "credential_package_expired", "package_expired"},
		{"no grants", func(p *CredentialPackage) { p.Grants = nil }, "credential_package_invalid", "grants"},
		{"invalid grant", func(p *CredentialPackage) { p.Grants[0].Method = "x" }, "credential_package_invalid", "grant_method"},
		{"duplicate grant", func(p *CredentialPackage) { p.Grants[1].CredentialID = plantest.GrantA }, "credential_package_invalid", "grant_duplicate"},
		{"grant too long", func(p *CredentialPackage) {
			p.Grants[0].NotAfter = p.Grants[0].NotBefore.Add(MaxGrantLifetime + time.Second)
		}, "credential_package_invalid", "grant_window"},
		{"grant not yet valid", func(p *CredentialPackage) {
			p.Grants[0].NotBefore = now.Add(time.Hour)
			p.Grants[0].NotAfter = now.Add(2 * time.Hour)
		}, "credential_package_invalid", "grant_not_yet_valid"},
		{"grant expired", func(p *CredentialPackage) {
			p.Grants[0].NotBefore = now.Add(-2 * time.Hour)
			p.Grants[0].NotAfter = now.Add(-time.Hour)
		}, "credential_package_expired", "grant_expired"},
		{"no bindings", func(p *CredentialPackage) { p.Bindings = nil }, "credential_package_invalid", "bindings"},
		{"duplicate binding", func(p *CredentialPackage) { p.Bindings = append(p.Bindings, p.Bindings[0]) }, "credential_package_invalid", "binding_duplicate"},
		{"binding unknown grant", func(p *CredentialPackage) { p.Bindings[0].CredentialID = fixturePackageID }, "credential_package_invalid", "binding_grant"},
		{"binding unknown target", func(p *CredentialPackage) { p.Bindings[0].TargetID = "name:ghost" }, "credential_package_invalid", "binding_target"},
		{"binding differs from plan", func(p *CredentialPackage) {
			// Every target bound to grant A with A's scope widened and B
			// removed: consistent in itself, and only the final plan says
			// core-a names grant B.
			p.Bindings[2].CredentialID = plantest.GrantA
			p.Grants[0].Scope.TargetIDs = []string{"name:127.0.0.1", "name:core-a", "name:edge-b"}
			p.Grants = p.Grants[:1]
		}, "credential_package_invalid", "binding_mismatch"},
		{"scope transport", func(p *CredentialPackage) { p.Grants[0].Scope.Transports = []string{"native"} }, "credential_package_invalid", "scope"},
		{"scope port", func(p *CredentialPackage) { p.Grants[0].Scope.Ports = []uint16{2222} }, "credential_package_invalid", "scope"},
		{"scope target", func(p *CredentialPackage) { p.Grants[0].Scope.TargetIDs = []string{"name:127.0.0.1"} }, "credential_package_invalid", "scope"},
		{"unbound target", func(p *CredentialPackage) { p.Bindings = p.Bindings[:2]; p.Grants = p.Grants[:1] }, "credential_package_invalid", "target_unbound"},
		{"unbound grant", func(p *CredentialPackage) {
			p.Grants = append(p.Grants, p.Grants[1])
			p.Grants[2].CredentialID = fixturePackageID
		}, "credential_package_invalid", "grant_unbound"},
	}
}

func TestPackageValidationVectors(t *testing.T) {
	plan := plantest.FinalPlan()
	header := plantest.Header(plan, executionplan.Committed)
	for _, c := range validationVectors() {
		p := fixturePackage("p")
		c.edit(&p)
		err := p.Validate(&plan, header, audienceLocal, now)
		if err == nil || !strings.HasPrefix(err.Error(), c.code+": rule="+c.rule+":") {
			t.Errorf("%s: want %s rule %s, got %v", c.name, c.code, c.rule, err)
		}
	}
	// A draft plan has no final digest and cannot receive a package.
	draft := plantest.DraftPlan()
	if err := fixturePackage("p").Validate(&draft, header, audienceLocal, now); err == nil || !strings.Contains(err.Error(), "plan_digest") {
		t.Errorf("draft plan: %v", err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestProvidedStageParity: every committed-stage
// vector is refused at the provided stage with the same code and rule,
// except exactly plan_digest and binding_mismatch, which need the final
// plan; and the draft's scopes are the final plan's.
func TestProvidedStageParity(t *testing.T) {
	final := plantest.FinalPlan()
	draft := plantest.DraftPlan()
	header := plantest.Header(draft, executionplan.Draft)
	scopes := ScopeOf(&draft)
	if len(scopes) != len(final.Targets) {
		t.Fatalf("%d scopes for %d targets", len(scopes), len(final.Targets))
	}
	for i, sc := range ScopeOf(&final) {
		if sc.BindingID == "" || scopes[i].BindingID != "" || sc.TargetID != scopes[i].TargetID || sc.Transport != scopes[i].Transport || sc.Port != scopes[i].Port {
			t.Fatalf("scope %d differs between draft and final: %+v vs %+v", i, scopes[i], sc)
		}
	}
	finalOnly := map[string]bool{"plan_digest": true, "binding_mismatch": true}
	seen := map[string]bool{}
	for _, c := range validationVectors() {
		p := fixturePackage("p")
		c.edit(&p)
		err := p.ValidateProvided(scopes, header, audienceLocal, now)
		seen[c.rule] = true
		if finalOnly[c.rule] {
			if err != nil {
				t.Errorf("%s: the provided stage refused a final-plan rule: %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.HasPrefix(err.Error(), c.code+": rule="+c.rule+":") {
			t.Errorf("%s: provided stage want %s rule %s, got %v", c.name, c.code, c.rule, err)
		}
	}
	for r := range finalOnly {
		if !seen[r] {
			t.Errorf("no vector exercises %s", r)
		}
	}
	// A package for the final plan passes the provided stage against the
	// draft before any digest exists, and fails commit only on plan_digest.
	p := fixturePackage("p")
	p.PlanDigest = executionplan.Sum([]byte("not the final plan"))
	if err := p.ValidateProvided(scopes, header, audienceLocal, now); err != nil {
		t.Errorf("provided stage checked the plan digest: %v", err)
	}
	if err := p.Validate(&final, plantest.Header(final, executionplan.Committed), audienceLocal, now); err == nil || !strings.Contains(err.Error(), "rule=plan_digest") {
		t.Errorf("committed stage: %v", err)
	}
}
