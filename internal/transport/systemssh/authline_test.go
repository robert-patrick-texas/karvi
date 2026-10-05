package systemssh

import (
	"bytes"
	"testing"
)

// TestAuthFilter: the line naming the method is taken out, whatever the
// writes' boundaries and line ends; every other line passes whole, and a
// last line without a newline at Flush.
func TestAuthFilter(t *testing.T) {
	var out bytes.Buffer
	f := &authFilter{next: &out}
	for _, chunk := range []string{"Warning: Permanently added 'x' (ED25519) to the list of known hosts.\r\n", "Authenticated to 127.0.0.1 ([127.0.0.1]:22) us", "ing \"publickey\".\r\nConnection to 127.0.0.1 closed.\r\n", "Transferred: sent 1"} {
		if _, err := f.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if f.Method() != "publickey" {
		t.Fatalf("method %q", f.Method())
	}
	if out.String() != "Warning: Permanently added 'x' (ED25519) to the list of known hosts.\r\nConnection to 127.0.0.1 closed.\r\n" {
		t.Fatalf("passed %q", out.String())
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
