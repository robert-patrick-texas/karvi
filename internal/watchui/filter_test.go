package watchui

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
)

// ids are the job IDs of rows in order (an unreadable file's base name).
func ids(rows []scoreboard.Row) []string {
	var out []string
	for _, r := range rows {
		if r.Error != "" {
			out = append(out, filepath.Base(r.Path))
		} else {
			out = append(out, r.Snapshot.ActivityID)
		}
	}
	return out
}

// TestMatches covers the filter's five fields: the operator, the job ID,
// the target names (the file's among them), MODE, and STATUS, case
// insensitively; an unreadable file by its name and the word invalid;
// and 28.2's words: a space at the end changes nothing, spaces alone
// filter nothing, and every word must occur, each in any field.
func TestMatches(t *testing.T) {
	rows := screenRows()
	for text, want := range map[string][]string{
		"":                     {"260924-134500-00", "260924-134403-00", "260922-080947-00", "260924-132832-00", "260923-134403-00", "260924-131100-00", "bad.json"},
		"SANCHEZ":              {"260924-134500-00"},
		"134403":               {"260924-134403-00", "260923-134403-00"},
		"vzn-ohio":             {"260924-134500-00", "260924-132832-00", "260923-134403-00"},
		"site-x":               {"260924-134403-00"},
		"crun":                 {"260924-134500-00"},
		"cmd":                  {"260922-080947-00", "260923-134403-00"},
		"errored":              {"260923-134403-00"},
		"run":                  {"260924-134500-00", "260924-134403-00", "260924-132832-00", "260924-131100-00"},
		"bad":                  {"bad.json"},
		"invalid":              {"bad.json"},
		"nothing":              nil,
		"cmd ":                 {"260922-080947-00", "260923-134403-00"},
		"   ":                  {"260924-134500-00", "260924-134403-00", "260922-080947-00", "260924-132832-00", "260923-134403-00", "260924-131100-00", "bad.json"},
		"cmd quinlan":          {"260922-080947-00", "260923-134403-00"},
		"quinlan\terrored":     {"260923-134403-00"},
		" run  LEE ":           {"260924-131100-00"},
		"cmd lee":              nil,
		"vzn-ohio run sanchez": {"260924-134500-00"},
	} {
		if got := ids(Filtered(rows, text)); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("filter %q: %v, want %v", text, got, want)
		}
	}
}

// TestOrder covers the sort: each key orders each section within itself,
// ties fall back to time, S reverses the section whole, and the
// unreadable file stays last.
func TestOrder(t *testing.T) {
	rows := screenRows()
	cases := []struct {
		key      string
		reverse  bool
		running  []string
		finished []string
	}{
		{"time", false, []string{"260924-134403-00", "260924-134500-00"}, []string{"260924-132832-00", "260924-131100-00", "260923-134403-00", "260922-080947-00", "bad.json"}},
		{"time", true, []string{"260924-134500-00", "260924-134403-00"}, []string{"260922-080947-00", "260923-134403-00", "260924-131100-00", "260924-132832-00", "bad.json"}},
		{"operator", false, []string{"260924-134403-00", "260924-134500-00"}, []string{"260924-131100-00", "260924-132832-00", "260923-134403-00", "260922-080947-00", "bad.json"}},
		{"mode", false, []string{"260924-134500-00", "260924-134403-00"}, []string{"260923-134403-00", "260922-080947-00", "260924-132832-00", "260924-131100-00", "bad.json"}},
		{"status", false, []string{"260924-134403-00", "260924-134500-00"}, []string{"260922-080947-00", "260924-132832-00", "260923-134403-00", "260924-131100-00", "bad.json"}},
		{"fail", false, []string{"260924-134403-00", "260924-134500-00"}, []string{"260923-134403-00", "260924-132832-00", "260924-131100-00", "260922-080947-00", "bad.json"}},
		{"fail", true, []string{"260924-134500-00", "260924-134403-00"}, []string{"260922-080947-00", "260924-131100-00", "260924-132832-00", "260923-134403-00", "bad.json"}},
		{"target", false, []string{"260924-134403-00", "260924-134500-00"}, []string{"260922-080947-00", "260924-131100-00", "260924-132832-00", "260923-134403-00", "bad.json"}},
		{"", false, []string{"260924-134403-00", "260924-134500-00"}, []string{"260924-132832-00", "260924-131100-00", "260923-134403-00", "260922-080947-00", "bad.json"}},
	}
	for _, c := range cases {
		running, finished := Order(rows, c.key, c.reverse)
		if got := ids(running); strings.Join(got, " ") != strings.Join(c.running, " ") {
			t.Errorf("%s reverse=%v running %v, want %v", c.key, c.reverse, got, c.running)
		}
		if got := ids(finished); strings.Join(got, " ") != strings.Join(c.finished, " ") {
			t.Errorf("%s reverse=%v finished %v, want %v", c.key, c.reverse, got, c.finished)
		}
	}
	// The cycle follows the columns left to right.
	if strings.Join(SortKeys, " ") != "time status operator mode fail target" || NextSortKey("time") != "status" || NextSortKey("target") != "time" || NextSortKey("") != "status" || NextSortKey("x") != "time" {
		t.Errorf("the cycle %v", SortKeys)
	}
	if !ValidSortKey("fail") || ValidSortKey("failed") || ValidSortKey("") {
		t.Errorf("ValidSortKey")
	}
}

// TestPrompt covers the filter on the screen: / opens the prompt in the
// footer's place, the text typed applies as the filter, Backspace edits,
// q types rather than leaves, Enter keeps the text with filter: TEXT and
// the counts as shown of all, the selection stays on its job while
// shown and moves to the first shown row otherwise, a second / edits
// the text, Escape clears it, and Ctrl-C leaves from the prompt.
func TestPrompt(t *testing.T) {
	m := model(160, 14)
	m.Frame()
	m.Key(kind(KeyDown)) // sanchez's crun
	if m.Selected != "260924-134500-00" {
		t.Fatalf("selected %q", m.Selected)
	}
	m.Key(rune_('/'))
	frame := m.Frame()
	if !m.Prompt || strings.TrimRight(frame[12], " ") != " /" {
		t.Errorf("prompt line %q", frame[12])
	}
	for _, r := range "quinlan" {
		m.Key(rune_(r))
	}
	frame = m.Frame()
	if strings.TrimRight(frame[12], " ") != " / quinlan" || m.Filter != "quinlan" {
		t.Errorf("typed %q filter %q", frame[12], m.Filter)
	}
	// The crun is hidden: the selection moved to the first shown row.
	if m.Selected != "260924-134403-00" || marked(frame) != "260924-134403-00" {
		t.Errorf("selection under the filter %q", m.Selected)
	}
	if got := ids(m.ordered()); strings.Join(got, " ") != "260924-134403-00 260924-132832-00 260923-134403-00 260922-080947-00" {
		t.Errorf("rows under quinlan: %v", got)
	}
	m.Key(rune_(0x7f))
	if m.Input != "quinla" || m.Filter != "quinla" {
		t.Errorf("backspace %q %q", m.Input, m.Filter)
	}
	if m.Key(rune_('q')) || m.Input != "quinlaq" {
		t.Errorf("q in the prompt: %q", m.Input)
	}
	m.Key(rune_(0x08))
	m.Key(kind(KeyEnter))
	frame = m.Frame()
	if m.Prompt || m.Filter != "quinla" {
		t.Errorf("after Enter: prompt %v filter %q", m.Prompt, m.Filter)
	}
	if got := strings.TrimRight(frame[12], " "); got != " 1 of 2 running  3 of 5 finished  1 stale  1 invalid   filter: quinla   /dev/shm/karvi/scoreboards  2s   q quit  ↵ detail  / filter  s sort  t theme  ? help" {
		t.Errorf("footer %q", got)
	}
	// The selection stays on its job while it is shown.
	m.Key(kind(KeyEnd))
	m.Key(rune_('/'))
	if m.Input != "quinla" {
		t.Errorf("a second / opens on %q", m.Input)
	}
	m.Key(rune_('n'))
	if m.Selected != "260922-080947-00" || m.Filter != "quinlan" {
		t.Errorf("selection %q filter %q", m.Selected, m.Filter)
	}
	m.Key(kind(KeyEscape))
	frame = m.Frame()
	if m.Prompt || m.Filter != "" || m.Input != "" || strings.Contains(frame[12], "filter:") || !strings.HasPrefix(frame[12], " 2 running  5 finished") {
		t.Errorf("after Escape: filter %q footer %q", m.Filter, frame[12])
	}
	if m.Selected != "260922-080947-00" {
		t.Errorf("selection after the filter cleared %q", m.Selected)
	}
	m.Key(rune_('/'))
	m.Key(rune_('#'))
	if len(m.ordered()) != 0 || marked(m.Frame()) != "" || m.Selected != "" {
		t.Errorf("no row matches #: %v selected %q", ids(m.ordered()), m.Selected)
	}
	if !m.Key(kind(KeyCtrlC)) {
		t.Errorf("Ctrl-C in the prompt does not leave")
	}
}

// TestSortKeys covers the sort on the screen, the cycle in the columns'
// order: s cycles the keys within each section, S reverses and restores,
// s after S starts the next key in its default direction, and the footer
// shows sort: KEY and ↓.
func TestSortKeys(t *testing.T) {
	m := model(140, 14)
	m.Frame()
	note := func() string {
		f := display.StripANSI(m.Frame()[12])
		if i := strings.Index(f, "sort:"); i >= 0 {
			return strings.SplitN(f[i:], "   ", 2)[0]
		}
		return ""
	}
	if note() != "" {
		t.Errorf("note under time %q", note())
	}
	m.Key(rune_('S'))
	if !m.Reverse || note() != "sort: time ↓" {
		t.Errorf("after S: reverse %v note %q", m.Reverse, note())
	}
	if got := ids(m.ordered()); !strings.HasPrefix(strings.Join(got, " "), "260924-134500-00 260924-134403-00 260922-080947-00") {
		t.Errorf("reversed time: %v", got)
	}
	m.Key(rune_('S'))
	if m.Reverse || note() != "" {
		t.Errorf("after the second S: reverse %v note %q", m.Reverse, note())
	}
	m.Key(rune_('S'))
	m.Key(rune_('s'))
	if m.SortKey != "status" || m.Reverse || note() != "sort: status" {
		t.Errorf("after s: key %q reverse %v note %q", m.SortKey, m.Reverse, note())
	}
	// By status: the running rows tie and keep their time order; the
	// finished rows read cancelled, completed, errored, then the stale
	// running one.
	frame := m.Frame()
	if !strings.Contains(frame[3], "quinlan") || !strings.Contains(frame[4], "sanchez") || !strings.HasPrefix(frame[5], " ──") || !strings.Contains(frame[6], "cancelled") || !strings.Contains(frame[7], "completed") || !strings.Contains(frame[8], "errored") {
		t.Errorf("by status: %q", frame[3:9])
	}
	for _, want := range []string{"operator", "mode", "fail", "target", "time", "status"} {
		m.Key(rune_('s'))
		if m.SortKey != want {
			t.Errorf("s cycled to %q, want %q", m.SortKey, want)
		}
	}
	m.Key(rune_('S'))
	if note() != "sort: status ↓" {
		t.Errorf("reversed status note %q", note())
	}
	// The selection follows its job through a resort.
	m.Selected = "260923-134403-00"
	m.Key(rune_('s'))
	m.Key(rune_('s'))
	if m.SortKey != "mode" || m.Selected != "260923-134403-00" || marked(m.Frame()) != "260923-134403-00" {
		t.Errorf("selection through the resort: %q under %s", m.Selected, m.SortKey)
	}
	// The table form applies both: quinlan's rows by mode, the
	// two cmd rows newest first, then the run.
	m.Filter = "quinlan"
	lines := m.TableLines()
	if len(lines) != 5 || !strings.Contains(lines[1], "260924-134403-00") || !strings.Contains(lines[2], "errored") || !strings.Contains(lines[3], "cancelled") || !strings.Contains(lines[4], "completed") {
		t.Errorf("table under the filter and mode: %q", lines)
	}
}

// TestRunOptions covers the options: --filter and --sort set the table form,
// json refuses them, and an unknown sort key is refused.
func TestRunOptions(t *testing.T) {
	dir := t.TempDir()
	for i, s := range []struct{ id, mode, status, op string }{{"260924-134500-00", "crun", "running", "sanchez"}, {"260924-134403-00", "run", "completed", "quinlan"}} {
		snap := snap(s.mode, s.status, screenRows()[i].Snapshot.Counts, nil, nil)
		snap.ActivityID, snap.Operator.Username = s.id, s.op
		snap.LastUpdatedAt = time.Now()
		data, _ := json.Marshal(snap)
		if err := os.WriteFile(filepath.Join(dir, s.id+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := Run(context.Background(), Options{Directory: dir, Format: "table", Filter: "SANCHEZ"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 2 || !strings.Contains(lines[1], "sanchez") {
		t.Errorf("table under --filter: %q", out.String())
	}
	out.Reset()
	if err := Run(context.Background(), Options{Directory: dir, Format: "table", Sort: "operator"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 3 || !strings.Contains(lines[1], "sanchez") || !strings.Contains(lines[2], "quinlan") {
		t.Errorf("table under --sort operator: %q", out.String())
	}
	for _, o := range []Options{{Directory: dir, Format: "json", Filter: "x"}, {Directory: dir, Format: "json", Sort: "time"}} {
		err := Run(context.Background(), o, strings.NewReader(""), &out)
		if err == nil || errorcodes.Of(err) != "watch_json_filter_unsupported" {
			t.Errorf("json with a filter or sort: %v", err)
		}
	}
	err := Run(context.Background(), Options{Directory: dir, Format: "table", Sort: "failed"}, strings.NewReader(""), &out)
	if err == nil || errorcodes.Of(err) != "watch_sort_unknown" {
		t.Errorf("unknown sort: %v", err)
	}
	// The screen starts with both.
	out.Reset()
	if err := Run(context.Background(), Options{Directory: dir, Format: "tui", Filter: "run", Sort: "fail", Rows: 12, Columns: 120}, strings.NewReader("q"), &out); err != nil {
		t.Fatal(err)
	}
	if text := display.StripANSI(out.String()); !strings.Contains(text, "filter: run") || !strings.Contains(text, "sort: fail") {
		t.Errorf("screen start: %q", text)
	}
}
