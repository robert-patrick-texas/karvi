package osutil

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"time"
)

const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// NewID returns the timestamp-prefixed identifier. The first sixteen
// suffix characters encode 80 random bits; four trailing checksum characters
// retain the required twenty-character printable suffix and detect typos.
func NewID(now time.Time) (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	encoded := encodeBase32(raw)
	sum := sha256.Sum256(raw)
	check := encodeBase32(sum[:3])[:4]
	_, off := now.Zone()
	sign := '+'
	if off < 0 {
		sign = '-'
		off = -off
	}
	zone := fmt.Sprintf("%c%02d%02d", sign, off/3600, (off%3600)/60)
	return now.Format("20060102T150405.000000") + zone + "-" + encoded + check, nil
}
func encodeBase32(b []byte) string {
	out := make([]byte, 0, (len(b)*8+4)/5)
	var acc uint64
	bits := 0
	for _, x := range b {
		acc = (acc << 8) | uint64(x)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, crockford[(acc>>bits)&31])
			if bits > 0 {
				acc &= (1 << bits) - 1
			} else {
				acc = 0
			}
		}
	}
	if bits > 0 {
		out = append(out, crockford[(acc<<(5-bits))&31])
	}
	return string(out)
}
