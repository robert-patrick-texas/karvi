package matching

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// TestCondenseWritesTheFold: Condense's text for each set, as the design
// session's prototype folded it (the fewest items, the last run on a tie),
// the grammar's characters escaped, items in natural order with text
// compared lowercase. internal/cli's TestCondenseReadsBackAsOneTl reads every
// text back through --tl's split and Spell.
func TestCondenseWritesTheFold(t *testing.T) {
	var hundreds []string // router001 to router300 but router100 and router251
	for n := 1; n <= 300; n++ {
		if n != 100 && n != 251 {
			hundreds = append(hundreds, fmt.Sprintf("router%03d", n))
		}
	}
	for _, tc := range []struct {
		names []string
		want  string
	}{
		// The prototype's cases (point5/cases.txt, its "fewest items" folds).
		{strings.Split("router01,router02,router03,router04,router10,edge1,edge2", ","), "edge[1-2],router[01-04,10]"},
		{[]string{"r9", "r10"}, "r[9-10]"},
		{[]string{"r09", "r10"}, "r[09-10]"},
		{[]string{"r1", "r01", "r001"}, "r[1,01,001]"},
		{[]string{"r08", "r9"}, "r[08,9]"},
		{[]string{"pod1-sw1", "pod2-sw1"}, "pod[1-2]-sw1"},
		{[]string{"pod1-sw1", "pod1-sw2", "pod2-sw1", "pod2-sw2"}, "pod1-sw[1-2],pod2-sw[1-2]"},
		{[]string{"core1-dc2", "core2-dc2", "core3-dc2", "core1-dc3"}, "core[1-3]-dc2,core1-dc3"},
		{[]string{"rtr1a", "rtr2a", "rtr3a"}, "rtr[1-3]a"},
		{[]string{"austin", "dallas", "router01"}, "austin,dallas,router01"},
		{[]string{"192.0.2.1", "192.0.2.2", "192.0.2.3", "192.0.2.10"}, "192.0.2.[1-3,10]"},
		{[]string{"192.0.2.1", "192.0.3.1"}, "192.0.[2-3].1"},
		{[]string{"2001:db8::1", "2001:db8::2", "2001:db8::a1", "2001:db8::a2"}, "2001:db8::[1-2],2001:db8::a[1-2]"},
		{hundreds, "router[001-099,101-250,252-300]"},
		// Ranges at the start's width; other widths their own items.
		{[]string{"r099", "r100"}, "r[099-100]"},
		{[]string{"r00", "r01", "r1"}, "r[00-01,1]"},
		{[]string{"r05", "r06", "r07", "r08", "r09", "r10", "r11"}, "r[05-11]"},
		{[]string{"r005", "r6"}, "r[005,6]"},
		{[]string{"r09", "r9", "r10"}, "r[9-10,09]"},
		{[]string{"r1", "r2", "r02"}, "r[1-2,02]"},
		{[]string{"r0", "r1"}, "r[0-1]"},
		{[]string{"1", "2", "5"}, "[1-2,5]"},
		{[]string{"1a", "2a", "3a"}, "[1-3]a"},
		{[]string{"r18446744073709551615", "r18446744073709551616", "r18446744073709551617"}, "r[18446744073709551615-18446744073709551617]"},
		// The grammar's characters escaped (S1's settle point 2).
		{[]string{"lab*1", "lab*2"}, `lab\*[1-2]`},
		{[]string{"a,b"}, `a\,b`},
		{[]string{"!edge1", "edge1"}, `\!edge1,edge1`},
		{[]string{"x!1"}, "x!1"},
		{[]string{"x[1]"}, `x\[1\]`},
		{[]string{"x[1]", "x[2]"}, `x\[[1-2]\]`},
		{[]string{"^core", "core$"}, `\^core,core\$`},
		{[]string{`back\slash1`}, `back\\slash1`},
		{[]string{"a?1"}, `a\?1`},
		{[]string{"core 1", "core 2"}, `core\ [1-2]`},
		{[]string{"-"}, `\-`},
		{[]string{"-r1", "-r2"}, "-r[1-2]"},
		// Natural order, text lowercase (S1's settle point 3).
		{[]string{"dallas1", "Boston1", "Boston2", "austin1"}, "austin1,Boston[1-2],dallas1"},
		{[]string{"edge1", "edge2", "EDGE-CORE1", "core1"}, "core1,EDGE-CORE1,edge[1-2]"}, // "-" before "1", as --order sorted
		{[]string{"Austin-rtr1", "austin-rtr2"}, "Austin-rtr1,austin-rtr2"},
		{[]string{"router1", "Router1"}, "Router1,router1"},
		{[]string{"r10x", "r9y"}, "r9y,r10x"},
		{[]string{"r01y", "r1x"}, "r1x,r01y"},
		{[]string{"a1", "a-1"}, "a-1,a1"},
		// Only ASCII digits fold, as only they make a range.
		{[]string{"r١", "r٢"}, "r١,r٢"},
		// A byte that is no UTF-8 is kept as it is beside an escape.
		{[]string{"\xff*1"}, "\xff\\*1"},
		// A repeat is one name; an empty name is no item.
		{[]string{"r1", "r1", "r2"}, "r[1-2]"},
		{[]string{"", "r1"}, "r1"},
		{nil, ""},
	} {
		in := append([]string(nil), tc.names...)
		if got := Condense(in); got != tc.want {
			t.Errorf("Condense(%q) = %q, want %q", tc.names, got, tc.want)
		}
		if !reflect.DeepEqual(in, tc.names) {
			t.Errorf("Condense(%q) changed its argument to %q", tc.names, in)
		}
		rng := rand.New(rand.NewSource(int64(len(tc.names))))
		for range 3 {
			rng.Shuffle(len(in), func(i, j int) { in[i], in[j] = in[j], in[i] })
			if got := Condense(in); got != tc.want {
				t.Errorf("Condense(%q), the same names in another order, = %q, want %q", in, got, tc.want)
			}
		}
	}
}
