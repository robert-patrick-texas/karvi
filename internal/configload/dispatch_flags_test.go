package configload

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestDispatchKeysFromTheCLILayer: run's Dispatch options are overrides of
// their keys in the cli layer, so a global lock refuses one, a value out of
// the key's range is the key's own error, and the start width above the
// ceiling is the cross-key check's, as from --set.
func TestDispatchKeysFromTheCLILayer(t *testing.T) {
	global := filepath.Join(t.TempDir(), "global.toml")
	saved := globalRoots
	globalRoots = []string{global}
	t.Cleanup(func() { globalRoots = saved })
	if err := os.WriteFile(global, []byte("[config-lock]\n\"dispatch.wave-max-width\" = true\n[dispatch]\nwave-max-width = 8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		flags map[string]any
		code  string
	}{
		{map[string]any{"dispatch.wave-max-width": int64(64)}, "config_lock_violation"},
		{map[string]any{"dispatch.wave-gate-timed-delay": "2h0m0s"}, "config_value_out_of_range"},
		{map[string]any{"dispatch.wave-gate-timed-delay": "-1s"}, "config_duration_negative"},
		{map[string]any{"dispatch.halt-on-error-percent": int64(101)}, "config_value_out_of_range"},
		{map[string]any{"dispatch.parallel-workers": int64(-1)}, "config_value_out_of_range"},
		{map[string]any{"dispatch.wave-start-width": int64(9)}, "config_dispatch_wave_max_below_start"},
		{map[string]any{"dispatch.wave-start-width": int64(8), "dispatch.halt-on-error-count": int64(0)}, ""},
	} {
		_, err := Load(Options{HomeDir: t.TempDir(), Environment: []string{}, FlagValues: cliFlags(tc.flags)})
		if got := errorcodes.Of(err); got != tc.code {
			t.Errorf("%v: %q (%v), want %q", tc.flags, got, err, tc.code)
		}
	}
}
