package systemssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey/sshkeytest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
	"github.com/robert-patrick-texas/karvi/platform"
)

// The master's lines as OpenSSH 9.6 writes them at DEBUG1 (captured against
// the fake): the method, each command's request and end, the refusal's one
// sign, and the debug lines dropped while every other line reaches the
// diagnostics.
func TestMasterLines(t *testing.T) {
	var diagnostics synchronizedBuffer
	m := newMasterLines(&diagnostics)
	write := func(text string) {
		t.Helper()
		if _, err := m.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	write("debug1: Server accepts key: /tmp/k ED25519 SHA256:x explicit\nAuthenticated to 127.0.0.1 ([127.0.0.1]:45853) using \"publickey\".\n")
	command := func(lines ...string) commandLines {
		t.Helper()
		m.begin()
		write("debug1: multiplexing control connection\ndebug1: channel 1: new mux-control [mux-control] (inactive timeout: 0)\n")
		write("debug1: channel 2: new session [client-session] (inactive timeout: 0)\ndebug1: Sending command: x\n")
		for _, l := range lines {
			write(l + "\n")
		}
		// A line arrives in pieces.
		write("debug1: channel 2: free: cli")
		write("ent-session, nchannels 3\ndebug1: channel 1: free: mux-control, nchannels 2\n")
		return m.settle(time.Second)
	}
	if c := command("debug1: client_input_channel_req: channel 2 rtype eow@openssh.com reply 0", "debug1: client_input_channel_req: channel 2 rtype exit-status reply 0"); !c.status || c.signal || c.refused || !c.freed {
		t.Fatalf("exit-status: %+v", c)
	}
	if c := command("debug1: client_input_channel_req: channel 2 rtype exit-signal reply 0"); c.status || !c.signal || !c.freed {
		t.Fatalf("exit-signal: %+v", c)
	}
	if c := command(); c.status || c.signal || c.refused || !c.freed {
		t.Fatalf("neither: %+v", c)
	}
	if c := command("channel 2: chan_read_failed for istate 3"); !c.refused {
		t.Fatalf("refused: %+v", c)
	}
	// A late line of another channel is not the command's.
	if c := command("debug1: client_input_channel_req: channel 3 rtype exit-status reply 0"); c.status {
		t.Fatalf("another channel's status: %+v", c)
	}
	write("Connection to 127.0.0.1 closed by remote host.\nTransferred: sent 2152, received 1404 bytes, in 1.0 seconds\ndebug1: Exit status -1")
	m.begin()
	m.end()
	if c := m.settle(time.Second); c.freed || c.channel != "" {
		t.Fatalf("after the end: %+v", c)
	}
	if m.Method() != "publickey" {
		t.Fatalf("method %q", m.Method())
	}
	if got := diagnostics.String(); got != "Connection to 127.0.0.1 closed by remote host.\nTransferred: sent 2152, received 1404 bytes, in 1.0 seconds\n" {
		t.Fatalf("diagnostics %q", got)
	}
}

// execDriver is a prepared exec driver over this host's OpenSSH client to
// the fake, logging in by a key the fake authorizes.
func execDriver(t *testing.T, opts fakedevice.Options) (*Driver, *fakedevice.Server) {
	d, srv, _ := execDriverRelayed(t, opts)
	return d, srv
}

// execDriverRelayed is execDriver through a TCP relay whose cut ends every
// connection, as a device gone does.
func execDriverRelayed(t *testing.T, opts fakedevice.Options) (*Driver, *fakedevice.Server, func()) {
	t.Helper()
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no OpenSSH client")
	}
	key, _ := sshkeytest.Ed25519(t, "")
	opts.AuthorizedKeys = sshkeytest.Authorized(t, key)
	srv, err := fakedevice.Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	port, cut := relay(t, srv.Port())
	dir := testsocket.Dir(t)
	keyPath := filepath.Join(dir, "k")
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(dir, "c")
	if err := os.Mkdir(control, 0o700); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{"ssh.include-user-config=false", "audit.journald-required=false", "ssh.host-key-policy=accept-new", `ssh.known-hosts-file="` + filepath.Join(dir, "kh") + `"`}})
	if err != nil {
		t.Fatal(err)
	}
	def, _ := platform.Builtin("linux")
	factory := Factory{Binary: ssh, Config: cfg, Home: home, BaseDir: home, ScratchDir: dir, ControlRoot: control, AskpassPath: "/bin/true"}
	opened, err := factory.Open(context.Background(), platform.OpenRequest{
		Address: "127.0.0.1", Port: uint16(port), Username: "netops", Keys: []string{keyPath},
		Definition: def, Channel: platform.ChannelExec,
		Metadata: map[string]string{"canonical_name": "fake"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := opened.(*Driver)
	t.Cleanup(func() { d.Close() })
	if err := d.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	return d, srv, cut
}

// relay forwards a local port to the fake's; cut closes every connection
// through it.
func relay(t *testing.T, to int) (int, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
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
			go func() { _, _ = io.Copy(out, in); out.Close() }()
			go func() { _, _ = io.Copy(in, out); in.Close() }()
		}
	}()
	cut := func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	}
	t.Cleanup(func() { l.Close(); cut() })
	return l.Addr().(*net.TCPAddr).Port, cut
}

// TestExecThroughTheMaster: the exec table through one master, a client per
// command and no PTY or shell; a 255 decided by the master's lines: the
// command's own, a signal (unnamed), a channel closed without a status. The
// method is the master's, and its socket goes at the end.
func TestExecThroughTheMaster(t *testing.T) {
	d, srv := execDriver(t, fakedevice.Options{Persona: fakedevice.PersonaLinux})
	if d.AuthMethod() != "publickey" {
		t.Fatalf("auth %q", d.AuthMethod())
	}
	for _, tc := range []struct {
		command, code, stdout, stderr, signal string
		status                                int // -1 for none
	}{
		{"uname -snrm", "", "Linux fake 6.8.0-0-generic x86_64\n", "", "", 0},
		{"both", "", "to stdout\n", "to stderr\n", "", 0},
		{"fail 3", "command_exit_nonzero", "", "failing with 3\n", "", 3},
		{"fail 255", "command_exit_nonzero", "", "failing with 255\n", "", 255},
		{"signal TERM", "command_exit_signal", "", "", "unnamed", -1},
		{"nostatus", "command_exit_missing", "", "", "", -1},
		{"ls /nonexistent", "command_exit_nonzero", "", "sh: 1: ls: not found\n", "", 127},
	} {
		r := d.Execute(context.Background(), platform.Command{Text: tc.command, Timeout: 5 * time.Second})
		status := -1
		if r.Exec != nil && r.Exec.ExitStatus != nil {
			status = *r.Exec.ExitStatus
		}
		if r.ErrorCode != tc.code || string(r.Output) != tc.stdout || r.Exec == nil || string(r.Exec.Stderr) != tc.stderr || r.Exec.ExitSignal != tc.signal || status != tc.status {
			t.Errorf("%s: code %q stdout %q exec %+v status %d err %v", tc.command, r.ErrorCode, r.Output, r.Exec, status, r.Err)
		}
	}
	if !d.Usable() {
		t.Fatal("the session is not usable")
	}
	socket := d.master.socket
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		t.Fatalf("the socket is left: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for srv.Connections() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := srv.Channels(); len(got) != 1 || got[0] != 7 || len(srv.PTYRequests()) != 0 || srv.Sessions() != 0 {
		t.Fatalf("channels %v ptys %d shells %d", got, len(srv.PTYRequests()), srv.Sessions())
	}
}

// TestExecTimeoutThroughTheMaster: at the command timeout the client is
// killed with the notice remote_command_not_stopped: no signal reaches the
// device, the command is left running, and the master serves the next.
func TestExecTimeoutThroughTheMaster(t *testing.T) {
	d, srv := execDriver(t, fakedevice.Options{Persona: fakedevice.PersonaLinux, Delay: map[string]time.Duration{"slow": time.Minute}})
	r := d.Execute(context.Background(), platform.Command{Text: "slow", Timeout: 300 * time.Millisecond})
	if r.ErrorCode != "command_timeout" || len(r.Notices) != 1 || r.Notices[0].Code != "remote_command_not_stopped" {
		t.Fatalf("slow: %q %v %+v", r.ErrorCode, r.Err, r.Notices)
	}
	if r := d.Execute(context.Background(), platform.Command{Text: "uname -snrm", Timeout: 5 * time.Second}); r.Err != nil || !strings.HasPrefix(string(r.Output), "Linux fake") {
		t.Fatalf("the next command: %q %v", r.Output, r.Err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(srv.LeftRunning()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := srv.LeftRunning(); len(got) != 1 || got[0] != "slow" || len(srv.Signals()) != 0 {
		t.Fatalf("left running %q signals %q", got, srv.Signals())
	}
}

// TestExecRefusedThroughTheMaster: a device that refuses the exec request
// (the IOS XE persona) is ssh_session_channel_refused, and the session
// ends, as on scrapligo-v1.
func TestExecRefusedThroughTheMaster(t *testing.T) {
	d, _ := execDriver(t, fakedevice.Options{StartPrivileged: true})
	r := d.Execute(context.Background(), platform.Command{Text: "show version", Timeout: 5 * time.Second})
	if r.ErrorCode != "ssh_session_channel_refused" || d.Usable() {
		t.Fatalf("code %q err %v usable %t", r.ErrorCode, r.Err, d.Usable())
	}
}

// TestExecConnectionLost: the device gone under a command ends the master;
// the session's code is the connection's, and the session ends.
func TestExecConnectionLost(t *testing.T) {
	d, _, cut := execDriverRelayed(t, fakedevice.Options{Persona: fakedevice.PersonaLinux, Delay: map[string]time.Duration{"slow": time.Minute}})
	go func() {
		time.Sleep(300 * time.Millisecond)
		cut()
	}()
	r := d.Execute(context.Background(), platform.Command{Text: "slow", Timeout: 10 * time.Second})
	if r.ErrorCode != "command_session_lost" || d.Usable() {
		t.Fatalf("code %q err %v usable %t", r.ErrorCode, r.Err, d.Usable())
	}
}
