package executor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey/sshkeytest"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
	"github.com/robert-patrick-texas/karvi/records"
)

// The root commands (--sudo) of EXAMPLES chapter 96 end to end: the
// executor, each transport, and the fake's sudo in each sudo's form
// (classic sudo's prompt and messages, and sudo-rs's), as the container
// showed them.

// sudoSecret is the sudo password the tests give; it must reach nothing but
// sudo's input.
const sudoSecret = "Zq7-sudo-SECRET"

// sudoJob is what one device's job left: its records, the fake's lines, the
// live view, the --debug lines, every file of the job, and the device's
// name.
type sudoJob struct {
	recs  []records.CommandRecord
	lines []string
	live  string
	debug []string
	files string
	name  string
}

// eachSudo runs fn as a subtest for each transport and each sudo's form.
func eachSudo(t *testing.T, fn func(t *testing.T, transport string, rs bool)) {
	for _, transport := range []string{"native", "system"} {
		for _, rs := range []bool{false, true} {
			name := transport + "/classic"
			if rs {
				name = transport + "/sudo-rs"
			}
			t.Run(name, func(t *testing.T) { fn(t, transport, rs) })
		}
	}
}

// runSudo runs one device's job on the fake's Linux persona (opts) over
// transport, on the exec channel: the commands, a root command where sudo
// says so, under a grant whose enable password is enable. The system
// transport is this host's OpenSSH, logging in by a key the fake
// authorizes.
func runSudo(t *testing.T, transport string, opts fakedevice.Options, enable string, commands []string, sudo []bool) sudoJob {
	t.Helper()
	opts.Persona, opts.Username, opts.Password = fakedevice.PersonaLinux, "u", "p"
	var sets []string
	var keyPath, control string
	if transport == "system" {
		ssh, err := exec.LookPath("ssh")
		if err != nil {
			t.Skip("no OpenSSH client")
		}
		key, _ := sshkeytest.Ed25519(t, "")
		opts.AuthorizedKeys = sshkeytest.Authorized(t, key)
		dir := testsocket.Dir(t)
		keyPath, control = filepath.Join(dir, "k"), filepath.Join(dir, "c")
		if err := os.WriteFile(keyPath, key, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(control, 0o700); err != nil {
			t.Fatal(err)
		}
		sets = []string{fmt.Sprintf("ssh.transports.system=%q", ssh), "ssh.include-user-config=false"}
	}
	srv, err := fakedevice.Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	h := newGateHarness(t, sets, nil)
	h.target.Device.Transport = transport
	h.target.Device.Platform = "linux"
	h.target.Channel = executionplan.ChannelExec
	h.target.Device.Port = uint16(srv.Port())
	g := liveGrant(enable)
	if keyPath != "" {
		g.g.Keys = []credentials.KeyRef{{Path: keyPath}}
		h.exec.opts.ControlRoot = control
	}
	h.exec.opts.Grants = g
	h.exec.opts.Commands, h.exec.opts.Sudo = commands, sudo
	var mu sync.Mutex
	var debug []string
	h.exec.opts.Debug = func(s string) {
		mu.Lock()
		debug = append(debug, s)
		mu.Unlock()
	}
	live := &liveRecorder{}
	h.exec.opts.Live = live.open
	_, recs := h.run(context.Background())
	var files strings.Builder
	err = filepath.WalkDir(h.store.Paths().Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		files.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("debug:\n%s\nfake lines: %q", strings.Join(debug, "\n"), srv.Lines())
		}
	})
	mu.Lock()
	j := sudoJob{recs: recs, lines: srv.Lines(), live: live.text(), debug: append([]string(nil), debug...), files: files.String(), name: h.target.Device.CanonicalName}
	mu.Unlock()
	// The session was the transport's own.
	if ready := map[string]string{"native": "native SSH session ready ", "system": "system SSH exec master ready "}[transport]; len(j.debugWith(ready)) != 1 {
		t.Fatalf("no %q line: the job did not run on %s", ready, transport)
	}
	return j
}

// stderrOf is a record's stderr, "<null>" for none.
func stderrOf(r records.CommandRecord) string {
	if r.Stderr == nil {
		return "<null>"
	}
	return *r.Stderr
}

// debugWith is the debug lines that begin with prefix.
func (j sudoJob) debugWith(prefix string) []string {
	var out []string
	for _, line := range j.debug {
		if strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	return out
}

// noSecret fails t if the password, sudo's prompt, or its prompt's words
// reached the records, the job's files, the live view, or --debug.
func (j sudoJob) noSecret(t *testing.T, secret string) {
	t.Helper()
	for what, text := range map[string]string{"the job's files": j.files, "the live view": j.live, "--debug": strings.Join(j.debug, "\n")} {
		for _, bad := range []string{secret, "karvi-sudo-", "Password:", "password for"} {
			if strings.Contains(text, bad) {
				t.Errorf("%q reached %s", bad, what)
			}
		}
	}
}

// sudoWireLine is a root command's exec request with a password: its
// prompt and its command.
var sudoWireLine = regexp.MustCompile(`^exec: sudo -S -p '(karvi-sudo-[0-9a-f]{16}:)' -- (.*)$`)

// TestRootCommandOnAnExecTarget: root commands answered by sudo's prompt,
// over each transport and under each sudo: each sent as sudo -S -p with a
// prompt of its own, the password written once to its prompt (the fake
// read one line a command), the output root's; each record keeps its
// command as written, with privilege sudo, its stderr without the prompt
// or the line end sudo-rs writes after it, and keeps to the published
// schema; the live view shows a root command at "#", the plain one beside
// it at "$" with no privilege; --debug says sudo=password on a root
// command's start line and device sudo answered once it is done; and the
// password and the prompt reach no file of the job, no live view, and no
// debug line.
func TestRootCommandOnAnExecTarget(t *testing.T) {
	eachSudo(t, func(t *testing.T, transport string, rs bool) {
		j := runSudo(t, transport, fakedevice.Options{SudoAsks: true, SudoRS: rs, Enable: sudoSecret}, sudoSecret,
			[]string{"uname -snrm", "whoami", "id -u"}, []bool{false, true, true})
		if len(j.recs) != 3 || j.recs[0].Privilege != "" || j.recs[0].Root() {
			t.Fatalf("records %s", describe(j.recs))
		}
		for i, want := range []string{"", "root\n", "0\n"} {
			r := j.recs[i]
			canarytest.SchemaParity(t, "../../schema/command-record.schema.json", r)
			if i == 0 {
				continue
			}
			if r.Status != "success" || r.Command != []string{"", "whoami", "id -u"}[i] || r.Privilege != records.PrivilegeSudo || r.Output != want || stderrOf(r) != "" {
				t.Errorf("record %d: status %s command %q privilege %q output %q stderr %q", i+1, r.Status, r.Command, r.Privilege, r.Output, stderrOf(r))
			}
		}
		if len(j.lines) != 5 || j.lines[0] != "exec: uname -snrm" || j.lines[2] != "<secret>" || j.lines[4] != "<secret>" {
			t.Fatalf("the fake's lines %q", j.lines)
		}
		first, second := sudoWireLine.FindStringSubmatch(j.lines[1]), sudoWireLine.FindStringSubmatch(j.lines[3])
		if first == nil || first[2] != "whoami" || second == nil || second[2] != "id -u" || first[1] == second[1] {
			t.Errorf("the root commands' wires %q and %q: each its command and a prompt of its own", j.lines[1], j.lines[3])
		}
		if !strings.HasPrefix(j.live, j.name+"$ uname -snrm\n") || !strings.Contains(j.live, j.name+"# whoami\nroot\n") || !strings.Contains(j.live, j.name+"# id -u\n0\n") {
			t.Errorf("the live view %q", j.live)
		}
		starts := j.debugWith("device command start ")
		if len(starts) != 3 || strings.Contains(starts[0], "sudo=") || !strings.HasSuffix(starts[1], " sudo=password") || !strings.HasSuffix(starts[2], " sudo=password") {
			t.Errorf("start lines %q", starts)
		}
		if got := j.debugWith("device sudo answered "); len(got) != 2 || got[0] != fmt.Sprintf("device sudo answered target=%q index=2", j.name) || got[1] != fmt.Sprintf("device sudo answered target=%q index=3", j.name) {
			t.Errorf("answered lines %q", got)
		}
		j.noSecret(t, sudoSecret)
	})
}

// TestRootCommandPasswordRefused: a password sudo refuses is written once,
// as --debug's device sudo answered says; sudo's second prompt closes the
// input, and the command is privilege_failed with sudo's own lines kept on
// its record (the prompts and their line ends out) and its exit status 1;
// the device halts there.
func TestRootCommandPasswordRefused(t *testing.T) {
	eachSudo(t, func(t *testing.T, transport string, rs bool) {
		const wrong = "Wrong-sudo-SECRET"
		j := runSudo(t, transport, fakedevice.Options{SudoAsks: true, SudoRS: rs, Enable: sudoSecret}, wrong,
			[]string{"whoami", "id -u"}, []bool{true, true})
		want := "Sorry, try again.\nsudo: no password was provided\nsudo: 1 incorrect password attempt\n"
		if rs {
			want = "sudo: Authentication failed, try again.\nsudo: Authentication required but not attempted\n"
		}
		if len(j.recs) != 2 {
			t.Fatalf("records %s", describe(j.recs))
		}
		r := j.recs[0]
		if r.Error == nil || r.Error.Code != "privilege_failed" || stderrOf(r) != want || r.ExitStatus == nil || *r.ExitStatus != 1 || r.Privilege != records.PrivilegeSudo || r.Output != "" {
			t.Errorf("record 1: error %+v stderr %q exit %v output %q", r.Error, stderrOf(r), r.ExitStatus, r.Output)
		}
		if j.recs[1].Status != "not_attempted_prior_command_failure" {
			t.Errorf("record 2: status %s", j.recs[1].Status)
		}
		if len(j.lines) != 2 || sudoWireLine.FindStringSubmatch(j.lines[0]) == nil || j.lines[1] != "<secret>" {
			t.Errorf("the fake's lines %q, want the root command and one password line", j.lines)
		}
		if got := j.debugWith("device sudo answered "); len(got) != 1 || got[0] != fmt.Sprintf("device sudo answered target=%q index=1", j.name) {
			t.Errorf("answered lines %q", got)
		}
		j.noSecret(t, wrong)
	})
}

// TestRootCommandNoPasswordAsked: a NOPASSWD rule with a password held: sudo
// asks nothing, the password is never written (the fake read no line), the
// command runs as root with an empty stderr, and nothing says sudo was
// answered.
func TestRootCommandNoPasswordAsked(t *testing.T) {
	eachSudo(t, func(t *testing.T, transport string, rs bool) {
		j := runSudo(t, transport, fakedevice.Options{SudoRS: rs}, sudoSecret, []string{"whoami"}, []bool{true})
		if len(j.recs) != 1 || j.recs[0].Status != "success" || j.recs[0].Output != "root\n" || stderrOf(j.recs[0]) != "" {
			t.Fatalf("records %s", describe(j.recs))
		}
		if len(j.lines) != 1 || sudoWireLine.FindStringSubmatch(j.lines[0]) == nil {
			t.Errorf("the fake's lines %q", j.lines)
		}
		if starts := j.debugWith("device command start "); len(starts) != 1 || !strings.HasSuffix(starts[0], " sudo=password") {
			t.Errorf("start lines %q", starts)
		}
		if got := j.debugWith("device sudo answered "); len(got) != 0 {
			t.Errorf("answered lines %q", got)
		}
		j.noSecret(t, sudoSecret)
	})
}

// TestRootCommandWithoutPassword: a grant with no enable password sends
// sudo -n -- TEXT (sudo=none): a NOPASSWD rule runs it as root; a rule that
// wants a password is sudo's own refusal, in its own words,
// command_exit_nonzero.
func TestRootCommandWithoutPassword(t *testing.T) {
	eachSudo(t, func(t *testing.T, transport string, rs bool) {
		j := runSudo(t, transport, fakedevice.Options{SudoRS: rs}, "", []string{"whoami"}, []bool{true})
		if len(j.recs) != 1 || j.recs[0].Status != "success" || j.recs[0].Output != "root\n" {
			t.Fatalf("NOPASSWD: records %s", describe(j.recs))
		}
		if len(j.lines) != 1 || j.lines[0] != "exec: sudo -n -- whoami" {
			t.Errorf("NOPASSWD: the fake's lines %q", j.lines)
		}
		if starts := j.debugWith("device command start "); len(starts) != 1 || !strings.HasSuffix(starts[0], " sudo=none") {
			t.Errorf("start lines %q", starts)
		}
		j = runSudo(t, transport, fakedevice.Options{SudoAsks: true, SudoRS: rs, Enable: sudoSecret}, "", []string{"whoami"}, []bool{true})
		want := "sudo: a password is required\n"
		if rs {
			want = "sudo: interactive authentication is required\n"
		}
		if len(j.recs) != 1 || j.recs[0].Error == nil || j.recs[0].Error.Code != "command_exit_nonzero" || stderrOf(j.recs[0]) != want {
			t.Fatalf("a password wanted: records %s stderr %q", describe(j.recs), stderrOf(j.recs[0]))
		}
	})
}
