package watchui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/records"
)

// The table and its two pinned lines.
// Model is what the screen shows between two refreshes: the rows as read,
// the time of the reading, the terminal's size, the theme, and how far each
// section is scrolled. Frame lays it out: the clock on the top line, the
// header with a line under it, the running rows, the rule, the finished
// rows filling the screen, a blank line, the footer, a blank line. The
// line under the header and the rule are always on the screen: the running
// rows and the rule are pinned, and only the finished rows scroll. The
// --format table form prints the same header and rows once, at any width,
// with no colour and no lines.

// Model is the screen's state.
type Model struct {
	Rows      []scoreboard.Row
	Now       time.Time      // the time of the last reading, the clock
	Location  *time.Location // the effective timezone
	Directory string         // named in the footer
	Refresh   time.Duration  // named in the footer
	Theme     string         // dark, light, nocolor; t toggles it
	Colors    map[string]string
	Colour    bool // whether escapes are written at all
	Cols      int
	Lines     int // the terminal's rows (Rows is the scoreboard's)
	Scroll    int // finished rows scrolled past; follows the selection
	RunScroll int // running rows scrolled past, only when they alone would fill the table

	// The interaction.
	Selected   string // the selected job's ID, or an unreadable file's path
	selIndex   int    // its last row index, for when the job is gone
	Pane       bool   // the detail pane is open on the selected job
	PaneScroll int    // the pane's first target column shown
	Help       bool   // the ? overlay is up

	// The filter and the sort.
	Filter  string // the applied filter text; empty shows every row
	Prompt  bool   // the / prompt is open in the footer's place
	Input   string // the prompt's text as typed, applied as the filter
	SortKey string // one of SortKeys; empty is time
	Reverse bool   // S: the current key's direction reversed
}

// color is a role's colour under the model's theme and the configured
// display.colors.* overrides.
func (m *Model) color(role string) string {
	return display.RoleColor(m.Theme, role, m.Colors[role])
}

// piece is a run of text in one colour; a line is pieces, already padded.
type piece struct {
	text string
	role string
	bold bool
}

// render writes a line's pieces with their colours, or plain.
func (m *Model) render(pieces []piece) string {
	var b strings.Builder
	for _, p := range pieces {
		if p.role == "" || !m.Colour {
			b.WriteString(p.text)
			continue
		}
		b.WriteString(display.ANSIStyle(p.text, m.color(p.role), true, p.bold))
	}
	return b.String()
}

// widths are the columns' widths for one screen.
type widths struct {
	jobID, status, operator, mode, done, doneX, fail, actv, target int
	timeW                                                          int
}

const (
	minCols    = 40 // under either the screen shows one line saying so
	minLines   = 8  // the six fixed lines, the rule, and one row
	fixedLines = 6  // the clock, the header, its line, a blank, the footer, a blank

	widthJobID      = 16 // YYMMDD-HHMMSS-xx
	widthJobIDShort = 9  // its last nine: HHMMSS-xx
	widthTime       = 8  // HH:MM:SS, or a duration
	widthStatus     = 10 // incomplete
	widthOperator   = 8  // the shrunk width and the least
	widthMode       = 8  // exercise
	widthCount      = 4  // FAIL and ACTV
	gap             = 2
)

// layout sizes the columns for a line of avail cells (0 for no limit): the
// operator column as wide as the longest name shown, at least eight; DONE
// as wide as the total needs; TARGET what remains, and when nothing
// remains OPERATOR shrinks to eight, then JOB-ID to its last nine.
func (m *Model) layout(avail int) widths {
	w := widths{jobID: widthJobID, timeW: widthTime, status: widthStatus, operator: widthOperator, mode: widthMode, doneX: 3, fail: widthCount, actv: widthCount}
	for _, r := range m.Rows {
		if n := len([]rune(r.Snapshot.Operator.Username)); n > w.operator {
			w.operator = n
		}
		if n := len(strconv.Itoa(r.Snapshot.Counts.Total)); n > w.doneX {
			w.doneX = n
		}
	}
	w.done = 2*w.doneX + 1
	if avail <= 0 {
		return w
	}
	fixed := func() int {
		return 2 + w.jobID + gap + w.timeW + gap + w.status + gap + w.operator + gap + w.mode + gap + w.done + gap + w.fail + gap + w.actv + gap
	}
	w.target = avail - fixed()
	if w.target < 1 {
		w.operator = widthOperator
		w.target = avail - fixed()
	}
	if w.target < 1 {
		w.jobID = widthJobIDShort
		w.target = avail - fixed()
	}
	if w.target < 1 {
		w.target = 1
	}
	return w
}

// pad fits text to width cells, left- or right-aligned, cut when longer.
func pad(text string, width int, right bool) string {
	runes := []rune(text)
	if len(runes) > width {
		runes = runes[:width]
	}
	fill := strings.Repeat(" ", width-len(runes))
	if right {
		return fill + string(runes)
	}
	return string(runes) + fill
}

// header is the heading line: the flag column blank, the headings bold in
// label, DONE placed so its O stands over the slash.
func (m *Model) header(w widths) []piece {
	h := func(text string, width int, right bool) piece {
		return piece{pad(text, width, right), "label", true}
	}
	done := strings.Repeat(" ", w.doneX-1) + "DONE"
	return []piece{{"  ", "", false}, h("JOB-ID", w.jobID, false), {"  ", "", false}, h("TIME", w.timeW, false), {"  ", "", false}, h("STATUS", w.status, false), {"  ", "", false}, h("OPERATOR", w.operator, false), {"  ", "", false}, h("MODE", w.mode, false), {"  ", "", false}, h(done, w.done, false), {"  ", "", false}, h("FAIL", w.fail, true), {"  ", "", false}, h("ACTV", w.actv, true), {"  ", "", false}, {"TARGET", "label", true}}
}

// row is one job's line (rules 1 to 5 and 7): the flag, then the cells; a
// stale row takes muted throughout; an unreadable file shows ? and its
// path in TARGET.
func (m *Model) row(r scoreboard.Row, w widths) []piece {
	sep := piece{"  ", "", false}
	if r.Error != "" {
		name := pad(filepath.Base(r.Path), w.jobID, false)
		if w.jobID == widthJobIDShort && len([]rune(name)) > widthJobIDShort {
			name = pad(lastRunes(name, widthJobIDShort), w.jobID, false)
		}
		return []piece{{"? ", "error", false}, {name, "muted", false}, sep, {pad("-", w.timeW, false), "muted", false}, sep, {pad("invalid", w.status, false), "error", false}, sep, {pad("-", w.operator, false), "muted", false}, sep, {pad("-", w.mode, false), "muted", false}, sep, {strings.Repeat(" ", w.doneX) + "-" + strings.Repeat(" ", w.doneX), "muted", false}, sep, {pad("-", w.fail, true), "muted", false}, sep, {pad("-", w.actv, true), "muted", false}, sep, {cut(r.Path, w.target), "muted", false}}
	}
	s := r.Snapshot
	id := s.ActivityID
	if w.jobID == widthJobIDShort {
		id = lastRunes(id, widthJobIDShort)
	}
	flag := piece{"  ", "", false}
	statusRole := statusRole(s.Status)
	failRole := "muted"
	if s.Counts.Failed > 0 {
		failRole = "error"
	}
	roles := map[string]string{"x": "accent", "y": "value", "actv": "address", "target": "target", "fail": failRole, "status": statusRole, "plain": ""}
	if r.Stale {
		flag = piece{"! ", "muted", false}
		for k := range roles {
			roles[k] = "muted"
		}
	}
	return []piece{flag, {pad(id, w.jobID, false), roles["plain"], false}, sep, {pad(TimeCell(s, m.Now, m.Location), w.timeW, false), roles["plain"], false}, sep, {pad(s.Status, w.status, false), roles["status"], false}, sep, {pad(s.Operator.Username, w.operator, false), roles["plain"], false}, sep, {pad(s.Mode, w.mode, false), roles["plain"], false}, sep,
		{pad(strconv.Itoa(s.Counts.Completed), w.doneX, true), roles["x"], false}, {"/", roles["y"], false}, {pad(strconv.Itoa(s.Counts.Total), w.doneX, false), roles["y"], false}, sep,
		{pad(strconv.Itoa(s.Counts.Failed), w.fail, true), roles["fail"], false}, sep, {pad(strconv.Itoa(s.Counts.InFlight), w.actv, true), roles["actv"], false}, sep, {TargetCell(s, w.target), roles["target"], false}}
}

// statusRole is the role a status takes: success, warning, error,
// accent for a running job.
func statusRole(status string) string {
	switch status {
	case "completed", "exercised":
		return "success"
	case "halted", "incomplete", "cancelled":
		return "warning"
	case "errored":
		return "error"
	}
	return "accent"
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// cut is text at most width cells, ending with … when cut; 0 is no limit.
func cut(text string, width int) string {
	if width <= 0 || len([]rune(text)) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	return string([]rune(text)[:width-1]) + "…"
}

// Running says whether a row belongs above the rule: a job not at a
// terminal status and not stale (rules 6 and 7).
func Running(r scoreboard.Row) bool {
	return r.Error == "" && !r.Stale && !Terminal(r.Snapshot.Status)
}

// Terminal says whether a status is a job's last: records.FinalStatuses,
// the one list.
func Terminal(status string) bool {
	return records.TerminalStatus(status)
}

// Frame is the screen: the clock, the header and the line
// under it, the running rows, the rule, the finished rows, with the
// selected row marked in the border column, the detail pane when open
// a blank line, the footer, a blank line, each line with a
// one-space border on the left. The line under the header and the rule
// never leave the screen: the running rows stand pinned
// between them and the finished rows scroll in the room that remains; a
// running section that alone would fill the table scrolls within itself,
// leaving the rule and one finished row. A line wider than the terminal
// is cut by the screen (the columns after TARGET, OPERATOR, and
// JOB-ID never give way). A terminal too small for the fixed lines, the
// rule, and one row shows one line saying so; the ? overlay
// takes the table's place.
func (m *Model) Frame() []string {
	if m.Lines < minLines || m.Cols < minCols {
		return []string{fmt.Sprintf(" terminal too small for the watch screen: %d x %d, needs %d x %d", m.Cols, m.Lines, minCols, minLines)}
	}
	avail := m.Cols - 2
	w := m.layout(avail)
	running, finished := m.visible()
	rows := append(append([]scoreboard.Row{}, running...), finished...)
	selected := m.resolve(rows)
	line := func(i int, r scoreboard.Row) string {
		border := " "
		pieces := m.row(r, w)
		if i == selected {
			border = ">"
			for k := range pieces {
				pieces[k].bold = true
				if pieces[k].role == "" {
					pieces[k].role = "value"
				}
			}
		}
		return border + m.render(pieces)
	}
	rule := " " + m.render([]piece{{strings.Repeat("─", avail), "border", false}})
	// The help overlay takes the pane's place too, so the key list is whole.
	pane := m.Pane && !m.Help
	capacity := m.Lines - fixedLines // the table's lines: the rule and the rows
	if pane {
		capacity -= m.paneHeight() + 1
	}
	if capacity < 2 {
		capacity = 2
	}
	// The two windows: the running rows take what they need, up to the
	// table less the rule and, when there are finished rows, one of them;
	// the finished rows take the rest. Each window follows the selection
	// and never scrolls past its section's end.
	runCap := capacity - 1
	if len(finished) > 0 && runCap > 1 {
		runCap--
	}
	runShown := len(running)
	if runShown > runCap {
		runShown = runCap
	}
	finCap := capacity - runShown - 1
	if selected >= 0 && selected < len(running) {
		m.RunScroll = follow(m.RunScroll, selected, runShown)
	} else if selected >= len(running) {
		m.Scroll = follow(m.Scroll, selected-len(running), finCap)
	}
	m.RunScroll = clampScroll(m.RunScroll, len(running), runShown)
	m.Scroll = clampScroll(m.Scroll, len(finished), finCap)
	finEnd := m.Scroll + finCap
	if finEnd > len(finished) {
		finEnd = len(finished)
	}
	var table []string
	for i := m.RunScroll; i < m.RunScroll+runShown; i++ {
		table = append(table, line(i, running[i]))
	}
	table = append(table, rule)
	for i := m.Scroll; i < finEnd; i++ {
		table = append(table, line(len(running)+i, finished[i]))
	}
	shownFinished := finEnd - m.Scroll
	frame := []string{m.clockLine(), " " + m.render(m.header(w)), rule}
	if m.Help {
		help := helpLines()
		if len(help) > capacity {
			help = help[:capacity]
		}
		frame = append(frame, help...)
	} else {
		frame = append(frame, table...)
	}
	for len(frame) < 3+capacity {
		frame = append(frame, "")
	}
	if pane && selected >= 0 {
		height := m.paneHeight()
		title := " " + m.render([]piece{{"── ", "border", false}, {rowKey(rows[selected]), "label", true}, {" " + strings.Repeat("─", avail-4-len([]rune(rowKey(rows[selected])))), "border", false}})
		frame = append(frame, title)
		pane := m.paneLines(rows[selected], height, m.Cols)
		if len(pane) > height {
			pane = pane[:height]
		}
		frame = append(frame, pane...)
		for len(frame) < 3+capacity+1+height {
			frame = append(frame, "")
		}
	}
	frame = append(frame, "", " "+m.render(m.footer(len(running), len(finished), shownFinished)), "")
	if m.Prompt {
		// The prompt takes the footer's place.
		frame[len(frame)-2] = " " + m.render([]piece{{"/ ", "label", true}, {m.Input, "value", false}})
	}
	return frame
}

// follow moves a window of capacity lines, scrolled past scroll lines, the
// least that brings the line at index into it.
func follow(scroll, index, capacity int) int {
	if index < scroll {
		return index
	}
	if capacity > 0 && index >= scroll+capacity {
		return index - capacity + 1
	}
	return scroll
}

// clampScroll keeps a window of capacity lines within a section of n
// lines: never past the end, never before the start.
func clampScroll(scroll, n, capacity int) int {
	if scroll > n-capacity {
		scroll = n - capacity
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}

// clockLine is the top line: the time of the last refresh at the right
// end in the effective timezone, in timestamp.
func (m *Model) clockLine() string {
	clock := m.Now.In(m.location()).Format("2006-01-02 15:04:05")
	left := m.Cols - 1 - len(clock)
	if left < 0 {
		left = 0
	}
	return strings.Repeat(" ", left) + m.render([]piece{{clock, "timestamp", false}}) + " "
}

func (m *Model) location() *time.Location {
	if m.Location == nil {
		return time.Local
	}
	return m.Location
}

// footer counts the rows, names the directory and the refresh,
// the filter and the sort when set, and the keys. Under a
// filter the counts read as shown of all: 2 of 3 running. The line fits
// the width: the key hints shrink to q quit and ? help first, then the
// directory is cut from its left, so the counts, the filter, and the sort
// are always whole.
func (m *Model) footer(running, finished, shown int) []piece {
	stale, invalid, allRunning, allFinished := 0, 0, 0, 0
	for _, r := range m.Rows {
		if r.Error != "" {
			invalid++
		} else if r.Stale {
			stale++
		}
		if Running(r) {
			allRunning++
		} else {
			allFinished++
		}
	}
	role := func(n int, worst string) string {
		if n > 0 {
			return worst
		}
		return "muted"
	}
	of := func(n, all int) string {
		if !filtering(m.Filter) {
			return strconv.Itoa(n)
		}
		return strconv.Itoa(n) + " of " + strconv.Itoa(all)
	}
	finishedText := of(finished, allFinished) + " finished"
	if shown < finished {
		finishedText += " (" + strconv.Itoa(shown) + " shown)"
	}
	pieces := []piece{{of(running, allRunning) + " running", "accent", false}, {"  ", "", false}, {finishedText, "label", false}, {"  ", "", false}, {strconv.Itoa(stale) + " stale", role(stale, "warning"), false}, {"  ", "", false}, {strconv.Itoa(invalid) + " invalid", role(invalid, "error"), false}}
	if filtering(m.Filter) {
		pieces = append(pieces, piece{"   ", "", false}, piece{"filter: " + strings.TrimSpace(m.Filter), "accent", false})
	}
	if note := m.sortNote(); note != "" {
		pieces = append(pieces, piece{"   ", "", false}, piece{note, "accent", false})
	}
	width := func(extra []piece) int {
		n := 0
		for _, p := range append(pieces, extra...) {
			n += len([]rune(p.text))
		}
		return n
	}
	avail := m.Cols - 1 // the border
	dir := m.Directory
	tail := func(hints string) []piece {
		return []piece{{"   ", "", false}, {dir, "muted", false}, {"  ", "", false}, {m.Refresh.String(), "muted", false}, {"   ", "", false}, {hints, "label", false}}
	}
	hints := "q quit  ↵ detail  / filter  s sort  t theme  ? help"
	if m.Cols > 0 && width(tail(hints)) > avail {
		hints = "q quit  ? help"
	}
	if over := width(tail(hints)) - avail; m.Cols > 0 && over > 0 {
		if keep := len([]rune(dir)) - over - 1; keep > 0 {
			dir = "…" + lastRunes(dir, keep)
		} else {
			dir = "…"
		}
	}
	return append(pieces, tail(hints)...)
}

// TableLines is the --format table form: the header and the rows in the
// table's order under the filter and the sort, no border,
// no lines, no colour, TARGET uncut.
func (m *Model) TableLines() []string {
	w := m.layout(0)
	colour := m.Colour
	m.Colour = false
	defer func() { m.Colour = colour }()
	lines := []string{m.render(m.header(w))}
	running, finished := m.visible()
	for _, r := range append(running, finished...) {
		lines = append(lines, strings.TrimRight(m.render(m.row(r, w)), " "))
	}
	return lines
}
