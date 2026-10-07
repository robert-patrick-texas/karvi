package osutil

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// TestRawTerminalLinesEndsEachLineAtTheFirstColumn writes karvi's lines in
// the pieces a writer may be handed: a lone line feed gains its carriage
// return, one already after a carriage return does not, a carriage return
// ending one write counts for a line feed opening the next, and every
// other byte is written as it came.
func TestRawTerminalLinesEndsEachLineAtTheFirstColumn(t *testing.T) {
	var got bytes.Buffer
	w := &rawLines{w: &got}
	for _, piece := range []string{"DEBUG one\n", "DEBUG two\r\n", "\x1b[34m! header\x1b[0m\r", "\n", "\n", "tab\there\n"} {
		n, err := w.Write([]byte(piece))
		if err != nil || n != len(piece) {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", piece, n, err, len(piece))
		}
	}
	want := "DEBUG one\r\nDEBUG two\r\n\x1b[34m! header\x1b[0m\r\n\r\ntab\there\r\n"
	if got.String() != want {
		t.Fatalf("written %q, want %q", got.String(), want)
	}
}

// TestRawTerminalLinesTranslatesOnlyOnATerminal: a redirected stream is
// returned as it is; a terminal is written through the translation.
func TestRawTerminalLinesTranslatesOnlyOnATerminal(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if w := RawTerminalLines(file); w != file {
		t.Fatalf("a regular file became %T", w)
	}
	master, slave := openPTY(t)
	defer master.Close()
	defer slave.Close()
	if _, ok := RawTerminalLines(slave).(*rawLines); !ok {
		t.Fatalf("a terminal was returned as %T", RawTerminalLines(slave))
	}
}

// openPTY opens a pseudo-terminal pair without a library: /dev/ptmx, its
// lock cleared, and the numbered slave.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		t.Fatalf("unlock: %v", errno)
	}
	var n uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); errno != 0 {
		master.Close()
		t.Fatalf("number: %v", errno)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	return master, slave
}
