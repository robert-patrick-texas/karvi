package osutil

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoginUser: the login UID names an account only when it is set and is
// not the operator's own; an account the password database lacks is its
// number.
func TestLoginUser(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, content string
		uid           int
		want          string
	}{
		{"unset", "4294967295", 1000, ""},
		{"the operator's own", "1000\n", 1000, ""},
		{"another account", "0", 1000, "root"},
		{"no such account", "3999999999", 1000, "3999999999"},
		{"not a number", "x", 1000, ""},
	} {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := loginUserFrom(path, tc.uid); got != tc.want {
			t.Errorf("%s: loginUserFrom(%q) = %q, want %q", tc.name, tc.content, got, tc.want)
		}
	}
	if got := loginUserFrom(filepath.Join(dir, "absent"), 1000); got != "" {
		t.Errorf("an unreadable file named %q", got)
	}
	// This process's own login UID never names the process's own account.
	if got := LoginUser(0, os.Getuid()); got == currentName(t) {
		t.Errorf("LoginUser named the process's own account %q", got)
	}
}

func currentName(t *testing.T) string {
	t.Helper()
	o, err := CurrentOperator()
	if err != nil {
		t.Fatal(err)
	}
	return o.Username
}
