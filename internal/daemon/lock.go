package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// LockPath is the daemon's lifetime lock for a daemon socket: d.lock beside
// it.
func LockPath(daemonSocket string) string {
	return filepath.Join(filepath.Dir(daemonSocket), osutil.DaemonLockName)
}

// lockPoll is how often a start waiting for the lock tries it again.
const lockPoll = 25 * time.Millisecond

// stoppedWaiting is a wait for the lock ended by the daemon's own stop
// before it started: its context done, or a signal, sig.
type stoppedWaiting struct{ sig os.Signal }

func (stoppedWaiting) Error() string { return "stopped before the lifetime lock was taken" }

// takeLifetimeLock is a daemon's lock for its life (EXAMPLES chapter 108):
// an exclusive flock on path, taken before Serve sweeps or touches a socket
// and held until its files are removed, the process's exit releasing it, a
// kill included. While another daemon holds it, a daemon whose socket
// answers is running, refused at once; one whose socket does not answer is
// still stopping, its files not yet removed, and is waited for up to bound,
// then refused with the pid the file names. ctx's end or a signal on
// signals ends the wait, stoppedWaiting. The holder writes its pid into the
// file, which stays when it exits: removing it would race its next taker.
func takeLifetimeLock(ctx context.Context, signals <-chan os.Signal, path, socket string, bound time.Duration) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(bound)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if answers(socket) {
			f.Close()
			return nil, errorcodes.Errorf("daemon_already_running", "daemon already running at %s", socket)
		}
		if !time.Now().Before(deadline) {
			pid := holderPID(f)
			f.Close()
			return nil, errorcodes.Errorf("daemon_already_running", "daemon still stopping: pid %s", pid)
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, stoppedWaiting{}
		case sig := <-signals:
			f.Close()
			return nil, stoppedWaiting{sig: sig}
		case <-time.After(lockPoll):
		}
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// answers says whether a daemon listens at socket.
func answers(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, 150*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// holderPID is the pid the lock's holder wrote, "unknown" when the file
// names none.
func holderPID(f *os.File) string {
	buf := make([]byte, 32)
	n, _ := f.ReadAt(buf, 0)
	pid := strings.TrimSpace(string(buf[:n]))
	if _, err := strconv.Atoi(pid); err != nil {
		return "unknown"
	}
	return pid
}
