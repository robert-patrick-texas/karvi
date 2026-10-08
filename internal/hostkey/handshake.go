package hostkey

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// keyTypeOf is the known_hosts key type an algorithm verifies.
func keyTypeOf(algorithm string) string {
	switch algorithm {
	case "rsa-sha2-512", "rsa-sha2-256":
		return "ssh-rsa"
	}
	return algorithm
}

// EnrolledTypes are the key types the store holds under identity, in file
// order without repeats. A missing store holds none.
func EnrolledTypes(file, identity string) ([]string, error) {
	keys, err := matchingKeys(file, []string{identity})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, key := range keys {
		if !containsString(out, key.Type) {
			out = append(out, key.Type)
		}
	}
	return out, nil
}

// HostKeyAlgorithms is the device's host-key list (the configured list a
// transport implements) as offered to a device known under identity: under insecure, or with no entry, the whole
// list; otherwise the list filtered to the enrolled key types, so a match
// is decided on a key the store knows. Entries leaving nothing to offer
// are host_key_changed before any connection.
func (p Policy) HostKeyAlgorithms(list []string, identity string) ([]string, error) {
	all := append([]string(nil), list...)
	if p.Mode == Insecure || identity == "" {
		return all, nil
	}
	types, err := EnrolledTypes(p.KnownHostsFile, identity)
	if err != nil {
		return nil, errorCode(err, "host_key_trust_store_unavailable", identity, p.KnownHostsFile)
	}
	if len(types) == 0 {
		return all, nil
	}
	var out []string
	for _, algorithm := range all {
		if containsString(types, keyTypeOf(algorithm)) {
			out = append(out, algorithm)
		}
	}
	if len(out) == 0 {
		return nil, &Error{Code: "host_key_changed", Host: identity, Path: p.KnownHostsFile, Err: fmt.Errorf("the store holds only key types the host-key algorithm list does not offer (%s)", strings.Join(types, ", "))}
	}
	return out, nil
}

// Verdict is what Verify found beside its error: Stored when this call
// stored the key under accept-new (false when another process stored the
// same key under the lock meanwhile), Mismatch when insecure accepted a key
// different from the stored one. The caller says either, on the device's
// first record (EnrolledPhrase, MismatchPhrase).
type Verdict struct {
	Stored   bool
	Mismatch *KeyMismatch
}

// Verify applies the policy to the one host key a handshake presents:
// keyType is its SSH name (ssh-ed25519, ecdsa-sha2-nistp256, ssh-rsa) and
// wire its wire encoding. accept-new enrolls an unknown host under the file
// lock and rejects a changed key; secure rejects both; insecure accepts
// both, a key differing from the stored one reported in the Verdict.
func Verify(p Policy, host string, port int, keyType string, wire []byte) (Verdict, error) {
	presented := []keyRecord{{Type: keyType, Blob: base64.StdEncoding.EncodeToString(wire)}}
	enrolled, err := matchingKeys(p.KnownHostsFile, []string{Identity(host, port)})
	if errors.Is(err, os.ErrNotExist) {
		enrolled, err = nil, nil
	}
	if err != nil {
		if p.Mode != Insecure {
			return Verdict{}, errorCode(err, "host_key_trust_store_unavailable", host, p.KnownHostsFile)
		}
		enrolled = nil
	}
	inspection := Inspection{Presented: presented, PresentedFingerprints: fingerprints(presented), EnrolledFingerprints: fingerprints(enrolled)}
	switch {
	case len(enrolled) == 0:
		inspection.Comparison = Unknown
	case sameKey(enrolled, presented[0]):
		inspection.Comparison = Match
	default:
		inspection.Comparison = Mismatch
	}
	if p.Mode == Insecure {
		if inspection.Comparison == Mismatch {
			return Verdict{Mismatch: &KeyMismatch{Enrolled: inspection.EnrolledFingerprints, Presented: inspection.PresentedFingerprints}}, nil
		}
		return Verdict{}, nil
	}
	switch inspection.Comparison {
	case Match:
		return Verdict{}, nil
	case Mismatch:
		return Verdict{}, &Error{Code: "host_key_changed", Host: host, Path: p.KnownHostsFile, Err: errors.New(mismatchDescription(host, inspection))}
	}
	if p.Mode == Secure {
		return Verdict{}, &Error{Code: "host_key_not_enrolled", Host: host, Path: p.KnownHostsFile, Err: errors.New("secure mode requires a matching key before access")}
	}
	stored, err := Enroll(p.KnownHostsFile, host, port, presented)
	return Verdict{Stored: stored}, err
}

// TypeLabel is OpenSSH's name for a key of the SSH type keyType, the one its
// "Permanently added" line gives: ED25519, ECDSA, RSA, DSA, ED25519-SK,
// ECDSA-SK; an unknown type in capitals.
func TypeLabel(keyType string) string {
	switch {
	case keyType == "ssh-ed25519":
		return "ED25519"
	case strings.HasPrefix(keyType, "ecdsa-sha2-"):
		return "ECDSA"
	case keyType == "ssh-rsa" || strings.HasPrefix(keyType, "rsa-sha2-"):
		return "RSA"
	case keyType == "ssh-dss":
		return "DSA"
	case keyType == "sk-ssh-ed25519@openssh.com":
		return "ED25519-SK"
	case keyType == "sk-ecdsa-sha2-nistp256@openssh.com":
		return "ECDSA-SK"
	}
	return strings.ToUpper(keyType)
}

// Phrase is a host-key line's words around the device's name, so that a
// terminal draws the name in its own colour: the notice's message is
// Text(device), and a client's line "! " and the same.
type Phrase struct {
	Before, After string
}

// Text is the phrase around the device's name.
func (p Phrase) Text(device string) string { return p.Before + device + p.After }

// EnrolledPhrase is the host_key_enrolled notice's: this session stored the
// device's key of OpenSSH's type label.
func EnrolledPhrase(label string) Phrase {
	return Phrase{Before: "ssh accepted new host-key ", After: " (" + label + ")"}
}

// TypesNotOffered is the failure of a handshake that offered only the key
// types enrolled under identity to a device offering none of them: the key
// changed.
func TypesNotOffered(p Policy, identity string, offered []string) error {
	host := identity
	enrolled, _ := matchingKeys(p.KnownHostsFile, []string{identity})
	return &Error{Code: "host_key_changed", Host: host, Path: p.KnownHostsFile, Err: fmt.Errorf("SSH host key mismatch for %s: enrolled=%s; the device offers no key of the enrolled types (offered: %s)", host, strings.Join(fingerprints(enrolled), ","), strings.Join(offered, ","))}
}

func sameKey(keys []keyRecord, key keyRecord) bool {
	for _, k := range keys {
		if k.Type == key.Type && k.Blob == key.Blob {
			return true
		}
	}
	return false
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func errorCode(err error, code, host, path string) error {
	if errorcodes.Of(err) != "" {
		return err
	}
	return &Error{Code: code, Host: host, Path: path, Err: err}
}
