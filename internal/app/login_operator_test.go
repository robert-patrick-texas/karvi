package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/askpass"
)

// TestALoginTellsTheHelperItServesALogin: a login's OpenSSH runs with the
// askpass helper told it serves a login (askpass.LoginEnv), which the
// broker sets only where the login hands it the terminal's operator, so a
// device's prompt the broker does not answer goes to the operator. The fake
// ssh succeeds only when told; nothing reaches a terminal.
func TestALoginTellsTheHelperItServesALogin(t *testing.T) {
	helper := filepath.Join(t.TempDir(), "karvi-askpass")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KARVI_ASKPASS_PATH", helper)
	script := `[ "$` + askpass.LoginEnv + `" = 1 ] || { echo "` + askpass.LoginEnv + ` is '$` + askpass.LoginEnv + `'" >&2; exit 1; }` + "\n"
	result, _, stderr := fakeLoginWith(t, script, &bytes.Buffer{}, filepath.Join(t.TempDir(), "audit.jsonl"), false)
	if result.ExitCode != 0 || result.Error != "" {
		t.Errorf("exit=%d error=%q, want a login whose helper is told; stderr %q", result.ExitCode, result.Error, stderr)
	}
}
