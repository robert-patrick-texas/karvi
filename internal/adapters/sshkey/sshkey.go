// Package sshkey reads an OpenSSH private key file's contents for the
// planner's judgement of the operator's keys: whether both transports can
// sign with it without a passphrase, and its public key's fingerprint. It is
// the adapter that keeps golang.org/x/crypto/ssh out of the credential code,
// as internal/transport/native keeps it out of the session.
package sshkey

import (
	"bytes"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"
)

// The reasons a key is not used, as operator_key_skipped names them.
const (
	ReasonPassphrase  = "protected by a passphrase"
	ReasonDSA         = "a DSA key, no longer used"
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
	case isDSA(data):
		// Ahead of the passphrase: without it the key is still not used.
		return "", ReasonDSA
	case errors.As(err, &missing):
		return "", ReasonPassphrase
	case strings.Contains(err.Error(), "unhandled key type"):
		// An OpenSSH key of a type x/crypto has no signer for: the
		// hardware-backed sk- keys, which need their device.
		return "", ReasonUnsupported
	}
	return "", ReasonUnreadable
}

// oidDSA is the DSA algorithm's identifier in a PKCS#8 file (RFC 3279).
var oidDSA = asn1.ObjectIdentifier{1, 2, 840, 10040, 4, 1}

// isDSA reports whether a key file names a DSA key, which x/crypto reads in
// no form: by a PEM block's type, by the public key's type in an OpenSSH
// block's header, which a passphrase leaves readable, or by a PKCS#8
// block's algorithm. A PKCS#8 block under a passphrase names no type. Every
// block is asked: OpenSSL writes a key's parameters in a block ahead of it.
func isDSA(data []byte) bool {
	for block, rest := pem.Decode(data); block != nil; block, rest = pem.Decode(rest) {
		if isDSABlock(block) {
			return true
		}
	}
	return false
}

func isDSABlock(block *pem.Block) bool {
	switch block.Type {
	case "DSA PRIVATE KEY":
		return true
	case "OPENSSH PRIVATE KEY":
		rest, ok := bytes.CutPrefix(block.Bytes, []byte("openssh-key-v1\x00"))
		if !ok {
			return false
		}
		var header struct {
			Cipher, KDF, KDFOptions string
			Keys                    uint32
			Public                  []byte
			Rest                    []byte `ssh:"rest"`
		}
		var public struct {
			Type string
			Rest []byte `ssh:"rest"`
		}
		return ssh.Unmarshal(rest, &header) == nil && ssh.Unmarshal(header.Public, &public) == nil && public.Type == "ssh-dss"
	case "PRIVATE KEY":
		var file struct {
			Version   int
			Algorithm pkix.AlgorithmIdentifier
			Key       []byte
		}
		_, err := asn1.Unmarshal(block.Bytes, &file)
		return err == nil && file.Algorithm.Algorithm.Equal(oidDSA)
	}
	return false
}
