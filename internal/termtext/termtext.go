// Package termtext renders the bytes a terminal was sent as the text it
// showed: a login's transcript, and the shell's output and prompts. It holds
// the line being written as rows of the terminal's width. Text wraps at the
// width; a carriage return goes to the row's start; a newline ends the line on
// its last row and moves down a row otherwise; a backspace and the cursor's
// left, right, and column moves stay in the row; cursor up and down move
// between the line's rows; erase in line, erase below, insert character, and
// delete character apply; the screen cleared discards the unfinished line (the
// editor redraws it); any other cursor positioning ends the line. A finished
// line is written whole, its rows joined and its trailing blanks removed.
// Every other sequence and control is dropped, tabs kept.
//
// The limits are the terminal's: a key the far end does not echo cannot
// appear, a full-screen program comes out as its text in the order drawn, and
// a device that shows a long line as a scrolled window gives the window.
package termtext

import (
	"bytes"
	"io"
	"unicode/utf8"
)

// Resize is a change of the terminal's width: from byte Offset of the input
// on, the terminal had Columns columns.
type Resize struct {
	Offset  int64
	Columns int
}

// Render is the text data showed on a terminal of the given columns, resized
// at each of resizes in order of their offsets. Columns of 0 is a terminal
// without a width: nothing wraps, and cursor up and down do nothing.
func Render(data []byte, columns int, resizes []Resize) []byte {
	var out bytes.Buffer
	r := New(&out, columns)
	var at int64
	for _, rs := range resizes {
		if rs.Offset > int64(len(data)) {
			break
		}
		if rs.Offset > at {
			r.Write(data[at:rs.Offset])
			at = rs.Offset
		}
		r.Resize(rs.Columns)
	}
	r.Write(data[at:])
	r.Close()
	return out.Bytes()
}

// parser states between writes.
const (
	ground  = iota
	escape  // after ESC
	escNext // ESC and an intermediate: one more byte ends it
	csi     // ESC [ and its parameters
	str     // an OSC, DCS, SOS, PM, or APC string, to BEL or ESC \
	strEsc  // ESC inside such a string
)

// Renderer renders a stream written to it, a finished line at a time, onto
// out. It is not safe for concurrent use.
type Renderer struct {
	out   io.Writer
	err   error
	width int

	// The unfinished line: cells in rows of width, the cursor at idx
	// (row*width + column). pending is the cursor past the last column after
	// a character written there, the wrap still to happen; held is a cursor
	// positioned elsewhere, the line ended unless the screen is cleared next.
	cells   []rune
	idx     int
	pending bool
	held    bool

	state   int
	params  []byte
	private bool
	utf     []byte
}

// New is a Renderer writing to out for a terminal of the given columns.
func New(out io.Writer, columns int) *Renderer {
	return &Renderer{out: out, width: max(columns, 0)}
}

// Resize changes the terminal's width from the next byte written on. The
// unfinished line keeps its cells, read in rows of the new width.
func (r *Renderer) Resize(columns int) {
	r.fix()
	r.width = max(columns, 0)
}

// Write renders p, writing each line it finishes. A sequence or a character
// split between two writes is joined. The error is the first from out.
func (r *Renderer) Write(p []byte) (int, error) {
	for _, b := range p {
		r.byte(b)
	}
	return len(p), r.err
}

// Close writes the unfinished line, if any, with an incomplete character as
// its bytes.
func (r *Renderer) Close() error {
	r.flushUTF()
	r.resolve()
	if len(r.cells) > 0 {
		r.emit()
	}
	return r.err
}

func (r *Renderer) byte(b byte) {
	switch r.state {
	case escape:
		r.escapeByte(b)
		return
	case escNext:
		r.state = ground
		return
	case csi:
		r.csiByte(b)
		return
	case str:
		switch b {
		case 0x07:
			r.state = ground
		case 0x1b:
			r.state = strEsc
		}
		return
	case strEsc:
		// ESC \ ends the string; any other ESC ends it too, and starts anew.
		if b == '\\' {
			r.state = ground
		} else {
			r.state = escape
			r.escapeByte(b)
		}
		return
	}
	if len(r.utf) > 0 || b >= 0x80 {
		r.utf = append(r.utf, b)
		r.drainUTF()
		return
	}
	r.ground(rune(b))
}

// drainUTF puts each character complete in the pending bytes, an invalid
// byte as itself.
func (r *Renderer) drainUTF() {
	for len(r.utf) > 0 {
		if !utf8.FullRune(r.utf) {
			return
		}
		c, n := utf8.DecodeRune(r.utf)
		if c == utf8.RuneError && n == 1 {
			r.ground(rawCell(r.utf[0]))
		} else {
			r.ground(c)
		}
		r.utf = r.utf[n:]
	}
	r.utf = nil
}

func (r *Renderer) flushUTF() {
	for _, b := range r.utf {
		r.ground(rawCell(b))
	}
	r.utf = nil
}

// rawCell holds a byte that is not UTF-8 as a negative cell, so the line is
// written back with the byte as it came.
func rawCell(b byte) rune { return -1 - rune(b) }

func (r *Renderer) ground(c rune) {
	switch {
	case c == 0x1b:
		r.state = escape
	case c == '\n':
		r.resolve()
		r.fix()
		if r.width > 0 && r.row() < r.lastRow() {
			r.idx += r.width
		} else {
			r.emit()
		}
	case c == '\r':
		r.resolve()
		r.fix()
		r.idx -= r.col()
	case c == '\b':
		r.resolve()
		r.fix()
		if r.col() > 0 {
			r.idx--
		}
	case c == '\t':
		r.resolve()
		r.put('\t')
	case c >= 0 && (c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0)):
		// another control: dropped
	default:
		r.resolve()
		r.put(c)
	}
}

func (r *Renderer) escapeByte(b byte) {
	r.state = ground
	switch {
	case b == '[':
		r.state, r.params, r.private = csi, r.params[:0], false
	case b == ']' || b == 'P' || b == 'X' || b == '^' || b == '_':
		r.state = str
	case b >= 0x20 && b <= 0x2f:
		// a character set or similar: ESC, the intermediate, one byte
		r.state = escNext
	case b == 0x1b:
		r.state = escape
	}
}

func (r *Renderer) csiByte(b byte) {
	switch {
	case b >= 0x30 && b <= 0x3f:
		if b == '?' || b == '>' || b == '<' || b == '=' {
			r.private = true
		}
		r.params = append(r.params, b)
	case b >= 0x20 && b <= 0x2f:
		// an intermediate: kept out of the parameters
		r.private = true
	case b >= 0x40 && b <= 0x7e:
		r.state = ground
		if !r.private {
			r.control(b, firstParam(r.params))
		}
	case b < 0x20:
		// a control inside the sequence is executed, as a terminal does;
		// an ESC abandons the sequence for a new one
		r.ground(rune(b))
		if r.state == ground {
			r.state = csi
		}
	default:
		// DEL is ignored; a byte past ASCII abandons the sequence
		if b >= 0x80 {
			r.state = ground
		}
	}
}

// firstParam is the sequence's first numeric parameter, 0 when absent.
func firstParam(p []byte) int {
	n := 0
	for _, b := range p {
		if b < '0' || b > '9' {
			break
		}
		n = min(n*10+int(b-'0'), 1<<16)
	}
	return n
}

func (r *Renderer) control(final byte, n int) {
	if final == 'J' && n == 2 {
		// the screen cleared: the unfinished line is the editor's to redraw
		r.held = false
		r.cells, r.idx, r.pending = r.cells[:0], 0, false
		return
	}
	switch final {
	case 'J':
		if n != 0 {
			// erase above, or the scrollback: nothing of the line
			return
		}
	case 'K', 'A', 'B', 'C', 'D', 'G', 'P', '@':
	case 'H', 'f', 'd', 'E', 'F':
		// any other positioning ends the line, unless the screen is
		// cleared before anything else is written
		r.held = true
		return
	default:
		return
	}
	r.resolve()
	r.fix()
	one := max(n, 1)
	start := r.idx - r.col()
	switch final {
	case 'K':
		switch n {
		case 0:
			r.blank(r.idx, r.rowEnd())
		case 1:
			r.blank(start, r.idx+1)
		case 2:
			r.blank(start, r.rowEnd())
		}
	case 'J':
		if n == 0 && r.idx < len(r.cells) {
			r.cells = r.cells[:r.idx]
		}
	case 'A':
		if r.width > 0 {
			r.idx = max(r.idx-one*r.width, r.col())
		}
	case 'B':
		if r.width > 0 {
			r.idx += one * r.width
		}
	case 'C':
		r.idx += one
		if r.width > 0 {
			r.idx = min(r.idx, start+r.width-1)
		}
	case 'D':
		r.idx = max(r.idx-one, start)
	case 'G':
		r.idx = start + one - 1
		if r.width > 0 {
			r.idx = start + min(one, r.width) - 1
		}
	case 'P':
		end := min(r.rowEnd(), len(r.cells))
		if r.idx < end {
			k := min(one, end-r.idx)
			copy(r.cells[r.idx:end], r.cells[r.idx+k:end])
			r.blank(end-k, end)
		}
	case '@':
		r.insert(one)
	}
}

// resolve settles a held line before anything else touches it: the line
// ends, and the cursor is at the start of a new one.
func (r *Renderer) resolve() {
	if !r.held {
		return
	}
	r.held = false
	if len(r.cells) > 0 {
		r.emit()
	}
	r.idx, r.pending = 0, false
}

func (r *Renderer) row() int {
	if r.width == 0 {
		return 0
	}
	return r.idx / r.width
}

func (r *Renderer) col() int {
	if r.width == 0 {
		return r.idx
	}
	return r.idx % r.width
}

// lastRow is the line's last row holding a cell.
func (r *Renderer) lastRow() int {
	if r.width == 0 || len(r.cells) == 0 {
		return 0
	}
	return (len(r.cells) - 1) / r.width
}

// rowEnd is the index past the cursor's row; without a width, the line's end.
func (r *Renderer) rowEnd() int {
	if r.width == 0 {
		return len(r.cells)
	}
	return r.idx - r.col() + r.width
}

// fix takes a pending wrap back: the cursor is on the last column.
func (r *Renderer) fix() {
	if r.pending {
		r.idx--
		r.pending = false
	}
}

func (r *Renderer) grow(n int) {
	for len(r.cells) < n {
		r.cells = append(r.cells, ' ')
	}
}

func (r *Renderer) put(c rune) {
	r.pending = false
	r.grow(r.idx + 1)
	r.cells[r.idx] = c
	r.idx++
	if r.width > 0 && r.idx%r.width == 0 {
		r.pending = true
	}
}

func (r *Renderer) blank(from, to int) {
	for i := from; i < min(to, len(r.cells)); i++ {
		r.cells[i] = ' '
	}
}

// insert opens n blanks at the cursor; on a terminal with a width what is
// pushed past the row's last column is lost.
func (r *Renderer) insert(n int) {
	if r.idx >= len(r.cells) {
		return
	}
	if r.width == 0 {
		r.cells = append(r.cells[:r.idx], append(make([]rune, n), r.cells[r.idx:]...)...)
		r.blank(r.idx, r.idx+n)
		return
	}
	end := r.rowEnd()
	r.grow(end)
	n = min(n, end-r.idx)
	copy(r.cells[r.idx+n:end], r.cells[r.idx:end-n])
	r.blank(r.idx, r.idx+n)
}

// emit writes the finished line and starts the next.
func (r *Renderer) emit() {
	end := len(r.cells)
	for end > 0 && r.cells[end-1] == ' ' {
		end--
	}
	line := make([]byte, 0, end+1)
	for _, c := range r.cells[:end] {
		if c < 0 {
			line = append(line, byte(-1-c))
		} else {
			line = utf8.AppendRune(line, c)
		}
	}
	line = append(line, '\n')
	if r.err == nil {
		_, r.err = r.out.Write(line)
	}
	r.cells, r.idx, r.pending = r.cells[:0], 0, false
}
