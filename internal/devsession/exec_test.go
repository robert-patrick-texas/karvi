package devsession

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
)

// fakeExec is an in-memory exec connection: each command's channel is the
// script's entry for it.
type fakeExec struct {
	mu       sync.Mutex
	script   map[string]fakeRun
	started  []string
	stopped  []string
	closed   bool
	startErr error
	gone     chan struct{} // closed at the connection's Close
	// silentStart is a device that does not answer the channel's open:
	// Start waits for the connection's Close, and with startsAnyway
	// returns a channel then. startDelay is a device that answers the
	// open that late.
	silentStart  bool
	startsAnyway bool
	startDelay   time.Duration
}

// fakeRun is what one command does: its two streams, then its end. Hold
// keeps both streams open until Stop. Silent is a device that no longer
// answers: the streams stay open past Stop, until the connection's Close,
// or until closeAfter from Stop, the device closing the channel late.
// stopHangs and waitHangs hold Stop and Wait until the connection's Close.
type fakeRun struct {
	stdout, stderr string
	status         *int
	signal         string
	waitErr        error
	hold           bool
	silent         bool
	closeAfter     time.Duration
	stopHangs      bool
	waitHangs      bool
}

func code(n int) *int { return &n }

func (f *fakeExec) Start(command string, _ bool) (ExecChannel, error) {
	if f.silentStart {
		<-f.gone
		if !f.startsAnyway {
			return nil, errorcodes.Errorf("command_session_lost", "the connection is closed")
		}
	}
	time.Sleep(f.startDelay)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.started = append(f.started, command)
	run := f.script[command]
	ch := &fakeChannel{f: f, command: command, run: run}
	if run.hold || run.silent {
		var w1, w2 *io.PipeWriter
		ch.stdout, w1 = io.Pipe()
		ch.stderr, w2 = io.Pipe()
		go func() { _, _ = io.WriteString(w1, run.stdout) }()
		go func() { _, _ = io.WriteString(w2, run.stderr) }()
		end := func() { w1.Close(); w2.Close() }
		ch.stop = end
		if run.silent {
			go func() { <-f.gone; end() }()
			ch.stop = func() {
				if run.closeAfter > 0 {
					time.AfterFunc(run.closeAfter, end)
				}
			}
		}
	} else {
		ch.stdout, ch.stderr = strings.NewReader(run.stdout), strings.NewReader(run.stderr)
	}
	return ch, nil
}

func (f *fakeExec) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		close(f.gone)
	}
	f.closed = true
	return nil
}

type fakeChannel struct {
	f              *fakeExec
	command        string
	run            fakeRun
	stdout, stderr io.Reader
	stop           func()
}

func (c *fakeChannel) Stdout() io.Reader     { return c.stdout }
func (c *fakeChannel) Input() io.WriteCloser { return nil }
func (c *fakeChannel) Stderr() io.Reader     { return c.stderr }
func (c *fakeChannel) Wait() (*int, string, error) {
	if c.run.waitHangs {
		<-c.f.gone
		return nil, "", errorcodes.Errorf("command_session_lost", "the connection is closed")
	}
	return c.run.status, c.run.signal, c.run.waitErr
}
func (c *fakeChannel) Stop() []platform.Notice {
	c.f.mu.Lock()
	c.f.stopped = append(c.f.stopped, c.command)
	c.f.mu.Unlock()
	if c.run.stopHangs {
		<-c.f.gone
	}
	if c.stop != nil {
		c.stop()
	}
	return nil
}

func execSession(script map[string]fakeRun, opts ExecOptions) (*ExecSession, *fakeExec) {
	f := &fakeExec{script: script, gone: make(chan struct{})}
	if opts.Definition.Name == "" {
		opts.Definition = platform.Definition{Name: "linux", FailurePatterns: []string{"% Invalid input"}}
	}
	return OpenExec(f, opts), f
}

// TestExecOutcomes: each way a command ends on its exec channel, its code,
// and the session usable after every one of them.
func TestExecOutcomes(t *testing.T) {
	script := map[string]fakeRun{
		"both":     {stdout: "data\n", stderr: "warn\n", status: code(0)},
		"fail 2":   {stderr: "ls: cannot access\n", status: code(2)},
		"signal":   {signal: "TERM"},
		"nostatus": {stdout: "partial\n"},
		"pattern":  {stderr: "% Invalid input detected\n", status: code(0)},
		"pattern1": {stdout: "% Invalid input detected\n", status: code(1)},
	}
	s, _ := execSession(script, ExecOptions{})
	for _, tc := range []struct {
		command, code, stdout, stderr string
		status                        *int
		signal                        string
	}{
		{"both", "", "data\n", "warn\n", code(0), ""},
		{"fail 2", "command_exit_nonzero", "", "ls: cannot access\n", code(2), ""},
		{"signal", "command_exit_signal", "", "", nil, "TERM"},
		{"nostatus", "command_exit_missing", "partial\n", "", nil, ""},
		{"pattern", "device_command_error", "", "% Invalid input detected\n", code(0), ""},
		// The status first: a non-zero exit is its own code, whatever the
		// patterns find.
		{"pattern1", "command_exit_nonzero", "% Invalid input detected\n", "", code(1), ""},
	} {
		r := s.Execute(context.Background(), platform.Command{Text: tc.command, Timeout: 5 * time.Second})
		if r.ErrorCode != tc.code || string(r.Output) != tc.stdout || r.Exec == nil || string(r.Exec.Stderr) != tc.stderr || r.Exec.ExitSignal != tc.signal || (r.Exec.ExitStatus == nil) != (tc.status == nil) || (tc.status != nil && *r.Exec.ExitStatus != *tc.status) {
			t.Errorf("%s: code %q output %q exec %+v", tc.command, r.ErrorCode, r.Output, r.Exec)
		}
		if tc.code != "" && (!r.DeviceError || r.ErrorCategory != "device" || !r.External) {
			t.Errorf("%s: device %t category %q external %t", tc.command, r.DeviceError, r.ErrorCategory, r.External)
		}
		if !s.Usable() {
			t.Fatalf("%s left the session unusable", tc.command)
		}
	}
}

// TestExecTimeoutStopsTheCommand: the command timeout stops the command
// (the transport's KILL and close), hands back what settled, and leaves
// the connection serving the next command; a cancel stops it the same way
// with the context's error, and closes the connection.
func TestExecTimeoutStopsTheCommand(t *testing.T) {
	script := map[string]fakeRun{"slow": {stdout: "partial\n", hold: true}, "next": {stdout: "ok\n", status: code(0)}}
	s, f := execSession(script, ExecOptions{})
	r := s.Execute(context.Background(), platform.Command{Text: "slow", Timeout: 200 * time.Millisecond})
	if r.ErrorCode != "command_timeout" || r.ErrorCategory != "timeout" || string(r.Output) != "partial\n" || r.Exec == nil || r.Exec.ExitStatus != nil {
		t.Fatalf("timeout: code %q output %q exec %+v", r.ErrorCode, r.Output, r.Exec)
	}
	if !s.Usable() || len(f.stopped) != 1 {
		t.Fatalf("usable %t stopped %q", s.Usable(), f.stopped)
	}
	if r := s.Execute(context.Background(), platform.Command{Text: "next", Timeout: time.Second}); r.Err != nil || string(r.Output) != "ok\n" || !*r.ConnectionReused {
		t.Fatalf("the next command: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	if r := s.Execute(ctx, platform.Command{Text: "slow", Timeout: time.Minute}); !errors.Is(r.Err, context.Canceled) || r.ErrorCode != "" || string(r.Output) != "partial\n" || len(f.stopped) != 2 || s.Usable() || !f.closed {
		t.Fatalf("cancel: %v code %q output %q, stopped %q usable %t closed %t", r.Err, r.ErrorCode, r.Output, f.stopped, s.Usable(), f.closed)
	}
}

// timed runs Execute and reports how long it took, failing the test when
// it has not returned in 10 s: nothing else here ends an Execute a silent
// device holds.
func timed(t *testing.T, s *ExecSession, ctx context.Context, c platform.Command) (platform.Result, time.Duration) {
	t.Helper()
	done := make(chan platform.Result, 1)
	begun := time.Now()
	go func() { done <- s.Execute(ctx, c) }()
	select {
	case r := <-done:
		return r, time.Since(begun)
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: Execute has not returned in 10s", c.Text)
		return platform.Result{}, 0
	}
}

// TestExecStartIsBounded: a device that does not answer the channel's open
// holds Start until the connection is closed. The command's timeout and
// the context each end it by closing the connection: a timeout is
// command_timeout, its message saying the command had not started, a
// cancel the context's error, after execCutGrace; neither record carries
// exec fields, and the session's own Close does not wait. A channel the
// start returns after the close is stopped.
func TestExecStartIsBounded(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		s, f := execSession(nil, ExecOptions{})
		f.silentStart = true
		r, took := timed(t, s, context.Background(), platform.Command{Text: "uname -s", Timeout: 200 * time.Millisecond, TimeoutSource: platform.DeclaredTimeout})
		if r.ErrorCode != "command_timeout" || r.ErrorCategory != "timeout" || !r.Retryable || !r.External || fmt.Sprint(r.Err) != "command timed out after 200ms (--timeout) before it started; the connection was closed" {
			t.Fatalf("code %q category %q retryable %t external %t err %v", r.ErrorCode, r.ErrorCategory, r.Retryable, r.External, r.Err)
		}
		if r.Exec != nil || r.ConnectionReused == nil || *r.ConnectionReused || s.Usable() || !f.closed || took >= execEndGrace {
			t.Fatalf("exec %+v reused %v usable %t closed %t took %s", r.Exec, r.ConnectionReused, s.Usable(), f.closed, took)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("timeout, a channel after the close", func(t *testing.T) {
		t.Parallel()
		s, f := execSession(map[string]fakeRun{"slow": {hold: true}}, ExecOptions{})
		f.silentStart, f.startsAnyway = true, true
		r, _ := timed(t, s, context.Background(), platform.Command{Text: "slow", Timeout: 200 * time.Millisecond})
		if r.ErrorCode != "command_timeout" || r.Exec != nil || s.Usable() {
			t.Fatalf("code %q exec %+v usable %t", r.ErrorCode, r.Exec, s.Usable())
		}
		stopped := func() int {
			f.mu.Lock()
			defer f.mu.Unlock()
			return len(f.stopped)
		}
		for deadline := time.Now().Add(5 * time.Second); stopped() == 0 && time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
		}
		if stopped() != 1 {
			t.Fatal("the channel was not stopped")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		t.Parallel()
		s, f := execSession(nil, ExecOptions{})
		f.silentStart = true
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)
		r, took := timed(t, s, ctx, platform.Command{Text: "uname -s", Timeout: time.Minute})
		if !errors.Is(r.Err, context.Canceled) || r.ErrorCode != "" || r.Exec != nil || s.Usable() || !f.closed || took < execCutGrace || took >= execEndGrace {
			t.Fatalf("err %v code %q exec %+v usable %t closed %t took %s", r.Err, r.ErrorCode, r.Exec, s.Usable(), f.closed, took)
		}
	})
	// A start answered within the grace: the command the device took is
	// stopped before the connection is closed, and the record is a
	// command's, with its exec fields.
	t.Run("cancel, the start answered late", func(t *testing.T) {
		t.Parallel()
		s, f := execSession(map[string]fakeRun{"slow": {hold: true}}, ExecOptions{})
		f.startDelay = 200 * time.Millisecond
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)
		r, took := timed(t, s, ctx, platform.Command{Text: "slow", Timeout: time.Minute})
		if !errors.Is(r.Err, context.Canceled) || r.Exec == nil || len(f.stopped) != 1 || s.Usable() || !f.closed || took >= 100*time.Millisecond+execCutGrace {
			t.Fatalf("err %v exec %+v stopped %q usable %t closed %t took %s", r.Err, r.Exec, f.stopped, s.Usable(), f.closed, took)
		}
	})
}

// TestExecGivenUpOnASilentDevice: a command given up is stopped, and its
// channel's end is the device's to give. The context's end gives it
// execCutGrace and closes the connection, at the channel's end when that
// comes first; the command's timeout and the limit give it execEndGrace,
// keeping the connection when the device closes the channel and closing
// it, the message saying so, when it does not. A stop the device holds,
// and an exit status it never sends, are bounded the same way. What
// settled is kept in every case.
func TestExecGivenUpOnASilentDevice(t *testing.T) {
	const closedText = "; the device did not close the channel in 2s and the connection was closed"
	short := platform.Command{Timeout: 200 * time.Millisecond}
	long := platform.Command{Timeout: time.Minute}
	for _, tc := range []struct {
		name    string
		run     fakeRun
		command platform.Command
		cancel  bool
		limit   int64
		code    string
		text    string        // the message, a cancel's aside
		closed  bool          // the connection closed, the session unusable
		least   time.Duration // Execute takes this long at least,
		under   time.Duration // and less than this
	}{
		{name: "cancel", run: fakeRun{stdout: "partial\n", silent: true}, command: long, cancel: true, closed: true, least: execCutGrace, under: execEndGrace},
		{name: "cancel, the channel closed late", run: fakeRun{stdout: "partial\n", silent: true, closeAfter: 100 * time.Millisecond}, command: long, cancel: true, closed: true, under: execCutGrace},
		{name: "timeout", run: fakeRun{stdout: "partial\n", silent: true}, command: short, code: "command_timeout", text: "command timed out after 200ms" + closedText, closed: true, least: execEndGrace},
		{name: "timeout, the channel closed late", run: fakeRun{stdout: "partial\n", silent: true, closeAfter: 300 * time.Millisecond}, command: short, code: "command_timeout", text: "command timed out after 200ms; the command was stopped and its channel closed", under: execEndGrace},
		{name: "limit", run: fakeRun{stdout: "partial\n", silent: true}, command: long, limit: 4, code: "output_limit_exceeded", text: "command output exceeded 4 bytes (output.max-command-bytes) across stdout and stderr, 8 observed" + closedText, closed: true, least: execEndGrace},
		{name: "cancel, the stop held", run: fakeRun{stdout: "partial\n", silent: true, stopHangs: true}, command: long, cancel: true, closed: true, least: execCutGrace, under: execEndGrace},
		{name: "timeout, the stop held", run: fakeRun{stdout: "partial\n", silent: true, stopHangs: true}, command: short, code: "command_timeout", text: "command timed out after 200ms" + closedText, closed: true, least: execEndGrace},
		{name: "cancel, no exit status", run: fakeRun{stdout: "partial\n", waitHangs: true}, command: long, cancel: true, closed: true, least: execCutGrace, under: execEndGrace},
		{name: "timeout, no exit status", run: fakeRun{stdout: "partial\n", waitHangs: true}, command: short, code: "command_timeout", text: "command timed out after 200ms waiting for its exit status" + closedText, closed: true, least: execEndGrace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, f := execSession(map[string]fakeRun{"x": tc.run, "next": {stdout: "ok\n", status: code(0)}}, ExecOptions{MaxOutputBytes: tc.limit})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				time.AfterFunc(100*time.Millisecond, cancel)
			}
			command := tc.command
			command.Text = "x"
			r, took := timed(t, s, ctx, command)
			output := "partial\n"
			if tc.limit > 0 {
				output = output[:tc.limit]
			}
			if r.ErrorCode != tc.code || r.Exec == nil || string(r.Output) != output || len(f.stopped) != 1 {
				t.Fatalf("code %q exec %+v output %q stopped %q err %v", r.ErrorCode, r.Exec, r.Output, f.stopped, r.Err)
			}
			if tc.cancel && !errors.Is(r.Err, context.Canceled) || !tc.cancel && fmt.Sprint(r.Err) != tc.text {
				t.Fatalf("err %v", r.Err)
			}
			if s.Usable() == tc.closed || f.closed != tc.closed || took < tc.least || tc.under > 0 && took >= tc.under {
				t.Fatalf("usable %t closed %t took %s", s.Usable(), f.closed, took)
			}
			next := s.Execute(context.Background(), platform.Command{Text: "next", Timeout: time.Second})
			if tc.closed && next.ErrorCode != "command_session_lost" || !tc.closed && (next.Err != nil || string(next.Output) != "ok\n") {
				t.Fatalf("the next command: code %q output %q", next.ErrorCode, next.Output)
			}
		})
	}
}

// TestExecLimitCountsBothStreams: the limit bounds stdout and stderr
// together; the command is stopped, the first limit bytes kept between the
// two, and the connection serves the next command.
func TestExecLimitCountsBothStreams(t *testing.T) {
	script := map[string]fakeRun{"big": {stdout: strings.Repeat("o", 600), stderr: strings.Repeat("e", 600), status: code(0)}}
	s, f := execSession(script, ExecOptions{MaxOutputBytes: 1000})
	r := s.Execute(context.Background(), platform.Command{Text: "big", Timeout: 5 * time.Second})
	if r.ErrorCode != "output_limit_exceeded" || len(r.Output)+len(r.Exec.Stderr) != 1000 || len(f.stopped) != 1 || !s.Usable() {
		t.Fatalf("code %q stdout %d stderr %d stopped %q usable %t", r.ErrorCode, len(r.Output), len(r.Exec.Stderr), f.stopped, s.Usable())
	}
}

// TestExecCommandOwnBounds: a command's MaxBytes replaces the session's
// limit across both streams, larger or smaller, the message naming its
// source, and the connection serves the next command; a timeout's message
// names its source.
func TestExecCommandOwnBounds(t *testing.T) {
	script := map[string]fakeRun{"big": {stdout: strings.Repeat("o", 600), stderr: strings.Repeat("e", 600), status: code(0)}, "slow": {hold: true}}
	for _, tc := range []struct {
		maxBytes int64
		want     int
		code     string
		text     string
	}{
		{0, 1000, "output_limit_exceeded", "command output exceeded 1000 bytes (output.max-command-bytes) across stdout and stderr, "},
		{500, 500, "output_limit_exceeded", "command output exceeded 500 bytes (--maxbytes) across stdout and stderr, "},
		{2000, 1200, "", ""},
	} {
		s, _ := execSession(script, ExecOptions{MaxOutputBytes: 1000})
		r := s.Execute(context.Background(), platform.Command{Text: "big", Timeout: 5 * time.Second, MaxBytes: tc.maxBytes})
		if r.ErrorCode != tc.code || len(r.Output)+len(r.Exec.Stderr) != tc.want || !strings.HasPrefix(fmt.Sprint(r.Err), tc.text) || !s.Usable() {
			t.Errorf("maxbytes %d: code %q bytes %d err %v usable %t", tc.maxBytes, r.ErrorCode, len(r.Output)+len(r.Exec.Stderr), r.Err, s.Usable())
		}
	}
	s, _ := execSession(script, ExecOptions{})
	r := s.Execute(context.Background(), platform.Command{Text: "slow", Timeout: 200 * time.Millisecond, TimeoutSource: platform.DeclaredTimeout})
	if r.ErrorCode != "command_timeout" || fmt.Sprint(r.Err) != "command timed out after 200ms (--timeout); the command was stopped and its channel closed" {
		t.Fatalf("timeout: %q %v", r.ErrorCode, r.Err)
	}
}

// TestExecSpoolsEachStream: past the threshold each stream settles into a
// spool of its own, stdout's and stderr's named apart.
func TestExecSpoolsEachStream(t *testing.T) {
	dir := t.TempDir()
	script := map[string]fakeRun{"both": {stdout: strings.Repeat("o", 300), stderr: strings.Repeat("e", 200), status: code(0)}}
	s, _ := execSession(script, ExecOptions{Spool: Spool{Dir: dir, Threshold: 100, Activity: "act", Device: "srv1"}})
	r := s.Execute(context.Background(), platform.Command{Text: "both", Timeout: 5 * time.Second})
	if r.Err != nil || r.Spool == nil || r.Exec.StderrSpool == nil || r.Spool.Bytes != 300 || r.Exec.StderrSpool.Bytes != 200 {
		t.Fatalf("%v spool %+v stderr spool %+v", r.Err, r.Spool, r.Exec.StderrSpool)
	}
	if !strings.HasSuffix(r.Spool.Path, ".1."+strconv.Itoa(os.Getpid())+".spool") || !strings.HasSuffix(r.Exec.StderrSpool.Path, ".1."+strconv.Itoa(os.Getpid())+".stderr.spool") || filepath.Dir(r.Spool.Path) != dir {
		t.Fatalf("names %s %s", r.Spool.Path, r.Exec.StderrSpool.Path)
	}
	if data, _ := os.ReadFile(r.Exec.StderrSpool.Path); string(data) != strings.Repeat("e", 200) {
		t.Fatalf("stderr spool holds %q", data)
	}
}

// TestExecConnectionEnds: a channel that cannot start, and a connection
// lost under a command, end the session with the transport's code; nothing
// more is sent.
func TestExecConnectionEnds(t *testing.T) {
	s, f := execSession(nil, ExecOptions{})
	f.startErr = errorcodes.Errorf("ssh_session_channel_refused", "the device refused the session channel")
	r := s.Execute(context.Background(), platform.Command{Text: "uname -s", Timeout: time.Second})
	if r.ErrorCode != "ssh_session_channel_refused" || r.ErrorCategory != "connection" || r.Exec != nil || s.Usable() || !f.closed {
		t.Fatalf("refused: code %q category %q exec %+v usable %t", r.ErrorCode, r.ErrorCategory, r.Exec, s.Usable())
	}
	// The system transport learns of a refused exec request at the end:
	// the record is the refused start's, no exec fields.
	s, _ = execSession(map[string]fakeRun{"x": {waitErr: errorcodes.Errorf("ssh_session_channel_refused", "the device refused the exec request")}}, ExecOptions{})
	r = s.Execute(context.Background(), platform.Command{Text: "x", Timeout: time.Second})
	if r.ErrorCode != "ssh_session_channel_refused" || r.ErrorCategory != "connection" || r.Exec != nil || s.Usable() {
		t.Fatalf("refused at the end: code %q category %q exec %+v usable %t", r.ErrorCode, r.ErrorCategory, r.Exec, s.Usable())
	}
	s, _ = execSession(map[string]fakeRun{"x": {stdout: "half", waitErr: errorcodes.Errorf("session_keepalive_timeout", "no answer")}}, ExecOptions{})
	r = s.Execute(context.Background(), platform.Command{Text: "x", Timeout: time.Second})
	if r.ErrorCode != "session_keepalive_timeout" || string(r.Output) != "half" || s.Usable() {
		t.Fatalf("lost: code %q output %q usable %t", r.ErrorCode, r.Output, s.Usable())
	}
	if r := s.Execute(context.Background(), platform.Command{Text: "x", Timeout: time.Second}); r.ErrorCode != "command_session_lost" {
		t.Fatalf("after the loss: %q", r.ErrorCode)
	}
}
