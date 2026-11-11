package natural

import "testing"

// TestCompare pins the natural order: a run of digits by value and then the
// fewer digits first, any other rune lowercase by code point, a name that is
// the start of another first, names alike so far by their text as written.
// Each pair is in order, its reverse the opposite.
func TestCompare(t *testing.T) {
	for _, pair := range [][2]string{
		{"router2", "router10"},                             // by value
		{"edge1", "edge01"},                                 // one value: the fewer digits first
		{"edge01", "edge3"},                                 // the value before the width
		{"router10", "router010"},                           // one value: the fewer digits first
		{"a1b", "a12"},                                      // the run's value, whatever follows it
		{"192.0.2.9", "192.0.2.10"},                         // every run, an address's too
		{"n99999999999999999999", "n100000000000000000000"}, // numbers of any size
		{"austin1", "Boston1"},                              // lowercase
		{"EDGE-CORE1", "edge1"},                             // "-" before "1"
		{"router", "router1"},                               // the start of another first
		{"Austin-rtr1", "austin-rtr1"},                      // alike but for case: as written
	} {
		a, b := pair[0], pair[1]
		if got := Compare(a, b); got >= 0 {
			t.Errorf("Compare(%q, %q) = %d, want < 0", a, b, got)
		}
		if got := Compare(b, a); got <= 0 {
			t.Errorf("Compare(%q, %q) = %d, want > 0", b, a, got)
		}
	}
	if got := Compare("router01", "router01"); got != 0 {
		t.Errorf("Compare(%q, %q) = %d, want 0", "router01", "router01", got)
	}
}
