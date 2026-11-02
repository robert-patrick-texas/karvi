package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/credentialframe"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

// lockedLog is a daemon log the test reads while the server writes it.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// TestAcceptOutOfDescriptorsIsRetried: with the process out of file
// descriptors (EMFILE), a connection waiting on either socket cannot be
// accepted; the daemon logs each failure, retries, and serves both
// connections once descriptors are free again, still running.
func TestAcceptOutOfDescriptorsIsRetried(t *testing.T) {
	dir := testsocket.Dir(t)
	socket := filepath.Join(dir, "d.sock")
	var log lockedLog
	s := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid(), Logger: slog.New(slog.NewTextHandler(&log, nil))}
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx) }()
	t.Cleanup(func() {
		stop()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
		}
	})
	// The daemon answers once its start is done: the state file appears at
	// its rename, before the daemon opens the folder to sync it, so the file
	// alone does not say the start is over. The ping's connection is read to
	// its end, the daemon's descriptor for it closed, so that none of the
	// daemon's is freed under the limit below.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		err := pingToEnd(socket)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon did not answer: %v", err)
		}
	}
	// Both client sockets are made first; then the limit is set to the
	// lowest free descriptor, so the daemon's accept of either connection
	// gets EMFILE until the limit is restored.
	sockets := map[string]int{}
	for _, path := range []string{socket, CredentialSocket(socket)} {
		fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		sockets[path] = fd
	}
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	lowest, err := syscall.Dup(0)
	if err != nil {
		t.Fatal(err)
	}
	syscall.Close(lowest)
	restored := false
	restore := func() {
		if !restored {
			restored = true
			if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
				t.Fatal(err)
			}
		}
	}
	defer restore()
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: uint64(lowest), Max: limit.Max}); err != nil {
		t.Fatal(err)
	}
	for path, fd := range sockets {
		if err := syscall.Connect(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
			restore()
			t.Fatalf("connect %s: %v", path, err)
		}
	}
	retried := func() bool {
		for path := range sockets {
			if !strings.Contains(log.String(), `msg="accept failed, retrying" socket=`+path+" ") {
				return false
			}
		}
		return true
	}
	for deadline := time.Now().Add(5 * time.Second); !retried(); time.Sleep(10 * time.Millisecond) {
		select {
		case err := <-served:
			restore()
			t.Fatalf("Serve returned at a passing accept error: %v\n%s", err, log.String())
		default:
		}
		if time.Now().After(deadline) {
			restore()
			t.Fatalf("no retry of both accepts logged:\n%s", log.String())
		}
	}
	restore()
	conns := map[string]net.Conn{}
	for path, fd := range sockets {
		f := os.NewFile(uintptr(fd), path)
		conn, err := net.FileConn(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		conns[path] = conn
	}
	req, err := ipc.NewRequest("request-1", "ping", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ipc.Write(conns[socket], req, 1<<20); err != nil {
		t.Fatal(err)
	}
	var resp ipc.Response
	if err := ipc.Read(conns[socket], &resp, 1<<20); err != nil || resp.Error != nil || len(resp.Result) == 0 {
		t.Fatalf("ping after the retry: %+v %v", resp, err)
	}
	// The credential channel answers a frame that is not one with its
	// receipt.
	_ = conns[CredentialSocket(socket)].(*net.UnixConn).CloseWrite()
	receipt, err := credentialframe.ReadReceipt(conns[CredentialSocket(socket)])
	if err != nil || receipt.Accepted || len(receipt.Findings) == 0 || receipt.Findings[0].Code != "credential_frame_malformed" {
		t.Fatalf("credential channel after the retry: %+v %v", receipt, err)
	}
	select {
	case err := <-served:
		t.Fatalf("Serve returned: %v", err)
	default:
	}
	t.Logf("daemon log:\n%s", log.String())
}

// pingToEnd asks the daemon at socket for a ping and reads the connection
// to its end, which comes once the daemon has closed its side.
func pingToEnd(socket string) error {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req, err := ipc.NewRequest("request-0", "ping", map[string]any{})
	if err != nil {
		return err
	}
	if err := ipc.Write(conn, req, 1<<20); err != nil {
		return err
	}
	var resp ipc.Response
	if err := ipc.Read(conn, &resp, 1<<20); err != nil {
		return err
	}
	if resp.Error != nil || len(resp.Result) == 0 {
		return fmt.Errorf("ping answered %+v", resp)
	}
	_, err = io.Copy(io.Discard, conn)
	return err
}

// scriptedListener answers each accept with the next of its errors.
type scriptedListener struct{ errs []error }

func (l *scriptedListener) AcceptUnix() (*net.UnixConn, error) {
	err := l.errs[0]
	l.errs = l.errs[1:]
	return nil, err
}

// TestAFatalAcceptErrorStopsTheDaemon: passing accept errors are retried
// after a pause from 5 ms doubling, each logged; an error that does not
// pass is logged, cancels the daemon's context so that Serve's shutdown
// runs, and is returned for Serve to report.
func TestAFatalAcceptErrorStopsTheDaemon(t *testing.T) {
	accept := func(errno syscall.Errno) error {
		return &net.OpError{Op: "accept", Net: "unix", Err: os.NewSyscallError("accept4", errno)}
	}
	var log lockedLog
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Server{cancel: cancel, Logger: slog.New(slog.NewTextHandler(&log, nil))}
	l := &scriptedListener{errs: []error{accept(syscall.EMFILE), accept(syscall.ENFILE), accept(syscall.EBADF)}}
	var wg sync.WaitGroup
	err := s.acceptLoop(ctx, l, "/run/karvi/d.sock", &wg, nil)
	if !errors.Is(err, syscall.EBADF) || ctx.Err() == nil || len(l.errs) != 0 {
		t.Fatalf("acceptLoop = %v, context %v, %d accepts left", err, ctx.Err(), len(l.errs))
	}
	for _, want := range []string{
		`level=WARN msg="accept failed, retrying" socket=/run/karvi/d.sock error="accept unix: accept4: too many open files" retry_in=5ms`,
		`level=WARN msg="accept failed, retrying" socket=/run/karvi/d.sock error="accept unix: accept4: too many open files in system" retry_in=10ms`,
		`level=ERROR msg="accept failed, stopping" socket=/run/karvi/d.sock error="accept unix: accept4: bad file descriptor"`,
	} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("the log lacks %s:\n%s", want, log.String())
		}
	}
}
