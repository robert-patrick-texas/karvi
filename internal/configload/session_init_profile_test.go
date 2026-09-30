package configload

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestSessionInitProfileValidation: each profile rule refused at load with
// its code and key, and the valid
// forms (a named no-op, blank and tabbed commands, both on-error values, the
// timeout range ends) accepted.
func TestSessionInitProfileValidation(t *testing.T) {
	load := func(t *testing.T, body string) error {
		t.Helper()
		path := filepath.Join(t.TempDir(), "karvi.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, ExplicitRoots: []string{path}})
		return err
	}
	const catchAll = "[[session-init-map]]\nprofile = \"p\"\nname = \"*\"\n"
	for _, tc := range []struct {
		name, body, code, key string
	}{
		{"commands missing", "[session-init.p]\non-error = \"continue\"\n" + catchAll, "config_session_init_commands_missing", "session-init.p.commands"},
		{"commands a string", "[session-init.p]\ncommands = \"show clock\"\n" + catchAll, "config_type_error", "session-init.p.commands"},
		{"commands hold a number", "[session-init.p]\ncommands = [\"show clock\", 5]\n" + catchAll, "config_type_error", "session-init.p.commands"},
		{"command with a NUL", "[session-init.p]\ncommands = [\"show\\u0000clock\"]\n" + catchAll, "config_session_init_command_invalid", "session-init.p.commands"},
		{"on-error abort", "[session-init.p]\ncommands = []\non-error = \"abort\"\n" + catchAll, "config_enum_value_invalid", "session-init.p.on-error"},
		{"on-error not a string", "[session-init.p]\ncommands = []\non-error = true\n" + catchAll, "config_enum_value_invalid", "session-init.p.on-error"},
		{"timeout negative", "[session-init.p]\ncommands = []\ncommand-timeout = \"-5s\"\n" + catchAll, "config_duration_negative", "session-init.p.command-timeout"},
		{"timeout not a duration", "[session-init.p]\ncommands = []\ncommand-timeout = \"fast\"\n" + catchAll, "config_duration_error", "session-init.p.command-timeout"},
		{"timeout a number", "[session-init.p]\ncommands = []\ncommand-timeout = 5\n" + catchAll, "config_type_error", "session-init.p.command-timeout"},
		{"timeout zero", "[session-init.p]\ncommands = []\ncommand-timeout = \"0s\"\n" + catchAll, "config_session_init_command_timeout_out_of_range", "session-init.p.command-timeout"},
		{"timeout 500ms", "[session-init.p]\ncommands = []\ncommand-timeout = \"500ms\"\n" + catchAll, "config_session_init_command_timeout_out_of_range", "session-init.p.command-timeout"},
		{"timeout 13h", "[session-init.p]\ncommands = []\ncommand-timeout = \"13h\"\n" + catchAll, "config_session_init_command_timeout_out_of_range", "session-init.p.command-timeout"},
		{"profile named none", "[session-init.none]\ncommands = []\n", "config_session_init_profile_name_reserved", "session-init.none"},
		{"map rule without profile", "[session-init.p]\ncommands = []\n[[session-init-map]]\nplatform = \"cisco_iosxe\"\n" + catchAll, "config_session_init_map_profile_missing", "session-init-map.0.profile"},
		{"map rule profile not a string", "[session-init.p]\ncommands = []\n[[session-init-map]]\nprofile = 1\nplatform = \"cisco_iosxe\"\n" + catchAll, "config_type_error", "session-init-map.0.profile"},
		{"map rule unknown profile", "[session-init.p]\ncommands = []\n[[session-init-map]]\nprofile = \"q\"\nplatform = \"cisco_iosxe\"\n" + catchAll, "config_session_init_map_profile_unknown", "session-init-map.0.profile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := load(t, tc.body)
			var ce *Error
			if !errors.As(err, &ce) || ce.Code != tc.code || ce.Key != tc.key {
				t.Fatalf("err=%v, want %s at %s", err, tc.code, tc.key)
			}
			if ce.Source.Path == "" {
				t.Fatalf("no source: %+v", ce)
			}
		})
	}
	for _, body := range []string{
		"[session-init.p]\ncommands = []\n" + catchAll,
		"[session-init.p]\ncommands = [\"\", \"   \", \"show\\tclock\"]\non-error = \"continue\"\ncommand-timeout = \"1s\"\n" + catchAll,
		"[session-init.p]\ncommands = [\"terminal width 511\"]\non-error = \"fail-device\"\ncommand-timeout = \"12h\"\n" + catchAll,
		"[session-init.p]\ncommands = []\n[session-init.\"a b\"]\ncommands = []\n",
	} {
		if err := load(t, body); err != nil {
			t.Fatalf("valid profile refused: %v\n%s", err, body)
		}
	}
}
