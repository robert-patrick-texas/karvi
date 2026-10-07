package systemssh

import (
	"bytes"
	"testing"
)

// TestAuthFilter: the line naming the method and the line saying the host's
// key was stored are taken out, whatever the writes' boundaries and line
// ends, the second reported with its label; every other line passes whole,
// and a last line without a newline at Flush.
func TestAuthFilter(t *testing.T) {
	var out bytes.Buffer
	var labels []string
	f := &authFilter{next: &out, enrolled: func(label string) { labels = append(labels, label) }}
	for _, chunk := range []string{"Warning: Permanently added '[127.0.0.1]:2222' (ED", "25519) to the list of known hosts.\r\n", "Authenticated to 127.0.0.1 ([127.0.0.1]:22) us", "ing \"publickey\".\r\nConnection to 127.0.0.1 closed.\r\n", "Transferred: sent 1"} {
		if _, err := f.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if f.Method() != "publickey" {
		t.Fatalf("method %q", f.Method())
	}
	if out.String() != "Connection to 127.0.0.1 closed.\r\n" || len(labels) != 1 || labels[0] != "ED25519" {
		t.Fatalf("passed %q, enrolled %q", out.String(), labels)
	}
	f.Flush()
	if !bytes.HasSuffix(out.Bytes(), []byte("closed.\r\nTransferred: sent 1")) {
		t.Fatalf("after Flush %q", out.String())
	}
	g := &authFilter{next: &out}
	_, _ = g.Write([]byte("Authenticated to h ([192.0.2.1]:22) using \"keyboard-interactive\".\n"))
	if g.Method() != "keyboard-interactive" {
		t.Fatalf("method %q", g.Method())
	}
}

// TestEnrolledLabel: OpenSSH's enrollment line for each key type, an IPv6
// identity, a host alias with a port; nothing else.
func TestEnrolledLabel(t *testing.T) {
	for line, want := range map[string]string{
		"Warning: Permanently added '127.0.0.1' (ED25519) to the list of known hosts.":       "ED25519",
		"Warning: Permanently added '[fake-iosxe]:2222' (ECDSA) to the list of known hosts.": "ECDSA",
		"Warning: Permanently added '2001:db8::1' (RSA) to the list of known hosts.\r":       "RSA",
		"Warning: Permanently added 'r1' (ED25519-SK) to the list of known hosts.":           "ED25519-SK",
	} {
		if got, ok := enrolledLabel(line); !ok || got != want {
			t.Errorf("enrolledLabel(%q) = %q, %t", line, got, ok)
		}
	}
	for _, line := range []string{"Warning: the ECDSA host key for 'r1' differs from the key for the IP address '192.0.2.1'", "AUTHORIZED ACCESS ONLY", ""} {
		if _, ok := enrolledLabel(line); ok {
			t.Errorf("enrolledLabel(%q) matched", line)
		}
	}
}
