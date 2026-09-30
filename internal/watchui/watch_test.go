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

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/records"
)

func snap(mode, status string, counts records.Counts, targets []records.ScoreboardTarget, inputs []executionplan.ScopeInput) records.ScoreboardSnapshot {
	started := time.Date(2026, 9, 24, 13, 44, 3, 0, time.Local)
	s := records.ScoreboardSnapshot{SchemaVersion: 2, ActivityID: "260924-134403-00", Operator: records.Operator{Username: "quinlan"}, ActivityType: "run", Mode: mode, Status: status, Counts: counts, Targets: targets, Inputs: inputs, StartedAt: started, LastUpdatedAt: started}
	if status != "running" {
		ended := started.Add(4*time.Minute + 12*time.Second)
		s.EndedAt = &ended
	}
	return s
}

// TestCells covers the four cells: TIME as a
// duration then the end time, DONE with the slash in one place, TARGET
// with the file first, then the running devices, then the rest, cut with
// an ellipsis.
func TestCells(t *testing.T) {
	running := snap("run", "running", records.Counts{Total: 120, Completed: 57, Failed: 1, InFlight: 8},
		[]records.ScoreboardTarget{{Name: "a", State: "succeeded"}, {Name: "gtn-wan-9500-1", State: "running"}, {Name: "b", State: "queued"}},
		[]executionplan.ScopeInput{{Kind: "tf", Value: "site-x.txt"}, {Kind: "target", Value: "b"}})
	now := running.StartedAt.Add(4*time.Minute + 12*time.Second)
	if got := TimeCell(running, now, time.Local); got != "4m12s" {
		t.Errorf("running TIME %q", got)
	}
	done := snap("cmd", "errored", records.Counts{Total: 2, Completed: 2, Failed: 2}, nil, nil)
	if got := TimeCell(done, now, time.Local); got != "13:48:15" {
		t.Errorf("ended TIME %q", got)
	}
	for d, want := range map[time.Duration]string{41 * time.Second: "0m41s", 37*time.Minute + 2*time.Second: "37m02s", 3*time.Hour + 2*time.Minute + 59*time.Second: "3h02m"} {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
	if got := DoneCell(running.Counts); got != " 57/120" {
		t.Errorf("DONE %q", got)
	}
	if got := DoneCell(records.Counts{Total: 2, Completed: 2}); got != "  2/2  " {
		t.Errorf("DONE small %q", got)
	}
	if got := DoneCell(records.Counts{Total: 1200, Completed: 3}); got != "   3/1200" {
		t.Errorf("DONE wide %q", got)
	}
	if got := TargetCell(running, 0); got != "site-x.txt gtn-wan-9500-1 a b" {
		t.Errorf("TARGET %q", got)
	}
	if got := TargetCell(running, 12); got != "site-x.txt …" {
		t.Errorf("TARGET cut %q", got)
	}
}

// TestTableColumns: --format table prints the screen's columns in order, the
// flag column marks a stale row with ! and an unreadable file with ?, and
// no escape enters a cell.
func TestTableColumns(t *testing.T) {
	rows := []scoreboard.Row{
		{Snapshot: snap("crun", "running", records.Counts{Total: 12, Completed: 3, InFlight: 2, NotStarted: 7}, []records.ScoreboardTarget{{Name: "vzn-ohio", State: "running"}, {Name: "vzn-ftc", State: "queued"}}, []executionplan.ScopeInput{{Kind: "all"}}), Stale: true},
		{Path: "/dev/shm/karvi/scoreboards/bad.json", Error: "unexpected end of JSON input"},
	}
	m := &Model{Rows: rows, Now: time.Now(), Location: time.Local, Theme: "dark", Colour: true}
	lines := m.TableLines()
	if len(lines) != 3 {
		t.Fatalf("lines: %q", lines)
	}
	if !strings.HasPrefix(lines[0], "  JOB-ID") || !strings.Contains(lines[0], "TIME") || !strings.HasSuffix(lines[0], "TARGET") {
		t.Errorf("header %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "! 260924-134403-00") || !strings.Contains(lines[1], "running") || !strings.Contains(lines[1], "crun") || !strings.Contains(lines[1], "  3/12") || !strings.HasSuffix(lines[1], "vzn-ohio vzn-ftc") {
		t.Errorf("row %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "? bad.json") || !strings.Contains(lines[2], "invalid") || !strings.HasSuffix(lines[2], "/bad.json") {
		t.Errorf("invalid row %q", lines[2])
	}
	if strings.Contains(strings.Join(lines, ""), "\x1b") {
		t.Errorf("escape in table output")
	}
}

// TestRunScreen is the TUI against the fake terminal of 8 by 40: the
// screen is entered, the frame drawn at the terminal's size (JOB-ID at its
// last nine at this width, every line cut), and q leaves
// it with the cursor shown and the alternate screen dropped, nothing else
// written. The directory holds one snapshot.
func TestRunScreen(t *testing.T) {
	dir := t.TempDir()
	s := snap("run", "completed", records.Counts{Total: 2, Completed: 2}, []records.ScoreboardTarget{{Name: "vzn-ftc", State: "succeeded"}, {Name: "vzn-ohio", State: "succeeded"}}, []executionplan.ScopeInput{{Kind: "target", Value: "vzn-ftc"}, {Kind: "target", Value: "vzn-ohio"}})
	s.Producer = records.Producer{Hostname: "dev", PID: 1, AppVersion: "0.15.0"}
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "260924-134403-00.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	opts := Options{Directory: dir, Format: "tui", Theme: "nocolor", TimestampPattern: "hh:mm:ss yyyy-mm-dd", Refresh: time.Hour, MaxFiles: 100, StaleAfter: time.Hour, Rows: 8, Columns: 40}
	if err := Run(context.Background(), opts, strings.NewReader("xq"), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "\x1b[?1049h\x1b[?25l\x1b[H\x1b[2J\x1b[1;1H") || !strings.Contains(got, "\x1b[2;1H   JOB-ID") {
		t.Errorf("enter and header: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m\x1b[?25h\x1b[?1049l") {
		t.Errorf("leave: %q", got)
	}
	// Eight rows: the clock, the header, its line, the rule, the row, a
	// blank, the footer, a blank.
	if !strings.Contains(got, "\x1b[3;1H ──") || !strings.Contains(got, "\x1b[4;1H ──") || !strings.Contains(got, "\x1b[5;1H>  134403-00  13:48:15  completed") || strings.Contains(got, "\n") {
		t.Errorf("row: %q", got)
	}
	if !strings.Contains(got, "\x1b[8;1H\x1b[K") || strings.Contains(got, "\x1b[9;1H") {
		t.Errorf("rows beyond the frame: %q", got)
	}
	for _, line := range strings.Split(strings.TrimPrefix(got, "\x1b[?1049h\x1b[?25l\x1b[H\x1b[2J"), "\x1b[K") {
		if w := display.VisibleWidth(line); w > 40 {
			t.Errorf("line wider than the terminal (%d): %q", w, line)
		}
	}
	// The context's end leaves the screen the same way, with no key.
	out.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, opts, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "\x1b[0m\x1b[?25h\x1b[?1049l") {
		t.Errorf("leave on context: %q", out.String())
	}
}
