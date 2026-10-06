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
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/termtext"
	"github.com/robert-patrick-texas/karvi/platform"
)

var (
	usernameRE = regexp.MustCompile(`(?im)(username|login)\s*:\s*$`)
	passwordRE = regexp.MustCompile(`(?im)password\s*:\s*$`)
	promptRE   = regexp.MustCompile(`(?m)[^\r\n]{0,240}[>#$]\s*$`)
)

type Factory struct {
	Config         configload.Snapshot
	MaxOutputBytes int64
	// Timeouts are the invocation's (platform.Timeouts); a zero field
	// falls back to Config.
	Timeouts platform.Timeouts
}
type Driver struct {
	f      Factory
	req    platform.OpenRequest
	conn   net.Conn
	mu     sync.Mutex
	prompt []byte
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
// read after it, up to the next prompt or the secret prompt (whose
// `Password:` is the answer), or up to the failure.
func (d *Driver) noteSetup(before []byte, statement string, buf []byte) {
	output := strings.TrimRight(string(cleanOutput(buf, statement, nil)), " \t")
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
	timeout := platform.Pick(d.f.Timeouts.Prompt, d.f.Config.Duration("execution.prompt-timeout"))
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	buf, kind, err := d.readUntil(ctx, timeout, d.f.MaxOutputBytes, usernameRE, passwordRE, promptRE)
	if err != nil {
		return fmt.Errorf("telnet_login_prompt: %w", err)
	}
	if kind == 2 {
		// The device's prompt without a login exchange.
		d.prompt = lastPrompt(buf)
	}
	if kind == 0 {
		if err := d.writeLine(d.req.Username); err != nil {
			return errorcodes.Errorf("telnet_write_failed", "send username: %w", err)
		}
		_, kind, err = d.readUntil(ctx, timeout, d.f.MaxOutputBytes, passwordRE, promptRE)
		if err != nil {
			return errorcodes.Errorf("telnet_login_prompt", "wait for password prompt: %w", err)
		}
	}
	if kind == 0 {
		var pass []byte
		if d.req.Password == nil {
			return errorcodes.Errorf("credential_password_missing", "telnet password is unavailable")
		}
		if err := d.req.Password(func(v []byte) error { pass = append([]byte(nil), v...); return nil }); err != nil {
			return errorcodes.Ensure(err, "secret_password_unset")
		}
		defer wipe(pass)
		if err := d.writeLine(string(pass)); err != nil {
			return errorcodes.Errorf("telnet_write_failed", "send password: %w", err)
		}
		buf, kind, err = d.readUntil(ctx, timeout, d.f.MaxOutputBytes, promptRE)
		if err != nil {
			return errorcodes.Errorf("authentication_failed", "telnet authentication failed: %w", err)
		}
		d.prompt = lastPrompt(buf)
	} else if kind == 1 {
		d.prompt = lastPrompt(buf)
	}
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
		enableTimeout := platform.Pick(d.f.Timeouts.Enable, d.f.Config.Duration("execution.enable-timeout"))
		if enableTimeout <= 0 {
			enableTimeout = 10 * time.Second
		}
		enableDeadline := time.Now().Add(enableTimeout)
		buf, kind, err = d.readUntil(ctx, enableTimeout, d.f.MaxOutputBytes, passwordRE, promptRE)
		d.noteSetup(d.prompt, level.Escalate, buf)
		if err != nil {
			return errorcodes.Errorf("privilege_failed", "wait for the prompt after %q: %w", level.Escalate, err)
		}
		if kind == 1 {
			d.prompt = lastPrompt(buf)
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
			if err := d.writeLine(string(enable)); err != nil {
				return errorcodes.Errorf("telnet_write_failed", "send enable password: %w", err)
			}
			buf, _, err = d.readUntil(ctx, max(time.Until(enableDeadline), time.Millisecond), d.f.MaxOutputBytes, promptRE)
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
		buf, _, err = d.readUntil(ctx, timeout, d.f.MaxOutputBytes, promptRE)
		d.noteSetup(d.prompt, cmd, buf)
		if err != nil {
			return errorcodes.Errorf("telnet_paging_command_failed", "telnet paging command: %w", err)
		}
		d.prompt = lastPrompt(buf)
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
	timeout, source := platform.Pick(d.f.Timeouts.TelnetRead, d.f.Config.Duration("telnet.read-timeout")), "telnet.read-timeout"
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
	buf, _, err := d.readUntil(ctx, timeout, limit, promptRE)
	observed := err == nil
	after := ""
	if observed {
		after = strings.TrimSpace(string(lastPrompt(buf)))
	}
	if err != nil {
		// A timeout, a failed read, or output over the limit before the
		// prompt leaves the shell out of step with the next command.
		d.broken = true
	}
	result := platform.Result{StartedAt: start, EndedAt: time.Now(), PromptBefore: before, Prompt: after, PromptSource: "observed", PromptObserved: &observed, Output: cleanOutput(buf, c.Text, d.prompt)}
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
	d.prompt = lastPrompt(buf)
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
func (d *Driver) readUntil(ctx context.Context, timeout time.Duration, limit int64, patterns ...*regexp.Regexp) ([]byte, int, error) {
	deadline := time.Now().Add(timeout)
	// A context deadline (execution.device-timeout) ends a blocked read too.
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = d.conn.SetReadDeadline(deadline)
	var out bytes.Buffer
	tmp := make([]byte, 4096)
	state := byte(0)
	cmd := byte(0)
	for {
		select {
		case <-ctx.Done():
			return out.Bytes(), -1, ctx.Err()
		default:
		}
		n, err := d.conn.Read(tmp)
		if n > 0 {
			clean, responses, newState, newCmd := filterTelnet(tmp[:n], state, cmd)
			state, cmd = newState, newCmd
			if len(responses) > 0 {
				_, _ = d.conn.Write(responses)
			}
			out.Write(clean)
			data := out.Bytes()
			for i, p := range patterns {
				if p.Match(data) {
					return append([]byte(nil), data...), i, nil
				}
			}
		}
		if err != nil {
			return out.Bytes(), -1, err
		}
		if limit > 0 && int64(out.Len()) > limit+4096 {
			return out.Bytes(), -1, errorcodes.Errorf("output_limit_exceeded", "output limit exceeded")
		}
	}
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
func lastPrompt(buf []byte) []byte {
	m := promptRE.Find(rendered(buf))
	return append([]byte(nil), bytes.TrimSpace(m)...)
}
func cleanOutput(buf []byte, command string, prompt []byte) []byte {
	lines := strings.Split(string(rendered(buf)), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == strings.TrimSpace(command) {
		lines = lines[1:]
	}
	for len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" || promptRE.MatchString(last) || len(prompt) > 0 && last == strings.TrimSpace(string(prompt)) {
			lines = lines[:len(lines)-1]
		} else {
			break
		}
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
