package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/testdir"
)

// TestConfigShowRefusesAKeyItCannotShow: config show refuses a name that is
// no key, and a key a table could hold that the configuration does not set,
// on standard error and exit 2, with nothing on standard output for the keys
// it could show before it.
func TestConfigShowRefusesAKeyItCannotShow(t *testing.T) {
	dir := testdir.Short(t)
	cfg := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	sets := append([]string{"--config", cfg}, dryRunSets(t, base)...)
	for _, c := range []struct {
		key  string
		code int
		line string
	}{
		{"timezone", 0, ""},
		{"no.such.key", 2, "config_unknown_key: unknown configuration key for no.such.key\n"},
		{"ssh.transports.other", 2, "config_key_unset: ssh.transports.other is not set\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := Main(append(append([]string{}, sets...), "config", "show", "basedir", c.key), strings.NewReader(""), &stdout, &stderr)
		if code != c.code || stderr.String() != c.line || (code == 0) != strings.HasPrefix(stdout.String(), "key:        basedir\n") {
			t.Errorf("%s: exit %d stdout=%q stderr=%q", c.key, code, stdout.String(), stderr.String())
		}
	}
}
