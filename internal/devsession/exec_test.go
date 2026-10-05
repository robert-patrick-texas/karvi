package devsession

import (
	"context"
	"errors"
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
}

// fakeRun is what one command does: its two streams, then its end. Hold
// keeps both streams open until Stop.
type fakeRun struct {
	stdout, stderr string
	status         *int
	signal         string
	waitErr        error
	hold           bool
}

func code(n int) *int { return &n }

func (f *fakeExec) Start(command string) (ExecChannel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.started = append(f.started, command)
	run := f.script[command]
	ch := &fakeChannel{f: f, command: command, run: run}
	if run.hold {
		var w1, w2 *io.PipeWriter
		ch.stdout, w1 = io.Pipe()
		ch.stderr, w2 = io.Pipe()
		go func() { _, _ = io.WriteString(w1, run.stdout) }()
		go func() { _, _ = io.WriteString(w2, run.stderr) }()
		ch.stop = func() { w1.Close(); w2.Close() }
	} else {
		ch.stdout, ch.stderr = strings.NewReader(run.stdout), strings.NewReader(run.stderr)
	}
	return ch, nil
}

func (f *fakeExec) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
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

func (c *fakeChannel) Stdout() io.Reader { return c.stdout }
func (c *fakeChannel) Stderr() io.Reader { return c.stderr }
func (c *fakeChannel) Wait() (*int, string, error) {
	return c.run.status, c.run.signal, c.run.waitErr
}
func (c *fakeChannel) Stop() []platform.Notice {
	c.f.mu.Lock()
	c.f.stopped = append(c.f.stopped, c.command)
	c.f.mu.Unlock()
	if c.stop != nil {
		c.stop()
	}
	return nil
}

func execSession(script map[string]fakeRun, opts ExecOptions) (*ExecSession, *fakeExec) {
	f := &fakeExec{script: script}
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
// with the context's error.
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
	if r := s.Execute(ctx, platform.Command{Text: "slow", Timeout: time.Minute}); !errors.Is(r.Err, context.Canceled) || len(f.stopped) != 2 {
		t.Fatalf("cancel: %v, stopped %q", r.Err, f.stopped)
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
