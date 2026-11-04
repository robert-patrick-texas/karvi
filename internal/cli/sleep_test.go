package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestSleepIsASourceInTheOrder: --sleep is a command source where it is
// written, between --cmd and --cf alike (EXAMPLES chapter 95); a sleep is
// waited before the next command, sleeps in a row adding up, and the list
// carries none when no command has one.
func TestSleepIsASourceInTheOrder(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "post.txt")
	if err := os.WriteFile(file, []byte("show ip bgp summary\n  --sleep 30s  \n--sleep=15s\nshow ip route summary\n--sleepy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inv := mustParse(t, "run", "--target", "r1", "--cmd", "clear ip bgp 192.0.2.1 soft", "--sleep", "1m", "--sleep=30s", "--cf", file, "--cmd", "show clock", "--timeout", "10s")
	want := []CommandSource{{Value: "clear ip bgp 192.0.2.1 soft"}, {Sleep: true, Value: "1m"}, {Sleep: true, Value: "30s"}, {File: true, Value: file}, {Value: "show clock"}}
	if !reflect.DeepEqual(inv.Sources, want) {
		t.Fatalf("sources %+v", inv.Sources)
	}
	var stderr bytes.Buffer
	set, code, ok := commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr})
	if !ok {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if want := []string{"clear ip bgp 192.0.2.1 soft", "show ip bgp summary", "show ip route summary", "--sleepy", "show clock"}; !reflect.DeepEqual(set.commands, want) {
		t.Fatalf("commands %q, want %q", set.commands, want)
	}
	s := func(d time.Duration) int64 { return int64(d) }
	if want := []int64{0, s(90 * time.Second), s(45 * time.Second), 0, 0}; !reflect.DeepEqual(set.decl.sleeps, want) {
		t.Fatalf("sleeps %v, want %v", set.decl.sleeps, want)
	}
	if len(set.decl.timeouts) != 5 || set.decl.timeouts[4] != s(10*time.Second) {
		t.Fatalf("the --timeout belongs to show clock, the fifth command: %v", set.decl.timeouts)
	}
	// A leading sleep waits before the first command.
	inv = mustParse(t, "command", "r1", "--sleep", "5s", "--cmd", "show clock")
	if set, _, ok = commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr}); !ok || !reflect.DeepEqual(set.decl.sleeps, []int64{s(5 * time.Second)}) {
		t.Fatalf("leading sleep: %v", set.decl.sleeps)
	}
	// No sleep: the list is empty, as the other declaration lists are.
	inv = mustParse(t, "command", "r1", "--cmd", "show clock", "--cmd", "show users")
	if set, _, ok = commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr}); !ok || set.decl.sleeps == nil || len(set.decl.sleeps) != 0 {
		t.Fatalf("no sleep: %#v", set.decl.sleeps)
	}
	// --literal sends a file's --sleep line as written; --sleep on the
	// command line is still a sleep.
	inv = mustParse(t, "command", "r1", "--literal", "--cmd", "show clock", "--sleep", "2s", "--cf", file)
	if set, _, ok = commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr}); !ok {
		t.Fatalf("literal: %s", stderr.String())
	}
	if want := []string{"show clock", "show ip bgp summary", "  --sleep 30s  ", "--sleep=15s", "show ip route summary", "--sleepy"}; !reflect.DeepEqual(set.commands, want) || !reflect.DeepEqual(set.decl.sleeps, []int64{0, s(2 * time.Second), 0, 0, 0, 0}) {
		t.Fatalf("literal: commands %q sleeps %v", set.commands, set.decl.sleeps)
	}
}

// TestSleepRefusals: the range, the placement, and the file line's form are
// each refused before any connection, naming what to write (EXAMPLES
// chapter 95).
func TestSleepRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code string
		text string
	}{
		{[]string{"command", "r1", "--cmd", "a", "--sleep", "0s", "--cmd", "b"}, "cli_option_value_invalid", `--sleep takes a duration of 1s..12h (30s, 5m, 1h30m), not "0s"`},
		{[]string{"command", "r1", "--cmd", "a", "--sleep", "500ms", "--cmd", "b"}, "cli_option_value_invalid", "not \"500ms\""},
		{[]string{"run", "--target", "r1", "--cmd", "a", "--sleep", "13h", "--cmd", "b"}, "cli_option_value_invalid", "not \"13h\""},
		{[]string{"run", "--target", "r1", "--cmd", "a", "--sleep", "soon", "--cmd", "b"}, "cli_option_value_invalid", ""},
		{[]string{"run", "--target", "r1", "--cmd", "a", "--sleep", "5s", "--timeout", "10s", "--cmd", "b"}, "declaration_after_sleep", "--timeout follows --sleep; write it right after its --cmd, before the --sleep"},
		{[]string{"command", "r1", "--sleep", "5s", "show", "clock"}, "cli_command_text_mixed", `freeform text "show clock" cannot be combined with --cmd, --cf, or --sleep`},
	} {
		_, err := Parse(tc.args)
		if errorcodes.Of(err) != tc.code || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%q: %v, want %s containing %q", tc.args, err, tc.code, tc.text)
		}
	}
	dir := t.TempDir()
	for name, content := range map[string]string{"bare": "show clock\n--sleep\n", "words": "show clock\n--sleep 30s show users\n", "short": "show clock\n--sleep=0s\n", "last": "show clock\n--sleep 30s\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		args []string
		code string
		text string
	}{
		{[]string{"command", "r1", "--cmd", "show clock", "--sleep", "30s"}, "sleep_without_command", "a --sleep of 30s comes after the last command"},
		{[]string{"command", "r1", "--sleep", "30s"}, "sleep_without_command", ""},
		{[]string{"command", "r1", "--cf", filepath.Join(dir, "last")}, "sleep_without_command", "a --sleep of 30s"},
		{[]string{"command", "r1", "--cf", filepath.Join(dir, "bare"), "--cmd", "show users"}, "commands_file_sleep_invalid", "commands file " + filepath.Join(dir, "bare") + " line 2: --sleep takes a duration of 1s..12h (30s, 5m, 1h30m), not \"\"; the line is --sleep DURATION alone"},
		{[]string{"command", "r1", "--cf", filepath.Join(dir, "words"), "--cmd", "show users"}, "commands_file_sleep_invalid", `not "30s show users"`},
		{[]string{"command", "r1", "--cf", filepath.Join(dir, "short"), "--cmd", "show users"}, "commands_file_sleep_invalid", `not "0s"`},
	} {
		inv := mustParse(t, tc.args...)
		var stderr bytes.Buffer
		if _, _, ok := commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr}); ok || !strings.HasPrefix(stderr.String(), tc.code+": ") || !strings.Contains(stderr.String(), tc.text) {
			t.Errorf("%q: %q, want %s containing %q", tc.args, stderr.String(), tc.code, tc.text)
		}
	}
}

// TestSleepInAFolderList: a device's own file carries its sleeps into its
// list, and a device whose file ends in one is refused by name at planning.
func TestSleepInAFolderList(t *testing.T) {
	lists := filepath.Join(t.TempDir(), "lists")
	if err := os.MkdirAll(lists, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"core-nyc-01": "clear counters\n--sleep 20s\nshow interfaces counters\n", "core-nyc-02": "show version\n--sleep 20s\n"} {
		if err := os.WriteFile(filepath.Join(lists, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inv := mustParse(t, "run", "--target", "r1", "--cmd", "show clock", "--sleep", "5s", "--cf", lists)
	var stderr bytes.Buffer
	set, code, ok := commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr})
	if !ok || set.perTarget == nil {
		t.Fatalf("exit %d %s", code, stderr.String())
	}
	one, err := set.perTarget("core-nyc-01")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"show clock", "clear counters", "show interfaces counters"}; !reflect.DeepEqual(one.Commands, want) || !reflect.DeepEqual(one.SleepsNS, []int64{0, int64(5 * time.Second), int64(20 * time.Second)}) {
		t.Fatalf("core-nyc-01 %q sleeps %v", one.Commands, one.SleepsNS)
	}
	if _, err := set.perTarget("core-nyc-02"); errorcodes.Of(err) != "sleep_without_command" {
		t.Fatalf("a file ending in a sleep: %v", err)
	}
}

// TestStreamKeepsASleepLine: in stream mode a --sleep line is command
// material in the order typed, kept as --cmd's option form is.
func TestStreamKeepsASleepLine(t *testing.T) {
	args, role, stdinOption, detached := streamOptionArgs("--sleep 30s")
	if !reflect.DeepEqual(args, []string{"--sleep", "30s"}) || role != roleSleep || stdinOption != "" || detached != nil || !streamCommandRole(role) {
		t.Fatalf("args %q role %v stdin %q detached %v", args, role, stdinOption, detached)
	}
}
