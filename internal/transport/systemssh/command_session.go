package systemssh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/askpass"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
)

// synchronizedBuffer collects bounded OpenSSH diagnostics from stderr while
// stdout is consumed by the prompt-oriented command session: the last
// maxBytes, since OpenSSH's failure is its last line and a device's banner
// before it can be any size. It is safe for a debug/error path to inspect
// while the copy goroutine is still active.
type synchronizedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	maxBytes  int
	truncated bool
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.maxBytes <= 0 {
		b.maxBytes = 64 << 10
	}
	n := len(p)
	if n >= b.maxBytes {
		b.buf.Reset()
		p = p[n-b.maxBytes:]
		b.truncated = true
	} else if over := b.buf.Len() + n - b.maxBytes; over > 0 {
		b.buf.Next(over)
		b.truncated = true
	}
	_, _ = b.buf.Write(p)
	// Report the full input length because intentionally truncating diagnostic
	// retention must not make io.Copy treat stderr handling as a write failure.
	return n, nil
}

// reset drops what was written so far.
func (b *synchronizedBuffer) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
	b.truncated = false
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	value := b.buf.String()
	if b.truncated {
		// The first line kept may be cut; it is dropped whole.
		if i := strings.IndexByte(value, '\n'); i >= 0 {
			value = value[i+1:]
		}
		value = "[karvi: OpenSSH diagnostic truncated]\n" + value
	}
	return value
}

// startShell starts one fresh interactive OpenSSH process for the device
// session and returns it as a stream, with the askpass broker that serves
// its authentication; the caller closes the broker once the first prompt has
// arrived.
func (d *Driver) startShell(ctx context.Context) (*processStream, *askpass.Broker, error) {
	material := callbackMaterial{username: d.req.Username, password: d.req.Password}
	end := newProcessEnd()
	broker, err := askpass.Start(d.f.ScratchDir, material, askpass.Hooks{End: end.end})
	if err != nil {
		return nil, nil, fmt.Errorf("askpass_start_failed: %w", err)
	}
	askPath, err := findAskpass(d.f.AskpassPath)
	if err != nil {
		broker.Close()
		return nil, nil, fmt.Errorf("dependency_askpass_unavailable: %w", err)
	}

	// baseArgs disables ControlMaster. A reused master may be unable to open
	// another channel on Cisco IOS XE, yielding "Master refused session
	// request: Permission denied".
	args := d.baseArgs()
	// VERBOSE, over the generated file's ERROR, so a failed algorithm
	// negotiation ("Unable to negotiate ...") reaches the diagnostics and
	// the line naming the method that authenticated reaches authFilter;
	// login keeps ERROR for the operator's terminal.
	args = append(args,
		"-o", "LogLevel=VERBOSE",
		"-o", "BatchMode=no",
		"-tt",
		"--", d.req.Address,
	)

	sessionCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(sessionCtx, d.binary, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		broker.Close()
		return nil, nil, fmt.Errorf("command_session_stdin_pipe_failed: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		broker.Close()
		return nil, nil, fmt.Errorf("command_session_stdout_pipe_failed: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		broker.Close()
		return nil, nil, fmt.Errorf("command_session_stderr_pipe_failed: stderr pipe: %w", err)
	}
	cmd.Env = childEnvironment(d.f.Config, append(broker.Environment(),
		"SSH_ASKPASS="+askPath,
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=karvi:0",
	)...)

	// Read before OpenSSH starts, which may store the key.
	enrolled := d.hostKeyEnrolled()
	d.debugf("system SSH command session starting binary=%q target=%q address=%q port=%d host_key_policy=%s control_path=disabled", d.binary, d.req.Metadata["canonical_name"], d.req.Address, d.req.Port, d.f.hostKey.Mode)
	stderrCopied := make(chan struct{})
	// Wait closes the stderr pipe, so it runs only after the copy has read
	// OpenSSH's last diagnostic line; otherwise a failure that ends the
	// process at once (a failed algorithm negotiation) can be classified from
	// an empty buffer.
	waited, err := osutil.StartTied(cmd, func() { <-stderrCopied })
	if err != nil {
		cancel()
		_ = stdin.Close()
		broker.Close()
		return nil, nil, fmt.Errorf("command_session_start_failed: start OpenSSH: %w", err)
	}
	end.started(cmd.Process)

	diagnostics := &synchronizedBuffer{maxBytes: 64 << 10}
	auth := &authFilter{next: diagnostics, enrolled: enrolled}
	done := make(chan struct{})
	stream := &processStream{cmd: cmd, stdin: stdin, stdout: stdout, stderr: diagnostics, auth: auth, done: done, cancel: cancel, failures: d.sessionFailure(), jobEnded: ctx.Done()}
	go func() {
		_, _ = io.Copy(auth, stderr)
		auth.Flush()
		close(stderrCopied)
	}()
	go func() {
		err := <-waited
		stream.stateMu.Lock()
		stream.waitErr = err
		stream.stateMu.Unlock()
		close(done)
	}()
	return stream, broker, nil
}

// sessionFailure is what this device's OpenSSH diagnostics are classified
// against.
func (d *Driver) sessionFailure() sessionFailure {
	f := sessionFailure{offered: d.f.offered,
		effective: func() sshalgorithms.Lists {
			return d.f.Effective.get(d.binary, func() sshalgorithms.Lists {
				return effectiveAlgorithms(d.f.Config, d.binary, append(d.hostArgs(), "--", d.req.Address))
			})
		},
		aliveInterval: time.Duration(ceilSeconds(d.f.Config.Duration("ssh.server-alive-interval"))) * time.Second, aliveCountMax: d.f.Config.Int("ssh.server-alive-count-max")}
	if policy := d.f.hostKey; policy.Mode != hostkey.Insecure {
		if types, err := hostkey.EnrolledTypes(policy.KnownHostsFile, d.f.hostKeyIdentity); err == nil && len(types) > 0 {
			identity := d.f.hostKeyIdentity
			f.typesNotOffered = func(offered []string) error { return hostkey.TypesNotOffered(policy, identity, offered) }
		}
	}
	return f
}

func compactDiagnostic(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 512 {
		value = value[:512] + "..."
	}
	return value
}
