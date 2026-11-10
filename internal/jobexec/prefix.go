package jobexec

import (
	"bytes"
	"io"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/display"
)

// Line mode (display.run.prefix, a command's display.command.prefix;
// EXAMPLES chapter 106): every line of a record's text display is its
// device's name, ": ", and the line, as pdsh and clush write it, an empty
// line "NAME: " too; no device header, no ICMP gate's line (nor its
// --debug lines), and no border, as the gathered display. The echo line, the
// answer (stdout then stderr), the follow's notice in its place, and the
// failure and not-attempted lines without the target= the name repeats;
// gathered, the group's folded names before each of its lines, no group
// header, the block's marker unprefixed. karvi's own lines begin "! ";
// the footer stays the last line. The name in the target colour, ": " in
// the label colour, a failure line in the error colour, the device's
// output uncoloured.

// lineMode says whether the text display is line mode: its key set, the
// format text, and not the live view (whose session's bytes are the
// display).
func (r *recordRenderer) lineMode() bool {
	return r.prefix && r.format == "text" && !r.live
}

// linePrefix is line mode's "NAME: " before each of a record's lines, nil
// outside line mode.
func (r *recordRenderer) linePrefix(name string) []byte {
	if !r.lineMode() {
		return nil
	}
	return []byte(display.ANSIStyle(name, r.lineStyle.Target, r.colorEnabled, display.RoleBold("target")) +
		display.ANSIStyle(": ", r.lineStyle.Label, r.colorEnabled, false))
}

// appendLines appends text, one of karvi's lines about a record, styled by
// its role (colour and bold; "" and false plain): without a prefix as one
// line, with one each of its lines after the prefix, styled apart.
func (r *recordRenderer) appendLines(dst, prefix []byte, text, colour string, bold bool) []byte {
	if prefix == nil {
		return appendLine(dst, display.ANSIStyle(text, colour, r.colorEnabled, bold))
	}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		dst = append(dst, prefix...)
		dst = append(dst, display.ANSIStyle(line, colour, r.colorEnabled, bold)...)
		dst = append(dst, '\n')
	}
	return dst
}

// prefixChunk bounds what a linePrefixer holds before it writes: an
// answer's text comes in one write, however large.
const prefixChunk = 64 << 10

// linePrefixer writes an answer streamed through it with prefix before
// each line, put at the line's first byte, so a last line break leaves no
// prefix waiting; it holds nothing between writes, and at most
// prefixChunk and one prefix within one.
type linePrefixer struct {
	w      io.Writer
	prefix []byte
	mid    bool // within a line, its prefix written
	buf    []byte
}

// Write returns 0 with the error though earlier pieces went out, as every
// writer in the chain does; no caller reads the count.
func (p *linePrefixer) Write(b []byte) (int, error) {
	for rest := b; len(rest) > 0; {
		if !p.mid {
			p.buf = append(p.buf, p.prefix...)
			p.mid = true
		}
		take := len(rest)
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			take = i + 1
		}
		take = min(take, max(prefixChunk-len(p.buf), 1))
		p.buf = append(p.buf, rest[:take]...)
		p.mid = rest[take-1] != '\n'
		rest = rest[take:]
		if len(p.buf) >= prefixChunk {
			if err := p.flush(); err != nil {
				return 0, err
			}
		}
	}
	if err := p.flush(); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (p *linePrefixer) flush() error {
	if len(p.buf) == 0 {
		return nil
	}
	_, err := p.w.Write(p.buf)
	p.buf = p.buf[:0]
	return err
}
