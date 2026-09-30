package configload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlatformResolutionValidation: the platform names configuration
// supplies are checked against the known set at load, whatever on-unknown
// says; the enum is the registry's; names normalise, so an alias in upper
// case and "Generic" are accepted; the defaults are unset, fail, generic.
func TestPlatformResolutionValidation(t *testing.T) {
	load := func(t *testing.T, body string) (Snapshot, error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "karvi.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, ExplicitRoots: []string{path}})
	}
	source := "[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \"/dev/null\"\n"
	for _, tc := range []struct {
		name, body, code, key, text string
	}{
		{"default unknown", "[platform-resolution]\ndefault = \"cisco_iosx\"\n", "config_platform_resolution_unknown", "platform-resolution.default", "\"cisco_iosx\" is not a known platform (known: generic, cisco_iosxe"},
		{"fallback unknown under fail", "[platform-resolution]\nunknown-fallback = \"cisco_iosx\"\n", "config_platform_resolution_unknown", "platform-resolution.unknown-fallback", "\"cisco_iosx\" is not a known platform"},
		{"fallback unknown under warn", "[platform-resolution]\non-unknown = \"warn\"\nunknown-fallback = \"cisco_iosx\"\n", "config_platform_resolution_unknown", "platform-resolution.unknown-fallback", "\"cisco_iosx\" is not a known platform"},
		{"fallback blank", "[platform-resolution]\nunknown-fallback = \"\"\n", "config_platform_resolution_unknown", "platform-resolution.unknown-fallback", "\"\" is not a known platform"},
		{"on-unknown not in the enum", "[platform-resolution]\non-unknown = \"maybe\"\n", "config_enum_value_invalid", "platform-resolution.on-unknown", "must be one of fail, warn"},
		{"source default unknown", source + "defaults.platform = \"cisco_iosx\"\n", "config_inventory_source_platform_unknown", "inventory-source.0.defaults.platform", "inventory source lab: defaults.platform \"cisco_iosx\" is not a known platform"},
		{"source default unknown under warn", "[platform-resolution]\non-unknown = \"warn\"\n" + source + "defaults.platform = \"cisco_iosx\"\n", "config_inventory_source_platform_unknown", "inventory-source.0.defaults.platform", "\"cisco_iosx\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.body)
			var ce *Error
			if !errors.As(err, &ce) || ce.Code != tc.code || ce.Key != tc.key {
				t.Fatalf("err=%v, want %s at %s", err, tc.code, tc.key)
			}
			if !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("message %q lacks %q", err.Error(), tc.text)
			}
			if ce.Source.Path == "" {
				t.Fatalf("no source: %+v", ce)
			}
		})
	}
	t.Run("defaults", func(t *testing.T) {
		snap, err := load(t, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := snap.String("platform-resolution.default") + "|" + snap.String("platform-resolution.on-unknown") + "|" + snap.String("platform-resolution.unknown-fallback"); got != "cisco_iosxe|fail|generic" {
			t.Fatalf("defaults %q", got)
		}
	})
	t.Run("names normalise", func(t *testing.T) {
		snap, err := load(t, "[platform.c9300]\ndriver = \"cisco_iosxe\"\n[platform-resolution]\ndefault = \"C9300\"\non-unknown = \"warn\"\nunknown-fallback = \"Generic\"\n"+source+"defaults.platform = \" Cisco_IOSXE \"\n")
		if err != nil {
			t.Fatal(err)
		}
		if got := snap.String("platform-resolution.default"); got != "C9300" {
			t.Fatalf("default kept as written, got %q", got)
		}
	})
}
