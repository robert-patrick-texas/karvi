package osutil

import (
	"io"
	"os"
	"os/signal"
	"syscall"
	"unsafe"
)

const (
	tcgets     = 0x5401
	tcsets     = 0x5402
	tiocgwinsz = 0x5413
)

type windowSize struct {
	Rows    uint16
	Columns uint16
	XPixel  uint16
	YPixel  uint16
}

// ioctl runs one ioctl on f's descriptor through f.SyscallConn, never
// f.Fd(), which would put f into blocking mode for good and so end its read
// deadlines: termline's prompt ends at its context's end (a signal) by one.
func ioctl(f *os.File, req uintptr, arg unsafe.Pointer) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if err := rc.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}
func IsTerminal(f *os.File) bool {
	var t syscall.Termios
	return ioctl(f, tcgets, unsafe.Pointer(&t)) == nil
}

// IsRegularFile reports whether f is a regular file, as a stream redirected
// into a file is; the display writes one plain.
func IsRegularFile(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode().IsRegular()
}

// IsPipe reports whether f is a pipe, as a stream into a pager is.
func IsPipe(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeNamedPipe != 0
}

// TerminalWidth returns the current terminal column count. It returns zero
// when the descriptor is not a terminal or when the kernel does not report a
// usable width. Callers treat zero as "do not alter layout" rather than
// guessing a width for redirected output.
func TerminalWidth(f *os.File) int {
	if f == nil || !IsTerminal(f) {
		return 0
	}
	var size windowSize
	if ioctl(f, tiocgwinsz, unsafe.Pointer(&size)) != nil || size.Columns == 0 {
		return 0
	}
	return int(size.Columns)
}

// ForegroundGroup says whether this process's group is the foreground
// process group of f's terminal (tcgetpgrp, as wget and pv ask before
// drawing progress): false for a job sent to the background. known is
// false where the terminal cannot say: f not a terminal, or not this
// process's controlling terminal.
func ForegroundGroup(f *os.File) (foreground, known bool) {
	if f == nil {
		return false, false
	}
	var group int32
	if ioctl(f, syscall.TIOCGPGRP, unsafe.Pointer(&group)) != nil {
		return false, false
	}
	return int(group) == syscall.Getpgrp(), true
}

// TerminalSize returns the rows and columns of a terminal, or zeros when f is
// not a terminal (login session metadata).
func TerminalSize(f *os.File) (rows, columns int) {
	if f == nil || !IsTerminal(f) {
		return 0, 0
	}
	var size windowSize
	if ioctl(f, tiocgwinsz, unsafe.Pointer(&size)) != nil {
		return 0, 0
	}
	return int(size.Rows), int(size.Columns)
}

// RawMode puts a terminal into the mode the watch screen reads keys in:
// no line buffering, no echo, no
// signal keys (Ctrl-C arrives as a byte and the screen leaves on it), no
// flow control, one byte at a time. Output processing stays as it was; the
// screen addresses the cursor and writes no newline. The returned function
// restores the terminal's settings and is called however the screen ends.
// It is the tree's one raw-mode path: the line editor (termline, stream
// mode's lines and the credential prompts) borrows it too.
func RawMode(f *os.File) (restore func() error, err error) {
	var old syscall.Termios
	if err := ioctl(f, tcgets, unsafe.Pointer(&old)); err != nil {
		return nil, err
	}
	next := old
	next.Iflag &^= syscall.ICRNL | syscall.IXON | syscall.INLCR | syscall.IGNCR
	next.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ISIG | syscall.IEXTEN
	next.Cc[syscall.VMIN] = 1
	next.Cc[syscall.VTIME] = 0
	if err := ioctl(f, tcsets, unsafe.Pointer(&next)); err != nil {
		return nil, err
	}
	return func() error { return ioctl(f, tcsets, unsafe.Pointer(&old)) }, nil
}

// NotifyResize delivers one value per change of the terminal's size
// (SIGWINCH) until stop is called. The receiver re-reads TerminalSize; the
// signal carries no size of its own.
func NotifyResize() (changes <-chan os.Signal, stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	return ch, func() { signal.Stop(ch) }
}

// RawTerminalLines is f for karvi's own lines while another process holds
// the terminal in raw mode, as script(1) holds a recorded login's: output
// processing is off there, so a line feed alone moves down a row and leaves
// the column where the line ended. Each line feed that does not follow a
// carriage return is written after one. f that is not a terminal is
// returned as it is, so a redirected stream keeps its bytes. The writer
// names the terminal it writes to (Terminal), so the display's questions of
// a terminal, its colour and its width, are answered for f.
func RawTerminalLines(f *os.File) io.Writer {
	if !IsTerminal(f) {
		return f
	}
	return &rawLines{w: f, terminal: f}
}

type rawLines struct {
	w        io.Writer
	terminal *os.File
	last     byte // the last byte written, so a carriage return ending one write counts for the next
}

// Terminal is the terminal the lines are written to.
func (r *rawLines) Terminal() *os.File { return r.terminal }

func (r *rawLines) Write(p []byte) (int, error) {
	out := make([]byte, 0, len(p)+8)
	for _, b := range p {
		if b == '\n' && r.last != '\r' {
			out = append(out, '\r')
		}
		out = append(out, b)
		r.last = b
	}
	if _, err := r.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}
