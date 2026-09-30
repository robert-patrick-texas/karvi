package watchui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/records"
)

func kind(k KeyKind) Key { return Key{Kind: k} }
func rune_(r rune) Key   { return Key{Kind: KeyRune, Rune: r} }
func marked(frame []string) string {
	for _, l := range frame {
		if strings.HasPrefix(l, ">") {
			fields := strings.Fields(strings.TrimPrefix(display.StripANSI(l), ">"))
			if fields[0] == "!" || fields[0] == "?" {
				fields = fields[1:]
			}
			return fields[0]
		}
	}
	return ""
}

// TestSelection covers the selection: the first row is selected at the start;
// Down, j, Up, k, PgDn, PgUp, End, and Home move it; the selection is a
// job, so a refresh that reorders the rows keeps it, and a job that
// leaves the directory leaves the selection on the row that took its
// place; the window follows the selection; every other key does nothing.
func TestSelection(t *testing.T) {
	m := model(120, 10) // capacity 4: two running rows, the rule, one finished
	m.Frame()
	if m.Selected != "260924-134403-00" || marked(m.Frame()) != "260924-134403-00" {
		t.Fatalf("first selection %q", m.Selected)
	}
	steps := []struct {
		key  Key
		want string
	}{
		{kind(KeyDown), "260924-134500-00"}, {rune_('j'), "260924-132832-00"}, {kind(KeyUp), "260924-134500-00"}, {rune_('k'), "260924-134403-00"},
		{kind(KeyUp), "260924-134403-00"}, {kind(KeyEnd), "bad.json"}, {kind(KeyHome), "260924-134403-00"},
		{kind(KeyPageDown), "260923-134403-00"}, {kind(KeyPageDown), "bad.json"}, {kind(KeyPageUp), "260924-132832-00"},
		{rune_('x'), "260924-132832-00"}, {kind(KeyOther), "260924-132832-00"}, {kind(KeyRight), "260924-132832-00"},
	}
	for _, st := range steps {
		if m.Key(st.key) {
			t.Fatalf("%v quit", st.key)
		}
		frame := m.Frame()
		if got := marked(frame); !strings.HasPrefix(st.want, got) && got != st.want {
			t.Errorf("after %v: marked %q, want %q", st.key, got, st.want)
		}
		if got := m.Selected; !strings.HasSuffix(got, st.want) {
			t.Errorf("after %v: selected %q, want %q", st.key, got, st.want)
		}
	}
	// The stale job ends: its row moves below the newer finished ones;
	// the selection follows the job.
	m.Key(kind(KeyEnd))
	m.Key(kind(KeyUp))
	m.Key(kind(KeyUp))
	m.Key(kind(KeyUp))
	if m.Selected != "260924-131100-00" || m.selIndex != 3 {
		t.Fatalf("selected %q at %d", m.Selected, m.selIndex)
	}
	// The stale job's snapshot is rewritten: ended, and with a later start
	// than the row above it, so it moves up a row; the selection follows.
	ended := time.Date(2026, 9, 24, 13, 50, 0, 0, time.Local)
	m.Rows[5].Stale = false
	m.Rows[5].Snapshot.Status = "completed"
	m.Rows[5].Snapshot.EndedAt = &ended
	m.Rows[5].Snapshot.StartedAt = time.Date(2026, 9, 24, 13, 30, 0, 0, time.Local)
	m.Frame()
	if m.Selected != "260924-131100-00" || m.selIndex != 2 || marked(m.Frame()) != "260924-131100-00" {
		t.Errorf("selection lost across the reorder: %q at %d", m.Selected, m.selIndex)
	}
	// The job leaves the directory: the selection stays on its row's place.
	idx := m.selIndex
	m.Rows = append(m.Rows[:5], m.Rows[6:]...)
	m.Frame()
	if m.selIndex != idx || m.Selected == "260924-131100-00" || m.Selected == "" {
		t.Errorf("after the job left: index %d (was %d), selected %q", m.selIndex, idx, m.Selected)
	}
	if !m.Key(rune_('q')) || !m.Key(kind(KeyCtrlC)) {
		t.Errorf("q and Ctrl-C do not leave")
	}
	empty := &Model{Cols: 120, Lines: 10, Now: time.Now()}
	empty.Frame()
	empty.Key(kind(KeyDown))
	empty.Key(kind(KeyEnter))
	if empty.Selected != "" || empty.Pane || marked(empty.Frame()) != "" {
		t.Errorf("an empty directory selects %q, pane %v", empty.Selected, empty.Pane)
	}
}

// TestPane covers the detail pane: Enter opens the pane in the
// lower third (at least eight lines) on the selected job with its title,
// the kind of work, operator, host, pid, daemon or in process, dispatch,
// wave and width, started and elapsed or ended, the metrics, and the
// targets in columns running first, then failed, then the rest; the
// pane follows the selection; Escape closes it; the table keeps its
// running rows above it.
func TestPane(t *testing.T) {
	m := model(120, 24)
	mode := "parallel"
	s := &m.Rows[1].Snapshot
	s.DispatchMode = &mode
	s.Width, s.WaveNumber = 8, 2
	s.Producer = records.Producer{Hostname: "ops.example", PID: 879625}
	s.Daemon = true
	s.Commands = &records.ScoreboardCommands{Count: 3, File: "show-set.txt"}
	s.Metrics = &records.ScoreboardMetrics{OutputBytes: 15012, Finished: 57, MinMS: 725, AvgMS: 739, MaxMS: 749}
	s.Targets = []records.ScoreboardTarget{{Name: "gtn-a", State: "succeeded"}, {Name: "gtn-b", State: "failed"}, {Name: "gtn-wan-9500-1", State: "running"}, {Name: "gtn-c", State: "queued"}}
	m.Frame()
	m.Key(kind(KeyEnter))
	frame := m.Frame()
	if len(frame) != 24 {
		t.Fatalf("%d lines", len(frame))
	}
	// the clock, the header, its line, capacity 24-6-9 = 9 table lines, the title, 8 pane lines, blank, footer, blank
	title := frame[12]
	if !strings.HasPrefix(title, " ── 260924-134403-00 ───") {
		t.Errorf("pane title %q (frame %q)", title, frame)
	}
	pane := frame[13:21]
	want := []string{
		"   run: 3 statements from show-set.txt",
		"   operator quinlan  host ops.example  pid 879625  daemon",
		"   dispatch parallel  wave 2  width 8",
		"   started 2026-09-24 13:44:03  elapsed 4m12s",
		"   output 15012 bytes  min 725ms  avg 739ms  max 749ms",
		"   gtn-wan-9500-1 running  gtn-c queued",
		"   gtn-b failed",
		"   gtn-a succeeded",
	}
	for i, w := range want {
		if got := strings.TrimRight(pane[i], " "); got != w {
			t.Errorf("pane line %d\n got %q\nwant %q", i, got, w)
		}
	}
	if !strings.HasPrefix(frame[22], " 2 running") || !strings.Contains(frame[22], "q quit  ? help") {
		t.Errorf("footer %q", frame[22])
	}
	m.Key(kind(KeyDown))
	frame = m.Frame()
	if !strings.HasPrefix(frame[12], " ── 260924-134500-00 ") || !strings.HasPrefix(strings.TrimRight(frame[13], " "), "   crun") {
		t.Errorf("pane did not follow: %q %q", frame[12], frame[13])
	}
	m.Key(kind(KeyEnd))
	frame = m.Frame()
	if !strings.Contains(frame[13], "invalid snapshot") || !strings.Contains(frame[14], "/bad.json") || !strings.Contains(frame[15], "unexpected end of JSON input") {
		t.Errorf("invalid pane: %q", frame[13:16])
	}
	m.Key(kind(KeyHome))
	m.Key(kind(KeyDown))
	m.Key(kind(KeyDown))
	frame = m.Frame()
	if !strings.HasPrefix(strings.TrimRight(frame[13], " "), "   run") || !strings.Contains(frame[16], "ended 13:28:37 (0m05s)") {
		t.Errorf("ended pane: %q", frame[13:17])
	}
	m.Key(kind(KeyEscape))
	frame = m.Frame()
	if strings.HasPrefix(frame[12], " ──") || len(frame) != 24 {
		t.Errorf("pane still open: %q", frame[12])
	}
	small := model(120, 10)
	small.Frame()
	small.Key(kind(KeyEnter))
	frame = small.Frame()
	// Ten lines: the pane is one line; a running row and the rule stay.
	if len(frame) != 10 || !strings.HasPrefix(frame[5], " ── ") || !strings.HasPrefix(frame[3], ">") || !strings.HasPrefix(frame[4], " ──") {
		t.Errorf("small pane: %q", frame)
	}
}

// TestTargetColumns: five hundred targets in a 24-line pane fill the
// width in columns; Right scrolls a column, Left back, the last line
// says which columns are shown; the order is running, failed, the rest.
func TestTargetColumns(t *testing.T) {
	m := model(120, 24)
	s := &m.Rows[1].Snapshot
	s.Targets = nil
	for i := 0; i < 500; i++ {
		state := "queued"
		switch {
		case i%100 == 0:
			state = "running"
		case i%50 == 0:
			state = "failed"
		case i < 250:
			state = "succeeded"
		}
		s.Targets = append(s.Targets, records.ScoreboardTarget{Name: "r" + strconv.Itoa(i), State: state})
	}
	m.Frame()
	m.Key(kind(KeyEnter))
	frame := m.Frame()
	first := strings.Fields(frame[17])
	if len(first) < 8 || first[0] != "r0" || first[1] != "running" {
		t.Errorf("first target line %q", frame[17])
	}
	if !strings.Contains(frame[20], "columns 1–") || !strings.Contains(frame[20], " of ") {
		t.Errorf("column note %q", frame[20])
	}
	m.Key(kind(KeyRight))
	frame2 := m.Frame()
	if !strings.Contains(frame2[20], "columns 2–") || frame2[17] == frame[17] {
		t.Errorf("Right did not scroll: %q", frame2[20])
	}
	m.Key(kind(KeyLeft))
	if got := m.Frame()[17]; got != frame[17] {
		t.Errorf("Left did not scroll back: %q", got)
	}
	for i := 0; i < 1000; i++ {
		m.Key(kind(KeyRight))
	}
	last := m.Frame()[20]
	if !strings.Contains(last, " of ") || !strings.HasSuffix(strings.TrimSpace(display.StripANSI(last)), strings.TrimSpace(strings.Split(display.StripANSI(last), " of ")[1])) {
		t.Errorf("clamped: %q", last)
	}
	// Every target is visible in some scroll position: the note has a line
	// of its own and hides no cell.
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		m.Key(kind(KeyLeft))
	}
	for {
		f := m.Frame()
		for _, line := range f[17:21] {
			for _, w := range strings.Fields(display.StripANSI(line)) {
				if strings.HasPrefix(w, "r") {
					seen[w] = true
				}
			}
		}
		before := m.PaneScroll
		m.Key(kind(KeyRight))
		m.Frame()
		if m.PaneScroll == before {
			break
		}
	}
	for i := 0; i < 500; i++ {
		if !seen["r"+strconv.Itoa(i)] {
			t.Errorf("target r%d never shown", i)
		}
	}
}

// TestThemeHelpAndSize: t toggles dark and light for this screen
// and nocolor stays; ? overlays the key list and any key closes
// it; a terminal under 40 by 6 shows one line saying so.
func TestThemeHelpAndSize(t *testing.T) {
	m := model(120, 14)
	m.Colour = true
	m.Key(rune_('t'))
	if m.Theme != "light" || !strings.Contains(m.Frame()[3], display.ANSIPrefix("blue", true)+" 57") {
		t.Errorf("theme %q after t: %q", m.Theme, m.Frame()[3])
	}
	m.Key(rune_('t'))
	if m.Theme != "dark" {
		t.Errorf("theme %q after the second t", m.Theme)
	}
	m.Theme = "nocolor"
	m.Key(rune_('t'))
	if m.Theme != "nocolor" {
		t.Errorf("nocolor toggled to %q", m.Theme)
	}
	m.Key(kind(KeyEnter))
	m.Key(rune_('?'))
	frame := m.Frame()
	// The overlay takes the table's and the pane's place; the line under
	// the header stays, the rule and the pane's title go.
	if !m.Help || !strings.Contains(frame[3], "keys") || !strings.Contains(frame[4], "q, Ctrl-C") || len(frame) != 14 || !strings.HasPrefix(display.StripANSI(frame[2]), " ──") || strings.Contains(strings.Join(frame[3:], "\n"), "──") {
		t.Errorf("help overlay: %q", frame)
	}
	if m.Lines = 24; !strings.Contains(m.Frame()[15], "any key closes it") {
		t.Errorf("help cut at 24 lines: %q", m.Frame()[3:16])
	}
	m.Lines = 14
	m.Pane = false
	m.Key(kind(KeyDown))
	if m.Help || m.Selected != "260924-134403-00" {
		t.Errorf("the closing key acted: help %v selected %q", m.Help, m.Selected)
	}
	m.Key(rune_('?'))
	if !m.Key(rune_('q')) {
		t.Errorf("q under help does not leave")
	}
	m.Help = false
	m.Cols, m.Lines = 39, 14
	if f := m.Frame(); len(f) != 1 || !strings.Contains(f[0], "too small") {
		t.Errorf("narrow: %q", f)
	}
	m.Cols, m.Lines = 120, 7
	if f := m.Frame(); len(f) != 1 || !strings.Contains(f[0], "120 x 7, needs 40 x 8") {
		t.Errorf("short: %q", f)
	}
}

// TestWorkLine: the pane's first line for each kind of work.
func TestWorkLine(t *testing.T) {
	cases := map[string]records.ScoreboardSnapshot{
		"login":                                 {Mode: "login"},
		"exercise":                              {Mode: "exercise"},
		"cmd: 1 statement":                      {Mode: "cmd", Commands: &records.ScoreboardCommands{Count: 1}},
		"run: 3 statements from show-set.txt":   {Mode: "run", Commands: &records.ScoreboardCommands{Count: 3, File: "show-set.txt"}},
		"crun: into backups, 3 replaced 0 kept": {Mode: "crun", Collection: &records.ScoreboardCollection{Directory: "backups", Replaced: 3}},
		"run":                                   {ActivityType: "run"},
	}
	for want, s := range cases {
		if got := workLine(s); got != want {
			t.Errorf("workLine = %q, want %q", got, want)
		}
	}
	_ = scoreboard.Row{}
}
