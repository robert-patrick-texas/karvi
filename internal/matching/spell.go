package matching

import (
	"math/big"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/natural"
)

// Spells reports whether the pattern holds a numeric range, so that it
// spells a list of words (Spell). A single-digit range ("[1-3]") is one;
// a class ("[a-c]", "[!0-3]") is not.
func (p Pattern) Spells() bool { return len(p.spans) > 0 }

// Words is the number of words Spell gives, counted without spelling them:
// the product, over the pattern's numeric ranges, of the numbers each
// range's items name, an item that repeats another's numbers counted again.
// 1 for a pattern without a range.
func (p Pattern) Words() *big.Int {
	total := big.NewInt(1)
	for _, s := range p.spans {
		sum := new(big.Int)
		for _, it := range s.items {
			lo, _ := new(big.Int).SetString(string(it.lo), 10)
			hi, _ := new(big.Int).SetString(string(it.hi), 10)
			sum.Add(sum, hi.Sub(hi, lo).Add(hi, big.NewInt(1)))
		}
		total.Mul(total, sum)
	}
	return total
}

// Spell gives the words the pattern spells, in written order: the pattern
// as written, each numeric range replaced by one of its numbers written at
// its item's width (the lower bound's, zero-padded), the first range
// varying slowest, a range's items in written order and each from its lower
// bound up. Every other rune stays as written, its case and any other glob
// or escape kept, so a word is itself a pattern and Match accepts each
// word. ok is false, and nothing is spelled, when Words is above limit.
func (p Pattern) Spell(limit int) (words []string, ok bool) {
	if p.Words().Cmp(big.NewInt(int64(limit))) > 0 {
		return nil, false
	}
	runes := []rune(p.source)
	words = []string{""}
	at := 0
	for _, s := range p.spans {
		text := string(runes[at:s.start])
		numbers := s.numbers()
		next := make([]string, 0, len(words)*len(numbers))
		for _, w := range words {
			for _, n := range numbers {
				next = append(next, w+text+n)
			}
		}
		words = next
		at = s.end + 1
	}
	tail := string(runes[at:])
	for i := range words {
		words[i] += tail
	}
	return words, true
}

// numbers is a range's numbers as Spell writes them, in written order.
func (s rangeSpan) numbers() []string {
	var out []string
	for _, it := range s.items {
		n := append([]rune(nil), it.lo...)
		for {
			out = append(out, strings.Repeat("0", max(0, it.width-len(n)))+string(n))
			if natural.CompareNumbers(n, it.hi) >= 0 {
				break
			}
			n = increment(n)
		}
	}
	return out
}

// increment adds one to a number written as decimal digits.
func increment(n []rune) []rune {
	for i := len(n) - 1; i >= 0; i-- {
		if n[i] < '9' {
			n[i]++
			return n
		}
		n[i] = '0'
	}
	return append([]rune{'1'}, n...)
}
