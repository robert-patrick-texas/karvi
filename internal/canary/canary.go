// Package canary seeds secret canaries and finds them in any encoding a leak
// would take. It imports nothing that can name a
// secret type, so the walker can prove it safe like any other contract.
package canary

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Value is one seeded canary: "ndcanary-" plus twenty random lowercase
// Crockford base32 characters, so it is a valid password for the fake device
// and survives every escaping unchanged.
type Value struct{ Raw string }

const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// New returns a fresh random canary.
func New() Value {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = crockford[int(b[i])%len(crockford)]
	}
	return Value{Raw: "ndcanary-" + string(b)}
}

// Hit is one occurrence of a canary in scanned data.
type Hit struct {
	Source   string
	Offset   int
	Encoding string
}

func (h Hit) String() string { return h.Source + ":" + strconv.Itoa(h.Offset) + ":" + h.Encoding }

// encodings returns every byte form the canary takes: raw, hex in both
// cases, base64 in the standard and URL alphabets at every 3-byte alignment,
// and the space-joined decimal bytes fmt prints for a []byte.
func (v Value) encodings() map[string][]byte {
	raw := []byte(v.Raw)
	out := map[string][]byte{
		"raw":       raw,
		"hex":       []byte(hex.EncodeToString(raw)),
		"HEX":       []byte(strings.ToUpper(hex.EncodeToString(raw))),
		"fmt-bytes": []byte(strings.Trim(fmt.Sprint(raw), "[]")),
	}
	for shift := 0; shift < 3; shift++ {
		padded := append(make([]byte, shift), raw...)
		for name, enc := range map[string]*base64.Encoding{"base64": base64.RawStdEncoding, "base64url": base64.RawURLEncoding} {
			s := enc.EncodeToString(padded)
			// Drop the characters that mix prefix bits with the canary, and
			// the final character when it carries bits beyond the canary.
			s = s[(shift*8+5)/6:]
			if (shift+len(raw))%3 != 0 {
				s = s[:len(s)-1]
			}
			out[fmt.Sprintf("%s+%d", name, shift)] = []byte(s)
		}
	}
	return out
}

// Scan reports every occurrence of every canary in data, in any encoding.
func Scan(source string, data []byte, values ...Value) []Hit {
	var hits []Hit
	for _, v := range values {
		for name, needle := range v.encodings() {
			if len(needle) == 0 {
				continue
			}
			for start := 0; ; {
				i := indexFrom(data, needle, start)
				if i < 0 {
					break
				}
				hits = append(hits, Hit{Source: source, Offset: i, Encoding: name})
				start = i + 1
			}
		}
	}
	return hits
}

func indexFrom(data, needle []byte, from int) int {
	if from >= len(data) {
		return -1
	}
	i := strings.Index(string(data[from:]), string(needle))
	if i < 0 {
		return -1
	}
	return from + i
}

// Found reports whether any canary occurs in data.
func Found(data []byte, values ...Value) bool { return len(Scan("", data, values...)) > 0 }
