package watchui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/display"
)

// listRows are a list's jobs: a folder without a summary, an unfinished
// run, an unreadable folder, a run of two devices with one failed, an
// exercise, and a command.
func listRows() []ListRow {
	return []ListRow{
		{JobID: "261016-020953-00", Operator: "netops", Status: "unfinished", NoSummary: true},
		{JobID: "261016-003707-00", Operator: "netops", Kind: "run", Status: "unfinished", Total: 2, Devices: []string{"fake-iosxe", "dead-iosxe"}},
		{JobID: "261016-003305-00", Invalid: true, Path: "/jobs/261016/261016-003305-00/summary.json", Operator: "netops"},
		{JobID: "261016-002853-00", Operator: "netops", Kind: "run", Status: "errored", Finished: true, Duration: 4 * time.Second, Completed: 2, Total: 2, Fails: 1, Devices: []string{"fake-iosxe", "dead-iosxe"}},
		{JobID: "261015-235308-00", Operator: "netops.test", Kind: "crun", Status: "exercised", Finished: true, Completed: 1, Total: 1, Devices: []string{"fake-iosxe"}},
		{JobID: "261015-235303-00", Operator: "1001", Kind: "cmd", Status: "completed", Finished: true, Duration: 75 * time.Minute, Completed: 1, Total: 1, Devices: []string{"fake-iosxe"}},
	}
}

// TestListTableLayout is the list to a pipe: watch's headings without ACTV
// and with MODE in four cells, OPERATOR as wide as the longest name, DONE's
// O over every row's slash, an unfinished job's u over its total and u in
// FAIL with TIME blank, a folder without a summary's u in MODE and over u,
// an unreadable folder's ? row with its path, TIME a
// duration, and every name whole (EXAMPLES chapter 61, issue 3).
func TestListTableLayout(t *testing.T) {
	lines := ListTable{Rows: listRows()}.Lines()
	want := []string{
		"  JOB-ID            TIME      STATUS      OPERATOR     MODE    DONE   FAIL  TARGET",
		"  261016-020953-00            unfinished  netops       u       u/u       u",
		"  261016-003707-00            unfinished  netops       run     u/2       u  fake-iosxe dead-iosxe",
		"? 261016-003305-00  -         invalid     netops       -        -        -  /jobs/261016/261016-003305-00/summary.json",
		"  261016-002853-00  0m04s     errored     netops       run     2/2       1  fake-iosxe dead-iosxe",
		"  261015-235308-00  0m00s     exercised   netops.test  crun    1/1       0  fake-iosxe",
		"  261015-235303-00  1h15m     completed   1001         cmd     1/1       0  fake-iosxe",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	o := strings.Index(lines[0], "DONE") + 1
	for _, row := range lines[1:] {
		if row[0] == '?' {
			if row[o] != '-' {
				t.Errorf("the ? row's - not under DONE's O: %q", row)
			}
			continue
		}
		if row[o] != '/' {
			t.Errorf("DONE's O not over the slash:\n%q\n%q", lines[0], row)
		}
	}
}

// TestListTableTargetAtWidth is chapter 61's sketch of a job of twelve
// devices: at 80 columns TARGET is the 7 cells the fixed columns leave, +12;
// at 120 five names and +7; at 160 nine and +3; to a pipe all twelve. A name
// is never cut, and every line fits; an unreadable folder's path is whole or
// ….
func TestListTableTargetAtWidth(t *testing.T) {
	var names []string
	for i := 1; i <= 12; i++ {
		names = append(names, fmt.Sprintf("wide-%03d", i))
	}
	row := ListRow{JobID: "261015-151448-00", Operator: "netops", Kind: "run", Status: "completed", Finished: true, Duration: 3 * time.Second, Completed: 12, Total: 12, Devices: names}
	for _, c := range []struct {
		width int
		want  string
	}{
		{80, "+12"},
		{120, "wide-001 wide-002 wide-003 wide-004 wide-005 +7"},
		{160, "wide-001 wide-002 wide-003 wide-004 wide-005 wide-006 wide-007 wide-008 wide-009 +3"},
		{0, strings.Join(names, " ")},
		{40, "+12"},
	} {
		lines := ListTable{Rows: []ListRow{row}, Width: c.width}.Lines()
		if got := lines[1][73:]; got != c.want {
			t.Errorf("width %d: TARGET %q, want %q", c.width, got, c.want)
		}
		for _, line := range lines {
			if c.width >= 80 && len(line) > c.width {
				t.Errorf("width %d: line of %d: %q", c.width, len(line), line)
			}
		}
	}
	// An unreadable folder's path: whole where it fits, … where it does
	// not, at a terminal; whole to a pipe; under --target alike.
	bad := ListRow{JobID: "261016-003305-00", Invalid: true, Path: "/opt/karvi/shared/jobs/261016/261016-003305-00/summary.json", Operator: "netops"}
	for width, want := range map[int]string{0: bad.Path, 80: "…", 131: "…", 132: bad.Path} {
		if got := (ListTable{Rows: []ListRow{bad}, Width: width}).Lines()[1][73:]; got != want {
			t.Errorf("? row at %d: TARGET %q, want %q", width, got, want)
		}
		if got := (ListTable{Rows: []ListRow{bad}, Width: width - 14, ByDevice: true}).Lines()[1][59:]; width > 0 && got != want {
			t.Errorf("? device row at %d: DEVICE %q, want %q", width-14, got, want)
		}
	}
	// Two names of ten cells: neither fits the 7 cells at 80, both at 100.
	two := ListRow{JobID: "261016-002853-00", Operator: "netops", Kind: "run", Status: "errored", Finished: true, Completed: 2, Total: 2, Fails: 1, Devices: []string{"fake-iosxe", "dead-iosxe"}}
	for width, want := range map[int]string{80: "+2", 85: "+2", 86: "fake-iosxe +1", 100: "fake-iosxe dead-iosxe"} {
		if got := (ListTable{Rows: []ListRow{two}, Width: width}).Lines()[1][73:]; got != want {
			t.Errorf("width %d: TARGET %q, want %q", width, got, want)
		}
	}
}

// TestListTableByDevice is the list under --target: a row per device, its
// OUTCOME in the place of STATUS, DONE, and FAIL, 59 cells before DEVICE;
// an unfinished job's device unfinished, an unreadable folder's row invalid
// with … as its DEVICE, its path not fitting the 21 cells left at 80.
func TestListTableByDevice(t *testing.T) {
	rows := []ListRow{
		{JobID: "261016-003707-00", Operator: "netops", Kind: "run", Status: "unfinished", Device: "dead-iosxe", Outcome: "unfinished"},
		{JobID: "261016-003305-00", Invalid: true, Path: "/jobs/261016/261016-003305-00/summary.json", Operator: "netops"},
		{JobID: "261016-002853-00", Operator: "netops", Kind: "run", Status: "errored", Finished: true, Duration: 4 * time.Second, Device: "dead-iosxe", Outcome: "failed"},
		{JobID: "261016-002853-00", Operator: "netops", Kind: "run", Status: "errored", Finished: true, Duration: 4 * time.Second, Device: "fake-iosxe", Outcome: "not_started"},
	}
	lines := ListTable{Rows: rows, ByDevice: true, Width: 80}.Lines()
	want := []string{
		"  JOB-ID            TIME      OPERATOR  MODE  OUTCOME      DEVICE",
		"  261016-003707-00            netops    run   unfinished   dead-iosxe",
		"? 261016-003305-00  -         netops    -     invalid      …",
		"  261016-002853-00  0m04s     netops    run   failed       dead-iosxe",
		"  261016-002853-00  0m04s     netops    run   not_started  fake-iosxe",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if strings.Index(lines[0], "DEVICE") != 59 {
		t.Errorf("DEVICE at %d, want 59", strings.Index(lines[0], "DEVICE"))
	}
}

// TestListTableColours: with colour on, the headings bold label, the
// status by its role (unfinished warning, invalid error), an unfinished
// job's two u in warning, a folder without a summary's every u, FAIL error above zero and muted at zero, x accent
// and y value, TARGET target, a device's outcome by its role; and the plain
// text the same as without colour.
func TestListTableColours(t *testing.T) {
	plain := ListTable{Rows: listRows()}.Lines()
	lines := ListTable{Rows: listRows(), Colour: true, Theme: "dark"}.Lines()
	for i := range plain {
		if display.StripANSI(lines[i]) != plain[i] {
			t.Errorf("line %d differs stripped:\n%q\n%q", i, display.StripANSI(lines[i]), plain[i])
		}
	}
	style := func(text, role string, bold bool) string {
		return display.ANSIStyle(text, display.RoleColor("dark", role, ""), true, bold)
	}
	for i, wants := range map[int][]string{
		0: {style("JOB-ID          ", "label", true), style("TARGET", "label", true)},
		1: {style("u   ", "warning", false), style("  u", "warning", false) + style("/", "value", false) + style("u  ", "warning", false), style("   u", "warning", false)},
		2: {style("unfinished", "warning", false), style("  u", "warning", false) + style("/", "value", false) + style("2  ", "value", false), style("   u", "warning", false), style("fake-iosxe dead-iosxe", "target", false)},
		3: {style("? ", "error", false), style("invalid   ", "error", false), style("/jobs/261016/261016-003305-00/summary.json", "muted", false)},
		4: {style("errored   ", "error", false), style("  2", "accent", false) + style("/", "value", false), style("   1", "error", false)},
		5: {style("exercised ", "success", false), style("   0", "muted", false)},
	} {
		for _, want := range wants {
			if !strings.Contains(lines[i], want) {
				t.Errorf("line %d lacks %q: %q", i, want, lines[i])
			}
		}
	}
	device := ListTable{Rows: []ListRow{{JobID: "261016-003707-00", Operator: "netops", Kind: "run", Status: "unfinished", Device: "dead-iosxe", Outcome: "unfinished"}, {JobID: "261016-002853-00", Operator: "netops", Kind: "run", Finished: true, Device: "dead-iosxe", Outcome: "failed"}, {JobID: "261016-002853-00", Operator: "netops", Kind: "run", Finished: true, Device: "fake-iosxe", Outcome: "succeeded"}}, ByDevice: true, Colour: true, Theme: "dark"}.Lines()
	for i, want := range map[int]string{1: style("unfinished ", "warning", false), 2: style("failed     ", "error", false), 3: style("succeeded  ", "success", false)} {
		if !strings.Contains(device[i], want) {
			t.Errorf("device line %d lacks %q: %q", i, want, device[i])
		}
	}
}
