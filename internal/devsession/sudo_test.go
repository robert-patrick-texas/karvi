package devsession

import (
	"bufio"
	"context"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/platform"
)

// sudoConn is an in-memory exec connection whose root commands play sudo
// as the container showed it (EXAMPLES chapter 96, ex96-sudo-container and
// ex96d-stderr): classic sudo (sudo.ws 1.9.17p2) writes the prompt as
// given, with no line end, and one only when its read finds the input's
// end; sudo-rs (0.2.13, Ubuntu 26.04's) writes "[sudo: PROMPT] Password: "
// and a line end once its read ends. password is the one sudo takes, ""
// for a NOPASSWD rule. chunk > 0 writes stderr that many bytes a read, so
// a prompt reaches the session split.
type sudoConn struct {
	rs       bool
	password string
	chunk    int

	mu      sync.Mutex
	started []sudoStart
}

type sudoStart struct {
	wire  string
	input bool
	read  string // what the fake's sudo read on its input
}

var sudoWirePattern = regexp.MustCompile(`^sudo -S -p '(karvi-sudo-[0-9a-f]{16}:)' -- (.*)$`)

func (c *sudoConn) Start(command string, input bool) (ExecChannel, error) {
	ch := &sudoChannel{done: make(chan struct{})}
	var outW, errW *io.PipeWriter
	ch.stdout, outW = io.Pipe()
	ch.stderr, errW = io.Pipe()
	var inR *io.PipeReader
	if input {
		inR, ch.input = io.Pipe()
	}
	c.mu.Lock()
	c.started = append(c.started, sudoStart{wire: command, input: input})
	at := len(c.started) - 1
	c.mu.Unlock()
	ch.close = func() {
		outW.Close()
		errW.Close()
		if inR != nil {
			inR.Close()
		}
	}
	go func() {
		defer close(ch.done)
		defer ch.close()
		ch.status = c.play(command, inR, outW, errW, at)
	}()
	return ch, nil
}

func (c *sudoConn) Close() error { return nil }

// write puts s on stderr, in pieces of c.chunk bytes when set.
func (c *sudoConn) write(w io.Writer, s string) {
	for s != "" {
		n := len(s)
		if c.chunk > 0 && c.chunk < n {
			n = c.chunk
		}
		_, _ = io.WriteString(w, s[:n])
		s = s[n:]
	}
}

// play is one command: sudo's part, then the command's own.
func (c *sudoConn) play(wire string, in io.Reader, out, errOut io.Writer, at int) int {
	text, ok := strings.CutPrefix(wire, "sudo -n -- ")
	if ok && c.password != "" {
		if c.rs {
			c.write(errOut, "sudo: interactive authentication is required\n")
		} else {
			c.write(errOut, "sudo: a password is required\n")
		}
		return 1
	}
	if !ok {
		m := sudoWirePattern.FindStringSubmatch(wire)
		if m == nil {
			c.write(errOut, "sh: 1: not a sudo line\n")
			return 127
		}
		prompt, lines := m[1], bufio.NewReader(in)
		text = m[2]
		for attempt := 1; c.password != ""; attempt++ {
			if c.rs {
				c.write(errOut, "[sudo: "+prompt+"] Password: ")
			} else {
				c.write(errOut, prompt)
			}
			line, err := lines.ReadString('\n')
			c.mu.Lock()
			c.started[at].read += line
			c.mu.Unlock()
			if c.rs {
				c.write(errOut, "\n")
			}
			if err != nil {
				if c.rs {
					c.write(errOut, "sudo: Authentication required but not attempted\n")
				} else {
					c.write(errOut, "\nsudo: no password was provided\nsudo: 1 incorrect password attempt\n")
				}
				return 1
			}
			if strings.TrimSuffix(line, "\n") == c.password {
				break
			}
			if c.rs {
				c.write(errOut, "sudo: Authentication failed, try again.\n")
			} else {
				c.write(errOut, "Sorry, try again.\n")
			}
		}
	}
	switch text {
	case "whoami":
		_, _ = io.WriteString(out, "root\n")
	case "warn":
		// The command's own stderr, whole: the chunks are sudo's.
		_, _ = io.WriteString(errOut, "\nerr-line\n")
		_, _ = io.WriteString(out, "out-line\n")
	case "head -c1":
		// Reads its input: ends at the input's end.
		if in != nil {
			_, _ = io.ReadAll(in)
		}
	case "tail":
		// Its stderr ends as a prompt in sudo-rs's form would begin, and it
		// reads its input.
		_, _ = io.WriteString(errOut, "partial [sudo: karvi-su")
		if in != nil {
			_, _ = io.ReadAll(in)
		}
	}
	return 0
}

type sudoChannel struct {
	stdout, stderr io.Reader
	input          io.WriteCloser
	close          func()
	done           chan struct{}
	status         int
}

func (x *sudoChannel) Stdout() io.Reader { return x.stdout }
func (x *sudoChannel) Stderr() io.Reader { return x.stderr }
func (x *sudoChannel) Input() io.WriteCloser {
	if x.input == nil {
		return nil
	}
	return x.input
}
func (x *sudoChannel) Wait() (*int, string, error) {
	<-x.done
	status := x.status
	return &status, "", nil
}
func (x *sudoChannel) Stop() []platform.Notice {
	x.close()
	return nil
}

// secretOf delivers a password as the request's EnablePassword does.
func secretOf(password string) func(func([]byte) error) error {
	return func(fn func([]byte) error) error { return fn([]byte(password)) }
}

// sudoLog keeps what Live and Debug were given.
type sudoLog struct {
	mu   sync.Mutex
	text strings.Builder
}

func (l *sudoLog) Write(b []byte) {
	l.mu.Lock()
	l.text.Write(b)
	l.mu.Unlock()
}
func (l *sudoLog) Secret([]byte) {}
func (l *sudoLog) line(s string) { l.Write([]byte(s + "\n")) }
func (l *sudoLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}

// TestSudoCommandOnBothSudos: a root command under classic sudo and
// sudo-rs, the prompt whole or split across reads: the password written
// once, at the prompt, the prompt and its line end out of stderr, Live,
// and Debug, and the password nowhere but the command's input; a wrong
// password is privilege_failed, outranking sudo's exit status, with sudo's
// own messages kept.
func TestSudoCommandOnBothSudos(t *testing.T) {
	for _, form := range []struct {
		name    string
		rs      bool
		refused string // stderr after a wrong password
	}{
		{"classic", false, "Sorry, try again.\nsudo: no password was provided\nsudo: 1 incorrect password attempt\n"},
		{"sudo-rs", true, "sudo: Authentication failed, try again.\nsudo: Authentication required but not attempted\n"},
	} {
		for _, chunk := range []int{0, 1, 3, 7} {
			conn := &sudoConn{rs: form.rs, password: "pw1", chunk: chunk}
			live, debug := &sudoLog{}, &sudoLog{}
			s := OpenExec(conn, ExecOptions{Definition: platform.Definition{Name: "linux"}, SudoPassword: secretOf("pw1"), Live: live, Debug: debug.line})
			r := s.Execute(context.Background(), platform.Command{Text: "whoami", Timeout: 5 * time.Second, Sudo: true})
			if r.ErrorCode != "" || string(r.Output) != "root\n" || string(r.Exec.Stderr) != "" || r.Exec.SudoPrompts != 1 {
				t.Errorf("%s chunk %d: code %q output %q stderr %q prompts %d", form.name, chunk, r.ErrorCode, r.Output, r.Exec.Stderr, r.Exec.SudoPrompts)
			}
			// The command's own stderr after the prompt: its blank first
			// line is kept under both sudos.
			r = s.Execute(context.Background(), platform.Command{Text: "warn", Timeout: 5 * time.Second, Sudo: true})
			if r.ErrorCode != "" || string(r.Output) != "out-line\n" || string(r.Exec.Stderr) != "\nerr-line\n" {
				t.Errorf("%s chunk %d warn: code %q output %q stderr %q", form.name, chunk, r.ErrorCode, r.Output, r.Exec.Stderr)
			}
			conn.password = "other"
			r = s.Execute(context.Background(), platform.Command{Text: "whoami", Timeout: 5 * time.Second, Sudo: true})
			if r.ErrorCode != "privilege_failed" || string(r.Exec.Stderr) != form.refused || r.Exec.SudoPrompts != 2 || r.Exec.ExitStatus == nil || *r.Exec.ExitStatus != 1 || !r.DeviceError || r.ErrorCategory != "device" {
				t.Errorf("%s chunk %d refused: code %q stderr %q prompts %d exec %+v", form.name, chunk, r.ErrorCode, r.Exec.Stderr, r.Exec.SudoPrompts, r.Exec)
			}
			if !s.Usable() {
				t.Errorf("%s chunk %d: a refused password left the session unusable", form.name, chunk)
			}
			for i, st := range conn.started {
				if !st.input || !sudoWirePattern.MatchString(st.wire) {
					t.Errorf("%s chunk %d command %d: wire %q input %t", form.name, chunk, i+1, st.wire, st.input)
				}
				if want := "pw1\n"; st.read != want {
					t.Errorf("%s chunk %d command %d: sudo read %q, want %q once", form.name, chunk, i+1, st.read, want)
				}
			}
			if a, b := sudoWirePattern.FindStringSubmatch(conn.started[0].wire)[1], sudoWirePattern.FindStringSubmatch(conn.started[1].wire)[1]; a == b {
				t.Errorf("%s: two commands shared the prompt %q", form.name, a)
			}
			for _, text := range []string{live.String(), debug.String()} {
				if strings.Contains(text, "pw1") || strings.Contains(text, "karvi-sudo-") || strings.Contains(text, "Password:") {
					t.Errorf("%s chunk %d: the password or the prompt reached Live or Debug: %q", form.name, chunk, text)
				}
			}
			// Live has both streams interleaved as they came.
			if !strings.Contains(live.String(), "root\n") || !strings.Contains(live.String(), "err-line") || !strings.Contains(live.String(), "out-line\n") {
				t.Errorf("%s chunk %d: Live %q", form.name, chunk, live.String())
			}
		}
	}
}

// TestSudoCommandNoPasswordAsked: a NOPASSWD rule under a held password:
// no prompt, nothing written, the command runs; but a command that reads
// its input waits, the input held, to its command timeout (the caution of
// EXAMPLES chapter 96, issue 4), and what the filter held of its stderr
// then is handed over with the rest.
func TestSudoCommandNoPasswordAsked(t *testing.T) {
	for _, rs := range []bool{false, true} {
		conn := &sudoConn{rs: rs}
		s := OpenExec(conn, ExecOptions{Definition: platform.Definition{Name: "linux"}, SudoPassword: secretOf("pw1")})
		r := s.Execute(context.Background(), platform.Command{Text: "whoami", Timeout: 5 * time.Second, Sudo: true})
		if r.ErrorCode != "" || string(r.Output) != "root\n" || r.Exec.SudoPrompts != 0 || conn.started[0].read != "" {
			t.Errorf("rs %t: code %q output %q prompts %d read %q", rs, r.ErrorCode, r.Output, r.Exec.SudoPrompts, conn.started[0].read)
		}
		r = s.Execute(context.Background(), platform.Command{Text: "head -c1", Timeout: 300 * time.Millisecond, Sudo: true})
		if r.ErrorCode != "command_timeout" {
			t.Errorf("rs %t: a command reading its input under a held password: %q", rs, r.ErrorCode)
		}
		// Given up while the filter holds a tail that may begin a prompt:
		// the tail is the command's, in the record.
		r = s.Execute(context.Background(), platform.Command{Text: "tail", Timeout: 300 * time.Millisecond, Sudo: true})
		if r.ErrorCode != "command_timeout" || string(r.Exec.Stderr) != "partial [sudo: karvi-su" {
			t.Errorf("rs %t: a held tail given up: %q stderr %q", rs, r.ErrorCode, r.Exec.Stderr)
		}
	}
}

// TestSudoCommandWithoutPassword: no password sends sudo -n, the input at
// its end: a NOPASSWD rule runs the command, a rule that wants a password
// is sudo's own refusal, command_exit_nonzero.
func TestSudoCommandWithoutPassword(t *testing.T) {
	for _, rs := range []bool{false, true} {
		conn := &sudoConn{rs: rs}
		s := OpenExec(conn, ExecOptions{Definition: platform.Definition{Name: "linux"}})
		r := s.Execute(context.Background(), platform.Command{Text: "whoami", Timeout: 5 * time.Second, Sudo: true})
		if r.ErrorCode != "" || string(r.Output) != "root\n" || conn.started[0].wire != "sudo -n -- whoami" || conn.started[0].input {
			t.Errorf("rs %t: code %q output %q started %+v", rs, r.ErrorCode, r.Output, conn.started[0])
		}
		conn.password = "pw1"
		r = s.Execute(context.Background(), platform.Command{Text: "whoami", Timeout: 5 * time.Second, Sudo: true})
		if r.ErrorCode != "command_exit_nonzero" || !strings.Contains(string(r.Exec.Stderr), "sudo: ") || r.Exec.SudoPrompts != 0 {
			t.Errorf("rs %t: a password wanted: code %q stderr %q", rs, r.ErrorCode, r.Exec.Stderr)
		}
	}
	// A command that is not a root command is sent as written.
	conn := &sudoConn{}
	s := OpenExec(conn, ExecOptions{Definition: platform.Definition{Name: "linux"}, SudoPassword: secretOf("pw1")})
	_ = s.Execute(context.Background(), platform.Command{Text: "whoami", Timeout: 5 * time.Second})
	if conn.started[0].wire != "whoami" || conn.started[0].input {
		t.Errorf("a plain command: %+v", conn.started[0])
	}
}

// TestSudoFilterEverySplit: each form's stream, split at every byte, reads
// the same through the filter: the prompts and their line ends removed by
// the rule, the rest kept in order, and the prompts counted.
func TestSudoFilterEverySplit(t *testing.T) {
	const p = "karvi-sudo-0123456789abcdef:"
	for _, tc := range []struct {
		name, stream, want string
		prompts            int
	}{
		{"classic answered", p + "\nerr-line\n", "\nerr-line\n", 1},
		{"classic refused", p + "Sorry, try again.\n" + p + "\nsudo: no password was provided\n", "Sorry, try again.\nsudo: no password was provided\n", 2},
		{"sudo-rs answered", "[sudo: " + p + "] Password: \n\nerr-line\n", "\nerr-line\n", 1},
		{"sudo-rs refused", "[sudo: " + p + "] Password: \nsudo: Authentication failed, try again.\n[sudo: " + p + "] Password: \nsudo: Authentication required but not attempted\n", "sudo: Authentication failed, try again.\nsudo: Authentication required but not attempted\n", 2},
		{"no prompt", "[sudo: karvi-sudo-x] and karvi-sudo-01\n", "[sudo: karvi-sudo-x] and karvi-sudo-01\n", 0},
		{"a tail like a prompt's start", "warning: [sudo: karvi-su", "warning: [sudo: karvi-su", 0},
	} {
		for cut := 0; cut <= len(tc.stream); cut++ {
			for _, second := range []int{0, 1, 5} {
				f := newSudoFilter(p)
				var got strings.Builder
				n := 0
				rest := tc.stream
				for _, size := range []int{cut, second} {
					if size > len(rest) || size == 0 {
						continue
					}
					out, k := f.write([]byte(rest[:size]))
					got.Write(out)
					n += k
					rest = rest[size:]
				}
				out, k := f.write([]byte(rest))
				got.Write(out)
				got.Write(f.end())
				n += k
				if got.String() != tc.want || n != tc.prompts {
					t.Fatalf("%s cut %d+%d: %q, %d prompts; want %q, %d", tc.name, cut, second, got.String(), n, tc.want, tc.prompts)
				}
			}
		}
	}
}
