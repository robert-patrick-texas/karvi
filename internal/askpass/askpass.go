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
	"regexp"
	"strings"
	"sync"
	"time"

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
}
type response struct {
	// Secret is encoded as base64 by encoding/json. Keeping it as bytes lets
	// both sides wipe their explicit working copy after the one-use exchange.
	Secret []byte `json:"secret,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Material is what the broker answers from: the login's username and
// password, the two fields OpenSSH asks for.
type Material interface {
	UsernameSet() bool
	PasswordSet() bool
	WithUsername(func([]byte) error) error
	WithPassword(func([]byte) error) error
}

// Hooks are a broker's calls into the transport it serves, each optional.
type Hooks struct {
	// Requested is called when a request bearing the token arrives, before
	// it is answered: OpenSSH asks only once key exchange is done, so a
	// login reads the trust store then (Driver.Interactive).
	Requested func()
	// Change is called for a change prompt, before the broker refuses it,
	// and ends OpenSSH, which is then waiting on the helper: refused,
	// OpenSSH would send the device an empty answer and go on to the
	// password method.
	Change func()
}

type Broker struct {
	socket, token string
	listener      net.Listener
	material      Material
	hooks         Hooks
	done          chan struct{}
	once          sync.Once
	mu            sync.Mutex
	change        *string // the change prompt's words, once refused
}

// Start serves one request from OpenSSH's askpass helper on a socket in
// dir, calling hooks as it goes.
func Start(dir string, material Material, hooks Hooks) (*Broker, error) {
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
	b := &Broker{socket: socket, token: token, listener: ln, material: material, hooks: hooks, done: make(chan struct{})}
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
	if b.hooks.Requested != nil {
		b.hooks.Requested()
	}
	words, asked := openSSHWords(req.Prompt)
	if !asked {
		json.NewEncoder(conn).Encode(response{Error: "askpass_prompt_unrecognized"})
		return
	}
	var secret []byte
	switch Field(words) {
	case FieldChange:
		b.mu.Lock()
		b.change = &words
		b.mu.Unlock()
		if b.hooks.Change != nil {
			b.hooks.Change()
		}
		json.NewEncoder(conn).Encode(response{Error: "authentication_change_requested"})
		return
	case FieldUsername:
		if !b.material.UsernameSet() {
			json.NewEncoder(conn).Encode(response{Error: "askpass_username_unset"})
			return
		}
		_ = b.material.WithUsername(func(v []byte) error { secret = append(secret, v...); return nil })
	case FieldPassword:
		if !b.material.PasswordSet() {
			json.NewEncoder(conn).Encode(response{Error: "askpass_password_unset"})
			return
		}
		_ = b.material.WithPassword(func(v []byte) error { secret = append(secret, v...); return nil })
	default:
		json.NewEncoder(conn).Encode(response{Error: "askpass_prompt_unrecognized"})
		return
	}
	_ = json.NewEncoder(conn).Encode(response{Secret: secret})
	wipe(secret)
	secret = nil
}

// Change returns ChangeRequested's error once the broker has refused a
// change prompt, or nil.
func (b *Broker) Change() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.change == nil {
		return nil
	}
	return ChangeRequested(*b.change)
}

// ChangeRequested is the error a change prompt ends the connection with, on
// both SSH transports; prompt is the prompt's own words.
func ChangeRequested(prompt string) error {
	return errorcodes.Errorf("authentication_change_requested", "the device asks to change a secret: %q", prompt)
}

// openSSHPrefix is the user@host OpenSSH puts before a device's prompt, the
// host being the HostKeyAlias: "user@host's " in its password method's
// prompt and "(user@host) " before a keyboard-interactive server's. Neither
// name holds a space; a user may hold an @, a host none.
var openSSHPrefix = regexp.MustCompile(`^(?:\S*@[^@\s]*'s |\(\S*@[^@\s()]*\) )`)

// openSSHWords returns a prompt's own words, the user@host before them cut,
// and whether OpenSSH asks it for a device. OpenSSH's own questions begin
// otherwise: a key's passphrase, a FIDO or PKCS#11 PIN, user presence, the
// host key, and a password change ("Enter user@host's old password: ").
func openSSHWords(prompt string) (string, bool) {
	at := openSSHPrefix.FindStringIndex(prompt)
	if at == nil {
		return "", false
	}
	return prompt[at[1]:], true
}

// What a prompt asks for (Field).
const (
	FieldChange   = "change"
	FieldUsername = "username"
	FieldPassword = "password"
)

// promptPatterns are the RE2 patterns Field matches a prompt against,
// case-insensitively, list by list in this order. Each is a word of its own
// (promptEdgeStart, promptEdgeEnd), so a device named in a prompt (pin-gw1, repeater1,
// new-rtr1) is not a field's word; user alone is found inside a word.
var promptPatterns = []struct {
	field    string
	patterns []string
}{
	// A new, next, or confirmed secret, or a yes/no question in setting one.
	{FieldChange, []string{`new`, `next`, `re-?enter(ed)?`, `re-?typed?`, `(re)?confirm(ed)?`, `verify`, `repeat(ed)?`, `again`, `once more`, `chang(e|ed|es|ing)`, `renew(ed)?`, `generat(e|es|ed|ing)`, `creat(e|ed)`, `\(y/n\)`, `\[y/n\]`, `yes/no`}},
	// Any prompt naming user ("Password for user jsmith:" among them), a
	// login, or a name.
	{FieldUsername, []string{`[\w-]*user[\w-]*`, `log ?[io]n`, `name\s*:`}},
	// A password, passcode, passphrase, PIN, token, code, or OTP.
	{FieldPassword, []string{`passw\w*`, `passcode`, `passphrase`, `pass\s*:`, `pin`, `token(code)?`, `code`, `otp`}},
}

// promptEdgeStart and promptEdgeEnd make a pattern a word of its own:
// where its match's own first or last character is an ASCII letter, digit,
// or underscore, the character beside it is none of those and no hyphen,
// which would join the match to a longer word (pin-gw1, repeater1); a
// bracket or colon at a match's end stands as it is (PIN(y/n)?, Name:x).
// Before the match: the start, a character neither, a word character with
// a boundary after it, or a hyphen with none; after it, the same mirrored.
const promptEdgeStart, promptEdgeEnd = `(?:^|[^\w-]|\w\b|-\B)`, `(?:$|[^\w-]|\b\w|\B-)`

// promptList is one of promptPatterns' lists, compiled as one expression.
type promptList struct {
	field string
	re    *regexp.Regexp
}

var promptLists = func() []promptList {
	lists := make([]promptList, len(promptPatterns))
	for i, l := range promptPatterns {
		words := make([]string, len(l.patterns))
		for j, p := range l.patterns {
			words[j] = promptEdgeStart + `(?:` + p + `)` + promptEdgeEnd
		}
		lists[i] = promptList{field: l.field, re: regexp.MustCompile(`(?i)(?:` + strings.Join(words, `|`) + `)`)}
	}
	return lists
}()

// Field names what a device's prompt asks for, by the first of
// promptPatterns' lists to match it: FieldChange, a change of a secret,
// which karvi does not answer; FieldUsername; FieldPassword; or "" for no
// list, an empty answer. Both SSH transports answer by it, the broker here
// (the prompt's words, OpenSSH's user@host cut) and the native transport's
// keyboard-interactive. The enable secret is never an answer: a command
// session sends it in-band, and a login leaves it to the operator.
func Field(prompt string) string {
	for _, l := range promptLists {
		if l.re.MatchString(prompt) {
			return l.field
		}
	}
	return ""
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
	if err := json.NewEncoder(conn).Encode(request{Token: token, Prompt: prompt}); err != nil {
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
