package configload

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestSpoolKeys: the spool keys at the loader, registry 21: the keys load
// with their defaults, freecheck takes
// display.color's three words and no other, the threshold's range is 0 to
// 1 GiB with 0 inside it, and the removed key's name stays refused.
func TestSpoolKeys(t *testing.T) {
	snap, err := Load(Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if snap.String("spooldir") != "auto" || snap.String("freecheck") != "auto" || snap.Int64("output.spool-threshold-bytes") != 1048576 {
		t.Fatalf("defaults: %q %q %d", snap.String("spooldir"), snap.String("freecheck"), snap.Int64("output.spool-threshold-bytes"))
	}
	for _, set := range []string{`freecheck="auto"`, `freecheck="never"`, `output.spool-threshold-bytes=0`, `output.spool-threshold-bytes=1073741824`, `spooldir="/var/tmp/spool"`} {
		if _, err := Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{set}}); err != nil {
			t.Errorf("%s: %v", set, err)
		}
	}
	for set, code := range map[string]string{
		`freecheck="enabled"`:                     "config_enum_value_invalid",
		`freecheck=true`:                          "config_enum_value_invalid",
		`output.spool-threshold-bytes=-1`:         "config_value_out_of_range",
		`output.spool-threshold-bytes=1073741825`: "config_value_out_of_range",
		`output.memory-spool-threshold-bytes=1`:   "config_key_removed",
	} {
		if _, err := Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{set}}); errorcodes.Of(err) != code {
			t.Errorf("%s: %v, want %s", set, err, code)
		}
	}
	snap, err = Load(Options{InternalOnly: true, Environment: []string{"KARVI__FREECHECK=never", "KARVI__OUTPUT__SPOOL_THRESHOLD_BYTES=0", "KARVI__SPOOLDIR=/tmp/x"}})
	if err != nil || snap.String("freecheck") != "never" || snap.Int64("output.spool-threshold-bytes") != 0 || snap.String("spooldir") != "/tmp/x" {
		t.Fatalf("from the environment: %v %q %d", err, snap.String("freecheck"), snap.Int64("output.spool-threshold-bytes"))
	}
}
