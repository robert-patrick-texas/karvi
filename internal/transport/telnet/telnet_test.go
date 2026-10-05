package telnet

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
)

// device answers each line on its end of the pipe: show slow never, a
// device error for show bogus, a prompt otherwise; it records every line.
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
		case "configure terminal":
			_, _ = conn.Write([]byte(text + "\r\ndev(config)#"))
		case "end":
			_, _ = conn.Write([]byte(text + "\r\ndev#"))
		case "show bogus":
			_, _ = conn.Write([]byte(text + "\r\n% Invalid input detected at '^' marker.\r\ndev#"))
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
	time.Sleep(50 * time.Millisecond)
	if got := strings.Join(dv.saw(), "|"); got != "show clock|show bogus|show slow" {
		t.Fatalf("device saw %q", got)
	}
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
	iosxe, _ := platform.Builtin("cisco_iosxe")
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
			time.Sleep(50 * time.Millisecond)
			if got := strings.Join(dv.saw(), "|"); got != tc.saw {
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
