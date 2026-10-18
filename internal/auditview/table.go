package auditview

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/display"
)

// The table (docs/EXAMPLES.md, chapter 64, issues 1 to 4; DEVICE's width
// chapter 66): no TIME; the columns at fixed widths, one space between them,
// JOB-ID 16, OPERATOR 18, MODE 5, EVENT 7, OUTCOME 16, DEVICE 30, then
// COMMAND. A cell longer than its column is cut with …; at a terminal the
// line is cut with … so that at least one cell stays free at its right edge,
// COMMAND cut or not shown at all; to a pipe or a file nothing cuts the line.
// OPERATOR and DEVICE, heading and cells, are centred in their columns, an
// odd cell to the right, while the width holds the column whole and the cell
// after it, and at the column's left when the width cuts into it.

// Table lays out the rows: the width to fit (0 for no limit), and whether
// the colours are on.
type Table struct {
	Width  int
	Colour bool
}

// widths are the columns' before COMMAND.
var widths = []int{16, 18, 5, 7, 16, 30}

// headings are the columns', COMMAND last.
var headings = []string{"JOB-ID", "OPERATOR", "MODE", "EVENT", "OUTCOME", "DEVICE", "COMMAND"}

// The columns centred while whole, and the theme whose roles colour them:
// the dark one, as the reader reads no configuration.
const (
	colOperator = 1
	colDevice   = 5
	theme       = "dark"
)

// piece is a run of text in one role, "" plain.
type piece struct {
	text, role string
}

// Heading is the heading line.
func (t Table) Heading() string {
	cells := make([]piece, len(headings))
	for i, h := range headings {
		cells[i] = piece{h, "label"}
	}
	return t.line(cells)
}

// Line is one row's line.
func (t Table) Line(r Row) string {
	return t.line([]piece{
		{r.ID, ""},
		{operatorCell(r.Operator, r.LoginUser), "accent"},
		{r.Mode, ""},
		{r.Event, ""},
		{r.Outcome, outcomeRole(r.Outcome)},
		{r.Device, "target"},
		{visible(r.Command), ""},
	})
}

// line lays out the cells: each before COMMAND cut to its column and padded,
// centred or at its left, then COMMAND; the line's end trimmed of spaces,
// then cut to the width less one cell.
func (t Table) line(cells []piece) string {
	var pieces []piece
	for i, c := range cells[:len(widths)] {
		text := cut(c.text, widths[i])
		left := 0
		if (i == colOperator || i == colDevice) && t.whole(i) {
			left = (widths[i] - runes(text)) / 2
		}
		pieces = append(pieces, piece{strings.Repeat(" ", left), ""}, piece{text, c.role}, piece{strings.Repeat(" ", widths[i]-left-runes(text)) + " ", ""})
	}
	pieces = append(pieces, cells[len(widths)])
	pieces = trimRight(pieces)
	if t.Width > 0 {
		pieces = fit(pieces, t.Width-1)
	}
	var b strings.Builder
	for _, p := range pieces {
		b.WriteString(t.paint(p))
	}
	return b.String()
}

// whole reports whether the width holds column i whole and the cell after
// it, where the line's … can stand without cutting the column's last cell.
func (t Table) whole(i int) bool {
	end := i
	for _, w := range widths[:i+1] {
		end += w
	}
	return t.Width == 0 || end <= t.Width-2
}

// paint is a piece in its role's colour and weight, when the colours are on.
func (t Table) paint(p piece) string {
	if p.role == "" || p.text == "" {
		return p.text
	}
	return display.ANSIStyle(p.text, display.RoleColor(theme, p.role, "default"), t.Colour, display.RoleBold(p.role))
}

// operatorCell is OPERATOR's text (issue 3): the operator, or
// OPERATOR/LOGINUSER when the event names a login user, the operator's part
// cut with … so that /LOGINUSER stays whole, and a login user too long for
// that cut at the pair's end.
func operatorCell(operator, loginUser string) string {
	w := widths[colOperator]
	if loginUser == "" {
		return cut(operator, w)
	}
	tail := "/" + loginUser
	if runes(tail) >= w-1 {
		return cut(operator+tail, w)
	}
	return cut(operator, w-runes(tail)) + tail
}

// outcomeRole is OUTCOME's colour by the word (issue 4): the success role
// for success, complete, and exercise; the warning role for work not done or
// cut short, cancel, halt, incomplete, and a command's not_attempted_*,
// not_started_*, and incomplete_*; the error role for every other word.
func outcomeRole(outcome string) string {
	switch {
	case outcome == "":
		return ""
	case outcome == "success" || outcome == "complete" || outcome == "exercise":
		return "success"
	case outcome == "cancel" || outcome == "halt" || outcome == "incomplete",
		strings.HasPrefix(outcome, "not_attempted_"), strings.HasPrefix(outcome, "not_started_"), strings.HasPrefix(outcome, "incomplete_"):
		return "warning"
	}
	return "error"
}

// visible is a statement with each control character written as Go writes
// it in a quoted string (\t, \n, \x1b), so that a row stays one line and its
// width is its cells.
func visible(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if isControl(r) {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isControl(r rune) bool { return r < ' ' || r == 0x7f }

// cut is text at most width cells, ending with … when cut.
func cut(text string, width int) string {
	if runes(text) <= width {
		return text
	}
	r := []rune(text)
	return string(r[:width-1]) + "…"
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// trimRight is the pieces without the spaces at the line's end.
func trimRight(pieces []piece) []piece {
	for len(pieces) > 0 {
		last := &pieces[len(pieces)-1]
		last.text = strings.TrimRight(last.text, " ")
		if last.text != "" {
			break
		}
		pieces = pieces[:len(pieces)-1]
	}
	return pieces
}

// fit is the pieces in at most limit cells: whole when they fit, else cut
// so that … takes the last cell.
func fit(pieces []piece, limit int) []piece {
	total := 0
	for _, p := range pieces {
		total += runes(p.text)
	}
	if total <= limit || limit < 1 {
		return pieces
	}
	var out []piece
	room := limit - 1
	for _, p := range pieces {
		n := runes(p.text)
		if n >= room {
			out = append(out, piece{string([]rune(p.text)[:room]), p.role})
			break
		}
		out = append(out, p)
		room -= n
	}
	return append(out, piece{"…", ""})
}
