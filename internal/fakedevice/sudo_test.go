package fakedevice

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// runInput is run with the channel's input given: input, then its end, as
// sudo saw printf's output on a pipe in the container. The input goes
// through StdinPipe, its error ignored: a command that reads none may end
// first, and Session.Stdin's copy would then make Wait return its EOF in
// place of the status.
func runInput(t *testing.T, srv *Server, command, input string) (string, string, int) {
	t.Helper()
	sess, err := connect(t, srv).NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var stdout, stderr bytes.Buffer
	sess.Stdout, sess.Stderr = &stdout, &stderr
	in, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start(command); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(in, input)
	in.Close()
	code, _ := status(t, sess.Wait())
	return stdout.String(), stderr.String(), code
}

// countSecrets is how many "<secret>" lines srv recorded.
func countSecrets(srv *Server) int {
	n := 0
	for _, l := range srv.Lines() {
		if l == "<secret>" {
			n++
		}
	}
	return n
}

// TestLinuxSudoAsks: under SudoAsks the persona's sudo writes, in each
// sudo's form, the bytes the container's sudo wrote on stderr (EXAMPLES
// chapter 96: ex96d-stderr, ex96e-unseen; the prompt there
// karvi-sudo-3f9c0a7e:), reads one line of the channel's input a try, up to
// three, records each as "<secret>", and runs the command as root once the
// password is given.
func TestLinuxSudoAsks(t *testing.T) {
	const p = "karvi-sudo-0123456789abcdef:"
	wire := "sudo -S -p '" + p + "' -- "
	rsP := "[sudo: " + p + "] Password: "
	for _, form := range []struct {
		name string
		rs   bool
	}{{"classic", false}, {"sudo-rs", true}} {
		srv := startLinux(t, Options{SudoAsks: true, SudoRS: form.rs, Enable: "pw1"})
		for _, tc := range []struct {
			name, command, input string
			stdout               string
			classic, rs          string // stderr
			status, secrets      int
		}{
			{"the password", wire + "whoami", "pw1\n", "root\n", p, rsP + "\n", 0, 1},
			{"a wrong password, then the input's end", wire + "whoami", "bad\n", "",
				p + "Sorry, try again.\n" + p + "\nsudo: no password was provided\nsudo: 1 incorrect password attempt\n",
				rsP + "\nsudo: Authentication failed, try again.\n" + rsP + "\nsudo: Authentication required but not attempted\n", 1, 1},
			{"the input's end at the first prompt", wire + "whoami", "", "",
				p + "\nsudo: no password was provided\nsudo: a password is required\n",
				rsP + "\nsudo: Authentication required but not attempted\n", 1, 0},
			{"three wrong passwords", wire + "whoami", "a\nb\nc\n", "",
				p + "Sorry, try again.\n" + p + "Sorry, try again.\n" + p + "sudo: 3 incorrect password attempts\n",
				strings.Repeat(rsP+"\nsudo: Authentication failed, try again.\n", 2) + rsP + "\nsudo: maximum 3 incorrect authentication attempts\n", 1, 3},
			{"-S with no -p", "sudo -S -- whoami", "pw1\n", "root\n", "[sudo] password for netops: ", "[sudo: authenticate] Password: \n", 0, 1},
			{"no -S, no terminal", "sudo -- whoami", "", "",
				"sudo: a terminal is required to read the password; either use the -S option to read from standard input or configure an askpass helper\nsudo: a password is required\n",
				"sudo: A terminal is required to authenticate\n", 1, 0},
			{"a command not found, the password", wire + "nosuchcmd -x", "pw1\n", "",
				p + "sudo: nosuchcmd: command not found\n", rsP + "\nsudo: 'nosuchcmd': command not found\n", 1, 1},
			{"sudo -n", "sudo -n -- whoami", "", "", "sudo: a password is required\n", "sudo: interactive authentication is required\n", 1, 0},
			{"sudo -n id -u, the qualification's L3", "sudo -n id -u", "", "", "sudo: a password is required\n", "sudo: interactive authentication is required\n", 1, 0},
		} {
			before := countSecrets(srv)
			stdout, stderr, code := runInput(t, srv, tc.command, tc.input)
			want := tc.classic
			if form.rs {
				want = tc.rs
			}
			if stdout != tc.stdout || stderr != want || code != tc.status {
				t.Errorf("%s, %s: stdout %q stderr %q status %d; want %q %q %d", form.name, tc.name, stdout, stderr, code, tc.stdout, want, tc.status)
			}
			if got := countSecrets(srv) - before; got != tc.secrets {
				t.Errorf("%s, %s: %d password lines recorded, want %d", form.name, tc.name, got, tc.secrets)
			}
		}
	}
	// With Enable empty, sudo takes the login password, as sudo's own
	// default does.
	srv := startLinux(t, Options{SudoAsks: true})
	if stdout, _, code := runInput(t, srv, wire+"id -u", "pw\n"); stdout != "0\n" || code != 0 {
		t.Errorf("the login password: stdout %q status %d", stdout, code)
	}
}

// TestLinuxSudoNoPasswordAsked: without SudoAsks every sudo command runs as
// root with no prompt and nothing read, a password held or not, the
// command's not found in each sudo's words; an option the persona does not
// know, or no command, is its usage line.
func TestLinuxSudoNoPasswordAsked(t *testing.T) {
	for _, form := range []struct {
		name     string
		rs       bool
		notFound string
	}{{"classic", false, "sudo: nosuchcmd: command not found\n"}, {"sudo-rs", true, "sudo: 'nosuchcmd': command not found\n"}} {
		srv := startLinux(t, Options{SudoRS: form.rs})
		for _, tc := range []struct {
			command, input, stdout, stderr string
			status                         int
		}{
			{"sudo -S -p 'karvi-sudo-0123456789abcdef:' -- whoami", "pw1\n", "root\n", "", 0},
			{"sudo -n -- id -u", "", "0\n", "", 0},
			{"sudo -n id -u", "", "0\n", "", 0},
			{"sudo -n -- nosuchcmd -x", "", "", form.notFound, 1},
			{"sudo -x whoami", "", "", "usage: sudo [-n] [-S] [-p prompt] [--] command (the fake's sudo)\n", 1},
			{"sudo -n --", "", "", "usage: sudo [-n] [-S] [-p prompt] [--] command (the fake's sudo)\n", 1},
			{"sudo -p 'open -- whoami", "", "", "usage: sudo [-n] [-S] [-p prompt] [--] command (the fake's sudo)\n", 1},
			{"whoami", "", "netops\n", "", 0},
			{"id -u", "", "1000\n", "", 0},
		} {
			stdout, stderr, code := runInput(t, srv, tc.command, tc.input)
			if stdout != tc.stdout || stderr != tc.stderr || code != tc.status {
				t.Errorf("%s, %s: stdout %q stderr %q status %d; want %q %q %d", form.name, tc.command, stdout, stderr, code, tc.stdout, tc.stderr, tc.status)
			}
		}
		if n := countSecrets(srv); n != 0 {
			t.Errorf("%s: sudo read %d password lines with none asked", form.name, n)
		}
	}
}

// TestParseSudo: the options in any order, -p's argument quoted as sh
// quotes it, the command the rest of the line as written.
func TestParseSudo(t *testing.T) {
	for _, tc := range []struct {
		line string
		want sudoCall
		ok   bool
	}{
		{"sudo -S -p 'karvi-sudo-ab:' -- ip  route", sudoCall{stdin: true, prompt: "karvi-sudo-ab:", command: "ip  route"}, true},
		{"sudo -p 'a b' -S -- x", sudoCall{stdin: true, prompt: "a b", command: "x"}, true},
		{"sudo -p P -n id -u", sudoCall{nonInteractive: true, prompt: "P", command: "id -u"}, true},
		{"sudo -- -x", sudoCall{command: "-x"}, true},
		{"sudo", sudoCall{usage: true}, true},
		{"sudoedit x", sudoCall{}, false},
		{"whoami", sudoCall{}, false},
	} {
		got, ok := parseSudo(tc.line)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q: %+v %t, want %+v %t", tc.line, got, ok, tc.want, tc.ok)
		}
	}
}

// TestLinuxShellSudo: the shell's sudo reads no password: a command runs
// as root where none is asked, and one wanted is sudo -n's refusal.
func TestLinuxShellSudo(t *testing.T) {
	c := dial(t, startLinux(t, Options{}))
	c.expect("netops@fake:~$ ")
	c.send("sudo -n id -u\n")
	if got := c.expect("netops@fake:~$ "); got != "sudo -n id -u\r\n0\r\nnetops@fake:~$ " {
		t.Fatalf("no password asked: %q", got)
	}
	c = dial(t, startLinux(t, Options{SudoAsks: true}))
	c.expect("netops@fake:~$ ")
	c.send("sudo whoami\n")
	if got := c.expect("netops@fake:~$ "); got != "sudo whoami\r\nsudo: a password is required\r\nnetops@fake:~$ " {
		t.Fatalf("a password wanted: %q", got)
	}
}
