package systemssh

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/askpass"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
	"github.com/robert-patrick-texas/karvi/platform"
)

// TestMain lets the test binary play the real askpass helper, linked as
// karvi-askpass (promptDriver).
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "karvi-askpass" {
		os.Exit(askpass.HelperMain(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// promptDriver is an opened driver over this host's OpenSSH client to a
// fake asking prompts in keyboard-interactive, its password answered
// through the real askpass helper; the Linux persona over the exec channel
// when exec is set, otherwise an IOS XE shell.
func promptDriver(t *testing.T, prompts []fakedevice.KbdPrompt, exec_ bool) (*Driver, *fakedevice.Server) {
	t.Helper()
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no OpenSSH client")
	}
	opts := fakedevice.Options{KbdPrompts: prompts, StartPrivileged: true}
	def, _ := platform.Builtin("cisco_ios")
	channel := platform.ChannelShell
	if exec_ {
		opts.Persona = fakedevice.PersonaLinux
		def, _ = platform.Builtin("linux")
		channel = platform.ChannelExec
	}
	srv, err := fakedevice.Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	dir := testsocket.Dir(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "karvi-askpass")
	if err := os.Symlink(exe, helper); err != nil {
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
	factory := Factory{Binary: ssh, Config: cfg, Home: home, BaseDir: home, ScratchDir: dir, ControlRoot: control, AskpassPath: helper}
	opened, err := factory.Open(context.Background(), platform.OpenRequest{
		Address: "127.0.0.1", Port: uint16(srv.Port()), Username: "netops",
		Password:   func(fn func([]byte) error) error { return fn([]byte("pw")) },
		Definition: def, Channel: channel,
		Metadata: map[string]string{"canonical_name": "fake"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := opened.(*Driver)
	t.Cleanup(func() { d.Close() })
	return d, srv
}

// TestChangePromptEndsOpenSSH: over the system transport a change prompt
// ends OpenSSH while it waits on the helper, in a shell's session, an exec
// device's master, and a login alike: the device gets no answer in the
// round and no password-method attempt after it, and the failure is
// authentication_change_requested with the prompt's own words. A token
// prompt, the control, takes the password through the same helper.
func TestChangePromptEndsOpenSSH(t *testing.T) {
	change := []fakedevice.KbdPrompt{{Prompt: "Enter new PIN: ", Want: "empty"}}
	want := `authentication_change_requested: the device asks to change a secret: "Enter new PIN: "`
	for _, tc := range []struct {
		name string
		run  func(*Driver) error
		exec bool
	}{
		{"shell", func(d *Driver) error { return d.Prepare(context.Background()) }, false},
		{"exec", func(d *Driver) error { return d.Prepare(context.Background()) }, true},
		{"login", func(d *Driver) error {
			var out bytes.Buffer
			return d.Interactive(context.Background(), strings.NewReader("exit\n"), &out, &out)
		}, false},
	} {
		d, srv := promptDriver(t, change, tc.exec)
		if err := tc.run(d); errorcodes.Message(err) != want {
			t.Errorf("%s: %v, want %s", tc.name, err, want)
		}
		if got := srv.KbdAnswers(); len(got) != 0 {
			t.Errorf("%s: the device received %q, want nothing", tc.name, got)
		}
		if srv.Sessions() != 0 {
			t.Errorf("%s: sessions=%d", tc.name, srv.Sessions())
		}
	}
	d, srv := promptDriver(t, []fakedevice.KbdPrompt{{Prompt: "Token: ", Want: "password"}}, false)
	if err := d.Prepare(context.Background()); err != nil {
		t.Fatalf("token prompt: %v", err)
	}
	if got := srv.KbdAnswers(); strings.Join(got, "|") != `"Token: " password` {
		t.Errorf("token prompt: the device received %q", got)
	}
}
