package scrapligov1

import (
	"context"
	"errors"
	"io"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
)

// ExecConnection is karvi's connection to an exec device: the handshake,
// the host-key policy, and authentication as Dial's, the keepalives once
// authenticated, and a session channel per command, each running one exec
// request with no PTY and standard input at its end.
type ExecConnection struct {
	c *connection
	// close is the transport wrapper's close, which closes the connection.
	close func() error
}

var (
	_ devsession.ExecConn   = (*ExecConnection)(nil)
	_ platform.AuthReporter = (*ExecConnection)(nil)
)

// DialExec opens the connection and returns once it has authenticated.
func DialExec(ctx context.Context, req DialRequest) (*ExecConnection, error) {
	req.exec = true
	t, c, err := open(ctx, req)
	if err != nil {
		return nil, err
	}
	return &ExecConnection{c: c, close: func() error { return t.Close(true) }}, nil
}

// AuthMethod is the method that authenticated the connection.
func (e *ExecConnection) AuthMethod() string { return e.c.AuthMethod() }

// Close closes the connection.
func (e *ExecConnection) Close() error { return e.close() }

// Start opens a session channel and runs the command on it. A device that
// refuses the channel or the exec request on a live connection is
// ssh_session_channel_refused; a connection gone is the keepalive's code
// or command_session_lost.
func (e *ExecConnection) Start(command string) (devsession.ExecChannel, error) {
	e.c.mu.Lock()
	client, lost := e.c.client, e.c.lost
	e.c.mu.Unlock()
	if lost != nil {
		return nil, lost
	}
	if client == nil {
		return nil, errorcodes.Errorf("command_session_lost", "the connection is closed")
	}
	session, err := client.NewSession()
	if err != nil {
		var refused *ssh.OpenChannelError
		if errors.As(err, &refused) {
			return nil, errorcodes.Errorf("ssh_session_channel_refused", "the device refused the session channel: %v", err)
		}
		return nil, e.lost(err)
	}
	stdout, err := session.StdoutPipe()
	if err == nil {
		var stderr io.Reader
		if stderr, err = session.StderrPipe(); err == nil {
			if err = session.Start(command); err == nil {
				return &execChannel{e: e, session: session, stdout: noting{stdout, e.c}, stderr: noting{stderr, e.c}}, nil
			}
		}
	}
	_ = session.Close()
	if e.alive() {
		return nil, errorcodes.Errorf("ssh_session_channel_refused", "the device refused the exec request: %v", err)
	}
	return nil, e.lost(err)
}

// lost is the connection's end: the keepalive's code when it ended it,
// else command_session_lost.
func (e *ExecConnection) lost(err error) error {
	if lost := e.c.lostError(); lost != nil {
		return lost
	}
	return errorcodes.Errorf("command_session_lost", "the connection ended: %v", err)
}

// aliveWait bounds the question alive asks.
const aliveWait = 5 * time.Second

// alive asks the device whether the connection is up: the keepalive
// request, whose refusal is an answer. A channel that closed without a
// status reads the same as one a lost connection closed; this tells them
// apart.
func (e *ExecConnection) alive() bool {
	e.c.mu.Lock()
	client, lost := e.c.client, e.c.lost
	e.c.mu.Unlock()
	if client == nil || lost != nil {
		return false
	}
	answered := make(chan error, 1)
	go func() {
		_, _, err := client.SendRequest(keepaliveRequest, true, nil)
		answered <- err
	}()
	select {
	case err := <-answered:
		return err == nil
	case <-time.After(aliveWait):
		return false
	}
}

// noting notes output's arrival for the keepalive, as the shell's reads do.
type noting struct {
	r io.Reader
	c *connection
}

func (n noting) Read(b []byte) (int, error) {
	k, err := n.r.Read(b)
	if k > 0 {
		n.c.lastRead.Store(time.Now().UnixNano())
	}
	return k, err
}

// execChannel is one command on its session channel.
type execChannel struct {
	e              *ExecConnection
	session        *ssh.Session
	stdout, stderr io.Reader
}

func (x *execChannel) Stdout() io.Reader { return x.stdout }
func (x *execChannel) Stderr() io.Reader { return x.stderr }

// Wait is x/crypto's account of the end: an exit status, or a signal's
// name (whose status x/crypto makes up, 128 and the number, and which is
// not kept), or neither on a live connection.
func (x *execChannel) Wait() (*int, string, error) {
	err := x.session.Wait()
	var exit *ssh.ExitError
	var missing *ssh.ExitMissingError
	switch {
	case err == nil:
		zero := 0
		return &zero, "", nil
	case errors.As(err, &exit):
		if signal := exit.Signal(); signal != "" {
			return nil, signal, nil
		}
		status := exit.ExitStatus()
		return &status, "", nil
	case errors.As(err, &missing) && x.e.alive():
		return nil, "", nil
	}
	return nil, "", x.e.lost(err)
}

// Stop asks the device to end the command with KILL and closes the channel:
// closing alone leaves the command running.
func (x *execChannel) Stop() []platform.Notice {
	_ = x.session.Signal(ssh.SIGKILL)
	_ = x.session.Close()
	return nil
}
