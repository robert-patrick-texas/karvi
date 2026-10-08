// Package askpass implements the single-use authenticated local callback used
// to deliver one already-resolved field to OpenSSH.
package askpass

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

const (
	SocketEnv = "KARVI_ASKPASS_SOCKET"
	TokenEnv  = "KARVI_ASKPASS_TOKEN"
)

type request struct {
	Token  string `json:"token"`
	Prompt string `json:"prompt"`
	Kind   string `json:"kind"`
}
type response struct {
	// Secret is encoded as base64 by encoding/json. Keeping it as bytes lets
	// both sides wipe their explicit working copy after the one-use exchange.
	Secret []byte `json:"secret,omitempty"`
	Error  string `json:"error,omitempty"`
}
type Broker struct {
	socket, token string
	listener      net.Listener
	material      credentials.Material
	requested     func() // called as a request bearing the token arrives, or nil
	done          chan struct{}
	once          sync.Once
}

// Start serves one request from OpenSSH's askpass helper on a socket in
// dir. requested, when not nil, is called when a request bearing the token
// arrives, before it is answered: OpenSSH asks only once key exchange is
// done, so a login reads the trust store then (Driver.Interactive).
func Start(dir string, material credentials.Material, requested func()) (*Broker, error) {
	if material == nil {
		return nil, errorcodes.Errorf("askpass_material_missing", "credential material is nil")
	}
	// dir is the scratch directory, which as a rule exists already and may be
	// the operator's own (tempdir). karvi sets permissions only on what it
	// creates, so a missing directory is made 0700 and an existing one is left
	// as it is; the socket below is 0600 and answers only to the token.
	if err := osutil.MakeDirectories(dir, 0700); err != nil {
		return nil, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(raw)
	socket := filepath.Join(dir, osutil.AskpassSocketName(token[:16]))
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0600); err != nil {
		ln.Close()
		os.Remove(socket)
		return nil, err
	}
	b := &Broker{socket: socket, token: token, listener: ln, material: material, requested: requested, done: make(chan struct{})}
	go b.serve()
	return b, nil
}
func (b *Broker) Environment() []string {
	return []string{SocketEnv + "=" + b.socket, TokenEnv + "=" + b.token}
}
func (b *Broker) Close() {
	b.once.Do(func() {
		if b.listener != nil {
			b.listener.Close()
		}
		os.Remove(b.socket)
		close(b.done)
	})
}
func (b *Broker) Wait(timeout time.Duration) {
	select {
	case <-b.done:
	case <-time.After(timeout):
		b.Close()
	}
}
func (b *Broker) serve() {
	defer b.Close()
	_ = b.listener.(*net.UnixListener).SetDeadline(time.Now().Add(5 * time.Minute))
	conn, err := b.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	var req request
	if err := json.NewDecoder(io.LimitReader(bufio.NewReader(conn), 64*1024)).Decode(&req); err != nil {
		json.NewEncoder(conn).Encode(response{Error: "askpass_request_invalid"})
		return
	}
	expected, _ := hex.DecodeString(b.token)
	got, err := hex.DecodeString(req.Token)
	if err != nil || len(got) != len(expected) || subtle.ConstantTimeCompare(got, expected) != 1 {
		json.NewEncoder(conn).Encode(response{Error: "askpass_auth_failed"})
		return
	}
	if b.requested != nil {
		b.requested()
	}
	kind := classify(req.Prompt, req.Kind)
	var secret []byte
	switch kind {
	case "username":
		if !b.material.UsernameSet() {
			json.NewEncoder(conn).Encode(response{Error: "askpass_username_unset"})
			return
		}
		_ = b.material.WithUsername(func(v []byte) error { secret = append(secret, v...); return nil })
	case "password":
		if !b.material.PasswordSet() {
			json.NewEncoder(conn).Encode(response{Error: "askpass_password_unset"})
			return
		}
		_ = b.material.WithPassword(func(v []byte) error { secret = append(secret, v...); return nil })
	case "enable_password":
		if !b.material.EnablePasswordSet() {
			json.NewEncoder(conn).Encode(response{Error: "askpass_enable_password_unset"})
			return
		}
		_ = b.material.WithEnablePassword(func(v []byte) error { secret = append(secret, v...); return nil })
	case "host_key":
		secret = []byte("yes")
	default:
		json.NewEncoder(conn).Encode(response{Error: "askpass_prompt_unrecognized"})
		return
	}
	_ = json.NewEncoder(conn).Encode(response{Secret: secret})
	wipe(secret)
	secret = nil
}
func classify(prompt, kind string) string {
	if kind != "" && kind != "unknown" {
		return kind
	}
	p := strings.ToLower(prompt)
	switch {
	case strings.Contains(p, "enable") && strings.Contains(p, "password"):
		return "enable_password"
	case strings.Contains(p, "password") || strings.Contains(p, "passcode"):
		return "password"
	case strings.Contains(p, "username") || strings.Contains(p, "login as"):
		return "username"
	case strings.Contains(p, "continue connecting") || strings.Contains(p, "authenticity of host"):
		return "host_key"
	default:
		return "unknown"
	}
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// HelperMain is the complete karvi-askpass process implementation.
func HelperMain(args []string) int {
	socket, token := os.Getenv(SocketEnv), os.Getenv(TokenEnv)
	if socket == "" || token == "" {
		return 2
	}
	prompt := ""
	if len(args) > 0 {
		prompt = args[0]
	}
	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return 3
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := json.NewEncoder(conn).Encode(request{Token: token, Prompt: prompt, Kind: "unknown"}); err != nil {
		return 4
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil || resp.Error != "" {
		return 5
	}
	defer wipe(resp.Secret)
	if _, err := os.Stdout.Write(resp.Secret); err != nil {
		return 6
	}
	if _, err := os.Stdout.Write([]byte{'\n'}); err != nil {
		return 6
	}
	return 0
}
