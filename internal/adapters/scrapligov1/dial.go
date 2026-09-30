package scrapligov1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	scraplioptions "github.com/scrapli/scrapligo/driver/options"
	scraplilogging "github.com/scrapli/scrapligo/logging"
	scraplitransport "github.com/scrapli/scrapligo/transport"
	"golang.org/x/crypto/ssh"

	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
)

// DialRequest is one scrapligo-v1 connection to a device.
type DialRequest struct {
	// Host is the canonical name the device's host-key identity is formed
	// from with Port.
	Host     string
	Address  string
	Port     int
	Username string
	// Password delivers the password to the callback at authentication
	// time; nil when the credential has none.
	Password func(func([]byte) error) error
	Policy   hostkey.Policy
	// ConnectTimeout bounds the TCP dial; HandshakeTimeout the SSH
	// handshake, authentication, and the PTY and shell requests.
	ConnectTimeout, HandshakeTimeout time.Duration
	// KeepaliveInterval, when positive, sends an SSH keepalive request on
	// the interval once the shell has started; after KeepaliveCountMax
	// consecutive unanswered ones the connection is closed and the stream
	// ends with session_keepalive_timeout.
	KeepaliveInterval time.Duration
	KeepaliveCountMax int
	// Term is the PTY's terminal type ("" sends none, as ssh -tt does
	// without TERM).
	Term string
	// Algorithms are the device's SSH algorithm lists; empty means the
	// defaults.
	Algorithms  sshalgorithms.Lists
	Warn, Debug func(string)
}

// Dial opens karvi's connection through scrapligo's transport wrapper and
// returns the device shell as a devsession.Stream: one TCP connection, the
// host-key policy in the handshake, a PTY, and a shell. scrapligo's
// channel and network driver are not used.
func Dial(ctx context.Context, req DialRequest) (devsession.Stream, error) {
	if req.Port == 0 {
		req.Port = 22
	}
	if req.Host == "" {
		req.Host = req.Address
	}
	if req.ConnectTimeout <= 0 {
		req.ConnectTimeout = 10 * time.Second
	}
	if req.HandshakeTimeout <= 0 {
		req.HandshakeTimeout = 10 * time.Second
	}
	if req.KeepaliveCountMax <= 0 {
		req.KeepaliveCountMax = 3
	}
	c := &connection{ctx: ctx, req: req, closed: make(chan struct{})}
	logger, err := scraplilogging.NewInstance()
	if err != nil {
		return nil, errorcodes.Errorf("native_session_open_failed", "create the scrapligo logger: %v", err)
	}
	t, err := scraplitransport.NewTransport(logger, req.Address, "",
		scraplioptions.WithCustomTransport(c),
		scraplioptions.WithPort(req.Port),
		scraplioptions.WithTimeoutSocket(req.ConnectTimeout),
	)
	if err != nil {
		return nil, errorcodes.Errorf("native_session_open_failed", "create the scrapligo transport: %v", err)
	}
	if err := t.Open(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return &stream{t: t, ended: make(chan struct{})}, nil
}

// connection is karvi's scrapligo transport implementation over x/crypto.
type connection struct {
	ctx     context.Context
	req     DialRequest
	offered sshalgorithms.Lists

	mu      sync.Mutex
	raw     net.Conn
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	// lost is why the keepalive ended the connection; reads and writes
	// report it in place of the closed connection's own error.
	lost error
	// lastRead is when device output last arrived (Unix nanoseconds): the
	// keepalive, as OpenSSH's, is sent only when nothing was received for
	// the interval.
	lastRead atomic.Int64

	closed    chan struct{}
	closeOnce sync.Once
}

var _ scraplitransport.Implementation = (*connection)(nil)

func (c *connection) debugf(format string, args ...any) {
	if c.req.Debug != nil {
		c.req.Debug(fmt.Sprintf(format, args...))
	}
}

// Open dials, runs the handshake with the policy in HostKeyCallback, and
// starts the shell. Its errors carry their codes.
func (c *connection) Open(a *scraplitransport.Args) error {
	req := c.req
	target := net.JoinHostPort(req.Address, strconv.Itoa(a.Port))
	offered, etmDropped, err := offer(req.Algorithms)
	if err != nil {
		return err
	}
	if offered[sshalgorithms.HostKey], err = req.Policy.HostKeyAlgorithms(offered[sshalgorithms.HostKey], hostkey.Identity(req.Host, a.Port)); err != nil {
		return err
	}
	c.offered = offered
	c.debugf("native SSH connection dialing target=%q address=%s host_key_policy=%s %s etm_macs_dropped_for_aes128_cbc=%t", req.Host, target, req.Policy.Mode, offered.Describe(), etmDropped)
	dialer := net.Dialer{Timeout: a.TimeoutSocket}
	raw, err := dialer.DialContext(c.ctx, "tcp", target)
	if err != nil {
		if c.ctx.Err() != nil {
			return c.ctx.Err()
		}
		return errorcodes.Errorf("native_session_open_failed", "%v", err)
	}
	c.mu.Lock()
	c.raw = raw
	c.mu.Unlock()

	// The handshake through the shell request is bounded by the handshake
	// timeout and ends at once on a cancel.
	_ = raw.SetDeadline(time.Now().Add(req.HandshakeTimeout))
	opened := make(chan struct{})
	defer close(opened)
	go func() {
		select {
		case <-c.ctx.Done():
			_ = raw.Close()
		case <-opened:
		}
	}()

	config := &ssh.ClientConfig{
		User:              req.Username,
		Auth:              []ssh.AuthMethod{ssh.PasswordCallback(c.password), ssh.KeyboardInteractive(c.keyboardInteractive)},
		HostKeyAlgorithms: offered[sshalgorithms.HostKey],
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			return hostkey.Verify(req.Policy, req.Host, a.Port, key.Type(), key.Marshal(), req.Warn)
		},
	}
	config.KeyExchanges = offered[sshalgorithms.Kex]
	config.Ciphers = offered[sshalgorithms.Ciphers]
	config.MACs = offered[sshalgorithms.MACs]
	sshConn, channels, requests, err := ssh.NewClientConn(raw, target, config)
	if err != nil {
		_ = raw.Close()
		return c.handshakeError(target, a.Port, err)
	}
	client := ssh.NewClient(sshConn, channels, requests)
	c.mu.Lock()
	c.client = client
	c.mu.Unlock()
	session, err := client.NewSession()
	if err != nil {
		return c.stepError(target, "open the session channel", err)
	}
	c.mu.Lock()
	c.session = session
	c.mu.Unlock()
	stdin, err := session.StdinPipe()
	if err != nil {
		return c.stepError(target, "open the session input", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return c.stepError(target, "open the session output", err)
	}
	// As ssh -tt sends from a pipe: the terminal type, no size, no modes.
	if err := session.RequestPty(req.Term, 0, 0, ssh.TerminalModes{}); err != nil {
		return c.stepError(target, "request a PTY", err)
	}
	if err := session.Shell(); err != nil {
		return c.stepError(target, "start the shell", err)
	}
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	_ = raw.SetDeadline(time.Time{})
	c.mu.Lock()
	c.stdin, c.stdout = stdin, stdout
	c.mu.Unlock()
	c.debugf("native SSH connection shell started target=%q address=%s", req.Host, target)
	if req.KeepaliveInterval > 0 {
		go c.keepalive(client)
	}
	return nil
}

// keepaliveRequest is the global request OpenSSH's client sends for
// ServerAliveInterval, so a device sees the same request from both
// transports. No server implements it: the refusal RFC 4254 requires for an
// unknown request is the answer, and any reply proves the peer alive.
const keepaliveRequest = "keepalive@openssh.com"

// keepalive sends the request whenever nothing has arrived from the device
// for the interval. As OpenSSH counts: the interval runs from the last
// output or request, any reply clears the count, and an interval that ends
// with KeepaliveCountMax requests unanswered closes the connection at once,
// (count + 1) intervals after the last output; the stream's reader reports
// session_keepalive_timeout.
func (c *connection) keepalive(client *ssh.Client) {
	interval, countMax := c.req.KeepaliveInterval, c.req.KeepaliveCountMax
	c.lastRead.Store(time.Now().UnixNano())
	timer := time.NewTimer(interval)
	defer timer.Stop()
	answered := make(chan struct{}, 1)
	unanswered := 0
	for {
		select {
		case <-c.closed:
			return
		case <-answered:
			unanswered = 0
		case <-timer.C:
			if idle := time.Since(time.Unix(0, c.lastRead.Load())); idle < interval {
				unanswered = 0 // output arrived: the peer is alive
				timer.Reset(interval - idle)
				continue
			}
			select {
			case <-answered:
				unanswered = 0
			default:
			}
			if unanswered >= countMax {
				c.debugf("native SSH connection keepalive timeout target=%q unanswered=%d interval=%s", c.req.Host, unanswered, interval)
				c.mu.Lock()
				c.lost = devsession.KeepaliveTimeout(countMax, interval, "")
				c.mu.Unlock()
				_ = c.Close()
				return
			}
			unanswered++
			timer.Reset(interval)
			go func() {
				// Blocks until the reply or the connection's end.
				if _, _, err := client.SendRequest(keepaliveRequest, true, nil); err == nil {
					select {
					case answered <- struct{}{}:
					default:
					}
				}
			}()
		}
	}
}

func (c *connection) password() (string, error) {
	if c.req.Password == nil {
		return "", errors.New("the credential has no password")
	}
	var value string
	err := c.req.Password(func(raw []byte) error {
		value = string(raw)
		return nil
	})
	return value, err
}

func (c *connection) keyboardInteractive(_, _ string, questions []string, _ []bool) ([]string, error) {
	if len(questions) == 0 {
		return nil, nil
	}
	password, err := c.password()
	if err != nil {
		return nil, err
	}
	answers := make([]string, len(questions))
	for i := range answers {
		answers[i] = password
	}
	return answers, nil
}

var noCommonAlgorithm = regexp.MustCompile(`no common algorithm for (key exchange|host key|client to server cipher|server to client cipher|client to server MAC|server to client MAC); client offered: \[[^\]]*\], server offered: \[([^\]]*)\]`)

// offer is the device's lists filtered to what the vendored x/crypto
// implements; when the cipher offer holds aes128-cbc the encrypt-then-MAC
// MACs leave the MAC offer, since x/crypto's CBC ignores encrypt-then-MAC
// and the session breaks.
func offer(lists sshalgorithms.Lists) (sshalgorithms.Lists, bool, error) {
	if len(lists) == 0 {
		lists = sshalgorithms.Defaults()
	}
	offered, err := lists.Offer("scrapligo-v1", xcryptoImplements)
	if err != nil {
		return nil, false, err
	}
	for _, cipher := range offered[sshalgorithms.Ciphers] {
		if cipher != "aes128-cbc" {
			continue
		}
		var macs []string
		for _, mac := range offered[sshalgorithms.MACs] {
			if !strings.HasSuffix(mac, "-etm@openssh.com") {
				macs = append(macs, mac)
			}
		}
		if len(macs) == 0 {
			return nil, false, errorcodes.Errorf("ssh_algorithms_unavailable", "the MAC list holds only encrypt-then-MAC algorithms, which scrapligo-v1 cannot use beside aes128-cbc")
		}
		offered[sshalgorithms.MACs] = macs
		return offered, true, nil
	}
	return offered, false, nil
}

// xcryptoHostKeys are the host-key algorithms the vendored x/crypto client
// verifies.
var xcryptoHostKeys = map[string]bool{
	ssh.KeyAlgoED25519: true, ssh.KeyAlgoECDSA521: true, ssh.KeyAlgoECDSA384: true, ssh.KeyAlgoECDSA256: true,
	ssh.KeyAlgoRSASHA512: true, ssh.KeyAlgoRSASHA256: true, ssh.KeyAlgoRSA: true,
}

// xcryptoImplements asks x/crypto itself: its SetDefaults keeps only the
// key exchanges, ciphers, and MACs it implements.
func xcryptoImplements(kind sshalgorithms.Kind, name string) bool {
	var config ssh.Config
	switch kind {
	case sshalgorithms.HostKey:
		return xcryptoHostKeys[name]
	case sshalgorithms.Kex:
		config.KeyExchanges = []string{name}
		config.SetDefaults()
		return len(config.KeyExchanges) == 1
	case sshalgorithms.Ciphers:
		config.Ciphers = []string{name}
		config.SetDefaults()
		return len(config.Ciphers) == 1
	case sshalgorithms.MACs:
		config.MACs = []string{name}
		config.SetDefaults()
		return len(config.MACs) == 1
	}
	return false
}

func (c *connection) handshakeError(target string, port int, err error) error {
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	if errorcodes.Of(err) != "" {
		return err // the host-key policy's own code
	}
	text := err.Error()
	switch {
	case noCommonAlgorithm.MatchString(text):
		m := noCommonAlgorithm.FindStringSubmatch(text)
		deviceOffer := strings.Fields(m[2])
		kind := map[string]sshalgorithms.Kind{"key exchange": sshalgorithms.Kex, "host key": sshalgorithms.HostKey}[m[1]]
		switch {
		case strings.HasSuffix(m[1], "cipher"):
			kind = sshalgorithms.Ciphers
		case strings.HasSuffix(m[1], "MAC"):
			kind = sshalgorithms.MACs
		}
		identity := hostkey.Identity(c.req.Host, port)
		if kind == sshalgorithms.HostKey && c.req.Policy.Mode != hostkey.Insecure {
			if types, _ := hostkey.EnrolledTypes(c.req.Policy.KnownHostsFile, identity); len(types) > 0 {
				return hostkey.TypesNotOffered(c.req.Policy, identity, deviceOffer)
			}
		}
		return sshalgorithms.NegotiationFailed(kind, c.offered[kind], deviceOffer)
	case strings.Contains(text, "unable to authenticate"):
		return errorcodes.Errorf("authentication_failed", "SSH authentication failed for %s at %s: %v", c.req.Username, target, err)
	case errors.Is(err, os.ErrDeadlineExceeded):
		return errorcodes.Errorf("native_session_open_failed", "SSH handshake with %s did not complete within %s", target, c.req.HandshakeTimeout)
	}
	return errorcodes.Errorf("native_session_open_failed", "SSH handshake with %s: %v", target, err)
}

func (c *connection) stepError(target, step string, err error) error {
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return errorcodes.Errorf("native_session_open_failed", "%s on %s: not done within %s", step, target, c.req.HandshakeTimeout)
	}
	return errorcodes.Errorf("native_session_open_failed", "%s on %s: %v", step, target, err)
}

// Close ends the session, the client, and the TCP connection.
func (c *connection) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	c.mu.Lock()
	session, client, raw := c.session, c.client, c.raw
	c.session, c.client, c.raw = nil, nil, nil
	c.mu.Unlock()
	if session != nil {
		_ = session.Close()
	}
	if client != nil {
		_ = client.Close()
	}
	if raw != nil {
		_ = raw.Close()
	}
	return nil
}

func (c *connection) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session != nil
}

func (c *connection) Read(n int) ([]byte, error) {
	c.mu.Lock()
	stdout, lost := c.stdout, c.lost
	c.mu.Unlock()
	if lost != nil {
		return nil, lost
	}
	if stdout == nil {
		return nil, io.EOF
	}
	b := make([]byte, n)
	n, err := stdout.Read(b)
	if n > 0 {
		c.lastRead.Store(time.Now().UnixNano())
	}
	if err != nil {
		if lost := c.lostError(); lost != nil {
			err = lost
		}
	}
	return b[:n], err
}

func (c *connection) lostError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lost
}

func (c *connection) Write(b []byte) error {
	c.mu.Lock()
	stdin, lost := c.stdin, c.lost
	c.mu.Unlock()
	if lost != nil {
		return lost
	}
	if stdin == nil {
		return io.ErrClosedPipe
	}
	_, err := stdin.Write(b)
	return err
}

// shellEndGrace is how long Close waits for the device to end the shell
// after the session's exit commands before closing the connection.
const shellEndGrace = 2 * time.Second

// stream is the scrapligo wrapper as a devsession.Stream and Aborter. Only
// devsession's reader calls Read. The wrapper's Close(false) waits for the
// read lock a blocked read holds, so every close is Close(true).
type stream struct {
	t       *scraplitransport.Transport
	pending []byte
	err     error

	ended     chan struct{}
	endOnce   sync.Once
	closeOnce sync.Once
}

var (
	_ devsession.Stream  = (*stream)(nil)
	_ devsession.Aborter = (*stream)(nil)
)

func (s *stream) Read(b []byte) (int, error) {
	for len(s.pending) == 0 {
		if s.err != nil {
			return 0, s.err
		}
		data, err := s.t.Read()
		s.pending = data
		if err != nil {
			s.err = err
			s.endOnce.Do(func() { close(s.ended) })
		}
	}
	n := copy(b, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

func (s *stream) Write(b []byte) (int, error) {
	if err := s.t.Write(b); err != nil {
		return 0, err
	}
	return len(b), nil
}

// Close waits for the shell's end up to the grace, then closes the
// connection.
func (s *stream) Close() error {
	s.closeOnce.Do(func() {
		select {
		case <-s.ended:
		case <-time.After(shellEndGrace):
		}
		_ = s.t.Close(true)
	})
	return nil
}

// Abort closes the connection at once: the session has failed.
func (s *stream) Abort() {
	s.closeOnce.Do(func() { _ = s.t.Close(true) })
}
