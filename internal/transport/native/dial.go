package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"

	"github.com/robert-patrick-texas/karvi/internal/askpass"
	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
	"github.com/robert-patrick-texas/karvi/platform"
)

// DialRequest is one native connection to a device.
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
	// Keys are the private key files offered first, in order, by path:
	// each is read at authentication, and one that cannot be read or
	// parsed then is passed over. With keys and no Password, no password
	// method is offered.
	Keys   []string
	Policy hostkey.Policy
	// NoKeyboardInteractive and NoPassword leave that password method out
	// (ssh.keyboard-interactive-authentication and
	// ssh.password-authentication false), as the system transport's
	// KbdInteractiveAuthentication and PasswordAuthentication do.
	NoKeyboardInteractive, NoPassword bool
	// ConnectTimeout bounds the TCP dial; HandshakeTimeout the SSH
	// handshake, authentication, and the PTY and shell requests.
	ConnectTimeout, HandshakeTimeout time.Duration
	// KeepaliveInterval, when positive, sends an SSH keepalive request on
	// the interval once the shell has started; after KeepaliveCountMax
	// consecutive unanswered ones the connection is shut and the shell's reads
	// end with session_keepalive_timeout.
	KeepaliveInterval time.Duration
	KeepaliveCountMax int
	// Term is the PTY's terminal type ("" sends none, as ssh -tt does
	// without TERM).
	Term string
	// Algorithms are the device's SSH algorithm lists; empty means the
	// defaults.
	Algorithms sshalgorithms.Lists
	Debug      func(string)
	// HostKeyNotice is called with what the handshake found of the device's
	// key: stored under accept-new, or accepted under insecure though it
	// differs from the stored one (platform.OpenRequest.HostKeyNotice).
	HostKeyNotice func(platform.HostKeyNotice)
	// exec stops the opening at authentication (DialExec): no session
	// channel, PTY, or shell.
	exec bool
}

// Dial opens karvi's connection and returns the device shell as a
// devsession.Stream: one TCP connection, the host-key policy in the
// handshake, a PTY, and a shell.
func Dial(ctx context.Context, req DialRequest) (devsession.Stream, error) {
	c, err := connect(ctx, req)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// connect fills the request's defaults and opens the connection.
func connect(ctx context.Context, req DialRequest) (*connection, error) {
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
	c := &connection{ctx: ctx, req: req, closed: make(chan struct{}), ended: make(chan struct{}), left: make(chan struct{})}
	if err := c.open(); err != nil {
		c.shut()
		return nil, err
	}
	return c, nil
}

// connection is karvi's connection over x/crypto: the device shell as a
// devsession.Stream and Aborter, or an exec device's client (DialExec). Only
// devsession's reader calls Read.
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
	// left is closed once the connection has ended; cause is then why, the
	// client's Wait: a reset, the device's disconnect or close. Nil when
	// karvi's own shut ended it.
	left  chan struct{}
	cause error
	// lastRead is when device output last arrived (Unix nanoseconds): the
	// keepalive, as OpenSSH's, is sent only when nothing was received for
	// the interval.
	lastRead atomic.Int64

	// closed is closed when the connection is shut; ended when a read
	// first fails, the shell's end that Close waits for.
	closed   chan struct{}
	shutOnce sync.Once
	ended    chan struct{}
	endOnce  sync.Once
	// endWaitOnce makes the wait for the connection's end one wait
	// (endedError).
	endWaitOnce sync.Once
	// closeOnce makes Close and Abort one shut.
	closeOnce sync.Once
	// auth is the method of the last authentication callback x/crypto
	// called: it tries the methods in order and stops at the first that
	// succeeds, so once the handshake ends it names the method that did.
	auth atomic.Value
	// authEnd is why a callback ended the connection, once one has: a
	// change prompt (askpass.ChangeRequested) or a field asked a second
	// time (askpass.AskedAgain); the handshake ends with it.
	authEnd atomic.Pointer[error]
	// sentUsername and sentPassword are set once the field has gone to the
	// device, each at most once a connection, as the askpass broker gives it.
	sentUsername, sentPassword atomic.Bool
}

var (
	_ devsession.Stream  = (*connection)(nil)
	_ devsession.Aborter = (*connection)(nil)
)

// The authentication methods, OpenSSH's names.
const (
	authPublicKey           = "publickey"
	authKeyboardInteractive = "keyboard-interactive"
	authPassword            = "password"
)

// AuthMethod is the method that authenticated, "" before authentication.
func (c *connection) AuthMethod() string {
	v, _ := c.auth.Load().(string)
	return v
}

// noteSigner notes a public-key signature when its signer signs, with the
// algorithm x/crypto chose from the signer's and the server's.
type noteSigner struct {
	ssh.MultiAlgorithmSigner
	c    *connection
	path string
}

func (s noteSigner) SignWithAlgorithm(rand io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	s.c.auth.Store(authPublicKey)
	s.c.debugf("native SSH connection key signs path=%q algorithm=%s", s.path, algorithm)
	return s.MultiAlgorithmSigner.SignWithAlgorithm(rand, data, algorithm)
}

// rsaSigning is the order an RSA key's algorithms are offered in, strongest
// first, as OpenSSH's client; x/crypto's own order leads with rsa-sha2-256.
// ssh-rsa (SHA-1) is last, for a device that names nothing stronger.
var rsaSigning = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}

// algorithmSigner is signer naming its algorithms, an RSA key's in
// rsaSigning's order. noteSigner must hold one: through a wrapper that is an
// ssh.Signer and no more, x/crypto signs with the key's format alone,
// ssh-rsa for an RSA key.
func algorithmSigner(signer ssh.Signer) (ssh.MultiAlgorithmSigner, error) {
	as, ok := signer.(ssh.AlgorithmSigner)
	if !ok {
		return nil, fmt.Errorf("its signer (%T) takes no algorithm", signer)
	}
	algorithms := []string{signer.PublicKey().Type()}
	if algorithms[0] == ssh.KeyAlgoRSA {
		algorithms = rsaSigning
	}
	return ssh.NewSignerWithAlgorithms(as, algorithms)
}

func (c *connection) debugf(format string, args ...any) {
	if c.req.Debug != nil {
		c.req.Debug(fmt.Sprintf(format, args...))
	}
}

// open dials, runs the handshake with the policy in HostKeyCallback, and
// starts the shell. Its errors carry their codes.
func (c *connection) open() error {
	req := c.req
	target := net.JoinHostPort(req.Address, strconv.Itoa(req.Port))
	offered, etmDropped, err := offer(req.Algorithms)
	if err != nil {
		return err
	}
	if offered[sshalgorithms.HostKey], err = req.Policy.HostKeyAlgorithms(offered[sshalgorithms.HostKey], hostkey.Identity(req.Host, req.Port)); err != nil {
		return err
	}
	c.offered = offered
	c.debugf("native SSH connection dialing target=%q address=%s host_key_policy=%s %s etm_macs_dropped_for_aes128_cbc=%t", req.Host, target, req.Policy.Mode, offered.Describe(), etmDropped)
	dialer := net.Dialer{Timeout: req.ConnectTimeout}
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

	// The keys first, in the credential's order, then keyboard-interactive,
	// each prompt answered by what it asks for, and password (a server
	// may allow keyboard-interactive and refuse password), as the system
	// transport's PreferredAuthentications; the password methods beside a
	// password, or alone for a credential without keys (whose missing
	// password the callbacks report), each where its setting leaves it on.
	var auth []ssh.AuthMethod
	if len(req.Keys) > 0 {
		auth = append(auth, ssh.PublicKeysCallback(c.signers))
	}
	if req.Password != nil || len(req.Keys) == 0 {
		if !req.NoKeyboardInteractive {
			auth = append(auth, ssh.KeyboardInteractive(c.keyboardInteractive))
		}
		if !req.NoPassword {
			auth = append(auth, ssh.PasswordCallback(c.passwordMethod))
		}
	}
	config := &ssh.ClientConfig{
		User: req.Username,
		Auth: auth,
		// A callback that ended the connection ends the handshake: after its
		// error x/crypto would go on to the next method, the password.
		AuthCallback: func(*ssh.ClientAuthContext) (ssh.AuthMethod, error) {
			if ended := c.authEnd.Load(); ended != nil {
				return nil, *ended
			}
			return nil, nil
		},
		HostKeyAlgorithms: offered[sshalgorithms.HostKey],
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			verdict, err := hostkey.Verify(req.Policy, req.Host, req.Port, key.Type(), key.Marshal())
			if req.HostKeyNotice != nil {
				if verdict.Stored {
					req.HostKeyNotice(platform.HostKeyNotice{Code: platform.HostKeyEnrolled, Label: hostkey.TypeLabel(key.Type())})
				}
				if m := verdict.Mismatch; m != nil {
					req.HostKeyNotice(platform.HostKeyNotice{Code: platform.HostKeyMismatchAccepted, Enrolled: m.Enrolled, Presented: m.Presented})
				}
			}
			return err
		},
	}
	config.KeyExchanges = offered[sshalgorithms.Kex]
	config.Ciphers = offered[sshalgorithms.Ciphers]
	config.MACs = offered[sshalgorithms.MACs]
	sshConn, channels, requests, err := ssh.NewClientConn(raw, target, config)
	if err != nil {
		_ = raw.Close()
		return c.handshakeError(target, req.Port, err)
	}
	if m, ok := sshConn.(ssh.AlgorithmsConnMetadata); ok {
		c.debugf("native SSH connection negotiated target=%q address=%s %s", req.Host, target, negotiated(m.Algorithms()))
	}
	client := ssh.NewClient(sshConn, channels, requests)
	c.mu.Lock()
	c.client = client
	c.mu.Unlock()
	go func() {
		// The connection's own end, kept: once it has ended, x/crypto
		// answers a channel's opening, read, and write with EOF alone.
		err := client.Wait()
		select {
		case <-c.closed: // karvi's own shut, which closes closed first
			err = nil
		default:
		}
		c.mu.Lock()
		c.cause = err
		c.mu.Unlock()
		close(c.left)
	}()
	if req.exec {
		// An exec device is ready once it has authenticated; each command
		// opens its own channel (DialExec).
		if c.ctx.Err() != nil {
			return c.ctx.Err()
		}
		_ = raw.SetDeadline(time.Time{})
		c.debugf("native SSH connection authenticated for exec target=%q address=%s", req.Host, target)
		if req.KeepaliveInterval > 0 {
			go c.keepalive(client)
		}
		return nil
	}
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
// (count + 1) intervals after the last output; the shell's reader reports
// session_keepalive_timeout. A connection that has ended is not a silent
// device: the keepalive stops there, and the end keeps its own cause.
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
		case <-c.left:
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
				select {
				case <-c.left:
					return // ended, not silent
				default:
				}
				c.debugf("native SSH connection keepalive timeout target=%q unanswered=%d interval=%s", c.req.Host, unanswered, interval)
				c.mu.Lock()
				c.lost = devsession.KeepaliveTimeout(countMax, interval, "")
				c.mu.Unlock()
				c.shut()
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

// signers reads the credential's key files as they are at authentication,
// compared with nothing: a file that can no longer be read or parsed (moved,
// or given a passphrase since planning) is passed over with a debug line,
// and the server's refusal of the rest is the authentication failure.
func (c *connection) signers() ([]ssh.Signer, error) {
	out := make([]ssh.Signer, 0, len(c.req.Keys))
	for _, path := range c.req.Keys {
		data, err := os.ReadFile(path)
		if err != nil {
			c.debugf("native SSH connection key passed over path=%q: %v", path, err)
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		clear(data)
		var named ssh.MultiAlgorithmSigner
		if err == nil {
			named, err = algorithmSigner(signer)
		}
		if err != nil {
			c.debugf("native SSH connection key passed over path=%q: %v", path, err)
			continue
		}
		out = append(out, noteSigner{MultiAlgorithmSigner: named, c: c, path: path})
	}
	return out, nil
}

// passwordMethod is the password method's callback: the password, noted,
// unless keyboard-interactive sent it, which ends the connection.
func (c *connection) passwordMethod() (string, error) {
	c.auth.Store(authPassword)
	if c.sentPassword.Load() {
		return "", c.endAuth(askpass.AskedAgain(askpass.FieldPassword))
	}
	password, err := c.password()
	if err == nil {
		c.sentPassword.Store(true)
	}
	return password, err
}

// endAuth records err as why the connection ends (AuthCallback) and
// returns it.
func (c *connection) endAuth(err error) error {
	c.authEnd.Store(&err)
	return err
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

// keyboardInteractive answers each prompt by what it asks for
// (askpass.Field), as the system transport's askpass broker does: the
// password, the username, each at most once a connection, or, for a prompt
// no list matches, an empty answer, as OpenSSH sends for a prompt the
// broker refuses. A round is judged whole, in order, before anything is
// resolved: one holding a change prompt or a field asked a second time is
// answered by none, the first such prompt ending the handshake with its
// error (AuthCallback), as the broker meets OpenSSH's requests in order.
func (c *connection) keyboardInteractive(_, _ string, questions []string, _ []bool) ([]string, error) {
	if len(questions) == 0 {
		return nil, nil
	}
	c.auth.Store(authKeyboardInteractive)
	fields := make([]string, len(questions))
	asked := map[string]bool{askpass.FieldUsername: c.sentUsername.Load(), askpass.FieldPassword: c.sentPassword.Load()}
	for i, question := range questions {
		switch fields[i] = askpass.Field(question); fields[i] {
		case askpass.FieldChange:
			return nil, c.endAuth(askpass.ChangeRequested(question))
		case askpass.FieldUsername, askpass.FieldPassword:
			if asked[fields[i]] {
				return nil, c.endAuth(askpass.AskedAgain(fields[i]))
			}
			asked[fields[i]] = true
		}
	}
	answers := make([]string, len(questions))
	for i, field := range fields {
		switch field {
		case askpass.FieldPassword:
			password, err := c.password()
			if err != nil {
				return nil, err
			}
			answers[i] = password
		case askpass.FieldUsername:
			answers[i] = c.req.Username
		}
	}
	for _, field := range fields {
		switch field {
		case askpass.FieldPassword:
			c.sentPassword.Store(true)
		case askpass.FieldUsername:
			c.sentUsername.Store(true)
		}
	}
	return answers, nil
}

// negotiatedKinds are the lists a negotiation failure names, by x/crypto's
// words for them.
var negotiatedKinds = map[string]sshalgorithms.Kind{
	"key exchange": sshalgorithms.Kex, "host key": sshalgorithms.HostKey,
	"client to server cipher": sshalgorithms.Ciphers, "server to client cipher": sshalgorithms.Ciphers,
	"client to server MAC": sshalgorithms.MACs, "server to client MAC": sshalgorithms.MACs,
}

// offer is the device's lists filtered to what the vendored x/crypto
// implements; when the cipher offer holds aes128-cbc the encrypt-then-MAC
// MACs leave the MAC offer, since x/crypto's CBC ignores encrypt-then-MAC
// and the session breaks.
func offer(lists sshalgorithms.Lists) (sshalgorithms.Lists, bool, error) {
	if len(lists) == 0 {
		lists = sshalgorithms.Defaults()
	}
	offered, err := lists.Offer("native", xcryptoImplements)
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
			return nil, false, errorcodes.Errorf("ssh_algorithms_unavailable", "the MAC list holds only encrypt-then-MAC algorithms, which native cannot use beside aes128-cbc")
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
// key exchanges, ciphers, and MACs it implements, and adds
// curve25519-sha256@libssh.org beside curve25519-sha256.
func xcryptoImplements(kind sshalgorithms.Kind, name string) bool {
	var config ssh.Config
	switch kind {
	case sshalgorithms.HostKey:
		return xcryptoHostKeys[name]
	case sshalgorithms.Kex:
		config.KeyExchanges = []string{name}
		config.SetDefaults()
		return slices.Contains(config.KeyExchanges, name)
	case sshalgorithms.Ciphers:
		config.Ciphers = []string{name}
		config.SetDefaults()
		return slices.Contains(config.Ciphers, name)
	case sshalgorithms.MACs:
		config.MACs = []string{name}
		config.SetDefaults()
		return slices.Contains(config.MACs, name)
	}
	return false
}

// negotiated names the algorithms a completed handshake chose, for the debug
// line after it (EXAMPLES chapter 94): the key exchange, the host-key
// algorithm, the cipher, and the MAC. A cipher or MAC whose two directions
// differ is written to-device/from-device, and an AEAD cipher's MAC, which
// x/crypto leaves empty, is <implicit>, as OpenSSH's -v writes it. A failed
// handshake returns no connection, so it names none; a device's choice is the
// same at every connection, so its next completed one names it.
func negotiated(a ssh.NegotiatedAlgorithms) string {
	pair := func(toDevice, fromDevice string) string {
		if toDevice == fromDevice {
			return toDevice
		}
		return toDevice + "/" + fromDevice
	}
	mac := func(d ssh.DirectionAlgorithms) string {
		if d.MAC == "" {
			return "<implicit>"
		}
		return d.MAC
	}
	return fmt.Sprintf("kex=%s hostkey=%s cipher=%s mac=%s", a.KeyExchange, a.HostKey, pair(a.Write.Cipher, a.Read.Cipher), pair(mac(a.Write), mac(a.Read)))
}

func (c *connection) handshakeError(target string, port int, err error) error {
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	if ended := c.authEnd.Load(); ended != nil {
		return *ended
	}
	if errorcodes.Of(err) != "" {
		return err // the host-key policy's own code
	}
	// x/crypto's negotiation error names the list, its own offer, and the
	// device's.
	var negotiation *ssh.AlgorithmNegotiationError
	if errors.As(err, &negotiation) {
		if kind, named := negotiatedKinds[negotiation.What]; named {
			deviceOffer := negotiation.RequestedAlgorithms
			identity := hostkey.Identity(c.req.Host, port)
			if kind == sshalgorithms.HostKey && c.req.Policy.Mode != hostkey.Insecure {
				if types, _ := hostkey.EnrolledTypes(c.req.Policy.KnownHostsFile, identity); len(types) > 0 {
					return hostkey.TypesNotOffered(c.req.Policy, identity, deviceOffer)
				}
			}
			// A list karvi did not hold was x/crypto's own, which its
			// error names.
			offerer, offered := "karvi", c.offered[kind]
			if _, held := c.offered[kind]; !held {
				offerer, offered = "native", negotiation.SupportedAlgorithms
			}
			return sshalgorithms.NegotiationFailed(kind, offerer, offered, deviceOffer)
		}
	}
	text := err.Error()
	switch {
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

// shut ends the session, the client, and the TCP connection at once.
func (c *connection) shut() {
	c.shutOnce.Do(func() { close(c.closed) })
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
}

// Read returns the shell's output and notes its arrival for the keepalive.
// Once the keepalive has ended the connection, its code is the error; once
// the device or the network has, command_session_lost naming why
// (endedError). The shell's end is marked before that is asked, so Close
// does not wait on it.
func (c *connection) Read(b []byte) (int, error) {
	c.mu.Lock()
	stdout, lost := c.stdout, c.lost
	c.mu.Unlock()
	if lost != nil {
		c.end()
		return 0, lost
	}
	if stdout == nil {
		c.end()
		return 0, io.EOF
	}
	n, err := stdout.Read(b)
	if n > 0 {
		c.lastRead.Store(time.Now().UnixNano())
	}
	if err != nil {
		c.end()
		if ended := c.endedError(); ended != nil {
			err = ended
		}
	}
	return n, err
}

// end marks the shell's end, which Close waits for.
func (c *connection) end() { c.endOnce.Do(func() { close(c.ended) }) }

func (c *connection) lostError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lost
}

func (c *connection) Write(b []byte) (int, error) {
	c.mu.Lock()
	stdin, lost := c.stdin, c.lost
	c.mu.Unlock()
	if lost != nil {
		return 0, lost
	}
	if stdin == nil {
		return 0, io.ErrClosedPipe
	}
	n, err := stdin.Write(b)
	if err != nil {
		if ended := c.endedError(); ended != nil {
			err = ended
		}
	}
	return n, err
}

// endWait bounds the wait for the connection's end once a channel has
// shown one: x/crypto ends the channels first, then its Wait. A shell the
// device ends with the connection up waits it out, once: what fails after
// that wait finds the connection's end already known or not at all.
const endWait = 500 * time.Millisecond

// endedError is the connection's end under its code: the keepalive's, or
// command_session_lost naming why the device or the network ended it; nil
// while the connection is up and after karvi's own shut.
func (c *connection) endedError() error {
	if lost := c.lostError(); lost != nil {
		return lost
	}
	c.endWaitOnce.Do(func() {
		select {
		case <-c.left:
		case <-time.After(endWait):
		}
	})
	select {
	case <-c.left:
	default:
		return nil
	}
	if lost := c.lostError(); lost != nil {
		return lost
	}
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if cause == nil {
		return nil
	}
	return errorcodes.Errorf("command_session_lost", "the connection ended: %s", endText(cause))
}

// endTextMax bounds endText: a disconnect's message is the device's text,
// up to a packet of it.
const endTextMax = 2048

// endText is why: the device's close with no message, the network's error
// in the kernel's words without the addresses, or x/crypto's own words (a
// disconnect's reason and its message, which x/crypto quotes), their start
// kept up to endTextMax bytes.
func endText(err error) string {
	if errors.Is(err, io.EOF) {
		return "closed by the device"
	}
	text := err.Error()
	var op *net.OpError
	if errors.As(err, &op) {
		text = op.Err.Error()
		var sys *os.SyscallError
		if errors.As(op.Err, &sys) {
			text = sys.Err.Error()
		}
	}
	if len(text) > endTextMax {
		cut := endTextMax
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "..."
	}
	return text
}

// shellEndGrace is how long Close waits for the device to end the shell
// after the session's exit commands before closing the connection.
const shellEndGrace = 2 * time.Second

// Close waits for the shell's end up to the grace, then shuts the
// connection; once the job has ended (the context the connection was
// opened with: a stop or a cancel) it shuts it at once.
func (c *connection) Close() error {
	c.closeOnce.Do(func() {
		select {
		case <-c.ended:
		case <-c.ctx.Done():
		case <-time.After(shellEndGrace):
		}
		c.shut()
	})
	return nil
}

// Abort shuts the connection at once: the session has failed.
func (c *connection) Abort() {
	c.closeOnce.Do(c.shut)
}
