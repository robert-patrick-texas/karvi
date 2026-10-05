package fakedevice

import (
	"io"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// A server restarted on Options.Port with the same seed is the same device
// to a trust store: the host-key identity holds the port.
func TestStartOnAGivenPort(t *testing.T) {
	seed := make([]byte, 32)
	first, err := Start(Options{HostKeySeed: seed})
	if err != nil {
		t.Fatal(err)
	}
	port := first.Port()
	first.Close()
	second, err := Start(Options{HostKeySeed: seed, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.Port() != port {
		t.Fatalf("port %d, want %d", second.Port(), port)
	}
}

// client is one interactive shell on the fake, as a transport would open
// it: password authentication, a PTY, the shell; the test types lines and
// reads until the text it expects has arrived.
type client struct {
	t    *testing.T
	conn *ssh.Client
	in   io.WriteCloser
	out  io.Reader
	seen []byte
}

func dial(t *testing.T, srv *Server) *client {
	t.Helper()
	conn, err := ssh.Dial("tcp", srv.Addr(), &ssh.ClientConfig{User: "netops", Auth: []ssh.AuthMethod{ssh.Password("pw")}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.RequestPty("vt100", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	in, _ := sess.StdinPipe()
	out, _ := sess.StdoutPipe()
	if err := sess.Shell(); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, conn: conn, in: in, out: out}
	t.Cleanup(func() { conn.Close() })
	return c
}

// expect reads until want has arrived since the last expect, within five
// seconds, and returns everything read since then.
func (c *client) expect(want string) string {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 4096)
	for !strings.Contains(string(c.seen), want) {
		if time.Now().After(deadline) {
			c.t.Fatalf("waiting for %q; read so far %q", want, c.seen)
		}
		n, err := c.out.Read(buf)
		if n > 0 {
			c.seen = append(c.seen, buf[:n]...)
		}
		if err != nil {
			c.t.Fatalf("stream ended waiting for %q; read %q: %v", want, c.seen, err)
		}
	}
	got := string(c.seen)
	c.seen = c.seen[:0]
	return got
}

func (c *client) send(text string) {
	c.t.Helper()
	if _, err := io.WriteString(c.in, text); err != nil {
		c.t.Fatal(err)
	}
}

// TestCopyRunningConfigIsAValuePrompt:
// the copy asks for the destination with the device's trailing space, takes
// one echoed line, answers [OK], and returns the prompt; the line is
// recorded as <value:TEXT>. A saved configuration's reload goes straight to
// its [confirm].
func TestCopyRunningConfigIsAValuePrompt(t *testing.T) {
	srv, err := Start(Options{StartPrivileged: true})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	c := dial(t, srv)
	c.expect("Router#")
	c.send("copy running-config startup-config\n")
	c.expect("Destination filename [startup-config]? ")
	c.send("\n")
	if got := c.expect("Router#"); !strings.Contains(got, "Building configuration...\r\n[OK]\r\nRouter#") {
		t.Fatalf("after the default: %q", got)
	}
	c.send("copy running-config startup-config\n")
	c.expect("? ")
	c.send("backup-config\n")
	if got := c.expect("Router#"); !strings.HasPrefix(got, "backup-config\r\n") {
		t.Fatalf("the value is echoed: %q", got)
	}
	c.send("reload\n")
	if got := c.expect("[confirm]"); strings.Contains(got, "Save?") {
		t.Fatalf("a saved configuration asks no save question: %q", got)
	}
	c.send("\n")
	c.expect("\r\n") // the reloading device's last bytes
	want := []string{"copy running-config startup-config", "<value:>", "copy running-config startup-config", "<value:backup-config>", "reload", "<confirm>"}
	if got := srv.Lines(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", got, want)
	}
}

// TestUnsavedReloadAsksToSaveFirst: under Options.Unsaved the reload asks
// "Save? [yes/no]: " before its [confirm]; y or yes saves, n or no skips,
// anything else re-asks; a copy clears the state.
func TestUnsavedReloadAsksToSaveFirst(t *testing.T) {
	srv, err := Start(Options{StartPrivileged: true, Unsaved: true})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	c := dial(t, srv)
	c.expect("Router#")
	c.send("reload\n")
	c.expect("System configuration has been modified. Save? [yes/no]: ")
	c.send("maybe\n")
	c.expect("Save? [yes/no]: ") // re-asked
	c.send("y\n")
	if got := c.expect("[confirm]"); !strings.Contains(got, "Building configuration...\r\n[OK]\r\nProceed with reload? [confirm]") {
		t.Fatalf("after yes: %q", got)
	}
	c.send("q") // abandon, so the shell goes on
	c.expect("Router#")
	c.send("reload\n")
	if got := c.expect("[confirm]"); strings.Contains(got, "Save?") {
		t.Fatalf("saved by the answer, no second question: %q", got)
	}
	c.send("q")
	c.expect("Router#")

	// A second shell starts unsaved again; no answers skip the save.
	c2 := dial(t, srv)
	c2.expect("Router#")
	c2.send("reload\n")
	c2.expect("Save? [yes/no]: ")
	c2.send("no\n")
	if got := c2.expect("[confirm]"); strings.Contains(got, "[OK]") {
		t.Fatalf("no must not save: %q", got)
	}
	c2.send("q")
	c2.expect("Router#")
	c2.send("copy running-config startup-config\n")
	c2.expect("? ")
	c2.send("\n")
	c2.expect("Router#")
	c2.send("reload\n")
	if got := c2.expect("[confirm]"); strings.Contains(got, "Save?") {
		t.Fatalf("the copy cleared the state: %q", got)
	}
	want := "reload|<value:maybe>|<value:y>|<abandon>|reload|<abandon>|reload|<value:no>|<abandon>|copy running-config startup-config|<value:>|reload"
	if got := strings.Join(srv.Lines(), "|"); got != want {
		t.Fatalf("lines %q, want %q", got, want)
	}
}

// TestConfigurationModeAndTheQualificationShowCommands: the commands the
// device qualification script's rows send (scripts/device-qualification.sh
// D3, D4, D14). "configure terminal" at the privileged prompt gives the
// (config)# prompt, which "end" and "exit" leave, and at the user prompt it
// is invalid input; a line typed in configuration mode is invalid input,
// since no configuration is modelled; "show privilege" names the level and
// "show users" one vty line.
func TestConfigurationModeAndTheQualificationShowCommands(t *testing.T) {
	srv, err := Start(Options{Enable: "en"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	c := dial(t, srv)
	c.expect("Router>")
	c.send("configure terminal\n")
	if got := c.expect("Router>"); !strings.Contains(got, "% Invalid input") {
		t.Fatalf("configure terminal at the user prompt: %q", got)
	}
	c.send("show privilege\n")
	if got := c.expect("Router>"); !strings.Contains(got, "Current privilege level is 1\r\n") {
		t.Fatalf("show privilege at the user prompt: %q", got)
	}
	c.send("enable\n")
	c.expect("Password: ")
	c.send("en\n")
	c.expect("Router#")
	c.send("show privilege\n")
	if got := c.expect("Router#"); !strings.Contains(got, "Current privilege level is 15\r\n") {
		t.Fatalf("show privilege after enable: %q", got)
	}
	c.send("configure terminal\n")
	if got := c.expect("Router(config)#"); !strings.Contains(got, "Enter configuration commands") {
		t.Fatalf("configure terminal: %q", got)
	}
	c.send("hostname other\n")
	if got := c.expect("Router(config)#"); !strings.Contains(got, "% Invalid input") {
		t.Fatalf("a configuration line: %q", got)
	}
	c.send("end\n")
	c.expect("Router#")
	c.send("configure terminal\n")
	c.expect("Router(config)#")
	c.send("exit\n")
	c.expect("Router#")
	c.send("show users\n")
	if got := c.expect("Router#"); !strings.Contains(got, "vty 0     netops") {
		t.Fatalf("show users: %q", got)
	}
}
