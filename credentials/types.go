// Package credentials defines reusable credential-resolution interfaces and
// safe metadata. Concrete secret storage and backend implementations are kept
// under internal/ so importing this package does not widen their attack surface.
package credentials

import (
	"context"
	"time"

	"github.com/robert-patrick-texas/karvi/inventory"
)

type BackendMode string

const (
	OperatorKeyed BackendMode = "operator"
	DeviceKeyed   BackendMode = "device"
	FormulaBased  BackendMode = "formula"
)

type Outcome string

const (
	Success          Outcome = "success"
	NotFound         Outcome = "not_found"
	Unavailable      Outcome = "unavailable"
	PermissionDenied Outcome = "permission_denied"
	Malformed        Outcome = "malformed"
	Incomplete       Outcome = "incomplete"
)

type Operator struct {
	Username   string   `json:"username"`
	UID        int      `json:"uid"`
	PrimaryGID int      `json:"primary_gid,omitempty"`
	Groups     []string `json:"groups,omitempty"`
	Home       string   `json:"-"`
}

// Match is the safe evidence of what a credential matched on. It describes
// the criterion (the pattern, the file, the line) and never echoes the
// device being resolved: the planner merges grants only when their evidence
// is equal, so the struct stays comparable and device-free.
type Match struct {
	Category    string `json:"category"`
	SafeValue   string `json:"safe_value,omitempty"`
	ValueDigest string `json:"value_digest,omitempty"`
	Pattern     string `json:"pattern,omitempty"`
	Source      string `json:"source,omitempty"`
	Line        int    `json:"line,omitempty"`
	// CredKey is a credential CSV row's explicit credkey or, for a blank
	// cell, the generated label BACKEND:LINE. It is
	// a provenance label, omitted for other backends, and distinct from a
	// grant's credential_id.
	CredKey string `json:"credkey,omitempty"`
}

type FieldSource struct {
	Backend   string `json:"backend"`
	Path      string `json:"path"`
	Transform string `json:"transform,omitempty"`
}

// Material exposes secrets only inside caller-scoped callbacks. Implementations
// must not provide String or serialization methods that reveal values.
type Material interface {
	UsernameSet() bool
	PasswordSet() bool
	EnablePasswordSet() bool
	WithUsername(func([]byte) error) error
	WithPassword(func([]byte) error) error
	WithEnablePassword(func([]byte) error) error
	Destroy()
}

// KeyRef names a private key by its file, never its bytes: the path the
// connecting process reads and the SHA-256 fingerprint of its public key
// as seen at planning (SHA256:..., OpenSSH's spelling).
type KeyRef struct {
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
}

type Credential struct {
	Material     Material               `json:"-"`
	Backend      string                 `json:"backend"`
	MatchedOn    Match                  `json:"matched_on"`
	Policy       string                 `json:"policy"`
	FieldSources map[string]FieldSource `json:"field_sources"`
	// Keys are the keys the credential offers, in order; a credential
	// with keys needs no password.
	Keys []KeyRef `json:"keys,omitempty"`
}

// Notice is a resolution notice about one device, carried to its first
// record and shown by a dry run: the code, the operator message, and
// string details that never hold a secret.
type Notice struct {
	Code    string
	Message string
	Details map[string]string
}

type ResolveRequest struct {
	Operator         Operator
	Device           inventory.Device
	Policy           string
	UsernameTemplate string
	RequiredEnable   bool
	Now              time.Time
}

type BackendResult struct {
	Outcome    Outcome
	Credential Credential
	ErrorCode  string
	Message    string
	Retryable  bool
}

type Backend interface {
	Name() string
	Mode() BackendMode
	Resolve(ctx context.Context, req ResolveRequest) BackendResult
}

// Keyed is the capability of honouring a credential key: a backend that
// can answer "the credential whose key is K" for a
// device pinned by the inventory's credkeyref. It is a capability a backend
// declares, not a backend type: the credential CSV has it, and a later keyed
// store (a SQLite credential store, a JSON credential file) joins by
// implementing it. The resolver asks only keyed backends for a pinned
// device and skips every other.
//
// A backend that returns true promises that, for a request whose
// Device.CredKeyRef is set, it answers Success only with the credential
// holding that key and NotFound when it holds no such key; it never answers
// a pinned request with a general credential. The method returns a bool so
// that one backend type can hold the capability under one configuration and
// not another.
type Keyed interface {
	HonoursCredKey() bool
}

type Resolved struct {
	Credential     Credential
	DeviceUsername string // explicit safe projection used for accountability/audit
	Notices        []Notice
}

type Resolver interface {
	Resolve(ctx context.Context, operator Operator, device inventory.Device) (Resolved, error)
}
