package display

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTimestampExamples(t *testing.T) {
	value := time.Date(2026, 9, 10, 18, 7, 6, 123000000, time.FixedZone("EDT", -4*60*60))
	tests := map[string]string{
		"hh:mm:ss yyyy-mm-dd":       "18:07:06 2026-09-10",
		"dd/mm/yyyy hh:mm:ss.sss":   "10/09/2026 18:07:06.123",
		"yyyy mm dd - hh:mm:ss.sss": "2026 09 10 - 18:07:06.123",
		"YYYY-MM-DD HH:mm:ss.SSS":   "2026-09-10 18:07:06.123",
	}
	for pattern, want := range tests {
		formatter, err := NewFormatter(pattern, "America/New_York")
		if err != nil {
			t.Fatalf("%q: %v", pattern, err)
		}
		if got := formatter.Timestamp(value); got != want {
			t.Fatalf("%q = %q, want %q", pattern, got, want)
		}
	}
}

func TestThemeRoleColors(t *testing.T) {
	if got := RoleColor("dark", "accent", "default"); got != "cyan" {
		t.Fatalf("dark accent=%q, want cyan", got)
	}
	if got := RoleColor("light", "warning", "default"); got != "orange" {
		t.Fatalf("light warning=%q, want orange", got)
	}
	if got := RoleColor("dark", "warning", "blue"); got != "blue" {
		t.Fatalf("explicit override=%q, want blue", got)
	}
	old := os.Getenv("COLORFGBG")
	t.Cleanup(func() { _ = os.Setenv("COLORFGBG", old) })
	if err := os.Setenv("COLORFGBG", "0;15"); err != nil {
		t.Fatal(err)
	}
	if got := EffectiveTheme("auto"); got != "light" {
		t.Fatalf("auto theme=%q, want light", got)
	}
}

func TestLineTemplate(t *testing.T) {
	formatter, err := NewFormatter(DefaultTimestampPattern, "auto")
	if err != nil {
		t.Fatal(err)
	}
	got, err := formatter.RenderLine("<target> [<address>] user=<user>", Values{Target: "r1", Address: "192.0.2.1", User: "netops"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "r1 [192.0.2.1] user=netops" {
		t.Fatalf("got %q", got)
	}
	if err := ValidateLineTemplate("<secret>"); err == nil {
		t.Fatal("expected unknown placeholder failure")
	}
	if err := ValidateLineTemplate("one\ntwo"); err == nil {
		t.Fatal("expected multiline failure")
	}
}

func TestBorderRepeat(t *testing.T) {
	got, err := RenderBorder("<repeat:-:40>\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.Repeat("-", 40)+"\n" {
		t.Fatalf("got %q", got)
	}
	if _, err := RenderBorder("<repeat:-:0>"); err == nil {
		t.Fatal("expected invalid count")
	}
}

func TestStyledLineUsesDistinctSemanticColors(t *testing.T) {
	formatter, err := NewFormatter(DefaultTimestampPattern, "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	style := LineStyle{
		Enabled: true,
		Target:  "cyan", Address: "yellow", Label: "gray", Value: "white",
		Accent: "blue", Success: "green", Warning: "magenta", Error: "red", Muted: "gray",
	}
	got, err := formatter.RenderStyledLine("<target> [<address>] platform=<platform> user=<user>", Values{
		Target: "router1", Address: "192.0.2.10", Platform: "cisco_iosxe", User: "svc.operator",
	}, style)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"\x1b[1;36mrouter1\x1b[0m",
		"\x1b[90m [\x1b[0m",
		"\x1b[1;33m192.0.2.10\x1b[0m",
		"\x1b[90m] platform=\x1b[0m",
		"\x1b[37mcisco_iosxe\x1b[0m",
		"\x1b[90m user=\x1b[0m",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("styled line missing %q: %q", want, got)
		}
	}
	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(got, "")
	if plain != "router1 [192.0.2.10] platform=cisco_iosxe user=svc.operator" {
		t.Fatalf("plain projection = %q", plain)
	}
}

func TestStyledLineDisabledMatchesPlainLine(t *testing.T) {
	formatter, err := NewFormatter(DefaultTimestampPattern, "auto")
	if err != nil {
		t.Fatal(err)
	}
	values := Values{Target: "r1", Address: "192.0.2.1", Platform: "generic"}
	plain, err := formatter.RenderLine("<target> [<address>] platform=<platform>", values)
	if err != nil {
		t.Fatal(err)
	}
	styled, err := formatter.RenderStyledLine("<target> [<address>] platform=<platform>", values, LineStyle{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if styled != plain {
		t.Fatalf("disabled styled line = %q, want %q", styled, plain)
	}
}

func TestDarkThemeIdentityAndTimestampRoles(t *testing.T) {
	if got := RoleColor("dark", "target", "default"); got != "yellow" {
		t.Fatalf("dark target=%q, want yellow", got)
	}
	if got := RoleColor("dark", "address", "default"); got != "magenta" {
		t.Fatalf("dark address=%q, want magenta", got)
	}
	if got := RoleColor("dark", "label", "default"); got != "blue" {
		t.Fatalf("dark label=%q, want blue", got)
	}
	if got := RoleColor("light", "address", "default"); got != "magenta" {
		t.Fatalf("light address=%q, want magenta", got)
	}
	formatter, err := NewFormatter("hh:mm:ss", "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	line, err := formatter.RenderStyledLine("<timestamp> <reference-id>", Values{
		Timestamp:   time.Date(2026, 9, 10, 12, 34, 56, 0, time.FixedZone("EDT", -4*60*60)),
		ReferenceID: "abc123",
	}, LineStyle{Enabled: true, Timestamp: "magenta", Accent: "blue"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "\x1b[35m12:34:56\x1b[0m") {
		t.Fatalf("timestamp did not use independent timestamp role: %q", line)
	}
	if !strings.Contains(line, "\x1b[34mabc123\x1b[0m") {
		t.Fatalf("reference did not retain accent role: %q", line)
	}
}

func TestRenderLinesMovesArtifactsToSecondLineFirst(t *testing.T) {
	formatter, err := NewFormatter(DefaultTimestampPattern, "auto")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := formatter.RenderLines(
		"exit=<exit-code> elapsed=<elapsed> artifacts=<artifacts>",
		Values{ExitCode: 101, Elapsed: 3 * time.Second, Artifacts: "/home/operator/.local/share/karvi/jobs/a-very-long-id"},
		36,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines=%#v, want two", lines)
	}
	if lines[0] != "exit=101 elapsed=3s" {
		t.Fatalf("first line=%q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "artifacts=") {
		t.Fatalf("second line=%q, want artifacts element", lines[1])
	}
}

func TestRenderLinesSplitsAtElementBoundary(t *testing.T) {
	formatter, err := NewFormatter(DefaultTimestampPattern, "auto")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := formatter.RenderLines(
		"<target> [<address>] platform=<platform> user=<user>",
		Values{Target: "r1", Address: "192.0.2.10", Platform: "cisco_iosxe", User: "svc.operator"},
		31,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines=%#v, want two", lines)
	}
	if strings.Contains(lines[0], "\n") || strings.Contains(lines[1], "\n") {
		t.Fatalf("unexpected embedded newline: %#v", lines)
	}
	if strings.Contains(strings.Join(lines, "|"), "svc.|operator") {
		t.Fatalf("value was split: %#v", lines)
	}
}

func TestVisibleWidthIgnoresANSIAndCropLinesOnlyCropsContent(t *testing.T) {
	colored := "\x1b[1;33mrouter1\x1b[0m"
	if got := VisibleWidth(colored); got != len("router1") {
		t.Fatalf("visible width=%d, want %d", got, len("router1"))
	}
	if got := CropLines("----------\nshort\n", 6); got != "------\nshort\n" {
		t.Fatalf("cropped=%q", got)
	}
}

// TestCropLinesKeepsEscapesWhole: a cut through a coloured cell keeps the
// escape sequence whole at zero width and closes the colour with a reset;
// a cut before the colour opens carries no reset; a line with no escape is
// cut as before (the watch screen fits its frame this way).
func TestCropLinesKeepsEscapesWhole(t *testing.T) {
	line := "ab \x1b[36mrunning\x1b[0m cd"
	cases := map[int]string{
		6:  "ab \x1b[36mrun\x1b[0m",
		2:  "ab",
		13: "ab \x1b[36mrunning\x1b[0m cd",
		20: line,
	}
	for width, want := range cases {
		if got := CropLines(line, width); got != want {
			t.Errorf("CropLines(%q, %d) = %q, want %q", line, width, got, want)
		}
	}
	if got := CropLines("plain text", 5); got != "plain" {
		t.Errorf("plain %q", got)
	}
}

// TestColorEnabledNoColor: under auto, colour is on at a terminal unless
// NO_COLOR is set and not empty; always and never are explicit choices
// NO_COLOR does not override; the nocolor theme is off whatever the mode.
func TestColorEnabledNoColor(t *testing.T) {
	for _, tc := range []struct {
		mode, theme, noColor string
		terminal, want       bool
	}{
		{"auto", "dark", "", true, true},
		{"auto", "dark", "1", true, false},
		{"auto", "dark", "", false, false},
		{"always", "dark", "1", false, true},
		{"never", "dark", "", true, false},
		{"always", "nocolor", "", true, false},
	} {
		t.Setenv("NO_COLOR", tc.noColor)
		if got := ColorEnabled(tc.mode, tc.theme, tc.terminal); got != tc.want {
			t.Errorf("ColorEnabled(%s, %s, terminal=%v) with NO_COLOR=%q = %v, want %v", tc.mode, tc.theme, tc.terminal, tc.noColor, got, tc.want)
		}
	}
}
