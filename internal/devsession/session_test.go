package devsession

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
)

// shell is an in-memory device shell: what the fake IOS XE server answers,
// as a Stream. It records every line it receives; a secret line is "<secret>".
type shellOptions struct {
	hostname   string
	privileged bool
	enable     string // "" means enable needs no secret
	silent     bool   // never print a prompt
	delay      map[string]time.Duration
	// secretDelay is the wait before the answer to the enable secret.
	secretDelay time.Duration
	// echoSecret writes the enable secret back before answering it, as a
	// misconfigured device or a terminal server may.
	echoSecret bool
	bigLines   int
	rejectPage bool
	// reloadDrops makes a confirmed reload drop the connection instead of
	// going silent.
	reloadDrops bool
	// unsaved starts the shell with the running configuration modified, so
	// reload asks "Save? [yes/no]: " first, as the fake's -unsaved does.
	unsaved bool
}

type shell struct {
	shellOptions

	toDevice   *io.PipeWriter // session writes here
	fromDevice *io.PipeReader // session reads here
	deviceIn   *io.PipeReader
	deviceOut  *io.PipeWriter

	mu     sync.Mutex
	lines  []string
	closed bool
	config bool // in global configuration mode; the serving goroutine's own
}

func newShell(opts shellOptions) *shell {
	s := &shell{shellOptions: opts}
	if s.hostname == "" {
		s.hostname = "Router"
	}
	s.deviceIn, s.toDevice = io.Pipe()
	s.fromDevice, s.deviceOut = io.Pipe()
	go s.run()
	return s
}

func (s *shell) Read(p []byte) (int, error)  { return s.fromDevice.Read(p) }
func (s *shell) Write(p []byte) (int, error) { return s.toDevice.Write(p) }
func (s *shell) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	_ = s.toDevice.Close()
	_ = s.fromDevice.Close()
	return nil
}
func (s *shell) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}
func (s *shell) record(l string) {
	s.mu.Lock()
	s.lines = append(s.lines, l)
	s.mu.Unlock()
}
func (s *shell) prompt() string {
	if s.config {
		return s.hostname + "(config)#"
	}
	if s.privileged {
		return s.hostname + "#"
	}
	return s.hostname + ">"
}
func (s *shell) w(text string) bool {
	_, err := io.WriteString(s.deviceOut, text)
	return err == nil
}

func (s *shell) run() {
	defer s.deviceOut.Close()
	if s.silent {
		io.Copy(io.Discard, s.deviceIn)
		return
	}
	if !s.w("\r\nfake banner\r\n" + s.prompt()) {
		return
	}
	// Bytes, as the fake device reads them: a CR or an LF ends a line, CR LF
	// is one ending, and a [confirm] takes one key without a line.
	reader := bufio.NewReader(s.deviceIn)
	var line []byte
	hidden := false
	confirm := "" // the command waiting at its [confirm]
	value := ""   // the command waiting at its value prompt for one line
	unsaved := s.unsaved
	lastCR := false
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return
		}
		if b == '\n' && lastCR {
			lastCR = false
			continue
		}
		lastCR = b == '\r'
		if confirm != "" {
			command := confirm
			confirm = ""
			if b != '\r' && b != '\n' && b != 'y' && b != 'Y' {
				s.record("<abandon>")
				s.w("\r\n" + s.prompt())
				continue
			}
			s.record("<confirm>")
			if command == "reload" {
				s.w("\r\n")
				if s.reloadDrops {
					return // the device drops the connection
				}
				io.Copy(io.Discard, reader) // nothing more, the connection up
				return
			}
			s.w("\r\n" + s.prompt())
			continue
		}
		if b != '\n' && b != '\r' {
			line = append(line, b)
			continue
		}
		text := string(line)
		line = line[:0]
		if hidden {
			hidden = false
			s.record("<secret>")
			if s.echoSecret {
				s.w(text)
			}
			time.Sleep(s.secretDelay)
			if s.enable == "" || text == s.enable {
				s.privileged = true
				if !s.w("\r\n" + s.prompt()) {
					return
				}
			} else if !s.w("\r\n% Bad secrets\r\n\r\n" + s.prompt()) {
				return
			}
			continue
		}
		echo := text + "\r\n" // the PTY echoes the line
		if value != "" {
			// A value prompt takes the whole line, echoed.
			s.record("<value:" + text + ">")
			switch value {
			case "copy":
				value, unsaved = "", false
				s.w(echo + "Building configuration...\r\n[OK]\r\n" + s.prompt())
			case "reload":
				switch strings.ToLower(text) {
				case "y", "yes":
					value, unsaved, confirm = "", false, "reload"
					s.w(echo + "Building configuration...\r\n[OK]\r\nProceed with reload? [confirm]")
				case "n", "no":
					value, confirm = "", "reload"
					s.w(echo + "Proceed with reload? [confirm]")
				default:
					s.w(echo + "System configuration has been modified. Save? [yes/no]: ")
				}
			}
			continue
		}
		s.record(text)
		if d := s.delay[text]; d > 0 {
			time.Sleep(d)
		}
		switch text {
		case "":
			s.w("\r\n" + s.prompt())
		case "enable":
			if s.privileged {
				s.w(echo + s.prompt())
				continue
			}
			if s.enable == "" {
				s.privileged = true
				s.w(echo + s.prompt())
				continue
			}
			hidden = true
			s.w(echo + "Password: ")
		case "configure terminal":
			// Global configuration mode: the prompt changes, and "end" answers
			// with nothing but the next prompt.
			s.config = true
			s.w(echo + "Enter configuration commands, one per line.  End with CNTL/Z.\r\n" + s.prompt())
		case "end":
			s.config = false
			s.w(echo + s.prompt())
		case "exit":
			s.w(echo)
			return
		case "terminal length 0", "terminal width 512":
			if s.rejectPage {
				s.w(echo + "     ^\r\n% Invalid input detected at '^' marker.\r\n\r\n" + s.prompt())
			} else {
				s.w(echo + s.prompt())
			}
		case "show clock":
			s.w(echo + "*10:00:00.000 UTC Tue Sep 15 2026\r\n" + s.prompt())
		case "show big":
			var b strings.Builder
			b.WriteString(echo)
			for i := 0; i < s.bigLines; i++ {
				fmt.Fprintf(&b, "line %06d %s\r\n", i, strings.Repeat("x", 60))
			}
			b.WriteString(s.prompt())
			s.w(b.String())
		case "show slow":
			s.w(echo + "slow output\r\n" + s.prompt())
		case "clear counters":
			confirm = text
			s.w(echo + "Clear \"show interface\" counters on all interfaces [confirm]")
		case "reload":
			if unsaved {
				value = "reload"
				s.w(echo + "System configuration has been modified. Save? [yes/no]: ")
				continue
			}
			confirm = text
			s.w(echo + "Proceed with reload? [confirm]")
		case "copy running-config startup-config":
			value = "copy"
			s.w(echo + "Destination filename [startup-config]? ")
		default:
			s.w(echo + "     ^\r\n% Invalid input detected at '^' marker.\r\n\r\n" + s.prompt())
		}
	}
}

func iosxe(t *testing.T) platform.Definition {
	t.Helper()
	def, ok := platform.Builtin("cisco_iosxe")
	if !ok {
		t.Fatal("no cisco_iosxe built-in")
	}
	return def
}

func secret(v string) func(func([]byte) error) error {
	return func(f func([]byte) error) error { return f([]byte(v)) }
}

func open(t *testing.T, sh *shell, opts Options) *Session {
	t.Helper()
	opts.LoginTimeout = 3 * time.Second
	opts.EnableTimeout = 2 * time.Second
	opts.PromptTimeout = 2 * time.Second
	s, err := Open(context.Background(), sh, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func TestUserExecEscalatesOnceThenPagingThenCommands(t *testing.T) {
	sh := newShell(shellOptions{enable: "en"})
	s := open(t, sh, Options{Definition: iosxe(t), EnableSecret: secret("en")})
	if s.Level() != "exec" || s.Prompt() != "Router>" {
		t.Fatalf("after open level=%q prompt=%q", s.Level(), s.Prompt())
	}
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if s.Level() != "privilege-exec" || s.Prompt() != "Router#" {
		t.Fatalf("after prepare level=%q prompt=%q", s.Level(), s.Prompt())
	}
	r := s.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 2 * time.Second})
	if r.Err != nil || string(r.Output) != "*10:00:00.000 UTC Tue Sep 15 2026\n" || r.Prompt != "Router#" || r.PromptSource != "observed" || *r.ConnectionReused {
		t.Fatalf("show clock: %+v output=%q", r, r.Output)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	want := []string{"enable", "<secret>", "terminal length 0", "terminal width 512", "show clock", "exit"}
	if got := sh.Lines(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("device saw %q, want %q", got, want)
	}
}

func TestPrivilegedStartSendsNothingForPrivilege(t *testing.T) {
	sh := newShell(shellOptions{privileged: true, enable: "en"})
	s := open(t, sh, Options{Definition: iosxe(t), EnableSecret: secret("en")})
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	s.Close()
	time.Sleep(50 * time.Millisecond)
	if got := sh.Lines(); got[0] != "terminal length 0" {
		t.Fatalf("device saw %q; enable must not be sent at a privileged prompt", got)
	}
}

func TestWrongEnableSecretIsOneAttemptAndEndsTheSession(t *testing.T) {
	sh := newShell(shellOptions{enable: "right"})
	s := open(t, sh, Options{Definition: iosxe(t), EnableSecret: secret("wrong")})
	err := s.Prepare(context.Background())
	if errorcodes.Of(err) != "privilege_failed" {
		t.Fatalf("prepare error = %v, want privilege_failed", err)
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Fatalf("the secret leaked into the error: %v", err)
	}
	if !strings.Contains(err.Error(), "Router>") {
		t.Fatalf("the error does not name the observed prompt: %v", err)
	}
	attempts := 0
	for _, l := range sh.Lines() {
		if l == "enable" {
			attempts++
		}
	}
	if attempts != 1 {
		t.Fatalf("enable attempts = %d, want 1 (lines %q)", attempts, sh.Lines())
	}
	r := s.Execute(context.Background(), platform.Command{Text: "show clock"})
	if r.ErrorCode != "command_session_lost" {
		t.Fatalf("after a failed privilege step the session must be ended, got %+v", r)
	}
}

// TestEnableTimeoutBoundsTheWholeStep: the
// Password: prompt and the privileged prompt each arrive within the enable
// timeout, the step as a whole does not, and that is privilege_failed.
func TestEnableTimeoutBoundsTheWholeStep(t *testing.T) {
	sh := newShell(shellOptions{enable: "en", delay: map[string]time.Duration{"enable": 1200 * time.Millisecond}, secretDelay: 1200 * time.Millisecond})
	s := open(t, sh, Options{Definition: iosxe(t), EnableSecret: secret("en")}) // a 2s enable timeout
	start := time.Now()
	err := s.Prepare(context.Background())
	if errorcodes.Of(err) != "privilege_failed" || !strings.Contains(err.Error(), `after the enable secret within 2s of "enable"`) {
		t.Fatalf("prepare error = %v, want privilege_failed naming the step's bound", err)
	}
	if elapsed := time.Since(start); elapsed < 1900*time.Millisecond || elapsed > 2300*time.Millisecond {
		t.Fatalf("the step ended after %s, want the 2s enable timeout", elapsed)
	}
	if s.Usable() {
		t.Fatal("the session must be ended")
	}
}

func TestEnableSecretAskedButAbsent(t *testing.T) {
	sh := newShell(shellOptions{enable: "en"})
	s := open(t, sh, Options{Definition: iosxe(t)})
	err := s.Prepare(context.Background())
	if errorcodes.Of(err) != "privilege_failed" || !strings.Contains(err.Error(), "credential has none") {
		t.Fatalf("prepare error = %v", err)
	}
}

func TestEnableWithoutSecretPrompt(t *testing.T) {
	sh := newShell(shellOptions{enable: ""})
	s := open(t, sh, Options{Definition: iosxe(t)})
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if s.Level() != "privilege-exec" {
		t.Fatalf("level = %q", s.Level())
	}
}

// setupText is SetupLines as the text file shows them, one string per
// statement: the prompt, the statement, and after "=" the answer.
func setupText(s *Session) string {
	var lines []string
	for _, l := range s.SetupLines() {
		lines = append(lines, l.PromptBefore+l.Statement+"="+l.Output)
	}
	return strings.Join(lines, "|")
}

// TestSetupLinesAreWhatPrepareSent: each set-up statement with the prompt
// it was sent at, as the device wrote it, and the device's answer.
// enable's answer is the device's
// `Password:`; the secret is not a statement; a session that starts
// privileged sends no enable and has no line for it; a failed step is the
// last line.
func TestSetupLinesAreWhatPrepareSent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shell shellOptions
		enter func(func([]byte) error) error
		code  string
		want  string
	}{
		{name: "enable with a secret, then paging", shell: shellOptions{enable: "en"}, enter: secret("en"),
			want: "Router>enable=Password:\n|Router#terminal length 0=|Router#terminal width 512="},
		{name: "enable without a secret prompt", shell: shellOptions{},
			want: "Router>enable=|Router#terminal length 0=|Router#terminal width 512="},
		{name: "privileged at login", shell: shellOptions{privileged: true},
			want: "Router#terminal length 0=|Router#terminal width 512="},
		{name: "a wrong secret", shell: shellOptions{enable: "right"}, enter: secret("wrong"), code: "privilege_failed",
			want: "Router>enable=Password:\n"},
		{name: "a secret asked, none resolved", shell: shellOptions{enable: "en"}, code: "privilege_failed",
			want: "Router>enable=Password:\n"},
		{name: "paging rejected", shell: shellOptions{privileged: true, rejectPage: true}, code: "paging_disable_failed",
			want: "Router#terminal length 0=     ^\n% Invalid input detected at '^' marker.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := open(t, newShell(tc.shell), Options{Definition: iosxe(t), EnableSecret: tc.enter})
			defer s.Close()
			if err := s.Prepare(context.Background()); errorcodes.Of(err) != tc.code && !(tc.code == "" && err == nil) {
				t.Fatalf("prepare: %v, want %q", err, tc.code)
			}
			if got := setupText(s); got != tc.want {
				t.Fatalf("set-up lines\n got %q\nwant %q", got, tc.want)
			}
		})
	}
	var none *Session
	if none.SetupLines() != nil {
		t.Fatal("a session that never opened has no set-up lines")
	}
}

// TestAnEchoedEnableSecretReachesNothing: a device that echoes the secret
// puts it in the bytes read after it. Nothing of that read is kept: not in
// the set-up lines, and not in the message of the timeout that follows,
// which through v0.12.1 quoted the read's last line, the secret itself.
func TestAnEchoedEnableSecretReachesNothing(t *testing.T) {
	const enable = "Zq7-echoed-SECRET"
	for _, tc := range []struct {
		name  string
		delay time.Duration
		code  string
	}{
		{name: "the prompt returns"},
		{name: "no prompt within the enable timeout", delay: 3 * time.Second, code: "privilege_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := open(t, newShell(shellOptions{enable: enable, echoSecret: true, secretDelay: tc.delay}), Options{Definition: iosxe(t), EnableSecret: secret(enable)})
			defer s.Close()
			err := s.Prepare(context.Background())
			if errorcodes.Of(err) != tc.code && !(tc.code == "" && err == nil) {
				t.Fatalf("prepare: %v, want %q", err, tc.code)
			}
			if err != nil && strings.Contains(err.Error(), enable) {
				t.Fatalf("the echoed secret is in the error: %v", err)
			}
			if got := setupText(s); strings.Contains(got, enable) || !strings.HasPrefix(got, "Router>enable=Password:\n") {
				t.Fatalf("set-up lines %q", got)
			}
		})
	}
}

func TestPagingRejectionIsPagingDisableFailed(t *testing.T) {
	sh := newShell(shellOptions{privileged: true, rejectPage: true})
	s := open(t, sh, Options{Definition: iosxe(t)})
	err := s.Prepare(context.Background())
	if errorcodes.Of(err) != "paging_disable_failed" || !strings.Contains(err.Error(), "terminal length 0") {
		t.Fatalf("prepare error = %v", err)
	}
}

func TestDeviceErrorFromThePlatformPatternsAndNoneForGeneric(t *testing.T) {
	sh := newShell(shellOptions{privileged: true})
	s := open(t, sh, Options{Definition: iosxe(t)})
	r := s.Execute(context.Background(), platform.Command{Text: "show bogus"})
	if r.ErrorCode != "device_command_error" || !r.DeviceError || r.ErrorCategory != "device" || !strings.Contains(string(r.Output), "% Invalid input") {
		t.Fatalf("cisco_iosxe device error: %+v output=%q", r, r.Output)
	}
	s.Close()

	generic, _ := platform.Builtin("generic")
	sh2 := newShell(shellOptions{privileged: true})
	s2 := open(t, sh2, Options{Definition: generic})
	if err := s2.Prepare(context.Background()); err != nil {
		t.Fatalf("generic prepare: %v", err)
	}
	r = s2.Execute(context.Background(), platform.Command{Text: "show bogus"})
	if r.Err != nil || r.DeviceError || !strings.Contains(string(r.Output), "% Invalid input") {
		t.Fatalf("generic must not check command errors: %+v output=%q", r, r.Output)
	}
	s2.Close()
	time.Sleep(50 * time.Millisecond)
	if got := sh2.Lines(); got[0] != "show bogus" {
		t.Fatalf("generic sent setup commands: %q", got)
	}
}

func TestCommandTimeoutEndsTheSession(t *testing.T) {
	sh := newShell(shellOptions{privileged: true, delay: map[string]time.Duration{"show slow": 2 * time.Second}})
	s := open(t, sh, Options{Definition: iosxe(t)})
	started := time.Now()
	r := s.Execute(context.Background(), platform.Command{Text: "show slow", Timeout: 300 * time.Millisecond})
	if r.ErrorCode != "command_timeout" || r.ErrorCategory != "timeout" || !r.Retryable || *r.PromptObserved {
		t.Fatalf("timeout result: %+v", r)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the timeout was reported after %s; it must be bounded by the command's own deadline", elapsed)
	}
	r = s.Execute(context.Background(), platform.Command{Text: "show clock"})
	if r.ErrorCode != "command_session_lost" {
		t.Fatalf("after a timeout the session must be ended, got %+v", r)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	for _, l := range sh.Lines() {
		if l == "exit" {
			t.Fatal("exit must not be sent on a desynchronised shell")
		}
	}
}

// TestUsableAndConnectionReused: connection_reused is false on the first
// command written and true
// after it, Prepare's commands not counting; a device error and output over
// the limit after the prompt leave the session usable; a timeout, output
// over the limit before the prompt, and a lost stream end it, and a later
// command is refused without being written.
func TestUsableAndConnectionReused(t *testing.T) {
	// 56 lines of 73 bytes settle to exactly the limit before the prompt
	// (the last line's newline is the tail's until the prompt line lands);
	// the closing newline at the prompt passes it: the limit after the
	// prompt, the shell in step.
	sh := newShell(shellOptions{enable: "en", bigLines: 56, delay: map[string]time.Duration{"show slow": 2 * time.Second}})
	s := open(t, sh, Options{Definition: iosxe(t), EnableSecret: secret("en"), MaxOutputBytes: 56*73 - 1})
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	reused := []bool{}
	for _, text := range []string{"show clock", "show bogus", "show big", "show clock"} {
		r := s.Execute(context.Background(), platform.Command{Text: text, Timeout: 2 * time.Second})
		if r.ConnectionReused == nil {
			t.Fatalf("%s: connection_reused unset", text)
		}
		reused = append(reused, *r.ConnectionReused)
		if !s.Usable() {
			t.Fatalf("%s (%s) ended the session", text, r.ErrorCode)
		}
		if text == "show big" && r.ErrorCode != "output_limit_exceeded" {
			t.Fatalf("show big after the prompt: %+v", r)
		}
	}
	if fmt.Sprint(reused) != "[false true true true]" {
		t.Fatalf("connection_reused %v, want [false true true true]", reused)
	}
	r := s.Execute(context.Background(), platform.Command{Text: "show slow", Timeout: 300 * time.Millisecond})
	if r.ErrorCode != "command_timeout" || s.Usable() {
		t.Fatalf("timeout: %+v usable=%v", r, s.Usable())
	}
	r = s.Execute(context.Background(), platform.Command{Text: "show version"})
	if r.ErrorCode != "command_session_lost" || r.ConnectionReused != nil {
		t.Fatalf("after the timeout: %+v", r)
	}
	s.Close()
	time.Sleep(50 * time.Millisecond)
	for _, l := range sh.Lines() {
		if l == "show version" {
			t.Fatal("a command was written to an ended session")
		}
	}

	big := newShell(shellOptions{privileged: true, bigLines: 2000})
	s = open(t, big, Options{Definition: iosxe(t), MaxOutputBytes: 4096})
	if r := s.Execute(context.Background(), platform.Command{Text: "show big", Timeout: 5 * time.Second}); r.ErrorCode != "output_limit_exceeded" || s.Usable() {
		t.Fatalf("limit before the prompt: %s usable=%v", r.ErrorCode, s.Usable())
	}
	s.Close()

	lost := newShell(shellOptions{privileged: true})
	s = open(t, lost, Options{Definition: iosxe(t)})
	_ = lost.deviceOut.Close()
	if r := s.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 2 * time.Second}); r.ErrorCode != "command_session_lost" || s.Usable() {
		t.Fatalf("lost stream: %+v usable=%v", r, s.Usable())
	}
	s.Close()
	if s.Usable() {
		t.Fatal("a closed session is usable")
	}
}

func TestOutputLimit(t *testing.T) {
	sh := newShell(shellOptions{privileged: true, bigLines: 2000})
	s := open(t, sh, Options{Definition: iosxe(t), MaxOutputBytes: 4096})
	r := s.Execute(context.Background(), platform.Command{Text: "show big", Timeout: 5 * time.Second})
	if r.ErrorCode != "output_limit_exceeded" || r.ErrorCategory != "output" || len(r.Output) != 4096 {
		t.Fatalf("limit result: code=%s category=%s bytes=%d err=%v", r.ErrorCode, r.ErrorCategory, len(r.Output), r.Err)
	}
	// The record holds exactly the first limit settled bytes: the cleaned
	// output's own prefix, no echo, no carriage return, and
	// observed is the settled count at the cut, above the limit by less than
	// one settled piece.
	if !strings.HasPrefix(string(r.Output), "line 000000 xxxx") || strings.Contains(string(r.Output), "\r") {
		t.Fatalf("the limited output is not the cleaned prefix: %q", r.Output[:40])
	}
	if !strings.Contains(r.Err.Error(), "4096 bytes") {
		t.Fatalf("the message must carry the configured count: %v", r.Err)
	}
	var observed int
	if _, err := fmt.Sscanf(r.Err.Error(), "command output exceeded 4096 bytes (%d observed)", &observed); err != nil || observed <= 4096 || observed > 4096+4096+73 {
		t.Fatalf("observed %d from %q (%v)", observed, r.Err, err)
	}
}

func TestLoginTimeoutWithoutAPrompt(t *testing.T) {
	sh := newShell(shellOptions{silent: true})
	_, err := Open(context.Background(), sh, Options{Definition: iosxe(t), LoginTimeout: 300 * time.Millisecond})
	if errorcodes.Of(err) != "command_session_prompt_timeout" {
		t.Fatalf("open error = %v", err)
	}
}

func TestValidateRejectsAnUnknownPrivilegedLevel(t *testing.T) {
	def := iosxe(t)
	def.PrivilegedLevel = "privilege_exec"
	err := Validate(def)
	if errorcodes.Of(err) != "platform_privilege_level_unknown" || !strings.Contains(err.Error(), "privilege_exec") {
		t.Fatalf("validate = %v", err)
	}
	def.PrivilegedLevel = "privilege-exec"
	if err := Validate(def); err != nil {
		t.Fatalf("validate built-in: %v", err)
	}
	generic, _ := platform.Builtin("generic")
	if err := Validate(generic); err != nil {
		t.Fatalf("validate generic: %v", err)
	}
	bad := generic
	bad.PromptPattern = "("
	if errorcodes.Of(Validate(bad)) != "platform_prompt_pattern_invalid" {
		t.Fatalf("validate bad pattern = %v", Validate(bad))
	}
}

func TestCleanResponseKeepsLegitimateOutput(t *testing.T) {
	raw := []byte("show clock\r\nshow clock\r\n12:00:00 UTC\r\nrouter#")
	if got, _ := settleChunks(t, [][]byte{raw}, "show clock", "router#", "router#"); string(got) != "show clock\n12:00:00 UTC\n" {
		t.Fatalf("cleaned response = %q", got)
	}
}

// renderedLastLine is the last line of data as a read sees it: rendered,
// without its trailing blanks.
func renderedLastLine(data string) string {
	r := newResponse("", "", newSettled(1<<20, Spool{}, 0, nil, nil))
	_ = r.feed([]byte(data))
	return r.lastLine()
}

func TestPromptMatchingShapes(t *testing.T) {
	p, err := compile(iosxe(t))
	if err != nil {
		t.Fatal(err)
	}
	for line, want := range map[string]string{
		"Router>":               "exec",
		"sw-01.lab#":            "privilege-exec",
		"Router(config)#":       "configuration",
		"Router(config-if)#":    "configuration",
		"banner ends in #":      "",
		"Router# ":              "privilege-exec",
		"\x1b[0mRouter#\x1b[0m": "privilege-exec",
		// a window title and a mode switch, as bash's prompt carries them
		"\x1b[?2004h\x1b]0;netops@dev: ~\x07Router#": "privilege-exec",
		"Routx\ber#": "privilege-exec",
	} {
		_, level, ok := p.match(renderedLastLine("stuff\r\n" + line))
		if (want == "") == ok || level != want {
			t.Fatalf("%q: level=%q ok=%v want %q", line, level, ok, want)
		}
	}
	generic, _ := platform.Builtin("generic")
	g, _ := compile(generic)
	for line, want := range map[string]bool{"dev#": true, "server01$": true, "user@host:~$": false, "Ready.": false} {
		if _, _, ok := g.match(renderedLastLine("x\n" + line)); ok != want {
			t.Fatalf("generic %q: ok=%v want %v", line, ok, want)
		}
	}
}

// TestBlindReturnIsConsumedByTheConfirm: the returns go in the command's
// write, the [confirm] takes one,
// the prompt returns, the output is the whole exchange, and the session goes
// on; a return the command does not consume is answered with another
// prompt, which the output keeps.
func TestBlindReturnIsConsumedByTheConfirm(t *testing.T) {
	sh := newShell(shellOptions{privileged: true})
	s := open(t, sh, Options{Definition: iosxe(t)})
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := s.Execute(context.Background(), platform.Command{Text: "clear counters", Blind: true, BlindReturns: 1, Timeout: 2 * time.Second})
	if r.Err != nil || len(r.Notices) != 0 || r.Prompt != "Router#" || r.PromptObserved == nil || !*r.PromptObserved {
		t.Fatalf("confirmed command: %+v", r)
	}
	if got := string(r.Output); got != "Clear \"show interface\" counters on all interfaces [confirm]\n" {
		t.Fatalf("the output must be the exchange, got %q", got)
	}
	if !s.Usable() {
		t.Fatal("the session must go on after a confirmed command")
	}
	r = s.Execute(context.Background(), platform.Command{Text: "show clock", Blind: true, BlindReturns: 1, Timeout: 2 * time.Second})
	if r.Err != nil || len(r.Notices) != 0 {
		t.Fatalf("spare return: %+v", r)
	}
	if got := string(r.Output); got != "*10:00:00.000 UTC Tue Sep 15 2026\nRouter#\n" {
		t.Fatalf("a spare return's extra prompt stays in the output, got %q", got)
	}
	r = s.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 2 * time.Second})
	if r.Err != nil || string(r.Output) != "*10:00:00.000 UTC Tue Sep 15 2026\n" {
		t.Fatalf("after the blind commands: %+v", r)
	}
	// The spare return is the empty line the device answers with a prompt.
	want := "terminal length 0|terminal width 512|clear counters|<confirm>|show clock||show clock"
	if got := strings.Join(sh.Lines(), "|"); got != want {
		t.Fatalf("device saw %q, want %q", got, want)
	}
}

// TestBlindSendWithoutAReturningPromptIsASuccessWithTheNotice: the blind
// wait passes (reload, nothing more written), or the stream ends during it
// (the device drops the connection): the record is a success holding all
// bytes read with prompt_not_observed_after_blind_send, and the session
// ends without the exit commands.
func TestBlindSendWithoutAReturningPromptIsASuccessWithTheNotice(t *testing.T) {
	for _, tc := range []struct {
		name   string
		drops  bool
		wait   time.Duration
		reason string
	}{
		{"the wait passes", false, 300 * time.Millisecond, "blind_wait_expired"},
		{"the stream ends", true, 5 * time.Second, "session_ended"},
		{"a wait of zero", false, 0, "blind_wait_expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := newShell(shellOptions{privileged: true, reloadDrops: tc.drops})
			s := open(t, sh, Options{Definition: iosxe(t)})
			if err := s.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			r := s.Execute(context.Background(), platform.Command{Text: "reload", Blind: true, BlindReturns: 1, Timeout: tc.wait})
			if r.Err != nil || r.ErrorCode != "" || r.Prompt != "" || r.PromptObserved == nil || *r.PromptObserved {
				t.Fatalf("%s: %+v", tc.name, r)
			}
			if len(r.Notices) != 1 || r.Notices[0].Code != "prompt_not_observed_after_blind_send" || r.Notices[0].Details["reason"] != tc.reason || r.Notices[0].Details["blind_returns"] != 1 || r.Notices[0].Details["blind_wait"] != tc.wait.String() {
				t.Fatalf("%s: notices %+v", tc.name, r.Notices)
			}
			if tc.wait > 0 && !strings.Contains(string(r.Output), "Proceed with reload? [confirm]") {
				t.Fatalf("%s: the output must hold all bytes read, got %q", tc.name, r.Output)
			}
			if elapsed := time.Since(started); elapsed > 2*time.Second {
				t.Fatalf("%s: took %s", tc.name, elapsed)
			}
			if s.Usable() {
				t.Fatalf("%s: the session must be ended", tc.name)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if slices.Contains(sh.Lines(), "exit") {
				t.Fatalf("%s: exit must not be sent on a shell out of step", tc.name)
			}
		})
	}
}

// TestBlindWaitUnderAParentDeadline: a deadline above the command (the
// device's) that passes during the blind wait is not the notice; the
// caller names it (the executor: device_timeout).
func TestBlindWaitUnderAParentDeadline(t *testing.T) {
	sh := newShell(shellOptions{privileged: true})
	s := open(t, sh, Options{Definition: iosxe(t)})
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	r := s.Execute(ctx, platform.Command{Text: "reload", Blind: true, BlindReturns: 1, Timeout: 5 * time.Second})
	if r.Err == nil || len(r.Notices) != 0 || r.PromptObserved == nil || *r.PromptObserved {
		t.Fatalf("parent deadline: %+v", r)
	}
}

func expect(pattern, response string) platform.Expectation {
	return platform.Expectation{Pattern: regexp.MustCompile(pattern), Response: response}
}

// TestExpectationAnswersAValuePrompt: the copy's value prompt is matched
// with the
// device's trailing space kept, answered with a bare return that takes the
// default or with a name, the exchange is the output, and the session goes
// on under the ordinary record.
func TestExpectationAnswersAValuePrompt(t *testing.T) {
	sh := newShell(shellOptions{privileged: true})
	s := open(t, sh, Options{Definition: iosxe(t)})
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	copyCmd := "copy running-config startup-config"
	// A $-anchored pattern must account for the trailing space.
	r := s.Execute(context.Background(), platform.Command{Text: copyCmd, Timeout: 2 * time.Second, Expectations: []platform.Expectation{expect(`filename \[startup-config\]\? $`, "")}})
	if r.Err != nil || len(r.Notices) != 0 || r.Prompt != "Router#" || r.PromptObserved == nil || !*r.PromptObserved {
		t.Fatalf("copy with the default: %+v", r)
	}
	if got := string(r.Output); got != "Destination filename [startup-config]? \nBuilding configuration...\n[OK]\n" {
		t.Fatalf("the output is the exchange, got %q", got)
	}
	r = s.Execute(context.Background(), platform.Command{Text: copyCmd, Timeout: 2 * time.Second, Expectations: []platform.Expectation{expect(`filename \[startup-config\]\?`, "backup-config")}})
	if r.Err != nil || string(r.Output) != "Destination filename [startup-config]? backup-config\nBuilding configuration...\n[OK]\n" {
		t.Fatalf("copy with a name: %+v %q", r, r.Output)
	}
	if !s.Usable() {
		t.Fatal("the session must go on")
	}
	r = s.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: 2 * time.Second})
	if r.Err != nil || string(r.Output) != "*10:00:00.000 UTC Tue Sep 15 2026\n" {
		t.Fatalf("after the copies: %+v", r)
	}
	want := "terminal length 0|terminal width 512|" + copyCmd + "|<value:>|" + copyCmd + "|<value:backup-config>|show clock"
	if got := strings.Join(sh.Lines(), "|"); got != want {
		t.Fatalf("device saw %q, want %q", got, want)
	}
}

// TestExpectationsAreConsumedOnceInDeclaredOrder: a blind reload under an
// unsaved configuration is asked to save;
// two declarations with the same pattern answer the twice-asked prompt in
// turn (the first answer is re-asked), the confirm's bare return follows,
// a declaration for a prompt never asked stays unconsumed without a notice,
// and the mark keeps the save pattern from firing again on its own echo.
// The record is the blind-send success with the notice, whose message has no
// returns clause for a command blind by the flag alone; and on a saved
// configuration the save declaration is simply not consumed.
func TestExpectationsAreConsumedOnceInDeclaredOrder(t *testing.T) {
	declarations := []platform.Expectation{
		expect(`Never asked`, "x"),
		expect(`Save\? \[yes/no\]:`, "maybe"),
		expect(`Save\? \[yes/no\]:`, "y"),
		expect(`confirm\]`, ""),
	}
	for _, tc := range []struct {
		name    string
		unsaved bool
		lines   string
		output  string
	}{
		{"unsaved", true, "reload|<value:maybe>|<value:y>|<confirm>", "System configuration has been modified. Save? [yes/no]: maybe\nSystem configuration has been modified. Save? [yes/no]: y\nBuilding configuration...\n[OK]\nProceed with reload? [confirm]\n"},
		{"saved", false, "reload|<confirm>", "Proceed with reload? [confirm]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := newShell(shellOptions{privileged: true, unsaved: tc.unsaved})
			s := open(t, sh, Options{Definition: iosxe(t)})
			if err := s.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			r := s.Execute(context.Background(), platform.Command{Text: "reload", Blind: true, Timeout: 500 * time.Millisecond, Expectations: declarations})
			if r.Err != nil || r.Prompt != "" || r.PromptObserved == nil || *r.PromptObserved || len(r.Notices) != 1 {
				t.Fatalf("blind reload: %+v", r)
			}
			n := r.Notices[0]
			if n.Code != "prompt_not_observed_after_blind_send" || n.Details["reason"] != "blind_wait_expired" || n.Details["blind_returns"] != 0 || !strings.HasPrefix(n.Message, "the prompt did not return after the command: the blind wait of 500ms passed") {
				t.Fatalf("notice: %+v", n)
			}
			if elapsed := time.Since(started); elapsed > 2*time.Second {
				t.Fatalf("took %s; the blind wait bounds the exchange", elapsed)
			}
			if !strings.HasSuffix(string(r.Output), tc.output) {
				t.Fatalf("output %q, want a suffix of %q", r.Output, tc.output)
			}
			if got := strings.Join(sh.Lines()[2:], "|"); got != tc.lines {
				t.Fatalf("device saw %q, want %q", got, tc.lines)
			}
			if s.Usable() {
				t.Fatal("the session must be ended")
			}
		})
	}
}

// TestMistypedPatternIsCommandTimeoutNamingTheLastLine: a declared pattern
// that never appears is the ordinary
// command_timeout under the command's own deadline, whose message names the
// last line seen and the count answered; the session ends.
func TestMistypedPatternIsCommandTimeoutNamingTheLastLine(t *testing.T) {
	sh := newShell(shellOptions{privileged: true})
	s := open(t, sh, Options{Definition: iosxe(t)})
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := s.Execute(context.Background(), platform.Command{Text: "copy running-config startup-config", Timeout: 300 * time.Millisecond, Expectations: []platform.Expectation{expect(`Destinaton`, "x")}})
	if r.ErrorCode != "command_timeout" || r.Err == nil || len(r.Notices) != 0 {
		t.Fatalf("mistyped pattern: %+v", r)
	}
	want := `command timed out after 300ms while waiting for a returning prompt; the last line seen was "Destination filename [startup-config]?" and 0 of 1 declared responses were answered; the session is closed`
	if r.Err.Error() != want {
		t.Fatalf("message %q, want %q", r.Err.Error(), want)
	}
	if s.Usable() {
		t.Fatal("the session must be ended")
	}
	if got := strings.Join(sh.Lines()[2:], "|"); got != "copy running-config startup-config" {
		t.Fatalf("device saw %q", got)
	}
}

// PromptBefore is the prompt the statement was typed at and Prompt the one
// that came back: they differ across a mode change, a statement may answer
// with nothing but the next prompt, and a statement that times out still
// has the prompt it was sent at.
func TestPromptBeforeIsThePromptTheStatementWasSentAt(t *testing.T) {
	sh := newShell(shellOptions{privileged: true, delay: map[string]time.Duration{"show slow": 2 * time.Second}})
	s := open(t, sh, Options{Definition: iosxe(t)})
	for _, tc := range []struct{ text, before, after, output string }{
		{"show clock", "Router#", "Router#", "*10:00:00.000 UTC Tue Sep 15 2026\n"},
		{"configure terminal", "Router#", "Router(config)#", "Enter configuration commands, one per line.  End with CNTL/Z.\n"},
		{"end", "Router(config)#", "Router#", ""},
	} {
		r := s.Execute(context.Background(), platform.Command{Text: tc.text, Timeout: 2 * time.Second})
		if r.Err != nil || r.PromptBefore != tc.before || r.Prompt != tc.after || string(r.Output) != tc.output {
			t.Fatalf("%s: promptbefore=%q prompt=%q output=%q err=%v, want %q %q %q", tc.text, r.PromptBefore, r.Prompt, r.Output, r.Err, tc.before, tc.after, tc.output)
		}
	}
	r := s.Execute(context.Background(), platform.Command{Text: "show slow", Timeout: 300 * time.Millisecond})
	if r.ErrorCode != "command_timeout" || r.PromptBefore != "Router#" || r.Prompt != "" {
		t.Fatalf("show slow: code=%q promptbefore=%q prompt=%q", r.ErrorCode, r.PromptBefore, r.Prompt)
	}
	// Not sent: the session is ended, and nothing was typed at any prompt.
	if r := s.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: time.Second}); r.ErrorCode != "command_session_lost" || r.PromptBefore != "" {
		t.Fatalf("after the timeout: code=%q promptbefore=%q", r.ErrorCode, r.PromptBefore)
	}
}
