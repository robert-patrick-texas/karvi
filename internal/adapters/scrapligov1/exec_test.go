package scrapligov1

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/platform"
)

func execSession(t *testing.T, srv *fakedevice.Server) (*devsession.ExecSession, *ExecConnection) {
	t.Helper()
	conn, err := DialExec(context.Background(), dialRequest(srv, hostkey.Insecure, filepath.Join(t.TempDir(), "absent")))
	if err != nil {
		t.Fatal(err)
	}
	def, _ := platform.Builtin("linux")
	s := devsession.OpenExec(conn, devsession.ExecOptions{Definition: def})
	t.Cleanup(func() { s.Close() })
	return s, conn
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
	deadline := time.Now().Add(5 * time.Second)
	for len(srv.Signals()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := srv.Signals(); len(got) != 1 || got[0] != "KILL" || len(srv.LeftRunning()) != 0 {
		t.Fatalf("signals %q left running %q", got, srv.LeftRunning())
	}
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
