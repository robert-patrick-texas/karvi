package watchui

import (
	"strconv"
	"strings"
	"time"
)

// job list's table (EXAMPLES chapter 61, issue 3): watch's headings,
// cells, and status colours from each job's summary, without ACTV; TIME the
// job's duration, MODE its kind in four cells, OPERATOR always. The line
// fits the terminal's width: TARGET takes what the fixed columns leave, each
// name whole while it fits and the rest counted, +N, an unreadable
// folder's path whole or …; to a pipe or a file nothing is cut. Under
// --target a row is one device, its OUTCOME and the device in the place of
// STATUS, DONE, FAIL, and TARGET.

// ListRow is one row of job list's table.
type ListRow struct {
	JobID string
	// Invalid marks a folder whose summary could not be read or decoded:
	// ? in the flag column and Path in TARGET or DEVICE.
	Invalid  bool
	Path     string
	Operator string // the owner's name, or its UID
	Kind     string // cmd, run, or crun
	// Status is the final status, or unfinished; Duration and the counts
	// are a finished job's, Total an unfinished one's too.
	Status                  string
	Finished                bool
	Duration                time.Duration
	Completed, Total, Fails int
	Devices                 []string
	// NoSummary marks a job folder without a summary: unfinished, its
	// kind and its total not known either (u).
	NoSummary bool
	// Device and Outcome are the row's device under --target; an
	// unfinished job's device has the outcome unfinished.
	Device, Outcome string
}

// ListTable is job list's text: its rows, whether they are devices, the
// width to fit (0 for no limit), and the colours.
type ListTable struct {
	Rows     []ListRow
	ByDevice bool
	Width    int
	Colour   bool
	Theme    string
	Colors   map[string]string
}

const (
	widthKind    = 4  // crun
	widthOutcome = 11 // not_started
	unknownCount = "u"
)

// listWidths are the columns' widths: OPERATOR as wide as the longest name,
// at least eight; DONE as wide as the largest total needs.
type listWidths struct {
	operator, doneX, target int
}

// Lines are the heading line and the rows, each trimmed at its right.
func (t ListTable) Lines() []string {
	m := &Model{Theme: t.Theme, Colors: t.Colors, Colour: t.Colour}
	w := listWidths{operator: widthOperator, doneX: 3}
	for _, r := range t.Rows {
		if n := len([]rune(r.Operator)); n > w.operator {
			w.operator = n
		}
		if n := len(strconv.Itoa(r.Total)); n > w.doneX {
			w.doneX = n
		}
	}
	header := t.header(w)
	if t.Width > 0 {
		// At least one cell: a terminal narrower than the fixed columns
		// still gets the count, never the names uncut.
		w.target = max(t.Width-width(header), 1)
	}
	lines := []string{strings.TrimRight(m.render(append(header, t.lastHeading())), " ")}
	for _, r := range t.Rows {
		lines = append(lines, strings.TrimRight(m.render(t.row(r, w)), " "))
	}
	return lines
}

// width is the cells the pieces take.
func width(pieces []piece) int {
	n := 0
	for _, p := range pieces {
		n += len([]rune(p.text))
	}
	return n
}

func (t ListTable) lastHeading() piece {
	if t.ByDevice {
		return piece{"DEVICE", "label", true}
	}
	return piece{"TARGET", "label", true}
}

// header is the heading line up to the last column, DONE placed so its O
// stands over the slash, as watch's.
func (t ListTable) header(w listWidths) []piece {
	h := func(text string, width int, right bool) piece {
		return piece{pad(text, width, right), "label", true}
	}
	sep := piece{"  ", "", false}
	pieces := []piece{sep, h("JOB-ID", widthJobID, false), sep, h("TIME", widthTime, false), sep}
	if !t.ByDevice {
		pieces = append(pieces, h("STATUS", widthStatus, false), sep)
	}
	pieces = append(pieces, h("OPERATOR", w.operator, false), sep, h("MODE", widthKind, false), sep)
	if t.ByDevice {
		return append(pieces, h("OUTCOME", widthOutcome, false), sep)
	}
	done := strings.Repeat(" ", w.doneX-1) + "DONE"
	return append(pieces, h(done, 2*w.doneX+1, false), sep, h("FAIL", widthCount, true), sep)
}

// row is one row's pieces: a job's, or under --target a device's.
func (t ListTable) row(r ListRow, w listWidths) []piece {
	sep := piece{"  ", "", false}
	flag, plain, last := piece{"  ", "", false}, "", "target"
	timeCell, mode, modeRole := "", r.Kind, ""
	if r.Finished {
		timeCell = Duration(r.Duration)
	}
	if r.NoSummary {
		mode, modeRole = unknownCount, "warning"
	}
	if r.Invalid {
		flag, plain, last = piece{"? ", "error", false}, "muted", "muted"
		timeCell, mode = "-", "-"
	}
	if modeRole == "" {
		modeRole = plain
	}
	pieces := []piece{flag, {pad(r.JobID, widthJobID, false), plain, false}, sep, {pad(timeCell, widthTime, false), plain, false}, sep}
	if !t.ByDevice {
		status := r.Status
		if r.Invalid {
			status = "invalid"
		}
		pieces = append(pieces, piece{pad(status, widthStatus, false), statusRole(status), false}, sep)
	}
	pieces = append(pieces, piece{pad(r.Operator, w.operator, false), plain, false}, sep, piece{pad(mode, widthKind, false), modeRole, false}, sep)
	if t.ByDevice {
		outcome, device := r.Outcome, r.Device
		if r.Invalid {
			outcome, device = "invalid", fitPath(r.Path, w.target)
		}
		return append(pieces, piece{pad(outcome, widthOutcome, false), outcomeRole(outcome), false}, sep, piece{device, last, false})
	}
	return append(append(pieces, t.counts(r, w)...), sep, piece{t.target(r, w.target), last, false})
}

// fitPath is an unreadable folder's path in width cells, 0 for no limit:
// whole when it fits, … when it does not (EXAMPLES chapter 61, issue 4).
func fitPath(path string, width int) string {
	if width <= 0 || len([]rune(path)) <= width {
		return path
	}
	return "…"
}

// counts are DONE and FAIL: completed over total and the failed devices;
// an unfinished job's u over its total and u, in warning, the counts not
// known (EXAMPLES chapter 61, issue 3), and u over u for a folder without a
// summary (issue 5); an unreadable folder's -.
func (t ListTable) counts(r ListRow, w listWidths) []piece {
	sep := piece{"  ", "", false}
	switch {
	case r.Invalid:
		blank := strings.Repeat(" ", w.doneX)
		return []piece{{blank + "-" + blank, "muted", false}, sep, {pad("-", widthCount, true), "muted", false}}
	case r.NoSummary:
		return []piece{{pad(unknownCount, w.doneX, true), "warning", false}, {"/", "value", false}, {pad(unknownCount, w.doneX, false), "warning", false}, sep, {pad(unknownCount, widthCount, true), "warning", false}}
	case !r.Finished:
		return []piece{{pad(unknownCount, w.doneX, true), "warning", false}, {"/", "value", false}, {pad(strconv.Itoa(r.Total), w.doneX, false), "value", false}, sep, {pad(unknownCount, widthCount, true), "warning", false}}
	}
	failRole := "muted"
	if r.Fails > 0 {
		failRole = "error"
	}
	return []piece{{pad(strconv.Itoa(r.Completed), w.doneX, true), "accent", false}, {"/", "value", false}, {pad(strconv.Itoa(r.Total), w.doneX, false), "value", false}, sep, {pad(strconv.Itoa(r.Fails), widthCount, true), failRole, false}}
}

// target is TARGET in width cells, 0 for no limit: an unreadable folder's
// path (fitPath), or the devices in the plan's order, each whole while it
// fits and the rest counted, +N; +N alone when no name fits.
func (t ListTable) target(r ListRow, width int) string {
	if r.Invalid {
		return fitPath(r.Path, width)
	}
	all := strings.Join(r.Devices, " ")
	if width <= 0 || len([]rune(all)) <= width {
		return all
	}
	for shown := len(r.Devices) - 1; shown > 0; shown-- {
		text := strings.Join(r.Devices[:shown], " ") + " +" + strconv.Itoa(len(r.Devices)-shown)
		if len([]rune(text)) <= width {
			return text
		}
	}
	return "+" + strconv.Itoa(len(r.Devices))
}

// outcomeRole is the role a device's outcome takes.
func outcomeRole(outcome string) string {
	switch outcome {
	case "success", "ready":
		return "success"
	case "fail", "not_ready", "invalid":
		return "error"
	}
	return "warning"
}
