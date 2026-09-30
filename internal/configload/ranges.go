package configload

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/configschema"
)

// Documented ranges, enforced.
//
// A registry row that states a range carries it as Min and Max literals in
// the row's own syntax, with ZeroDisables for the rows whose 0 means off
// (configschema.Entry). validateRanges is the one rule that applies them:
// every integer, number, and duration row with a bound is checked here, and
// a value outside is config_value_out_of_range with a message naming the
// range. Rules that relate two keys (a maximum below a minimum) stay
// hand-written in validate.go and run after this one, so their operands
// are already in range. Part of the configuration review of v0.14.0.

// bound is one parsed end of a range.
type bound struct {
	value     float64 // nanoseconds for a duration, else the number
	exclusive bool
	literal   string // as the row wrote it, for the message
}

// parseBound reads a Min or Max literal for a row of the given kind. An
// empty literal is no bound.
func parseBound(kind configschema.Kind, literal string) (bound, bool, error) {
	if literal == "" {
		return bound{}, false, nil
	}
	b := bound{literal: literal}
	if strings.HasPrefix(literal, ">") || strings.HasPrefix(literal, "<") {
		b.exclusive = true
		literal = literal[1:]
	}
	switch kind {
	case configschema.Duration:
		d, err := time.ParseDuration(literal)
		if err != nil {
			return bound{}, false, err
		}
		b.value = float64(d)
	case configschema.Integer:
		n, err := strconv.ParseInt(literal, 10, 64)
		if err != nil {
			return bound{}, false, err
		}
		b.value = float64(n)
	default:
		f, err := strconv.ParseFloat(literal, 64)
		if err != nil {
			return bound{}, false, err
		}
		b.value = f
	}
	return b, true, nil
}

// rangeOf parses a row's bounds; ok is false for a row without any.
func rangeOf(e configschema.Entry) (min, max bound, hasMin, hasMax bool, err error) {
	if min, hasMin, err = parseBound(e.Kind, e.Min); err != nil {
		return
	}
	max, hasMax, err = parseBound(e.Kind, e.Max)
	return
}

// describeRange words a row's range for the refusal: "1s..5m",
// ">0..<100", "at least 0", "0 (disables) or 1m..720h".
func describeRange(e configschema.Entry, min, max bound, hasMin, hasMax bool) string {
	var s string
	switch {
	case hasMin && hasMax:
		s = min.literal + ".." + max.literal
	case hasMin:
		s = "at least " + min.literal
	default:
		s = "at most " + max.literal
	}
	if e.ZeroDisables {
		s = "0 (disables) or " + s
	}
	return s
}

// inRange reports whether v satisfies the bounds.
func inRange(v float64, min, max bound, hasMin, hasMax bool) bool {
	if hasMin && (v < min.value || (min.exclusive && v == min.value)) {
		return false
	}
	if hasMax && (v > max.value || (max.exclusive && v == max.value)) {
		return false
	}
	return true
}

// validateRanges applies every row's range to the effective value. It runs
// after the scalar type check, so a duration here parses.
func (l *loader) validateRanges() error {
	for _, e := range configschema.Entries() {
		min, max, hasMin, hasMax, err := rangeOf(e)
		if err != nil {
			return fmt.Errorf("registry row %s: bad range literal: %w", e.Path, err)
		}
		if !hasMin && !hasMax {
			continue
		}
		var v float64
		switch e.Kind {
		case configschema.Duration:
			v = float64(l.snap.Duration(e.Path))
		case configschema.Integer:
			v = float64(l.snap.Int64(e.Path))
		default:
			v = l.snap.Float(e.Path)
		}
		if e.ZeroDisables && v == 0 {
			continue
		}
		if !inRange(v, min, max, hasMin, hasMax) {
			return l.semantic("config_value_out_of_range", e.Path, "must be "+describeRange(e, min, max, hasMin, hasMax))
		}
	}
	return nil
}
