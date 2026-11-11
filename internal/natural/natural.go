// Package natural is the natural order of names, the one comparison by
// which karvi orders names for the operator: --order sorted dispatches
// devices (inventory.Sort), a gathered header's fold lists its items
// (matching.Condense), the gathered display breaks a tie between its
// groups, failed-devices.txt lists its devices, and the watch screen's
// target key orders its rows; and the decimal numbers it compares, which
// the selector grammar's numeric ranges share.
// The package imports nothing of karvi's, so the public inventory package
// shares it with matching without carrying the selector grammar and the
// platform definitions.
package natural

import (
	"cmp"
	"strings"
	"unicode"
)

// Compare orders names naturally: rune by rune, a run of digits against a
// run of digits by value and then the fewer digits first (router2 before
// router10, edge1 before edge01), any other rune lowercase by code point; a
// name that is the start of the other first; names alike so far by their
// text as written.
func Compare(a, b string) int {
	x, y := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(x) && j < len(y) {
		if IsDigit(x[i]) && IsDigit(y[j]) {
			ei, ej := i, j
			for ei < len(x) && IsDigit(x[ei]) {
				ei++
			}
			for ej < len(y) && IsDigit(y[ej]) {
				ej++
			}
			if c := cmp.Or(CompareNumbers(Number(x[i:ei]), Number(y[j:ej])), (ei-i)-(ej-j)); c != 0 {
				return c
			}
			i, j = ei, ej
			continue
		}
		if c := cmp.Compare(unicode.ToLower(x[i]), unicode.ToLower(y[j])); c != 0 {
			return c
		}
		i, j = i+1, j+1
	}
	return cmp.Or(cmp.Compare(len(x)-i, len(y)-j), strings.Compare(a, b))
}

// Number is a run of decimal digits without its leading zeros, "0" for
// zero, so two numbers of any size compare by CompareNumbers.
func Number(digits []rune) []rune {
	z := 0
	for z < len(digits)-1 && digits[z] == '0' {
		z++
	}
	return digits[z:]
}

// CompareNumbers orders two numbers as Number writes them: the longer is
// larger, and two of one length compare digit by digit.
func CompareNumbers(a, b []rune) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	for i := range a {
		if a[i] != b[i] {
			return int(a[i]) - int(b[i])
		}
	}
	return 0
}

// IsDigit reports whether r is an ASCII digit: only those make a run of
// digits, a number, or a numeric range.
func IsDigit(r rune) bool { return r >= '0' && r <= '9' }
