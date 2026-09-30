package watchui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
)

// The / filter and the s sort.
// The filter is words: whitespace separates the terms, and each term is a
// case-insensitive substring that must occur in one of the row's five
// fields, the operator, the job ID, the target names, MODE, and STATUS. A
// space typed at the end of the text changes nothing, and a second word
// narrows (earlier the whole text was one
// substring, and "cmd " matched no row). The sort
// key orders each section within itself, so the running rows stay above
// the rule and the finished rows below it; ties fall back to time, and S
// reverses the key. Both are the session's: the keys and the two options
// set them, and no configuration key does.

// SortKeys are the s key's cycle in order: the sortable columns as the
// header shows them, left to right,
// time first. JOB-ID carries the start time, so TIME is its key; DONE and
// ACTV have none.
var SortKeys = []string{"time", "status", "operator", "mode", "fail", "target"}

// ValidSortKey says whether key is one of SortKeys.
func ValidSortKey(key string) bool {
	for _, k := range SortKeys {
		if k == key {
			return true
		}
	}
	return false
}

// NextSortKey is the key after key in the cycle (an empty key is time),
// time after the last and after an unknown key.
func NextSortKey(key string) string {
	if key == "" {
		key = "time"
	}
	for i, k := range SortKeys {
		if k == key && i+1 < len(SortKeys) {
			return SortKeys[i+1]
		}
	}
	return SortKeys[0]
}

// Terms are the filter's words: the text lowercased and split at
// whitespace. No term means no filter.
func Terms(text string) []string { return strings.Fields(strings.ToLower(text)) }

// filtering says whether text filters anything: a text of spaces alone
// does not, so the footer counts and the selection read as unfiltered.
func filtering(text string) bool { return len(Terms(text)) > 0 }

// Matches says whether a row shows under the filter text: every term of
// the text occurs in one of the row's
// fields. A text without a term shows every row.
func Matches(r scoreboard.Row, text string) bool {
	for _, term := range Terms(text) {
		if !matchesTerm(r, term) {
			return false
		}
	}
	return true
}

// matchesTerm says whether one lowercased term occurs in the operator,
// the job ID, one of the names the TARGET column shows (a target file's
// name among them), the MODE, or the STATUS. An unreadable file offers
// its file name and the word invalid, as its row shows them.
func matchesTerm(r scoreboard.Row, term string) bool {
	has := func(field string) bool { return strings.Contains(strings.ToLower(field), term) }
	if r.Error != "" {
		return has(filepath.Base(r.Path)) || has("invalid")
	}
	s := r.Snapshot
	if has(s.Operator.Username) || has(s.ActivityID) || has(s.Mode) || has(s.Status) {
		return true
	}
	for _, in := range s.Inputs {
		if in.Kind == "tf" && has(in.Value) {
			return true
		}
	}
	for _, t := range s.Targets {
		if has(t.Name) {
			return true
		}
	}
	return false
}

// Filtered is the rows that match text, in their order.
func Filtered(rows []scoreboard.Row, text string) []scoreboard.Row {
	if !filtering(text) {
		return rows
	}
	var kept []scoreboard.Row
	for _, r := range rows {
		if Matches(r, text) {
			kept = append(kept, r)
		}
	}
	return kept
}

// firstTarget is the TARGET column's first name, the sort field of the
// target key.
func firstTarget(r scoreboard.Row) string {
	cell := TargetCell(r.Snapshot, 0)
	if i := strings.IndexByte(cell, ' '); i >= 0 {
		return cell[:i]
	}
	return cell
}

// byTime is the time order of a section: the running rows
// oldest first, the finished rows newest first, then by job ID. It is
// every key's tie-break.
func byTime(a, b scoreboard.Row, running bool) int {
	sa, sb := a.Snapshot.StartedAt, b.Snapshot.StartedAt
	if !sa.Equal(sb) {
		if sa.Before(sb) == running {
			return -1
		}
		return 1
	}
	return -strings.Compare(a.Snapshot.ActivityID, b.Snapshot.ActivityID)
}

// compare orders two readable rows of one section under key: negative
// when a comes first. Text keys sort ascending, FAIL most first, each
// falling back to time.
func compare(a, b scoreboard.Row, key string, running bool) int {
	c := 0
	switch key {
	case "operator":
		c = strings.Compare(a.Snapshot.Operator.Username, b.Snapshot.Operator.Username)
	case "mode":
		c = strings.Compare(a.Snapshot.Mode, b.Snapshot.Mode)
	case "status":
		c = strings.Compare(a.Snapshot.Status, b.Snapshot.Status)
	case "fail":
		c = b.Snapshot.Counts.Failed - a.Snapshot.Counts.Failed
	case "target":
		c = strings.Compare(firstTarget(a), firstTarget(b))
	}
	if c != 0 {
		return c
	}
	return byTime(a, b, running)
}

// Order is the rows in the table's order under a sort key: the running
// rows, then the finished rows, each
// section sorted within itself by the key and reversed whole when
// reverse is set; an unreadable file always last. An unknown key sorts
// by time.
func Order(rows []scoreboard.Row, key string, reverse bool) (running, finished []scoreboard.Row) {
	for _, r := range rows {
		if Running(r) {
			running = append(running, r)
		} else {
			finished = append(finished, r)
		}
	}
	section := func(rows []scoreboard.Row, isRunning bool) {
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			if (a.Error != "") != (b.Error != "") {
				return a.Error == ""
			}
			if a.Error != "" {
				return false
			}
			c := compare(a, b, key, isRunning)
			if reverse {
				c = -c
			}
			return c < 0
		})
	}
	section(running, true)
	section(finished, false)
	return running, finished
}

// Ordered is the rows in the default order: by time, the
// running rows oldest first, the finished rows newest first, an
// unreadable file last.
func Ordered(rows []scoreboard.Row) (running, finished []scoreboard.Row) {
	return Order(rows, "time", false)
}

// visible is the model's rows as the screen shows them: filtered, then
// each section in the sort's order.
func (m *Model) visible() (running, finished []scoreboard.Row) {
	return Order(Filtered(m.Rows, m.Filter), m.SortKey, m.Reverse)
}

// sortNote is the footer's sort text: empty under time in
// its default direction, sort: KEY otherwise, with ↓ when reversed.
func (m *Model) sortNote() string {
	key := m.SortKey
	if key == "" {
		key = "time"
	}
	if key == "time" && !m.Reverse {
		return ""
	}
	note := "sort: " + key
	if m.Reverse {
		note += " ↓"
	}
	return note
}
