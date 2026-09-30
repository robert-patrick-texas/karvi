package configload

import (
	"fmt"
	"strings"
)

type LockDecl struct {
	Pattern string    `json:"pattern"`
	Source  SourceRef `json:"source"`
}

func lockMatches(pattern, key string) bool {
	p := strings.Split(strings.ToLower(pattern), ".")
	k := strings.Split(strings.ToLower(key), ".")
	if len(p) == 0 {
		return false
	}
	for i, seg := range p {
		if seg == "*" && i == len(p)-1 {
			return len(k) > i
		}
		if i >= len(k) {
			return false
		}
		if seg != "*" && seg != k[i] {
			return false
		}
	}
	return len(p) == len(k)
}
func specificity(pattern string) []int {
	p := strings.Split(pattern, ".")
	out := make([]int, len(p))
	for i, s := range p {
		if s != "*" {
			out[i] = 1
		}
	}
	return out
}
func compareSpec(a, b []int) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

func winningLock(locks []LockDecl, key string) (*LockDecl, error) {
	var matches []LockDecl
	for _, l := range locks {
		if lockMatches(l.Pattern, key) {
			matches = append(matches, l)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	winner := matches[0]
	ws := specificity(winner.Pattern)
	for _, candidate := range matches[1:] {
		cs := specificity(candidate.Pattern)
		cmp := compareSpec(cs, ws)
		if cmp > 0 {
			winner = candidate
			ws = cs
		} else if cmp == 0 {
			return nil, fmt.Errorf("%q at %s:%d and %q at %s:%d both match %q", winner.Pattern, winner.Source.Path, winner.Source.Line, candidate.Pattern, candidate.Source.Path, candidate.Source.Line, key)
		}
	}
	return &winner, nil
}
