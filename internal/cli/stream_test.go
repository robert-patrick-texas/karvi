package cli

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/termline"
)

// TestStreamLoopDraftsAndDirectives is stream mode's line grammar:
// comments and blank lines skipped; an option
// line as the word and the rest of the line; a command line as written,
// with its declarations after it; a line the parser refuses reported by
// its number and dropped (--typo, a declaration before any command);
// --cf - refused; --of with its path after a space refused and dropped, a
// bare --cd dropped when read, --cd=PATH an option that stays;
// --go executing the draft and clearing the commands
// alone; --sendit the same; --reset clearing everything; --end leaving
// without executing what follows or what is drafted; the exit the last
// job's.
func TestStreamLoopDraftsAndDirectives(t *testing.T) {
	var runs [][]string
	execute := func(_ int, argv []string) int {
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
		"--of /tmp/x",
		"--cd",
		"--cd=/tmp/c",
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
	got := streamLoop(context.Background(), streamScanner(in), &stderr, execute)
	if got != exitcode.ExitPartialFailure {
		t.Errorf("exit %d, want the last job's %d", got, exitcode.ExitPartialFailure)
	}
	want := [][]string{
		{"run", "--target", "router1", "--tl", "r2,r3", "--dispatch", "parallel", "--cd=/tmp/c", "--cmd", "show clock", "--expect", "confirm=y", "--cmd", `  show version\r`, "--cmd", `\r`},
		{"run", "--target", "router1", "--tl", "r2,r3", "--dispatch", "parallel", "--cd=/tmp/c", "--cmd", "show ip route"},
	}
	if !reflect.DeepEqual(runs, want) {
		t.Errorf("runs:\n%q\nwant:\n%q", runs, want)
	}
	for _, m := range []string{"stream line 7 dropped: ", "stream line 8 dropped: ", "stream line 13: standard input is the stream; --cf - is not accepted", "stream line 14 dropped: cli_option_value_detached: --of takes its PATH with =: --of=/tmp/x", "stream line 15 dropped: cli_option_value_missing: --cd takes its PATH with =: --cd=PATH"} {
		if !strings.Contains(stderr.String(), m) {
			t.Errorf("stderr lacks %q:\n%s", m, stderr.String())
		}
	}
	if strings.Count(stderr.String(), "\n") != 5 {
		t.Errorf("stderr has lines beyond the five reports:\n%s", stderr.String())
	}
}

// TestStreamLoopEndsOnEOFAndCancel: the input's end leaves without
// executing the draft, exit 0 when no job ran; a cancelled context
// (Ctrl-C) leaves while the reader still waits on the input.
func TestStreamLoopEndsOnEOFAndCancel(t *testing.T) {
	ran := 0
	execute := func(int, []string) int { ran++; return 0 }
	var stderr bytes.Buffer
	if got := streamLoop(context.Background(), streamScanner(strings.NewReader("--target r1\nshow clock\n")), &stderr, execute); got != 0 || ran != 0 {
		t.Errorf("EOF: exit %d, ran %d", got, ran)
	}
	r, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := streamLoop(ctx, streamScanner(r), &stderr, execute); got != 0 || ran != 0 {
		t.Errorf("cancel: exit %d, ran %d", got, ran)
	}
	// A cancellation outranks lines already read: a --go behind it never runs.
	if got := streamLoop(ctx, streamScanner(strings.NewReader("--target r1\nshow clock\n--go\n")), &stderr, execute); got != 0 || ran != 0 {
		t.Errorf("cancel with lines buffered: exit %d, ran %d", got, ran)
	}
}

// TestStreamLoopKeptCommandsAndPurges: a command given in option form
// (--cmd and its aliases, --cf) stays in the draft after --go, --sendit, and
// --clear, in its order, with the declarations after it, while a bare line
// and its declarations clear; --purge-commands empties every command and
// --purge-targets every target input, each by a prefix from --purge-c and
// --purge-t, one dash or two; --purge and --purge- are refused as naming
// both; --go with no command left prints a notice; --cf, --tf, and --tfr
// naming - are refused in every spelling; --exit leaves as --end does.
func TestStreamLoopKeptCommandsAndPurges(t *testing.T) {
	var runs [][]string
	execute := func(_ int, argv []string) int {
		runs = append(runs, append([]string{}, argv...))
		return exitcode.ExitPartialFailure
	}
	var stderr bytes.Buffer
	in := strings.Join([]string{
		"--target r1",
		"--tl r2,r3",
		"--dispatch parallel",
		"--cmd show clock",
		"show run  \t",
		"--expect confirm=y",
		"-c reload",
		"--expect confirm=y",
		"--go",
		"show ip route",
		"--sendit",
		"show arp",
		"--clear",
		"--go",
		"--purge-t",
		"--target r4",
		"--purge-commands",
		"--go",
		"--cf=/tmp/commands.txt",
		"-sendit",
		"--purge",
		"-purge-",
		"--purge-co",
		"--go",
		"--tf=-",
		"--cf -",
		"--tfr -",
		"show clock",
		"--purge-targ",
		"--go",
		"--reset",
		"--go",
		"--exit",
		"--target never",
		"--go",
	}, "\n") + "\n"
	if got := streamLoop(context.Background(), streamScanner(strings.NewReader(in)), &stderr, execute); got != exitcode.ExitPartialFailure {
		t.Errorf("exit %d", got)
	}
	targets := []string{"--target", "r1", "--tl", "r2,r3", "--dispatch", "parallel"}
	want := [][]string{
		append(append([]string{"run"}, targets...), "--cmd", "show clock", "--cmd", "show run", "--expect", "confirm=y", "-c", "reload", "--expect", "confirm=y"),
		append(append([]string{"run"}, targets...), "--cmd", "show clock", "-c", "reload", "--expect", "confirm=y", "--cmd", "show ip route"),
		append(append([]string{"run"}, targets...), "--cmd", "show clock", "-c", "reload", "--expect", "confirm=y"),
		{"run", "--dispatch", "parallel", "--target", "r4", "--cf=/tmp/commands.txt"},
		{"run", "--dispatch", "parallel", "--cmd", "show clock"},
	}
	if !reflect.DeepEqual(runs, want) {
		t.Errorf("runs:\n%q\nwant:\n%q", runs, want)
	}
	wantErr := "stream line 18: nothing to send\n" +
		"stream line 21 dropped: cli_option_ambiguous: --purge is ambiguous in stream: --purge-commands, --purge-targets\n" +
		"stream line 22 dropped: cli_option_ambiguous: --purge- is ambiguous in stream: --purge-commands, --purge-targets\n" +
		"stream line 24: nothing to send\n" +
		"stream line 25: standard input is the stream; --tf - is not accepted\n" +
		"stream line 26: standard input is the stream; --cf - is not accepted\n" +
		"stream line 27: standard input is the stream; --tfr - is not accepted\n" +
		"stream line 32: nothing to send\n"
	if stderr.String() != wantErr {
		t.Errorf("stderr:\n%s\nwant:\n%s", stderr.String(), wantErr)
	}
}

// TestStreamLoopSingleDashAndQuotes: a line beginning with one dash is an
// option line as one beginning with two is, the command options and the
// directives among them (-go leaving the option-form commands, -clear
// finding no bare one), and a - alone is a command; a command that begins
// with a dash goes as --cmd's value; an option's value wholly wrapped in one
// pair of quotes loses them, and every other quote is sent as written, a
// bare line's among them; a one-dash word run does not know is dropped, and
// so is a line that would begin run's freeform command text (a - or -- word
// with text after it, -- alone, a flag given text after a space), which
// would otherwise take every command after it.
func TestStreamLoopSingleDashAndQuotes(t *testing.T) {
	var runs [][]string
	execute := func(_ int, argv []string) int {
		runs = append(runs, append([]string{}, argv...))
		return exitcode.ExitSuccess
	}
	var stderr bytes.Buffer
	in := strings.Join([]string{
		"-target r1",
		`--target "r2"`,
		"-c show clock",
		"-cmd show version",
		"-command show ip route",
		"-",
		"--cmd -foo bar",
		"--cmd=-baz",
		`--cmd "show clock"`,
		`--cmd 'show clock'`,
		`-c="show clock"`,
		`"show clock"`,
		`echo "a  b"`,
		`--cmd echo "a  b"`,
		`--cmd "show clock`,
		`--cmd "a" "b"`,
		"- foo",
		"-typo",
		"-- foo",
		"--",
		"--no-daemon yes",
		"-go",
		"-c x",
		"-clear",
		"-go",
		"-reset",
		"-c y",
		"-sendit",
		"-quit",
		"-target never",
		"-go",
	}, "\n") + "\n"
	if got := streamLoop(context.Background(), streamScanner(strings.NewReader(in)), &stderr, execute); got != exitcode.ExitSuccess {
		t.Errorf("exit %d", got)
	}
	want := [][]string{
		{"run", "-target", "r1", "--target", "r2",
			"-c", "show clock", "-cmd", "show version", "-command", "show ip route", "--cmd", "-",
			"--cmd", "-foo bar", "--cmd=-baz", "--cmd", "show clock", "--cmd", "show clock", "-c=show clock",
			"--cmd", `"show clock"`, "--cmd", `echo "a  b"`, "--cmd", `echo "a  b"`, "--cmd", `"show clock`, "--cmd", `"a" "b"`},
		{"run", "-target", "r1", "--target", "r2",
			"-c", "show clock", "-cmd", "show version", "-command", "show ip route",
			"--cmd", "-foo bar", "--cmd=-baz", "--cmd", "show clock", "--cmd", "show clock", "-c=show clock",
			"--cmd", `echo "a  b"`, "--cmd", `"show clock`, "--cmd", `"a" "b"`, "-c", "x"},
		{"run", "-c", "y"},
	}
	if !reflect.DeepEqual(runs, want) {
		t.Errorf("runs:\n%q\nwant:\n%q", runs, want)
	}
	for _, m := range []string{
		`stream line 17 dropped: cli_positional_unexpected: "- foo" leaves text no option takes`,
		"stream line 18 dropped: cli_option_unknown: unknown option -typo in run",
		`stream line 19 dropped: cli_positional_unexpected: "-- foo" leaves text no option takes`,
		`stream line 20 dropped: cli_positional_unexpected: "--" leaves text no option takes`,
		`stream line 21 dropped: cli_positional_unexpected: "--no-daemon yes" leaves text no option takes`,
	} {
		if !strings.Contains(stderr.String(), m) {
			t.Errorf("stderr lacks %q:\n%s", m, stderr.String())
		}
	}
	if strings.Count(stderr.String(), "\n") != 5 {
		t.Errorf("stderr has lines beyond the five reports:\n%s", stderr.String())
	}
}

// TestStreamLoopReadFailure: a line over the limit ends the stream with
// stream_input_read_failed and its exit, naming the line; the lines behind
// it are never read.
func TestStreamLoopReadFailure(t *testing.T) {
	ran := 0
	execute := func(int, []string) int { ran++; return 0 }
	var stderr bytes.Buffer
	in := "--target r1\n" + strings.Repeat("x", streamLineLimit+1) + "\nshow clock\n--go\n"
	got := streamLoop(context.Background(), streamScanner(strings.NewReader(in)), &stderr, execute)
	if got != exitcode.ExitGenericError || ran != 0 {
		t.Errorf("exit %d, ran %d", got, ran)
	}
	if !strings.HasPrefix(stderr.String(), "stream_input_read_failed: stream line 2 is longer than the 1048576-byte limit") {
		t.Errorf("stderr %q", stderr.String())
	}
}

// TestStreamOptionArgs: the word alone, word=value, and word then the rest
// of the line as one value; the command part recognised through run's
// table (the declarations, --cmd and its aliases, --cf, by prefix too); the
// standard-input value named for --cf, --tf, and --tfr in either spelling;
// a word the table does not resolve is an option line for the probe; an
// option whose value attaches with = alone refuses text after a space; one
// dash as two; a value wholly wrapped in one pair of quotes loses them, in
// either spelling and before the standard-input and detached checks, and a
// value with other quotes, or an unmatched or empty pair, keeps them as
// written (an empty pair is an empty value).
func TestStreamOptionArgs(t *testing.T) {
	for _, tc := range []struct {
		line        string
		args        []string
		commandPart bool
		stdinOption string
		detached    string
	}{
		{"--dp", []string{"--dp"}, false, "", ""},
		{"--target=router1", []string{"--target=router1"}, false, "", ""},
		{"--target router1", []string{"--target", "router1"}, false, "", ""},
		{"--tl   r1 r2  ", []string{"--tl", "r1 r2"}, false, "", ""},
		{"--expect confirm=y", []string{"--expect", "confirm=y"}, true, "", ""},
		{"--blind", []string{"--blind"}, true, "", ""},
		{"--blind-return 2", []string{"--blind-return", "2"}, true, "", ""},
		{"--exp a=b", []string{"--exp", "a=b"}, true, "", ""},
		{"--blind-wait 2s", []string{"--blind-wait", "2s"}, false, "", ""},
		{"--cmd show clock", []string{"--cmd", "show clock"}, true, "", ""},
		{"--command=show clock", []string{"--command=show clock"}, true, "", ""},
		{"--c show clock", []string{"--c", "show clock"}, true, "", ""},
		{"--cf /tmp/x", []string{"--cf", "/tmp/x"}, true, "", ""},
		{"--cf -", []string{"--cf", "-"}, true, "--cf", ""},
		{"--cf=-", []string{"--cf=-"}, true, "--cf", ""},
		{"--tf -", []string{"--tf", "-"}, false, "--tf", ""},
		{"--tf=-", []string{"--tf=-"}, false, "--tf", ""},
		{"--tfr -", []string{"--tfr", "-"}, false, "--tfr", ""},
		{"--typo -", []string{"--typo", "-"}, false, "", ""},
		{"--t -", []string{"--t", "-"}, false, "", ""},
		{"--of", []string{"--of"}, false, "", ""},
		{"--of=/tmp/x", []string{"--of=/tmp/x"}, false, "", ""},
		{"--of=/tmp/a b", []string{"--of=/tmp/a b"}, false, "", ""},
		{"--of /tmp/x", []string{"--of", "/tmp/x"}, false, "", "cli_option_value_detached: --of takes its PATH with =: --of=/tmp/x"},
		{"--of out", []string{"--of", "out"}, false, "", "cli_option_value_detached: --of takes its PATH with =: --of=out"},
		{"-c show clock", []string{"-c", "show clock"}, true, "", ""},
		{"-command=show clock", []string{"-command=show clock"}, true, "", ""},
		{"-target r1", []string{"-target", "r1"}, false, "", ""},
		{`--cmd "show clock"`, []string{"--cmd", "show clock"}, true, "", ""},
		{`--cmd 'show clock'`, []string{"--cmd", "show clock"}, true, "", ""},
		{`--cmd="show clock"`, []string{"--cmd=show clock"}, true, "", ""},
		{`--cmd '"x"'`, []string{"--cmd", `"x"`}, true, "", ""},
		{`--cmd ""`, []string{"--cmd", ""}, true, "", ""},
		{`--cmd "`, []string{"--cmd", `"`}, true, "", ""},
		{`--cmd "a" "b"`, []string{"--cmd", `"a" "b"`}, true, "", ""},
		{`--cmd "show clock'`, []string{"--cmd", `"show clock'`}, true, "", ""},
		{`--cmd echo "a b"`, []string{"--cmd", `echo "a b"`}, true, "", ""},
		{`--expect "confirm=y"`, []string{"--expect", "confirm=y"}, true, "", ""},
		{`--expect="a=b"`, []string{"--expect=a=b"}, true, "", ""},
		{`--cf "-"`, []string{"--cf", "-"}, true, "--cf", ""},
		{`--tf='-'`, []string{"--tf=-"}, false, "--tf", ""},
		{`--of "/tmp/x"`, []string{"--of", "/tmp/x"}, false, "", "cli_option_value_detached: --of takes its PATH with =: --of=/tmp/x"},
		{`--of="/tmp/a b"`, []string{"--of=/tmp/a b"}, false, "", ""},
	} {
		args, role, stdinOption, detached := streamOptionArgs(tc.line)
		commandPart := streamCommandRole(role)
		if !reflect.DeepEqual(args, tc.args) || commandPart != tc.commandPart || stdinOption != tc.stdinOption {
			t.Errorf("%q: %q %v %q, want %q %v %q", tc.line, args, commandPart, stdinOption, tc.args, tc.commandPart, tc.stdinOption)
		}
		got := ""
		if detached != nil {
			got = errorcodes.Message(detached)
		}
		if got != tc.detached {
			t.Errorf("%q: detached %q, want %q", tc.line, got, tc.detached)
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

// TestStreamTerminalEditsAndHistory: the terminal reader over an in-memory
// terminal (no raw mode under test): Ctrl-A and Ctrl-E move within the
// line, the up arrow recalls the line before, Ctrl-C ends the stream as
// io.EOF, and the loop takes the reader's lines as it takes the scanner's,
// a line ended by \n (typed ahead through the terminal's own mode) as one
// ended by \r.
func TestStreamTerminalEditsAndHistory(t *testing.T) {
	var echo bytes.Buffer
	in := strings.NewReader("show clock\x01! \x05 detail\r\x1b[A\r\x03")
	s := &streamTerminal{line: termline.FromReader(in, &echo)}
	var got []string
	for {
		line, err := s.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, line)
	}
	if want := []string{"! show clock detail", "! show clock detail"}; !reflect.DeepEqual(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}
	var runs int
	execute := func(int, []string) int { runs++; return 0 }
	var stderr bytes.Buffer
	s = &streamTerminal{line: termline.FromReader(strings.NewReader("--target r1\nshow clock\r--go\n\x04"), io.Discard)}
	if exit := streamLoop(context.Background(), s.next, &stderr, execute); exit != 0 || runs != 1 || stderr.Len() != 0 {
		t.Errorf("exit %d, runs %d, stderr %q", exit, runs, stderr.String())
	}
}

// TestStreamLoopReadsNoLineAhead: the loop asks for a line only when it is
// ready for it, so no read runs beside a job; at a terminal such a read
// would hold the terminal in raw mode through the job, taking its Ctrl-C
// as a key and its credential prompt's answer as the next line.
func TestStreamLoopReadsNoLineAhead(t *testing.T) {
	input := []string{"--target r1", "show clock", "--go", "show version", "--go"}
	var reads int32
	next := func() (string, error) {
		i := int(atomic.AddInt32(&reads, 1)) - 1
		if i >= len(input) {
			return "", io.EOF
		}
		return input[i], nil
	}
	// The reads made when each job starts and when it ends: the job's own
	// lines, and not one more.
	var seen [][2]int32
	execute := func(int, []string) int {
		start := atomic.LoadInt32(&reads)
		time.Sleep(20 * time.Millisecond)
		seen = append(seen, [2]int32{start, atomic.LoadInt32(&reads)})
		return 0
	}
	var stderr bytes.Buffer
	if exit := streamLoop(context.Background(), next, &stderr, execute); exit != 0 {
		t.Fatalf("exit %d: %s", exit, stderr.String())
	}
	if want := [][2]int32{{3, 3}, {5, 5}}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("reads at each job's start and end %v, want %v", seen, want)
	}
}
