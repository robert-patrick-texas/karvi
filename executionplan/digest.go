// Package executionplan holds the public, non-secret execution-plan contracts.
// It imports only the inventory
// model and the standard library, so nothing in it can name a secret type.
package executionplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Digest is a SHA-256 value. Its JSON form is the 64-character lowercase hex
// string used throughout karvi records; the zero value is "unset".
type Digest [32]byte

// Sum returns the SHA-256 of data.
func Sum(data []byte) Digest { return Digest(sha256.Sum256(data)) }

// SumJSON returns the SHA-256 of the JSON encoding of v. Every plan digest
// uses this rule: the canonical form is the wire form, with the digest field
// itself cleared by the caller before encoding.
func SumJSON(v any) (Digest, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Digest{}, err
	}
	return Sum(data), nil
}

// IsZero reports whether the digest is unset; encoding/json honors it for
// the omitzero tag.
func (d Digest) IsZero() bool { return d == Digest{} }

// String returns the lowercase hex form.
func (d Digest) String() string { return hex.EncodeToString(d[:]) }

// MarshalText encodes the digest as lowercase hex.
func (d Digest) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText accepts exactly 64 lowercase hex characters.
func (d *Digest) UnmarshalText(text []byte) error {
	if len(text) != 64 {
		return fmt.Errorf("digest must be 64 hex characters, got %d", len(text))
	}
	for _, c := range text {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return fmt.Errorf("digest must be lowercase hex")
		}
	}
	_, err := hex.Decode(d[:], text)
	return err
}

// ParseDigest parses the hex form used in records.
func ParseDigest(s string) (Digest, error) {
	var d Digest
	err := d.UnmarshalText([]byte(s))
	return d, err
}
