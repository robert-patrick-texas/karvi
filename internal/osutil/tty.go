package osutil

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
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

func ioctlTermios(fd uintptr, req uintptr, t *syscall.Termios) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
func ReadTTY(prompt string, masked bool) (string, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		// The caller attaches credential_prompt_unavailable; repeating the code
		// here rendered it twice.
		return "", fmt.Errorf("no controlling terminal: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprint(f, prompt); err != nil {
		return "", err
	}
	var old syscall.Termios
	if masked {
		if err := ioctlTermios(f.Fd(), tcgets, &old); err != nil {
			return "", err
		}
		next := old
		next.Lflag &^= syscall.ECHO
		if err := ioctlTermios(f.Fd(), tcsets, &next); err != nil {
			return "", err
		}
		defer ioctlTermios(f.Fd(), tcsets, &old)
	}
	line, err := bufio.NewReader(f).ReadString('\n')
	if masked {
		fmt.Fprintln(f)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
func IsTerminal(f *os.File) bool {
	var t syscall.Termios
	return ioctlTermios(f.Fd(), tcgets, &t) == nil
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
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), tiocgwinsz, uintptr(unsafe.Pointer(&size)))
	if errno != 0 || size.Columns == 0 {
		return 0
	}
	return int(size.Columns)
}

// TerminalSize returns the rows and columns of a terminal, or zeros when f is
// not a terminal (login session metadata).
func TerminalSize(f *os.File) (rows, columns int) {
	if f == nil || !IsTerminal(f) {
		return 0, 0
	}
	var size windowSize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), tiocgwinsz, uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
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
// It goes through the same ioctl path as ReadTTY: golang.org/x/sys/unix is
// not vendored and this needs nothing more.
func RawMode(f *os.File) (restore func() error, err error) {
	var old syscall.Termios
	if err := ioctlTermios(f.Fd(), tcgets, &old); err != nil {
		return nil, err
	}
	next := old
	next.Iflag &^= syscall.ICRNL | syscall.IXON | syscall.INLCR | syscall.IGNCR
	next.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ISIG | syscall.IEXTEN
	next.Cc[syscall.VMIN] = 1
	next.Cc[syscall.VTIME] = 0
	if err := ioctlTermios(f.Fd(), tcsets, &next); err != nil {
		return nil, err
	}
	return func() error { return ioctlTermios(f.Fd(), tcsets, &old) }, nil
}

// NotifyResize delivers one value per change of the terminal's size
// (SIGWINCH) until stop is called. The receiver re-reads TerminalSize; the
// signal carries no size of its own.
func NotifyResize() (changes <-chan os.Signal, stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	return ch, func() { signal.Stop(ch) }
}
