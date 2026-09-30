package credentialpackage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
)

// Protected is what a channel carries. Body holds
// the canonical package bytes under local-peer and is empty under sealed,
// whose ciphertext travels in Envelope.Sealed. It refuses every sink as the
// package does.
type Protected struct {
	Envelope Envelope
	Body     credentials.SecretBytes
}

// Destroy wipes the body.
func (p Protected) Destroy() { p.Body.Destroy() }

func refuseProtected(what string) error {
	return fmt.Errorf("secret_serialization_refused: a protected credential package cannot be %s encoded", what)
}

func (p Protected) String() string                    { return redacted }
func (p Protected) GoString() string                  { return redacted }
func (p Protected) Format(f fmt.State, _ rune)        { _, _ = io.WriteString(f, redacted) }
func (p Protected) LogValue() slog.Value              { return slog.StringValue(redacted) }
func (p Protected) MarshalJSON() ([]byte, error)      { return nil, refuseProtected("JSON") }
func (p Protected) MarshalText() ([]byte, error)      { return nil, refuseProtected("text") }
func (p Protected) AppendText([]byte) ([]byte, error) { return nil, refuseProtected("text") }
func (p Protected) GobEncode() ([]byte, error)        { return nil, refuseProtected("gob") }
func (p Protected) MarshalBinary() ([]byte, error)    { return nil, refuseProtected("binary") }

// Protector turns a package into what a channel carries and back: the
// PackageProtector. The local-peer implementation is the
// identity over the canonical bytes with the audience checked; a sealed
// provider would compose a Sealer. Protect derives the envelope and the
// body from one package; Unprotect returns the package and leaves the
// caller's body to the caller to destroy.
type Protector interface {
	Protection() Protection
	Protect(ctx context.Context, p CredentialPackage) (Protected, error)
	Unprotect(ctx context.Context, in Protected, audience string) (CredentialPackage, error)
}

// Sealer is the contract a reviewed sealed provider implements over bytes.
// The package owns encoding on both
// sides, so no provider reaches the shadow type; the associated data a
// provider binds is AssociatedData of the envelope. Not composed into a
// Protector in v1: ProtectorFor(ProtectionSealed) answers
// protection_unsupported until a channel carries sealed packages.
type Sealer interface {
	KeyID() string
	Seal(ctx context.Context, aad []byte, plaintext credentials.SecretBytes) (SealedPayload, error)
	Open(ctx context.Context, aad []byte, payload SealedPayload) (credentials.SecretBytes, error)
}

// AssociatedData is what a Sealer binds: the envelope's canonical JSON with
// sealed and package_digest cleared, so issuer, audience, job ID, plan
// digest, and times are covered by the cipher.
func AssociatedData(e Envelope) ([]byte, error) {
	e.Sealed = nil
	e.PackageDigest = executionplan.Digest{}
	return json.Marshal(e)
}

// ProtectorFor returns the protector for a protection. A provider is added
// here under review; there is no registration function.
func ProtectorFor(p Protection) (Protector, error) {
	switch p {
	case ProtectionLocalPeer:
		return localPeer{}, nil
	case ProtectionSealed:
		return nil, invalid("protection_unsupported", "sealed packages are not accepted in v1")
	default:
		return nil, invalid("protection", "%q is not local-peer or sealed", string(p))
	}
}

// localPeer is the in-memory same-UID protector: the body is the canonical
// package and the envelope its safe projection.
type localPeer struct{}

func (localPeer) Protection() Protection { return ProtectionLocalPeer }

func (localPeer) Protect(_ context.Context, p CredentialPackage) (Protected, error) {
	if p.Protection != ProtectionLocalPeer {
		return Protected{}, invalid("protection", "package protection %q is not local-peer", string(p.Protection))
	}
	proj, err := p.SafeProjection()
	if err != nil {
		return Protected{}, err
	}
	body, _, err := encode(p)
	if err != nil {
		return Protected{}, err
	}
	return Protected{Envelope: proj.Envelope(), Body: body}, nil
}

func (localPeer) Unprotect(_ context.Context, in Protected, audience string) (CredentialPackage, error) {
	e := in.Envelope
	switch e.Protection {
	case ProtectionLocalPeer:
	case ProtectionSealed:
		return CredentialPackage{}, invalid("protection_unsupported", "sealed packages are not accepted in v1")
	default:
		return CredentialPackage{}, invalid("protection", "%q is not local-peer or sealed", string(e.Protection))
	}
	if err := e.Validate(); err != nil {
		return CredentialPackage{}, err
	}
	if !containsString(e.Audience, audience) {
		return CredentialPackage{}, invalid("audience", "envelope audience %v does not include %q", e.Audience, audience)
	}
	if !in.Body.IsSet() || in.Body.Len() == 0 {
		return CredentialPackage{}, invalid("body", "a local-peer package has no body")
	}
	pkg, err := decode(in.Body)
	if err != nil {
		return CredentialPackage{}, err
	}
	proj, err := pkg.SafeProjection()
	if err != nil {
		pkg.Destroy()
		return CredentialPackage{}, err
	}
	if err := e.Matches(proj); err != nil {
		pkg.Destroy()
		return CredentialPackage{}, err
	}
	return pkg, nil
}
