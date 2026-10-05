package credentialpackage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
)

// SchemaVersion is the credential-package counter: 2 adds a grant's keys.
const SchemaVersion = 2

// ContentType names the package in envelope associated data.
const ContentType = "application/vnd.karvi.credential-package.v2"

// v1 time bounds; constants, not configuration.
const (
	MaxPackageWindow = 10 * time.Minute
	MaxGrantLifetime = 24 * time.Hour
)

// Protection names how a package crosses to the daemon.
type Protection string

const (
	ProtectionLocalPeer Protection = "local-peer"
	ProtectionSealed    Protection = "sealed"
)

// Principal is the package issuer.
type Principal struct {
	Kind     string `json:"kind"` // operator | credential-authority
	Username string `json:"username"`
	UID      int    `json:"uid"`
	Hostname string `json:"hostname"`
}

const (
	IssuerOperator            = "operator"
	IssuerCredentialAuthority = "credential-authority"
)

// TargetCredentialBinding ties one plan target to one grant.
type TargetCredentialBinding struct {
	TargetID     string `json:"target_id"`
	CredentialID string `json:"credential_id"`
}

// CredentialPackage is the secret-bearing package. It has no
// JSON tags and no wire form; a Protector is its only way to bytes.
type CredentialPackage struct {
	SchemaVersion int
	PackageID     string
	JobID         string
	PlanDigest    executionplan.Digest
	Issuer        Principal
	Audience      []string
	Protection    Protection
	IssuedAt      time.Time
	ExpiresAt     time.Time
	Grants        []CredentialGrant
	Bindings      []TargetCredentialBinding
}

// SafePackageProjection is what the manifest, audit, and reports carry.
type SafePackageProjection struct {
	SchemaVersion int                       `json:"schema_version"`
	PackageID     string                    `json:"package_id"`
	JobID         string                    `json:"job_id"`
	PlanDigest    executionplan.Digest      `json:"plan_digest"`
	PackageDigest executionplan.Digest      `json:"package_digest,omitzero"`
	Issuer        Principal                 `json:"issuer"`
	Audience      []string                  `json:"audience"`
	Protection    Protection                `json:"protection"`
	IssuedAt      time.Time                 `json:"issued_at"`
	ExpiresAt     time.Time                 `json:"expires_at"`
	Grants        []GrantProjection         `json:"grants"`
	Bindings      []TargetCredentialBinding `json:"bindings"`
}

// Envelope is the non-secret outer record, the ProtectedCredentialPackage.
// Under local-peer the package body travels in the credential
// frame; under sealed it is the ciphertext here.
type Envelope struct {
	SchemaVersion int                  `json:"schema_version"`
	ContentType   string               `json:"content_type"`
	PackageID     string               `json:"package_id"`
	JobID         string               `json:"job_id"`
	PlanDigest    executionplan.Digest `json:"plan_digest"`
	Issuer        Principal            `json:"issuer"`
	Audience      []string             `json:"audience"`
	Protection    Protection           `json:"protection"`
	IssuedAt      time.Time            `json:"issued_at"`
	ExpiresAt     time.Time            `json:"expires_at"`
	PackageDigest executionplan.Digest `json:"package_digest"`
	Sealed        *SealedPayload       `json:"sealed,omitempty"`
}

// SealedPayload carries ciphertext for the sealed protection (interface and
// schema only in v1).
type SealedPayload struct {
	KeyID      string `json:"key_id"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

func expired(rule, format string, args ...any) error {
	return fmt.Errorf("credential_package_expired: rule=%s: %s", rule, fmt.Sprintf(format, args...))
}

// Grant returns the grant with the given ID.
func (p CredentialPackage) Grant(credentialID string) (CredentialGrant, bool) {
	for _, g := range p.Grants {
		if g.CredentialID == credentialID {
			return g, true
		}
	}
	return CredentialGrant{}, false
}

// GrantForTarget returns the grant bound to the target.
// ForTarget is GrantForTarget under the executor's grant-provider name.
func (p CredentialPackage) ForTarget(targetID string) (CredentialGrant, bool) {
	return p.GrantForTarget(targetID)
}

func (p CredentialPackage) GrantForTarget(targetID string) (CredentialGrant, bool) {
	for _, b := range p.Bindings {
		if b.TargetID == targetID {
			return p.Grant(b.CredentialID)
		}
	}
	return CredentialGrant{}, false
}

// SafeProjection builds the safe form and fills its digest. The digest
// never covers secret bytes: it is SHA-256 over this projection with
// package_digest cleared, made unique by package_id.
func (p CredentialPackage) SafeProjection() (SafePackageProjection, error) {
	s := SafePackageProjection{
		SchemaVersion: p.SchemaVersion, PackageID: p.PackageID, JobID: p.JobID, PlanDigest: p.PlanDigest,
		Issuer: p.Issuer, Audience: append([]string{}, p.Audience...), Protection: p.Protection,
		IssuedAt: p.IssuedAt, ExpiresAt: p.ExpiresAt, Grants: []GrantProjection{}, Bindings: append([]TargetCredentialBinding{}, p.Bindings...),
	}
	for _, g := range p.Grants {
		gp, err := g.SafeProjection()
		if err != nil {
			return SafePackageProjection{}, err
		}
		s.Grants = append(s.Grants, gp)
	}
	sum, err := s.Sum()
	if err != nil {
		return SafePackageProjection{}, err
	}
	s.PackageDigest = sum
	return s, nil
}

// Sum is the local-peer package digest: the projection with package_digest
// cleared.
func (s SafePackageProjection) Sum() (executionplan.Digest, error) {
	s.PackageDigest = executionplan.Digest{}
	return executionplan.SumJSON(s)
}

// Envelope builds the local-peer envelope from the projection.
func (s SafePackageProjection) Envelope() Envelope {
	return Envelope{
		SchemaVersion: s.SchemaVersion, ContentType: ContentType, PackageID: s.PackageID, JobID: s.JobID, PlanDigest: s.PlanDigest,
		Issuer: s.Issuer, Audience: append([]string{}, s.Audience...), Protection: s.Protection, IssuedAt: s.IssuedAt, ExpiresAt: s.ExpiresAt,
		PackageDigest: s.PackageDigest,
	}
}

// Validate checks the envelope's own structure. Under sealed the digest
// must be the SHA-256 of the ciphertext.
func (e Envelope) Validate() error {
	if e.SchemaVersion != SchemaVersion {
		return invalid("envelope_schema", "schema_version %d is not %d", e.SchemaVersion, SchemaVersion)
	}
	if e.ContentType != ContentType {
		return invalid("envelope_content_type", "%q is not %q", e.ContentType, ContentType)
	}
	if !executionplan.ValidID(e.PackageID) || !executionplan.ValidJobID(e.JobID) {
		return invalid("envelope_id", "package_id must be a valid identifier and job_id a job ID (YYMMDD-HHMMSS-xx)")
	}
	if e.PlanDigest.IsZero() || e.PackageDigest.IsZero() {
		return invalid("envelope_digest", "plan_digest and package_digest are required")
	}
	switch e.Protection {
	case ProtectionLocalPeer:
		if e.Sealed != nil {
			return invalid("envelope_protection", "local-peer carries no sealed payload")
		}
	case ProtectionSealed:
		if e.Sealed == nil || e.Sealed.KeyID == "" || len(e.Sealed.Nonce) == 0 || len(e.Sealed.Ciphertext) == 0 {
			return invalid("envelope_protection", "sealed needs key_id, nonce, and ciphertext")
		}
		if executionplan.Sum(e.Sealed.Ciphertext) != e.PackageDigest {
			return invalid("envelope_digest", "package_digest is not the ciphertext digest")
		}
	default:
		return invalid("envelope_protection", "%q is not local-peer or sealed", string(e.Protection))
	}
	return nil
}

// Matches checks that the envelope describes the projection.
func (e Envelope) Matches(s SafePackageProjection) error {
	if e.PackageID != s.PackageID || e.JobID != s.JobID || e.PlanDigest != s.PlanDigest || e.PackageDigest != s.PackageDigest || e.Protection != s.Protection {
		return invalid("envelope_mismatch", "envelope and package identity differ")
	}
	return nil
}

// TargetScope is what the provided-stage validation knows of a plan
// target: the identity, transport kind, and
// effective port are fixed at draft and unchanged by finalize; BindingID
// is filled by finalize and empty on a draft.
type TargetScope struct {
	TargetID  string
	Transport string
	Port      uint16
	BindingID string
}

// ScopeOf derives the target scopes from a plan at any stage.
func ScopeOf(plan *executionplan.ExecutionPlan) []TargetScope {
	out := make([]TargetScope, 0, len(plan.Targets))
	for _, t := range plan.Targets {
		out = append(out, TargetScope{TargetID: t.TargetID, Transport: t.Device.Transport, Port: effectivePort(t.Device.Transport, t.Device.Port), BindingID: t.CredentialBindingID})
	}
	return out
}

// Validate implements every package rejection rule against the final plan
// and header: the committed stage. audience is the
// daemon's own audience and now is the daemon's clock.
func (p CredentialPackage) Validate(plan *executionplan.ExecutionPlan, header executionplan.PublicJobHeader, audience string, now time.Time) error {
	return p.validate(ScopeOf(plan), plan.PlanDigest, header, audience, now, true)
}

// ValidateProvided is the provided stage: every rule
// of Validate in the same order except plan_digest and binding_mismatch,
// the two that need the final plan, against the draft's target scopes and
// the header the daemon holds at prepare.
func (p CredentialPackage) ValidateProvided(targets []TargetScope, header executionplan.PublicJobHeader, audience string, now time.Time) error {
	return p.validate(targets, executionplan.Digest{}, header, audience, now, false)
}

// validate is the one rule set; committed enables the two final-plan rules.
func (p CredentialPackage) validate(targets []TargetScope, planDigest executionplan.Digest, header executionplan.PublicJobHeader, audience string, now time.Time, committed bool) error {
	if p.SchemaVersion != SchemaVersion {
		return invalid("schema_version", "%d is not the supported version %d", p.SchemaVersion, SchemaVersion)
	}
	if !executionplan.ValidID(p.PackageID) {
		return invalid("package_id", "%q is not a valid identifier", p.PackageID)
	}
	if p.JobID != header.JobID {
		return invalid("job_id", "package %q differs from header %q", p.JobID, header.JobID)
	}
	if committed && (planDigest.IsZero() || p.PlanDigest != planDigest) {
		return invalid("plan_digest", "package %s differs from the plan's %s", p.PlanDigest, planDigest)
	}
	if p.Issuer.Kind != IssuerOperator {
		return invalid("issuer_kind", "%q is not supported in v1", p.Issuer.Kind)
	}
	if p.Issuer.UID != header.Operator.UID || p.Issuer.Username != header.Operator.Username {
		return invalid("issuer", "package issuer %s/%d differs from header operator %s/%d", p.Issuer.Username, p.Issuer.UID, header.Operator.Username, header.Operator.UID)
	}
	if !containsString(p.Audience, audience) {
		return invalid("audience", "package audience %v does not include %q", p.Audience, audience)
	}
	for _, a := range p.Audience {
		if a == "" {
			return invalid("audience", "an audience entry is empty")
		}
	}
	switch p.Protection {
	case ProtectionLocalPeer:
	case ProtectionSealed:
		return invalid("protection_unsupported", "sealed packages are not accepted in v1")
	default:
		return invalid("protection", "%q is not local-peer or sealed", string(p.Protection))
	}
	if p.IssuedAt.IsZero() || p.ExpiresAt.IsZero() || !p.IssuedAt.Before(p.ExpiresAt) {
		return invalid("package_window", "issued_at must precede expires_at")
	}
	if p.ExpiresAt.Sub(p.IssuedAt) > MaxPackageWindow {
		return invalid("package_window", "acceptance window exceeds %s", MaxPackageWindow)
	}
	if now.Before(p.IssuedAt) {
		return invalid("package_not_yet_valid", "issued at %s, now %s", p.IssuedAt.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	if !now.Before(p.ExpiresAt) {
		return expired("package_expired", "expired at %s, now %s", p.ExpiresAt.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	if len(p.Grants) == 0 {
		return invalid("grants", "the package carries no grant")
	}
	grants := map[string]CredentialGrant{}
	for _, g := range p.Grants {
		if err := g.Validate(); err != nil {
			return err
		}
		if _, dup := grants[g.CredentialID]; dup {
			return invalid("grant_duplicate", "grant %s appears twice", g.CredentialID)
		}
		if g.NotAfter.Sub(g.NotBefore) > MaxGrantLifetime {
			return invalid("grant_window", "grant %s validity exceeds %s", g.CredentialID, MaxGrantLifetime)
		}
		if now.Before(g.NotBefore) {
			return invalid("grant_not_yet_valid", "grant %s is valid from %s", g.CredentialID, g.NotBefore.Format(time.RFC3339))
		}
		if !now.Before(g.NotAfter) {
			return expired("grant_expired", "grant %s expired at %s", g.CredentialID, g.NotAfter.Format(time.RFC3339))
		}
		grants[g.CredentialID] = g
	}
	if len(p.Bindings) == 0 {
		return invalid("bindings", "the package carries no binding")
	}
	bound := map[string]string{}
	used := map[string]bool{}
	for _, b := range p.Bindings {
		if _, dup := bound[b.TargetID]; dup {
			return invalid("binding_duplicate", "target %q is bound twice", b.TargetID)
		}
		g, ok := grants[b.CredentialID]
		if !ok {
			return invalid("binding_grant", "target %q binds unknown grant %q", b.TargetID, b.CredentialID)
		}
		t := scopeTarget(targets, b.TargetID)
		if t == nil {
			return invalid("binding_target", "%q is not a plan target", b.TargetID)
		}
		if committed && t.BindingID != b.CredentialID {
			return invalid("binding_mismatch", "target %q names grant %q, binding names %q", b.TargetID, t.BindingID, b.CredentialID)
		}
		if !g.Scope.Covers(b.TargetID, t.Transport, t.Port) {
			return invalid("scope", "grant %s does not cover target %q over %s port %d", g.CredentialID, b.TargetID, t.Transport, t.Port)
		}
		bound[b.TargetID] = b.CredentialID
		used[b.CredentialID] = true
	}
	for _, t := range targets {
		if _, ok := bound[t.TargetID]; !ok {
			return invalid("target_unbound", "plan target %q has no binding", t.TargetID)
		}
	}
	for id := range grants {
		if !used[id] {
			return invalid("grant_unbound", "grant %s binds no target", id)
		}
	}
	return nil
}

func scopeTarget(targets []TargetScope, id string) *TargetScope {
	for i := range targets {
		if targets[i].TargetID == id {
			return &targets[i]
		}
	}
	return nil
}

// effectivePort applies the transport default when the projection omits
// the port (zero is never serialized).
func effectivePort(transport string, port uint16) uint16 {
	if port != 0 {
		return port
	}
	if transport == "telnet" {
		return 23
	}
	return 22
}

// Destroy wipes every grant's secrets.
func (p CredentialPackage) Destroy() {
	for _, g := range p.Grants {
		g.Destroy()
	}
}

func refusePackage(what string) error {
	return fmt.Errorf("secret_serialization_refused: a credential package cannot be %s encoded", what)
}

func (p CredentialPackage) String() string                    { return redacted }
func (p CredentialPackage) GoString() string                  { return redacted }
func (p CredentialPackage) Format(f fmt.State, _ rune)        { _, _ = io.WriteString(f, redacted) }
func (p CredentialPackage) LogValue() slog.Value              { return slog.StringValue(redacted) }
func (p CredentialPackage) MarshalJSON() ([]byte, error)      { return nil, refusePackage("JSON") }
func (p CredentialPackage) MarshalText() ([]byte, error)      { return nil, refusePackage("text") }
func (p CredentialPackage) AppendText([]byte) ([]byte, error) { return nil, refusePackage("text") }
func (p CredentialPackage) GobEncode() ([]byte, error)        { return nil, refusePackage("gob") }
func (p CredentialPackage) MarshalBinary() ([]byte, error)    { return nil, refusePackage("binary") }

// wirePackage and wireGrant are the unexported shadow of the package that
// encode marshals. Secrets are []byte so the decoded form can be wiped.
type wirePackage struct {
	SchemaVersion int                       `json:"schema_version"`
	PackageID     string                    `json:"package_id"`
	JobID         string                    `json:"job_id"`
	PlanDigest    executionplan.Digest      `json:"plan_digest"`
	Issuer        Principal                 `json:"issuer"`
	Audience      []string                  `json:"audience"`
	Protection    Protection                `json:"protection"`
	IssuedAt      time.Time                 `json:"issued_at"`
	ExpiresAt     time.Time                 `json:"expires_at"`
	Grants        []wireGrant               `json:"grants"`
	Bindings      []TargetCredentialBinding `json:"bindings"`
}

type wireGrant struct {
	CredentialID   string               `json:"credential_id"`
	Method         Method               `json:"method"`
	Username       []byte               `json:"username"`
	Password       []byte               `json:"password"`
	EnablePassword []byte               `json:"enable_password"`
	Reference      SecretReference      `json:"reference"`
	Policy         string               `json:"policy"`
	Backend        string               `json:"backend"`
	MatchedOn      credentials.Match    `json:"matched_on"`
	Keys           []credentials.KeyRef `json:"keys"`
	Scope          CredentialScope      `json:"scope"`
	NotBefore      time.Time            `json:"not_before"`
	NotAfter       time.Time            `json:"not_after"`
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (w *wireGrant) wipe() {
	wipe(w.Username)
	wipe(w.Password)
	wipe(w.EnablePassword)
}

// encode is the privileged serializer: it copies each secret through
// WithBytes into the shadow, marshals it, wraps the bytes as SecretBytes,
// and wipes every intermediate. Only a Protector calls it; the wiped
// intermediate is returned for the test that proves it.
func encode(p CredentialPackage) (credentials.SecretBytes, []byte, error) {
	w := wirePackage{
		SchemaVersion: p.SchemaVersion, PackageID: p.PackageID, JobID: p.JobID, PlanDigest: p.PlanDigest, Issuer: p.Issuer,
		Audience: append([]string{}, p.Audience...), Protection: p.Protection, IssuedAt: p.IssuedAt, ExpiresAt: p.ExpiresAt,
		Grants: []wireGrant{}, Bindings: append([]TargetCredentialBinding{}, p.Bindings...),
	}
	defer func() {
		for i := range w.Grants {
			w.Grants[i].wipe()
		}
	}()
	for _, g := range p.Grants {
		wg := wireGrant{CredentialID: g.CredentialID, Method: g.Method, Reference: g.Reference, Policy: g.Policy, Backend: g.Backend, MatchedOn: g.MatchedOn, Keys: append([]credentials.KeyRef{}, g.Keys...), Scope: g.Scope, NotBefore: g.NotBefore, NotAfter: g.NotAfter}
		for _, f := range []struct {
			src credentials.SecretString
			dst *[]byte
		}{{g.Username, &wg.Username}, {g.Password, &wg.Password}, {g.EnablePassword, &wg.EnablePassword}} {
			if !f.src.IsSet() {
				*f.dst = []byte{}
				continue
			}
			if err := f.src.WithBytes(func(b []byte) error { *f.dst = append([]byte{}, b...); return nil }); err != nil {
				return credentials.SecretBytes{}, nil, err
			}
		}
		w.Grants = append(w.Grants, wg)
	}
	data, err := json.Marshal(w)
	if err != nil {
		return credentials.SecretBytes{}, nil, err
	}
	out := credentials.NewSecretBytes(data)
	wipe(data)
	return out, data, nil
}

// decode reverses encode and wipes the decoded shadow.
func decode(b credentials.SecretBytes) (CredentialPackage, error) {
	var w wirePackage
	err := b.WithBytes(func(data []byte) error {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		return dec.Decode(&w)
	})
	if err != nil {
		return CredentialPackage{}, invalid("package_malformed", "%v", err)
	}
	defer func() {
		for i := range w.Grants {
			w.Grants[i].wipe()
		}
	}()
	p := CredentialPackage{
		SchemaVersion: w.SchemaVersion, PackageID: w.PackageID, JobID: w.JobID, PlanDigest: w.PlanDigest, Issuer: w.Issuer,
		Audience: append([]string{}, w.Audience...), Protection: w.Protection, IssuedAt: w.IssuedAt, ExpiresAt: w.ExpiresAt,
		Grants: []CredentialGrant{}, Bindings: append([]TargetCredentialBinding{}, w.Bindings...),
	}
	for _, wg := range w.Grants {
		g := CredentialGrant{CredentialID: wg.CredentialID, Method: wg.Method, Reference: wg.Reference, Policy: wg.Policy, Backend: wg.Backend, MatchedOn: wg.MatchedOn, Scope: wg.Scope, NotBefore: wg.NotBefore, NotAfter: wg.NotAfter}
		if len(wg.Keys) > 0 {
			g.Keys = wg.Keys
		}
		if len(wg.Username) > 0 {
			g.Username = credentials.NewSecretStringFromBytes(wg.Username)
		}
		if len(wg.Password) > 0 {
			g.Password = credentials.NewSecretStringFromBytes(wg.Password)
		}
		if len(wg.EnablePassword) > 0 {
			g.EnablePassword = credentials.NewSecretStringFromBytes(wg.EnablePassword)
		}
		p.Grants = append(p.Grants, g)
	}
	return p, nil
}
