package inventory

import (
	"bytes"
	"crypto/sha256"
	"sort"
	"strings"
)

// Dispatch order names. The
// alias "name" is accepted for OrderSorted and recorded as "sorted".
const (
	OrderDefault = "default"
	OrderSorted  = "sorted"
	OrderShuffle = "shuffle"
	OrderRandom  = "random"
)

// CanonicalOrder maps a configured dispatch.order value to its recorded name.
func CanonicalOrder(order string) string {
	if order == "name" {
		return OrderSorted
	}
	return order
}

// Rank is the shuffle rank: the SHA-256 digest of the UTF-8
// bytes of key, one NUL byte, and the lowercase device name. It is a
// permanent contract: a recorded key reproduces an order.
func Rank(key, name string) []byte {
	h := sha256.New()
	h.Write([]byte(key))
	h.Write([]byte{0})
	h.Write([]byte(strings.ToLower(name)))
	return h.Sum(nil)
}

// Sort orders devices in place by the canonical order name:
// "default" keeps the assembled sequence, "sorted" sorts by lowercase name in
// byte order, and "shuffle" and "random" rank by Rank(key, name) ascending
// with ties broken by name. For "random" the caller supplies the epoch
// seconds as key.
func Sort(devices []Device, order, key string) {
	switch CanonicalOrder(order) {
	case OrderSorted:
		sort.SliceStable(devices, func(i, j int) bool {
			return strings.ToLower(devices[i].CanonicalName) < strings.ToLower(devices[j].CanonicalName)
		})
	case OrderShuffle, OrderRandom:
		ranks := make([][]byte, len(devices))
		for i, d := range devices {
			ranks[i] = Rank(key, d.CanonicalName)
		}
		idx := make([]int, len(devices))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool {
			if c := bytes.Compare(ranks[idx[a]], ranks[idx[b]]); c != 0 {
				return c < 0
			}
			return strings.ToLower(devices[idx[a]].CanonicalName) < strings.ToLower(devices[idx[b]].CanonicalName)
		})
		ordered := make([]Device, len(devices))
		for i, j := range idx {
			ordered[i] = devices[j]
		}
		copy(devices, ordered)
	}
}
