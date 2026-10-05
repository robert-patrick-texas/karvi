package devsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
)

// The exec channel's session. A device whose platform's channel is exec has
// one connection for its command list, ready once it has authenticated, and
// each command runs on an exec channel of its own, in order, without a pty
// and with standard input at its end. There is no prompt, privilege step,
// paging command, or exit command, and nothing carries between commands.
// Each stream settles as a shell's response does (settle.go): to memory up
// to the spool threshold and then to a spool of its own, the limit counting
// the two together; the platform's failure patterns are searched in both,
// and asked only when the command exited 0. A command timeout, a cancel,
// and the limit stop the command (ExecChannel.Stop) and close its channel,
// leaving the connection serving the next; a connection lost ends the
// session. The transport supplies the connection (ExecConn).

// ExecConn is one connection that runs commands on exec channels.
type ExecConn interface {
	// Start opens an exec channel and starts the command on it. Its error
	// carries the transport's code: ssh_session_channel_refused for a
	// device that refuses the channel, the connection's own when it is
	// gone.
	Start(command string) (ExecChannel, error)
	Close() error
}

// ExecChannel is one command running on its exec channel.
type ExecChannel interface {
	Stdout() io.Reader
	Stderr() io.Reader
	// Wait returns how the command ended, once both streams have been read
	// to their end: its exit status, nil when none came back, and the
	// signal's name ("unnamed" where the transport names none, "" for
	// none). A connection lost is an error carrying the transport's code;
	// a channel closed without a status on a live connection is neither.
	Wait() (status *int, signal string, err error)
	// Stop ends a command given up (a timeout, a cancel, the limit) and
	// closes its channel; the notices say what could not be done.
	Stop() []platform.Notice
}

// ExecOptions configure an exec session.
type ExecOptions struct {
	Definition     platform.Definition // the failure patterns
	MaxOutputBytes int64
	Spool          Spool
	InFlightBytes  *atomic.Int64
	Debug          func(string)
}

// ExecSession runs a device's commands on exec channels of its connection.
type ExecSession struct {
	conn ExecConn
	opts ExecOptions

	opMu   sync.Mutex // one command at a time
	mu     sync.Mutex
	sent   int
	broken bool
	closed bool
}

// OpenExec is the session on a connection that has authenticated.
func OpenExec(conn ExecConn, opts ExecOptions) *ExecSession {
	return &ExecSession{conn: conn, opts: opts}
}

// Usable reports whether the connection can take another command: neither
// closed nor lost.
func (s *ExecSession) Usable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.broken && !s.closed
}

// Close closes the connection; no exit command is sent.
func (s *ExecSession) Close() error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	return s.conn.Close()
}

func (s *ExecSession) fail() {
	s.mu.Lock()
	s.broken = true
	s.mu.Unlock()
	_ = s.conn.Close()
}

func (s *ExecSession) debugf(format string, args ...any) {
	if s.opts.Debug != nil {
		s.opts.Debug(fmt.Sprintf(format, args...))
	}
}

// execChunk is one read from a stream, or its end.
type execChunk struct {
	stream int // 0 stdout, 1 stderr
	data   []byte
	err    error // io.EOF at the stream's end
}

// Execute runs one command on its own exec channel.
func (s *ExecSession) Execute(ctx context.Context, command platform.Command) platform.Result {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	started := time.Now()
	if !s.Usable() {
		return failed(started, "command_session_lost", "connection", true, false, errors.New("the device session is not usable"))
	}
	timeout := defaultDuration(command.Timeout, 120*time.Second)
	hash := sha256.Sum256([]byte(command.Text))
	tag := hex.EncodeToString(hash[:8])
	s.sent++
	index, reused := s.sent, s.sent > 1
	s.debugf("device session exec start sha256=%s bytes=%d index=%d", tag, len(command.Text), index)
	ch, err := s.conn.Start(command.Text)
	if err != nil {
		s.fail()
		result := codedFailure(started, err)
		result.ConnectionReused = &reused
		s.debugf("device session exec failed to start sha256=%s code=%s", tag, result.ErrorCode)
		return result
	}

	var shared int64
	sinks := [2]*settled{
		newSettled(s.opts.MaxOutputBytes, s.opts.Spool, index, s.opts.Definition.FailurePatterns, func(path string) {
			s.debugf("device session spool opened sha256=%s path=%s", tag, path)
		}),
		newSettled(s.opts.MaxOutputBytes, Spool{Dir: s.opts.Spool.Dir, Threshold: s.opts.Spool.Threshold, Activity: s.opts.Spool.Activity, Device: s.opts.Spool.Device, Stderr: true}, index, s.opts.Definition.FailurePatterns, func(path string) {
			s.debugf("device session stderr spool opened sha256=%s path=%s", tag, path)
		}),
	}
	for _, sink := range sinks {
		sink.shared, sink.progress = &shared, s.opts.InFlightBytes
	}
	if s.opts.InFlightBytes != nil {
		defer s.opts.InFlightBytes.Store(0) // no command in flight once Execute returns
	}

	// Each stream is read on its own goroutine and settled here, one chunk
	// at a time, so the two sinks share their count without a lock; a
	// reader waits for its chunk to settle before reading the next.
	chunks := make(chan execChunk)
	settledOne := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	quit := make(chan struct{})
	var readers sync.WaitGroup
	for i, r := range []io.Reader{ch.Stdout(), ch.Stderr()} {
		readers.Add(1)
		go func(i int, r io.Reader) {
			defer readers.Done()
			buf := make([]byte, 32<<10)
			for {
				n, err := r.Read(buf)
				if n > 0 {
					select {
					case chunks <- execChunk{stream: i, data: buf[:n]}:
					case <-quit:
						return
					}
					select {
					case <-settledOne[i]:
					case <-quit:
						return
					}
				}
				if err != nil {
					select {
					case chunks <- execChunk{stream: i, err: err}:
					case <-quit:
					}
					return
				}
			}
		}(i, r)
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	stop := func() []platform.Notice {
		close(quit)
		notices := ch.Stop()
		readers.Wait()
		return notices
	}
	result := platform.Result{ConnectionReused: &reused, StartedAt: started, Exec: &platform.ExecResult{}}
	// handOver gives the result what each stream settled.
	handOver := func() {
		var out platform.Result
		sinks[0].output(&out)
		result.Output, result.Spool = out.Output, out.Spool
		var errOut platform.Result
		sinks[1].output(&errOut)
		result.Exec.Stderr, result.Exec.StderrSpool = errOut.Output, errOut.Spool
	}

	var readErr error
	for open := 2; open > 0; {
		select {
		case c := <-chunks:
			if c.err != nil {
				open--
				if !errors.Is(c.err, io.EOF) && readErr == nil {
					readErr = c.err
				}
				continue
			}
			werr := sinks[c.stream].write(c.data)
			settledOne[c.stream] <- struct{}{}
			if werr != nil {
				result.Notices = stop()
				result.EndedAt = time.Now()
				var limit *limitError
				if errors.As(werr, &limit) {
					handOver()
					result.ErrorCode, result.ErrorCategory, result.External = "output_limit_exceeded", "output", true
					result.Err = fmt.Errorf("command output exceeded %d bytes across stdout and stderr (%d observed); the command was stopped", s.opts.MaxOutputBytes, limit.observed)
				} else {
					sinks[0].discard()
					sinks[1].discard()
					result.ErrorCode, result.ErrorCategory = "output_spool_write_failed", "output"
					result.Err = errors.New(errorcodes.Message(werr))
				}
				s.debugf("device session exec failed sha256=%s code=%s output_bytes=%d", tag, result.ErrorCode, shared)
				return result
			}
		case <-deadline.C:
			result.ErrorCode, result.Err = "command_timeout", fmt.Errorf("command timed out after %s; the command was stopped and its channel closed", timeout)
			return s.givenUp(&result, stop, handOver, tag, &shared)
		case <-ctx.Done():
			result.Err = ctx.Err()
			return s.givenUp(&result, stop, handOver, tag, &shared)
		}
	}
	readers.Wait()

	// Both streams ended; the status follows, within the command's time.
	type exit struct {
		status *int
		signal string
		err    error
	}
	waited := make(chan exit, 1)
	go func() {
		status, signal, err := ch.Wait()
		waited <- exit{status, signal, err}
	}()
	var x exit
	select {
	case x = <-waited:
	case <-deadline.C:
		result.ErrorCode, result.Err = "command_timeout", fmt.Errorf("command timed out after %s waiting for its exit status; the command was stopped and its channel closed", timeout)
		return s.givenUp(&result, ch.Stop, handOver, tag, &shared)
	case <-ctx.Done():
		result.Err = ctx.Err()
		return s.givenUp(&result, ch.Stop, handOver, tag, &shared)
	}
	result.EndedAt = time.Now()
	handOver()
	if x.err == nil && readErr != nil {
		x.err = readErr
	}
	if x.err != nil {
		// The connection is gone: what settled is kept, and nothing more
		// is sent.
		s.fail()
		coded := streamError(x.err)
		code := errorcodes.Of(coded)
		result.ErrorCode, result.ErrorCategory, result.External = code, "connection", true
		if entry, ok := errorcodes.Lookup(code); ok {
			result.ErrorCategory, result.Retryable = entry.Category, entry.Retryable
		}
		result.Err = errors.New(errorcodes.Message(coded))
		s.debugf("device session exec failed sha256=%s code=%s output_bytes=%d", tag, code, shared)
		return result
	}
	result.Exec.ExitStatus, result.Exec.ExitSignal = x.status, x.signal
	switch {
	case x.signal != "":
		result.ErrorCode, result.Err = "command_exit_signal", fmt.Errorf("ended by signal %s", x.signal)
	case x.status == nil:
		result.ErrorCode, result.Err = "command_exit_missing", errors.New("closed without an exit status")
	case *x.status != 0:
		result.ErrorCode, result.Err = "command_exit_nonzero", fmt.Errorf("exited %d", *x.status)
	case sinks[0].failures.found || sinks[1].failures.found:
		result.ErrorCode, result.Err = "device_command_error", errors.New("device reported a command error")
	}
	if result.Err != nil {
		result.DeviceError, result.ErrorCategory, result.External = true, "device", true
	}
	s.debugf("device session exec complete sha256=%s output_bytes=%d code=%q", tag, shared, result.ErrorCode)
	return result
}

// givenUp ends a command a timeout or a cancel gave up: the command is
// stopped and its channel closed, what settled is handed back, and the
// connection serves the next command. The caller has set the result's
// error: a timeout's with its code, or a cancel's, the context's, with
// none, which the executor classifies.
func (s *ExecSession) givenUp(result *platform.Result, stop func() []platform.Notice, handOver func(), tag string, shared *int64) platform.Result {
	result.Notices = append(result.Notices, stop()...)
	result.EndedAt = time.Now()
	handOver()
	if result.ErrorCode != "" {
		result.ErrorCategory, result.External, result.Retryable = "timeout", true, true
	}
	s.debugf("device session exec given up sha256=%s code=%q output_bytes=%d", tag, result.ErrorCode, *shared)
	return *result
}

// codedFailure is a channel that could not start: the transport's code and
// its registry category.
func codedFailure(started time.Time, err error) platform.Result {
	coded := streamError(err)
	code := errorcodes.Of(coded)
	result := failed(started, code, "connection", true, false, errors.New(errorcodes.Message(coded)))
	if entry, ok := errorcodes.Lookup(code); ok {
		result.ErrorCategory, result.Retryable = entry.Category, entry.Retryable
	}
	return result
}
