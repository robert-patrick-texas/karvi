package native

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/platform"
)

// The two texts of a connection the device ended without a message.
const (
	endedReset  = "command_session_lost: the connection ended: connection reset by peer"
	endedClosed = "command_session_lost: the connection ended: closed by the device"
)

// endedCase is one way a connection ends under a command. want holds the
// texts the case may give, the socket's own account: a close reads as a
// reset when it met karvi's bytes in flight, and a reset reads as a close
// when a write took the socket's error before the read did. The bytes and
// the write are a keepalive's over the shell; over exec also x/crypto's
// own, a channel's end of input or close around each command, so there
// every case may read either way. TestEndText holds each text to its cause.
type endedCase struct {
	name      string
	way       string
	during    bool          // the device ends it while the command runs
	keepalive time.Duration // the interval, count 2; the next command comes after the count
	want      []string
}

// endedCases are the cases; exact holds a case without a keepalive to the
// text of its way.
func endedCases(exact bool) []endedCase {
	either := []string{endedReset, endedClosed}
	reset, closed := either, either
	if exact {
		reset, closed = []string{endedReset}, []string{endedClosed}
	}
	return []endedCase{
		{name: "reset between two commands", way: fakedevice.EndReset, want: reset},
		{name: "reset during a command", way: fakedevice.EndReset, during: true, want: reset},
		{name: "reset between two commands, the keepalive counting", way: fakedevice.EndReset, keepalive: 50 * time.Millisecond, want: either},
		{name: "close between two commands", way: fakedevice.EndClose, want: closed},
		{name: "close during a command", way: fakedevice.EndClose, during: true, want: closed},
		{name: "close between two commands, the keepalive counting", way: fakedevice.EndClose, keepalive: 50 * time.Millisecond, want: either},
	}
}

// ending is asked before the fake ends the connection; the function it
// returns waits for the end as x/crypto's client reports it.
func ending(t *testing.T, c *connection) func() {
	t.Helper()
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	done := make(chan struct{})
	go func() {
		_ = client.Wait()
		close(done)
	}()
	return func() {
		t.Helper()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the connection had not ended in 5s")
		}
	}
}

// endOnceReceived ends the fake's connections once it has taken line.
func endOnceReceived(t *testing.T, srv *fakedevice.Server, line, way string) {
	for deadline := time.Now().Add(5 * time.Second); !slices.Contains(srv.Lines(), line); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Errorf("the fake had not received %q in 5s", line)
			return
		}
	}
	srv.End(way)
}

// checkEnded is the result of a command on a connection the device ended:
// command_session_lost by the registry's values, naming why.
func checkEnded(t *testing.T, r platform.Result, want []string) {
	t.Helper()
	if r.ErrorCode != "command_session_lost" || r.ErrorCategory != "connection" || r.Retryable || r.Err == nil {
		t.Fatalf("result: code=%q category=%q retryable=%t err=%v", r.ErrorCode, r.ErrorCategory, r.Retryable, r.Err)
	}
	if !slices.Contains(want, r.Err.Error()) {
		t.Fatalf("message %q, want one of %q", r.Err, want)
	}
}

// pastTheCount waits until a keepalive of interval and count 2 that took the
// ended connection for a silent device would have said so.
func pastTheCount(interval time.Duration) { time.Sleep(5 * interval) }

// TestEndedConnectionNamesWhyOverTheShell:
// a command on a shell whose connection the device reset or closed is
// command_session_lost naming which, between two commands and during one,
// and whatever the keepalive counted since; the command after it is the
// session's own refusal.
func TestEndedConnectionNamesWhyOverTheShell(t *testing.T) {
	for _, tc := range endedCases(true) {
		t.Run(tc.name, func(t *testing.T) {
			srv := startFake(t, fakedevice.Options{Enable: "en", Delay: map[string]time.Duration{"show slow": 3 * time.Second}})
			req := dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent"))
			req.KeepaliveInterval, req.KeepaliveCountMax = tc.keepalive, 2
			s, err := Dial(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			ended := ending(t, s.(*connection))
			session := openSession(t, s, "cisco_ios")
			t.Cleanup(func() { session.Close() })
			if r := session.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 5 * time.Second}); r.Err != nil {
				t.Fatalf("the first command: %+v", r)
			}
			if tc.during {
				go endOnceReceived(t, srv, "show slow", tc.way)
				checkEnded(t, session.Execute(context.Background(), platform.Command{Text: "show slow", Timeout: 8 * time.Second}), tc.want)
				return
			}
			srv.End(tc.way)
			ended()
			pastTheCount(tc.keepalive)
			checkEnded(t, session.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 5 * time.Second}), tc.want)
			checkEnded(t, session.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 5 * time.Second}),
				[]string{"command_session_lost: the device session ended before the next command; no replacement login was attempted"})
		})
	}
}

// TestEndedConnectionNamesWhyOverExec:
// the same over exec, where a command between two is refused at its
// channel's opening and one under way at its wait.
func TestEndedConnectionNamesWhyOverExec(t *testing.T) {
	for _, tc := range endedCases(false) {
		t.Run(tc.name, func(t *testing.T) {
			srv := startFake(t, fakedevice.Options{Persona: fakedevice.PersonaLinux, Delay: map[string]time.Duration{"slow": 3 * time.Second}})
			req := dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent"))
			req.KeepaliveInterval, req.KeepaliveCountMax = tc.keepalive, 2
			conn, err := DialExec(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			ended := ending(t, conn.c)
			def, _ := platform.Builtin("linux")
			session := devsession.OpenExec(conn, devsession.ExecOptions{Definition: def})
			t.Cleanup(func() { session.Close() })
			if r := session.Execute(context.Background(), platform.Command{Text: "uname -snrm", Timeout: 5 * time.Second}); r.Err != nil {
				t.Fatalf("the first command: %+v", r)
			}
			if tc.during {
				go endOnceReceived(t, srv, "exec: slow", tc.way)
				checkEnded(t, session.Execute(context.Background(), platform.Command{Text: "slow", Timeout: 8 * time.Second}), tc.want)
				return
			}
			srv.End(tc.way)
			ended()
			pastTheCount(tc.keepalive)
			checkEnded(t, session.Execute(context.Background(), platform.Command{Text: "uname -snrm", Timeout: 5 * time.Second}), tc.want)
		})
	}
}

// TestShellEndedOnALiveConnectionKeepsItsText:
// a shell the device ends with the connection up (its "exit") has no
// connection's end to name: the session's own text, after the wait for one.
func TestShellEndedOnALiveConnectionKeepsItsText(t *testing.T) {
	srv := startFake(t, fakedevice.Options{Enable: "en"})
	s, err := Dial(context.Background(), dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent")))
	if err != nil {
		t.Fatal(err)
	}
	session := openSession(t, s, "cisco_ios")
	t.Cleanup(func() { session.Close() })
	checkEnded(t, session.Execute(context.Background(), platform.Command{Text: "exit", Timeout: 5 * time.Second}),
		[]string{"command_session_lost: the device session ended"})
}

// TestOwnShutIsNoEndedConnection:
// karvi's own shut is not the device's end: no cause is kept, and
// endedError answers nil.
func TestOwnShutIsNoEndedConnection(t *testing.T) {
	srv := startFake(t, fakedevice.Options{Enable: "en"})
	s, err := Dial(context.Background(), dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent")))
	if err != nil {
		t.Fatal(err)
	}
	c := s.(*connection)
	c.shut()
	select {
	case <-c.left:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection had not ended 5s after the shut")
	}
	if err := c.endedError(); err != nil {
		t.Fatalf("endedError after karvi's own shut: %v", err)
	}
}

// TestEndText:
// a close with no message in karvi's words, a socket's error in the
// kernel's words without the addresses, anything else as it is; the start
// kept up to endTextMax bytes, cut at a character's boundary.
func TestEndText(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 22}
	long := `ssh: disconnect, reason 11: "` + strings.Repeat("x", 5000) + `"`
	wide := "a" + strings.Repeat("é", 2000)
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"a close", io.EOF, "closed by the device"},
		{"a reset", &net.OpError{Op: "read", Net: "tcp", Source: addr, Addr: addr, Err: os.NewSyscallError("read", syscall.ECONNRESET)}, "connection reset by peer"},
		{"a socket error that is no system call's", &net.OpError{Op: "read", Net: "tcp", Source: addr, Addr: addr, Err: net.ErrClosed}, "use of closed network connection"},
		{"a disconnect", errors.New(`ssh: disconnect, reason 11: "idle timeout"`), `ssh: disconnect, reason 11: "idle timeout"`},
		{"at the bound", errors.New(strings.Repeat("x", endTextMax)), strings.Repeat("x", endTextMax)},
		{"past the bound", errors.New(long), long[:endTextMax] + "..."},
		{"past the bound inside a character", errors.New(wide), wide[:endTextMax-1] + "..."},
	} {
		got := endText(tc.err)
		if got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("%s: the text is not valid UTF-8", tc.name)
		}
	}
}

// drain reads the stream to its end and sends the error that ended it.
func drain(s devsession.Stream) <-chan error {
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := s.Read(buf); err != nil {
				done <- err
				return
			}
		}
	}()
	return done
}

// TestShellEndWaitsOnceAndNeverInClose:
// a shell the device ends with the connection up (the fake's "exit") is
// marked ended before the wait for the connection's end, so a Close at that
// moment returns at once; the read then ends EOF after the one wait, and a
// write after it does not wait again.
func TestShellEndWaitsOnceAndNeverInClose(t *testing.T) {
	open := func() (devsession.Stream, <-chan error) {
		srv := startFake(t, fakedevice.Options{Enable: "en"})
		s, err := Dial(context.Background(), dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent")))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		ended := drain(s)
		if _, err := s.Write([]byte("exit\n")); err != nil {
			t.Fatal(err)
		}
		return s, ended
	}
	within := func(what string, f func()) {
		t.Helper()
		started := time.Now()
		f()
		if elapsed := time.Since(started); elapsed > endWait/2 {
			t.Errorf("%s took %s: it waited for the connection's end", what, elapsed)
		}
	}
	awaitEnd := func(ended <-chan error) {
		t.Helper()
		select {
		case err := <-ended:
			if err != io.EOF {
				t.Fatalf("the read ended %v, want EOF", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the read had not ended in 5s")
		}
	}

	s, ended := open()
	within("Close as the shell ends", func() { s.Close() })
	awaitEnd(ended)

	s, ended = open()
	awaitEnd(ended)
	within("a write after the shell's end", func() {
		if _, err := s.Write([]byte("show clock\n")); err == nil {
			t.Error("a write after the shell's end succeeded")
		}
	})
}
