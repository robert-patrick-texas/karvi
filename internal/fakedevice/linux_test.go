package fakedevice

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func startLinux(t *testing.T, opts Options) *Server {
	t.Helper()
	opts.Persona = PersonaLinux
	srv, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

func connect(t *testing.T, srv *Server) *ssh.Client {
	t.Helper()
	conn, err := ssh.Dial("tcp", srv.Addr(), &ssh.ClientConfig{User: "netops", Auth: []ssh.AuthMethod{ssh.Password("pw")}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// run is one exec channel on conn: its two streams and Wait's error.
func run(t *testing.T, conn *ssh.Client, command string) (string, string, error) {
	t.Helper()
	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var stdout, stderr bytes.Buffer
	sess.Stdout, sess.Stderr = &stdout, &stderr
	err = sess.Run(command)
	return stdout.String(), stderr.String(), err
}

// status is the exit status and signal Wait reported: 0 and "" for
// success, -1 for no status.
func status(t *testing.T, err error) (int, string) {
	t.Helper()
	var exit *ssh.ExitError
	var missing *ssh.ExitMissingError
	switch {
	case err == nil:
		return 0, ""
	case errors.As(err, &exit):
		return exit.ExitStatus(), exit.Signal()
	case errors.As(err, &missing):
		return -1, ""
	}
	t.Fatalf("not an exit: %v", err)
	return 0, ""
}

// TestLinuxExecTable: each row of the exec table on one connection, a
// channel per command, as both transports' exec will run them; the lines
// are recorded as exec requests and the connection's channels counted.
func TestLinuxExecTable(t *testing.T) {
	srv := startLinux(t, Options{BigLines: 3})
	conn := connect(t, srv)
	cases := []struct {
		command, stdout, stderr string
		status                  int
		signal                  string
	}{
		{"uname -snrm", "Linux fake 6.8.0-0-generic x86_64\n", "", 0, ""},
		{"ip -br address", "lo               UNKNOWN        127.0.0.1/8 ::1/128 \neth0             UP             192.0.2.10/24 2001:db8::10/64 fe80::1/64 \n", "", 0, ""},
		{"fail 3", "", "failing with 3\n", 3, ""},
		{"fail 300", "", "failing with 300\n", 44, ""},
		{"both", "to stdout\n", "to stderr\n", 0, ""},
		{"big", "line 000001 " + strings.Repeat("x", 60) + "\nline 000002 " + strings.Repeat("x", 60) + "\nline 000003 " + strings.Repeat("x", 60) + "\n", "", 0, ""},
		{"signal TERM", "", "", 143, "TERM"},
		{"nostatus", "", "", -1, ""},
		{"sudo -n id -u", "0\n", "", 0, ""},
		{"ls /nonexistent", "", "sh: 1: ls: not found\n", 127, ""},
		{"slow", "slow output\n", "", 0, ""},
	}
	var want []string
	for _, c := range cases {
		stdout, stderr, err := run(t, conn, c.command)
		code, signal := status(t, err)
		if stdout != c.stdout || stderr != c.stderr || code != c.status || signal != c.signal {
			t.Errorf("%s: stdout %q stderr %q status %d signal %q; want %q %q %d %q", c.command, stdout, stderr, code, signal, c.stdout, c.stderr, c.status, c.signal)
		}
		want = append(want, "exec: "+c.command)
	}
	if got := srv.Lines(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", got, want)
	}
	if got := srv.Channels(); len(got) != 1 || got[0] != len(cases) {
		t.Fatalf("channels %v, want [%d]", got, len(cases))
	}
	if srv.Sessions() != 0 {
		t.Fatalf("exec opened %d shells", srv.Sessions())
	}
}

// TestLinuxSudoAsks: under Options.SudoAsks sudo -n refuses as sudo does.
func TestLinuxSudoAsks(t *testing.T) {
	srv := startLinux(t, Options{SudoAsks: true})
	stdout, stderr, err := run(t, connect(t, srv), "sudo -n id -u")
	if code, _ := status(t, err); stdout != "" || stderr != "sudo: a password is required\n" || code != 1 {
		t.Fatalf("stdout %q stderr %q status %d", stdout, stderr, code)
	}
}

// TestLinuxSlowEndedByKill: a KILL signal request ends the slow command by
// that signal, and the connection serves the next command; the signal is
// recorded and nothing is left running.
func TestLinuxSlowEndedByKill(t *testing.T) {
	srv := startLinux(t, Options{Delay: map[string]time.Duration{"slow": time.Minute}})
	conn := connect(t, srv)
	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start("slow"); err != nil {
		t.Fatal(err)
	}
	if err := sess.Signal(ssh.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if code, signal := status(t, sess.Wait()); code != 137 || signal != "KILL" {
		t.Fatalf("status %d signal %q, want 137 KILL", code, signal)
	}
	if stdout, _, err := run(t, conn, "uname -snrm"); err != nil || stdout == "" {
		t.Fatalf("the next command: %q %v", stdout, err)
	}
	if got := srv.Signals(); len(got) != 1 || got[0] != "KILL" {
		t.Fatalf("signals %q", got)
	}
	if got := srv.LeftRunning(); len(got) != 0 {
		t.Fatalf("left running %q", got)
	}
}

// TestLinuxSlowLeftRunning: a channel closed under the slow command, no
// signal sent, leaves it running, recorded.
func TestLinuxSlowLeftRunning(t *testing.T) {
	srv := startLinux(t, Options{Delay: map[string]time.Duration{"slow": time.Minute}})
	conn := connect(t, srv)
	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start("slow"); err != nil {
		t.Fatal(err)
	}
	sess.Close()
	deadline := time.Now().Add(5 * time.Second)
	for len(srv.LeftRunning()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the slow command was not recorded as left running")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := srv.LeftRunning(); len(got) != 1 || got[0] != "slow" {
		t.Fatalf("left running %q", got)
	}
}

// TestLinuxExecRecordsPTYRequests: a pty asked for on an exec channel is
// recorded as on a shell's; the IOS XE persona still refuses exec.
func TestLinuxExecRecordsPTYRequests(t *testing.T) {
	srv := startLinux(t, Options{})
	sess, err := connect(t, srv).NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.RequestPty("xterm", 0, 0, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Run("uname -snrm"); err != nil {
		t.Fatal(err)
	}
	if got := srv.PTYRequests(); len(got) != 1 || got[0].Term != "xterm" {
		t.Fatalf("pty requests %+v", got)
	}

	ios, err := Start(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer ios.Close()
	if _, _, err := run(t, connect(t, ios), "show version"); err == nil {
		t.Fatal("the IOS XE persona ran an exec request")
	}
	if got := ios.Lines(); len(got) != 1 || got[0] != "exec: show version" {
		t.Fatalf("lines %q", got)
	}
}

// TestLinuxShell: the shell for linux_shell answers the table on the
// terminal, both streams together, bash's message for a command not found,
// and exit ends it with status 0.
func TestLinuxShell(t *testing.T) {
	srv := startLinux(t, Options{})
	c := dial(t, srv)
	c.expect("netops@fake:~$ ")
	c.send("both\n")
	if got := c.expect("netops@fake:~$ "); got != "both\r\nto stdout\r\nto stderr\r\nnetops@fake:~$ " {
		t.Fatalf("both: %q", got)
	}
	c.send("nosuch\n")
	if got := c.expect("$ "); got != "nosuch\r\n-bash: nosuch: command not found\r\nnetops@fake:~$ " {
		t.Fatalf("not found: %q", got)
	}
	c.send("exit\n")
	if got := c.expect("logout\r\n"); got != "exit\r\nlogout\r\n" {
		t.Fatalf("exit: %q", got)
	}
	if got := strings.Join(srv.Lines(), "|"); got != "both|nosuch|exit" {
		t.Fatalf("lines %q", got)
	}
	if srv.Sessions() != 1 {
		t.Fatalf("sessions %d", srv.Sessions())
	}
}

// TestLinuxShellDecorations: under Options.Decorations the shell writes
// what bash does: bracketed paste on before each prompt and off after each
// line, the window title, and the coloured prompt (the bytes the renderer's
// tests carry, captured from a host).
func TestLinuxShellDecorations(t *testing.T) {
	srv := startLinux(t, Options{Decorations: true, Hostname: "dev"})
	c := dial(t, srv)
	prompt := "\x1b[?2004h\x1b]0;netops@dev: ~\x07\x1b[01;32mnetops@dev\x1b[00m:\x1b[01;34m~\x1b[00m$ "
	if got := c.expect("$ "); got != prompt {
		t.Fatalf("first prompt %q", got)
	}
	c.send("uname -snrm\n")
	if got := c.expect("$ "); got != "uname -snrm\r\n\x1b[?2004l\rLinux dev 6.8.0-0-generic x86_64\r\n"+prompt {
		t.Fatalf("a command: %q", got)
	}
}
