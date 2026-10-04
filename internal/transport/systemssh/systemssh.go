// Package systemssh is the process-backed OpenSSH adapter shared by direct
// command and run --transport system. It never places credentials in argv or
// plaintext environment variables.
package systemssh

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	Warn    func(string)
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
}
type Driver struct {
	f                                Factory
	req                              platform.OpenRequest
	binary, configPath, configDigest string
	session                          *devsession.Session
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
	hostkey.WarnInsecureSystem(ctx, policy, host, req.Address, int(req.Port), f.Warn)
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
	sum := sha256.Sum256([]byte(content))
	cf, err := os.CreateTemp(f.ScratchDir, "karvi-ssh-*.conf")
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
	return &Driver{f: f, req: req, binary: binary, configPath: cf.Name(), configDigest: hex.EncodeToString(sum[:])}, nil
}

// Prepare opens the device session: one interactive OpenSSH shell, the
// first prompt within the connect and prompt timeouts, then the session's
// privilege and paging steps. command and run share it.
func (d *Driver) Prepare(ctx context.Context) error {
	stream, broker, err := d.startShell(ctx)
	if err != nil {
		return err
	}
	connectTimeout := d.req.Timeout
	if connectTimeout <= 0 {
		connectTimeout = d.f.Config.Duration("ssh.connect-timeout")
	}
	if connectTimeout <= 0 {
		connectTimeout = 30 * time.Second
	}
	promptTimeout := d.f.Config.Duration("execution.prompt-timeout")
	if promptTimeout <= 0 {
		promptTimeout = 10 * time.Second
	}
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

func (d *Driver) Close() error {
	var sessionErr error
	if d.session != nil {
		sessionErr = d.session.Close()
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
func (d *Driver) Usable() bool { return d.session != nil && d.session.Usable() }

// The executor finds the set-up lines by a type assertion, which a renamed
// method would fail silently; this fails the build instead.
var _ platform.SetupReporter = (*Driver)(nil)

// SetupLines is the session's set-up as it was sent (platform.SetupReporter).
func (d *Driver) SetupLines() []platform.SetupLine { return d.session.SetupLines() }

// Execute sends one command through the prepared session.
func (d *Driver) Execute(ctx context.Context, c platform.Command) platform.Result {
	if d.session == nil {
		return failed(time.Now(), "command_session_lost", "connection", true, errors.New("the device session was not prepared"))
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = d.f.Config.Duration("execution.command-timeout")
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	c.Timeout = timeout
	return d.session.Execute(ctx, c)
}

// Interactive attaches the actual terminal to an OpenSSH process. Askpass is
// still forced, so password input never competes with device stdin.
func (d *Driver) Interactive(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	args := d.baseArgs()
	args = append(args, "-tt", d.req.Address)
	cmd := exec.CommandContext(ctx, d.binary, args...)
	material := callbackMaterial{username: d.req.Username, password: d.req.Password, enable: d.req.EnablePassword}
	broker, err := askpass.Start(d.f.ScratchDir, material)
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
	if err := cmd.Run(); err != nil {
		code, _, _, _ := classify(diagnostic.String(), err)
		d.debugf("system SSH interactive session failed code=%s diagnostic=%q", code, compactDiagnostic(diagnostic.String()))
		return fmt.Errorf("%s: %s", code, safeDiagnostic(diagnostic.String(), err))
	}
	d.debugf("system SSH interactive session completed target=%q", d.req.Metadata["canonical_name"])
	return nil
}
func (d *Driver) ConfigDigest() string { return d.configDigest }
func (d *Driver) baseArgs() []string {
	args := []string{"-F", d.configPath, "-o", "HostName=" + d.req.Address, "-o", "HostKeyAlias=" + d.f.hostKeyIdentity, "-o", "User=" + d.req.Username, "-o", "Port=" + strconv.Itoa(int(d.req.Port))}
	return append(args, d.controlArgs()...)
}

// controlArgs pins OpenSSH connection reuse off on the command line, where
// the first obtained value overrides both the generated file and
// ~/.ssh/config. One interactive shell is the device's whole session, so
// there is nothing for a master to share;
// the platform table's control-master and the ssh.control-* keys are inert.
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
	fmt.Fprintf(&b, "  PubkeyAuthentication %s\n", yesno(f.Config.Bool("ssh.pubkey-authentication")))
	fmt.Fprintf(&b, "  PasswordAuthentication %s\n", yesno(f.Config.Bool("ssh.password-authentication")))
	fmt.Fprintf(&b, "  KbdInteractiveAuthentication %s\n", yesno(f.Config.Bool("ssh.keyboard-interactive-authentication")))
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
func expandHome(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
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
		// OpenSSH's informational lines under LogLevel INFO are not the
		// failure.
		if strings.HasPrefix(line, "Warning: Permanently added ") || (strings.HasPrefix(line, "Connection to ") && strings.HasSuffix(line, " closed.")) {
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
