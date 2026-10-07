// Package testsocket provides temporary directories for tests that create Unix
// domain sockets.
package testsocket

import (
	"os"
	"testing"
)

// maxBaseLen keeps socket paths within the 107-byte Linux sun_path limit after
// adding os.MkdirTemp's suffix (at most 13 bytes), a slash, and the longest
// socket name karvi makes (37 bytes, an askpass socket under a seven-digit
// pid).
const maxBaseLen = 56

// Dir returns a private directory that is removed when the test ends. Unlike
// t.TempDir, the path does not embed the test name, and it falls back to /tmp
// when TMPDIR is too long to hold a socket path.
func Dir(t testing.TB) string {
	t.Helper()
	base := os.TempDir()
	if len(base) > maxBaseLen {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "nd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
