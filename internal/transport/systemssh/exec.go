package systemssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/askpass"
	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/platform"
)

// The exec channel on the system transport. An exec device's connection is
// an OpenSSH ControlMaster (ssh -M -N), karvi's own child with the managed
// configuration, started with a death signal; each command is an ssh -S
// client of it, at LogLevel QUIET and with ProxyCommand false, so that it
// reaches the device only through the master (a client that finds no
// master would otherwise connect and authenticate by itself) and its stderr
// is the command's alone. The master runs at LogLevel DEBUG1 with no -E
// file: karvi reads its stderr line by line (masterLines) for the method
// that authenticated, each command's exit-status or exit-signal, and a
// refused exec request, since a client's exit of 255 is the command's own
// 255, a signal, a channel closed without a status, or the connection's end
// alike. OpenSSH's client cannot send a signal request, so a command given
// up is left running on the device (remote_command_not_stopped).

// masterLines stands between the master's stderr and the diagnostics: it
// takes the lines the exec channel reads and the line saying the host's key
// was stored (to enrolled), drops OpenSSH's other debug lines, and passes
// every other line on whole, so the diagnostics classify
// a failure as the shell's do. A line is held until its newline.
type masterLines struct {
	next     io.Writer
	enrolled func(label string) // the request's host_key_enrolled notice, or nil (Driver.hostKeyEnrolled)

	mu      sync.Mutex
	pending []byte
	method  string
	command *commandLines // the command in flight, nil before the first
	ended   bool          // the master's stderr reached its end
	changed chan struct{} // closed and replaced at every change
}

// commandLines is what the master wrote of one command: its session
// channel's number, then the channel's requests and its end.
type commandLines struct {
	channel                        string // "" until the session's channel is made
	status, signal, refused, freed bool
}

var (
	newSessionLine  = regexp.MustCompile(`^debug1: channel (\d+): new session \[client-session\]`)
	channelReqLine  = regexp.MustCompile(`^debug1: client_input_channel_req: channel (\d+) rtype (exit-status|exit-signal) `)
	freeSessionLine = regexp.MustCompile(`^debug1: channel (\d+): free: client-session,`)
	// refusedLine is OpenSSH's one sign at DEBUG1 of an exec request the
	// device refused: the channel is closed at once and its read side
	// fails a second time. The "exec request failed" text is queued to the
	// client and lost with the channel.
	refusedLine = regexp.MustCompile(`^channel (\d+): chan_read_failed for istate 3$`)
	debugLine   = regexp.MustCompile(`^debug\d: `)
)

func newMasterLines(next io.Writer) *masterLines {
	return &masterLines{next: next, changed: make(chan struct{})}
}

func (m *masterLines) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = append(m.pending, p...)
	for {
		i := bytes.IndexByte(m.pending, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := m.pending[:i+1]
		m.pending = m.pending[i+1:]
		if err := m.take(line); err != nil {
			return len(p), err
		}
	}
}

// take reads one line; the caller holds mu.
func (m *masterLines) take(line []byte) error {
	text := string(bytes.TrimRight(line, "\r\n"))
	if g := authenticatedLine.FindStringSubmatch(text); g != nil {
		m.method = g[1]
		m.notify()
		return nil
	}
	if label, ok := enrolledLabel(text); ok {
		if m.enrolled != nil {
			m.enrolled(label)
		}
		return nil
	}
	c := m.command
	if g := newSessionLine.FindStringSubmatch(text); g != nil {
		if c != nil && c.channel == "" {
			c.channel = g[1]
		}
		return nil
	}
	if g := channelReqLine.FindStringSubmatch(text); g != nil {
		if c != nil && c.channel == g[1] {
			c.status, c.signal = g[2] == "exit-status", g[2] == "exit-signal"
		}
		return nil
	}
	if g := freeSessionLine.FindStringSubmatch(text); g != nil {
		if c != nil && c.channel == g[1] {
			c.freed = true
			m.notify()
		}
		return nil
	}
	if g := refusedLine.FindStringSubmatch(text); g != nil {
		if c != nil && c.channel == g[1] {
			c.refused = true
		}
		return nil
	}
	if debugLine.MatchString(text) {
		return nil
	}
	_, err := m.next.Write(line)
	return err
}

// notify wakes the waiters; the caller holds mu.
func (m *masterLines) notify() {
	close(m.changed)
	m.changed = make(chan struct{})
}

// end passes on a last line without a newline and marks the stream's end.
func (m *masterLines) end() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pending) > 0 {
		_ = m.take(m.pending)
		m.pending = nil
	}
	m.ended = true
	m.notify()
}

// Method is the method OpenSSH said authenticated, "" before it said so.
func (m *masterLines) Method() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.method
}

// begin starts a command's lines: the first session channel made after it
// is the command's. Commands run one at a time; a client killed at a
// timeout made its channel long before, and a channel not yet freed when
// the next is made has another number.
func (m *masterLines) begin() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.command = &commandLines{}
}

// settle waits until the command's session channel is freed, the master's
// stderr ends, or the bound passes, and returns what was read. The master
// writes a channel's requests and its end before the client it serves
// exits, so the wait is for this reader alone.
func (m *masterLines) settle(bound time.Duration) commandLines {
	deadline := time.NewTimer(bound)
	defer deadline.Stop()
	for {
		m.mu.Lock()
		c, done, changed := commandLines{}, m.ended, m.changed
		if m.command != nil {
			c = *m.command
		}
		m.mu.Unlock()
		if c.freed || done {
			return c
		}
		select {
		case <-changed:
		case <-deadline.C:
			return c
		}
	}
}

// settleWait bounds settle.
const settleWait = 5 * time.Second

// execMaster is one exec device's ControlMaster.
type execMaster struct {
	binary, socket, address string
	env                     []string
	cmd                     *exec.Cmd
	lines                   *masterLines
	diagnostics             *synchronizedBuffer
	failures                sessionFailure
	done                    chan struct{}

	mu      sync.Mutex
	waitErr error

	closeOnce sync.Once
}

// startMaster starts the device's master and returns it with the one-use
// askpass broker that serves its authentication; the caller closes the
// broker once the master is ready. The master is started tied to karvi
// (osutil.StartTied): its death signal is SIGTERM, on which OpenSSH removes
// its socket.
func (d *Driver) startMaster() (*execMaster, *askpass.Broker, error) {
	if err := osutil.CheckControlPathRoot(d.f.ControlRoot); err != nil {
		return nil, nil, err
	}
	name, err := osutil.NewControlSocketName()
	if err != nil {
		return nil, nil, errorcodes.Errorf("command_session_start_failed", "name the control socket: %v", err)
	}
	material := callbackMaterial{username: d.req.Username, password: d.req.Password, enable: d.req.EnablePassword}
	broker, err := askpass.Start(d.f.ScratchDir, material)
	if err != nil {
		return nil, nil, fmt.Errorf("askpass_start_failed: %w", err)
	}
	askPath, err := findAskpass(d.f.AskpassPath)
	if err != nil {
		broker.Close()
		return nil, nil, fmt.Errorf("dependency_askpass_unavailable: %w", err)
	}
	m := &execMaster{binary: d.binary, socket: filepath.Join(d.f.ControlRoot, name), address: d.req.Address, env: childEnvironment(d.f.Config),
		diagnostics: &synchronizedBuffer{maxBytes: 64 << 10}, failures: d.sessionFailure(), done: make(chan struct{})}
	m.lines = newMasterLines(m.diagnostics)
	m.lines.enrolled = d.hostKeyEnrolled()
	// The command line's control options come first and win over the
	// generated file's ControlMaster no and LogLevel ERROR.
	args := append(d.hostArgs(),
		"-o", "ControlMaster=yes",
		"-o", "ControlPath="+m.socket,
		"-o", "ControlPersist=no",
		"-o", "LogLevel=DEBUG1",
		"-o", "BatchMode=no",
		"-N", "-n",
		d.req.Address,
	)
	m.cmd = exec.Command(d.binary, args...)
	m.cmd.Env = childEnvironment(d.f.Config, append(broker.Environment(),
		"SSH_ASKPASS="+askPath,
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=karvi:0",
	)...)
	stderr, err := m.cmd.StderrPipe()
	if err != nil {
		broker.Close()
		return nil, nil, fmt.Errorf("command_session_stderr_pipe_failed: stderr pipe: %w", err)
	}
	d.debugf("system SSH exec master starting binary=%q target=%q address=%q port=%d host_key_policy=%s socket=%s", d.binary, d.req.Metadata["canonical_name"], d.req.Address, d.req.Port, d.f.hostKey.Mode, m.socket)
	// Wait closes the stderr pipe, so it runs once the last line is read.
	waited, err := osutil.StartTied(m.cmd, func() {
		_, _ = io.Copy(m.lines, stderr)
		m.lines.end()
	})
	if err != nil {
		broker.Close()
		return nil, nil, fmt.Errorf("command_session_start_failed: start OpenSSH: %w", err)
	}
	go func() {
		err := <-waited
		m.mu.Lock()
		m.waitErr = err
		m.mu.Unlock()
		close(m.done)
	}()
	return m, broker, nil
}

// control runs ssh -O OPERATION against the master's socket.
func (m *execMaster) control(operation string) error {
	cmd := exec.Command(m.binary, "-F", "none", "-o", "LogLevel=QUIET", "-S", m.socket, "-O", operation, m.address)
	cmd.Env = m.env
	return cmd.Run()
}

// ready waits for the master to answer -O check within bound: the
// connection has authenticated. A master that ends first is the session's
// failure under its diagnostics' code.
func (m *execMaster) ready(ctx context.Context, bound time.Duration) error {
	deadline := time.NewTimer(bound)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Lstat(m.socket); err == nil && m.control("check") == nil {
			return nil
		}
		select {
		case <-m.done:
			return m.failure(false)
		case <-ctx.Done():
			m.kill()
			return ctx.Err()
		case <-deadline.C:
			m.kill()
			return errorcodes.Errorf("command_session_prompt_timeout", "the connection was not ready after %s: the OpenSSH master did not answer -O check", bound)
		case <-tick.C:
		}
	}
}

// answers says whether the master is running and answers -O check.
func (m *execMaster) answers() bool {
	select {
	case <-m.done:
		return false
	default:
	}
	return m.control("check") == nil
}

// failure is the master's end under its diagnostics' code; lost says the
// session was ready, an end the diagnostics do not name being
// command_session_lost.
func (m *execMaster) failure(lost bool) error {
	select {
	case <-m.done:
	case <-time.After(2 * time.Second):
	}
	m.mu.Lock()
	cause := m.waitErr
	m.mu.Unlock()
	if cause == nil {
		cause = errorcodes.Errorf("command_session_lost", "the OpenSSH master ended")
	}
	err := m.failures.classify(m.diagnostics.String(), cause)
	if lost && errorcodes.Of(err) == "ssh_process_failed" {
		return errorcodes.Errorf("command_session_lost", "the connection ended: %s", safeDiagnostic(m.diagnostics.String(), cause))
	}
	return err
}

// kill ends the master: SIGTERM, on which OpenSSH removes its socket, then
// SIGKILL if it lingers.
func (m *execMaster) kill() {
	if m.cmd.Process == nil {
		return
	}
	_ = m.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-m.done:
		return
	case <-time.After(2 * time.Second):
	}
	_ = m.cmd.Process.Kill()
	select {
	case <-m.done:
	case <-time.After(500 * time.Millisecond):
	}
}

// close asks the master to exit (ssh -O exit) and kills it if it lingers.
func (m *execMaster) close() {
	m.closeOnce.Do(func() {
		select {
		case <-m.done:
			return
		default:
		}
		if m.control("exit") == nil {
			select {
			case <-m.done:
				return
			case <-time.After(2 * time.Second):
			}
		}
		m.kill()
	})
}

// execConn is the master as devsession's exec connection.
type execConn struct {
	m *execMaster
}

var _ devsession.ExecConn = (*execConn)(nil)

// Start runs the command in a client of the master.
func (c *execConn) Start(command string) (devsession.ExecChannel, error) {
	select {
	case <-c.m.done:
		return nil, c.m.failure(true)
	default:
	}
	c.m.lines.begin()
	cmd := exec.Command(c.m.binary, "-F", "none", "-S", c.m.socket,
		"-o", "ControlMaster=no",
		"-o", "ProxyCommand=false",
		"-o", "LogLevel=QUIET",
		"-o", "BatchMode=yes",
		"-n", "-T", c.m.address, command)
	cmd.Env = c.m.env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("command_session_stdout_pipe_failed: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("command_session_stderr_pipe_failed: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("command_session_start_failed: start OpenSSH: %w", err)
	}
	return &execClient{m: c.m, cmd: cmd, stdout: stdout, stderr: stderr}, nil
}

// Close ends the master.
func (c *execConn) Close() error {
	c.m.close()
	return nil
}

// execClient is one command in its client of the master.
type execClient struct {
	m              *execMaster
	cmd            *exec.Cmd
	stdout, stderr io.Reader

	waitOnce sync.Once
	waitErr  error
}

func (x *execClient) Stdout() io.Reader { return x.stdout }
func (x *execClient) Stderr() io.Reader { return x.stderr }

// exited waits for the client once, for Wait and Stop alike.
func (x *execClient) exited() error {
	x.waitOnce.Do(func() { x.waitErr = x.cmd.Wait() })
	return x.waitErr
}

// Wait is the client's exit, the command's status, but for 255, which the
// master's lines decide: exit-status is the command's own 255, exit-signal
// a signal OpenSSH does not name; with neither, a refused exec request ends
// the session (ssh_session_channel_refused), a master that answers -O
// check had a channel closed without a status, and a master gone is the
// connection's end.
func (x *execClient) Wait() (*int, string, error) {
	status := 0
	if err := x.exited(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, "", errorcodes.Errorf("command_session_lost", "wait for the OpenSSH client: %v", err)
		}
		status = exit.ExitCode() // -1 when a signal ended the client
	}
	if status >= 0 && status != 255 {
		return &status, "", nil
	}
	lines := x.m.lines.settle(settleWait)
	switch {
	case lines.status:
		status = 255
		return &status, "", nil
	case lines.signal:
		return nil, "unnamed", nil
	case lines.refused:
		return nil, "", errorcodes.Errorf("ssh_session_channel_refused", "the device refused the exec request")
	case x.m.answers():
		return nil, "", nil
	}
	return nil, "", x.m.failure(true)
}

// Stop kills the client, which closes the command's channel; OpenSSH's
// client cannot ask the device to end the command, so it may be left
// running.
func (x *execClient) Stop() []platform.Notice {
	if x.cmd.Process != nil {
		_ = x.cmd.Process.Kill()
	}
	_ = x.exited()
	return []platform.Notice{{Code: "remote_command_not_stopped", Message: "the command's channel was closed and the command may still be running on the device: OpenSSH's client cannot ask the device to end it"}}
}
