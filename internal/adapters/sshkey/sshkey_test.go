package sshkey_test

import (
	"bytes"
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
// every form that names the type (PKCS#8 under a passphrase names none and
// gets the passphrase's), anything else is not a key; no case returns the
// contents.
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
			want = sshkey.ReasonPassphrase
		}
		if fp, reason := sshkey.Inspect(file); fp != "" || reason != want {
			t.Errorf("DSA %s: %q %q, want %q", form, fp, reason, want)
		}
	}
	// PKCS#8 under a passphrase is the first block's type, the block
	// x/crypto reads, and that type whole.
	locked8 := dsa["pkcs8-locked"]
	parameters := pem.EncodeToMemory(&pem.Block{Type: "EC PARAMETERS", Bytes: []byte{0x06, 0}})
	for name, file := range map[string][]byte{
		"a block ahead of PKCS#8 under a passphrase": append(parameters, locked8...),
		"a longer block type":                        bytes.Replace(locked8, []byte("ENCRYPTED PRIVATE KEY"), []byte("ENCRYPTED PRIVATE KEYS"), 2),
		"a block type that ends as it":               bytes.Replace(locked8, []byte("ENCRYPTED PRIVATE KEY"), []byte("AN ENCRYPTED PRIVATE KEY"), 2),
		"the block type in small letters":            bytes.Replace(locked8, []byte("ENCRYPTED PRIVATE KEY"), []byte("encrypted private key"), 2),
		"the type in a header":                       pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEYS", Headers: map[string]string{"Type": "ENCRYPTED PRIVATE KEY"}, Bytes: []byte{0x30, 0}}),
	} {
		if fp, reason := sshkey.Inspect(file); fp != "" || reason != sshkey.ReasonUnreadable {
			t.Errorf("%s: %q %q", name, fp, reason)
		}
	}
	// Text ahead of the block and a certificate after it, as a PKCS#12
	// file's key and certificate are written out, leave the reason; a DSA
	// block after it gives DSA's, which is ahead of the passphrase's.
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{0x30, 0}})
	for name, tc := range map[string]struct {
		file []byte
		want string
	}{
		"text ahead of PKCS#8 under a passphrase": {append([]byte("Bag Attributes\n    friendlyName: lab\n"), locked8...), sshkey.ReasonPassphrase},
		"a certificate after it":                  {append(bytes.Clone(locked8), certificate...), sshkey.ReasonPassphrase},
		// The type alone decides: the block's contents are not read.
		"an empty block of the type": {pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY"}), sshkey.ReasonPassphrase},
		"a header line in the block": {pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Headers: map[string]string{"Comment": "lab"}, Bytes: []byte{0x30, 0}}), sshkey.ReasonPassphrase},
		"a DSA key after it":         {append(bytes.Clone(locked8), dsa["pem"]...), sshkey.ReasonDSA},
	} {
		if fp, reason := sshkey.Inspect(tc.file); fp != "" || reason != tc.want {
			t.Errorf("%s: %q %q, want %q", name, fp, reason, tc.want)
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
