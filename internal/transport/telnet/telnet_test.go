package telnet

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
)

// device answers each line on its end of the pipe: show slow never, show
// late after 450ms, show big with 3000 bytes, a device error for show bogus,
// show running-config in writes each cut just after a line's closing '#',
// show running-config | format in one write of lines ending in '>', a
// prompt otherwise; it records every line.
type device struct {
	mu    sync.Mutex
	lines []string
}

func (dv *device) serve(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		text := strings.TrimRight(line, "\r\n")
		dv.mu.Lock()
		dv.lines = append(dv.lines, text)
		dv.mu.Unlock()
		switch text {
		case "show slow":
		case "show late":
			// The delay is the behaviour under test (TestTelnetCommandOwnBounds):
			// an answer after a 300ms telnet.read-timeout and within a declared
			// 1s --timeout.
			time.Sleep(450 * time.Millisecond)
			_, _ = conn.Write([]byte(text + "\r\nlate\r\ndev#"))
		case "show big":
			_, _ = conn.Write([]byte(text + "\r\n" + strings.Repeat("x", 3000) + "\r\ndev#"))
		case "configure terminal":
			_, _ = conn.Write([]byte(text + "\r\ndev(config)#"))
		case "end":
			_, _ = conn.Write([]byte(text + "\r\ndev#"))
		case "show bogus":
			_, _ = conn.Write([]byte(text + "\r\n% Invalid input detected at '^' marker.\r\ndev#"))
		case "show running-config":
			for _, piece := range []string{text + "\r\nbanner motd ^C\r\n##########", "\r\n# Authorized access only #", "\r\n##########\r\n^C\r\nend\r\n", "dev#"} {
				_, _ = conn.Write([]byte(piece))
			}
		case "show running-config | format":
			_, _ = conn.Write([]byte(text + "\r\n<config>\r\n  <hostname>router1</hostname>\r\n</config>\r\ndev#"))
		default:
			_, _ = conn.Write([]byte(text + "\r\nout\r\ndev#"))
		}
	}
}

func (dv *device) saw() []string {
	dv.mu.Lock()
	defer dv.mu.Unlock()
	return append([]string(nil), dv.lines...)
}

// await is what the device has seen once it has seen n lines, or what it
// has seen after 5s.
func (dv *device) await(n int) []string {
	deadline := time.Now().Add(5 * time.Second)
	for len(dv.saw()) < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	return dv.saw()
}

// TestTimeoutEndsTheTelnetSession covers the session-ending rule
// for telnet: a device error leaves the session usable; a read timeout ends
// it, and a later command is refused without being written.
func TestTimeoutEndsTheTelnetSession(t *testing.T) {
	client, server := net.Pipe()
	dv := &device{}
	go dv.serve(server)
	d := &Driver{f: Factory{Config: configload.Snapshot{}, MaxOutputBytes: 1 << 20}, conn: client, prompt: []byte("dev#")}
	defer d.Close()
	ctx := context.Background()
	if r := d.Execute(ctx, platform.Command{Text: "show clock", Timeout: time.Second}); r.Err != nil || !d.Usable() {
		t.Fatalf("show clock: %+v usable=%v", r, d.Usable())
	}
	if r := d.Execute(ctx, platform.Command{Text: "show bogus", Timeout: time.Second}); r.ErrorCode != "device_command_error" || !d.Usable() {
		t.Fatalf("show bogus: %+v usable=%v", r, d.Usable())
	}
	if r := d.Execute(ctx, platform.Command{Text: "show slow", Timeout: 200 * time.Millisecond}); r.ErrorCode != "command_timeout" || d.Usable() {
		t.Fatalf("show slow: %+v usable=%v", r, d.Usable())
	}
	if r := d.Execute(ctx, platform.Command{Text: "show version", Timeout: time.Second}); r.ErrorCode != "command_session_lost" || r.ErrorCategory != "connection" {
		t.Fatalf("after the timeout: %+v", r)
	}
	if got := strings.Join(dv.await(3), "|"); got != "show clock|show bogus|show slow" {
		t.Fatalf("device saw %q", got)
	}
}

// TestTelnetCommandOwnBounds: a declared timeout replaces
// telnet.read-timeout for its command, where the configured command timeout
// stays capped by it, each message naming the bound that expired; a
// command's byte limit replaces the session's, named --maxbytes; a device
// deadline earlier than the command's leaves the code to the executor's
// device_timeout.
func TestTelnetCommandOwnBounds(t *testing.T) {
	cfg, err := configload.FromValues(map[string]any{"telnet.read-timeout": "300ms"})
	if err != nil {
		t.Fatal(err)
	}
	open := func() (*Driver, *device) {
		client, server := net.Pipe()
		dv := &device{}
		go dv.serve(server)
		return &Driver{f: Factory{Config: cfg, MaxOutputBytes: 1 << 20}, conn: client, prompt: []byte("dev#")}, dv
	}
	ctx := context.Background()
	d, _ := open()
	if r := d.Execute(ctx, platform.Command{Text: "show late", Timeout: time.Second, TimeoutSource: platform.DeclaredTimeout}); r.Err != nil || !strings.Contains(string(r.Output), "late") {
		t.Fatalf("declared timeout over the read cap: %+v", r)
	}
	d.Close()
	d, _ = open()
	if r := d.Execute(ctx, platform.Command{Text: "show late", Timeout: time.Second, TimeoutSource: platform.SessionTimeout}); r.ErrorCode != "command_timeout" || !strings.HasPrefix(r.Err.Error(), "command timed out after 300ms (telnet.read-timeout) while waiting for a returning prompt") {
		t.Fatalf("configured timeout under the read cap: %q %v", r.ErrorCode, r.Err)
	}
	d.Close()
	d, _ = open()
	if r := d.Execute(ctx, platform.Command{Text: "show slow", Timeout: 100 * time.Millisecond, TimeoutSource: platform.DeclaredTimeout}); r.ErrorCode != "command_timeout" || !strings.HasPrefix(r.Err.Error(), "command timed out after 100ms (--timeout)") {
		t.Fatalf("declared timeout: %q %v", r.ErrorCode, r.Err)
	}
	d.Close()
	d, _ = open()
	if r := d.Execute(ctx, platform.Command{Text: "show big", Timeout: time.Second, MaxBytes: 1024}); r.ErrorCode != "output_limit_exceeded" || len(r.Output) != 1024 || r.Err.Error() != "telnet output exceeded 1024 bytes (--maxbytes)" {
		t.Fatalf("declared limit: %q %d %v", r.ErrorCode, len(r.Output), r.Err)
	}
	if r := d.Execute(ctx, platform.Command{Text: "show big", Timeout: time.Second}); r.Err != nil || len(r.Output) < 3000 {
		t.Fatalf("the session's limit for the next command: %q %d %v", r.ErrorCode, len(r.Output), r.Err)
	}
	d.Close()
	d, _ = open()
	dctx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if r := d.Execute(dctx, platform.Command{Text: "show slow", Timeout: time.Second, TimeoutSource: platform.DeclaredTimeout}); r.ErrorCode != "device_timeout" || d.Usable() {
		t.Fatalf("device deadline first: %q %v usable=%v", r.ErrorCode, r.Err, d.Usable())
	}
	d.Close()
}

// shell is a telnet device that logs in at start ("dev>" or "dev#") and
// answers enable with a secret prompt or the privileged prompt.
func shell(conn net.Conn, start string, askSecret bool, dv *device) {
	_, _ = conn.Write([]byte(start))
	reader := bufio.NewReader(conn)
	secret := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		text := strings.TrimRight(line, "\r\n")
		dv.mu.Lock()
		dv.lines = append(dv.lines, text)
		dv.mu.Unlock()
		switch {
		case secret:
			secret = false
			_, _ = conn.Write([]byte("\r\ndev#"))
		case text == "enable" && askSecret:
			secret = true
			_, _ = conn.Write([]byte("enable\r\nPassword: "))
		default:
			_, _ = conn.Write([]byte(text + "\r\ndev#"))
		}
	}
}

// TestTelnetEscalationWithOptionalEnable:
// telnet escalates to the definition's privileged level whatever
// requires-enable says, sends nothing at privilege 15, and a secret prompt
// with no secret resolved is privilege_failed.
func TestTelnetEscalationWithOptionalEnable(t *testing.T) {
	iosxe, _ := platform.Builtin("cisco_ios")
	secret := func(f func([]byte) error) error { return f([]byte("en")) }
	for _, tc := range []struct {
		name, start string
		ask         bool
		enable      func(func([]byte) error) error
		code, saw   string
		// setup is SetupLines as prompt, statement, and answer: what the
		// job's output.TARGET.txt shows ahead of the first record. The secret
		// ("en") is sent and is in none of them.
		setup string
	}{
		{name: "privilege 15 at login", start: "dev#", saw: "terminal length 0|terminal width 512", setup: "dev#terminal length 0=|dev#terminal width 512="},
		{name: "enable without a secret", start: "dev>", saw: "enable|terminal length 0|terminal width 512", setup: "dev>enable=|dev#terminal length 0=|dev#terminal width 512="},
		{name: "a secret asked, none resolved", start: "dev>", ask: true, code: "privilege_failed", saw: "enable", setup: "dev>enable=Password:"},
		{name: "a secret asked and sent", start: "dev>", ask: true, enable: secret, saw: "enable|en|terminal length 0|terminal width 512", setup: "dev>enable=Password:|dev#terminal length 0=|dev#terminal width 512="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			dv := &device{}
			go shell(server, tc.start, tc.ask, dv)
			d := &Driver{f: Factory{Config: configload.Snapshot{}, MaxOutputBytes: 1 << 20}, req: platform.OpenRequest{Definition: iosxe, EnablePassword: tc.enable}, conn: client}
			defer d.Close()
			err := d.Prepare(context.Background())
			if code := errorCode(err); code != tc.code {
				t.Fatalf("prepare: %v, want %q", err, tc.code)
			}
			if tc.code == "privilege_failed" && !strings.Contains(err.Error(), "the credential has none") {
				t.Fatalf("message: %v", err)
			}
			if got := strings.Join(dv.await(len(strings.Split(tc.saw, "|"))), "|"); got != tc.saw {
				t.Fatalf("device saw %q, want %q", got, tc.saw)
			}
			var lines []string
			for _, l := range d.SetupLines() {
				lines = append(lines, l.PromptBefore+l.Statement+"="+l.Output)
			}
			if got := strings.Join(lines, "|"); got != tc.setup {
				t.Fatalf("set-up lines %q, want %q", got, tc.setup)
			}
		})
	}
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	return errorcodes.Of(err)
}

// PromptBefore is the prompt the statement was typed at and Prompt the one
// that came back, the device session's two meanings; until then telnet's
// Prompt held the one before.
func TestTelnetPromptBeforeAndAfter(t *testing.T) {
	client, server := net.Pipe()
	dv := &device{}
	go dv.serve(server)
	d := &Driver{f: Factory{Config: configload.Snapshot{}, MaxOutputBytes: 1 << 20}, conn: client, prompt: []byte("dev#")}
	defer d.Close()
	for _, tc := range []struct{ text, before, after string }{
		{"configure terminal", "dev#", "dev(config)#"},
		{"end", "dev(config)#", "dev#"},
		{"show clock", "dev#", "dev#"},
	} {
		if r := d.Execute(context.Background(), platform.Command{Text: tc.text, Timeout: time.Second}); r.Err != nil || r.PromptBefore != tc.before || r.Prompt != tc.after {
			t.Fatalf("%s: promptbefore=%q prompt=%q err=%v, want %q %q", tc.text, r.PromptBefore, r.Prompt, r.Err, tc.before, tc.after)
		}
	}
	if r := d.Execute(context.Background(), platform.Command{Text: "show slow", Timeout: 200 * time.Millisecond}); r.ErrorCode != "command_timeout" || r.PromptBefore != "dev#" || r.Prompt != "" {
		t.Fatalf("show slow: code=%q promptbefore=%q prompt=%q", r.ErrorCode, r.PromptBefore, r.Prompt)
	}
}

// The telnet shell's output and prompt are rendered as the terminal showed
// them, as the SSH shell's are: a colour dropped, a correction applied, a
// return overwriting, a space the device wrote on an inner line kept.
func TestOutputRendered(t *testing.T) {
	buf := []byte("show clokc\b\b\x1b[Kck\r\n\x1b[1m12:00\x1b[0m\r\nabc\rX\r\ndesc \r\nend\r\n\x1b]0;t\x07Router#")
	if got := string(cleanOutput(buf, "show clock", []byte("Router#"))); got != "12:00\nXbc\ndesc \nend" {
		t.Errorf("cleanOutput: %q", got)
	}
	if got := string(lastPrompt(buf)); got != "Router#" {
		t.Errorf("lastPrompt: %q", got)
	}
}

// telnetLive is a platform.Live that keeps the bytes and, for each secret,
// the bytes it came after.
type telnetLive struct {
	mu      sync.Mutex
	data    strings.Builder
	secrets []string
}

func (l *telnetLive) Write(p []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.data.Write(p)
}

func (l *telnetLive) Secret(v []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.secrets = append(l.secrets, string(v)+" after "+l.data.String())
}

// TestTelnetLiveSeesTheSession: the live view is given every byte read after
// the telnet negotiation, the enable step and a command's echo included,
// and the enable secret before it is written.
func TestTelnetLiveSeesTheSession(t *testing.T) {
	iosxe, _ := platform.Builtin("cisco_ios")
	client, server := net.Pipe()
	dv := &device{}
	go shell(server, "dev>", true, dv)
	live := &telnetLive{}
	d := &Driver{f: Factory{Config: configload.Snapshot{}, MaxOutputBytes: 1 << 20}, req: platform.OpenRequest{Definition: iosxe, EnablePassword: func(f func([]byte) error) error { return f([]byte("Zq7-en")) }, Live: live}, conn: client}
	defer d.Close()
	if err := d.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := d.Execute(context.Background(), platform.Command{Text: "show clock", Timeout: time.Second}); r.Err != nil {
		t.Fatal(r.Err)
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if got := live.data.String(); got != "dev>enable\r\nPassword: \r\ndev#terminal length 0\r\ndev#terminal width 512\r\ndev#show clock\r\ndev#" {
		t.Fatalf("view %q", got)
	}
	if len(live.secrets) != 1 || live.secrets[0] != "Zq7-en after dev>enable\r\nPassword: " {
		t.Fatalf("secrets %q", live.secrets)
	}
}

// queued is a device's end of the pipe whose writes go out in order from a
// goroutine of their own, each still a read of its own at the driver: the
// device reads on while the driver does not, so a driver that answers too
// soon fails its test instead of blocking on the pipe.
type queued struct {
	net.Conn
	out chan []byte
}

func queue(conn net.Conn) queued {
	q := queued{Conn: conn, out: make(chan []byte, 64)}
	go func() {
		for p := range q.out {
			if _, err := conn.Write(p); err != nil {
				return
			}
		}
	}()
	return q
}

func (q queued) Write(p []byte) (int, error) {
	q.out <- append([]byte(nil), p...)
	return len(p), nil
}

// login is a telnet device with a login exchange: each piece of its banner
// in a write of its own, then each of questions on a line of its own, each
// answer recorded after its question, then shell at start, asking for the
// enable secret.
func login(conn net.Conn, banner, questions []string, start string, dv *device) {
	for _, piece := range banner {
		if _, err := conn.Write([]byte(piece)); err != nil {
			return
		}
	}
	reader := bufio.NewReader(conn)
	for _, question := range questions {
		if _, err := conn.Write([]byte("\r\n" + question)); err != nil {
			return
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		dv.mu.Lock()
		dv.lines = append(dv.lines, question+strings.TrimRight(line, "\r\n"))
		dv.mu.Unlock()
	}
	shell(conn, start, true, dv)
}

// TestTelnetLoginAnswersNoBannerLine: a banner line ending in '>' or '#',
// arriving in a read of its own before Username: (with its newline, or cut
// before it), is not taken for the prompt: the username and the password go
// to the login's questions and the enable secret only to the enable step's.
func TestTelnetLoginAnswersNoBannerLine(t *testing.T) {
	iosxe, _ := platform.Builtin("cisco_ios")
	linux, _ := platform.Builtin("linux_shell")
	for _, tc := range []struct {
		name   string
		def    platform.Definition
		banner []string
		start  string
		saw    string
		prompt string
	}{
		{name: "a line ending in >", def: iosxe, banner: []string{"\r\nrouter1 is managed by the NOC\r\n", "Contact: <noc@example.com>\r\n"}, start: "\r\ndev>", saw: "Username: jsmith|Password: Zq7-pw|enable|Zq7-en|terminal length 0|terminal width 512", prompt: "dev#"},
		{name: "a line ending in #", def: iosxe, banner: []string{"\r\n##########\r\n"}, start: "\r\ndev>", saw: "Username: jsmith|Password: Zq7-pw|enable|Zq7-en|terminal length 0|terminal width 512", prompt: "dev#"},
		{name: "a line cut before its newline", def: iosxe, banner: []string{"\r\n##########"}, start: "\r\ndev>", saw: "Username: jsmith|Password: Zq7-pw|enable|Zq7-en|terminal length 0|terminal width 512", prompt: "dev#"},
		{name: "a broad prompt's line cut before its newline", def: linux, banner: []string{"\r\n# Authorized access only #"}, start: "\r\ndev$ ", saw: "Username: jsmith|Password: Zq7-pw", prompt: "dev$"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			dv := &device{}
			go login(queue(server), tc.banner, []string{"Username: ", "Password: "}, tc.start, dv)
			req := platform.OpenRequest{
				Definition:     tc.def,
				Username:       "jsmith",
				Password:       func(f func([]byte) error) error { return f([]byte("Zq7-pw")) },
				EnablePassword: func(f func([]byte) error) error { return f([]byte("Zq7-en")) },
			}
			d := &Driver{f: Factory{Config: configload.Snapshot{}, MaxOutputBytes: 1 << 20}, req: req, conn: client}
			defer d.Close()
			err := d.Prepare(context.Background())
			if got := strings.Join(dv.saw(), "|"); got != tc.saw {
				t.Fatalf("device saw %q, want %q (prepare: %v)", got, tc.saw, err)
			}
			if err != nil || string(d.prompt) != tc.prompt {
				t.Fatalf("prepare: %v, prompt %q, want %q", err, d.prompt, tc.prompt)
			}
		})
	}
}

// TestTelnetLoginWithPasswordAlone: a login whose first question is
// Password: (a line password) is answered with the login password; enable
// and its secret are sent at the privilege step, the secret only at the
// enable step's Password:.
func TestTelnetLoginWithPasswordAlone(t *testing.T) {
	iosxe, _ := platform.Builtin("cisco_ios")
	client, server := net.Pipe()
	dv := &device{}
	go login(queue(server), []string{"\r\nUser Access Verification\r\n"}, []string{"Password: "}, "\r\ndev>", dv)
	req := platform.OpenRequest{
		Definition:     iosxe,
		Username:       "jsmith",
		Password:       func(f func([]byte) error) error { return f([]byte("Zq7-pw")) },
		EnablePassword: func(f func([]byte) error) error { return f([]byte("Zq7-en")) },
	}
	d := &Driver{f: Factory{Config: configload.Snapshot{}, MaxOutputBytes: 1 << 20}, req: req, conn: client}
	defer d.Close()
	err := d.Prepare(context.Background())
	if got, want := strings.Join(dv.saw(), "|"), "Password: Zq7-pw|enable|Zq7-en|terminal length 0|terminal width 512"; got != want {
		t.Fatalf("device saw %q, want %q (prepare: %v)", got, want, err)
	}
	if err != nil || string(d.prompt) != "dev#" {
		t.Fatalf("prepare: %v, prompt %q", err, d.prompt)
	}
}

// TestTelnetPromptIsTheLastLineOnly: a read ends at the last line alone,
// when it is the device's prompt: a line of the output ending in '#' that a
// read is cut just after, or lines ending in '>' before the prompt in the
// same read, are output, kept whole in the record; the prompt is the last
// line, and the next command is in step. The platform's level patterns and
// the broad prompt of a platform without levels alike.
func TestTelnetPromptIsTheLastLineOnly(t *testing.T) {
	iosxe, _ := platform.Builtin("cisco_ios")
	for _, def := range []struct {
		name string
		def  platform.Definition
	}{{"levels", iosxe}, {"no levels", platform.Definition{}}} {
		for _, tc := range []struct{ name, text, output string }{
			{"a line ending in # cut by a read", "show running-config", "banner motd ^C\n##########\n# Authorized access only #\n##########\n^C\nend"},
			{"lines ending in > in the prompt's read", "show running-config | format", "<config>\n  <hostname>router1</hostname>\n</config>"},
		} {
			t.Run(def.name+", "+tc.name, func(t *testing.T) {
				client, server := net.Pipe()
				dv := &device{}
				go dv.serve(queue(server))
				d := &Driver{f: Factory{Config: configload.Snapshot{}, MaxOutputBytes: 1 << 20}, req: platform.OpenRequest{Definition: def.def}, conn: client, prompt: []byte("dev#")}
				defer d.Close()
				for _, c := range []struct{ text, output string }{{tc.text, tc.output}, {"show clock", "out"}} {
					r := d.Execute(context.Background(), platform.Command{Text: c.text, Timeout: 5 * time.Second})
					if r.Err != nil || string(r.Output) != c.output || r.PromptBefore != "dev#" || r.Prompt != "dev#" {
						t.Fatalf("%s: output %q promptbefore=%q prompt=%q err=%v, want %q", c.text, r.Output, r.PromptBefore, r.Prompt, r.Err, c.output)
					}
				}
			})
		}
	}
	if got := string(lastPrompt([]byte("show running-config | format\r\n<config>\r\n</config>\r\ndev#"))); got != "dev#" {
		t.Errorf("lastPrompt: %q", got)
	}
}

// readGate is a connection that says when each read begins.
type readGate struct {
	net.Conn
	reading chan struct{}
}

func (g readGate) Read(p []byte) (int, error) {
	select {
	case g.reading <- struct{}{}:
	default:
	}
	return g.Conn.Read(p)
}

// TestTelnetCancelEndsABlockedRead: a cancel (a job's cancel, a forced
// stop) ends a read blocked on a silent device at once, with the context's
// error, far inside the read's own timeout; a command so ended leaves the
// session unusable.
func TestTelnetCancelEndsABlockedRead(t *testing.T) {
	cfg, err := configload.FromValues(map[string]any{"execution.prompt-timeout": "1m"})
	if err != nil {
		t.Fatal(err)
	}
	iosxe, _ := platform.Builtin("cisco_ios")
	for _, tc := range []struct {
		name string
		// run reads from the device until it returns.
		run func(ctx context.Context, d *Driver) error
	}{
		{name: "a command", run: func(ctx context.Context, d *Driver) error {
			r := d.Execute(ctx, platform.Command{Text: "show slow", Timeout: time.Minute, TimeoutSource: platform.DeclaredTimeout})
			if d.Usable() {
				return errors.New("the session is still usable")
			}
			return r.Err
		}},
		{name: "the login", run: func(ctx context.Context, d *Driver) error { return d.Prepare(ctx) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			dv := &device{}
			go dv.serve(server)
			gate := readGate{Conn: client, reading: make(chan struct{}, 1)}
			d := &Driver{f: Factory{Config: cfg, MaxOutputBytes: 1 << 20}, req: platform.OpenRequest{Definition: iosxe}, conn: gate, prompt: []byte("dev#")}
			// The pipe itself is closed at the end, not the driver, whose
			// Close waits for a read that goes on.
			defer client.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- tc.run(ctx, d) }()
			<-gate.reading
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error %v, want the cancel", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the read went on after the cancel")
			}
		})
	}
}
