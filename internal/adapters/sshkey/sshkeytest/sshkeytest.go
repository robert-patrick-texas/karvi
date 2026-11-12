// Package sshkeytest makes private key files for tests: an Ed25519 key with
// and without a passphrase, an RSA key, and the files no transport signs
// with, a hardware-backed key's and a DSA key's. Only test binaries import
// it.
package sshkeytest

import (
	"crypto/dsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"math/big"
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

// RSA returns a new 2048-bit key's file, without a passphrase, and its
// public key's fingerprint.
func RSA(t testing.TB) (file []byte, fingerprint string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
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

// DSA returns one new DSA key's file in each form a key file takes, by
// name: "pem" (BEGIN DSA PRIVATE KEY), "openssh" (ssh-keygen's own format),
// and "pkcs8", each also under a passphrase ("pem-locked", "openssh-locked",
// "pkcs8-locked"). A locked form's private part is random bytes.
func DSA(t testing.TB) map[string][]byte {
	t.Helper()
	var k dsa.PrivateKey
	if err := dsa.GenerateParameters(&k.Parameters, rand.Reader, dsa.L1024N160); err != nil {
		t.Fatal(err)
	}
	if err := dsa.GenerateKey(&k, rand.Reader); err != nil {
		t.Fatal(err)
	}
	der := func(v any) []byte {
		t.Helper()
		out, err := asn1.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	random := make([]byte, 448)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	legacy := der(struct {
		Version       int
		P, Q, G, Y, X *big.Int
	}{0, k.P, k.Q, k.G, k.Y, k.X})
	pkcs8 := der(struct {
		Version   int
		Algorithm pkix.AlgorithmIdentifier
		Key       []byte
	}{0, pkix.AlgorithmIdentifier{
		Algorithm:  asn1.ObjectIdentifier{1, 2, 840, 10040, 4, 1},
		Parameters: asn1.RawValue{FullBytes: der(struct{ P, Q, G *big.Int }{k.P, k.Q, k.G})},
	}, der(k.X)})
	const algo = "ssh-dss"
	pub := ssh.Marshal(struct {
		Algo       string
		P, Q, G, Y *big.Int
	}{algo, k.P, k.Q, k.G, k.Y})
	priv := ssh.Marshal(struct {
		Check1, Check2 uint32
		Algo           string
		P, Q, G, Y, X  *big.Int
		Comment        string
	}{1, 1, algo, k.P, k.Q, k.G, k.Y, k.X, ""})
	for pad := byte(1); len(priv)%8 != 0; pad++ {
		priv = append(priv, pad)
	}
	openssh := func(cipher, kdf, options string, private []byte) []byte {
		body := append([]byte("openssh-key-v1\x00"), ssh.Marshal(struct {
			Cipher, KDF, KDFOptions string
			Keys                    uint32
			Public, Private         string
		}{cipher, kdf, options, 1, string(pub), string(private)})...)
		return pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body})
	}
	bcrypt := string(ssh.Marshal(struct {
		Salt   string
		Rounds uint32
	}{"0123456789abcdef", 16}))
	return map[string][]byte{
		"pem": pem.EncodeToMemory(&pem.Block{Type: "DSA PRIVATE KEY", Bytes: legacy}),
		"pem-locked": pem.EncodeToMemory(&pem.Block{Type: "DSA PRIVATE KEY", Bytes: random,
			Headers: map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-128-CBC,000102030405060708090A0B0C0D0E0F"}}),
		"openssh":        openssh("none", "none", "", priv),
		"openssh-locked": openssh("aes256-ctr", "bcrypt", bcrypt, random),
		"pkcs8":          pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}),
		"pkcs8-locked":   pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: random}),
	}
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
