// Package fakedevice is an in-process SSH server, the engineering fixture for
// both transports. It answers like a Cisco IOS XE device: password
// authentication, a PTY shell, the user-exec and privilege-exec prompts,
// enable with a secret, paging commands, a few show commands, the device's
// "% Invalid input" line, the [confirm] and value prompts of a few interactive
// commands, and exit. It refuses exec requests, as a device session never
// makes one (contract rule 1), and records them and every PTY request so a
// test can assert what a transport asked for. It needs golang.org/x/crypto/ssh,
// which go.mod and vendor/ carry for the scrapligo-v1 adapter.
package fakedevice

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// Options configure a server.
type Options struct {
	Hostname string // prompt stem; default "Router"
	Username string // default "netops"
	Password string // default "pw"
	Enable   string // the enable secret; "" means enable needs no secret
	// Delay holds per-command delays before the response is written.
	Delay map[string]time.Duration
	// LoginDelay is the wait before the shell's first prompt; SecretDelay the
	// wait before the answer to the enable secret. Delay["enable"] is the
	// wait before the Password: prompt.
	LoginDelay, SecretDelay time.Duration
	// EchoSecret echoes the enable secret as it is typed, as a misconfigured
	// device or terminal server may: the case that proves the secret reaches
	// no output file even then.
	EchoSecret bool
	// BigLines is the line count "show big" produces.
	BigLines int
	// StartPrivileged starts the shell at the privilege-exec prompt.
	StartPrivileged bool
	// Unsaved starts each shell with the running configuration modified, so
	// "reload" first asks "System configuration has been modified. Save?
	// [yes/no]: " and "copy running-config startup-config" clears the state.
	Unsaved bool
	// Port is the 127.0.0.1 port to listen on; zero takes a free one. A
	// host-key identity holds the port, so a restarted server that is to
	// be the same device listens where it did.
	Port int
	// HostKeySeed is the ed25519 host key's 32-byte seed; empty generates
	// a new key, so a restarted server presents a changed key.
	HostKeySeed []byte
	// ExtraHostKeys adds host keys beside the ed25519 one: "ecdsa256",
	// "ecdsa384", "ecdsa521", "rsa".
	ExtraHostKeys []string
	// RSASHA1Only replaces every host key with one RSA key that signs only
	// with ssh-rsa (SHA-1): a legacy device.
	RSASHA1Only bool
	// KeyExchanges, Ciphers, and MACs limit what the server offers, as a
	// device with only those algorithms; empty keeps x/crypto's defaults.
	KeyExchanges, Ciphers, MACs []string
	// AuthorizedKeys, in authorized_keys form, are the public keys that log
	// in as Username beside the password; empty accepts no key.
	AuthorizedKeys []byte
	// NoKeyboardInteractive refuses the keyboard-interactive method, as a
	// server that takes the password method alone.
	NoKeyboardInteractive bool
}

// PTYRequest is one pty-req a client sent.
type PTYRequest struct {
	Term          string
	Columns, Rows uint32
	ModeBytes     int
}

// Server is a running fake.
type Server struct {
	opts     Options
	listener net.Listener
	keys     []ssh.PublicKey
	config   *ssh.ServerConfig

	mu          sync.Mutex
	authorized  map[string]bool // fingerprints of the authorized keys
	keyLogins   []string        // fingerprints of the keys that logged in
	connections int
	sessions    int
	lines       []string
	ptys        []PTYRequest
	wg          sync.WaitGroup
	// muted, set by "show mute", is a peer gone silent with the connection
	// up: nothing more is answered, keepalive requests included.
	muted      atomic.Bool
	keepalives atomic.Int64
}

// Start listens on 127.0.0.1 and serves until Close.
func Start(opts Options) (*Server, error) {
	if opts.Hostname == "" {
		opts.Hostname = "Router"
	}
	if opts.Username == "" {
		opts.Username = "netops"
	}
	if opts.Password == "" {
		opts.Password = "pw"
	}
	if opts.BigLines == 0 {
		opts.BigLines = 1000
	}
	s := &Server{opts: opts, authorized: map[string]bool{}}
	for rest := opts.AuthorizedKeys; len(bytes.TrimSpace(rest)) > 0; {
		key, _, _, next, err := ssh.ParseAuthorizedKey(rest)
		if err != nil {
			return nil, fmt.Errorf("authorized keys: %w", err)
		}
		s.authorized[ssh.FingerprintSHA256(key)] = true
		rest = next
	}
	s.config = &ssh.ServerConfig{
		// x/crypto asks once per key offered and caches the answer for the
		// signature that follows; the key that logged in is the connection's
		// permission, recorded once the handshake ends.
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fingerprint := ssh.FingerprintSHA256(key)
			s.mu.Lock()
			defer s.mu.Unlock()
			if meta.User() == opts.Username && s.authorized[fingerprint] {
				return &ssh.Permissions{Extensions: map[string]string{"key": fingerprint}}, nil
			}
			return nil, fmt.Errorf("key %s refused for %q", fingerprint, meta.User())
		},
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if meta.User() == opts.Username && string(password) == opts.Password {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", meta.User())
		},
		KeyboardInteractiveCallback: func(meta ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := client("", "", []string{"Password: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if meta.User() == opts.Username && len(answers) == 1 && answers[0] == opts.Password {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", meta.User())
		},
	}
	if opts.NoKeyboardInteractive {
		s.config.KeyboardInteractiveCallback = nil
	}
	s.config.Config.KeyExchanges = opts.KeyExchanges
	s.config.Config.Ciphers = opts.Ciphers
	s.config.Config.MACs = opts.MACs
	signers, err := hostSigners(opts)
	if err != nil {
		return nil, err
	}
	for _, signer := range signers {
		s.config.AddHostKey(signer)
		s.keys = append(s.keys, signer.PublicKey())
	}
	s.listener, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.Port)))
	if err != nil {
		return nil, err
	}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

func hostSigners(opts Options) ([]ssh.Signer, error) {
	if opts.RSASHA1Only {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, err
		}
		signer, err := ssh.NewSignerFromKey(key)
		if err != nil {
			return nil, err
		}
		legacy, err := ssh.NewSignerWithAlgorithms(signer.(ssh.AlgorithmSigner), []string{ssh.KeyAlgoRSA})
		if err != nil {
			return nil, err
		}
		return []ssh.Signer{legacy}, nil
	}
	var priv ed25519.PrivateKey
	if len(opts.HostKeySeed) > 0 {
		if len(opts.HostKeySeed) != ed25519.SeedSize {
			return nil, fmt.Errorf("host key seed must be %d bytes", ed25519.SeedSize)
		}
		priv = ed25519.NewKeyFromSeed(opts.HostKeySeed)
	} else {
		var err error
		if _, priv, err = ed25519.GenerateKey(rand.Reader); err != nil {
			return nil, err
		}
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	signers := []ssh.Signer{signer}
	for _, kind := range opts.ExtraHostKeys {
		var key any
		switch kind {
		case "ecdsa256":
			key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		case "ecdsa384":
			key, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		case "ecdsa521":
			key, err = ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
		case "rsa":
			key, err = rsa.GenerateKey(rand.Reader, 2048)
		default:
			err = fmt.Errorf("unknown host key type %q", kind)
		}
		if err != nil {
			return nil, err
		}
		extra, err := ssh.NewSignerFromKey(key)
		if err != nil {
			return nil, err
		}
		signers = append(signers, extra)
	}
	return signers, nil
}

// Addr is the listening address, host:port.
func (s *Server) Addr() string { return s.listener.Addr().String() }

// Port is the listening port.
func (s *Server) Port() int { return s.listener.Addr().(*net.TCPAddr).Port }

// HostKeys are the server's public host keys in authorized-keys form, the
// ed25519 key first.
func (s *Server) HostKeys() []string {
	out := make([]string, 0, len(s.keys))
	for _, key := range s.keys {
		out = append(out, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))))
	}
	return out
}

// Connections is the number of TCP connections accepted so far.
func (s *Server) Connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connections
}

// Keepalives is how many keepalive@openssh.com requests arrived, answered
// or not.
func (s *Server) Keepalives() int { return int(s.keepalives.Load()) }

// Sessions is the number of shells started so far.
func (s *Server) Sessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions
}

// Lines returns every input line the shells received, in order; a hidden
// (secret) line is recorded as "<secret>" and a refused exec request as
// "exec: TEXT".
func (s *Server) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}

// KeyLogins returns the fingerprint of each key that logged in, in order.
func (s *Server) KeyLogins() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keyLogins...)
}

// PTYRequests returns every pty-req received, in order.
func (s *Server) PTYRequests() []PTYRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]PTYRequest(nil), s.ptys...)
}

// Close stops listening and waits for the accept loop.
func (s *Server) Close() {
	s.listener.Close()
	s.wg.Wait()
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.connections++
		s.mu.Unlock()
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		return // a key scan or a failed authentication
	}
	defer serverConn.Close()
	if serverConn.Permissions != nil && serverConn.Permissions.Extensions["key"] != "" {
		s.mu.Lock()
		s.keyLogins = append(s.keyLogins, serverConn.Permissions.Extensions["key"])
		s.mu.Unlock()
	}
	go func() {
		for req := range reqs {
			if req.Type == "keepalive@openssh.com" {
				s.keepalives.Add(1)
			}
			if s.muted.Load() {
				continue // a peer gone silent: keepalive requests go unanswered
			}
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}()
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			return
		}
		go s.channelRequests(channel, requests)
	}
}

func (s *Server) channelRequests(channel ssh.Channel, requests <-chan *ssh.Request) {
	for req := range requests {
		switch req.Type {
		case "exec":
			s.record("exec: " + sshString(req.Payload))
			if req.WantReply {
				req.Reply(false, nil)
			}
		case "pty-req":
			s.recordPTY(req.Payload)
			if req.WantReply {
				req.Reply(true, nil)
			}
		case "shell":
			if req.WantReply {
				req.Reply(true, nil)
			}
			s.mu.Lock()
			s.sessions++
			s.mu.Unlock()
			go s.shell(channel)
		case "env", "window-change":
			if req.WantReply {
				req.Reply(true, nil)
			}
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// sshString decodes an SSH string at the start of payload.
func sshString(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	n := binary.BigEndian.Uint32(payload)
	if int(n) > len(payload)-4 {
		return ""
	}
	return string(payload[4 : 4+n])
}

func (s *Server) recordPTY(payload []byte) {
	term := sshString(payload)
	rest := payload[min(len(payload), 4+len(term)):]
	var p PTYRequest
	p.Term = term
	if len(rest) >= 16 {
		p.Columns = binary.BigEndian.Uint32(rest[0:4])
		p.Rows = binary.BigEndian.Uint32(rest[4:8])
		modes := rest[16:]
		if len(modes) >= 4 {
			p.ModeBytes = int(binary.BigEndian.Uint32(modes))
		}
	}
	s.mu.Lock()
	s.ptys = append(s.ptys, p)
	s.mu.Unlock()
}

func (s *Server) record(line string) {
	s.mu.Lock()
	s.lines = append(s.lines, line)
	s.mu.Unlock()
}

// answer is the device's response body to one command line, without the
// prompt.
func (s *Server) answer(text string) string {
	switch strings.TrimSpace(text) {
	case "", "terminal length 0", "terminal width 512", "terminal width 511":
		return ""
	case "show clock":
		return "*10:00:00.000 UTC Tue Sep 15 2026\r\n"
	case "show version":
		return "Cisco IOS XE Software, Version 17.09.04a\r\nfake-iosxe uptime is 1 day\r\n"
	case "show running-config":
		// A small configuration for the collection run: a hostname, one
		// interface, and the two lines the volatile-line filter will want
		// to remove.
		return "Building configuration...\r\n\r\nCurrent configuration : 512 bytes\r\n!\r\n! Last configuration change at 10:00:00 UTC Tue Sep 15 2026\r\n!\r\nversion 17.9\r\nhostname " + s.opts.Hostname + "\r\n!\r\ninterface GigabitEthernet1\r\n ip address 192.0.2.1 255.255.255.0\r\n!\r\nntp clock-period 17179869\r\nend\r\n"
	case "show big":
		var sb strings.Builder
		for i := 1; i <= s.opts.BigLines; i++ {
			fmt.Fprintf(&sb, "line %06d %s\r\n", i, strings.Repeat("x", 60))
		}
		return sb.String()
	case "show slow":
		return "slow output\r\n"
	case "show users":
		return "    Line       User       Host(s)              Idle       Location\r\n*  1 vty 0     " + s.opts.Username + "     idle                 00:00:00 127.0.0.1\r\n"
	}
	return strings.Repeat(" ", 5) + "^\r\n% Invalid input detected at '^' marker.\r\n\r\n"
}

func (s *Server) shell(ch ssh.Channel) {
	defer ch.Close()
	privileged := s.opts.StartPrivileged
	// config is global configuration mode, entered by "configure terminal"
	// at the privileged prompt and left by "end" or "exit": the (config)#
	// prompt level the device qualification's configuration row observes.
	// One configuration line is modelled, "interface NAME": accepted with no
	// answer but the next prompt, (config-if)#, as a device accepts a
	// configuration line. Any other line typed there is invalid input.
	config, configIf := false, false
	prompt := func() string {
		if configIf {
			return s.opts.Hostname + "(config-if)#"
		}
		if config {
			return s.opts.Hostname + "(config)#"
		}
		if privileged {
			return s.opts.Hostname + "#"
		}
		return s.opts.Hostname + ">"
	}
	w := func(text string) bool {
		_, err := io.WriteString(ch, text)
		return err == nil
	}
	time.Sleep(s.opts.LoginDelay)
	if !w("\r\n" + prompt()) {
		return
	}
	reader := bufio.NewReader(ch)
	var line []byte
	hidden := false // reading the enable secret
	confirm := ""   // the command waiting at its [confirm] for one key
	// value is the command waiting at a value prompt for one echoed line:
	// "copy" at "Destination filename [startup-config]? ",
	// "reload" at "System configuration has been modified. Save? [yes/no]: ".
	// The line is recorded as <value:TEXT>, <value:> for an empty line that
	// takes the prompt's default.
	value := ""
	unsaved := s.opts.Unsaved // the running configuration is modified
	// reloadPrompt writes the D3 [confirm] of reload and enters that state.
	reloadPrompt := func() bool {
		confirm = "reload"
		return w("\r\nProceed with reload? [confirm]")
	}
	lastCR := false
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return
		}
		if b == '\n' && lastCR {
			lastCR = false
			continue
		}
		lastCR = b == '\r'
		if confirm != "" {
			// [confirm] takes one key, unechoed: a return or y confirms, any
			// other abandons the command.
			command := confirm
			confirm = ""
			if b != '\r' && b != '\n' && b != 'y' && b != 'Y' {
				s.record("<abandon>")
				if !w("\r\n" + prompt()) {
					return
				}
				continue
			}
			s.record("<confirm>")
			if command == "reload" {
				// A reloading device: nothing more is written and the prompt
				// never returns; what arrives is discarded.
				w("\r\n")
				for {
					if _, err := reader.ReadByte(); err != nil {
						return
					}
				}
			}
			if !w("\r\n" + prompt()) {
				return
			}
			continue
		}
		if b != '\n' && b != '\r' {
			line = append(line, b)
			if (!hidden || s.opts.EchoSecret) && !w(string(b)) { // the terminal echo
				return
			}
			continue
		}
		text := string(line)
		line = line[:0]
		if hidden {
			hidden = false
			s.record("<secret>")
			time.Sleep(s.opts.SecretDelay)
			if s.opts.Enable == "" || text == s.opts.Enable {
				privileged = true
				if !w("\r\n" + prompt()) {
					return
				}
			} else if !w("\r\n% Bad secrets\r\n\r\n" + prompt()) {
				return
			}
			continue
		}
		if value != "" {
			// A value prompt takes the whole line, echoed as it was typed.
			s.record("<value:" + text + ">")
			switch value {
			case "copy":
				// Any name, or the default, is accepted: the fake has one
				// startup configuration.
				value = ""
				unsaved = false
				if !w("\r\nBuilding configuration...\r\n[OK]\r\n" + prompt()) {
					return
				}
			case "reload":
				switch strings.ToLower(strings.TrimSpace(text)) {
				case "y", "yes":
					value = ""
					unsaved = false
					if !w("\r\nBuilding configuration...\r\n[OK]") || !reloadPrompt() {
						return
					}
				case "n", "no":
					value = ""
					if !reloadPrompt() {
						return
					}
				default:
					// As the device: anything else re-asks.
					if !w("\r\nSystem configuration has been modified. Save? [yes/no]: ") {
						return
					}
				}
			}
			continue
		}
		s.record(text)
		if d := s.opts.Delay[strings.TrimSpace(text)]; d > 0 {
			time.Sleep(d)
		}
		switch strings.TrimSpace(text) {
		case "":
			if !w("\r\n" + prompt()) {
				return
			}
		case "enable":
			if privileged || s.opts.Enable == "" {
				privileged = true
				if !w("\r\n" + prompt()) {
					return
				}
				continue
			}
			hidden = true
			if !w("\r\nPassword: ") {
				return
			}
		case "clear counters":
			confirm = "clear counters"
			if !w("\r\nClear \"show interface\" counters on all interfaces [confirm]") {
				return
			}
		case "reload":
			// An unsaved running configuration is offered for saving first,
			// a value prompt; then the [confirm].
			if unsaved {
				value = "reload"
				if !w("\r\nSystem configuration has been modified. Save? [yes/no]: ") {
					return
				}
				continue
			}
			if !reloadPrompt() {
				return
			}
		case "copy running-config startup-config":
			// The device's value prompt, with its trailing space; an empty
			// line takes the default in the brackets.
			value = "copy"
			if !w("\r\nDestination filename [startup-config]? ") {
				return
			}
		case "show mute":
			s.muted.Store(true)
			select {}
		case "disable":
			privileged = false
			if !w("\r\n" + prompt()) {
				return
			}
		case "configure terminal":
			if !privileged {
				if !w("\r\n" + s.answer(text) + prompt()) {
					return
				}
				continue
			}
			config = true
			if !w("\r\nEnter configuration commands, one per line.  End with CNTL/Z.\r\n" + prompt()) {
				return
			}
		case "end":
			config, configIf = false, false
			if !w("\r\n" + prompt()) {
				return
			}
		case "show privilege":
			level := 1
			if privileged {
				level = 15
			}
			if !w(fmt.Sprintf("\r\nCurrent privilege level is %d\r\n%s", level, prompt())) {
				return
			}
		case "exit", "logout", "quit":
			if config && strings.TrimSpace(text) == "exit" {
				// One level out: (config-if)# to (config)#, (config)# to exec.
				if configIf {
					configIf = false
				} else {
					config = false
				}
				if !w("\r\n" + prompt()) {
					return
				}
				continue
			}
			w("\r\n")
			return
		default:
			if config && strings.HasPrefix(strings.TrimSpace(text), "interface ") {
				configIf = true
				if !w("\r\n" + prompt()) {
					return
				}
				continue
			}
			if !w("\r\n" + s.answer(text) + prompt()) {
				return
			}
		}
	}
}
