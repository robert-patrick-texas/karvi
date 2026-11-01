// Package telnet implements an explicitly enabled, line-oriented Telnet driver.
// It never acts as an SSH fallback.
package telnet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/termtext"
	"github.com/robert-patrick-texas/karvi/platform"
)

// The login's questions, matched against the last line alone (readUntil).
var (
	usernameRE = regexp.MustCompile(`(?i)(username|login)\s*:\s*\z`)
	passwordRE = regexp.MustCompile(`(?i)password\s*:\s*\z`)
)

// lineMax bounds the last line a prompt or a question is looked for in: a
// longer line is output (the prompt patterns allow 240 characters at most),
// and no read scans more than it and the read's own bytes.
const lineMax = 4096

type Factory struct {
	Config         configload.Snapshot
	MaxOutputBytes int64
}
type Driver struct {
	f      Factory
	req    platform.OpenRequest
	conn   net.Conn
	mu     sync.Mutex
	prompt []byte
	// prompts is the definition's prompt shapes as the device session
	// compiles them, at the first read.
	prompts *devsession.Prompts
	// broken is set by a failure that ends the session: a later Execute
	// writes nothing.
	broken bool
	// setup is what Prepare sent after the login: the escalate command and
	// the paging commands, each with the prompt it was typed at (the last
	// one read from the device) and the answer (platform.SetupReporter).
	// The login itself is not a statement at a prompt and is not here; nor
	// is the enable secret, or anything read between it and the next prompt.
	setup []platform.SetupLine
}

// The executor finds the set-up lines by a type assertion, which a renamed
// method would fail silently; this fails the build instead.
var _ platform.SetupReporter = (*Driver)(nil)

// SetupLines is the set-up as it was sent, for the job's output.TARGET.txt.
func (d *Driver) SetupLines() []platform.SetupLine {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.setup
}

// noteSetup keeps one set-up statement that was written: buf is what was
// read after it, up to the next prompt (returned, removed from the answer)
// or the secret prompt (whose `Password:` is the answer), or up to the
// failure (returned nil).
func (d *Driver) noteSetup(before []byte, statement string, buf, returned []byte) {
	output := strings.TrimRight(string(cleanOutput(buf, statement, returned)), " \t")
	d.setup = append(d.setup, platform.SetupLine{PromptBefore: strings.TrimSpace(string(before)), Statement: statement, Output: output})
}

func (f Factory) Open(ctx context.Context, req platform.OpenRequest) (platform.Driver, error) {
	if !f.Config.Bool("security.allow-telnet") {
		return nil, fmt.Errorf("telnet_not_allowed: security.allow-telnet is false")
	}
	timeout := f.Config.Duration("telnet.connect-timeout")
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(req.Address, fmt.Sprint(req.Port)))
	if err != nil {
		return nil, fmt.Errorf("telnet_connection_failed: %w", err)
	}
	if f.MaxOutputBytes <= 0 {
		f.MaxOutputBytes = f.Config.Int64("output.max-command-bytes")
	}
	return &Driver{f: f, req: req, conn: conn}, nil
}
func (d *Driver) Prepare(ctx context.Context) error {
	if d.req.Channel == platform.ChannelExec {
		// The planner and the daemon's plan check refuse it first.
		return errorcodes.Errorf("channel_exec_over_telnet", "telnet has no exec channel")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	timeout := d.f.Config.Duration("execution.prompt-timeout")
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	// The login read ends at the username question, at the password
	// question (a line password's login), or at the device's prompt (no
	// login exchange), its kinds in that order.
	const (
		atUsername = iota
		atPassword
	)
	buf, kind, err := d.readUntil(ctx, timeout, d.f.MaxOutputBytes, usernameRE, passwordRE)
	if err != nil {
		return fmt.Errorf("telnet_login_prompt: %w", err)
	}
	if kind == atUsername {
		if err := d.writeLine(d.req.Username); err != nil {
			return errorcodes.Errorf("telnet_write_failed", "send username: %w", err)
		}
		buf, kind, err = d.readUntil(ctx, timeout, d.f.MaxOutputBytes, passwordRE)
		if err != nil {
			return errorcodes.Errorf("telnet_login_prompt", "wait for password prompt: %w", err)
		}
		// This read's password question and prompt, as the login read's.
		kind += atPassword
	}
	if kind == atPassword {
		var pass []byte
		if d.req.Password == nil {
			return errorcodes.Errorf("credential_password_missing", "telnet password is unavailable")
		}
		if err := d.req.Password(func(v []byte) error { pass = append([]byte(nil), v...); return nil }); err != nil {
			return errorcodes.Ensure(err, "secret_password_unset")
		}
		defer wipe(pass)
		if err := d.writeSecretLine(pass); err != nil {
			return errorcodes.Errorf("telnet_write_failed", "send password: %w", err)
		}
		buf, _, err = d.readUntil(ctx, timeout, d.f.MaxOutputBytes)
		if err != nil {
			return errorcodes.Errorf("authentication_failed", "telnet authentication failed: %w", err)
		}
	}
	// Every path here ended its last read at the device's prompt.
	d.prompt = lastPrompt(buf)
	// Escalate to the definition's privileged level when the device is not
	// already there (privilege 15 at login needs nothing). The enable secret
	// is optional: a device that asks for one when none was resolved is
	// privilege_failed.
	if level, ok := d.req.Definition.Level(d.req.Definition.PrivilegedLevel); ok && level.Escalate != "" && !atLevel(level, d.prompt) {
		if err := d.writeLine(level.Escalate); err != nil {
			return errorcodes.Errorf("privilege_failed", "send %q: %w", level.Escalate, err)
		}
		// execution.enable-timeout bounds the whole step, from the escalate
		// command to the level's prompt.
		enableTimeout := d.f.Config.Duration("execution.enable-timeout")
		if enableTimeout <= 0 {
			enableTimeout = 10 * time.Second
		}
		enableDeadline := time.Now().Add(enableTimeout)
		buf, kind, err = d.readUntil(ctx, enableTimeout, d.f.MaxOutputBytes, passwordRE)
		var returned []byte
		if err == nil && kind == 1 {
			returned = lastPrompt(buf)
		}
		d.noteSetup(d.prompt, level.Escalate, buf, returned)
		if err != nil {
			return errorcodes.Errorf("privilege_failed", "wait for the prompt after %q: %w", level.Escalate, err)
		}
		if kind == 1 {
			d.prompt = returned
		}
		if kind == 0 {
			if d.req.EnablePassword == nil {
				return errorcodes.Errorf("privilege_failed", "the device asked for an enable secret after %q and the credential has none", level.Escalate)
			}
			var enable []byte
			if err := d.req.EnablePassword(func(v []byte) error { enable = append([]byte(nil), v...); return nil }); err != nil {
				return errorcodes.Ensure(err, "secret_enable_password_unset")
			}
			defer wipe(enable)
			if err := d.writeSecretLine(enable); err != nil {
				return errorcodes.Errorf("telnet_write_failed", "send enable password: %w", err)
			}
			buf, _, err = d.readUntil(ctx, max(time.Until(enableDeadline), time.Millisecond), d.f.MaxOutputBytes)
			if err != nil {
				return errorcodes.Errorf("privilege_failed", "wait for privileged prompt: %w", err)
			}
			d.prompt = lastPrompt(buf)
		}
	}
	for _, cmd := range d.req.Definition.PagingCommands {
		if cmd == "" {
			continue
		}
		if err := d.writeLine(cmd); err != nil {
			return errorcodes.Errorf("telnet_write_failed", "send paging command: %w", err)
		}
		buf, _, err = d.readUntil(ctx, timeout, d.f.MaxOutputBytes)
		var returned []byte
		if err == nil {
			returned = lastPrompt(buf)
		}
		d.noteSetup(d.prompt, cmd, buf, returned)
		if err != nil {
			return errorcodes.Errorf("telnet_paging_command_failed", "telnet paging command: %w", err)
		}
		d.prompt = returned
	}
	return nil
}
func (d *Driver) Execute(ctx context.Context, c platform.Command) platform.Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	start := time.Now()
	// The transport read timeout is distinct from the general command budget.
	// Use the shorter positive value so a silent Telnet peer cannot hold a read
	// beyond either operator-configured limit; a command's declared timeout
	// replaces both, the operator having bounded that command alone.
	timeout, source := d.f.Config.Duration("telnet.read-timeout"), "telnet.read-timeout"
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	switch {
	case c.TimeoutSource == platform.DeclaredTimeout && c.Timeout > 0:
		timeout, source = c.Timeout, c.TimeoutSource
	case c.Timeout > 0 && c.Timeout < timeout:
		timeout, source = c.Timeout, c.TimeoutSource
	}
	limit, limitSource := c.Limit(d.f.MaxOutputBytes)
	if d.conn == nil || d.broken {
		return platform.Result{StartedAt: start, EndedAt: time.Now(), ErrorCode: "command_session_lost", ErrorCategory: "connection", External: true, Err: errors.New("the telnet session ended before the next command; no replacement login was attempted")}
	}
	if err := d.writeLine(c.Text); err != nil {
		d.broken = true
		return platform.Result{StartedAt: start, EndedAt: time.Now(), ErrorCode: "telnet_write_failed", ErrorCategory: "connection", External: true, Retryable: true, Err: err}
	}
	// before is the prompt the statement was typed at, the last one read
	// from the device; after is the one that came back, empty when none did
	// (the same two meanings as the device session's result).
	before := strings.TrimSpace(string(d.prompt))
	buf, _, err := d.readUntil(ctx, timeout, limit)
	observed := err == nil
	var returned []byte
	if observed {
		returned = lastPrompt(buf)
	}
	after := string(returned)
	if err != nil {
		// A timeout, a failed read, or output over the limit before the
		// prompt leaves the shell out of step with the next command.
		d.broken = true
	}
	result := platform.Result{StartedAt: start, EndedAt: time.Now(), PromptBefore: before, Prompt: after, PromptSource: "observed", PromptObserved: &observed, Output: cleanOutput(buf, c.Text, returned)}
	if int64(len(result.Output)) > limit && limit > 0 {
		result.Output = result.Output[:limit]
		result.ErrorCode = "output_limit_exceeded"
		result.ErrorCategory = "output"
		result.Err = fmt.Errorf("telnet output exceeded %d bytes%s", limit, platform.Named(limitSource))
		return result
	}
	if err != nil && errorcodes.Of(err) == "output_limit_exceeded" {
		result.ErrorCode = "output_limit_exceeded"
		result.ErrorCategory = "output"
		result.External = true
		result.Err = fmt.Errorf("telnet output exceeded %d bytes%s before the prompt returned; the session is closed", limit, platform.Named(limitSource))
		return result
	}
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			result.ErrorCode = "command_timeout"
			result.ErrorCategory = "timeout"
			result.Retryable = true
			err = fmt.Errorf("command timed out after %s%s while waiting for a returning prompt; the session is closed: %w", timeout, platform.Named(source), err)
			if dl, ok := ctx.Deadline(); ok && dl.Before(start.Add(timeout)) {
				// The read deadline was the device's (readUntil takes the
				// earlier): the executor names it device_timeout.
				result.ErrorCode, err = "device_timeout", context.DeadlineExceeded
			}
		} else {
			result.ErrorCode = "telnet_read_failed"
			result.ErrorCategory = "connection"
			result.Retryable = true
		}
		result.External = true
		result.Err = err
		return result
	}
	if deviceError(result.Output) {
		result.DeviceError = true
		result.ErrorCode = "device_command_error"
		result.ErrorCategory = "device"
		result.External = true
		result.Err = fmt.Errorf("device reported a command error")
	}
	d.prompt = returned
	return result
}

// Usable reports whether the session can take another command.
func (d *Driver) Usable() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.conn != nil && !d.broken
}

func (d *Driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn == nil {
		return nil
	}
	for _, cmd := range d.req.Definition.ExitCommands {
		_ = d.writeLine(cmd)
	}
	err := d.conn.Close()
	d.conn = nil
	return err
}
func (d *Driver) writeLine(s string) error { _, err := io.WriteString(d.conn, s+"\r\n"); return err }

// writeSecretLine is writeLine for a secret, the live view given it first.
func (d *Driver) writeSecretLine(secret []byte) error {
	if d.req.Live != nil {
		d.req.Live.Secret(secret)
	}
	return d.writeLine(string(secret))
}

// readUntil reads until the last line, the bytes after the last newline as
// the terminal showed them, is one of questions (the kind its index) or a
// prompt of the definition's as the device session matches it (the kind
// len(questions)), and has stood unchanged for the session's settle
// interval, devsession.PromptSettleDelay: a banner or output line ending in
// '#' or '>' that a read was cut just after is not taken for the prompt. A
// stream that ends at such a line ends the read at it too. A line a newline
// has ended is never matched, and each read scans its own bytes and the
// last line alone. The context's end, a cancel or a forced stop as well as
// its deadline, ends a blocked read at once and is the read's error.
func (d *Driver) readUntil(ctx context.Context, timeout time.Duration, limit int64, questions ...*regexp.Regexp) ([]byte, int, error) {
	if d.prompts == nil {
		prompts, err := devsession.CompilePrompts(d.req.Definition)
		if err != nil {
			return nil, -1, err
		}
		d.prompts = prompts
	}
	deadline := time.Now().Add(timeout)
	// A context deadline (execution.device-timeout) ends a blocked read too.
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	// The context's end sets a read deadline in the past; a read that ends
	// waits for it to be set, so that it never reaches a later read.
	conn, woke := d.conn, make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = conn.SetReadDeadline(time.Now())
		close(woke)
	})
	defer func() {
		if !stop() {
			<-woke
		}
	}()
	var out bytes.Buffer
	tmp := make([]byte, 4096)
	state := byte(0)
	cmd := byte(0)
	// line is where the last line begins in out, kind what it is (-1 for
	// neither), and settled when it will have stood for the interval.
	line, kind := 0, -1
	var settled time.Time
	for {
		readDeadline, settling := deadline, false
		if kind >= 0 && !settled.After(deadline) {
			readDeadline, settling = settled, true
		}
		_ = d.conn.SetReadDeadline(readDeadline)
		// The context is checked after the deadline is set, since setting it
		// undoes a past one the context's end set just before.
		if ctx.Err() != nil {
			return out.Bytes(), -1, ctx.Err()
		}
		n, err := d.conn.Read(tmp)
		if n > 0 {
			clean, responses, newState, newCmd := filterTelnet(tmp[:n], state, cmd)
			state, cmd = newState, newCmd
			if len(responses) > 0 {
				_, _ = d.conn.Write(responses)
			}
			if i := bytes.LastIndexByte(clean, '\n'); i >= 0 {
				line = out.Len() + i + 1
			}
			out.Write(clean)
			if d.req.Live != nil && len(clean) > 0 {
				d.req.Live.Write(clean)
			}
			if len(clean) > 0 {
				kind = lineKind(out.Bytes()[line:], questions, d.prompts)
				settled = time.Now().Add(devsession.PromptSettleDelay)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return out.Bytes(), -1, ctx.Err()
			}
			var ne net.Error
			timedOut := errors.As(err, &ne) && ne.Timeout()
			if kind >= 0 && (settling || !timedOut) {
				return out.Bytes(), kind, nil
			}
			return out.Bytes(), -1, err
		}
		if limit > 0 && int64(out.Len()) > limit+4096 {
			return out.Bytes(), -1, errorcodes.Errorf("output_limit_exceeded", "output limit exceeded")
		}
	}
}

// lineKind is what the last line, raw, is: the index of the first of
// questions it matches, len(questions) for a prompt, -1 for neither and for
// a line longer than lineMax.
func lineKind(raw []byte, questions []*regexp.Regexp, prompts *devsession.Prompts) int {
	if len(raw) > lineMax {
		return -1
	}
	text := lastLine(raw)
	if text == "" {
		return -1
	}
	for i, q := range questions {
		if q.MatchString(text) {
			return i
		}
	}
	if _, _, ok := prompts.Match(text); ok {
		return len(questions)
	}
	return -1
}

func filterTelnet(in []byte, state, verb byte) ([]byte, []byte, byte, byte) {
	const iac = 255
	var out, resp []byte
	for _, b := range in {
		switch state {
		case 0:
			if b == iac {
				state = 1
			} else {
				out = append(out, b)
			}
		case 1:
			switch b {
			case iac:
				out = append(out, b)
				state = 0
			case 251, 252, 253, 254:
				verb = b
				state = 2
			case 250:
				state = 3
			default:
				state = 0
			}
		case 2:
			reply := byte(254)
			if verb == 253 || verb == 254 {
				reply = 252
			}
			resp = append(resp, iac, reply, b)
			state = 0
		case 3:
			if b == iac {
				state = 4
			}
		case 4:
			if b == 240 {
				state = 0
			} else {
				state = 3
			}
		}
	}
	return out, resp, state, verb
}

// rendered is buf as the terminal showed it (termtext): no width, the spaces
// the device wrote kept, as the session layer renders the SSH shell's
// output and prompts.
func rendered(buf []byte) []byte {
	var out bytes.Buffer
	r := termtext.New(&out, 0)
	r.KeepSpaces = true
	r.Write(buf)
	r.Close()
	return out.Bytes()
}

// lastLine is buf's last line, the bytes after its last newline, as the
// terminal showed it (as rendered renders it) without its trailing blanks:
// the line a prompt or a login question is.
func lastLine(buf []byte) string {
	r := termtext.New(io.Discard, 0)
	r.KeepSpaces = true
	_, _ = r.Write(buf[bytes.LastIndexByte(buf, '\n')+1:])
	return strings.TrimRight(r.Pending(), " \t")
}

// lastPrompt is the prompt a read ended at: its last line, trimmed.
func lastPrompt(buf []byte) []byte {
	return []byte(strings.TrimSpace(lastLine(buf)))
}

// cleanOutput is buf as the terminal showed it without the echoed command
// line, the prompt the read ended at (returned, its last line; nil when it
// ended at none), and the blank lines at its end.
func cleanOutput(buf []byte, command string, returned []byte) []byte {
	lines := strings.Split(string(rendered(buf)), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == strings.TrimSpace(command) {
		lines = lines[1:]
	}
	trimBlank := func() {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
	}
	trimBlank()
	if len(returned) > 0 && len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == string(returned) {
		lines = lines[:len(lines)-1]
		trimBlank()
	}
	return []byte(strings.Join(lines, "\n"))
}
func deviceError(out []byte) bool {
	s := strings.ToLower(string(out))
	for _, p := range []string{"% invalid input", "% incomplete command", "syntax error", "unknown command"} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// atLevel reports whether prompt is the level's prompt.
func atLevel(level platform.PrivilegeLevel, prompt []byte) bool {
	re, err := regexp.Compile(level.Pattern)
	return err == nil && len(prompt) > 0 && re.Match(bytes.TrimSpace(prompt))
}
