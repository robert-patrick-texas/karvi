package configload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestOptionSource: a value an option sets is sourced to the option, in
// the snapshot (config show) and in a range error and a lock's refusal; an
// unknown zone is the timezone key's, at the option that set it.
func TestOptionSource(t *testing.T) {
	load := func(flags map[string]FlagValue) (Snapshot, error) {
		return Load(Options{HomeDir: t.TempDir(), Environment: []string{}, FlagValues: flags})
	}
	saved := globalRoots
	globalRoots = []string{filepath.Join(t.TempDir(), "absent.toml")}
	t.Cleanup(func() { globalRoots = saved })

	snap, err := load(map[string]FlagValue{"execution.blind-wait": {Value: "9s", Option: "--blind-wait"}})
	if err != nil || snap.Values["execution.blind-wait"].Source != (SourceRef{Layer: "cli", Path: "--blind-wait"}) {
		t.Fatalf("source %+v, %v", snap.Values["execution.blind-wait"].Source, err)
	}
	_, err = load(map[string]FlagValue{"execution.blind-wait": {Value: "20m0s", Option: "--blind-wait"}})
	if errorcodes.Of(err) != "config_value_out_of_range" || !strings.HasSuffix(err.Error(), "for execution.blind-wait at --blind-wait") {
		t.Errorf("range: %v", err)
	}
	_, err = load(map[string]FlagValue{"timezone": {Value: "Mars/Olympus", Option: "--timezone"}})
	if errorcodes.Of(err) != "display_timezone_invalid" || !strings.HasSuffix(err.Error(), "for timezone at --timezone") {
		t.Errorf("zone: %v", err)
	}
	global := filepath.Join(t.TempDir(), "global.toml")
	globalRoots = []string{global}
	if err := os.WriteFile(global, []byte("[config-lock]\n\"execution.halt-device-on-command-error\" = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = load(map[string]FlagValue{"execution.halt-device-on-command-error": {Value: false, Option: "--continue-device-on-error"}})
	if errorcodes.Of(err) != "config_lock_violation" || !strings.HasSuffix(err.Error(), "at --continue-device-on-error") {
		t.Errorf("lock: %v", err)
	}
}

// TestNoCredhLock is EXAMPLES chapter 56, issue 9: --no-credh is
// creds.helper-enabled = false in the flags layer, sourced to the option,
// and a site that locks the key refuses it.
func TestNoCredhLock(t *testing.T) {
	saved := globalRoots
	t.Cleanup(func() { globalRoots = saved })
	globalRoots = []string{filepath.Join(t.TempDir(), "absent.toml")}
	flags := map[string]FlagValue{"creds.helper-enabled": {Value: false, Option: "--no-credh"}}
	load := func() (Snapshot, error) {
		return Load(Options{HomeDir: t.TempDir(), Environment: []string{}, FlagValues: flags})
	}
	snap, err := load()
	if err != nil || snap.Bool("creds.helper-enabled") || snap.Values["creds.helper-enabled"].Source != (SourceRef{Layer: "cli", Path: "--no-credh"}) {
		t.Fatalf("%v %+v %v", snap.Bool("creds.helper-enabled"), snap.Values["creds.helper-enabled"].Source, err)
	}
	global := filepath.Join(t.TempDir(), "global.toml")
	globalRoots = []string{global}
	if err := os.WriteFile(global, []byte("[config-lock]\n\"creds.helper-enabled\" = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = load(); errorcodes.Of(err) != "config_lock_violation" || !strings.HasSuffix(err.Error(), "at --no-credh") {
		t.Errorf("lock: %v", err)
	}
}

// TestNoColorLock is EXAMPLES chapter 56, issue 13: --nocolor is
// display.theme = "nocolor" in the flags layer, sourced to the option, and
// a site that locks the key refuses it.
func TestNoColorLock(t *testing.T) {
	saved := globalRoots
	t.Cleanup(func() { globalRoots = saved })
	globalRoots = []string{filepath.Join(t.TempDir(), "absent.toml")}
	flags := map[string]FlagValue{"display.theme": {Value: "nocolor", Option: "--nocolor"}}
	load := func() (Snapshot, error) {
		return Load(Options{HomeDir: t.TempDir(), Environment: []string{}, FlagValues: flags})
	}
	snap, err := load()
	if err != nil || snap.String("display.theme") != "nocolor" || snap.Values["display.theme"].Source != (SourceRef{Layer: "cli", Path: "--nocolor"}) {
		t.Fatalf("%q %+v %v", snap.String("display.theme"), snap.Values["display.theme"].Source, err)
	}
	global := filepath.Join(t.TempDir(), "global.toml")
	globalRoots = []string{global}
	if err := os.WriteFile(global, []byte("[config-lock]\n\"display.theme\" = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = load(); errorcodes.Of(err) != "config_lock_violation" || !strings.HasSuffix(err.Error(), "at --nocolor") {
		t.Errorf("lock: %v", err)
	}
}
