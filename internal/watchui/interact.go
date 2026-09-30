package watchui

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/records"
)

// The keys and the detail pane, the / prompt, and the s and S keys. The
// selection is a job (its ID, or an unreadable file's
// path), not a row number: a refresh that reorders the rows keeps the job
// selected, and a job that leaves the directory leaves the selection on
// the row that took its place. Enter opens the pane in the lower third of
// the screen on the selected job, the pane follows the selection, Escape
// closes it; t toggles the theme for this screen; ? overlays the key list.

// rowKey identifies a row across refreshes.
func rowKey(r scoreboard.Row) string {
	if r.Error != "" {
		return r.Path
	}
	return r.Snapshot.ActivityID
}

// ordered is the table's rows in order, running first, under the filter
// and the sort.
func (m *Model) ordered() []scoreboard.Row {
	running, finished := m.visible()
	return append(running, finished...)
}

// resolve finds the selected row in rows, or moves the selection to the
// row at its old place when the job is gone from the directory and to the
// first shown row when the filter hides it, and returns its
// index (-1 with no rows).
func (m *Model) resolve(rows []scoreboard.Row) int {
	if len(rows) == 0 {
		m.Selected = ""
		return -1
	}
	for i, r := range rows {
		if rowKey(r) == m.Selected {
			m.selIndex = i
			return i
		}
	}
	if filtering(m.Filter) && m.Selected != "" {
		for _, r := range m.Rows {
			if rowKey(r) == m.Selected {
				m.selIndex = 0
				break
			}
		}
	}
	if m.selIndex >= len(rows) {
		m.selIndex = len(rows) - 1
	}
	if m.selIndex < 0 {
		m.selIndex = 0
	}
	m.Selected = rowKey(rows[m.selIndex])
	m.PaneScroll = 0
	return m.selIndex
}

// moveSelection moves the selection by delta rows, or to index when
// absolute, within the rows.
func (m *Model) moveSelection(delta int, absolute bool, index int) {
	rows := m.ordered()
	i := m.resolve(rows)
	if i < 0 {
		return
	}
	if absolute {
		i = index
	} else {
		i += delta
	}
	if i < 0 {
		i = 0
	}
	if i >= len(rows) {
		i = len(rows) - 1
	}
	if key := rowKey(rows[i]); key != m.Selected {
		m.Selected, m.selIndex, m.PaneScroll = key, i, 0
	}
}

// Key applies one key and says whether the screen leaves.
// While the help overlay is up any key but q and Ctrl-C closes it; while
// the / prompt is open the keys type into it (Ctrl-C still leaves).
func (m *Model) Key(k Key) (quit bool) {
	r := k.Rune
	if k.Kind != KeyRune {
		r = 0
	}
	if k.Kind == KeyCtrlC {
		return true
	}
	if m.Prompt {
		m.promptKey(k)
		return false
	}
	if r == 'q' {
		return true
	}
	if m.Help {
		m.Help = false
		return false
	}
	switch {
	case k.Kind == KeyUp || r == 'k':
		m.moveSelection(-1, false, 0)
	case k.Kind == KeyDown || r == 'j':
		m.moveSelection(1, false, 0)
	case k.Kind == KeyPageUp:
		m.moveSelection(-m.capacity(), false, 0)
	case k.Kind == KeyPageDown:
		m.moveSelection(m.capacity(), false, 0)
	case k.Kind == KeyHome:
		m.moveSelection(0, true, 0)
	case k.Kind == KeyEnd:
		m.moveSelection(0, true, len(m.Rows))
	case k.Kind == KeyEnter:
		if m.resolve(m.ordered()) >= 0 {
			m.Pane = true
		}
	case k.Kind == KeyEscape:
		m.Pane = false
	case k.Kind == KeyLeft:
		if m.PaneScroll > 0 {
			m.PaneScroll--
		}
	case k.Kind == KeyRight:
		m.PaneScroll++
	case r == 't':
		switch m.Theme {
		case "dark":
			m.Theme = "light"
		case "light":
			m.Theme = "dark"
		}
	case r == '?':
		m.Help = true
	case r == '/':
		// The prompt opens on the applied text, so a second / edits it.
		m.Prompt, m.Input = true, m.Filter
	case r == 's':
		// The next key in its default direction.
		m.SortKey, m.Reverse = NextSortKey(m.SortKey), false
	case r == 'S':
		m.Reverse = !m.Reverse
	}
	return false
}

// promptKey is one key while the / prompt is open: a
// printable rune is typed, Backspace edits, Enter keeps the text and
// returns to the table, Escape clears it and returns; the text applies as
// the filter while it is typed, so the table shows what it will keep.
func (m *Model) promptKey(k Key) {
	switch {
	case k.Kind == KeyEnter:
		m.Prompt, m.Filter = false, m.Input
	case k.Kind == KeyEscape:
		m.Prompt, m.Input, m.Filter = false, "", ""
	case k.Kind == KeyRune && (k.Rune == 0x7f || k.Rune == 0x08):
		if runes := []rune(m.Input); len(runes) > 0 {
			m.Input = string(runes[:len(runes)-1])
		}
		m.Filter = m.Input
	case k.Kind == KeyRune && k.Rune >= ' ':
		m.Input += string(k.Rune)
		m.Filter = m.Input
	}
}

// paneHeight is the pane's lines when open: the lower third of the
// screen, at least eight, fewer only when the screen has no room for
// the six fixed lines, the rule, one row, and the pane's title with them.
func (m *Model) paneHeight() int {
	if !m.Pane {
		return 0
	}
	h := m.Lines / 3
	if h < 8 {
		h = 8
	}
	if room := m.Lines - fixedLines - 1 - 1 - 1; h > room {
		h = room
	}
	if h < 1 {
		h = 1
	}
	return h
}

// capacity is the table's lines, the rule among them: the screen less the
// six fixed lines (the clock, the header, its line, the two blank lines,
// the footer) and the pane with its title. PgUp and PgDn move by it.
func (m *Model) capacity() int {
	c := m.Lines - fixedLines
	if m.Pane {
		c -= m.paneHeight() + 1
	}
	if c < 2 {
		c = 2
	}
	return c
}

// helpLines is the ? overlay: the key list.
func helpLines() []string {
	return []string{
		"   keys",
		"     q, Ctrl-C     leave the screen",
		"     ↑ ↓, k j      select a row",
		"     PgUp, PgDn    a screen of rows",
		"     Home, End     the first and last row",
		"     Enter         open the detail pane on the selected job",
		"     Escape        close the pane",
		"     ← →           scroll the pane's targets",
		"     /             filter the rows by words over operator, job ID, target, mode, status",
		"     s             cycle the sort key: time, status, operator, mode, fail, target",
		"     S             reverse the sort; S again restores it",
		"     t             toggle dark and light for this screen",
		"     ?             this list; any key closes it",
	}
}

// paneLines is the detail pane's content for a row: the
// kind of work in one line, the operator and host, the dispatch, the
// times, the metrics, then the targets in columns as name and state,
// running first, then failed, then the rest, scrolled by PaneScroll
// across the columns that do not fit; width is the pane's columns.
func (m *Model) paneLines(r scoreboard.Row, height, width int) []string {
	if r.Error != "" {
		return []string{"   " + m.render([]piece{{"invalid snapshot", "error", false}}), "   " + r.Path, "   " + m.render([]piece{{r.Error, "error", false}})}
	}
	s := r.Snapshot
	label := func(text string) piece { return piece{text, "label", false} }
	value := func(text string) piece { return piece{text, "value", false} }
	sep := piece{"  ", "", false}
	head := []piece{{workLine(s), "accent", false}}
	who := []piece{label("operator "), value(s.Operator.Username), sep, label("host "), value(s.Producer.Hostname), sep, label("pid "), value(strconv.Itoa(s.Producer.PID)), sep}
	if s.Daemon {
		who = append(who, value("daemon"))
	} else {
		who = append(who, value("in process"))
	}
	dispatch := []piece{label("dispatch ")}
	if s.DispatchMode != nil {
		dispatch = append(dispatch, value(*s.DispatchMode))
	} else {
		dispatch = append(dispatch, value("-"))
	}
	dispatch = append(dispatch, sep, label("wave "), value(strconv.Itoa(s.WaveNumber)), sep, label("width "), value(strconv.Itoa(s.Width)))
	when := []piece{label("started "), {s.StartedAt.In(m.location()).Format("2006-01-02 15:04:05"), "timestamp", false}, sep}
	if s.EndedAt != nil {
		when = append(when, label("ended "), piece{s.EndedAt.In(m.location()).Format("15:04:05"), "timestamp", false}, value(" ("+Duration(s.EndedAt.Sub(s.StartedAt))+")"))
	} else {
		when = append(when, label("elapsed "), value(Duration(m.Now.Sub(s.StartedAt))))
	}
	lines := []string{"   " + m.render(head), "   " + m.render(who), "   " + m.render(dispatch), "   " + m.render(when)}
	if mt := s.Metrics; mt != nil {
		metrics := []piece{label("output "), value(fmt.Sprintf("%d bytes", mt.OutputBytes))}
		if mt.InFlightBytes > 0 {
			// The responses still arriving.
			metrics = append(metrics, sep, label("in flight "), value(fmt.Sprintf("%d bytes", mt.InFlightBytes)))
		}
		if mt.Finished > 0 {
			metrics = append(metrics, sep, label("min "), value(fmt.Sprintf("%dms", mt.MinMS)), sep, label("avg "), value(fmt.Sprintf("%dms", mt.AvgMS)), sep, label("max "), value(fmt.Sprintf("%dms", mt.MaxMS)))
		}
		lines = append(lines, "   "+m.render(metrics))
	}
	rows := height - len(lines)
	if rows < 1 || len(s.Targets) == 0 {
		return lines
	}
	return append(lines, m.targetColumns(s.Targets, rows, width)...)
}

// workLine names the kind of work: the mode, the statements
// and their file, the collection directory and its counts.
func workLine(s records.ScoreboardSnapshot) string {
	mode := s.Mode
	if mode == "" {
		mode = s.ActivityType
	}
	switch mode {
	case "login", "exercise":
		return mode
	case "crun":
		if c := s.Collection; c != nil {
			return fmt.Sprintf("crun: into %s, %d replaced %d kept", c.Directory, c.Replaced, c.Kept)
		}
		return "crun"
	}
	if c := s.Commands; c != nil {
		text := fmt.Sprintf("%s: %d statement", mode, c.Count)
		if c.Count != 1 {
			text += "s"
		}
		if c.File != "" {
			text += " from " + c.File
		}
		return text
	}
	return mode
}

// targetColumns lays the targets out as name and state in as many
// columns as the width holds, rows deep, running first, then failed, then
// the rest; PaneScroll is the first column shown. When the columns
// overflow the width, the last line is given up to the note saying which
// are shown (← → columns 1–4 of 12) and the targets take one row fewer,
// so the note hides no cell and every target is reached by scrolling.
// targetCell is a target's text in the pane: its name and state, and
// beside a running target the settled bytes of its command so far when
// there are any.
func targetCell(t records.ScoreboardTarget) string {
	cell := t.Name + " " + t.State
	if t.State == records.TargetRunning && t.Bytes > 0 {
		cell += " " + strconv.FormatInt(t.Bytes, 10)
	}
	return cell
}

func (m *Model) targetColumns(targets []records.ScoreboardTarget, rows, width int) []string {
	order := func(state string) int {
		switch state {
		case records.TargetRunning:
			return 0
		case records.TargetFailed:
			return 1
		}
		return 2
	}
	sorted := append([]records.ScoreboardTarget(nil), targets...)
	sort.SliceStable(sorted, func(i, j int) bool { return order(sorted[i].State) < order(sorted[j].State) })
	cellWidth := 0
	for _, t := range sorted {
		if n := len([]rune(targetCell(t))); n > cellWidth {
			cellWidth = n
		}
	}
	fit := (width - 3) / (cellWidth + 2)
	if fit < 1 {
		fit = 1
	}
	perColumn := rows
	columns := (len(sorted) + perColumn - 1) / perColumn
	if columns > fit && rows > 1 {
		// The note needs the last line: lay the targets out one row shorter.
		perColumn = rows - 1
		columns = (len(sorted) + perColumn - 1) / perColumn
	}
	if m.PaneScroll > columns-fit {
		m.PaneScroll = columns - fit
	}
	if m.PaneScroll < 0 {
		m.PaneScroll = 0
	}
	last := m.PaneScroll + fit
	if last > columns {
		last = columns
	}
	lines := make([]string, rows)
	for c := m.PaneScroll; c < last; c++ {
		for i := 0; i < perColumn; i++ {
			idx := c*perColumn + i
			if idx >= len(sorted) {
				break
			}
			t := sorted[idx]
			cell := pad(targetCell(t), cellWidth, false)
			role := "target"
			switch t.State {
			case records.TargetFailed:
				role = "error"
			case records.TargetRunning:
				role = "accent"
			case records.TargetSucceeded:
				role = "success"
			case records.TargetQueued:
				role = "muted"
			default:
				role = "warning"
			}
			if lines[i] == "" {
				lines[i] = "  "
			}
			lines[i] += " " + m.render([]piece{{cell, role, false}}) + " "
		}
	}
	if columns > fit && rows > 1 {
		note := fmt.Sprintf("   ← → columns %d–%d of %d", m.PaneScroll+1, last, columns)
		lines[rows-1] = m.render([]piece{{note, "muted", false}})
	}
	return lines
}
