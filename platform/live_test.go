package platform

import (
	"bytes"
	"strings"
	"testing"
)

// TestSecretFilterRemovesEachSecret: every exact value is written
// <removed>, whole or cut across writes at any byte, the longer of two
// secrets that begin at one byte winning; a tail that only begins a secret
// waits, and Flush writes it as it is.
func TestSecretFilterRemovesEachSecret(t *testing.T) {
	const device = "Router>enable\r\nPassword: Zq7-SECRET\r\nRouter#show run\r\nenable password Zq7-SECRET-2\r\nZq7"
	const want = "Router>enable\r\nPassword: <removed>\r\nRouter#show run\r\nenable password <removed>\r\nZq7"
	for cut := 0; cut <= len(device); cut++ {
		var out bytes.Buffer
		f := SecretFilter{W: &out}
		f.Add([]byte("Zq7-SECRET"))
		f.Add([]byte("Zq7-SECRET-2"))
		f.Add(nil)
		if _, err := f.Write([]byte(device[:cut])); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(device[cut:])); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "SECRET") {
			t.Fatalf("cut %d: a secret was written: %q", cut, out.String())
		}
		if !strings.HasSuffix(out.String(), "enable password <removed>\r\n") {
			t.Fatalf("cut %d: the tail that begins a secret was written before Flush: %q", cut, out.String())
		}
		if err := f.Flush(); err != nil {
			t.Fatal(err)
		}
		if out.String() != want {
			t.Fatalf("cut %d:\n got %q\nwant %q", cut, out.String(), want)
		}
	}
}

// TestSecretFilterWithoutSecretsPassesEverything: no secret, no wait.
func TestSecretFilterWithoutSecretsPassesEverything(t *testing.T) {
	var out bytes.Buffer
	f := SecretFilter{W: &out}
	if _, err := f.Write([]byte("Router#")); err != nil || out.String() != "Router#" {
		t.Fatalf("wrote %q, %v", out.String(), err)
	}
}

// TestSecretFilterWipe: Wipe zeroes the copies it held and forgets them, so
// the value passes after it.
func TestSecretFilterWipe(t *testing.T) {
	var out bytes.Buffer
	f := SecretFilter{W: &out}
	f.Add([]byte("Zq7-SECRET"))
	held := f.secrets[0]
	f.Wipe()
	if !bytes.Equal(held, make([]byte, len(held))) || f.secrets != nil {
		t.Fatalf("held %q after Wipe, secrets %q", held, f.secrets)
	}
}
