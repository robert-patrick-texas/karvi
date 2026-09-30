package configload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMapSelectorPatternsValidated: a malformed selector in either map is
// refused when the configuration
// loads, naming the rule and the value, rather than never matching.
func TestMapSelectorPatternsValidated(t *testing.T) {
	load := func(t *testing.T, body string) error {
		t.Helper()
		path := filepath.Join(t.TempDir(), "karvi.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, ExplicitRoots: []string{path}})
		return err
	}
	credential := func(key, value string) string {
		return "[[credential-policy-map]]\npolicy = \"default\"\n" + key + " = " + value + "\n[[credential-policy-map]]\npolicy = \"default\"\nname = \"*\"\n"
	}
	session := func(key, value string) string {
		return "[session-init.p]\ncommands = []\n[[session-init-map]]\nprofile = \"p\"\n" + key + " = " + value + "\n[[session-init-map]]\nprofile = \"p\"\nname = \"*\"\n"
	}
	for _, tc := range []struct {
		name, body, key, value string
	}{
		{"credential site class not closed", credential("site", `"ny[c"`), "credential-policy-map.0", "ny[c"},
		{"credential name caret", credential("name", `"^sw"`), "credential-policy-map.0", "^sw"},
		{"credential group bare negation", credential("device-group", `["core", "!"]`), "credential-policy-map.0", "!"},
		{"credential platform trailing backslash", credential("platform", `'cisco\'`), "credential-policy-map.0", `cisco\`},
		{"session-init site dollar", session("site", `"nyc$"`), "session-init-map.0", "nyc$"},
		{"session-init name empty class", session("name", `"sw[!]"`), "session-init-map.0", "sw[!]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := load(t, tc.body)
			var ce *Error
			if !errors.As(err, &ce) || ce.Code != "config_match_rule_pattern_invalid" || ce.Key != tc.key || !strings.Contains(ce.Message, `"`+strings.ReplaceAll(tc.value, `\`, `\\`)+`"`) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	for _, body := range []string{
		credential("site", `["nyc*", "!nyc/lab*", '\!odd', "[!b]os"]`),
		session("name", `'sw\^1'`),
	} {
		if err := load(t, body); err != nil {
			t.Fatalf("valid selectors refused: %v", err)
		}
	}
}
