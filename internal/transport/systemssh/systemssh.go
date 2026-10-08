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
	"strconv"
	"strings"
	"sync"
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
	// Timeouts are the invocation's (platform.Timeouts); a zero field
	// falls back to Config.
	Timeouts platform.Timeouts
	// Spool is the session's spool: the directory,
	// the threshold, and the activity; the device is filled per session.
	Spool   devsession.Spool
	Debug   func(string)
	hostKey hostkey.Policy
	// Algorithms are the device's algorithm lists; empty means the
	// configuration's global lists.
	Algorithms sshalgorithms.Lists
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
	// the stored one beside the connection, and a difference, or a
	// comparison that could not complete, is the device's notice.
	if req.HostKeyNotice != nil {
		if m, nc := hostkey.CompareInsecure(ctx, policy, host, req.Address, int(req.Port)); m != nil {
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
	enableTimeout := platform.Pick(d.f.Timeouts.Enable, d.f.Config.Duration("execution.enable-timeout"))
	if enableTimeout <= 0 {
		enableTimeout = 10 * time.Second
	}
	// The ssh process dials, authenticates, and starts the shell in one
	// stretch karvi cannot see into, so the first prompt is awaited for the
	// connect and prompt timeouts together.
	session, err := devsession.Open(ctx, stream, devsession.Options{
		Definition: d.req.Definition, EnableSecret: d.req.EnablePassword, MaxOutputBytes: d.f.MaxOutputBytes, Spool: d.f.Spool.ForRequest(d.req), InFlightBytes: d.req.InFlightBytes,
		LoginTimeout: connectTimeout + promptTimeout, EnableTimeout: enableTimeout, PromptTimeout: promptTimeout,
		Debug: d.f.Debug,
	})
	// The one-use askpass broker serves the authentication, which is over
	// once the first prompt has arrived (or the open has failed).
	broker.Close()
	if err != nil {
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
		d.debugf("system SSH exec master failed code=%s diagnostic=%q", errorcodes.Of(err), compactDiagnostic(master.diagnostics.String()))
		return err
	}
	d.master = master
	d.exec = devsession.OpenExec(&execConn{m: master}, devsession.ExecOptions{Definition: d.req.Definition, MaxOutputBytes: d.f.MaxOutputBytes, Spool: d.f.Spool.ForRequest(d.req), InFlightBytes: d.req.InFlightBytes, Debug: d.f.Debug})
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
	promptTimeout = platform.Pick(d.f.Timeouts.Prompt, d.f.Config.Duration("execution.prompt-timeout"))
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
// notice under accept-new, the one policy that stores a key; nil otherwise:
// under insecure the trust store is /dev/null and OpenSSH says the same line
// of a key it did not keep.
func (d *Driver) hostKeyEnrolled() func(label string) {
	if d.f.hostKey.Mode != hostkey.AcceptNew || d.req.HostKeyNotice == nil {
		return nil
	}
	return func(label string) {
		d.req.HostKeyNotice(platform.HostKeyNotice{Code: platform.HostKeyEnrolled, Label: label})
	}
}

// firstContact is a login's first-contact check under accept-new: the
// login runs OpenSSH at LogLevel ERROR, which says nothing of a key it
// stores, so the trust store is read for the device's entry before the
// session and, when it has none, once more: at the first askpass request,
// when OpenSSH has finished key exchange and stored the key if it was to,
// else when the session ends (a login by keys alone asks nothing). A key
// found then is the request's host_key_enrolled notice. Nil when there is
// nothing to check. Neither read waits on the store's lock.
func (d *Driver) firstContact() func() {
	enrolled := d.hostKeyEnrolled()
	if enrolled == nil {
		return nil
	}
	file, identity := d.f.hostKey.KnownHostsFile, d.f.hostKeyIdentity
	if types, err := hostkey.EnrolledTypes(file, identity); err != nil || len(types) > 0 {
		return nil
	}
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
		c.Timeout, c.TimeoutSource = platform.Pick(d.f.Timeouts.Command, d.f.Config.Duration("execution.command-timeout")), platform.SessionTimeout
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
// still forced, so password input never competes with device stdin.
func (d *Driver) Interactive(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	args := d.baseArgs()
	args = append(args, "-tt", d.req.Address)
	cmd := exec.CommandContext(ctx, d.binary, args...)
	material := callbackMaterial{username: d.req.Username, password: d.req.Password, enable: d.req.EnablePassword}
	firstContact := d.firstContact()
	broker, err := askpass.Start(d.f.ScratchDir, material, firstContact)
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
		err = <-waited
	}
	if firstContact != nil {
		firstContact()
	}
	if err != nil {
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
	// OpenSSH's own preference for known key types.
	offered := f.offered
	if offered == nil {
		var err error
		if offered, err = f.offeredAlgorithms(func(sshalgorithms.Kind, string) bool { return true }); err != nil {
			return "", err
		}
	}
	b.WriteString("Host *\n")
	fmt.Fprintf(&b, "  StrictHostKeyChecking %s\n", strict)
	fmt.Fprintf(&b, "  HostKeyAlgorithms %s\n", strings.Join(offered[sshalgorithms.HostKey], ","))
	fmt.Fprintf(&b, "  KexAlgorithms %s\n", strings.Join(offered[sshalgorithms.Kex], ","))
	fmt.Fprintf(&b, "  Ciphers %s\n", strings.Join(offered[sshalgorithms.Ciphers], ","))
	fmt.Fprintf(&b, "  MACs %s\n", strings.Join(offered[sshalgorithms.MACs], ","))
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
	// keyboard-interactive and password, both answered with the password
	// (a server may allow keyboard-interactive and refuse password).
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

type callbackMaterial struct {
	username         string
	password, enable func(func([]byte) error) error
}

func (m callbackMaterial) UsernameSet() bool                        { return m.username != "" }
func (m callbackMaterial) PasswordSet() bool                        { return m.password != nil }
func (m callbackMaterial) EnablePasswordSet() bool                  { return m.enable != nil }
func (m callbackMaterial) WithUsername(fn func([]byte) error) error { return fn([]byte(m.username)) }
func (m callbackMaterial) WithPassword(fn func([]byte) error) error {
	if m.password == nil {
		return errorcodes.Errorf("secret_password_unset", "password unset")
	}
	return m.password(fn)
}
func (m callbackMaterial) WithEnablePassword(fn func([]byte) error) error {
	if m.enable == nil {
		return errorcodes.Errorf("secret_enable_password_unset", "enable password unset")
	}
	return m.enable(fn)
}
func (m callbackMaterial) Destroy() {}

func failed(start time.Time, code, cat string, external bool, err error) platform.Result {
	return platform.Result{StartedAt: start, EndedAt: time.Now(), ErrorCode: code, ErrorCategory: cat, External: external, Err: err}
}
func classify(stderr string, err error) (string, string, bool, bool) {
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "master refused session request"),
		strings.Contains(s, "administratively prohibited") && strings.Contains(s, "channel"):
		return "ssh_session_channel_refused", "connection", true, false
	case strings.Contains(s, "remote host identification has changed"),
		strings.Contains(s, "offending ") && strings.Contains(s, "host key"):
		return "host_key_changed", "connection", true, false
	case strings.Contains(s, "no ") && strings.Contains(s, "host key is known"),
		strings.Contains(s, "host key verification failed"):
		return "host_key_not_enrolled", "connection", true, false
	case strings.Contains(s, "permission denied"), strings.Contains(s, "authentication failed"):
		return "authentication_failed", "authentication", true, false
	case strings.Contains(s, "timeout, server ") && strings.Contains(s, " not responding"):
		// ServerAliveCountMax keepalives went unanswered.
		return "session_keepalive_timeout", "connection", true, true
	case strings.Contains(s, "connection timed out"), strings.Contains(s, "operation timed out"):
		return "connection_timeout", "connection", true, true
	case strings.Contains(s, "connection refused"):
		return "connection_refused", "connection", true, true
	case strings.Contains(s, "no route to host"), strings.Contains(s, "network is unreachable"):
		return "network_unreachable", "connection", true, true
	case strings.Contains(s, "could not resolve hostname"):
		return "ssh_name_resolution_error", "name_resolution", true, false
	default:
		return "ssh_process_failed", "connection", true, false
	}
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
	if len(s) > 2048 {
		s = s[:2048] + "..."
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
