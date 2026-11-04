// Package commandfile is a commands file's line grammar, read and written.
// A line is a statement sent to the device as written, unless it is one of
// karvi's own: a --sleep line, waited before the next command (EXAMPLES
// chapter 95), or a --sudo line, a command run as root (chapter 96). A
// job's rerun files are written in the same grammar, so that --cf on them
// sends what was sent: each command's \r endings, the sleep before it, and
// its --sudo (chapter 79, chapter 96).
package commandfile

import (
	"fmt"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// SleepMin and SleepMax bound one --sleep, --timeout's range (EXAMPLES
// chapter 95).
const (
	SleepMin = time.Second
	SleepMax = 12 * time.Hour
)

// SleepDuration is a --sleep value's duration: Go's form, 1s..12h.
func SleepDuration(value string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || d < SleepMin || d > SleepMax {
		return 0, fmt.Errorf("takes a duration of 1s..12h (30s, 5m, 1h30m), not %q", value)
	}
	return d, nil
}

// SleepLine reports whether a commands-file line is a --sleep statement,
// and its duration: the line, spaces around it trimmed, is --sleep DURATION
// or --sleep=DURATION alone. Any other line is device text, but a line whose
// first word is --sleep and is not a statement is refused rather than sent,
// so a mistyped sleep never reaches the device as text (EXAMPLES chapter
// 95).
func SleepLine(line string) (time.Duration, bool, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "--sleep")
	if !ok || (rest != "" && rest[0] != ' ' && rest[0] != '\t' && rest[0] != '=') {
		return 0, false, nil
	}
	value := strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "=") {
		value = rest[1:]
	}
	d, err := SleepDuration(value)
	if err != nil {
		return 0, false, fmt.Errorf("--sleep %w; the line is --sleep DURATION alone", err)
	}
	return d, true, nil
}

// SudoLine reports whether a commands-file line is a --sudo statement, and
// its command: the line, spaces around it trimmed, is --sudo TEXT or
// --sudo=TEXT, TEXT trimmed of the spaces around it. Any other line is
// device text, but a line whose first word is --sudo with no command after
// it is refused rather than sent (EXAMPLES chapter 96).
func SudoLine(line string) (string, bool, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "--sudo")
	if !ok || (rest != "" && rest[0] != ' ' && rest[0] != '\t' && rest[0] != '=') {
		return "", false, nil
	}
	text := strings.TrimSpace(strings.TrimPrefix(rest, "="))
	if text == "" {
		return "", false, fmt.Errorf("--sudo has no command; the line is --sudo TEXT")
	}
	return text, true, nil
}

// Statement is a command as karvi writes it back: a root command as its
// --sudo statement, --sudo TEXT, any other as its text. The rerun files, a
// collection's marker, the dry run's lists, and karvi-audit's rows write a
// root command so (EXAMPLES chapter 96).
func Statement(text string, root bool) string {
	if root {
		return "--sudo " + text
	}
	return text
}

// List is a command list built statement by statement in the order
// written: the commands, the sleep waited before each, and each one's root
// flag. A sleep waits before the next command added, the sleeps in a row
// adding up (EXAMPLES chapters 95 and 96). The command line's sources and
// a platform's crun-commands entries are both read into one.
type List struct {
	Commands []string
	sleeps   []int64 // nanoseconds before each command, 0 none
	sudo     []bool
	pending  time.Duration
}

// Sleep adds d to the wait before the next command.
func (l *List) Sleep(d time.Duration) { l.pending += d }

// Add appends a command, root for a --sudo one, the sleeps since the last
// command waited before it.
func (l *List) Add(text string, root bool) {
	l.Commands, l.sleeps, l.sudo = append(l.Commands, text), append(l.sleeps, int64(l.pending)), append(l.sudo, root)
	l.pending = 0
}

// Line adds one line by the grammar: a --sleep line's wait (SleepLine), a
// --sudo line's root command (SudoLine), any other line device text as
// written; literal adds every line as text. A line refused is
// commands_file_sleep_invalid or commands_file_sudo_invalid, its message
// the line's fault alone, for the caller to say where the line is.
func (l *List) Line(line string, literal bool) error {
	if literal {
		l.Add(line, false)
		return nil
	}
	d, ok, err := SleepLine(line)
	if err != nil {
		return errorcodes.Errorf("commands_file_sleep_invalid", "%v", err)
	}
	if ok {
		l.Sleep(d)
		return nil
	}
	text, ok, err := SudoLine(line)
	if err != nil {
		return errorcodes.Errorf("commands_file_sudo_invalid", "%v", err)
	}
	if ok {
		l.Add(text, true)
		return nil
	}
	l.Add(line, false)
	return nil
}

// Pending is the wait no command has followed yet, which the caller
// refuses once the list ends (sleep_without_command).
func (l *List) Pending() time.Duration { return l.pending }

// SleepsNS is the sleep before each command in nanoseconds, empty when no
// command has one, as a plan's list carries it.
func (l *List) SleepsNS() []int64 {
	for _, ns := range l.sleeps {
		if ns > 0 {
			return l.sleeps
		}
	}
	return []int64{}
}

// Sudo is each command's root flag, empty when none is root, as a plan's
// list carries it.
func (l *List) Sudo() []bool {
	for _, root := range l.sudo {
		if root {
			return l.sudo
		}
	}
	return []bool{}
}

// Text is a command list written as a commands file, one statement per
// line: the sleep before a command as --sleep lines (sleepLines), then the
// command, a root command as --sudo TEXT, its blind returns written back as
// its trailing \r sequences, so that the file read without --literal sends
// what was sent. returns, sleeps, and sudo are each empty or one entry per
// command. A command whose own text ends in \r, or begins as a --sleep or
// a --sudo line does, which only --literal sends, reads back as returns, a
// sleep, or a root command. The other declarations (--timeout, --maxbytes,
// --expect, --blind) have no form in a file and stay in the plan.
func Text(commands []string, returns []int, sleeps []int64, sudo []bool) []byte {
	var b strings.Builder
	for i, c := range commands {
		if i < len(sleeps) {
			for _, line := range sleepLines(time.Duration(sleeps[i])) {
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
		b.WriteString(Statement(c, i < len(sudo) && sudo[i]))
		if i < len(returns) {
			b.WriteString(strings.Repeat(`\r`, returns[i]))
		}
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// sleepLines are the --sleep lines whose durations add up to d, each within
// SleepMin..SleepMax: the plan holds the sleeps in a row as one sum, which
// may pass SleepMax, so it is written in parts of SleepMax, the part before
// a remainder under SleepMin shortened to leave SleepMin. A plan's sleep is
// at least SleepMin, being a --sleep's or a sum of them; 0 is none.
func sleepLines(d time.Duration) []string {
	var lines []string
	for d > 0 {
		part := min(d, SleepMax)
		if left := d - part; left > 0 && left < SleepMin {
			part = d - SleepMin
		}
		lines = append(lines, "--sleep "+part.String())
		d -= part
	}
	return lines
}
