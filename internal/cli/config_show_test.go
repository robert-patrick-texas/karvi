package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/testdir"
)

// configShowSets are the global options of a config show under a
// configuration of the test: an empty file and the dry-run sets.
func configShowSets(t *testing.T) []string {
	t.Helper()
	dir := testdir.Short(t)
	cfg := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	return append([]string{"--config", cfg}, dryRunSets(t, base)...)
}

// TestConfigShowRefusesAKeyItCannotShow: config show refuses a name that is
// no key, and a key a table could hold that the configuration does not set,
// on standard error and exit 2, with nothing on standard output for the keys
// it could show before it.
func TestConfigShowRefusesAKeyItCannotShow(t *testing.T) {
	sets := configShowSets(t)
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

// TestConfigShowFormatBesideKeys: --format encodes the full view alone;
// beside keys or --explain it is refused at exit 4 before the configuration
// loads (a --set naming no key is not reached), nothing on standard output.
// The full view's JSON is the values object.
func TestConfigShowFormatBesideKeys(t *testing.T) {
	sets := configShowSets(t)
	main := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Main(append(append([]string{}, sets...), args...), strings.NewReader(""), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	for _, c := range []struct {
		args []string
		line string
	}{
		{[]string{"--format", "json", "display.color"}, "config_show_format_conflict: --format json beside display.color\n"},
		{[]string{"--format", "toml", "--explain"}, "config_show_format_conflict: --format toml beside --explain\n"},
		{[]string{"--explain", "--format", "json", "basedir"}, "config_show_format_conflict: --format json beside --explain\n"},
	} {
		code, stdout, stderr := main(append([]string{"--set", "no.such.key=1", "config", "show"}, c.args...)...)
		if code != 4 || stdout != "" || stderr != c.line {
			t.Errorf("%q: exit %d stdout=%q stderr=%q", c.args, code, stdout, stderr)
		}
	}
	code, stdout, stderr := main("config", "show", "--format", "json")
	var view struct {
		Values map[string]any `json:"values"`
	}
	if err := json.Unmarshal([]byte(stdout), &view); code != 0 || err != nil || view.Values["display.color"] != "never" {
		t.Fatalf("the full view: exit %d %v stderr=%q", code, err, stderr)
	}
}
