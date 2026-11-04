package commandfile

import (
	"strings"
	"testing"
	"time"
)

// read is the reader's half of the grammar, as the command line's --cf
// reads a file without --literal (List.Line): each --sleep line added to
// the sleep before the next command, a --sudo line a root command, each
// other line a command, and then each command's trailing \r sequences
// counted off, as the command line's declarations do.
func read(t *testing.T, text string) (commands []string, returns []int, sleeps []int64, sudo []bool) {
	t.Helper()
	var l List
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if err := l.Line(line, false); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
	}
	for _, c := range l.Commands {
		n := 0
		for strings.HasSuffix(c, `\r`) {
			c, n = strings.TrimSuffix(c, `\r`), n+1
		}
		commands, returns = append(commands, c), append(returns, n)
	}
	return commands, returns, l.sleeps, l.sudo
}

// TestTextReadsBackToTheList: a list written as a file reads back to the
// same commands, returns, sleeps, and root commands, a sum of sleeps above
// SleepMax and one leaving a remainder under SleepMin among them, every
// --sleep line within the reader's range.
func TestTextReadsBackToTheList(t *testing.T) {
	sleeps := []int64{
		0,
		int64(2 * time.Second),
		int64(90 * time.Second),
		int64(13 * time.Hour),
		int64(12*time.Hour + 500*time.Millisecond),
		int64(24 * time.Hour),
		int64(1500 * time.Millisecond),
	}
	commands := []string{"clear counters", "show clock", "show version", "show users", "show ip route", "show inventory", "show logging"}
	returns := []int{1, 0, 0, 2, 0, 0, 0}
	sudo := []bool{false, false, true, false, false, true, false}
	text := string(Text(commands, returns, sleeps, sudo))
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if strings.HasPrefix(line, "--sleep") {
			if _, ok, err := SleepLine(line); !ok || err != nil {
				t.Fatalf("line %q does not read back as a sleep (%v)", line, err)
			}
		}
	}
	gotCommands, gotReturns, gotSleeps, gotSudo := read(t, text)
	if strings.Join(gotCommands, "|") != strings.Join(commands, "|") {
		t.Fatalf("commands %q, want %q\n%s", gotCommands, commands, text)
	}
	for i := range commands {
		if gotReturns[i] != returns[i] || gotSleeps[i] != sleeps[i] || gotSudo[i] != sudo[i] {
			t.Fatalf("command %d: returns %d sleep %s sudo %t, want %d, %s, and %t\n%s", i+1, gotReturns[i], time.Duration(gotSleeps[i]), gotSudo[i], returns[i], time.Duration(sleeps[i]), sudo[i], text)
		}
	}
	want := "clear counters\\r\n--sleep 2s\nshow clock\n--sleep 1m30s\n--sudo show version\n--sleep 12h0m0s\n--sleep 1h0m0s\nshow users\\r\\r\n"
	if !strings.HasPrefix(text, want) {
		t.Fatalf("file begins\n%s\nwant\n%s", text, want)
	}
}

// TestTextWithoutDeclarations: a list with no returns, sleeps, or root
// commands is its commands, one per line.
func TestTextWithoutDeclarations(t *testing.T) {
	if got := string(Text([]string{"show clock", "show version"}, nil, nil, nil)); got != "show clock\nshow version\n" {
		t.Fatalf("got %q", got)
	}
}

// TestSleepLine: the line's forms, a word that only begins as --sleep
// does, and a --sleep line that is not one.
func TestSleepLine(t *testing.T) {
	for _, tc := range []struct {
		line string
		want time.Duration
		ok   bool
		err  bool
	}{
		{"--sleep 5s", 5 * time.Second, true, false},
		{"  --sleep=1m  ", time.Minute, true, false},
		{"--sleepy", 0, false, false},
		{"show --sleep 5s", 0, false, false},
		{"--sleep 3", 0, false, true},
		{"--sleep 13h", 0, false, true},
		{"--sleep", 0, false, true},
	} {
		d, ok, err := SleepLine(tc.line)
		if d != tc.want || ok != tc.ok || (err != nil) != tc.err {
			t.Errorf("SleepLine(%q) = %s %t %v; want %s %t err=%t", tc.line, d, ok, err, tc.want, tc.ok, tc.err)
		}
	}
}

// TestSudoLine: the line's forms, the spaces trimmed, a word that only
// begins as --sudo does, and a --sudo line with no command.
func TestSudoLine(t *testing.T) {
	for _, tc := range []struct {
		line, want string
		ok, err    bool
	}{
		{"--sudo systemctl restart nginx", "systemctl restart nginx", true, false},
		{"  --sudo   iptables-save  ", "iptables-save", true, false},
		{"--sudo=id -u", "id -u", true, false},
		{"--sudo= id -u", "id -u", true, false},
		{"\t--sudo\tcat /etc/shadow", "cat /etc/shadow", true, false},
		{"--sudoers", "", false, false},
		{"show --sudo id", "", false, false},
		{"--sudo", "", false, true},
		{"--sudo   ", "", false, true},
		{"--sudo=", "", false, true},
	} {
		text, ok, err := SudoLine(tc.line)
		if text != tc.want || ok != tc.ok || (err != nil) != tc.err {
			t.Errorf("%q: %q %t %v", tc.line, text, ok, err)
		}
	}
}

// TestListReadsTheGrammar: a List built line by line takes a --sleep line
// as the wait before the next command, the sleeps in a row adding up, a
// --sudo line as a root command, any other line as text; literal adds every
// line as text; a refused line carries its code and the line's fault alone;
// the lists are empty when no command has a sleep or is root.
// TestStatement: a root command written back is its --sudo statement, and
// SudoLine reads it back to the command; any other is its text.
func TestStatement(t *testing.T) {
	if got := Statement("systemctl restart nginx", true); got != "--sudo systemctl restart nginx" {
		t.Errorf("root: %q", got)
	}
	if text, ok, err := SudoLine(Statement("id -u", true)); err != nil || !ok || text != "id -u" {
		t.Errorf("read back: %q %v %v", text, ok, err)
	}
	if got := Statement("whoami", false); got != "whoami" {
		t.Errorf("login user's: %q", got)
	}
}

func TestListReadsTheGrammar(t *testing.T) {
	var l List
	for _, line := range []string{"uptime", "--sleep 5s", "--sleep=1s", "--sudo systemctl restart nginx", "--sudoers"} {
		if err := l.Line(line, false); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
	}
	l.Sleep(2 * time.Second)
	l.Add("show clock", true)
	if want := []string{"uptime", "systemctl restart nginx", "--sudoers", "show clock"}; strings.Join(l.Commands, "|") != strings.Join(want, "|") {
		t.Fatalf("commands %q, want %q", l.Commands, want)
	}
	if got := l.SleepsNS(); len(got) != 4 || got[1] != int64(6*time.Second) || got[3] != int64(2*time.Second) || got[0] != 0 || got[2] != 0 {
		t.Fatalf("sleeps %v", got)
	}
	if got := l.Sudo(); len(got) != 4 || got[0] || !got[1] || got[2] || !got[3] {
		t.Fatalf("sudo %v", got)
	}
	if err := l.Line("--sleep 3s", false); err != nil || l.Pending() != 3*time.Second {
		t.Fatalf("pending %s %v", l.Pending(), err)
	}
	var plain List
	for _, line := range []string{"--sleep 5s", "--sudo id -u", "uptime"} {
		if err := plain.Line(line, true); err != nil {
			t.Fatal(err)
		}
	}
	if len(plain.Commands) != 3 || plain.Commands[1] != "--sudo id -u" || plain.Pending() != 0 || len(plain.SleepsNS()) != 0 || plain.SleepsNS() == nil || len(plain.Sudo()) != 0 || plain.Sudo() == nil {
		t.Fatalf("literal: %q %v %v", plain.Commands, plain.SleepsNS(), plain.Sudo())
	}
	for line, want := range map[string]string{
		"--sleep 3": `commands_file_sleep_invalid: --sleep takes a duration of 1s..12h (30s, 5m, 1h30m), not "3"; the line is --sleep DURATION alone`,
		"--sudo":    "commands_file_sudo_invalid: --sudo has no command; the line is --sudo TEXT",
	} {
		var r List
		if err := r.Line(line, false); err == nil || err.Error() != want || len(r.Commands) != 0 {
			t.Errorf("%q: %v, want %q", line, err, want)
		}
	}
}
