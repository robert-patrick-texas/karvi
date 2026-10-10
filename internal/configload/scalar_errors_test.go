package configload

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestScalarFaultSaidOnce: a value of the wrong type or outside its key's
// enumerated values says its fault once, in the form of the other
// configuration errors, the key and the source after it, where the text
// named the key twice and repeated itself as the cause.
func TestScalarFaultSaidOnce(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"[dispatch]\nparallel-workers = \"four\"\n", "config_type_error: must be an integer for dispatch.parallel-workers at %s:2"},
		{"[dispatch]\norder = \"bogus\"\n", "config_enum_value_invalid: must be one of default, sorted, name, shuffle, random for dispatch.order at %s:2"},
		{"[ssh]\nhost-key-policy = 1\n", "config_type_error: must be a string for ssh.host-key-policy at %s:2"},
		{"[config]\nreject-unknown-env = \"no\"\n", "config_type_error: must be a boolean for config.reject-unknown-env at %s:2"},
		{"[ssh-algorithms]\nkex = \"curve25519-sha256\"\n", "config_type_error: must be an array for ssh-algorithms.kex at %s:2"},
		{"[ssh-algorithms]\nkex = [1]\n", "config_type_error: must be an array of strings for ssh-algorithms.kex at %s:2"},
	} {
		path := filepath.Join(t.TempDir(), "karvi.toml")
		if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, ExplicitRoots: []string{path}})
		if want := fmt.Sprintf(tc.want, path); err == nil || err.Error() != want {
			t.Errorf("%q:\n got %v\nwant %s", tc.body, err, want)
		}
	}
}
