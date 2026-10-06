// Package devsession is karvi's one device session: the prompt, privilege,
// paging, command, and close rules of the device-session contract, implemented once
// over a byte stream that a transport provides (system OpenSSH's process
// pipes, scrapligo-v1's connection). The transports connect; this package
// speaks to the device.
package devsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
)

// Stream is one device shell as bytes. Read returns the device's output as
// it arrives and an error when the stream has ended; an error that carries
// an error code (errorcodes) is reported under that code, any other as
// command_session_lost. Close ends the transport without a device-side
// logout; the session sends the platform's exit commands itself first when
// the shell is still usable.
type Stream interface {
	io.Reader
	io.Writer
	Close() error
}

// Aborter is a Stream that can end its transport at once, without the
// grace a Close allows a remote logout. A session that has failed (a
// timeout, a cancel, a lost prompt) aborts rather than closes: nothing
// useful can follow on a desynchronised shell.
type Aborter interface {
	Abort()
}

// Options configure one session.
type Options struct {
	Definition platform.Definition
	// EnableSecret delivers the enable secret to the callback; nil when the
	// credential has none.
	EnableSecret func(func([]byte) error) error
	// MaxOutputBytes bounds one command's output (output.max-command-bytes),
	// counted on the settled bytes; a command's own MaxBytes replaces it.
	MaxOutputBytes int64
	// Spool is where a command's settled bytes go past the threshold;
	// Dir "" keeps every response in memory.
	Spool Spool
	// InFlightBytes, when given, receives the settled count of the command
	// in flight as it grows and 0 when the command ends, for the
	// scoreboard's target row.
	InFlightBytes *atomic.Int64
	// LoginTimeout bounds the wait for the first prompt after the stream
	// opens; EnableTimeout one privilege level's step, from its escalate
	// command to its prompt; PromptTimeout the wait after a paging command.
	LoginTimeout, EnableTimeout, PromptTimeout time.Duration
	Debug                                      func(string)
}

// promptSettleDelay is how long a prompt-shaped final line must remain
// unchanged before it is taken as the prompt: a banner line ending in '#'
// that arrived in its own packet is not mistaken for the prompt.
const promptSettleDelay = 100 * time.Millisecond

type chunk struct {
	data []byte
	err  error
}

// Session is one open device session.
type Session struct {
	stream  Stream
	opts    Options
	prompts *prompts
	chunks  chan chunk

	opMu sync.Mutex // one device operation at a time
	// sent counts the commands Execute has written, under opMu, so
	// connection_reused is false on the first and true after it; Prepare's
	// escalation and paging commands do not count.
	sent int
	// setup is what Prepare sent, under opMu: the escalate commands and the
	// paging commands, each with the prompt it was sent at and the device's
	// answer (SetupLines). Never the enable secret, nor anything read
	// between the secret and the next prompt.
	setup []platform.SetupLine

	stateMu sync.Mutex
	prompt  string
	level   string
	broken  bool
	closed  bool
}

func defaultDuration(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}

// Open reads the first prompt from a freshly opened stream. It does not
// escalate or disable paging; Prepare does. On failure the stream is
// closed.
func Open(ctx context.Context, stream Stream, opts Options) (*Session, error) {
	p, err := compile(opts.Definition)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = 64 << 20
	}
	opts.LoginTimeout = defaultDuration(opts.LoginTimeout, 40*time.Second)
	opts.EnableTimeout = defaultDuration(opts.EnableTimeout, 10*time.Second)
	opts.PromptTimeout = defaultDuration(opts.PromptTimeout, 10*time.Second)
	s := &Session{stream: stream, opts: opts, prompts: p, chunks: make(chan chunk, 32)}
	go s.pump()

	loginCtx, cancel := context.WithTimeout(ctx, opts.LoginTimeout)
	defer cancel()
	// The login read settles into memory; the banner reaches
	// nothing but the debug line.
	banner := s.setupResponse("", "")
	prompt, level, _, err := s.readUntil(loginCtx, nil, banner, nil)
	if err != nil {
		s.fail()
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, errorcodes.Errorf("command_session_prompt_timeout", "no device prompt after %s", opts.LoginTimeout)
		}
		return nil, streamError(err)
	}
	s.setPrompt(prompt, level)
	s.debugf("device session ready prompt=%q level=%q banner_bytes=%d", prompt, level, banner.sink.observed)
	return s, nil
}

// Prepare reaches the definition's privileged level (one attempt, never a
// step down) and sends its paging commands.
// On failure the session is ended.
func (s *Session) Prepare(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := s.acquirePrivilege(ctx); err != nil {
		s.fail()
		return err
	}
	for _, command := range s.opts.Definition.PagingCommands {
		if strings.TrimSpace(command) == "" {
			continue
		}
		if err := s.pagingCommand(ctx, command); err != nil {
			s.fail()
			return err
		}
	}
	return nil
}

// SetupLines is what Prepare sent, in order, for the job's
// output.TARGET.txt: these statements have no command record. After a
// failed Prepare the last line is the statement that failed, with what the
// device had answered by then. A driver whose session never opened asks a
// nil Session and has none.
func (s *Session) SetupLines() []platform.SetupLine {
	if s == nil {
		return nil
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.setup
}

// setupResponse is the reader of a login, enable, or paging read: the same
// streaming cleaner as a command's, settling into memory only, the
// failure patterns scanned for the paging answer.
func (s *Session) setupResponse(statement, before string) *response {
	return newResponse(statement, before, newSettled(s.opts.MaxOutputBytes, Spool{}, 0, s.opts.Definition.FailurePatterns, nil))
}

// noteSetup keeps one set-up statement that was written to the device and
// ends its read: returned is the prompt that ended it, removed from the
// answer; empty when none did or when the read ended at the secret prompt,
// which is the answer. The cleaned answer is returned for the caller that
// judges it; a set-up answer is a line or two, so it is in memory.
func (s *Session) noteSetup(before, statement string, resp *response, returned string) []byte {
	_ = resp.finish(returned)
	output := resp.sink.mem
	s.setup = append(s.setup, platform.SetupLine{PromptBefore: before, Statement: statement, Output: string(output)})
	return output
}

// acquirePrivilege escalates from the current level to the privileged one
// along the definition's chain, one attempt per step. A session that is at
// the level or above it sends nothing.
func (s *Session) acquirePrivilege(ctx context.Context) error {
	def := s.opts.Definition
	desired := def.PrivilegedLevel
	if desired == "" || len(def.PrivilegeLevels) == 0 {
		return nil
	}
	current := s.currentLevel()
	if current == desired || s.reaches(current, desired) {
		s.debugf("device session privilege level=%q satisfies %q; nothing sent", current, desired)
		return nil
	}
	// The chain from the desired level down to the current one.
	var chain []compiledLevel
	name := desired
	for name != "" && name != current {
		l, ok := s.compiledLevel(name)
		if !ok {
			return errorcodes.Errorf("privilege_failed", "privileged level %q is not reachable from the %q prompt %q", desired, current, s.currentPrompt())
		}
		chain = append(chain, l)
		name = l.Previous
	}
	if name != current {
		return errorcodes.Errorf("privilege_failed", "privileged level %q is not reachable from the %q prompt %q", desired, current, s.currentPrompt())
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if err := s.escalate(ctx, chain[i]); err != nil {
			return err
		}
	}
	return nil
}

// reaches reports whether from is at or above to: to is on from's chain of
// previous levels.
func (s *Session) reaches(from, to string) bool {
	name := from
	for i := 0; i < len(s.prompts.levels)+1 && name != ""; i++ {
		l, ok := s.compiledLevel(name)
		if !ok {
			return false
		}
		if l.Previous == to {
			return true
		}
		name = l.Previous
	}
	return false
}

func (s *Session) compiledLevel(name string) (compiledLevel, bool) {
	for _, l := range s.prompts.levels {
		if l.Name == name {
			return l, true
		}
	}
	return compiledLevel{}, false
}

// escalate makes the one attempt to reach level: its escalate command,
// the secret at the escalate prompt when the level asks for one, and the
// level's prompt, the whole step within the enable timeout. Anything else
// is privilege_failed with what was observed and never the secret.
func (s *Session) escalate(ctx context.Context, level compiledLevel) error {
	if level.Escalate == "" {
		return errorcodes.Errorf("privilege_failed", "level %q declares no escalation command", level.Name)
	}
	before := s.currentPrompt()
	s.debugf("device session escalating to level=%q from prompt=%q", level.Name, before)
	if err := s.write(level.Escalate + "\n"); err != nil {
		return errorcodes.Errorf("privilege_failed", "send %q: %v", level.Escalate, err)
	}
	stepCtx, cancel := context.WithTimeout(ctx, s.opts.EnableTimeout)
	defer cancel()
	resp := s.setupResponse(level.Escalate, before)
	prompt, observed, atSecret, err := s.readUntil(stepCtx, &level, resp, nil)
	last := resp.lastLine()
	// The set-up line is this first read alone: the statement and, at the
	// secret prompt, the device's `Password:`. What is read after the secret
	// is sent is never kept, so an echoed secret cannot reach the text file.
	if atSecret || err != nil {
		s.noteSetup(before, level.Escalate, resp, "")
	} else {
		s.noteSetup(before, level.Escalate, resp, prompt)
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return errorcodes.Errorf("privilege_failed", "no prompt within %s after %q; last output %q", s.opts.EnableTimeout, level.Escalate, last)
		}
		return errorcodes.Ensure(streamError(err), "privilege_failed")
	}
	if atSecret {
		if s.opts.EnableSecret == nil {
			return errorcodes.Errorf("privilege_failed", "the device asked for an enable secret after %q and the credential has none", level.Escalate)
		}
		var sendErr error
		if err := s.opts.EnableSecret(func(secret []byte) error {
			sendErr = s.writeSecret(secret)
			return nil
		}); err != nil {
			return errorcodes.Ensure(err, "privilege_failed")
		}
		if sendErr != nil {
			return errorcodes.Errorf("privilege_failed", "send the enable secret: %v", sendErr)
		}
		// The same deadline: the enable timeout bounds the level's whole
		// step, not each wait.
		// Nothing of this read but its prompt is used: a device or terminal
		// server that echoes the secret has put it in these bytes, so they
		// reach no message, record, or file. Through v0.12.1 the timeout's
		// message quoted their last line, which was the echoed secret.
		afterSecret := s.setupResponse("", "")
		prompt, observed, _, err = s.readUntil(stepCtx, nil, afterSecret, nil)
		afterSecret.sink.discard()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				return errorcodes.Errorf("privilege_failed", "no prompt after the enable secret within %s of %q", s.opts.EnableTimeout, level.Escalate)
			}
			return errorcodes.Ensure(streamError(err), "privilege_failed")
		}
	}
	if observed != level.Name {
		s.setPrompt(prompt, observed)
		return errorcodes.Errorf("privilege_failed", "the device stayed at the %q prompt %q after one %q attempt", observed, prompt, level.Escalate)
	}
	s.setPrompt(prompt, observed)
	s.debugf("device session at level=%q prompt=%q", observed, prompt)
	return nil
}

func (s *Session) pagingCommand(ctx context.Context, command string) error {
	previous := s.currentPrompt()
	if err := s.write(command + "\n"); err != nil {
		return errorcodes.Errorf("paging_disable_failed", "send %q: %v", command, err)
	}
	stepCtx, cancel := context.WithTimeout(ctx, s.opts.PromptTimeout)
	defer cancel()
	resp := s.setupResponse(command, previous)
	prompt, level, _, err := s.readUntil(stepCtx, nil, resp, nil)
	output := s.noteSetup(previous, command, resp, prompt)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return errorcodes.Errorf("paging_disable_failed", "no prompt within %s after %q", s.opts.PromptTimeout, command)
		}
		return errorcodes.Ensure(streamError(err), "paging_disable_failed")
	}
	s.setPrompt(prompt, level)
	if resp.sink.failures.found {
		return errorcodes.Errorf("paging_disable_failed", "the device rejected %q: %s", command, strings.TrimSpace(string(output)))
	}
	s.debugf("device session paging command sent command=%q prompt=%q", command, prompt)
	return nil
}

// Execute writes one command and reads until the prompt returns. The
// result is the contract's: the observed prompt, the cleaned and bounded
// output, device_command_error from the platform's failure patterns,
// command_timeout when the deadline passes (the session then ends).
//
// A blind command (Blind set; BlindReturns > 0 always comes with it) is
// written with its returns in the same write and awaited for Timeout, the
// blind wait. When the prompt does not return, because the wait passed or
// the stream ended, the result is a success that holds all bytes read and
// the notice prompt_not_observed_after_blind_send, and the session ends: the
// shell is out of step.
//
// The command's Expectations are answered as their prompts appear during
// the read, under the same deadline (an expecter); a declared pattern that
// never appears is the ordinary command_timeout, whose message then names
// the last line seen and the count answered.
func (s *Session) Execute(ctx context.Context, command platform.Command) platform.Result {
	started := time.Now()
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.unusable() {
		return failed(started, "command_session_lost", "connection", true, true, errors.New("the device session ended before the next command; no replacement login was attempted"))
	}
	blind := command.Blind
	timeout := defaultDuration(command.Timeout, 120*time.Second)
	if blind {
		timeout = max(command.Timeout, 0)
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	limit, limitSource := command.Limit(s.opts.MaxOutputBytes)

	hash := sha256.Sum256([]byte(command.Text))
	s.debugf("device session send sha256=%s bytes=%d blind=%t blind_returns=%d expectations=%d prompt=%q", hex.EncodeToString(hash[:8]), len(command.Text), blind, command.BlindReturns, len(command.Expectations), s.currentPrompt())
	if err := s.write(command.Text + "\n" + strings.Repeat("\r", max(command.BlindReturns, 0))); err != nil {
		s.fail()
		return failed(started, "command_session_lost", "connection", true, true, fmt.Errorf("write command to the device session: %w", err))
	}
	reused := s.sent > 0
	s.sent++
	var ex *expecter
	if len(command.Expectations) > 0 {
		ex = &expecter{declarations: command.Expectations, consumed: make([]bool, len(command.Expectations))}
	}
	// The prompt the statement was typed at: the last one matched from the
	// device's bytes (login, enable, paging, or the previous statement). A
	// read that ends without a prompt ends the session, so a statement that
	// is sent always has an observed prompt before it.
	before := s.currentPrompt()
	// The command's reader: the response settles as
	// it arrives, to memory up to the threshold and then to this command's
	// spool, named by its ordinal in the session.
	sink := newSettled(limit, s.opts.Spool, s.sent, s.opts.Definition.FailurePatterns, func(path string) {
		s.debugf("device session spool opened sha256=%s path=%s", hex.EncodeToString(hash[:8]), path)
	})
	sink.progress = s.opts.InFlightBytes
	if sink.progress != nil {
		defer sink.progress.Store(0) // no command in flight once Execute returns
	}
	resp := newResponse(command.Text, before, sink)
	prompt, level, _, err := s.readUntil(commandCtx, nil, resp, ex)
	ended := time.Now()
	observed := err == nil
	result := platform.Result{PromptBefore: before, Prompt: prompt, PromptSource: "observed", PromptObserved: &observed, ConnectionReused: &reused, StartedAt: started, EndedAt: ended}
	if err != nil {
		s.fail()
		var passed *limitError
		switch {
		case errors.As(err, &passed):
			// The settled byte that would pass the limit stopped the read:
			// the store holds exactly the first limit bytes.
			sink.output(&result)
			result.ErrorCode, result.ErrorCategory = "output_limit_exceeded", "output"
			result.External = true
			result.Err = fmt.Errorf("command output exceeded %d bytes%s before the prompt returned, %d observed; the session is closed", limit, platform.Named(limitSource), passed.observed)
		case errorcodes.Of(err) == "output_spool_write_failed":
			// Nothing of the output; the session is closed as
			// the limit closes it.
			result.ErrorCode, result.ErrorCategory = "output_spool_write_failed", "output"
			result.Err = errors.New(errorcodes.Message(err))
		case blind && ctx.Err() == nil:
			// The prompt did not return after the blind send: a success with
			// what was read; the session is ended.
			reason := "session_ended"
			if errors.Is(err, context.DeadlineExceeded) {
				reason = "blind_wait_expired"
			}
			s.cut(resp, &result)
			result.Notices = []platform.Notice{promptNotObserved(command.BlindReturns, timeout, reason)}
			s.debugf("device session blind send without a returning prompt sha256=%s reason=%s output_bytes=%d", hex.EncodeToString(hash[:8]), reason, sink.n)
			return result
		case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
			result.ErrorCode, result.ErrorCategory = "command_timeout", "timeout"
			result.External, result.Retryable = true, true
			// With declarations, the diagnostic of a mistyped pattern: the
			// line the device was waiting at, and how many were answered.
			diagnostic := ""
			if ex != nil {
				diagnostic = fmt.Sprintf("; the last line seen was %q and %d of %d declared responses were answered", resp.lastLine(), ex.answered, len(ex.declarations))
			}
			result.Err = fmt.Errorf("command timed out after %s%s while waiting for a returning prompt%s; the session is closed", timeout, platform.Named(command.TimeoutSource), diagnostic)
			s.cut(resp, &result)
		default:
			coded := streamError(err)
			code := errorcodes.Of(coded)
			result.ErrorCode, result.ErrorCategory, result.External, result.Retryable = code, "connection", true, false
			if entry, ok := errorcodes.Lookup(code); ok {
				result.ErrorCategory, result.Retryable = entry.Category, entry.Retryable
			}
			result.Err = errors.New(errorcodes.Message(coded))
			s.cut(resp, &result)
		}
		s.debugf("device session command failed sha256=%s code=%s output_bytes=%d spooled=%t", hex.EncodeToString(hash[:8]), result.ErrorCode, sink.n, result.Spool != nil)
		return result
	}
	s.setPrompt(prompt, level)
	// The tail gives up the prompt and the trailing blanks and settles; the
	// closing newline follows. A limit passed here is the
	// limit after the prompt: the shell is in step and the session usable.
	err = resp.finish(prompt)
	sink.output(&result)
	if err != nil {
		var passed *limitError
		if errors.As(err, &passed) {
			result.ErrorCode, result.ErrorCategory = "output_limit_exceeded", "output"
			result.External = true
			result.Err = fmt.Errorf("command output exceeded %d bytes%s, %d observed", limit, platform.Named(limitSource), passed.observed)
			return result
		}
		s.fail()
		result.ErrorCode, result.ErrorCategory = "output_spool_write_failed", "output"
		result.Err = errors.New(errorcodes.Message(err))
		return result
	}
	if sink.failures.found {
		result.DeviceError = true
		result.ErrorCode, result.ErrorCategory = "device_command_error", "device"
		result.External = true
		result.Err = errors.New("device reported a command error")
	}
	s.debugf("device session command complete sha256=%s output_bytes=%d spooled=%t prompt=%q device_error=%t", hex.EncodeToString(hash[:8]), sink.n, result.Spool != nil, prompt, result.DeviceError)
	return result
}

// cut ends a read that no prompt ended: what
// settled by the cut, cleaned as far as the cut allows, is handed back in
// the result with its place, memory or spool; which endings record it is
// the executor's rule, and the executor records what it is handed. A limit or a
// spool failure met by this last settling changes nothing of the cut's own
// code: the store holds the first limit bytes, or nothing.
func (s *Session) cut(resp *response, result *platform.Result) {
	_ = resp.finish("")
	resp.sink.output(result)
}

// Close sends the platform's exit commands when the shell is still usable
// and closes the stream.
func (s *Session) Close() error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.stateMu.Lock()
	if s.closed {
		s.stateMu.Unlock()
		return nil
	}
	s.closed = true
	usable := !s.broken
	s.stateMu.Unlock()
	if usable {
		for _, command := range s.opts.Definition.ExitCommands {
			if strings.TrimSpace(command) == "" {
				continue
			}
			if err := s.write(command + "\n"); err != nil {
				break
			}
		}
	}
	return s.stream.Close()
}

// Prompt is the prompt last observed.
func (s *Session) Prompt() string { return s.currentPrompt() }

// Level is the privilege level last observed ("" for a platform without
// levels).
func (s *Session) Level() string { return s.currentLevel() }

func (s *Session) pump() {
	defer close(s.chunks)
	buffer := make([]byte, 4096)
	for {
		n, err := s.stream.Read(buffer)
		if n > 0 {
			s.chunks <- chunk{data: append([]byte(nil), buffer[:n]...)}
		}
		if err != nil {
			s.chunks <- chunk{err: err}
			return
		}
	}
}

// expecter is the state of one command's expect-and-send declarations
// during its read. The declarations are
// tried in declared order on the last line received since the mark, each
// consumed once; the mark starts at the command's write and moves to the
// last line's end at each answer (response.markHere), so no pattern fires
// twice on the same prompt line once the echo of its answer lands there. Two
// declarations with the same pattern answer a twice-asked prompt in turn; a
// prompt the device does not ask leaves its declaration unconsumed, no
// notice.
type expecter struct {
	declarations []platform.Expectation
	consumed     []bool
	answered     int
}

// match returns the first unconsumed declaration whose pattern matches the
// last line of the response's tail since the mark, or -1. A last line
// settled from its front is a page, not a question, and is not tried.
func (ex *expecter) match(r *response) int {
	if ex == nil || r.overflow {
		return -1 // a read without declarations
	}
	line := r.sinceMark()
	if line == "" {
		return -1
	}
	for i, d := range ex.declarations {
		if !ex.consumed[i] && d.Pattern.MatchString(line) {
			return i
		}
	}
	return -1
}

// readUntil reads into r until a prompt-shaped final line remains
// unchanged for the settle interval, or, when level is given, until that
// level's escalate prompt is the final line (atSecret). It returns the
// prompt, its level, and the error: the context's when it expires, the
// limitError when the settled bytes would pass the limit, the spool's when
// it cannot be written, the stream's when it ends. What was read is in r:
// settled into its sink, the tail still held for the caller's finish.
//
// With an expecter, after each chunk and before the prompt check, the
// unconsumed declarations are tried; a match is answered with its response
// and one carriage return (the return a blind send writes), the settle
// timer is stopped, and the same read goes on under the same deadline,
// which is never reset. The exchange stays in the settled bytes, each
// echoed answer on its prompt line.
func (s *Session) readUntil(ctx context.Context, level *compiledLevel, r *response, ex *expecter) (prompt, observedLevel string, atSecret bool, err error) {
	var settle *time.Timer
	var settleC <-chan time.Time
	stopSettle := func() {
		if settle != nil && !settle.Stop() {
			select {
			case <-settle.C:
			default:
			}
		}
		settleC = nil
	}
	defer stopSettle()
	armSettle := func() {
		if settle == nil {
			settle = time.NewTimer(promptSettleDelay)
		} else {
			stopSettle()
			settle.Reset(promptSettleDelay)
		}
		settleC = settle.C
	}
	for {
		select {
		case <-ctx.Done():
			return "", "", false, ctx.Err()
		case <-settleC:
			settleC = nil
			if p, l, secret := r.candidate(level, s.prompts); p != "" {
				return p, l, secret, nil
			}
		case c, ok := <-s.chunks:
			if !ok {
				if p, l, secret := r.candidate(level, s.prompts); p != "" {
					return p, l, secret, nil
				}
				return "", "", false, s.endedError(nil)
			}
			if len(c.data) > 0 {
				if err := r.feed(c.data); err != nil {
					return "", "", false, err
				}
				if i := ex.match(r); i >= 0 {
					// A declared prompt: answer it. The line cannot be the
					// returning prompt as well, so the settle timer stops and
					// the read goes on for the device's next line.
					d := ex.declarations[i]
					ex.consumed[i] = true
					ex.answered++
					r.markHere()
					stopSettle()
					s.debugf("device session expectation answered index=%d pattern=%q response_bytes=%d", i+1, d.Pattern.String(), len(d.Response))
					if err := s.write(d.Response + "\r"); err != nil {
						return "", "", false, s.endedError(err)
					}
				} else if p, _, _ := r.candidate(level, s.prompts); p != "" {
					armSettle()
				} else {
					stopSettle()
				}
			}
			if c.err != nil {
				if p, l, secret := r.candidate(level, s.prompts); p != "" {
					return p, l, secret, nil
				}
				return "", "", false, s.endedError(c.err)
			}
		}
	}
}

func (s *Session) endedError(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return errorcodes.Errorf("command_session_lost", "the device session ended")
	}
	return err
}

// promptNotObserved is the notice of a blind send whose prompt did not
// return: the wait passed, or the stream ended during it. A
// command blind by the flag alone has no returns clause.
func promptNotObserved(returns int, wait time.Duration, reason string) platform.Notice {
	why := fmt.Sprintf("the blind wait of %s passed", wait)
	if reason == "session_ended" {
		why = "the session ended during the blind wait"
	}
	after := "the command"
	if returns > 0 {
		after = "the command and " + plural(returns, "blind return")
	}
	return platform.Notice{
		Code:    "prompt_not_observed_after_blind_send",
		Message: fmt.Sprintf("the prompt did not return after %s: %s; the record holds all bytes read and the session is closed", after, why),
		Details: map[string]any{"blind_returns": returns, "blind_wait": wait.String(), "reason": reason},
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// KeepaliveTimeout is the error a transport's stream ends with when the
// device answered none of count keepalive requests sent every interval: one
// code and one wording for both SSH transports. detail is the
// transport's own diagnostic, when it has one.
func KeepaliveTimeout(count int, interval time.Duration, detail string) error {
	if detail != "" {
		detail = " (" + detail + ")"
	}
	return errorcodes.Errorf("session_keepalive_timeout", "the device answered none of %d keepalives sent every %s; the session is closed%s", count, interval, detail)
}

// streamError is a stream's error under its code: the code it carries, or
// command_session_lost.
func streamError(err error) error {
	if errorcodes.Of(err) != "" {
		return err
	}
	if errors.Is(err, io.EOF) {
		return errorcodes.Errorf("command_session_lost", "the device session ended")
	}
	return errorcodes.Errorf("command_session_lost", "%v", err)
}

func (s *Session) write(text string) error {
	_, err := io.WriteString(s.stream, text)
	return err
}

// writeSecret writes the secret and a return without keeping a copy.
func (s *Session) writeSecret(secret []byte) error {
	line := make([]byte, 0, len(secret)+1)
	line = append(line, secret...)
	line = append(line, '\n')
	defer func() {
		for i := range line {
			line[i] = 0
		}
	}()
	_, err := s.stream.Write(line)
	return err
}

func (s *Session) fail() {
	s.stateMu.Lock()
	s.broken = true
	s.stateMu.Unlock()
	if a, ok := s.stream.(Aborter); ok {
		a.Abort()
		return
	}
	_ = s.stream.Close()
}

// Usable reports whether the session can take another command: it is
// neither closed nor ended by a failure.
func (s *Session) Usable() bool { return !s.unusable() }

func (s *Session) unusable() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.closed || s.broken
}

func (s *Session) setPrompt(prompt, level string) {
	s.stateMu.Lock()
	s.prompt, s.level = prompt, level
	s.stateMu.Unlock()
}

func (s *Session) currentPrompt() string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.prompt
}

func (s *Session) currentLevel() string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.level
}

func (s *Session) debugf(format string, args ...any) {
	if s.opts.Debug != nil {
		s.opts.Debug(fmt.Sprintf(format, args...))
	}
}

func failed(start time.Time, code, category string, external, retryable bool, err error) platform.Result {
	return platform.Result{StartedAt: start, EndedAt: time.Now(), ErrorCode: code, ErrorCategory: category, External: external, Retryable: retryable, Err: err}
}
