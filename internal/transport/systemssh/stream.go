package systemssh

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
)

// processStream is one interactive OpenSSH process as a devsession.Stream:
// its stdout is the device's output, its stdin the device's input, its
// stderr the bounded diagnostics that classify a failure. Cisco IOS XE and
// several other network SSH servers permit only one session channel per
// SSH transport, so this one shell is the device's whole session and no
// exec channel is ever requested.
type processStream struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *synchronizedBuffer
	// auth holds the method OpenSSH said authenticated.
	auth   *authFilter
	done   chan struct{}
	cancel func()
	// typesNotOffered, set when the offered host-key algorithms were
	// filtered to the enrolled key types, reports a device offering none of
	// them as the changed key it is.
	typesNotOffered func(offered []string) error
	// offered are the algorithm lists the generated configuration offers,
	// for a failed negotiation's message.
	offered sshalgorithms.Lists
	// aliveInterval and aliveCountMax are the generated configuration's
	// ServerAliveInterval and ServerAliveCountMax, for the keepalive
	// timeout's message.
	aliveInterval time.Duration
	aliveCountMax int

	stateMu sync.Mutex
	waitErr error

	closeOnce sync.Once
}

// Read returns the device's output. When the process has ended, the error
// carries the code OpenSSH's diagnostics classify to (host_key_changed,
// authentication_failed, connection_refused, ...).
func (p *processStream) Read(b []byte) (int, error) {
	n, err := p.stdout.Read(b)
	if err == nil {
		return n, nil
	}
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
	}
	p.stateMu.Lock()
	waitErr := p.waitErr
	p.stateMu.Unlock()
	if waitErr == nil && errors.Is(err, io.EOF) {
		// The remote logout OpenSSH reports as a clean exit.
		return n, io.EOF
	}
	cause := waitErr
	if cause == nil {
		cause = err
	}
	diagnostic := p.stderr.String()
	if p.typesNotOffered != nil {
		if offered, ok := hostKeyTypesOffered(diagnostic); ok {
			return n, p.typesNotOffered(offered)
		}
	}
	if _, failure, ok := negotiationFailure(diagnostic, p.offered); ok {
		return n, failure
	}
	code, _, _, _ := classify(diagnostic, cause)
	if code == "session_keepalive_timeout" {
		return n, devsession.KeepaliveTimeout(p.aliveCountMax, p.aliveInterval, "OpenSSH: "+safeDiagnostic(diagnostic, cause))
	}
	return n, errorcodes.Errorf(code, "%s", safeDiagnostic(diagnostic, cause))
}

func (p *processStream) Write(b []byte) (int, error) { return p.stdin.Write(b) }

// Close ends the process: stdin is closed so a remote logout the session
// already sent completes, then the process is killed if it lingers.
func (p *processStream) Close() error {
	p.closeOnce.Do(func() {
		_ = p.stdin.Close()
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			p.terminate()
			select {
			case <-p.done:
			case <-time.After(500 * time.Millisecond):
			}
		}
		if p.cancel != nil {
			p.cancel()
		}
	})
	return nil
}

// Abort kills the process at once: the session it served has failed.
func (p *processStream) Abort() {
	p.closeOnce.Do(func() {
		p.terminate()
		select {
		case <-p.done:
		case <-time.After(500 * time.Millisecond):
		}
		_ = p.stdin.Close()
	})
}

func (p *processStream) terminate() {
	if p.cancel != nil {
		p.cancel()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

// Diagnostics is what OpenSSH wrote to stderr so far.
func (p *processStream) Diagnostics() string { return p.stderr.String() }

// hostKeyTypesOffered reads OpenSSH's "no matching host key type found.
// Their offer: A,B" diagnostic.
func hostKeyTypesOffered(diagnostic string) ([]string, bool) {
	const marker = "no matching host key type found. Their offer: "
	i := strings.Index(diagnostic, marker)
	if i < 0 {
		return nil, false
	}
	rest := diagnostic[i+len(marker):]
	if j := strings.IndexAny(rest, "\r\n"); j >= 0 {
		rest = rest[:j]
	}
	return strings.Split(strings.TrimSpace(rest), ","), true
}
