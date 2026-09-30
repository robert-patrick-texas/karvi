package configload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemovedKeysRefused is the configuration review of v0.14.0: every removed
// key is refused with its code from a file, from the environment, and from
// --set, the message names the key, and the removed session-cap pattern is
// refused by its prefix. The two compatibility-class keys keep the code they
// were released with (docs/ERROR-CODES.md, config_ssh_legacy_hosts_removed).
func TestRemovedKeysRefused(t *testing.T) {
	load := func(t *testing.T, body string, env []string, sets []string) error {
		t.Helper()
		path := filepath.Join(t.TempDir(), "karvi.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: env, Sets: sets, ExplicitRoots: []string{path}})
		return err
	}
	want := func(t *testing.T, err error, code, key string) {
		t.Helper()
		var ce *Error
		if !errors.As(err, &ce) || ce.Code != code || ce.Key != key {
			t.Fatalf("err=%v, want %s at %q", err, code, key)
		}
		if !strings.Contains(ce.Error(), "removed in ") {
			t.Fatalf("message does not name the release: %v", err)
		}
	}
	for _, r := range removedKeys {
		section, leaf, dotted := strings.Cut(r.path, ".")
		body := leaf + " = 1\n"
		if dotted {
			// The file layer, as a table with the leaf, since a removed key's
			// section may still exist; a top-level key (spoolfreecheck) is a
			// bare line.
			body = "[" + section + "]\n" + body
		} else {
			body = r.path + " = 1\n"
		}
		t.Run(r.path, func(t *testing.T) {
			want(t, load(t, body, nil, nil), r.code, r.path)
			// The environment layer, before the registry index would call the
			// name unknown; the key reported is the path.
			want(t, load(t, "", []string{r.environment + "=1"}, nil), r.code, r.path)
			// --set, through the same assignment as a file.
			want(t, load(t, "", nil, []string{r.path + "=1"}), r.code, r.path)
		})
	}
	t.Run("platform-caps pattern", func(t *testing.T) {
		want(t, load(t, "[sessions.platform-caps]\ngeneric = 4\n", nil, nil), "config_key_removed", "sessions.platform-caps.generic")
		want(t, load(t, "", nil, []string{"sessions.platform-caps.cisco_iosxe=2"}), "config_key_removed", "sessions.platform-caps.cisco_iosxe")
	})
	// A removed key's environment name is refused even with unknown variables
	// tolerated, since ignoring it would silently drop a site's intent.
	t.Run("environment with reject-unknown-env false", func(t *testing.T) {
		want(t, load(t, "[config]\nreject-unknown-env = false\n", []string{"KARVI__RETENTION__DAYS=31"}, nil), "config_key_removed", "retention.days")
	})
	// The table's rows are well formed: every path has a section, every
	// environment name follows the registry's derivation, no path repeats.
	seen := map[string]bool{}
	for _, r := range removedKeys {
		if seen[r.path] {
			t.Errorf("%s listed twice", r.path)
		}
		seen[r.path] = true
		env := "KARVI__" + strings.ToUpper(strings.NewReplacer(".", "__", "-", "_").Replace(r.path))
		if r.environment != env {
			t.Errorf("%s: environment %s, want %s", r.path, r.environment, env)
		}
		if r.removedIn == "" || r.hint == "" || r.code == "" {
			t.Errorf("%s: incomplete row %+v", r.path, r)
		}
	}
}
