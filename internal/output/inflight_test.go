package output

import "testing"

// TestInFlightCounters covers the in-flight counters: one per device
// on first use, 0 without one, the total over all.
func TestInFlightCounters(t *testing.T) {
	var f InFlight
	if f.Bytes("a") != 0 || f.Total() != 0 {
		t.Fatal("an empty set counts something")
	}
	a := f.Counter("a")
	a.Store(500)
	f.Counter("b").Store(7)
	if f.Counter("a") != a || f.Bytes("a") != 500 || f.Bytes("b") != 7 || f.Bytes("c") != 0 || f.Total() != 507 {
		t.Fatalf("a=%d b=%d c=%d total=%d", f.Bytes("a"), f.Bytes("b"), f.Bytes("c"), f.Total())
	}
	a.Store(0)
	if f.Total() != 7 || len(f.Devices()) != 2 {
		t.Fatalf("after a's end: total %d devices %v", f.Total(), f.Devices())
	}
	var none *InFlight
	if none.Bytes("a") != 0 || none.Total() != 0 {
		t.Fatal("a nil set counts something")
	}
}
