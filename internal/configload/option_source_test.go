package configload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
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

// TestGatherAndPrefixOptionsLock: --gather and --prefix write their key in
// the flags layer, sourced to the option, true or (by the =false form)
// false; on run, crun, a stream's lines, and job follow the display.run
// key, on command the display.command key (internal/cli,
// TestGatherAndPrefixSetTheirKeys, TestJobFollowGatherAndPrefixSetTheirKeys).
// A site that locks the key refuses either form, as it refuses --order
// default (config_lock_violation, exit 3, at the option); --set, applied
// after the layer, beats the option where nothing locks.
func TestGatherAndPrefixOptionsLock(t *testing.T) {
	saved := globalRoots
	t.Cleanup(func() { globalRoots = saved })
	load := func(flags map[string]FlagValue, sets ...string) (Snapshot, error) {
		return Load(Options{HomeDir: t.TempDir(), Environment: []string{}, FlagValues: flags, Sets: sets})
	}
	for _, tc := range []struct{ key, option string }{
		{"display.run.gather", "--gather"},
		{"display.run.prefix", "--prefix"},
		{"display.command.gather", "--gather"},
		{"display.command.prefix", "--prefix"},
	} {
		globalRoots = []string{filepath.Join(t.TempDir(), "absent.toml")}
		for _, value := range []bool{true, false} {
			flags := map[string]FlagValue{tc.key: {Value: value, Option: tc.option}}
			snap, err := load(flags)
			if err != nil || snap.Bool(tc.key) != value || snap.Values[tc.key].Source != (SourceRef{Layer: "cli", Path: tc.option}) {
				t.Fatalf("%s %v: %v %+v %v", tc.key, value, snap.Bool(tc.key), snap.Values[tc.key].Source, err)
			}
			snap, err = load(flags, fmt.Sprintf("%s=%t", tc.key, !value))
			if err != nil || snap.Bool(tc.key) == value || snap.Values[tc.key].Source != (SourceRef{Layer: "set", Path: "--set[1]"}) {
				t.Fatalf("%s %v under --set: %v %+v %v", tc.key, value, snap.Bool(tc.key), snap.Values[tc.key].Source, err)
			}
		}
		global := filepath.Join(t.TempDir(), "global.toml")
		globalRoots = []string{global}
		if err := os.WriteFile(global, []byte(fmt.Sprintf("[config-lock]\n%q = true\n", tc.key)), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, value := range []bool{true, false} {
			_, err := load(map[string]FlagValue{tc.key: {Value: value, Option: tc.option}})
			if errorcodes.Of(err) != "config_lock_violation" || !strings.HasSuffix(err.Error(), "for "+tc.key+" at "+tc.option) || errorcodes.ExitAt(err, "config_load_failed") != exitcode.ExitConfigLockViolation {
				t.Errorf("%s %v locked: %v", tc.key, value, err)
			}
		}
		if _, err := load(nil); err != nil {
			t.Errorf("%s locked, no option: %v", tc.key, err)
		}
	}
}
