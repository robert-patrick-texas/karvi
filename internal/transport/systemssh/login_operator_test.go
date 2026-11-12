package systemssh

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/internal/testpty"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
	"github.com/robert-patrick-texas/karvi/platform"
	"golang.org/x/sys/unix"
)

// operatorKeys is what the operator does at one of the device's prompts
// the broker leaves to the operator: the prompt's own words, and the keys
// typed, or, where signal is set, that signal sent to the login instead.
type operatorKeys struct {
	prompt, keys string
	signal       syscall.Signal
}

// openSSHShown is OpenSSH's user@host before a keyboard-interactive
// server's prompt, as the operator sees it.
const openSSHShown = `\(netops@[^\s()@]+\) `

// operatorLogin is one login over the system transport with the Factory's
// Operator TerminalOperator, run by a child whose controlling terminal is a
// pseudo-terminal (TestOperatorLoginChild), to a Linux persona asking
// prompts: the keys typed at each of keys once its prompt is on the
// terminal and, where shell, "exit" once the shell is up with the terminal
// raw. It returns the login's error message, everything the terminal was
// sent (carriage returns removed), the offset after each prompt in it, the
// fake, and whether echo was on once the login ended.
func operatorLogin(t *testing.T, prompts []fakedevice.KbdPrompt, keys []operatorKeys, shell bool) (result, out string, after []int, srv *fakedevice.Server, echo bool) {
	t.Helper()
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no OpenSSH client")
	}
	master, slave := testpty.Open(t)
	// Wide enough that the editor does not wrap a long prompt.
	testpty.SetSize(t, slave, 50, 300)
	var mu sync.Mutex
	var seen bytes.Buffer
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			seen.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	terminal := func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.ReplaceAll(seen.String(), "\r", "")
	}
	echoOn := func() bool {
		tio, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
		if err != nil {
			t.Fatal(err)
		}
		return tio.Lflag&unix.ECHO != 0
	}
	srv, err := fakedevice.Start(fakedevice.Options{KbdPrompts: prompts, Persona: fakedevice.PersonaLinux})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	dir := testsocket.Dir(t)
	resultFile := filepath.Join(dir, "result")
	cmd := exec.Command(os.Args[0], "-test.run=^TestOperatorLoginChild$", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "KARVI_TEST_LOGIN_DIR="+dir, "KARVI_TEST_LOGIN_PORT="+strconv.Itoa(srv.Port()), "KARVI_TEST_LOGIN_RESULT="+resultFile)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
	})
	// waitFor fails at once where the login has ended without it.
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !cond(); time.Sleep(20 * time.Millisecond) {
			select {
			case err := <-exited:
				exited <- err
				ended, _ := os.ReadFile(resultFile)
				t.Fatalf("%s: the login ended first (%v), %q; the terminal was sent\n%s", what, err, ended, terminal())
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: not within 10 s; the terminal was sent\n%s", what, terminal())
			}
		}
	}
	from := 0
	for _, k := range keys {
		shown := regexp.MustCompile(openSSHShown + regexp.QuoteMeta(k.prompt))
		waitFor(fmt.Sprintf("the prompt %q", k.prompt), func() bool {
			at := shown.FindStringIndex(terminal()[from:])
			if at == nil {
				return false
			}
			from += at[1]
			return true
		})
		after = append(after, from)
		if k.signal != 0 {
			if err := cmd.Process.Signal(k.signal); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if _, err := master.WriteString(k.keys); err != nil {
			t.Fatal(err)
		}
	}
	if shell {
		waitFor("the shell", func() bool {
			return srv.Sessions() > 0 && strings.Contains(terminal()[from:], "netops@fake:~$ ") && !echoOn()
		})
		if _, err := master.WriteString("exit\r"); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-exited:
		exited <- err
		if err != nil {
			t.Fatalf("the login's child: %v; the terminal was sent\n%s", err, terminal())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the login did not end; the terminal was sent\n%s", terminal())
	}
	got, err := os.ReadFile(resultFile)
	if err != nil {
		t.Fatalf("the login's child left no result: %v; the terminal was sent\n%s", err, terminal())
	}
	return string(got), terminal(), after, srv, echoOn()
}

// TestOperatorLoginChild is operatorLogin's child: a login over this
// host's OpenSSH client to the fake at KARVI_TEST_LOGIN_PORT on its
// controlling terminal, its standard streams, the password answered
// through the real askpass helper and the rest by TerminalOperator, ended
// by SIGINT or SIGTERM as karvi is; the login's error message goes to
// KARVI_TEST_LOGIN_RESULT.
func TestOperatorLoginChild(t *testing.T) {
	dir := os.Getenv("KARVI_TEST_LOGIN_DIR")
	if dir == "" {
		t.Skip("run by operatorLogin")
	}
	port, err := strconv.Atoi(os.Getenv("KARVI_TEST_LOGIN_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "karvi-askpass")
	if err := os.Symlink(exe, helper); err != nil {
		t.Fatal(err)
	}
	control, home := filepath.Join(dir, "c"), filepath.Join(dir, "h")
	for _, d := range []string{control, home} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{"ssh.include-user-config=false", "audit.journald-required=false", "ssh.host-key-policy=accept-new", `ssh.known-hosts-file="` + filepath.Join(dir, "kh") + `"`}})
	if err != nil {
		t.Fatal(err)
	}
	def, _ := platform.Builtin("linux")
	factory := Factory{Binary: ssh, Config: cfg, Home: home, BaseDir: home, ScratchDir: dir, ControlRoot: control, AskpassPath: helper, Operator: TerminalOperator}
	opened, err := factory.Open(context.Background(), platform.OpenRequest{
		Address: "127.0.0.1", Port: uint16(port), Username: "netops",
		Password:   func(fn func([]byte) error) error { return fn([]byte("pw")) },
		Definition: def, Channel: platform.ChannelExec,
		Metadata: map[string]string{"canonical_name": "fake"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := opened.(*Driver)
	defer d.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = d.Interactive(ctx, os.Stdin, os.Stdout, os.Stderr)
	if err := os.WriteFile(os.Getenv("KARVI_TEST_LOGIN_RESULT"), []byte(errorcodes.Message(err)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// answered is the fake's record of prompts each answered as its Want says.
func answered(prompts []fakedevice.KbdPrompt) string {
	var want []string
	for _, p := range prompts {
		want = append(want, fmt.Sprintf("%q %s", p.Prompt, p.Want))
	}
	return strings.Join(want, "|")
}

// TestOperatorAnswersALoginsPrompts: in a login over the system transport
// the operator answers at the terminal what the broker does not answer
// itself: a change prompt (a SecurID new PIN, user-chosen or
// system-generated, next tokencode, next passcode; BoKS's new PIN; a
// password change), a
// second ask of the password, a prompt no list matches, and two in one
// round. Each is shown as OpenSSH gives it, its user@host before it; what
// is typed is not echoed, the device receives each answer at its prompt,
// the login comes up, and echo is on again once it ends.
func TestOperatorAnswersALoginsPrompts(t *testing.T) {
	passcode := fakedevice.KbdPrompt{Prompt: "Enter PASSCODE: ", Want: "password"}
	other := func(prompt string) fakedevice.KbdPrompt {
		return fakedevice.KbdPrompt{Prompt: prompt, Want: "other", NewRound: true}
	}
	pinChosen := []fakedevice.KbdPrompt{passcode,
		other("Enter your new PIN, containing 4 to 8 digits, or <Ctrl-D> to cancel the New PIN procedure: "),
		other("Please re-enter new PIN: ")}
	for _, row := range []struct {
		name    string
		prompts []fakedevice.KbdPrompt
		typed   []string // at each "other" prompt, in order
	}{
		{"RSA user-chosen PIN", append(pinChosen[:3:3], other("PIN Accepted. Wait for the token code to change, then enter the new passcode: ")), []string{"731904", "731904", "73190455"}},
		{"RSA user-chosen PIN, then the passcode asked again", append(pinChosen[:3:3], other("Enter PASSCODE: ")), []string{"731904", "731904", "73190455"}},
		{"RSA next tokencode", []fakedevice.KbdPrompt{passcode, other("Wait for the tokencode to change, then enter the new tokencode: ")}, []string{"604117"}},
		{"RSA next passcode", []fakedevice.KbdPrompt{passcode, other("Enter Next PASSCODE: ")}, []string{"39752846"}},
		{"RSA system-generated PIN", []fakedevice.KbdPrompt{passcode, other("Are you prepared to accept a system-generated PIN? (y/n): "), other("Wait for the token code to change, then enter the new passcode: ")}, []string{"y", "84029173"}},
		{"BoKS new PIN", []fakedevice.KbdPrompt{passcode, other("Are you ready to enter a new PIN? "), other("Enter a new PIN, 5 - 7 integers: "), other("Re-enter PIN: ")}, []string{"y", "583920", "583920"}},
		{"password change", []fakedevice.KbdPrompt{{Prompt: "Password: ", Want: "password"}, other("Current password: "), other("New password: "), other("Retype new password: ")}, []string{"Old-Secret-17", "New-Secret-42", "New-Secret-42"}},
		{"unrecognized prompt", []fakedevice.KbdPrompt{{Prompt: "Select a role [1-3]: ", Want: "other"}}, []string{"2"}},
		{"two in one round", []fakedevice.KbdPrompt{passcode, other("Enter new PIN: "), {Prompt: "Re-enter new PIN: ", Want: "other"}}, []string{"902736", "902736"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			var keys []operatorKeys
			for _, p := range row.prompts {
				if p.Want == "other" {
					keys = append(keys, operatorKeys{prompt: p.Prompt, keys: row.typed[len(keys)] + "\r"})
				}
			}
			result, out, after, srv, echo := operatorLogin(t, row.prompts, keys, true)
			if result != "" {
				t.Errorf("the login ended %q, want nil", result)
			}
			if got, want := strings.Join(srv.KbdAnswers(), "|"), answered(row.prompts); got != want {
				t.Errorf("the device received %q, want %q", got, want)
			}
			for i, k := range keys {
				// The editor's Enter right after the prompt: nothing echoed.
				// A one-key answer (y, 2) is in other text on the terminal,
				// so only a longer one is looked for everywhere.
				if !strings.HasPrefix(out[after[i]:], "\n") {
					t.Errorf("after the prompt %q the terminal was sent %q, want the line's end", k.prompt, out[after[i]:min(len(out), after[i]+40)])
				}
				if typed := row.typed[i]; len(typed) > 1 && strings.Contains(out, typed) {
					t.Errorf("the typed %q was echoed", typed)
				}
			}
			if !echo {
				t.Error("the terminal was left with echo off")
			}
			if t.Failed() {
				t.Logf("the terminal was sent\n%s", out)
			}
		})
	}
}

// TestOperatorCtrlCEndsALogin: Ctrl-C at a prompt the operator answers
// ends the login, credential_prompt_interrupted, before the device
// receives an answer in the round: no shell starts, and echo is on again.
func TestOperatorCtrlCEndsALogin(t *testing.T) {
	prompts := []fakedevice.KbdPrompt{{Prompt: "Enter PASSCODE: ", Want: "password"}, {Prompt: "Enter new PIN: ", Want: "other", NewRound: true}}
	result, out, _, srv, echo := operatorLogin(t, prompts, []operatorKeys{{prompt: "Enter new PIN: ", keys: "\x03"}}, false)
	if want := "credential_prompt_interrupted: interrupted at the device's prompt"; result != want {
		t.Errorf("the login ended %q, want %q", result, want)
	}
	if got, want := strings.Join(srv.KbdAnswers(), "|"), answered(prompts[:1]); got != want {
		t.Errorf("the device received %q, want %q", got, want)
	}
	if srv.Sessions() != 0 {
		t.Errorf("sessions=%d, want 0", srv.Sessions())
	}
	if !echo {
		t.Error("the terminal was left with echo off")
	}
	if t.Failed() {
		t.Logf("the terminal was sent\n%s", out)
	}
}

// TestOperatorSignalEndsALogin: a signal to karvi (SIGTERM, an INT from
// another terminal) while the operator is at the device's prompt ends the
// prompt and the login, credential_prompt_interrupted, as at a credential
// prompt: the device receives nothing more, no shell starts, and echo is
// on again.
func TestOperatorSignalEndsALogin(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		prompts := []fakedevice.KbdPrompt{{Prompt: "Enter PASSCODE: ", Want: "password"}, {Prompt: "Enter new PIN: ", Want: "other", NewRound: true}}
		result, out, _, srv, echo := operatorLogin(t, prompts, []operatorKeys{{prompt: "Enter new PIN: ", signal: sig}}, false)
		if want := "credential_prompt_interrupted: interrupted at the device's prompt"; result != want {
			t.Errorf("%s: the login ended %q, want %q", sig, result, want)
		}
		if got, want := strings.Join(srv.KbdAnswers(), "|"), answered(prompts[:1]); got != want {
			t.Errorf("%s: the device received %q, want %q", sig, got, want)
		}
		if srv.Sessions() != 0 {
			t.Errorf("%s: sessions=%d, want 0", sig, srv.Sessions())
		}
		if !echo {
			t.Errorf("%s: the terminal was left with echo off", sig)
		}
		if t.Failed() {
			t.Logf("the terminal was sent\n%s", out)
		}
	}
}

// TestOperatorCtrlDAnswersEmpty: Ctrl-D on an empty line at a prompt the
// operator answers sends the device an empty answer, as OpenSSH's own
// prompt does, and the login goes on: the next prompt is shown and read.
func TestOperatorCtrlDAnswersEmpty(t *testing.T) {
	prompts := []fakedevice.KbdPrompt{{Prompt: "Enter PASSCODE: ", Want: "password"}, {Prompt: "Enter new PIN: ", Want: "empty", NewRound: true}, {Prompt: "Re-enter PIN: ", Want: "other", NewRound: true}}
	result, out, _, srv, echo := operatorLogin(t, prompts, []operatorKeys{{prompt: "Enter new PIN: ", keys: "\x04"}, {prompt: "Re-enter PIN: ", keys: "583920\r"}}, true)
	if result != "" {
		t.Errorf("the login ended %q, want nil", result)
	}
	if got, want := strings.Join(srv.KbdAnswers(), "|"), answered(prompts); got != want {
		t.Errorf("the device received %q, want %q", got, want)
	}
	if !echo {
		t.Error("the terminal was left with echo off")
	}
	if t.Failed() {
		t.Logf("the terminal was sent\n%s", out)
	}
}

// TestOperatorWithoutATerminal: a login with no controlling terminal (its
// own session, none acquired) asks no one: a change prompt ends it as it
// ends a command session, authentication_change_requested, the device
// receiving nothing in that round.
func TestOperatorWithoutATerminal(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no OpenSSH client")
	}
	prompts := []fakedevice.KbdPrompt{{Prompt: "Enter PASSCODE: ", Want: "password"}, {Prompt: "Enter new PIN: ", Want: "other", NewRound: true}}
	srv, err := fakedevice.Start(fakedevice.Options{KbdPrompts: prompts, Persona: fakedevice.PersonaLinux})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	dir := testsocket.Dir(t)
	resultFile := filepath.Join(dir, "result")
	var out bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestOperatorLoginChild$", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "KARVI_TEST_LOGIN_DIR="+dir, "KARVI_TEST_LOGIN_PORT="+strconv.Itoa(srv.Port()), "KARVI_TEST_LOGIN_RESULT="+resultFile)
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Run(); err != nil {
		t.Fatalf("the login's child: %v\n%s", err, out.String())
	}
	got, err := os.ReadFile(resultFile)
	if err != nil {
		t.Fatalf("the login's child left no result: %v\n%s", err, out.String())
	}
	if want := `authentication_change_requested: the device asks to change a secret: "Enter new PIN: "`; string(got) != want {
		t.Errorf("the login ended %q, want %q", got, want)
	}
	if got, want := strings.Join(srv.KbdAnswers(), "|"), answered(prompts[:1]); got != want {
		t.Errorf("the device received %q, want %q", got, want)
	}
}
