// Package credentialpackage holds the protected credential-package contracts.
// It offers no function
// that returns plaintext; grants and packages refuse every encoder and
// formatter, and only their safe projections have a wire form.
package credentialpackage

import (
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
)

const redacted = "<redacted>"

// Method says how a grant carries its credential.
type Method string

const (
	MethodEmbeddedSecret         Method = "embedded-secret"
	MethodCredentialReference    Method = "credential-reference"
	MethodSigningAgentDelegation Method = "signing-agent-delegation"
)

// Transports a scope may name.
var knownTransports = map[string]bool{"native": true, "system": true, "telnet": true}

// SecretReference locates a credential held elsewhere; it is never a token.
type SecretReference struct {
	Provider string `json:"provider"`
	Locator  string `json:"locator"`
	KeyID    string `json:"key_id,omitempty"`
}

func (r SecretReference) empty() bool { return r.Provider == "" && r.Locator == "" && r.KeyID == "" }

// CredentialScope bounds where a grant may be used.
type CredentialScope struct {
	TargetIDs  []string `json:"target_ids"`
	Transports []string `json:"transports"`
	Ports      []uint16 `json:"ports"`
}

// Covers reports whether the scope admits the target, transport, and port.
func (s CredentialScope) Covers(targetID, transport string, port uint16) bool {
	return containsString(s.TargetIDs, targetID) && containsString(s.Transports, transport) && containsPort(s.Ports, port)
}

// CredentialGrant is one resolved credential bound to a scope and a validity
// window. It has no JSON tags because it has no wire form; the
// package encoder is its only serializer.
type CredentialGrant struct {
	CredentialID   string
	Method         Method
	Username       credentials.SecretString
	Password       credentials.SecretString
	EnablePassword credentials.SecretString
	Reference      SecretReference
	Policy         string
	Backend        string
	MatchedOn      credentials.Match
	Scope          CredentialScope
	NotBefore      time.Time
	NotAfter       time.Time
}

// GrantProjection is the safe form of a grant for manifests, audit, and
// reports. The device username is the one explicit accountability copy
// permitted.
type GrantProjection struct {
	CredentialID   string            `json:"credential_id"`
	Method         Method            `json:"method"`
	DeviceUsername string            `json:"device_username"`
	Policy         string            `json:"policy"`
	Backend        string            `json:"backend"`
	MatchedOn      credentials.Match `json:"matched_on"`
	Reference      *SecretReference  `json:"reference,omitempty"`
	Scope          CredentialScope   `json:"scope"`
	NotBefore      time.Time         `json:"not_before"`
	NotAfter       time.Time         `json:"not_after"`
}

func invalid(rule, format string, args ...any) error {
	return fmt.Errorf("credential_package_invalid: rule=%s: %s", rule, fmt.Sprintf(format, args...))
}

// Validate checks the grant's own contract. The
// lifetime bound and the relationship to a plan are package rules.
func (g CredentialGrant) Validate() error {
	if !executionplan.ValidID(g.CredentialID) {
		return invalid("grant_id", "%q is not a valid identifier", g.CredentialID)
	}
	switch g.Method {
	case MethodEmbeddedSecret:
		if !g.Username.IsSet() {
			return invalid("grant_username", "grant %s: embedded-secret needs a username", g.CredentialID)
		}
		if !g.Reference.empty() {
			return invalid("grant_reference", "grant %s: embedded-secret carries no reference", g.CredentialID)
		}
	case MethodCredentialReference, MethodSigningAgentDelegation:
		if g.Reference.Provider == "" || g.Reference.Locator == "" {
			return invalid("grant_reference", "grant %s: %s needs a provider and locator", g.CredentialID, g.Method)
		}
		if g.Username.IsSet() || g.Password.IsSet() || g.EnablePassword.IsSet() {
			return invalid("grant_embedded", "grant %s: %s carries no embedded secret", g.CredentialID, g.Method)
		}
	default:
		return invalid("grant_method", "grant %s: method %q is unknown", g.CredentialID, string(g.Method))
	}
	if err := g.Scope.validate(g.CredentialID); err != nil {
		return err
	}
	if g.NotBefore.IsZero() || g.NotAfter.IsZero() {
		return invalid("grant_window", "grant %s: not_before and not_after are required", g.CredentialID)
	}
	if !g.NotBefore.Before(g.NotAfter) {
		return invalid("grant_window", "grant %s: not_before must precede not_after", g.CredentialID)
	}
	return nil
}

func (s CredentialScope) validate(id string) error {
	if len(s.TargetIDs) == 0 {
		return invalid("grant_scope", "grant %s: scope names no target", id)
	}
	if !sort.StringsAreSorted(s.TargetIDs) || hasDuplicate(s.TargetIDs) {
		return invalid("grant_scope", "grant %s: target_ids must be sorted and unique", id)
	}
	if len(s.Transports) == 0 || hasDuplicate(s.Transports) {
		return invalid("grant_scope", "grant %s: transports must be non-empty and unique", id)
	}
	for _, t := range s.Transports {
		if !knownTransports[t] {
			return invalid("grant_scope", "grant %s: transport %q is unknown", id, t)
		}
	}
	if len(s.Ports) == 0 {
		return invalid("grant_scope", "grant %s: ports must be non-empty", id)
	}
	seen := map[uint16]bool{}
	for _, p := range s.Ports {
		if p == 0 || seen[p] {
			return invalid("grant_scope", "grant %s: port %d is zero or repeated", id, p)
		}
		seen[p] = true
	}
	return nil
}

// SafeProjection is the only exported path that reads a secret: it copies
// the username through WithBytes and nothing else.
func (g CredentialGrant) SafeProjection() (GrantProjection, error) {
	p := GrantProjection{CredentialID: g.CredentialID, Method: g.Method, Policy: g.Policy, Backend: g.Backend, MatchedOn: g.MatchedOn, Scope: g.Scope, NotBefore: g.NotBefore, NotAfter: g.NotAfter}
	if !g.Reference.empty() {
		ref := g.Reference
		p.Reference = &ref
	}
	if g.Username.IsSet() {
		if err := g.Username.WithBytes(func(b []byte) error { p.DeviceUsername = string(b); return nil }); err != nil {
			return GrantProjection{}, err
		}
	}
	return p, nil
}

// Equivalent reports whether two grants carry the same credential: same
// method, reference, and every secret equal in constant time.
func (g CredentialGrant) Equivalent(o CredentialGrant) bool {
	return g.Method == o.Method && g.Reference == o.Reference && g.Username.Equal(o.Username) && g.Password.Equal(o.Password) && g.EnablePassword.Equal(o.EnablePassword)
}

// Destroy wipes every secret the grant holds.
func (g CredentialGrant) Destroy() {
	g.Username.Destroy()
	g.Password.Destroy()
	g.EnablePassword.Destroy()
}

func refuse(what string) error {
	return fmt.Errorf("secret_serialization_refused: a credential grant cannot be %s encoded", what)
}

func (g CredentialGrant) String() string                    { return redacted }
func (g CredentialGrant) GoString() string                  { return redacted }
func (g CredentialGrant) Format(f fmt.State, _ rune)        { _, _ = io.WriteString(f, redacted) }
func (g CredentialGrant) LogValue() slog.Value              { return slog.StringValue(redacted) }
func (g CredentialGrant) MarshalJSON() ([]byte, error)      { return nil, refuse("JSON") }
func (g CredentialGrant) MarshalText() ([]byte, error)      { return nil, refuse("text") }
func (g CredentialGrant) AppendText([]byte) ([]byte, error) { return nil, refuse("text") }
func (g CredentialGrant) GobEncode() ([]byte, error)        { return nil, refuse("gob") }
func (g CredentialGrant) MarshalBinary() ([]byte, error)    { return nil, refuse("binary") }

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func containsPort(list []uint16, p uint16) bool {
	for _, x := range list {
		if x == p {
			return true
		}
	}
	return false
}

func hasDuplicate(list []string) bool {
	seen := map[string]bool{}
	for _, s := range list {
		if seen[s] {
			return true
		}
		seen[s] = true
	}
	return false
}
