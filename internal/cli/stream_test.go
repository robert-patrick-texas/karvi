package cli

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// TestStreamLoopDraftsAndDirectives is stream mode's line grammar:
// comments and blank lines skipped; an option
// line as the word and the rest of the line; a command line as written,
// with its declarations after it; a line the parser refuses reported by
// its number and dropped (--typo, a declaration before any command);
// --cf - refused; --go executing the draft and clearing the commands
// alone; --sendit the same; --reset clearing everything; --end leaving
// without executing what follows or what is drafted; the exit the last
// job's.
func TestStreamLoopDraftsAndDirectives(t *testing.T) {
	var runs [][]string
	execute := func(argv []string) int {
		runs = append(runs, append([]string{}, argv...))
		return exitcode.ExitPartialFailure
	}
	in := strings.NewReader(strings.Join([]string{
		"# a comment",
		"! another",
		"",
		"--target router1",
		"--tl r2,r3",
		"--dispatch parallel",
		"--typo",
		"--expect a=b",
		"show clock",
		"--expect confirm=y",
		`  show version\r`,
		`\r`,
		"--cf -",
		"--go",
		"show ip route",
		"--sendit",
		"--reset",
		"--target r9",
		"--end",
		"--target never",
		"show never",
		"--go",
	}, "\n") + "\n")
	var stderr bytes.Buffer
	got := streamLoop(context.Background(), in, &stderr, execute)
	if got != exitcode.ExitPartialFailure {
		t.Errorf("exit %d, want the last job's %d", got, exitcode.ExitPartialFailure)
	}
	want := [][]string{
		{"run", "--target", "router1", "--tl", "r2,r3", "--dispatch", "parallel", "--cmd", "show clock", "--expect", "confirm=y", "--cmd", `  show version\r`, "--cmd", `\r`},
		{"run", "--target", "router1", "--tl", "r2,r3", "--dispatch", "parallel", "--cmd", "show ip route"},
	}
	if !reflect.DeepEqual(runs, want) {
		t.Errorf("runs:\n%q\nwant:\n%q", runs, want)
	}
	for _, m := range []string{"stream line 7 dropped: ", "stream line 8 dropped: ", "stream line 13: standard input is the stream; --cf - is not accepted"} {
		if !strings.Contains(stderr.String(), m) {
			t.Errorf("stderr lacks %q:\n%s", m, stderr.String())
		}
	}
	if strings.Count(stderr.String(), "\n") != 3 {
		t.Errorf("stderr has lines beyond the three reports:\n%s", stderr.String())
	}
}

// TestStreamLoopEndsOnEOFAndCancel: the input's end leaves without
// executing the draft, exit 0 when no job ran; a cancelled context
// (Ctrl-C) leaves while the reader still waits on the input.
func TestStreamLoopEndsOnEOFAndCancel(t *testing.T) {
	ran := 0
	execute := func([]string) int { ran++; return 0 }
	var stderr bytes.Buffer
	if got := streamLoop(context.Background(), strings.NewReader("--target r1\nshow clock\n"), &stderr, execute); got != 0 || ran != 0 {
		t.Errorf("EOF: exit %d, ran %d", got, ran)
	}
	r, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := streamLoop(ctx, r, &stderr, execute); got != 0 || ran != 0 {
		t.Errorf("cancel: exit %d, ran %d", got, ran)
	}
}

// TestStreamOptionArgs: the word alone, word=value, and word then the rest
// of the line as one value; the three declarations recognised by their
// prefixes.
func TestStreamOptionArgs(t *testing.T) {
	for _, tc := range []struct {
		line        string
		args        []string
		declaration bool
	}{
		{"--dp", []string{"--dp"}, false},
		{"--target=router1", []string{"--target=router1"}, false},
		{"--target router1", []string{"--target", "router1"}, false},
		{"--tl   r1 r2  ", []string{"--tl", "r1 r2"}, false},
		{"--expect confirm=y", []string{"--expect", "confirm=y"}, true},
		{"--blind", []string{"--blind"}, true},
		{"--blind-return 2", []string{"--blind-return", "2"}, true},
		{"--exp a=b", []string{"--exp", "a=b"}, true},
	} {
		args, declaration := streamOptionArgs(tc.line)
		if !reflect.DeepEqual(args, tc.args) || declaration != tc.declaration {
			t.Errorf("%q: %q %v, want %q %v", tc.line, args, declaration, tc.args, tc.declaration)
		}
	}
}

// TestStreamRunsAJobFromStdin: karvi stream through Main against the test
// runtime (the fake device fails every session): the lines make a run,
// --go executes it, the footer ends its display, the exit is the job's.
func TestStreamRunsAJobFromStdin(t *testing.T) {
	_, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	in := "--target 127.0.0.1\n--transport system\nshow clock\n--go\n--end\n"
	var stdout, stderr bytes.Buffer
	got := Main(append(append([]string{}, sets...), "stream"), strings.NewReader(in), &stdout, &stderr)
	if got != exitcode.ExitPartialFailure {
		t.Fatalf("exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "! exit=101 ") || strings.Contains(stderr.String(), "dropped") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
