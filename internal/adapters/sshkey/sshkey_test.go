package sshkey_test

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey"
	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey/sshkeytest"
)

// TestInspect: a key without a passphrase gives its fingerprint, one with a
// passphrase and a hardware-backed one their reasons, anything else is not
// a key; no case returns the contents.
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
}
