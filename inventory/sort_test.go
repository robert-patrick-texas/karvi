package inventory

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func named(names ...string) []Device {
	out := make([]Device, len(names))
	for i, n := range names {
		out[i] = Direct(n, "", "", 0)
	}
	return out
}

func orderOf(devices []Device) []string {
	out := make([]string, len(devices))
	for i, d := range devices {
		out[i] = d.CanonicalName
	}
	return out
}

// TestRankIsAPermanentContract pins the shuffle rank to vectors computed outside
// karvi (sha256 of key, NUL, lowercase name). Changing them breaks the
// promise that a recorded shuffle_key reproduces an order.
func TestRankIsAPermanentContract(t *testing.T) {
	for _, tc := range []struct{ key, name, prefix string }{
		{"k", "r1", "0664c39947ad7666"},
		{"k", "r2", "1e2800481f683ecc"},
		{"k", "r3", "2c81fd638c071456"},
		{"k", "core-1", "fe38f33ce55cef55"},
		{"k", "CORE-1", "fe38f33ce55cef55"}, // lowercase name
		{"", "r1", "7060ccbc0d4653cb"},
		{"", "r2", "585a1e0552671750"},
		{"", "r3", "b2a9f859b696627d"},
		{"", "core-1", "c1f8f1ce2f86fd93"},
	} {
		if got := hex.EncodeToString(Rank(tc.key, tc.name))[:16]; got != tc.prefix {
			t.Fatalf("Rank(%q, %q)=%s, want %s", tc.key, tc.name, got, tc.prefix)
		}
	}
}

func TestSortOrders(t *testing.T) {
	for _, tc := range []struct {
		order, key string
		want       []string
	}{
		{"default", "", []string{"r3", "Core-1", "r1", "r2"}},
		{"sorted", "", []string{"Core-1", "r1", "r2", "r3"}},
		{"shuffle", "k", []string{"r1", "r2", "r3", "Core-1"}}, // ascending digest, see vectors above
		{"shuffle", "", []string{"r2", "r1", "r3", "Core-1"}},  // empty key allowed, fixed order
		{"random", "k", []string{"r1", "r2", "r3", "Core-1"}},  // same rank as shuffle with the same key
	} {
		devices := named("r3", "Core-1", "r1", "r2")
		Sort(devices, tc.order, tc.key)
		if got := orderOf(devices); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("order=%s key=%q: %q, want %q", tc.order, tc.key, got, tc.want)
		}
	}
}

func TestSortShuffleTiesByName(t *testing.T) {
	// Two spellings of one name rank equally; the tie goes to the lower name.
	devices := named("b", "a")
	devices[0].CanonicalName, devices[1].CanonicalName = "same", "same"
	devices[0].Name, devices[1].Name = "b", "a"
	Sort(devices, "shuffle", "x")
	if devices[0].Name != "b" || devices[1].Name != "a" {
		t.Fatalf("stable on equal rank and name: %q %q", devices[0].Name, devices[1].Name)
	}
}
