package configload

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"github.com/robert-patrick-texas/karvi/configschema"
)

// ValueMap is every value of the snapshot by key, each a copy: the
// configuration a plan carries (executionplan.Configuration), the object
// `config show --format json` prints as values.
func (s Snapshot) ValueMap() map[string]any {
	out := make(map[string]any, len(s.Values))
	for k, v := range s.Values {
		out[k] = clone(v.Data)
	}
	return out
}

// FromValues builds the configuration a plan's block carries: the values
// alone, with no sources, warnings, or locks, an integer key's value an
// int64, and the digest computed as Load computes it. A number key keeps
// the block's type, an int64 for an integral value, which its readers
// (Snapshot.Float) take as the load's float64. A key the registry does not
// know is refused.
func FromValues(values map[string]any) (Snapshot, error) {
	snap := Snapshot{Values: make(map[string]Value, len(values)), LoadedAt: time.Now()}
	for k, v := range values {
		if !configschema.IsKnownLeaf(k) {
			return Snapshot{}, fmt.Errorf("unknown key %q", k)
		}
		v = clone(v)
		if e, ok := configschema.Lookup(k); ok {
			n, err := kindNumber(e.Kind, v)
			if err != nil {
				return Snapshot{}, fmt.Errorf("%s: %w", k, err)
			}
			v = n
		}
		snap.Values[k] = Value{Data: v}
	}
	raw, err := snap.CanonicalJSON()
	if err != nil {
		return Snapshot{}, err
	}
	sum := sha256.Sum256(raw)
	snap.Digest = hex.EncodeToString(sum[:])
	return snap, nil
}

// kindNumber is v as the load gives a key of kind k: an integer key's
// value an int64, a fraction refused; any other value as it is.
func kindNumber(k configschema.Kind, v any) (any, error) {
	if x, ok := v.(float64); ok && k == configschema.Integer {
		if x != math.Trunc(x) || math.Abs(x) > 1<<53 {
			return nil, fmt.Errorf("%v is not an integer", x)
		}
		return int64(x), nil
	}
	return v, nil
}
