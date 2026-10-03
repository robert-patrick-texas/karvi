package cli

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// TestDispatchOptionsAreKeyOverrides: run's Dispatch options reach the
// configuration as overrides of their keys in the lock-aware cli layer, each
// with its key's type, --dp/--dw/--ds as --dispatch; an option not given
// writes nothing, so the key's configured value stands.
func TestDispatchOptionsAreKeyOverrides(t *testing.T) {
	inv, err := Parse([]string{"run", "--target", "r1", "--dw", "--workers", "3", "--start-width", "9", "--max-width", "11",
		"--halt-on-error-count", "1", "--halt-on-error-percent", "2", "--wave-gate-error-count", "3",
		"--wave-gate-error-percent", "4", "--wave-delay", "1m30s", "--cmd", "show clock"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"dispatch.default": "wave", "dispatch.parallel-workers": int64(3), "dispatch.wave-start-width": int64(9),
		"dispatch.wave-max-width": int64(11), "dispatch.halt-on-error-count": int64(1), "dispatch.halt-on-error-percent": int64(2),
		"dispatch.wave-gate-error-count": int64(3), "dispatch.wave-gate-error-percent": int64(4), "dispatch.wave-gate-timed-delay": "1m30s",
	}
	got := inv.common().ConfigFlags
	for key, v := range want {
		if !reflect.DeepEqual(got[key].Value, v) {
			t.Errorf("%s = %#v, want %#v", key, got[key].Value, v)
		}
	}
	// Each is sourced to its option by its long name; --dw is --dispatch.
	for _, d := range dispatchOptionKeys {
		if got[d.key].Option != "--"+d.opt.name {
			t.Errorf("%s from %q, want --%s", d.key, got[d.key].Option, d.opt.name)
		}
	}
	if got["dispatch.default"].Option != "--dispatch" {
		t.Errorf("--dw is sourced to %q", got["dispatch.default"].Option)
	}
	if len(dispatchOptionKeys) != len(want) {
		t.Errorf("%d dispatch options mapped, the test names %d", len(dispatchOptionKeys), len(want))
	}
	inv, err = Parse([]string{"run", "--target", "r1", "--cmd", "show clock"})
	if err != nil {
		t.Fatal(err)
	}
	for key := range want {
		if v, ok := inv.common().ConfigFlags[key]; ok {
			t.Errorf("%s = %#v without its option", key, v)
		}
	}
}

// TestDispatchOptionOutOfRange: a value out of its key's range is the key's
// own error at the configuration's exit, as from --set, where the command
// line had checked the ranges itself (dispatch_value_negative,
// dispatch_percent_out_of_range, exit 4).
func TestDispatchOptionOutOfRange(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	for _, args := range [][]string{
		{"run", "--target", "r1", "--halt-on-error-percent", "101", "show", "clock"},
		{"run", "--target", "r1", "--workers", "-1", "show", "clock"},
		{"run", "--target", "r1", "--wave-delay", "2h", "show", "clock"},
	} {
		var stdout, stderr bytes.Buffer
		got := Main(args, strings.NewReader(""), &stdout, &stderr)
		if got != exitcode.ExitConfigValidationError || !strings.HasPrefix(stderr.String(), "config_value_out_of_range: ") {
			t.Errorf("%v: exit=%d stderr=%q, want %d config_value_out_of_range", args, got, stderr.String(), exitcode.ExitConfigValidationError)
		}
	}
}

// TestOptionSources: the options that set a key outside the Dispatch block
// are sourced to themselves: the global options, --continue-device-on-error,
// and the crun word, which continues past a device error by itself.
func TestOptionSources(t *testing.T) {
	inv, err := Parse([]string{"--ipv4", "--timezone", "UTC", "--ansi", "strip", "run", "--target", "r1", "--continue-device-on-error", "--cmd", "x"})
	if err != nil {
		t.Fatal(err)
	}
	flags := inv.common().ConfigFlags
	continueOptions(inv, "run", flags)
	for key, source := range map[string]string{"name.address-family-preference": "--ipv4", "timezone": "--timezone", "output.ansi": "--ansi", "execution.halt-device-on-command-error": "--continue-device-on-error"} {
		if flags[key].Option != source {
			t.Errorf("%s from %q, want %s", key, flags[key].Option, source)
		}
	}
	inv, err = Parse([]string{"crun", "--all"})
	if err != nil {
		t.Fatal(err)
	}
	flags = inv.common().ConfigFlags
	continueOptions(inv, "crun", flags)
	if f := flags["execution.halt-device-on-command-error"]; f.Option != "crun" || f.Value != false {
		t.Errorf("crun: %+v", f)
	}
	inv, err = Parse([]string{"run", "--target", "r1", "--cmd", "x"})
	if err != nil {
		t.Fatal(err)
	}
	flags = inv.common().ConfigFlags
	continueOptions(inv, "run", flags)
	if f, ok := flags["execution.halt-device-on-command-error"]; ok {
		t.Errorf("a run without the option: %+v", f)
	}
}
