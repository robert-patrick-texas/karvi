package watchui

import (
	"io"
	"unicode/utf8"
)

// The key reader. A terminal in raw
// mode delivers a key as one byte, a UTF-8 rune, or an escape sequence; the
// reader turns the bytes into Keys. The screen gives each Key
// its meaning; every key it does not know does nothing.

// KeyKind names a key the screen may act on.
type KeyKind int

const (
	KeyRune KeyKind = iota // a printable or control character, in Rune
	KeyUp                  // the arrows
	KeyDown
	KeyLeft
	KeyRight
	KeyPageUp
	KeyPageDown
	KeyHome
	KeyEnd
	KeyEnter  // Return (CR or LF)
	KeyEscape // the Escape key on its own
	KeyCtrlC  // the byte 0x03, a signal key no longer in raw mode
	KeyOther  // an escape sequence the screen has no use for
)

// Key is one key press.
type Key struct {
	Kind KeyKind
	Rune rune // set for KeyRune
}

// csiFinals maps a CSI sequence's final byte (ESC [ final) or an SS3
// sequence's (ESC O final) to its key: the arrows, Home and End as most
// terminals send them in normal and application cursor mode.
var csiFinals = map[byte]KeyKind{'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft, 'H': KeyHome, 'F': KeyEnd}

// csiTildes maps the number of a CSI ESC [ n ~ sequence to its key: PgUp,
// PgDn, and the Home and End of terminals that send them this way.
var csiTildes = map[string]KeyKind{"1": KeyHome, "7": KeyHome, "4": KeyEnd, "8": KeyEnd, "5": KeyPageUp, "6": KeyPageDown}

// DecodeKey reads one key from the front of buf and says how many bytes it
// took. A lone escape byte is the Escape key: a terminal sends a sequence
// in one write, so a read that ends after ESC is the key itself. An
// unfinished sequence (ESC [ with no final byte yet) takes zero bytes; the
// caller reads more. An unknown sequence is KeyOther, consumed whole.
func DecodeKey(buf []byte) (Key, int) {
	if len(buf) == 0 {
		return Key{}, 0
	}
	switch buf[0] {
	case '\r', '\n':
		return Key{Kind: KeyEnter}, 1
	case 0x03:
		return Key{Kind: KeyCtrlC}, 1
	case 0x1b:
		return decodeEscape(buf)
	}
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && !utf8.FullRune(buf) {
		return Key{}, 0
	}
	return Key{Kind: KeyRune, Rune: r}, size
}

func decodeEscape(buf []byte) (Key, int) {
	if len(buf) == 1 {
		return Key{Kind: KeyEscape}, 1
	}
	switch buf[1] {
	case 'O': // SS3: ESC O final
		if len(buf) < 3 {
			return Key{}, 0
		}
		if kind, ok := csiFinals[buf[2]]; ok {
			return Key{Kind: kind}, 3
		}
		return Key{Kind: KeyOther}, 3
	case '[': // CSI: ESC [ parameters final, the final byte in 0x40..0x7e
		for i := 2; i < len(buf); i++ {
			c := buf[i]
			if c < 0x40 || c > 0x7e {
				continue
			}
			params := string(buf[2:i])
			if c == '~' {
				if kind, ok := csiTildes[params]; ok {
					return Key{Kind: kind}, i + 1
				}
				return Key{Kind: KeyOther}, i + 1
			}
			if kind, ok := csiFinals[c]; ok && params == "" {
				return Key{Kind: kind}, i + 1
			}
			return Key{Kind: KeyOther}, i + 1
		}
		return Key{}, 0
	}
	// ESC followed by another key (Alt-x): the Escape, then the key.
	return Key{Kind: KeyEscape}, 1
}

// ReadKeys decodes keys from r on its own goroutine and sends them on the
// returned channel until r ends or fails, when the channel closes. Bytes a
// read leaves unfinished wait for the next read. On a live terminal r is
// stdin in raw mode; the goroutine ends with the process, since a read of
// the terminal cannot be interrupted, and the screen has left raw mode by
// then.
func ReadKeys(r io.Reader) <-chan Key {
	keys := make(chan Key, 16)
	go func() {
		defer close(keys)
		var pending []byte
		buf := make([]byte, 64)
		for {
			n, err := r.Read(buf)
			pending = append(pending, buf[:n]...)
			for len(pending) > 0 {
				key, used := DecodeKey(pending)
				if used == 0 {
					if err != nil {
						// The stream ended inside a sequence: what is there
						// is an Escape and the rest as bytes.
						key, used = Key{Kind: KeyEscape}, 1
					} else {
						break
					}
				}
				pending = pending[used:]
				keys <- key
			}
			if err != nil {
				return
			}
		}
	}()
	return keys
}
