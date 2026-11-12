package native

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/platform"
)

func execSession(t *testing.T, srv *fakedevice.Server) (*devsession.ExecSession, *ExecConnection) {
	t.Helper()
	return execSessionAt(t, srv, srv.Port())
}

// execSessionAt is execSession through another port, a relay's. No
// keepalive is set (dialRequest), so none ends a test's connection.
func execSessionAt(t *testing.T, srv *fakedevice.Server, port int) (*devsession.ExecSession, *ExecConnection) {
	t.Helper()
	req := dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent"))
	req.Port = port
	conn, err := DialExec(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	def, _ := platform.Builtin("linux")
	s := devsession.OpenExec(conn, devsession.ExecOptions{Definition: def})
	t.Cleanup(func() { s.Close() })
	return s, conn
}

// freezingRelay forwards a local port to the fake's, each direction's bytes
// after delay; freeze stops both directions and closes nothing, so what
// karvi sends is taken and never answered, as by a device stopped under it.
func freezingRelay(t *testing.T, to int, delay time.Duration) (int, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	var frozen atomic.Bool
	forward := func(dst, src net.Conn) {
		buf := make([]byte, 32<<10)
		for {
			n, err := src.Read(buf)
			if frozen.Load() {
				return // what was read is dropped, and nothing more is read
			}
			if n > 0 {
				time.Sleep(delay)
				if _, werr := dst.Write(buf[:n]); werr != nil {
					err = werr
				}
			}
			if err != nil {
				dst.Close()
				return
			}
		}
	}
	go func() {
		for {
			in, err := l.Accept()
			if err != nil {
				return
			}
			out, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", to))
			if err != nil {
				in.Close()
				continue
			}
			mu.Lock()
			conns = append(conns, in, out)
			mu.Unlock()
			go forward(out, in)
			go forward(in, out)
		}
	}()
	t.Cleanup(func() {
		l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close() // ends a forward still reading
		}
	})
	return l.Addr().(*net.TCPAddr).Port, func() { frozen.Store(true) }
}

// cutBound is inside devsession's 2s grace for a timeout and outside the
// 500ms it gives the context's end.
const cutBound = 1500 * time.Millisecond

// executeWithin runs the command and fails when Execute has not returned in
// bound; the connection is then closed, which releases Execute and the
// cleanup's Close behind it.
func executeWithin(t *testing.T, bound time.Duration, ctx context.Context, s *devsession.ExecSession, conn *ExecConnection, command platform.Command) platform.Result {
	t.Helper()
	done := make(chan platform.Result, 1)
	go func() { done <- s.Execute(ctx, command) }()
	select {
	case r := <-done:
		return r
	case <-time.After(bound):
		conn.Close()
		t.Fatalf("%s: Execute had not returned in %s", command.Text, bound)
		return platform.Result{}
	}
}

// awaitSignal waits for the fake's first signal request.
func awaitSignal(srv *fakedevice.Server) {
	deadline := time.Now().Add(5 * time.Second)
	for len(srv.Signals()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

// TestExecOverTheFake: the exec table through karvi's connection, one
// connection with a channel per command and no PTY or shell; x/crypto's
// account of each end: a status, a named signal, none.
func TestExecOverTheFake(t *testing.T) {
	srv := startFake(t, fakedevice.Options{Persona: fakedevice.PersonaLinux})
	s, conn := execSession(t, srv)
	if conn.AuthMethod() != "keyboard-interactive" {
		t.Fatalf("auth %q", conn.AuthMethod())
	}
	for _, tc := range []struct {
		command, code, stdout, stderr, signal string
		status                                int // -1 for none
	}{
		{"uname -snrm", "", "Linux fake 6.8.0-0-generic x86_64\n", "", "", 0},
		{"both", "", "to stdout\n", "to stderr\n", "", 0},
		{"fail 3", "command_exit_nonzero", "", "failing with 3\n", "", 3},
		{"signal TERM", "command_exit_signal", "", "", "TERM", -1},
		{"nostatus", "command_exit_missing", "", "", "", -1},
		{"ls /nonexistent", "command_exit_nonzero", "", "sh: 1: ls: not found\n", "", 127},
	} {
		r := s.Execute(context.Background(), platform.Command{Text: tc.command, Timeout: 5 * time.Second})
		status := -1
		if r.Exec != nil && r.Exec.ExitStatus != nil {
			status = *r.Exec.ExitStatus
		}
		if r.ErrorCode != tc.code || string(r.Output) != tc.stdout || r.Exec == nil || string(r.Exec.Stderr) != tc.stderr || r.Exec.ExitSignal != tc.signal || status != tc.status {
			t.Errorf("%s: code %q stdout %q exec %+v status %d", tc.command, r.ErrorCode, r.Output, r.Exec, status)
		}
	}
	if !s.Usable() {
		t.Fatal("the session is not usable")
	}
	if got := srv.Channels(); len(got) != 1 || got[0] != 6 || len(srv.PTYRequests()) != 0 || srv.Sessions() != 0 {
		t.Fatalf("channels %v ptys %d shells %d", got, len(srv.PTYRequests()), srv.Sessions())
	}
}

// TestExecTimeoutKillsTheCommand: at the command timeout karvi asks for
// KILL and closes the channel; the fake sees the signal, nothing is left
// running, and the connection serves the next command.
func TestExecTimeoutKillsTheCommand(t *testing.T) {
	srv := startFake(t, fakedevice.Options{Persona: fakedevice.PersonaLinux, Delay: map[string]time.Duration{"slow": time.Minute}})
	s, _ := execSession(t, srv)
	r := s.Execute(context.Background(), platform.Command{Text: "slow", Timeout: 300 * time.Millisecond})
	if r.ErrorCode != "command_timeout" {
		t.Fatalf("slow: %q %v", r.ErrorCode, r.Err)
	}
	if r := s.Execute(context.Background(), platform.Command{Text: "uname -snrm", Timeout: 5 * time.Second}); r.Err != nil || !strings.HasPrefix(string(r.Output), "Linux fake") {
		t.Fatalf("the next command: %q %v", r.Output, r.Err)
	}
	awaitSignal(srv)
	if got := srv.Signals(); len(got) != 1 || got[0] != "KILL" || len(srv.LeftRunning()) != 0 {
		t.Fatalf("signals %q left running %q", got, srv.LeftRunning())
	}
}

// TestExecCancelKillsTheCommand: at the context's end karvi asks for KILL
// and then closes the connection, in that order: the fake sees the signal,
// nothing is left running, and Execute does not wait for the device.
func TestExecCancelKillsTheCommand(t *testing.T) {
	srv := startFake(t, fakedevice.Options{Persona: fakedevice.PersonaLinux, Delay: map[string]time.Duration{"slow": time.Minute}})
	s, conn := execSession(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(300*time.Millisecond, cancel)
	r := executeWithin(t, cutBound, ctx, s, conn, platform.Command{Text: "slow", Timeout: time.Minute})
	if !errors.Is(r.Err, context.Canceled) || r.ErrorCode != "" || r.Exec == nil || s.Usable() {
		t.Fatalf("slow: %q %v exec %+v usable %t", r.ErrorCode, r.Err, r.Exec, s.Usable())
	}
	awaitSignal(srv)
	if got := srv.Signals(); len(got) != 1 || got[0] != "KILL" || len(srv.LeftRunning()) != 0 {
		t.Fatalf("signals %q left running %q", got, srv.LeftRunning())
	}
}

// TestExecCancelWhileTheChannelOpens: a cancel landing in a start a device
// is answering, 10ms away each way, waits for the start, so that the
// command the device takes is asked to end before the connection is
// closed: the fake sees KILL and nothing is left running, wherever in the
// start the cancel lands (the open in flight, then the exec request).
func TestExecCancelWhileTheChannelOpens(t *testing.T) {
	for _, after := range []time.Duration{10 * time.Millisecond, 30 * time.Millisecond} {
		t.Run(after.String(), func(t *testing.T) {
			srv := startFake(t, fakedevice.Options{Persona: fakedevice.PersonaLinux, Delay: map[string]time.Duration{"slow": time.Minute}})
			port, _ := freezingRelay(t, srv.Port(), 10*time.Millisecond)
			s, conn := execSessionAt(t, srv, port)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			time.AfterFunc(after, cancel)
			r := executeWithin(t, cutBound, ctx, s, conn, platform.Command{Text: "slow", Timeout: time.Minute})
			if !errors.Is(r.Err, context.Canceled) || r.ErrorCode != "" || r.Exec == nil || s.Usable() {
				t.Fatalf("slow: %q %v exec %+v usable %t", r.ErrorCode, r.Err, r.Exec, s.Usable())
			}
			awaitSignal(srv)
			if got := srv.Signals(); len(got) != 1 || got[0] != "KILL" || len(srv.LeftRunning()) != 0 {
				t.Fatalf("signals %q left running %q", got, srv.LeftRunning())
			}
		})
	}
}

// TestExecGivenUpOnASilentDevice: a device that stops answering under a
// running command never closes its channel. A cancel ends the command
// 500ms on, and its timeout 2s on, each by closing the connection, the
// timeout's message saying so; no keepalive does it.
func TestExecGivenUpOnASilentDevice(t *testing.T) {
	silent := func(t *testing.T) (*devsession.ExecSession, *ExecConnection, func()) {
		t.Helper()
		srv := startFake(t, fakedevice.Options{Persona: fakedevice.PersonaLinux, Delay: map[string]time.Duration{"slow": time.Minute}})
		port, freeze := freezingRelay(t, srv.Port(), 0)
		s, conn := execSessionAt(t, srv, port)
		return s, conn, func() {
			// Frozen once the fake has the command and its answer to the
			// exec request, which it writes next, has crossed the relay.
			for deadline := time.Now().Add(5 * time.Second); len(srv.Lines()) == 0 && time.Now().Before(deadline); {
				time.Sleep(10 * time.Millisecond)
			}
			time.Sleep(50 * time.Millisecond)
			freeze()
		}
	}
	t.Run("cancel", func(t *testing.T) {
		t.Parallel()
		s, conn, freeze := silent(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { freeze(); time.AfterFunc(100*time.Millisecond, cancel) }()
		r := executeWithin(t, cutBound, ctx, s, conn, platform.Command{Text: "slow", Timeout: time.Minute})
		if !errors.Is(r.Err, context.Canceled) || r.ErrorCode != "" || r.Exec == nil || s.Usable() {
			t.Fatalf("code %q err %v exec %+v usable %t", r.ErrorCode, r.Err, r.Exec, s.Usable())
		}
	})
	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		s, conn, freeze := silent(t)
		go freeze()
		r := executeWithin(t, 5*time.Second, context.Background(), s, conn, platform.Command{Text: "slow", Timeout: 500 * time.Millisecond})
		if r.ErrorCode != "command_timeout" || r.Exec == nil || s.Usable() || r.Err.Error() != "command timed out after 500ms; the device did not close the channel in 2s and the connection was closed" {
			t.Fatalf("code %q err %v exec %+v usable %t", r.ErrorCode, r.Err, r.Exec, s.Usable())
		}
	})
}

// TestExecStartOnASilentDevice: a device that stops answering holds the
// next command's channel open; the context's end and the command's timeout
// each end the start by closing the connection, with no keepalive to do it.
func TestExecStartOnASilentDevice(t *testing.T) {
	silent := func(t *testing.T) (*devsession.ExecSession, *ExecConnection) {
		t.Helper()
		srv := startFake(t, fakedevice.Options{Persona: fakedevice.PersonaLinux})
		port, freeze := freezingRelay(t, srv.Port(), 0)
		s, conn := execSessionAt(t, srv, port)
		if r := s.Execute(context.Background(), platform.Command{Text: "uname -snrm", Timeout: 5 * time.Second}); r.Err != nil {
			t.Fatalf("the first command: %q %v", r.ErrorCode, r.Err)
		}
		freeze()
		return s, conn
	}
	t.Run("cancel", func(t *testing.T) {
		s, conn := silent(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		time.AfterFunc(100*time.Millisecond, cancel)
		r := executeWithin(t, cutBound, ctx, s, conn, platform.Command{Text: "uname -snrm", Timeout: time.Minute})
		if !errors.Is(r.Err, context.Canceled) || r.ErrorCode != "" || r.Exec != nil || s.Usable() {
			t.Fatalf("code %q err %v exec %+v usable %t", r.ErrorCode, r.Err, r.Exec, s.Usable())
		}
	})
	t.Run("timeout", func(t *testing.T) {
		s, conn := silent(t)
		r := executeWithin(t, cutBound, context.Background(), s, conn, platform.Command{Text: "uname -snrm", Timeout: 300 * time.Millisecond})
		if r.ErrorCode != "command_timeout" || r.ErrorCategory != "timeout" || r.Exec != nil || s.Usable() {
			t.Fatalf("code %q category %q err %v exec %+v usable %t", r.ErrorCode, r.ErrorCategory, r.Err, r.Exec, s.Usable())
		}
		if !strings.Contains(r.Err.Error(), "before it started") {
			t.Fatalf("message %q", r.Err)
		}
	})
}

// TestExecRefusedByADevice: a device that refuses the exec request (the
// IOS XE persona) is ssh_session_channel_refused, and the session ends.
func TestExecRefusedByADevice(t *testing.T) {
	srv := startFake(t, fakedevice.Options{})
	s, _ := execSession(t, srv)
	r := s.Execute(context.Background(), platform.Command{Text: "show version", Timeout: 5 * time.Second})
	if r.ErrorCode != "ssh_session_channel_refused" || r.Exec != nil || s.Usable() {
		t.Fatalf("code %q exec %+v usable %t", r.ErrorCode, r.Exec, s.Usable())
	}
}
