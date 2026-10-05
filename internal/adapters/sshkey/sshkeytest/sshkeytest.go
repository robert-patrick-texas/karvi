// Package sshkeytest makes private key files for tests: an Ed25519 key with
// and without a passphrase, and the file of a hardware-backed key, which
// no transport signs with. Only test binaries import it.
package sshkeytest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Ed25519 returns a new key's file, without a passphrase when passphrase
// is empty, and its public key's fingerprint.
func Ed25519(t testing.TB, passphrase string) (file []byte, fingerprint string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block), ssh.FingerprintSHA256(signer.PublicKey())
}

// SK returns the file of an unencrypted sk-ssh-ed25519@openssh.com key as
// ssh-keygen -t ed25519-sk writes it, its handle made up: the key lives in
// its device, and the file names it.
func SK(t testing.TB) []byte {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const algo = "sk-ssh-ed25519@openssh.com"
	pubBlob := ssh.Marshal(struct {
		Algo, Key, Application string
	}{algo, string(pub), "ssh:"})
	priv := ssh.Marshal(struct {
		Check1, Check2                 uint32
		Algo, Key, Application         string
		Flags                          uint8
		Handle, Reserved, Comment, Pad string
	}{1, 1, algo, string(pub), "ssh:", 1, "handle", "", "", "\x01\x02\x03"})
	body := append([]byte("openssh-key-v1\x00"), ssh.Marshal(struct {
		Cipher, KDF, KDFOptions string
		Keys                    uint32
		Public, Private         string
	}{"none", "none", "", 1, string(pubBlob), string(priv)})...)
	return pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body})
}

// Authorized is a key file's public key as an authorized_keys line.
func Authorized(t testing.TB, file []byte) []byte {
	t.Helper()
	signer, err := ssh.ParsePrivateKey(file)
	if err != nil {
		t.Fatal(err)
	}
	return ssh.MarshalAuthorizedKey(signer.PublicKey())
}
