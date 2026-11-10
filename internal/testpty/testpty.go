// Package testpty gives a test a pseudo-terminal without a library: the
// pair opened from /dev/ptmx, and a recorder of what the terminal was
// sent. It imports nothing of karvi's, so osutil's own tests use it too.
package testpty

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Open opens a pseudo-terminal pair: /dev/ptmx, its lock cleared, and the
// numbered slave; the test is skipped where the host has none. Both are
// closed at the test's end (a second close is harmless).
func Open(t testing.TB) (master, slave *os.File) {
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
	t.Cleanup(func() {
		slave.Close()
		master.Close()
	})
	return master, slave
}

// SetSize sets the terminal's window to rows by columns (TIOCSWINSZ), as
// a resized terminal window does; a pair opened here starts at 0 by 0,
// which a reader takes as a width it cannot say.
func SetSize(t testing.TB, f *os.File, rows, columns uint16) {
	t.Helper()
	size := [4]uint16{rows, columns, 0, 0}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&size))); errno != 0 {
		t.Fatalf("set the window size: %v", errno)
	}
}

// Recorder reads what the terminal was sent from its master side, as it
// comes, so a writer to the slave never fills the terminal's buffer.
type Recorder struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	done chan struct{}
}

// Record starts reading master until the slave's last descriptor closes.
func Record(master *os.File) *Recorder {
	r := &Recorder{done: make(chan struct{})}
	go func() {
		defer close(r.done)
		chunk := make([]byte, 32<<10)
		for {
			n, err := master.Read(chunk)
			r.mu.Lock()
			r.buf.Write(chunk[:n])
			r.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return r
}

// Wait closes slave and returns everything the terminal was sent, each
// line feed as the line discipline sends it (a carriage return before it)
// put back as the writer wrote it.
func (r *Recorder) Wait(t testing.TB, slave *os.File) string {
	t.Helper()
	if err := slave.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	select {
	case <-r.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the terminal's master side was not drained")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.ReplaceAll(r.buf.String(), "\r\n", "\n")
}
