// Package sshkey reads an OpenSSH private key file's contents for the
// planner's judgement of the operator's keys: whether both transports can
// sign with it without a passphrase, and its public key's fingerprint. It is
// the adapter that keeps golang.org/x/crypto/ssh out of the credential code,
// as internal/adapters/scrapligov1 keeps it out of the session.
package sshkey

import (
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"
)

// The reasons a key is not used, as operator_key_skipped names them.
const (
	ReasonPassphrase  = "protected by a passphrase"
	ReasonUnsupported = "a key type neither transport signs with (a hardware-backed key)"
	ReasonUnreadable  = "not a private key karvi reads"
)

// Inspect parses a private key's contents without a passphrase. It returns
// the public key's SHA-256 fingerprint (SHA256:..., OpenSSH's spelling) and
// "", or "" and the reason the key is not used. Nothing of the contents is
// kept or returned.
func Inspect(data []byte) (fingerprint, reason string) {
	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		return ssh.FingerprintSHA256(signer.PublicKey()), ""
	}
	var missing *ssh.PassphraseMissingError
	switch {
	case errors.As(err, &missing):
		return "", ReasonPassphrase
	case strings.Contains(err.Error(), "unhandled key type"):
		// An OpenSSH key of a type x/crypto has no signer for: the
		// hardware-backed sk- keys, which need their device.
		return "", ReasonUnsupported
	}
	return "", ReasonUnreadable
}
