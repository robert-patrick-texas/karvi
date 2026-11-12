package sshkey_test

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey"
	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey/sshkeytest"
)

// TestInspect: a key without a passphrase gives its fingerprint, one with a
// passphrase and a hardware-backed one their reasons, a DSA key its own in
// every form that names the type (PKCS#8 under a passphrase names none),
// anything else is not a key; no case returns the contents.
func TestInspect(t *testing.T) {
	plain, want := sshkeytest.Ed25519(t, "")
	if fp, reason := sshkey.Inspect(plain); reason != "" || fp != want {
		t.Fatalf("plain: %q %q, want %q", fp, reason, want)
	}
	locked, _ := sshkeytest.Ed25519(t, "lab passphrase")
	if fp, reason := sshkey.Inspect(locked); fp != "" || reason != sshkey.ReasonPassphrase {
		t.Fatalf("passphrase: %q %q", fp, reason)
	}
	if fp, reason := sshkey.Inspect(sshkeytest.SK(t)); fp != "" || reason != sshkey.ReasonUnsupported {
		t.Fatalf("sk: %q %q", fp, reason)
	}
	if fp, reason := sshkey.Inspect([]byte("not a key\n")); fp != "" || reason != sshkey.ReasonUnreadable {
		t.Fatalf("garbage: %q %q", fp, reason)
	}
	dsa := sshkeytest.DSA(t)
	if len(dsa) != 6 {
		t.Fatalf("DSA forms: %d", len(dsa))
	}
	// A key's parameters in a block ahead of it, as openssl dsaparam -genkey
	// writes them.
	dsa["parameters first"] = append(pem.EncodeToMemory(&pem.Block{Type: "DSA PARAMETERS", Bytes: []byte{0x30, 0}}), dsa["pem"]...)
	for form, file := range dsa {
		want := sshkey.ReasonDSA
		if form == "pkcs8-locked" {
			want = sshkey.ReasonUnreadable
		}
		if fp, reason := sshkey.Inspect(file); fp != "" || reason != want {
			t.Errorf("DSA %s: %q %q, want %q", form, fp, reason, want)
		}
	}
	// No DSA key: the type's name outside a key file's own places, an
	// OpenSSH header that ends before its key, and a PKCS#8 key of a type
	// no transport signs with.
	x25519, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := x509.MarshalPKCS8PrivateKey(x25519)
	if err != nil {
		t.Fatal(err)
	}
	for name, file := range map[string][]byte{
		"a public key line":        []byte("ssh-dss AAAAB3NzaC1kc3M= lab\n"),
		"an OpenSSH header cut":    []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEA\n-----END OPENSSH PRIVATE KEY-----\n"),
		"a PKCS#8 block of no key": []byte("-----BEGIN PRIVATE KEY-----\nc3NoLWRzcw==\n-----END PRIVATE KEY-----\n"),
		"an X25519 PKCS#8 key":     pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: other}),
	} {
		if fp, reason := sshkey.Inspect(file); fp != "" || reason != sshkey.ReasonUnreadable {
			t.Errorf("%s: %q %q", name, fp, reason)
		}
	}
}
