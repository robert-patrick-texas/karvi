package daemon

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

// The daemon's lifetime lock (lock.go, EXAMPLES chapter 108): a daemon
// started while another is still stopping, its listeners closed and a job's
// records still being written, waits for it to exit before it sweeps or
// takes the socket, so the stopping daemon's removals cannot take the new
// daemon's files.

// serveLocked runs s.Serve until the returned stop is called; the channel
// receives Serve's error.
func serveLocked(t *testing.T, s *Server) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx) }()
	t.Cleanup(stop)
	return stop, served
}

func waitAnswers(t *testing.T, socket string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ipc.Wait(ctx, socket); err != nil {
		t.Fatalf("no daemon answers at %s: %v", socket, err)
	}
}

func waitRefuses(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for answers(socket) {
		if time.Now().After(deadline) {
			t.Fatalf("the daemon at %s still answers", socket)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitServed(t *testing.T, served <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-served:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not return", what)
		return nil
	}
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Sys().(*syscall.Stat_t).Ino
}

// stoppingDaemon starts a daemon in dir with a job still accounting (its
// wait group held until the returned release, its accounting bound ten
// seconds), stops it, and returns once its listeners are closed.
func stoppingDaemon(t *testing.T, dir string) (release func(), served <-chan error) {
	t.Helper()
	socket := filepath.Join(dir, "d.sock")
	cfg := configload.Snapshot{Values: map[string]configload.Value{"daemon.forced-grace-seconds": {Data: int64(10)}}}
	s := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid(), Config: cfg}
	s.jobWait.Add(1)
	var once bool
	release = func() {
		if !once {
			once = true
			s.jobWait.Done()
		}
	}
	t.Cleanup(release)
	stop, served := serveLocked(t, s)
	waitAnswers(t, socket)
	stop()
	waitRefuses(t, socket)
	return release, served
}

func TestAStartWaitsForAStoppingDaemon(t *testing.T) {
	dir := testsocket.Dir(t)
	socket := filepath.Join(dir, "d.sock")
	state := filepath.Join(dir, "state.json")
	release, firstServed := stoppingDaemon(t, dir)
	stoppingSocket := inode(t, socket)
	second := &Server{Socket: socket, StatePath: state, UID: os.Geteuid()}
	stopSecond, secondServed := serveLocked(t, second)
	// A window with the first still accounting: the second neither returns
	// nor touches the socket.
	time.Sleep(300 * time.Millisecond)
	select {
	case err := <-secondServed:
		t.Fatalf("the second daemon returned while the first stops: %v", err)
	default:
	}
	if answers(socket) {
		t.Fatal("a daemon answers while the first stops")
	}
	if inode(t, socket) != stoppingSocket {
		t.Fatal("the second daemon replaced the stopping daemon's socket")
	}
	release()
	if err := waitServed(t, firstServed, "the stopping daemon"); err != nil {
		t.Fatalf("the stopping daemon's Serve = %v", err)
	}
	waitAnswers(t, socket)
	// Its answer comes after its state file is written (the accept loop).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := Ping(ctx, socket, 1<<20); err != nil {
		t.Fatalf("the second daemon does not answer: %v", err)
	}
	// The first's removals came before the second took the lock: the
	// second's files stay.
	for _, p := range []string{socket, CredentialSocket(socket), state, LockPath(socket)} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("the second daemon's %s: %v", filepath.Base(p), err)
		}
	}
	stopSecond()
	if err := waitServed(t, secondServed, "the second daemon"); err != nil {
		t.Fatalf("the second daemon's Serve = %v", err)
	}
	if _, err := os.Lstat(LockPath(socket)); err != nil {
		t.Errorf("the lock file is removed at the exit: %v", err)
	}
}

func TestAStartRefusesAStoppingDaemonAtTheBound(t *testing.T) {
	dir := testsocket.Dir(t)
	socket := filepath.Join(dir, "d.sock")
	release, firstServed := stoppingDaemon(t, dir)
	cfg := configload.Snapshot{Values: map[string]configload.Value{"daemon.start-timeout": {Data: "1s"}}}
	second := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid(), Config: cfg}
	began := time.Now()
	_, secondServed := serveLocked(t, second)
	err := waitServed(t, secondServed, "the second daemon")
	elapsed := time.Since(began)
	if errorcodes.Of(err) != "daemon_already_running" || errorcodes.Text(err) != "daemon still stopping: pid "+strconv.Itoa(os.Getpid()) {
		t.Fatalf("the second daemon's Serve = %v", err)
	}
	if elapsed < time.Second {
		t.Errorf("refused after %v, before daemon.start-timeout", elapsed)
	}
	select {
	case err := <-firstServed:
		t.Fatalf("the stopping daemon returned before the refusal: %v", err)
	default:
	}
	release()
	if err := waitServed(t, firstServed, "the stopping daemon"); err != nil {
		t.Fatalf("the stopping daemon's Serve = %v", err)
	}
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		t.Errorf("the socket after both: %v", err)
	}
}

func TestAStartBesideARunningDaemonIsRefusedAtOnce(t *testing.T) {
	_, socket, _ := startTestServer(t)
	served := inode(t, socket)
	second := &Server{Socket: socket, StatePath: filepath.Join(filepath.Dir(socket), "state.json"), UID: os.Geteuid()}
	began := time.Now()
	_, secondServed := serveLocked(t, second)
	err := waitServed(t, secondServed, "the second daemon")
	if errorcodes.Of(err) != "daemon_already_running" || errorcodes.Text(err) != "daemon already running at "+socket {
		t.Fatalf("the second daemon's Serve = %v", err)
	}
	if elapsed := time.Since(began); elapsed > 2*time.Second {
		t.Errorf("refused after %v; a running daemon is refused at once", elapsed)
	}
	if inode(t, socket) != served {
		t.Error("the refused daemon replaced the running daemon's socket")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := Ping(ctx, socket, 1<<20); err != nil {
		t.Fatalf("the running daemon no longer answers: %v", err)
	}
}

// TestARunningDaemonWhoseLockFileWasRemovedKeepsItsSocket: d.lock removed
// under a running daemon (by hand, or aged out of a temporary directory), a
// start takes a new lock file at once; the socket's answer still refuses
// it, and the running daemon's files stay.
func TestARunningDaemonWhoseLockFileWasRemovedKeepsItsSocket(t *testing.T) {
	_, socket, _ := startTestServer(t)
	served := inode(t, socket)
	if err := os.Remove(LockPath(socket)); err != nil {
		t.Fatal(err)
	}
	second := &Server{Socket: socket, StatePath: filepath.Join(filepath.Dir(socket), "other-state.json"), UID: os.Geteuid()}
	_, secondServed := serveLocked(t, second)
	err := waitServed(t, secondServed, "the second daemon")
	if errorcodes.Of(err) != "daemon_already_running" || errorcodes.Text(err) != "daemon already running at "+socket {
		t.Fatalf("the second daemon's Serve = %v", err)
	}
	if inode(t, socket) != served {
		t.Error("the refused daemon replaced the running daemon's socket")
	}
	for _, p := range []string{CredentialSocket(socket), filepath.Join(filepath.Dir(socket), "state.json")} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("the running daemon's %s: %v", filepath.Base(p), err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := Ping(ctx, socket, 1<<20); err != nil {
		t.Fatalf("the running daemon no longer answers: %v", err)
	}
}

// TestAStartIsRefusedWhenTheHolderBeginsToAnswer: two launches at once,
// the first holding the lock and binding while the second waits; the second
// is refused as soon as the first answers, not at its bound.
func TestAStartIsRefusedWhenTheHolderBeginsToAnswer(t *testing.T) {
	dir := testsocket.Dir(t)
	socket := filepath.Join(dir, "d.sock")
	lock, err := takeLifetimeLock(context.Background(), nil, LockPath(socket), socket, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	cfg := configload.Snapshot{Values: map[string]configload.Value{"daemon.start-timeout": {Data: "10s"}}}
	second := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid(), Config: cfg}
	_, served := serveLocked(t, second)
	time.Sleep(300 * time.Millisecond)
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	began := time.Now()
	err = waitServed(t, served, "the second daemon")
	if errorcodes.Text(err) != "daemon already running at "+socket || time.Since(began) > 2*time.Second {
		t.Fatalf("after %v: %v", time.Since(began), err)
	}
}

// TestASignalEndsTheWaitForTheLock: a daemon waiting for the lock ends at
// a signal, as it would at its context's end, starting nothing.
func TestASignalEndsTheWaitForTheLock(t *testing.T) {
	dir := testsocket.Dir(t)
	socket := filepath.Join(dir, "d.sock")
	lock, err := takeLifetimeLock(context.Background(), nil, LockPath(socket), socket, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	signals := make(chan os.Signal, 1)
	cfg := configload.Snapshot{Values: map[string]configload.Value{"daemon.start-timeout": {Data: "10s"}}}
	var logged bytes.Buffer
	waiting := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid(), Config: cfg, Signals: signals, Logger: slog.New(slog.NewTextHandler(&logged, nil))}
	_, served := serveLocked(t, waiting)
	time.Sleep(100 * time.Millisecond)
	began := time.Now()
	signals <- syscall.SIGTERM
	if err := waitServed(t, served, "the waiting daemon"); err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if elapsed := time.Since(began); elapsed > time.Second {
		t.Errorf("ended %v after the signal", elapsed)
	}
	for _, p := range []string{socket, CredentialSocket(socket), filepath.Join(dir, "state.json")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("the waiting daemon made %s: %v", filepath.Base(p), err)
		}
	}
	if !strings.Contains(logged.String(), "code=daemon_stopped signal=terminated") {
		t.Errorf("log = %q", logged.String())
	}
}

// lockHolderMain is the test binary linked as daemon-lock-holder: it takes
// the lifetime lock at os.Args[1] for the socket os.Args[2], says so, and
// holds it until it is killed.
func lockHolderMain() int {
	lock, err := takeLifetimeLock(context.Background(), nil, os.Args[1], os.Args[2], 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("held")
	time.Sleep(time.Minute)
	lock.Close()
	return 0
}

func TestAKilledDaemonsLockIsFree(t *testing.T) {
	dir := testsocket.Dir(t)
	socket := filepath.Join(dir, "d.sock")
	holder := filepath.Join(t.TempDir(), "daemon-lock-holder")
	linkTestBinary(t, holder)
	cmd := exec.Command(holder, LockPath(socket), socket)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "held\n" {
		t.Fatalf("the holder said %q: %v", line, err)
	}
	_, err = takeLifetimeLock(context.Background(), nil, LockPath(socket), socket, 100*time.Millisecond)
	if want := "daemon still stopping: pid " + strconv.Itoa(cmd.Process.Pid); errorcodes.Text(err) != want {
		t.Fatalf("a take while it is held = %v, want %s", err, want)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil || !strings.Contains(err.Error(), "killed") {
		t.Fatalf("the holder's end = %v", err)
	}
	s := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid()}
	stop, served := serveLocked(t, s)
	waitAnswers(t, socket)
	stop()
	if err := waitServed(t, served, "the daemon"); err != nil {
		t.Fatalf("Serve = %v", err)
	}
}
