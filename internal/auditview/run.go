package auditview

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

// The run (docs/EXAMPLES.md, chapter 64, issue 5): journalctl's own refusal
// of a value is its message on standard error as it wrote it, exit 1; not
// following, the heading is written with the first row, and when journalctl
// returned no entry, `karvi-audit: no matches` is written to standard error
// in the warning colour; following, the heading with the first row or once
// journalctl has run headingWait without ending. An entry the reader
// cannot read has no row, and one line at the end counts them. The exit is
// 0 when journalctl ended 0, a follow ended by Ctrl-C or by its reader's
// closing included; 1 when journalctl is missing or ends otherwise. A reader
// that quits ends the run quietly, under --follow without waiting for the
// next event.

// headingWait is how long a follow's heading waits for journalctl to refuse
// a value before it is written with no row.
const headingWait = 500 * time.Millisecond

// Run reads the trail for the flags and writes the table to stdout, the
// reader's messages to stderr, and returns the exit status.
func Run(f *Flags, stdout, stderr *os.File) int {
	out := Colours(stdout, *f.NoColor)
	errs := Colours(stderr, *f.NoColor)
	path, err := exec.LookPath("journalctl")
	if err != nil {
		Message(stderr, errs, "error", "journalctl not found")
		return 1
	}
	cmd := exec.Command(path, Args(f)...)
	cmd.Stderr = stderr
	// journalctl ends with the reader, killed or not.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	entries, err := cmd.StdoutPipe()
	if err != nil {
		Message(stderr, errs, "error", err.Error())
		return 1
	}
	// A broken pipe is a write's error, not the end of the process, so the
	// reader stops journalctl and ends quietly; Ctrl-C reaches journalctl
	// too, which ends 0, and a signal for the reader alone is passed on.
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGPIPE, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		Message(stderr, errs, "error", err.Error())
		return 1
	}
	stopped := make(chan struct{})
	// quit is a write that failed, writeErr its error when it was not the
	// reader's going; gone is the reader's going under --follow.
	var quit bool
	var writeErr error
	var gone atomic.Bool
	// signalled is a signal the reader got, Ctrl-C's among them, which
	// reaches journalctl too, from the terminal and passed on: journalctl
	// ending by it is the run's end, not its failure.
	var signalled atomic.Bool
	go func() {
		for {
			select {
			case s := <-signals:
				if s != syscall.SIGPIPE {
					signalled.Store(true)
					_ = cmd.Process.Signal(s)
				}
			case <-stopped:
				return
			}
		}
	}()
	if *f.Follow && osutil.IsPipe(stdout) {
		go func() {
			if readerClosed(stdout) {
				gone.Store(true)
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
		}()
	}
	width := 0
	if osutil.IsTerminal(stdout) {
		width = osutil.TerminalWidth(stdout)
	}
	t := Table{Width: width, Colour: out}
	w := bufio.NewWriter(stdout)
	r := bufio.NewReader(entries)
	rows, unread, headed := 0, 0, false
	failed := func(err error) {
		quit = true
		if !errors.Is(err, syscall.EPIPE) {
			writeErr = err
		}
	}
	write := func(line string) bool {
		if _, err := w.WriteString(line + "\n"); err != nil {
			failed(err)
			return false
		}
		return true
	}
	// journalctl's lines, read apart from the writing, so that a follow's
	// heading can wait on a timer as well as on the first row.
	type read struct {
		line []byte
		err  error
	}
	lines := make(chan read, 64)
	go func() {
		for {
			line, err := r.ReadBytes('\n')
			lines <- read{line, err}
			if err != nil {
				return
			}
		}
	}()
	// Following, the heading comes with the first row or once journalctl
	// has run headingWait without ending, past the refusal of a value,
	// which journalctl says at its start (chapter 64, found while building
	// S2).
	var heading <-chan time.Time
	if *f.Follow {
		heading = time.After(headingWait)
	}
	ended := false
	for !quit && !ended {
		select {
		case <-heading:
			heading = nil
			if !headed {
				headed = write(t.Heading())
			}
		case got := <-lines:
			if len(got.line) > 0 {
				if row, ok := Decode(got.line); ok {
					if !headed {
						headed = write(t.Heading())
					}
					if !quit && write(t.Line(row)) {
						rows++
					}
				} else {
					unread++
				}
			}
			ended = got.err != nil
		}
		// Written whenever no line of journalctl's is waiting, so a
		// follow's rows show as they come.
		if !quit && len(lines) == 0 {
			if err := w.Flush(); err != nil {
				failed(err)
			}
		}
	}
	if quit {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		for !ended {
			ended = (<-lines).err != nil
		}
	}
	waitErr := cmd.Wait()
	close(stopped)
	if !quit {
		if err := w.Flush(); err != nil {
			failed(err)
		}
	}
	if unread > 0 {
		Message(stderr, errs, "warning", fmt.Sprintf("%d %s not shown: not audit schema %d", unread, plural(unread, "event", "events"), records.AuditSchemaVersion))
	}
	if writeErr != nil {
		Message(stderr, errs, "error", writeErr.Error())
		return 1
	}
	if quit || gone.Load() {
		return 0
	}
	if waitErr != nil && !(signalled.Load() && endedBySignal(waitErr)) {
		return 1
	}
	if rows == 0 && unread == 0 && !*f.Follow {
		Message(stderr, errs, "warning", "no matches")
	}
	return 0
}

// endedBySignal reports whether journalctl's wait error is its end by a
// signal.
func endedBySignal(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	return ok && status.Signaled()
}

// readerClosed waits for the reading end of the pipe on f to close, which a
// pipe's writing end reports as an error, and reports whether it did.
func readerClosed(f *os.File) bool {
	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: 0}}
	for {
		_, err := unix.Poll(fds, -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err == nil && fds[0].Revents&(unix.POLLERR|unix.POLLHUP) != 0
	}
}

// Colours reports whether the reader colours what it writes to f: at a
// terminal and through a pipe, never into a file, and not under --nocolor
// or NO_COLOR (issue 4).
func Colours(f *os.File, nocolor bool) bool {
	if nocolor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	return display.ColorEnabled("always", theme, display.DestinationOf(osutil.IsTerminal(f), osutil.IsRegularFile(f)))
}

// Message writes one of the reader's lines to w, `karvi-audit: text`, in the
// role's colour when colour is on.
func Message(w io.Writer, colour bool, role, text string) {
	line := Spec.Program + ": " + text
	fmt.Fprintln(w, display.ANSIStyle(line, display.RoleColor(theme, role, "default"), colour, display.RoleBold(role)))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
