package platform

import (
	"bytes"
	"io"
)

// Live is the operator's live view of one device's session (--live). A
// transport writes the device's bytes to it as it reads them, from the
// first, and gives it each secret before writing that secret to the
// device, so the view can keep it off the screen. It is display only:
// nothing written to it is recorded, and a write to it never fails the
// session.
type Live interface {
	Write(p []byte)
	Secret(value []byte)
}

// SecretFilter writes what passes through it to W with the exact value of
// each secret it holds written Removed. A value cut by the edge of a write
// is still found: a tail that could begin a secret waits for the next
// write, or for Flush. The live view's filter; it is not safe for
// concurrent use.
type SecretFilter struct {
	W       io.Writer
	secrets [][]byte
	carry   []byte
}

// Add holds a copy of secret, until Wipe; an empty value is ignored.
func (f *SecretFilter) Add(secret []byte) {
	if len(secret) == 0 {
		return
	}
	for _, s := range f.secrets {
		if bytes.Equal(s, secret) {
			return
		}
	}
	f.secrets = append(f.secrets, append([]byte(nil), secret...))
}

func (f *SecretFilter) Write(p []byte) (int, error) {
	buf := p
	if len(f.carry) > 0 {
		buf = append(f.carry, p...)
	}
	out, rest := f.scan(buf, false)
	f.carry = append(f.carry[:0], rest...)
	if len(out) > 0 {
		if _, err := f.W.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush writes what was held back: at the end of the session no more of a
// secret can follow it.
func (f *SecretFilter) Flush() error {
	if len(f.carry) == 0 {
		return nil
	}
	out, _ := f.scan(f.carry, true)
	_, err := f.W.Write(out)
	wipe(f.carry)
	f.carry = f.carry[:0]
	return err
}

// Wipe zeroes the secrets held and what waits, and forgets them.
func (f *SecretFilter) Wipe() {
	for _, s := range f.secrets {
		wipe(s)
	}
	f.secrets = nil
	wipe(f.carry)
	f.carry = nil
}

// scan returns buf with each secret found written Removed, the longest
// first where two begin at one byte, and, unless final, the tail that a
// longer write could still make a secret, which it leaves out.
func (f *SecretFilter) scan(buf []byte, final bool) (out, rest []byte) {
	if len(f.secrets) == 0 {
		return buf, nil
	}
	start := 0
	for i := 0; i < len(buf); i++ {
		tail := buf[i:]
		if !final && f.couldBegin(tail) {
			return append(out, buf[start:i]...), tail
		}
		if n := f.longestAt(tail); n > 0 {
			out = append(append(out, buf[start:i]...), Removed...)
			start = i + n
			i += n - 1
		}
	}
	return append(out, buf[start:]...), nil
}

// couldBegin reports whether tail is the beginning of a secret longer than
// it.
func (f *SecretFilter) couldBegin(tail []byte) bool {
	for _, s := range f.secrets {
		if len(tail) < len(s) && bytes.HasPrefix(s, tail) {
			return true
		}
	}
	return false
}

// longestAt is the length of the longest secret b begins with, 0 for none.
func (f *SecretFilter) longestAt(b []byte) int {
	n := 0
	for _, s := range f.secrets {
		if len(s) > n && bytes.HasPrefix(b, s) {
			n = len(s)
		}
	}
	return n
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
