package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// startBound is a daemon runtime at daemon.start-timeout 1s whose lifetime
// lock this process holds, as a daemon still stopping does: its pid in
// d.lock, no socket file. release frees the lock, once. launched is the pid
// of the daemon serve process launched for the runtime, watched for from
// now, 0 when none was seen.
func startBound(t *testing.T) (g globalOptions, socket string, release func(), launched func() int) {
	t.Helper()
	base, g, socket := daemonTestRuntime(t)
	g.sets = append(g.sets, `daemon.start-timeout="1s"`)
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(daemon.LockPath(socket), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid()) + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release = func() { once.Do(func() { f.Close() }) }
	// The last cleanup: a daemon a failing test left, waiting for the lock
	// or started after it, is not left on the host.
	t.Cleanup(func() {
		if pid := launched(); pid != 0 && !reaped(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	t.Cleanup(release)
	marker := fmt.Sprintf("basedir=%q", base)
	find := func() int {
		entries, _ := os.ReadDir("/proc")
		for _, e := range entries {
			pid, err := strconv.Atoi(e.Name())
			if err != nil || pid == os.Getpid() {
				continue
			}
			line, _ := os.ReadFile("/proc/" + e.Name() + "/cmdline")
			if strings.Contains(string(line), marker) && strings.HasSuffix(string(line), "\x00daemon\x00serve\x00") {
				return pid
			}
		}
		return 0
	}
	seen, stop := make(chan int, 1), make(chan struct{})
	go func() {
		for {
			if pid := find(); pid != 0 {
				seen <- pid
				return
			}
			select {
			case <-stop:
				seen <- 0
				return
			case <-time.After(2 * time.Millisecond):
			}
		}
	}()
	var pid int
	var asked sync.Once
	launched = func() int {
		asked.Do(func() { close(stop); pid = <-seen })
		return pid
	}
	t.Cleanup(func() { launched() })
	return g, socket, release, launched
}

// reaped reports whether pid has ended and been waited for: a process
// ended and not waited for keeps its entry in /proc.
func reaped(pid int) bool {
	_, err := os.Stat("/proc/" + strconv.Itoa(pid))
	return os.IsNotExist(err)
}

// answers reports whether a daemon answers at socket.
func answers(socket string, maxFrame int64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := daemon.Ping(ctx, socket, maxFrame)
	return err == nil
}

// TestAStartWaitsByItsDaemonsClock: a start that finds a daemon still
// stopping (its lock held) waits by the launched daemon's own count of
// daemon.start-timeout, not a second count of the client's. The lock freed
// in time, the daemon starts. Never freed, the start fails once its daemon
// has been refused, has ended, and has been waited for, the refusal in the
// log. Freed about where the daemon's count ends, the start succeeds if and
// only if a daemon then answers, and a failed start has left no daemon.
func TestAStartWaitsByItsDaemonsClock(t *testing.T) {
	t.Run("freed in time", func(t *testing.T) {
		g, socket, release, _ := startBound(t)
		time.AfterFunc(500*time.Millisecond, release)
		var stderr bytes.Buffer
		rt, err := ensureTestDaemon(context.Background(), g, &stderr)
		defer stopTestDaemon(t, socket, rt.MaxFrame)
		if err != nil {
			t.Fatalf("the start: %v", err)
		}
		if !strings.HasPrefix(stderr.String(), "daemon started socket=") || !answers(socket, rt.MaxFrame) {
			t.Fatalf("stderr %q, answering %t", stderr.String(), answers(socket, rt.MaxFrame))
		}
	})
	t.Run("never freed", func(t *testing.T) {
		g, socket, _, launched := startBound(t)
		var stderr bytes.Buffer
		rt, err := ensureTestDaemon(context.Background(), g, &stderr)
		defer stopTestDaemon(t, socket, rt.MaxFrame)
		if pid := launched(); pid == 0 || !reaped(pid) {
			t.Errorf("the launched daemon %d was not waited for by the failed start", pid)
		}
		if want := "daemon_start_failed: daemon failed to start; inspect " + rt.LogPath; err == nil || err.Error() != want {
			t.Fatalf("the start: %v", err)
		}
		log, _ := os.ReadFile(rt.LogPath)
		if want := fmt.Sprintf("daemon_already_running: daemon still stopping: pid %d", os.Getpid()); !strings.Contains(string(log), want) {
			t.Fatalf("the log lacks %q:\n%s", want, log)
		}
	})
	// The daemon's count ends a second after its start-up, some
	// milliseconds past its first sighting; the releases straddle it. A
	// loaded host moves every release to one side, where the rule holds
	// as well.
	for _, past := range []time.Duration{0, 20 * time.Millisecond, 40 * time.Millisecond, 60 * time.Millisecond, 100 * time.Millisecond} {
		t.Run("freed at the bound, "+past.String(), func(t *testing.T) {
			g, socket, release, launched := startBound(t)
			var stderr bytes.Buffer
			began := time.Now()
			time.AfterFunc(time.Second+past, release)
			rt, err := ensureTestDaemon(context.Background(), g, &stderr)
			pid := launched()
			defer stopTestDaemon(t, socket, rt.MaxFrame)
			if err == nil {
				if !answers(socket, rt.MaxFrame) {
					t.Fatal("the start succeeded and no daemon answers")
				}
				return
			}
			if errorcodes.Of(err) != "daemon_start_failed" || pid == 0 || !reaped(pid) {
				t.Fatalf("the start: %v after %s, its daemon %d", err, time.Since(began), pid)
			}
			release()
			if answers(socket, rt.MaxFrame) {
				t.Fatal("a daemon answers after the failed start")
			}
		})
	}
}

// TestAStartKillsADaemonThatNeitherAnswersNorEnds: startBackstop past
// daemon.start-timeout the launched daemon is killed and waited for, and
// the start fails saying so.
func TestAStartKillsADaemonThatNeitherAnswersNorEnds(t *testing.T) {
	g, _, release, launched := startBound(t)
	release()
	defer func(was time.Duration) { startBackstop = was }(startBackstop)
	startBackstop = 300 * time.Millisecond
	// TMPDIR reaches the daemon (daemonEnvironment), and TestMain's daemon
	// hangs on finding the marker there.
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, hangingDaemonMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	var stderr bytes.Buffer
	began := time.Now()
	rt, err := ensureTestDaemon(context.Background(), g, &stderr)
	took := time.Since(began)
	if pid := launched(); pid == 0 || !reaped(pid) {
		t.Errorf("the launched daemon %d was not waited for by the failed start", pid)
	}
	if want := "daemon_start_failed: daemon did not start in 1.3s and was killed; inspect " + rt.LogPath; err == nil || err.Error() != want || took < 1300*time.Millisecond {
		t.Fatalf("the start: %v after %s", err, took)
	}
}

// TestAnInterruptedStartLeavesItsDaemon: an interrupt during the wait ends
// the start at once and leaves the launched daemon, which goes on to start
// once the lock is free.
func TestAnInterruptedStartLeavesItsDaemon(t *testing.T) {
	g, socket, release, launched := startBound(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	var stderr bytes.Buffer
	began := time.Now()
	rt, err := ensureTestDaemon(ctx, g, &stderr)
	defer stopTestDaemon(t, socket, rt.MaxFrame)
	if took := time.Since(began); errorcodes.Of(err) != "daemon_start_failed" || !strings.Contains(err.Error(), "start interrupted, the launched daemon left to start") || took > 900*time.Millisecond {
		t.Fatalf("the start: %v after %s", err, took)
	}
	if pid := launched(); pid == 0 || reaped(pid) {
		t.Fatalf("the launched daemon %d did not outlive the interrupt", pid)
	}
	release()
	for deadline := time.Now().Add(5 * time.Second); !answers(socket, rt.MaxFrame) && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	if !answers(socket, rt.MaxFrame) {
		t.Fatal("the launched daemon did not start once the lock was free")
	}
}
