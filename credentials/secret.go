package credentials

import (
	"crypto/subtle"
	"fmt"
	"io"
	"log/slog"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

const redacted = "<redacted>"

// SecretString is the public secret-bearing string. It is a
// value type holding one pointer, with every method on the value receiver,
// so a by-value copy still redacts under fmt at any depth and still refuses
// every encoder whether or not the value is addressable. Plaintext is
// reachable only through WithBytes, scoped to the call.
type SecretString struct{ v *secrets.Value }

// SecretBytes is SecretString for byte content such as an encoded credential
// package or an askpass response.
type SecretBytes struct{ v *secrets.Value }

func NewSecretString(s string) SecretString          { return SecretString{v: secrets.New(s)} }
func NewSecretStringFromBytes(b []byte) SecretString { return SecretString{v: secrets.NewBytes(b)} }
func NewSecretBytes(b []byte) SecretBytes            { return SecretBytes{v: secrets.NewBytes(b)} }

func refuse(what string) error {
	return errorcodes.Errorf("secret_serialization_refused", "secret values cannot be %s encoded", what)
}

func withBytes(v *secrets.Value, fn func([]byte) error) error {
	if v == nil {
		return errorcodes.Errorf("secret_unset", "secret value is unset")
	}
	return v.WithBytes(fn)
}

func equal(a, b *secrets.Value) bool {
	aSet, bSet := a != nil && a.IsSet(), b != nil && b.IsSet()
	if !aSet || !bSet {
		return aSet == bSet
	}
	same := false
	_ = a.WithBytes(func(x []byte) error {
		return b.WithBytes(func(y []byte) error {
			same = subtle.ConstantTimeCompare(x, y) == 1
			return nil
		})
	})
	return same
}

func destroy(v *secrets.Value) {
	if v != nil {
		v.Destroy()
	}
}

// IsSet reports a non-empty, non-destroyed value.
func (s SecretString) IsSet() bool { return s.v != nil && s.v.IsSet() }

// Len is the byte length, 0 when unset or destroyed.
func (s SecretString) Len() int {
	if s.v == nil {
		return 0
	}
	return s.v.Len()
}

// WithBytes passes a copy of the plaintext to fn and wipes the copy after.
func (s SecretString) WithBytes(fn func([]byte) error) error { return withBytes(s.v, fn) }

// Equal compares in constant time; two unset values are equal.
func (s SecretString) Equal(o SecretString) bool { return equal(s.v, o.v) }

// Destroy wipes the shared value; every copy then reads as unset.
func (s SecretString) Destroy() { destroy(s.v) }

func (s SecretString) String() string                    { return redacted }
func (s SecretString) GoString() string                  { return redacted }
func (s SecretString) Format(f fmt.State, _ rune)        { _, _ = io.WriteString(f, redacted) }
func (s SecretString) LogValue() slog.Value              { return slog.StringValue(redacted) }
func (s SecretString) MarshalJSON() ([]byte, error)      { return nil, refuse("JSON") }
func (s SecretString) MarshalText() ([]byte, error)      { return nil, refuse("text") }
func (s SecretString) AppendText([]byte) ([]byte, error) { return nil, refuse("text") }
func (s SecretString) GobEncode() ([]byte, error)        { return nil, refuse("gob") }
func (s SecretString) MarshalBinary() ([]byte, error)    { return nil, refuse("binary") }

func (s SecretBytes) IsSet() bool { return s.v != nil && s.v.IsSet() }
func (s SecretBytes) Len() int {
	if s.v == nil {
		return 0
	}
	return s.v.Len()
}
func (s SecretBytes) WithBytes(fn func([]byte) error) error { return withBytes(s.v, fn) }
func (s SecretBytes) Equal(o SecretBytes) bool              { return equal(s.v, o.v) }
func (s SecretBytes) Destroy()                              { destroy(s.v) }
func (s SecretBytes) String() string                        { return redacted }
func (s SecretBytes) GoString() string                      { return redacted }
func (s SecretBytes) Format(f fmt.State, _ rune)            { _, _ = io.WriteString(f, redacted) }
func (s SecretBytes) LogValue() slog.Value                  { return slog.StringValue(redacted) }
func (s SecretBytes) MarshalJSON() ([]byte, error)          { return nil, refuse("JSON") }
func (s SecretBytes) MarshalText() ([]byte, error)          { return nil, refuse("text") }
func (s SecretBytes) AppendText([]byte) ([]byte, error)     { return nil, refuse("text") }
func (s SecretBytes) GobEncode() ([]byte, error)            { return nil, refuse("gob") }
func (s SecretBytes) MarshalBinary() ([]byte, error)        { return nil, refuse("binary") }
