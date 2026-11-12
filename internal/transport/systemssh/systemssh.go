// Package systemssh is the process-backed OpenSSH adapter shared by direct
// command and run --transport system. It never places credentials in argv or
// plaintext environment variables.
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/askpass"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
	"github.com/robert-patrick-texas/karvi/platform"
)

type Factory struct {
	Binary                                     string
	Config                                     configload.Snapshot
	ScratchDir, ControlRoot, Home, AskpassPath string
	// BaseDir is the operator's private root, which holds the trust store
	// under ssh.known-hosts-file "auto".
	BaseDir        string
	MaxOutputBytes int64
	// Spool is the session's spool: the directory,
	// the threshold, and the activity; the device is filled per session.
	Spool   devsession.Spool
	Debug   func(string)
	hostKey hostkey.Policy
	// Algorithms are the device's algorithm lists; empty means the
	// configuration's global lists.
	Algorithms sshalgorithms.Lists
	// Effective is the job's evaluation of OpenSSH's own lists, shared by
	// its devices; nil evaluates for each failure.
	Effective *EffectiveLists
	// Operator asks the operator of a login (Interactive) a device's prompt
	// the broker does not answer itself (askpass.Hooks.Operator):
	// TerminalOperator, or nil, a login then answering as a command
	// session does.
	Operator func(ctx context.Context, prompt string) ([]byte, error)
	// hostKeyIdentity is the device's host-key identity, passed to OpenSSH
	// as HostKeyAlias; it filters the offered
	// host-key algorithms.
	hostKeyIdentity string
	// offered are the lists written into the generated configuration, the
	// host-key list after the trust store's filter.
	offered sshalgorithms.Lists
	// identities are the credential's key files, offered in order; with
	// passwordless set the credential has keys and no password, and the
	// password methods are off.
	identities   []string
	passwordless bool
}
type Driver struct {
	f                  Factory
	req                platform.OpenRequest
	binary, configPath string
	session            *devsession.Session
	// stream is the OpenSSH process Prepare started, for AuthMethod.
	stream *processStream
	// exec and master are an exec device's session and its ControlMaster.
	exec   *devsession.ExecSession
	master *execMaster
}

func (f Factory) Open(ctx context.Context, req platform.OpenRequest) (platform.Driver, error) {
	policy, err := hostkey.Resolve(f.Config.String("ssh.host-key-policy"), f.Config.String("ssh.known-hosts-file"), f.Home, f.BaseDir)
	if err != nil {
		return nil, err
	}
	f.hostKey = policy
	host := req.Metadata["canonical_name"]
	if host == "" {
		host = req.Address
	}
	// Under insecure OpenSSH's store is /dev/null: the key is compared with
	// the stored one beside the connection; a key the store marks revoked
	// refuses the device, and a difference, or a comparison that could not
	// complete, is the device's notice.
	m, nc, err := hostkey.CompareInsecure(ctx, policy, host, req.Address, int(req.Port))
	if err != nil {
		return nil, err
	}
	if req.HostKeyNotice != nil {
		if m != nil {
			req.HostKeyNotice(platform.HostKeyNotice{Code: platform.HostKeyMismatchAccepted, Enrolled: m.Enrolled, Presented: m.Presented})
		} else if nc != nil {
			req.HostKeyNotice(platform.HostKeyNotice{Code: platform.HostKeyNotCompared, Cause: nc.Cause, Reason: nc.Reason})
		}
	}
	f.hostKeyIdentity = hostkey.Identity(host, int(req.Port))
	binary := f.Binary
	if binary == "" {
		var err error
		binary, err = exec.LookPath("ssh")
		if err != nil {
			return nil, fmt.Errorf("dependency_ssh_unavailable: %w", err)
		}
	}
	f.Binary = binary
	f.identities, f.passwordless = req.Keys, len(req.Keys) > 0 && req.Password == nil
	if f.offered, err = f.offeredAlgorithms(binaryCapabilities(f.Config, binary).implements); err != nil {
		return nil, err
	}
	if f.MaxOutputBytes <= 0 {
		f.MaxOutputBytes = f.Config.Int64("output.max-command-bytes")
	}
	if f.MaxOutputBytes <= 0 {
		f.MaxOutputBytes = 64 << 20
	}
	if f.ScratchDir == "" {
		f.ScratchDir = os.TempDir()
	}
	if err := os.MkdirAll(f.ScratchDir, 0700); err != nil {
		return nil, errorcodes.Errorf("ssh_config_write_failed", "create scratch directory: %w", err)
	}
	content, err := f.renderConfig()
	if err != nil {
		return nil, err
	}
	cf, err := os.CreateTemp(f.ScratchDir, osutil.ScratchFilePattern("karvi-ssh-", ".conf"))
	if err != nil {
		return nil, errorcodes.Errorf("ssh_config_write_failed", "create generated SSH configuration: %w", err)
	}
	if err := cf.Chmod(0600); err != nil {
		cf.Close()
		os.Remove(cf.Name())
		return nil, errorcodes.Errorf("ssh_config_write_failed", "write generated SSH configuration: %w", err)
	}
	if _, err := io.WriteString(cf, content); err != nil {
		cf.Close()
		os.Remove(cf.Name())
		return nil, errorcodes.Errorf("ssh_config_write_failed", "write generated SSH configuration: %w", err)
	}
	if err := cf.Close(); err != nil {
		os.Remove(cf.Name())
		return nil, errorcodes.Errorf("ssh_config_write_failed", "write generated SSH configuration: %w", err)
	}
	return &Driver{f: f, req: req, binary: binary, configPath: cf.Name()}, nil
}

// Prepare opens the device session: one interactive OpenSSH shell, the
// first prompt within the connect and prompt timeouts, then the session's
// privilege and paging steps; or, for an exec device, its ControlMaster,
// ready within the same bound. command and run share it.
func (d *Driver) Prepare(ctx context.Context) error {
	if d.req.Channel == platform.ChannelExec {
		return d.prepareExec(ctx)
	}
	stream, broker, err := d.startShell(ctx)
	if err != nil {
		return err
	}
	d.stream = stream
	connectTimeout, promptTimeout := d.loginTimeouts()
	enableTimeout := d.f.Config.Duration("execution.enable-timeout")
	if enableTimeout <= 0 {
		enableTimeout = 10 * time.Second
	}
	// The ssh process dials, authenticates, and starts the shell in one
	// stretch karvi cannot see into, so the first prompt is awaited for the
	// connect and prompt timeouts together.
	session, err := devsession.Open(ctx, stream, devsession.Options{
		Definition: d.req.Definition, EnableSecret: d.req.EnablePassword, MaxOutputBytes: d.f.MaxOutputBytes, Spool: d.f.Spool.ForRequest(d.req), InFlightBytes: d.req.InFlightBytes,
		LoginTimeout: connectTimeout + promptTimeout, EnableTimeout: enableTimeout, PromptTimeout: promptTimeout,
		Debug: d.f.Debug, Live: d.req.Live,
	})
	// The askpass broker serves the authentication, which is over once the
	// first prompt has arrived (or the open has failed); a connection the
	// broker ended (askpass.Broker.Err) is the failure's cause.
	broker.Close()
	if err != nil {
		if ended := broker.Err(); ended != nil {
			err = ended
		}
		d.debugf("system SSH command session open failed code=%s diagnostic=%q", errorcodes.Of(err), compactDiagnostic(stream.Diagnostics()))
		return err
	}
	d.session = session
	d.debugf("system SSH command session ready pid=%d prompt=%q level=%q", stream.cmd.Process.Pid, session.Prompt(), session.Level())
	return session.Prepare(ctx)
}

// prepareExec starts an exec device's ControlMaster and waits for it to
// answer -O check within the connect and prompt timeouts together, the
// bound a shell's login has; no first prompt, privilege, or paging.
func (d *Driver) prepareExec(ctx context.Context) error {
	master, broker, err := d.startMaster()
	if err != nil {
		return err
	}
	connectTimeout, promptTimeout := d.loginTimeouts()
	err = master.ready(ctx, connectTimeout+promptTimeout)
	broker.Close()
	if err != nil {
		if ended := broker.Err(); ended != nil {
			err = ended
		}
		d.debugf("system SSH exec master failed code=%s diagnostic=%q", errorcodes.Of(err), compactDiagnostic(master.diagnostics.String()))
		return err
	}
	d.master = master
	d.exec = devsession.OpenExec(&execConn{m: master}, devsession.ExecOptions{Definition: d.req.Definition, MaxOutputBytes: d.f.MaxOutputBytes, Spool: d.f.Spool.ForRequest(d.req), InFlightBytes: d.req.InFlightBytes, Debug: d.f.Debug, Live: d.req.Live, SudoPassword: d.req.EnablePassword})
	d.debugf("system SSH exec master ready pid=%d socket=%s auth=%q", master.cmd.Process.Pid, master.socket, master.lines.Method())
	return nil
}

// loginTimeouts are the connect and prompt timeouts a session's login is
// bounded by.
func (d *Driver) loginTimeouts() (connectTimeout, promptTimeout time.Duration) {
	connectTimeout = d.req.Timeout
	if connectTimeout <= 0 {
		connectTimeout = d.f.Config.Duration("ssh.connect-timeout")
	}
	if connectTimeout <= 0 {
		connectTimeout = 30 * time.Second
	}
	promptTimeout = d.f.Config.Duration("execution.prompt-timeout")
	if promptTimeout <= 0 {
		promptTimeout = 10 * time.Second
	}
	return connectTimeout, promptTimeout
}

func (d *Driver) Close() error {
	var sessionErr error
	if d.session != nil {
		sessionErr = d.session.Close()
	}
	if d.exec != nil {
		sessionErr = d.exec.Close()
	}
	removeErr := os.Remove(d.configPath)
	if os.IsNotExist(removeErr) {
		removeErr = nil
	}
	if sessionErr != nil {
		return sessionErr
	}
	return removeErr
}

// Usable reports the prepared session's answer; a driver whose session was
// never prepared is unusable.
func (d *Driver) Usable() bool {
	if d.exec != nil {
		return d.exec.Usable()
	}
	return d.session != nil && d.session.Usable()
}

// The executor finds the set-up lines by a type assertion, which a renamed
// method would fail silently; this fails the build instead.
var _ platform.SetupReporter = (*Driver)(nil)

var _ platform.AuthReporter = (*Driver)(nil)

// hostKeyEnrolled is what OpenSSH's "Permanently added" line reports, and a
// login's trust-store read (firstContact): the request's host_key_enrolled
// notice under accept-new, the one policy that stores a key, said once, for
// a device the trust store holds nothing for as the session starts; nil
// otherwise: under insecure the trust store is /dev/null and OpenSSH says
// the same line of a key it did not keep, and a device with a stored key
// has no key to store. OpenSSH writes its line at key exchange, before
// authentication and so before a device's banner, which can hold the same
// line: the first is OpenSSH's.
func (d *Driver) hostKeyEnrolled() func(label string) {
	if d.f.hostKey.Mode != hostkey.AcceptNew || d.req.HostKeyNotice == nil {
		return nil
	}
	if types, err := hostkey.EnrolledTypes(d.f.hostKey.KnownHostsFile, d.f.hostKeyIdentity); err != nil || len(types) > 0 {
		return nil
	}
	var once sync.Once
	return func(label string) {
		once.Do(func() {
			d.req.HostKeyNotice(platform.HostKeyNotice{Code: platform.HostKeyEnrolled, Label: label})
		})
	}
}

// firstContact is a login's first-contact check under accept-new: the
// login runs OpenSSH at LogLevel ERROR, which says nothing of a key it
// stores, so the trust store is read for the device's entry before the
// session and, when it has none, once more: at the first askpass request,
// when OpenSSH has finished key exchange and stored the key if it was to,
// else when the session ends (a login by keys alone asks nothing). A key
// found then is the request's host_key_enrolled notice. Nil when there is
// nothing to check (hostKeyEnrolled's first read). Neither read waits on the
// store's lock.
func (d *Driver) firstContact() func() {
	enrolled := d.hostKeyEnrolled()
	if enrolled == nil {
		return nil
	}
	file, identity := d.f.hostKey.KnownHostsFile, d.f.hostKeyIdentity
	var once sync.Once
	return func() {
		once.Do(func() {
			if types, err := hostkey.EnrolledTypes(file, identity); err == nil && len(types) > 0 {
				enrolled(hostkey.TypeLabel(types[0]))
			}
		})
	}
}

// AuthMethod is the method OpenSSH said authenticated the session
// (platform.AuthReporter).
func (d *Driver) AuthMethod() string {
	if d.master != nil {
		return d.master.lines.Method()
	}
	if d.stream == nil || d.stream.auth == nil {
		return ""
	}
	return d.stream.auth.Method()
}

// SetupLines is the session's set-up as it was sent (platform.SetupReporter);
// an exec device sends none.
func (d *Driver) SetupLines() []platform.SetupLine {
	if d.session == nil {
		return nil
	}
	return d.session.SetupLines()
}

// Execute sends one command through the prepared session, or runs it on an
// exec channel of its own.
func (d *Driver) Execute(ctx context.Context, c platform.Command) platform.Result {
	if d.session == nil && d.exec == nil {
		return failed(time.Now(), "command_session_lost", "connection", true, errors.New("the device session was not prepared"))
	}
	if c.Timeout <= 0 {
		c.Timeout, c.TimeoutSource = d.f.Config.Duration("execution.command-timeout"), platform.SessionTimeout
	}
	if c.Timeout <= 0 {
		c.Timeout = 120 * time.Second
	}
	if d.exec != nil {
		return d.exec.Execute(ctx, c)
	}
	return d.session.Execute(ctx, c)
}

// Interactive attaches the actual terminal to an OpenSSH process. Askpass is
// still forced, so password input never competes with device stdin; a
// prompt the broker does not answer itself goes to the Factory's Operator.
func (d *Driver) Interactive(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	args := d.baseArgs()
	args = append(args, "-tt", "--", d.req.Address)
	cmd := exec.CommandContext(ctx, d.binary, args...)
	material := callbackMaterial{username: d.req.Username, password: d.req.Password}
	firstContact := d.firstContact()
	end := newProcessEnd()
	hooks := askpass.Hooks{Requested: firstContact, End: end.end}
	if d.f.Operator != nil {
		hooks.Operator = func(prompt string) ([]byte, error) { return d.f.Operator(ctx, prompt) }
	}
	broker, err := askpass.Start(d.f.ScratchDir, material, hooks)
	if err != nil {
		return errorcodes.Ensure(err, "askpass_start_failed")
	}
	defer broker.Close()
	askPath, err := findAskpass(d.f.AskpassPath)
	if err != nil {
		return errorcodes.Errorf("dependency_askpass_unavailable", "locate karvi-askpass: %w", err)
	}
	cmd.Env = childEnvironment(d.f.Config, append(broker.Environment(), "SSH_ASKPASS="+askPath, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=karvi:0")...)
	d.debugf("system SSH interactive session starting binary=%q target=%q address=%q port=%d host_key_policy=%s", d.binary, d.req.Metadata["canonical_name"], d.req.Address, d.req.Port, d.f.hostKey.Mode)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	var diagnostic bytes.Buffer
	cmd.Stderr = io.MultiWriter(stderr, &diagnostic)
	waited, err := osutil.StartTied(cmd, nil)
	if err == nil {
		end.started(cmd.Process)
		err = <-waited
	}
	if firstContact != nil {
		firstContact()
	}
	if err != nil {
		if ended := broker.Err(); ended != nil {
			d.debugf("system SSH interactive session ended code=%s", errorcodes.Of(ended))
			return ended
		}
		code, _, _, _ := classify(diagnostic.String(), err)
		d.debugf("system SSH interactive session failed code=%s diagnostic=%q", code, compactDiagnostic(diagnostic.String()))
		return fmt.Errorf("%s: %s", code, safeDiagnostic(diagnostic.String(), err))
	}
	d.debugf("system SSH interactive session completed target=%q", d.req.Metadata["canonical_name"])
	return nil
}
func (d *Driver) baseArgs() []string {
	return append(d.hostArgs(), d.controlArgs()...)
}

// hostArgs are the generated configuration and the device: an exec
// device's master adds its own control options to them.
func (d *Driver) hostArgs() []string {
	return []string{"-F", d.configPath, "-o", "HostName=" + d.req.Address, "-o", "HostKeyAlias=" + d.f.hostKeyIdentity, "-o", "User=" + d.req.Username, "-o", "Port=" + strconv.Itoa(int(d.req.Port))}
}

// controlArgs pins OpenSSH connection reuse off on the command line, where
// the first obtained value overrides both the generated file and
// ~/.ssh/config. One interactive shell is a shell device's whole session,
// so there is nothing for a master to share; an exec device's master sets
// its own (exec.go).
func (d *Driver) controlArgs() []string {
	return []string{"-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ControlPersist=no"}
}

func (f Factory) renderConfig() (string, error) {
	var b strings.Builder
	if f.hostKey.Mode == "" {
		policy, err := hostkey.Resolve(f.Config.String("ssh.host-key-policy"), f.Config.String("ssh.known-hosts-file"), f.Home, f.BaseDir)
		if err != nil {
			return "", err
		}
		f.hostKey = policy
	}
	strict, userKnownHosts, globalKnownHosts := f.hostKey.OpenSSHSettings()
	// The device's lists as this binary implements them, the host-key list
	// filtered to the enrolled key types: an explicit list turns off
	// OpenSSH's own preference for known key types. A list the device's
	// lists do not hold (ssh-algorithms.source "transport") has no line,
	// so OpenSSH's defaults, or an included ~/.ssh/config's, apply.
	offered := f.offered
	if offered == nil {
		var err error
		if offered, err = f.offeredAlgorithms(func(sshalgorithms.Kind, string) bool { return true }); err != nil {
			return "", err
		}
	}
	b.WriteString("Host *\n")
	fmt.Fprintf(&b, "  StrictHostKeyChecking %s\n", strict)
	for _, list := range []struct {
		option string
		kind   sshalgorithms.Kind
	}{{"HostKeyAlgorithms", sshalgorithms.HostKey}, {"KexAlgorithms", sshalgorithms.Kex}, {"Ciphers", sshalgorithms.Ciphers}, {"MACs", sshalgorithms.MACs}} {
		if names, held := offered[list.kind]; held {
			fmt.Fprintf(&b, "  %s %s\n", list.option, strings.Join(names, ","))
		}
	}
	fmt.Fprintf(&b, "  UserKnownHostsFile %s\n", sshQuote(userKnownHosts))
	fmt.Fprintf(&b, "  GlobalKnownHostsFile %s\n", sshQuote(globalKnownHosts))
	yesno := func(v bool) string {
		if v {
			return "yes"
		}
		return "no"
	}
	// The credential's keys alone, in its order: no agent, no default
	// identity. The included ~/.ssh/config may still add IdentityFile
	// lines after these, which OpenSSH offers once these are refused; the
	// log names whichever key authenticated. A credential without keys
	// offers none, and one without a password no password method.
	fmt.Fprintf(&b, "  PubkeyAuthentication %s\n", yesno(len(f.identities) > 0))
	if len(f.identities) > 0 {
		b.WriteString("  IdentitiesOnly yes\n  IdentityAgent none\n")
		for _, path := range f.identities {
			fmt.Fprintf(&b, "  IdentityFile %s\n", sshQuote(path))
		}
	}
	// The methods in karvi's order on both transports: the keys, then
	// keyboard-interactive, each prompt answered by what it asks for
	// (askpass.Field), and password (a server may allow keyboard-interactive
	// and refuse password); the broker gives the username and the password
	// each at most once.
	b.WriteString("  PreferredAuthentications publickey,keyboard-interactive,password\n")
	fmt.Fprintf(&b, "  PasswordAuthentication %s\n", yesno(!f.passwordless && f.Config.Bool("ssh.password-authentication")))
	fmt.Fprintf(&b, "  KbdInteractiveAuthentication %s\n", yesno(!f.passwordless && f.Config.Bool("ssh.keyboard-interactive-authentication")))
	fmt.Fprintf(&b, "  ConnectTimeout %d\n", ceilSeconds(f.Config.Duration("ssh.connect-timeout")))
	fmt.Fprintf(&b, "  ServerAliveInterval %d\n", ceilSeconds(f.Config.Duration("ssh.server-alive-interval")))
	fmt.Fprintf(&b, "  ServerAliveCountMax %d\n", f.Config.Int("ssh.server-alive-count-max"))
	// Managed defaults precede the user include, so ~/.ssh/config cannot turn
	// connection reuse back on. A platform that enables reuse overrides these
	// on the command line (controlArgs).
	b.WriteString("  ControlMaster no\n  ControlPath none\n  ControlPersist no\n")
	b.WriteString("  NumberOfPasswordPrompts 1\n  LogLevel ERROR\n")
	if f.Config.Bool("ssh.include-user-config") {
		userConfig := filepath.Join(f.Home, ".ssh", "config")
		if st, err := os.Stat(userConfig); err == nil && st.Mode().IsRegular() {
			fmt.Fprintf(&b, "\nInclude %s\n", sshQuote(userConfig))
		}
	}
	return b.String(), nil
}
func sshQuote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
func ceilSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

// childEnvironment is osutil.ChildEnvironment under the configured
// allow-list; the launcher uses the same filter.
func childEnvironment(cfg configload.Snapshot, extra ...string) []string {
	return osutil.ChildEnvironment(cfg.Strings("security.child-environment-allowlist"), extra...)
}

// FindAskpass locates the karvi-askpass helper as a session would, without
// running it: the explicit path, KARVI_ASKPASS_PATH,
// beside the executable, then PATH.
func FindAskpass(explicit string) (string, error) { return findAskpass(explicit) }

func findAskpass(explicit string) (string, error) {
	if explicit != "" {
		if p, err := exec.LookPath(explicit); err == nil {
			return p, nil
		}
	}
	if p := os.Getenv("KARVI_ASKPASS_PATH"); p != "" {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p, nil
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, n := range []string{"karvi-askpass", "karvi-askpass-linux-amd64"} {
			p := filepath.Join(dir, n)
			if st, e := os.Stat(p); e == nil && st.Mode().Perm()&0111 != 0 {
				return p, nil
			}
		}
	}
	return exec.LookPath("karvi-askpass")
}

func (d *Driver) debugf(format string, args ...any) {
	if d.f.Debug != nil {
		d.f.Debug(fmt.Sprintf(format, args...))
	}
}

// processEnd ends an OpenSSH process from the askpass broker's End hook
// (askpass.Hooks.End): at once, while OpenSSH waits on the helper's answer,
// so it sends the device none and tries no other method. It sends SIGTERM:
// OpenSSH ends at once during authentication, and a login's restores the
// terminal. A request without the token may end it
// before OpenSSH has started, which then ends it as it starts; one that
// never starts leaves nothing to end.
type processEnd struct {
	mu    sync.Mutex
	proc  *os.Process
	ended bool
}

func newProcessEnd() *processEnd { return &processEnd{} }

// started records the started OpenSSH process, ending it if end came first.
func (e *processEnd) started(p *os.Process) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.proc = p
	if e.ended {
		_ = p.Signal(syscall.SIGTERM)
	}
}

// end ends the process started records, or the one it will.
func (e *processEnd) end() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ended = true
	if e.proc != nil {
		_ = e.proc.Signal(syscall.SIGTERM)
	}
}

// callbackMaterial is the askpass broker's material (askpass.Material): the
// login's username and password.
type callbackMaterial struct {
	username string
	password func(func([]byte) error) error
}

func (m callbackMaterial) UsernameSet() bool                        { return m.username != "" }
func (m callbackMaterial) PasswordSet() bool                        { return m.password != nil }
func (m callbackMaterial) WithUsername(fn func([]byte) error) error { return fn([]byte(m.username)) }
func (m callbackMaterial) WithPassword(fn func([]byte) error) error {
	if m.password == nil {
		return errorcodes.Errorf("secret_password_unset", "password unset")
	}
	return m.password(fn)
}

func failed(start time.Time, code, cat string, external bool, err error) platform.Result {
	return platform.Result{StartedAt: start, EndedAt: time.Now(), ErrorCode: code, ErrorCategory: cat, External: external, Err: err}
}

// openSSHFailure is one of the forms of OpenSSH's own lines saying why a
// session failed, matched to the whole line as OpenSSH prints it, and the
// classification it gives.
type openSSHFailure struct {
	form           *regexp.Regexp
	code, category string
	retryable      bool
}

// connectPrefix starts OpenSSH's line for a connection it could not make:
// to the device, or a jump host's to its target; errorPrefix also its lines
// for a read, the identification exchange, or the disconnect it sends.
const (
	connectPrefix = `(ssh: connect to host \S+ port \S+|channel \d+: open failed: connect failed)`
	errorPrefix   = `(ssh: connect to host \S+ port \S+|channel \d+: open failed: connect failed|(kex|ssh)_exchange_identification: read|Read from remote host \S+|client_loop: send disconnect|(\S+: )?Connection (to|from) \S+ port \d+)`
)

// hostKeyFailures are OpenSSH's lines naming why it refused the device's
// key, in the order a key is judged: revoked, changed, unknown.
var hostKeyFailures = []openSSHFailure{
	{form: regexp.MustCompile(`^(@ +WARNING: REVOKED HOST KEY DETECTED! +@|The \S+ host key for .+ is marked as revoked\.|\S+ host key for .+ was revoked and you have requested strict checking\.|Host key \S+ \S+ revoked by file .+)$`), code: "host_key_revoked", category: "connection", retryable: false},
	{form: regexp.MustCompile(`^(@ +WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! +@|Host key for .+ has changed and you have requested strict checking\.|Offending \S+ key in .+:\d+)$`), code: "host_key_changed", category: "connection", retryable: false},
	{form: regexp.MustCompile(`^No \S+ host key is known for .+ and you have requested strict checking\.$`), code: "host_key_not_enrolled", category: "connection", retryable: false},
}

// hostKeyVerificationFailed is OpenSSH's last line of every host-key
// refusal: its code is that of the host-key line before it, or
// host_key_not_enrolled.
var hostKeyVerificationFailed = regexp.MustCompile(`^Host key verification failed\.$`)

// openSSHFailures are the other forms. A login the device refused is every
// method refused ("user@host: Permission denied (methods).", or without
// "user@host: "), a password to try again, OpenSSH's own "Authentication
// failed.", or the device's disconnect for an authentication failure
// (OpenSSH's "Disconnected from" after it is in no form). A disconnect for
// another reason, and a failed negotiation, whose offer sessionFailure
// reads, end the session with no specific cause.
var openSSHFailures = []openSSHFailure{
	{form: regexp.MustCompile(`^(Master refused session request: .*|channel \d+: open failed: administratively prohibited(: .*)?)$`), code: "ssh_session_channel_refused", category: "connection", retryable: false},
	{form: regexp.MustCompile(`^((\S+@\S+: )?Permission denied \(.*\)\.|Permission denied, please try again\.|Authentication failed\.|Received disconnect from \S+ port \d+:\d+: (Authentication failed|Too many authentication failures)\b.*)$`), code: "authentication_failed", category: "authentication", retryable: false},
	{form: regexp.MustCompile(`^Timeout, server \S+ not responding\.$`), code: "session_keepalive_timeout", category: "connection", retryable: true},
	{form: regexp.MustCompile(`^(` + errorPrefix + `: (Connection|Operation) timed out|Connection timed out during banner exchange)$`), code: "connection_timeout", category: "connection", retryable: true},
	{form: regexp.MustCompile(`^` + connectPrefix + `: Connection refused$`), code: "connection_refused", category: "connection", retryable: true},
	// Permission denied on the connect is this host's refusal (a local
	// firewall or security policy): the device is unreachable from here.
	{form: regexp.MustCompile(`^(` + connectPrefix + `: (No route to host|Network is unreachable)|ssh: connect to host \S+ port \S+: Permission denied)$`), code: "network_unreachable", category: "connection", retryable: true},
	{form: regexp.MustCompile(`^\S+: Could not resolve hostname .+: .+$`), code: "ssh_name_resolution_error", category: "name_resolution", retryable: false},
	{form: regexp.MustCompile(`^(Received disconnect from \S+ port \d+:\d+: .*|Unable to negotiate with \S+ port \d+: .+\. Their offer: .*)$`), code: "ssh_process_failed", category: "connection", retryable: false},
}

// lastFailure is the last line of diagnostic in one of OpenSSH's forms for
// a failure, and its classification; ok is false when there is none. A
// device's banner, which OpenSSH writes during authentication on the same
// stream, comes before OpenSSH's own failure, and a line in no form (the
// device's text, OpenSSH's closing lines) decides nothing.
func lastFailure(diagnostic string) (line string, failure openSSHFailure, ok bool) {
	lines := strings.Split(diagnostic, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		text := strings.TrimRight(lines[i], "\r")
		if hostKeyVerificationFailed.MatchString(text) {
			for j := i - 1; j >= 0; j-- {
				for _, f := range hostKeyFailures {
					if f.form.MatchString(strings.TrimRight(lines[j], "\r")) {
						return text, f, true
					}
				}
			}
			return text, hostKeyFailures[len(hostKeyFailures)-1], true
		}
		for _, forms := range [][]openSSHFailure{hostKeyFailures, openSSHFailures} {
			for _, f := range forms {
				if f.form.MatchString(text) {
					return text, f, true
				}
			}
		}
	}
	return "", openSSHFailure{}, false
}

// classify is the code, category, externality, and retryability of the
// failure OpenSSH's diagnostics say (lastFailure), ssh_process_failed when
// none is said.
func classify(stderr string, err error) (string, string, bool, bool) {
	if _, f, ok := lastFailure(stderr); ok {
		return f.code, f.category, true, f.retryable
	}
	return "ssh_process_failed", "connection", true, false
}

func safeDiagnostic(stderr string, err error) string {
	var kept []string
	for _, line := range strings.Split(strings.ReplaceAll(stderr, "\r", ""), "\n") {
		// OpenSSH's closing line at LogLevel INFO and above is not the
		// failure; its first-contact line never reaches the diagnostics
		// (authFilter, masterLines).
		if closedLine.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	s := strings.TrimSpace(strings.Join(kept, "\n"))
	if s == "" {
		return err.Error()
	}
	// OpenSSH's failure is its last line: the message keeps the end, from a
	// line's start.
	if len(s) > 2048 {
		tail := s[len(s)-2048:]
		if i := strings.IndexByte(tail, '\n'); i >= 0 && i < len(tail)-1 {
			tail = tail[i+1:]
		}
		s = "..." + tail
	}
	return s
}

// offeredAlgorithms is the device's lists filtered to what the binary
// implements, the host-key list then filtered by the trust store.
func (f Factory) offeredAlgorithms(implements func(sshalgorithms.Kind, string) bool) (sshalgorithms.Lists, error) {
	lists := f.Algorithms
	if len(lists) == 0 {
		lists = f.Config.SSHAlgorithms()
	}
	offered, err := lists.Offer("system", implements)
	if err != nil {
		return nil, err
	}
	if f.hostKey.Mode == "" {
		policy, err := hostkey.Resolve(f.Config.String("ssh.host-key-policy"), f.Config.String("ssh.known-hosts-file"), f.Home, f.BaseDir)
		if err != nil {
			return nil, err
		}
		f.hostKey = policy
	}
	if offered[sshalgorithms.HostKey], err = f.hostKey.HostKeyAlgorithms(offered[sshalgorithms.HostKey], f.hostKeyIdentity); err != nil {
		return nil, err
	}
	return offered, nil
}
