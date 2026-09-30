package credentialpackage

import (
	"context"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
)

func rule(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	i := strings.Index(s, "rule=")
	if i < 0 {
		return s
	}
	s = s[i+len("rule="):]
	if j := strings.IndexByte(s, ':'); j >= 0 {
		s = s[:j]
	}
	return s
}

// TestLocalPeerProtectorRoundTrip is the round trip
// through the protector: Unprotect(Protect(p))
// equals p on every secret and plain field, the envelope is the
// projection's, and Protected refuses every sink.
func TestLocalPeerProtectorRoundTrip(t *testing.T) {
	seed := canarytest.Seed(t)
	ctx := context.Background()
	protector, err := ProtectorFor(ProtectionLocalPeer)
	if err != nil || protector.Protection() != ProtectionLocalPeer {
		t.Fatalf("ProtectorFor(local-peer): %v", err)
	}
	p := fixturePackage(seed.Raw)
	protected, err := protector.Protect(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer protected.Destroy()
	proj, _ := p.SafeProjection()
	if err := protected.Envelope.Validate(); err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, protected.Envelope) != mustJSON(t, proj.Envelope()) {
		t.Fatal("envelope is not the projection's")
	}
	if !protected.Body.IsSet() || protected.Body.Len() == 0 {
		t.Fatal("body is unset")
	}
	back, err := protector.Unprotect(ctx, protected, audienceLocal)
	if err != nil {
		t.Fatal(err)
	}
	defer back.Destroy()
	if len(back.Grants) != 2 || len(back.Bindings) != 3 {
		t.Fatalf("shape: %d grants %d bindings", len(back.Grants), len(back.Bindings))
	}
	for i := range p.Grants {
		if !p.Grants[i].Equivalent(back.Grants[i]) || !p.Grants[i].Username.Equal(back.Grants[i].Username) {
			t.Errorf("grant %d differs after the round trip", i)
		}
	}
	bp, _ := back.SafeProjection()
	if bj, pj := mustJSON(t, bp), mustJSON(t, proj); bj != pj {
		t.Errorf("projections differ:\n%s\n%s", bj, pj)
	}
	canarytest.Exercise(t, protected, canary.Refusing, seed)
	canarytest.Exercise(t, struct{ P Protected }{protected}, canary.Refusing, seed)
	canarytest.Exercise(t, struct{ p Protected }{protected}, canary.Hidden, seed)
	// The associated data a sealer would bind omits the digest and payload.
	a, err := AssociatedData(protected.Envelope)
	if err != nil {
		t.Fatal(err)
	}
	moved := protected.Envelope
	moved.PackageDigest = executionplan.Sum([]byte("other"))
	moved.Sealed = &SealedPayload{KeyID: "k", Nonce: []byte("n"), Ciphertext: []byte("c")}
	b, _ := AssociatedData(moved)
	if string(a) != string(b) {
		t.Error("associated data covers the digest or the payload")
	}
	if strings.Contains(string(a), seed.Raw) || !strings.Contains(string(a), audienceLocal) {
		t.Error("associated data content")
	}
	t.Logf("associated data: %s", a)
}

// TestProtectorRefusals is the refusal vector list.
func TestProtectorRefusals(t *testing.T) {
	ctx := context.Background()
	if _, err := ProtectorFor(ProtectionSealed); rule(err) != "protection_unsupported" {
		t.Errorf("sealed: %v", err)
	}
	if _, err := ProtectorFor("none"); rule(err) != "protection" {
		t.Errorf("unknown: %v", err)
	}
	protector, _ := ProtectorFor(ProtectionLocalPeer)
	other := fixturePackage("p")
	other.Protection = ProtectionSealed
	if _, err := protector.Protect(ctx, other); rule(err) != "protection" {
		t.Errorf("Protect of a sealed package: %v", err)
	}
	good := fixturePackage("p")
	protected, err := protector.Protect(ctx, good)
	if err != nil {
		t.Fatal(err)
	}
	defer protected.Destroy()
	cases := []struct {
		name     string
		edit     func(*Protected)
		audience string
		rule     string
	}{
		{"wrong audience", func(*Protected) {}, "daemon:other:1000", "audience"},
		{"sealed envelope", func(p *Protected) {
			p.Envelope.Protection = ProtectionSealed
			p.Envelope.Sealed = &SealedPayload{KeyID: "k", Nonce: []byte("n"), Ciphertext: []byte("c")}
			p.Envelope.PackageDigest = executionplan.Sum([]byte("c"))
		}, audienceLocal, "protection_unsupported"},
		{"unknown protection", func(p *Protected) { p.Envelope.Protection = "none" }, audienceLocal, "protection"},
		{"sealed payload under local-peer", func(p *Protected) {
			p.Envelope.Sealed = &SealedPayload{KeyID: "k", Nonce: []byte("n"), Ciphertext: []byte("c")}
		}, audienceLocal, "envelope_protection"},
		{"envelope names another package", func(p *Protected) {
			p.Envelope.PackageID = "20260914T120006.000000+0000-0123456789abcdefghjk"
		}, audienceLocal, "envelope_mismatch"},
		{"envelope names another digest", func(p *Protected) { p.Envelope.PackageDigest = executionplan.Sum([]byte("x")) }, audienceLocal, "envelope_mismatch"},
		{"empty body", func(p *Protected) { p.Body = credentials.SecretBytes{} }, audienceLocal, "body"},
		{"garbage body", func(p *Protected) { p.Body = credentials.NewSecretBytes([]byte("not json")) }, audienceLocal, "package_malformed"},
	}
	for _, c := range cases {
		in := Protected{Envelope: protected.Envelope, Body: protected.Body}
		in.Envelope.Audience = append([]string{}, protected.Envelope.Audience...)
		c.edit(&in)
		pkg, err := protector.Unprotect(ctx, in, c.audience)
		if err == nil {
			pkg.Destroy()
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if !strings.HasPrefix(err.Error(), "credential_package_invalid: rule="+c.rule+":") {
			t.Errorf("%s: want rule %s, got %v", c.name, c.rule, err)
		}
	}
}
