package configload

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/configload/tomlmini"
)

// TestRegistryRangesWellFormed: every row's Min and Max parse in the row's
// kind, Min does not exceed Max, a bound is only on a numeric or duration
// row, ZeroDisables only beside a bound, and the built-in default is
// inside the range (or the 0 that disables).
func TestRegistryRangesWellFormed(t *testing.T) {
	bounded := 0
	for _, e := range configschema.Entries() {
		min, max, hasMin, hasMax, err := rangeOf(e)
		if err != nil {
			t.Errorf("%s: %v", e.Path, err)
			continue
		}
		if !hasMin && !hasMax {
			if e.ZeroDisables {
				t.Errorf("%s: ZeroDisables without a range", e.Path)
			}
			continue
		}
		bounded++
		switch e.Kind {
		case configschema.Duration, configschema.Integer, configschema.Number:
		default:
			t.Errorf("%s: a range on a %s row", e.Path, e.Kind)
		}
		if hasMin && hasMax && min.value > max.value {
			t.Errorf("%s: min %s above max %s", e.Path, e.Min, e.Max)
		}
		def, err := tomlmini.ParseValue(e.DefaultLiteral)
		if err != nil {
			t.Fatalf("%s: default %q: %v", e.Path, e.DefaultLiteral, err)
		}
		v := literalValue(t, e.Kind, def)
		if e.ZeroDisables && v == 0 {
			continue
		}
		if !inRange(v, min, max, hasMin, hasMax) {
			t.Errorf("%s: default %s outside %s", e.Path, e.DefaultLiteral, describeRange(e, min, max, hasMin, hasMax))
		}
	}
	if bounded < 50 {
		t.Errorf("only %d bounded rows; the review filled more", bounded)
	}
}

func literalValue(t *testing.T, kind configschema.Kind, v any) float64 {
	t.Helper()
	switch x := v.(type) {
	case string:
		d, err := time.ParseDuration(x)
		if err != nil {
			t.Fatalf("duration %q: %v", x, err)
		}
		return float64(d)
	case int64:
		return float64(x)
	case float64:
		return x
	}
	t.Fatalf("unexpected literal %T for %s", v, kind)
	return 0
}

// TestRangesEnforced: for every bounded row, the bound itself loads (or is
// refused when exclusive), one step past either bound is
// config_value_out_of_range naming the key, and 0 loads only where the row
// says it disables.
func TestRangesEnforced(t *testing.T) {
	load := func(t *testing.T, set string) error {
		t.Helper()
		_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{set}})
		return err
	}
	literal := func(e configschema.Entry, v float64) string {
		switch e.Kind {
		case configschema.Duration:
			return strconv.Quote(time.Duration(v).String())
		case configschema.Integer:
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	step := func(e configschema.Entry) float64 {
		switch e.Kind {
		case configschema.Duration:
			return float64(time.Nanosecond)
		case configschema.Integer:
			return 1
		}
		return 0.001
	}
	refused := func(t *testing.T, e configschema.Entry, set string) {
		t.Helper()
		err := load(t, e.Path+"="+set)
		var ce *Error
		if !errors.As(err, &ce) || ce.Code != "config_value_out_of_range" || ce.Key != e.Path {
			t.Errorf("%s=%s: err=%v, want config_value_out_of_range at the key", e.Path, set, err)
		}
	}
	accepted := func(t *testing.T, e configschema.Entry, set string) {
		t.Helper()
		if err := load(t, e.Path+"="+set); err != nil {
			t.Errorf("%s=%s: refused: %v", e.Path, set, err)
		}
	}
	for _, e := range configschema.Entries() {
		min, max, hasMin, hasMax, err := rangeOf(e)
		if err != nil || (!hasMin && !hasMax) {
			continue
		}
		// Rows a relational rule also governs are checked at their bound
		// only where that rule allows; the relational rules have their own
		// tests.
		relational := map[string]bool{"dispatch.wave-max-width": true, "dispatch.wave-start-width": true, "watch.stale-after": true, "watch.refresh": true, "output.max-job-bytes": true, "output.max-command-bytes": true, "dispatch.admission-poll-min": true, "dispatch.admission-poll-max": true}[e.Path]
		t.Run(e.Path, func(t *testing.T) {
			if hasMin {
				if min.exclusive {
					refused(t, e, literal(e, min.value))
				} else if !relational {
					accepted(t, e, literal(e, min.value))
				}
				if min.value-step(e) >= 0 || e.Kind != configschema.Duration {
					refused(t, e, literal(e, min.value-step(e)))
				}
				if min.value > 0 && !e.ZeroDisables && !min.exclusive {
					refused(t, e, literal(e, 0))
				}
				if e.ZeroDisables {
					accepted(t, e, literal(e, 0))
				}
			}
			if hasMax {
				if max.exclusive {
					refused(t, e, literal(e, max.value))
				} else if !relational {
					accepted(t, e, literal(e, max.value))
				}
				refused(t, e, literal(e, max.value+step(e)))
			}
		})
	}
	// The message names the range in the row's words.
	err := load(t, `ssh.server-alive-interval="1ms"`)
	if err == nil || err.Error() != fmt.Sprintf("config_value_out_of_range: must be 0 (disables) or 1s..10m for ssh.server-alive-interval at --set[1]") {
		t.Errorf("message: %v", err)
	}
	if err := load(t, `dispatch.admission-poll-min="900ms"`); err == nil || !errors.Is(err, err) || errorCode(err) != "config_dispatch_admission_poll_min_exceeds_max" {
		t.Errorf("poll min above max: %v", err)
	}
}

func errorCode(err error) string {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}
