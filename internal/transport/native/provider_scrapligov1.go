package native

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/adapters/scrapligov1"
	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/platform"
)

func init() {
	register("scrapligo-v1", openScrapliGoV1, "generic", "cisco_iosxe", "cisco_iosxr", "cisco_nxos", "juniper_junos", "arista_eos", "linux")
}

// openScrapliGoV1 resolves the host-key policy; the connection and the
// device session open in Prepare.
func openScrapliGoV1(_ context.Context, f Factory, req platform.OpenRequest) (platform.Driver, error) {
	policy, err := hostkey.Resolve(f.Config.String("ssh.host-key-policy"), f.Config.String("ssh.known-hosts-file"), f.Home, f.BaseDir)
	if err != nil {
		return nil, err
	}
	if f.MaxOutputBytes <= 0 {
		f.MaxOutputBytes = f.Config.Int64("output.max-command-bytes")
	}
	return &scrapligoDriver{f: f, req: req, policy: policy}, nil
}

// scrapligoDriver is one device over scrapligo-v1: karvi's connection and
// karvi's device session, as the system transport's driver is.
type scrapligoDriver struct {
	f       Factory
	req     platform.OpenRequest
	policy  hostkey.Policy
	session *devsession.Session
	// exec is the exec device's session in place of the shell's: one
	// connection, a channel per command.
	exec *devsession.ExecSession
	// auth is the connection's own account of how it authenticated.
	auth platform.AuthReporter
}

func (d *scrapligoDriver) debugf(format string, args ...any) {
	if d.f.Debug != nil {
		d.f.Debug(fmt.Sprintf(format, args...))
	}
}

// Prepare dials, runs the handshake, starts the shell, and opens the device
// session: the first prompt, privilege, and paging.
func (d *scrapligoDriver) Prepare(ctx context.Context) error {
	host := d.req.Metadata["canonical_name"]
	if host == "" {
		host = d.req.Address
	}
	promptTimeout := durationOr(platform.Pick(d.f.Timeouts.Prompt, d.f.Config.Duration("execution.prompt-timeout")), 10*time.Second)
	d.debugf("native SSH session starting implementation=scrapligo-v1 target=%q address=%q port=%d host_key_policy=%s host_key_identity=%q channel=%s", host, d.req.Address, d.req.Port, d.policy.Mode, hostkey.Identity(host, int(d.req.Port)), d.channel())
	dial := scrapligov1.DialRequest{
		Host: host, Address: d.req.Address, Port: int(d.req.Port), Username: d.req.Username,
		Password:         d.req.Password,
		Keys:             d.req.Keys,
		Policy:           d.policy,
		ConnectTimeout:   durationOr(d.f.Config.Duration("native-ssh.connect-timeout"), 10*time.Second),
		HandshakeTimeout: durationOr(d.f.Config.Duration("native-ssh.handshake-timeout"), 10*time.Second),
		// As ssh.server-alive-* on the system transport: zero sends none.
		KeepaliveInterval: d.f.Config.Duration("native-ssh.keepalive-interval"),
		KeepaliveCountMax: d.f.Config.Int("native-ssh.keepalive-count-max"),
		Term:              terminalType(d.f.Config.Strings("security.child-environment-allowlist")),
		Algorithms:        d.f.Algorithms,
		Warn:              d.f.Warn,
		Debug:             d.f.Debug,
		Enrolled:          d.req.HostKeyEnrolled,
	}
	if d.channel() == platform.ChannelExec {
		// Ready once authenticated: no first prompt, privilege, or paging.
		conn, err := scrapligov1.DialExec(ctx, dial)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			d.debugf("native SSH session open failed target=%q code=%s", host, errorcodes.Of(err))
			return err
		}
		d.auth = conn
		d.exec = devsession.OpenExec(conn, devsession.ExecOptions{Definition: d.req.Definition, MaxOutputBytes: d.f.MaxOutputBytes, Spool: d.f.Spool.ForRequest(d.req), InFlightBytes: d.req.InFlightBytes, Debug: d.f.Debug})
		d.debugf("native SSH session ready target=%q channel=exec", host)
		return nil
	}
	stream, err := scrapligov1.Dial(ctx, dial)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d.debugf("native SSH session open failed target=%q code=%s", host, errorcodes.Of(err))
		return err
	}
	d.auth, _ = stream.(platform.AuthReporter)
	session, err := devsession.Open(ctx, stream, devsession.Options{
		Definition: d.req.Definition, EnableSecret: d.req.EnablePassword, MaxOutputBytes: d.f.MaxOutputBytes, Spool: d.f.Spool.ForRequest(d.req), InFlightBytes: d.req.InFlightBytes,
		LoginTimeout: promptTimeout, EnableTimeout: durationOr(platform.Pick(d.f.Timeouts.Enable, d.f.Config.Duration("execution.enable-timeout")), 10*time.Second), PromptTimeout: promptTimeout,
		Debug: d.f.Debug,
	})
	if err != nil {
		return err
	}
	d.session = session
	d.debugf("native SSH session ready target=%q prompt=%q level=%q", host, session.Prompt(), session.Level())
	return session.Prepare(ctx)
}

// channel is the target's channel, shell when the request names none.
func (d *scrapligoDriver) channel() string {
	if d.req.Channel == platform.ChannelExec {
		return platform.ChannelExec
	}
	return platform.ChannelShell
}

// Usable is the prepared session's answer; a driver never prepared is not
// usable.
func (d *scrapligoDriver) Usable() bool {
	if d.exec != nil {
		return d.exec.Usable()
	}
	return d.session != nil && d.session.Usable()
}

// The executor finds the set-up lines by a type assertion, which a renamed
// method would fail silently; this fails the build instead.
var _ platform.SetupReporter = (*scrapligoDriver)(nil)

// SetupLines is the session's set-up as it was sent (platform.SetupReporter);
// an exec device sends none.
func (d *scrapligoDriver) SetupLines() []platform.SetupLine {
	if d.session == nil {
		return nil
	}
	return d.session.SetupLines()
}

var _ platform.AuthReporter = (*scrapligoDriver)(nil)

// AuthMethod is the method that authenticated karvi's connection
// (platform.AuthReporter).
func (d *scrapligoDriver) AuthMethod() string {
	if d.auth == nil {
		return ""
	}
	return d.auth.AuthMethod()
}

// Execute sends one command through the prepared session.
func (d *scrapligoDriver) Execute(ctx context.Context, c platform.Command) platform.Result {
	if c.Timeout <= 0 {
		c.Timeout, c.TimeoutSource = durationOr(platform.Pick(d.f.Timeouts.Command, d.f.Config.Duration("execution.command-timeout")), 120*time.Second), platform.SessionTimeout
	}
	if d.exec != nil {
		return d.exec.Execute(ctx, c)
	}
	if d.session == nil {
		now := time.Now()
		return platform.Result{StartedAt: now, EndedAt: now, ErrorCode: "command_session_lost", ErrorCategory: "connection", External: true, Err: errors.New("the device session was not prepared")}
	}
	return d.session.Execute(ctx, c)
}

// Close sends the platform's exit commands on a usable session and closes
// the connection; an exec device's connection closes with no command.
func (d *scrapligoDriver) Close() error {
	if d.exec != nil {
		return d.exec.Close()
	}
	if d.session == nil {
		return nil
	}
	return d.session.Close()
}

func durationOr(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}

// terminalType is TERM from this process's environment when the child
// environment allowlist passes it, as the system transport's ssh -tt sends
// it.
func terminalType(allowlist []string) string {
	for _, name := range allowlist {
		if name == "TERM" {
			return os.Getenv("TERM")
		}
	}
	return ""
}
