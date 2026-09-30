package watchui

import (
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/records"
)

// screenRows are the example screen's jobs: two running, one finished, one
// stale running, one errored, one unreadable file.
func screenRows() []scoreboard.Row {
	at := func(day, h, mi, s int) time.Time { return time.Date(2026, 9, day, h, mi, s, 0, time.Local) }
	mk := func(id, mode, status, operator string, started time.Time, counts records.Counts, targets []records.ScoreboardTarget, inputs []executionplan.ScopeInput, ended *time.Time) scoreboard.Row {
		s := records.ScoreboardSnapshot{SchemaVersion: 2, ActivityID: id, Operator: records.Operator{Username: operator}, Mode: mode, Status: status, Counts: counts, Targets: targets, Inputs: inputs, StartedAt: started, LastUpdatedAt: started, EndedAt: ended}
		return scoreboard.Row{Snapshot: s, Path: "/dev/shm/karvi/scoreboards/" + id + ".json"}
	}
	end1, end2, end3 := at(24, 13, 28, 37), at(23, 13, 44, 6), at(22, 8, 9, 59)
	rows := []scoreboard.Row{
		mk("260924-134500-00", "crun", "running", "sanchez", at(24, 13, 45, 0), records.Counts{Total: 12, Completed: 3, InFlight: 2, NotStarted: 7}, []records.ScoreboardTarget{{Name: "vzn-ohio", State: "running"}, {Name: "vzn-ftc", State: "queued"}}, []executionplan.ScopeInput{{Kind: "target", Value: "vzn-ohio"}, {Kind: "target", Value: "vzn-ftc"}}, nil),
		mk("260924-134403-00", "run", "running", "quinlan", at(24, 13, 44, 3), records.Counts{Total: 120, Completed: 57, Failed: 1, InFlight: 8, NotStarted: 55}, []records.ScoreboardTarget{{Name: "gtn-wan-9500-1", State: "running"}, {Name: "gtn-gtn-7nx1", State: "running"}, {Name: "gtn-a", State: "succeeded"}}, []executionplan.ScopeInput{{Kind: "tf", Value: "site-x.txt"}}, nil),
		mk("260922-080947-00", "cmd", "cancelled", "quinlan", at(22, 8, 9, 47), records.Counts{Total: 1, Cancelled: 1}, []records.ScoreboardTarget{{Name: "gtn-wan-9500-1", State: "cancelled"}}, []executionplan.ScopeInput{{Kind: "target", Value: "gtn-wan-9500-1"}}, &end3),
		mk("260924-132832-00", "run", "completed", "quinlan", at(24, 13, 28, 32), records.Counts{Total: 2, Completed: 2}, []records.ScoreboardTarget{{Name: "vzn-ftc", State: "succeeded"}, {Name: "vzn-ohio", State: "succeeded"}}, nil, &end1),
		mk("260923-134403-00", "cmd", "errored", "quinlan", at(23, 13, 44, 3), records.Counts{Total: 2, Completed: 2, Failed: 2}, []records.ScoreboardTarget{{Name: "vzn-ohio", State: "failed"}, {Name: "vzn-ftc", State: "failed"}}, nil, &end2),
		mk("260924-131100-00", "run", "running", "lee", at(24, 13, 11, 0), records.Counts{Total: 80, Completed: 40, InFlight: 4, NotStarted: 36}, nil, []executionplan.ScopeInput{{Kind: "tf", Value: "ny-core.txt"}}, nil),
		{Path: "/dev/shm/karvi/scoreboards/bad.json", Error: "unexpected end of JSON input"},
	}
	rows[5].Stale = true
	return rows
}

func model(cols, lines int) *Model {
	return &Model{Rows: screenRows(), Now: time.Date(2026, 9, 24, 13, 48, 15, 0, time.Local), Location: time.Local, Directory: "/dev/shm/karvi/scoreboards", Refresh: 2 * time.Second, Theme: "dark", Cols: cols, Lines: lines}
}

// TestFrameLayout is the example screen at 120 by 14 without colour: the
// clock at the top right, the header with DONE's O over the slash and the
// line under it, the running rows by start above the rule, the
// finished rows newest first below it with the stale one flagged and the
// unreadable file last, a blank line, the footer, a blank line; every
// line within the width.
func TestFrameLayout(t *testing.T) {
	m := model(120, 14)
	frame := m.Frame()
	if len(frame) != 14 {
		t.Fatalf("%d lines", len(frame))
	}
	for i, line := range frame {
		if w := display.VisibleWidth(line); w > 120 {
			t.Errorf("line %d is %d wide: %q", i, w, line)
		}
	}
	if !strings.HasSuffix(frame[0], "2026-09-24 13:48:15 ") || strings.TrimSpace(frame[0]) != "2026-09-24 13:48:15" {
		t.Errorf("clock line %q", frame[0])
	}
	header := frame[1]
	if !strings.HasPrefix(header, "   JOB-ID            TIME      STATUS      OPERATOR  MODE        DONE   FAIL  ACTV  TARGET") {
		t.Errorf("header %q", header)
	}
	if frame[2] != " "+strings.Repeat("─", 118) {
		t.Errorf("line under the header %q", frame[2])
	}
	row := frame[3]
	slash := strings.Index(row, "/")
	if slash < 0 || header[slash] != 'O' {
		t.Errorf("DONE's O is not over the slash:\n%q\n%q", header, row)
	}
	want := []string{
		">  260924-134403-00  4m12s     running     quinlan   run        57/120     1     8  site-x.txt gtn-wan-9500-1 gtn-gtn-…",
		"   260924-134500-00  3m15s     running     sanchez   crun        3/12      0     2  vzn-ohio vzn-ftc",
		" " + strings.Repeat("─", 118),
		"   260924-132832-00  13:28:37  completed   quinlan   run         2/2       0     0  vzn-ftc vzn-ohio",
		" ! 260924-131100-00  37m15s    running     lee       run        40/80      0     4  ny-core.txt",
		"   260923-134403-00  13:44:06  errored     quinlan   cmd         2/2       2     0  vzn-ohio vzn-ftc",
		"   260922-080947-00  08:09:59  cancelled   quinlan   cmd         0/1       0     0  gtn-wan-9500-1",
		" ? bad.json          -         invalid     -         -            -        -     -  /dev/shm/karvi/scoreboards/bad.json",
	}
	for i, w := range want {
		if got := strings.TrimRight(frame[3+i], " "); got != w {
			t.Errorf("line %d\n got %q\nwant %q", 3+i, got, w)
		}
	}
	if frame[11] != "" || frame[13] != "" {
		t.Errorf("blank lines %q %q", frame[11], frame[13])
	}
	// The footer fits 120 columns with the short key hints (the
	// hints shrink first, the directory next).
	if got := strings.TrimRight(frame[12], " "); got != " 2 running  5 finished  1 stale  1 invalid   /dev/shm/karvi/scoreboards  2s   q quit  ? help" {
		t.Errorf("footer %q", got)
	}
	m.Cols = 140
	if got := strings.TrimRight(m.Frame()[12], " "); got != " 2 running  5 finished  1 stale  1 invalid   /dev/shm/karvi/scoreboards  2s   q quit  ↵ detail  / filter  s sort  t theme  ? help" {
		t.Errorf("wide footer %q", got)
	}
	m.Cols = 80
	if got := strings.TrimRight(m.Frame()[12], " "); got != " 2 running  5 finished  1 stale  1 invalid   …i/scoreboards  2s   q quit  ? help" || display.VisibleWidth(got) != 80 {
		t.Errorf("narrow footer %q", got)
	}
	m.Cols = 120
	if got := frame[11]; got != "" {
		t.Errorf("line before the footer %q", got)
	}
}

// TestFrameFillsAndScrolls: a short terminal shows the running rows and
// the rule, the footer counts the finished rows not shown, and the
// finished window follows the selection to the last row while the running
// rows and the rule stay where they are.
func TestFrameFillsAndScrolls(t *testing.T) {
	m := model(120, 10) // capacity 4: two running rows, the rule, one finished
	frame := m.Frame()
	if len(frame) != 10 {
		t.Fatalf("%d lines", len(frame))
	}
	if !strings.Contains(frame[3], "260924-134403-00") || !strings.HasPrefix(frame[5], " ──") || !strings.Contains(frame[6], "260924-132832-00") {
		t.Errorf("window: %q", frame[3:7])
	}
	if got := strings.TrimRight(frame[8], " "); !strings.HasPrefix(got, " 2 running  5 finished (1 shown)") {
		t.Errorf("footer %q", got)
	}
	m.Key(Key{Kind: KeyEnd})
	frame = m.Frame()
	if m.Scroll != 4 || !strings.Contains(frame[6], "bad.json") || !strings.HasPrefix(frame[6], ">") {
		t.Errorf("window at the end: scroll %d, last line %q", m.Scroll, frame[6])
	}
	if !strings.Contains(frame[3], "260924-134403-00") || !strings.Contains(frame[4], "260924-134500-00") || !strings.HasPrefix(frame[5], " ──") || !strings.HasPrefix(frame[2], " ──") {
		t.Errorf("the running rows and the lines moved: %q", frame[2:6])
	}
}

// TestPinnedLines covers the pinned lines on the operator's screen: with no
// running job the rule stands under the header's line and a sort that
// moves the selected job down a long finished list scrolls the finished
// rows alone, both lines staying; Home brings the first row back; with
// running rows, End keeps them and the rule on the screen; a running
// section that alone would fill the table scrolls within itself, leaving
// the rule and one finished row; a terminal under eight lines shows one
// line saying so.
func TestPinnedLines(t *testing.T) {
	quiet := &Model{Now: time.Date(2026, 9, 26, 14, 20, 57, 0, time.Local), Location: time.Local, Directory: "/tmp/nd/score", Refresh: time.Second, Theme: "dark", Cols: 110, Lines: 12}
	ops := []string{"quinlan", "lee", "sanchez", "netops"}
	for i := 0; i < 30; i++ {
		started := time.Date(2026, 9, 26, 12, 20, 56, 0, time.Local).Add(-time.Duration(9*i) * time.Minute)
		ended := started.Add(2 * time.Minute)
		s := records.ScoreboardSnapshot{SchemaVersion: 2, ActivityID: started.Format("060102-150405-00"), Operator: records.Operator{Username: ops[i%4]}, Mode: "run", Status: "completed", Counts: records.Counts{Total: 8, Completed: 8}, StartedAt: started, LastUpdatedAt: ended, EndedAt: &ended}
		quiet.Rows = append(quiet.Rows, scoreboard.Row{Snapshot: s, Path: "/tmp/nd/score/" + s.ActivityID + ".json"})
	}
	lines := func(m *Model) (under, rule bool, marked string) {
		f := m.Frame()
		return strings.HasPrefix(f[2], " ──"), strings.HasPrefix(f[3], " ──"), markedRow(f)
	}
	under, rule, sel := lines(quiet)
	if !under || !rule || sel != "260926-122056-00" {
		t.Fatalf("at the start: line %v rule %v selected %q", under, rule, sel)
	}
	quiet.Key(rune_('s')) // status: every row completed, the order by time
	quiet.Key(rune_('s')) // operator: quinlan's rows, the selected job the eighth of thirty
	under, rule, sel = lines(quiet)
	if !under || !rule || sel != "260926-122056-00" || quiet.Scroll == 0 {
		t.Errorf("after the sort: line %v rule %v marked %q scroll %d", under, rule, sel, quiet.Scroll)
	}
	if got := strings.TrimRight(quiet.Frame()[10], " "); !strings.HasPrefix(got, " 0 running  30 finished (5 shown)") || !strings.Contains(got, "sort: operator") {
		t.Errorf("footer %q", got)
	}
	quiet.Key(kind(KeyHome))
	under, rule, sel = lines(quiet)
	if !under || !rule || sel != "260926-121156-00" || quiet.Scroll != 0 {
		t.Errorf("after Home: line %v rule %v marked %q scroll %d", under, rule, sel, quiet.Scroll)
	}
	// With running rows: End scrolls the finished rows alone.
	m := model(120, 10)
	m.Frame()
	m.Key(kind(KeyEnd))
	f := m.Frame()
	if !strings.HasPrefix(f[2], " ──") || !strings.Contains(f[3], "260924-134403-00") || !strings.Contains(f[4], "260924-134500-00") || !strings.HasPrefix(f[5], " ──") || !strings.HasPrefix(f[6], ">") || !strings.Contains(f[6], "bad.json") {
		t.Errorf("End with running rows: %q", f[2:7])
	}
	// Six running jobs on a table of four lines: two running rows, the
	// rule, one finished row; the running window follows the selection.
	many := model(120, 10)
	for i := 0; i < 4; i++ {
		r := many.Rows[0]
		r.Snapshot.ActivityID = "260924-13450" + string(rune('1'+i)) + "-00"
		r.Snapshot.StartedAt = r.Snapshot.StartedAt.Add(time.Duration(i+1) * time.Second)
		many.Rows = append(many.Rows, r)
	}
	f = many.Frame()
	if !strings.HasPrefix(f[3], ">") || !strings.Contains(f[3], "260924-134403-00") || !strings.Contains(f[4], "260924-134500-00") || !strings.HasPrefix(f[5], " ──") || !strings.Contains(f[6], "260924-132832-00") || !strings.Contains(f[8], "6 running  5 finished (1 shown)") {
		t.Errorf("six running on four lines: %q", f[2:9])
	}
	many.Key(kind(KeyPageDown)) // four rows down, to the fifth running row
	f = many.Frame()
	if many.RunScroll != 3 || !strings.Contains(f[3], "260924-134502-00") || !strings.HasPrefix(f[4], ">") || !strings.Contains(f[4], "260924-134503-00") || !strings.HasPrefix(f[5], " ──") || !strings.Contains(f[6], "260924-132832-00") {
		t.Errorf("the running window: scroll %d %q", many.RunScroll, f[2:7])
	}
	many.Key(kind(KeyDown)) // the last running row
	many.Frame()            // drawn after every key, as the loop does
	many.Key(kind(KeyDown)) // the first finished row: the running window stays
	f = many.Frame()
	if many.RunScroll != 4 || !strings.Contains(f[3], "260924-134503-00") || !strings.Contains(f[4], "260924-134504-00") || !strings.HasPrefix(f[6], ">") || !strings.Contains(f[6], "260924-132832-00") {
		t.Errorf("into the finished rows: scroll %d %q", many.RunScroll, f[2:7])
	}
	many.Lines = 7
	if f := many.Frame(); len(f) != 1 || !strings.Contains(f[0], "120 x 7, needs 40 x 8") {
		t.Errorf("seven lines: %q", f)
	}
}

// markedRow is the job ID of the row marked > in a frame, or "".
func markedRow(frame []string) string {
	for _, line := range frame {
		if strings.HasPrefix(line, ">") {
			fields := strings.Fields(display.StripANSI(line[1:]))
			if len(fields) > 0 && (fields[0] == "!" || fields[0] == "?") {
				fields = fields[1:]
			}
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return ""
}

// TestFrameShrinks: the columns give way in order: TARGET
// first (cut with an ellipsis while the operator column keeps its width),
// then OPERATOR to eight, then JOB-ID to its last nine; at a width under
// the least, the screen cuts the line and the frame keeps every column.
func TestFrameShrinks(t *testing.T) {
	m := model(120, 14)
	m.Rows[0].Snapshot.Operator.Username = "alexandra-p"
	wide := m.Frame()[4]
	if !strings.Contains(wide, "alexandra-p  crun") {
		t.Errorf("wide operator %q", wide)
	}
	m.Cols = 90
	line := m.Frame()[4]
	if !strings.Contains(line, "alexandra-p  crun") || !strings.HasSuffix(line, "  v…") {
		t.Errorf("at 90 the target gives way first: %q", line)
	}
	m.Cols = 86
	line = m.Frame()[4]
	if !strings.Contains(line, "alexandr  crun") || !strings.HasSuffix(line, "  …") {
		t.Errorf("at 86 the operator shrinks: %q", line)
	}
	m.Cols = 82
	line = m.Frame()[4]
	if !strings.HasPrefix(line, "   134500-00  ") {
		t.Errorf("at 82 the job id shrinks: %q", line)
	}
	m.Cols = 40
	frame := m.Frame()
	if !strings.Contains(frame[3], "  8  …") {
		t.Errorf("at 40 the frame keeps every column: %q", frame[3])
	}
	for _, l := range Fit(frame, 14, 40) {
		if display.VisibleWidth(l) > 40 {
			t.Errorf("line wider than 40 after Fit: %q", l)
		}
	}
}

// TestFrameColours: with colour on, x is accent and y value, FAIL error
// above zero and muted at zero, ACTV address, the status by its role, the
// headings bold label, the rule border, the clock timestamp, a stale row
// muted throughout, the selected row bold; and the plain text is the same
// as without colour.
func TestFrameColours(t *testing.T) {
	m := model(120, 14)
	plain := m.Frame()
	m.Colour = true
	frame := m.Frame()
	for i := range plain {
		if display.StripANSI(frame[i]) != plain[i] {
			t.Errorf("line %d differs stripped:\n%q\n%q", i, display.StripANSI(frame[i]), plain[i])
		}
	}
	accent, value, errorC, muted, address := display.ANSIPrefix("cyan", false), display.ANSIPrefix("white", false), display.ANSIPrefix("red", false), display.ANSIPrefix("gray", false), display.ANSIPrefix("magenta", false)
	row := frame[4]
	for _, want := range []string{accent + "  3" + "\x1b[0m" + value + "/" + "\x1b[0m" + value + "12 ", muted + "   0", address + "   2", accent + "running   "} {
		if !strings.Contains(row, want) {
			t.Errorf("row lacks %q: %q", want, row)
		}
	}
	if !strings.Contains(frame[8], errorC+"   2") {
		t.Errorf("FAIL above zero not error: %q", frame[8])
	}
	if !strings.HasPrefix(frame[3], ">"+display.ANSIPrefix("white", true)+"  ") || !strings.Contains(frame[3], display.ANSIPrefix("cyan", true)+" 57") {
		t.Errorf("selected row not bold: %q", frame[3])
	}
	// The headings are bold label: blue under dark.
	if !strings.HasPrefix(frame[1], "   "+display.ANSIPrefix("blue", true)+"JOB-ID") {
		t.Errorf("header not bold label: %q", frame[1])
	}
	if !strings.HasPrefix(frame[5], " "+muted+"─") || !strings.HasPrefix(frame[2], " "+muted+"─") {
		t.Errorf("the lines not border: %q %q", frame[2], frame[5])
	}
	if !strings.Contains(frame[0], muted+"2026-09-24") {
		t.Errorf("clock not timestamp: %q", frame[0])
	}
	stale := frame[7]
	if strings.Contains(stale, accent) || strings.Contains(stale, address) || !strings.Contains(stale, muted+"! ") {
		t.Errorf("stale row not muted: %q", stale)
	}
	if !strings.Contains(frame[6], display.ANSIPrefix("green", false)+"completed") || !strings.Contains(frame[8], errorC+"errored") || !strings.Contains(frame[9], display.ANSIPrefix("orange", false)+"cancelled") {
		t.Errorf("status roles: %q %q %q", frame[6], frame[8], frame[9])
	}
	m.Theme = "light"
	if !strings.Contains(m.Frame()[4], display.ANSIPrefix("blue", false)+"  3") {
		t.Errorf("light accent: %q", m.Frame()[4])
	}
}
