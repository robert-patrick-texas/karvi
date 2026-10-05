// Package platform defines reusable network-platform contracts. Concrete
// driver adapters and prompt mechanics remain internal.
package platform

import (
	"context"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

var transportSelectorPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// PrivilegeLevel is one prompt level of a platform: the pattern its prompt
// matches and how the level is reached from the one below it. The built-in
// levels are scrapligo-v1's platform definitions; a
// level's Pattern is matched against the final line the device sent.
type PrivilegeLevel struct {
	Name           string `json:"name"`
	Pattern        string `json:"pattern"`
	Previous       string `json:"previous,omitempty"`
	Escalate       string `json:"escalate,omitempty"`
	EscalateAuth   bool   `json:"escalate_auth,omitempty"`
	EscalatePrompt string `json:"escalate_prompt,omitempty"`
	Deescalate     string `json:"deescalate,omitempty"`
}

type Definition struct {
	Name             string `json:"name"`
	Driver           string `json:"driver"`
	DefaultTransport string `json:"default_transport"`
	SSHPort          uint16 `json:"ssh_port"`
	TelnetPort       uint16 `json:"telnet_port"`
	// PrivilegedLevel names the level the session works at: the driver's
	// level name, one of PrivilegeLevels when the platform
	// declares them.
	PrivilegedLevel string `json:"privileged_level"`
	RequiresEnable  bool   `json:"requires_enable"`
	LegacyClass     string `json:"legacy_class"`
	SessionCap      int    `json:"session_cap"`
	// Channel is what karvi asks of the SSH session channel: ChannelShell,
	// the interactive shell of the device session, or ChannelExec, one exec
	// request per command. Every built-in is shell (linux becomes exec when
	// the exec channel is built); a [platform.NAME] table may set either
	// word on any driver, and a definition that leaves it unset is shell.
	Channel string `json:"channel"`
	// Fallback is what follows the credential policy's backends, in order:
	// FallbackNetvars (NETUSER, NETPASS, NETENABLE), FallbackKeys (the
	// operator's login name and keys, ssh.identities), and FallbackPrompt
	// (the terminal prompts). The network built-ins and generic are netvars
	// then prompt, linux and linux_shell keys alone; a definition that
	// leaves it unset (nil) is netvars then prompt, and an empty list is no
	// fallback.
	Fallback       []string `json:"fallback"`
	PagingCommands []string `json:"paging_commands"`
	// CrunCommands is the platform's collection list: what a crun that
	// names no command on its command line sends to a device of this
	// platform. The configuration first, then the
	// version; a site's [platform.NAME] crun-commands replaces the list
	// whole. A platform without one (generic, linux) is refused at
	// planning by a crun that relies on it.
	CrunCommands []string `json:"crun_commands,omitempty"`
	// CrunFilters is the platform's drop list for the collection file:
	// regular expressions in Go's syntax, each
	// matched against an output line of a block; a matching line is left
	// out of the collection file and nowhere else. The built-in lists drop
	// the lines that carry no information about the device (the byte count,
	// the NTP clock period, the uptime, the free memory, the time of the
	// show) and never the stamp that says when the configuration last
	// changed; a site's [platform.NAME] crun-filters replaces the list
	// whole, and an empty array turns the filter off. CompileFilters is the
	// one compiler of the list.
	CrunFilters  []string `json:"crun_filters,omitempty"`
	ExitCommands []string `json:"exit_commands"`
	// PrivilegeLevels are the platform's prompt levels. A platform without
	// them has one prompt shape, PromptPattern, and no privilege step.
	PrivilegeLevels []PrivilegeLevel `json:"privilege_levels,omitempty"`
	// PromptPattern is the prompt of a platform without levels; empty means
	// the broad prompt shape (a line ending in >, #, or $).
	PromptPattern string `json:"prompt_pattern,omitempty"`
	// FailurePatterns are the substrings a command's output contains when
	// the device rejected it (device_command_error). Empty means the
	// platform reports no command errors (generic).
	FailurePatterns []string `json:"failure_patterns,omitempty"`
	// Base is the built-in the definition is resolved from: a built-in's own
	// base (its name, or linux for linux_shell), an alias's driver's base, or
	// generic. Transport admission reads it, never Driver.
	Base string `json:"-"`
}

// The words of a platform's channel.
const (
	ChannelShell = "shell"
	ChannelExec  = "exec"
)

// The words of a platform's fallback.
const (
	FallbackNetvars = "netvars"
	FallbackKeys    = "keys"
	FallbackPrompt  = "prompt"
)

// DefaultFallback is the fallback of a definition that leaves the field
// unset: the network fleet's, the variables and then the prompts.
func DefaultFallback() []string { return []string{FallbackNetvars, FallbackPrompt} }

// CheckFallback refuses a fallback list holding a word other than netvars,
// keys, and prompt, or one word twice.
func CheckFallback(list []string) error {
	seen := map[string]bool{}
	for _, w := range list {
		switch w {
		case FallbackNetvars, FallbackKeys, FallbackPrompt:
		default:
			return errorcodes.Errorf("config_platform_fallback_invalid", "fallback %q is not %q, %q, or %q", w, FallbackNetvars, FallbackKeys, FallbackPrompt)
		}
		if seen[w] {
			return errorcodes.Errorf("config_platform_fallback_invalid", "fallback %q is listed twice", w)
		}
		seen[w] = true
	}
	return nil
}

// levelNames lists the definition's prompt levels for messages.
func (d Definition) levelNames() []string {
	names := make([]string, 0, len(d.PrivilegeLevels))
	for _, l := range d.PrivilegeLevels {
		names = append(names, l.Name)
	}
	return names
}

// Level returns the named privilege level.
func (d Definition) Level(name string) (PrivilegeLevel, bool) {
	for _, l := range d.PrivilegeLevels {
		if l.Name == name {
			return l, true
		}
	}
	return PrivilegeLevel{}, false
}

// Validate normalises a definition's name and fills its defaults; it refuses a
// blank name, an invalid default transport, a session cap outside 1..32, and
// a privileged level that is not one of the definition's levels (a table's
// privileged-level names a level of the base definition; a base without
// levels, generic or linux, has none to name).
// Configuration validation calls it on every resolved table, so the refusal
// comes at karvi config validate and at load; the device session's prompt
// compiler keeps the same check as the backstop before a connection.
func (d *Definition) Validate() error {
	d.Name = Normalize(d.Name)
	if d.Name == "" {
		return errorcodes.Errorf("platform_name_blank", "platform name is blank")
	}
	if d.PrivilegedLevel != "" {
		if len(d.PrivilegeLevels) == 0 {
			return errorcodes.Errorf("platform_privilege_level_unknown", "platform %s: privileged level %q: the definition has no prompt levels", d.Name, d.PrivilegedLevel)
		}
		if _, ok := d.Level(d.PrivilegedLevel); !ok {
			return errorcodes.Errorf("platform_privilege_level_unknown", "platform %s: privileged level %q is not one of its levels (%s)", d.Name, d.PrivilegedLevel, strings.Join(d.levelNames(), ", "))
		}
	}
	if d.Driver == "" {
		d.Driver = d.Name
	}
	if d.DefaultTransport == "" {
		d.DefaultTransport = "native"
	}
	d.DefaultTransport = strings.ToLower(strings.TrimSpace(d.DefaultTransport))
	if d.DefaultTransport != "default" && d.DefaultTransport != "preferred" && d.DefaultTransport != "telnet" && !transportSelectorPattern.MatchString(d.DefaultTransport) {
		return errorcodes.Errorf("platform_default_transport_invalid", "invalid default transport selector %q", d.DefaultTransport)
	}
	if d.SSHPort == 0 {
		d.SSHPort = 22
	}
	if d.TelnetPort == 0 {
		d.TelnetPort = 23
	}
	if d.SessionCap == 0 {
		d.SessionCap = 3
	}
	if d.SessionCap < 1 || d.SessionCap > 32 {
		return errorcodes.Errorf("config_platform_session_cap_out_of_range", "session cap must be 1..32")
	}
	if d.LegacyClass == "" {
		d.LegacyClass = "none"
	}
	if d.Channel == "" {
		d.Channel = ChannelShell
	}
	if d.Channel != ChannelShell && d.Channel != ChannelExec {
		return errorcodes.Errorf("config_platform_channel_invalid", "platform %s: channel %q is neither %q nor %q", d.Name, d.Channel, ChannelShell, ChannelExec)
	}
	if d.Fallback == nil {
		d.Fallback = DefaultFallback()
	}
	if err := CheckFallback(d.Fallback); err != nil {
		return errorcodes.Errorf("config_platform_fallback_invalid", "platform %s: %s", d.Name, strings.TrimPrefix(err.Error(), "config_platform_fallback_invalid: "))
	}
	return nil
}

type OpenRequest struct {
	Address        string
	Port           uint16
	Username       string
	Password       func(func([]byte) error) error
	EnablePassword func(func([]byte) error) error
	// Keys are the private key files the credential offers, in order, by
	// path: the connecting process reads each at the connection. A request
	// with keys and no Password offers no password method.
	Keys       []string
	Definition Definition
	Timeout    time.Duration
	Metadata   map[string]string
	// InFlightBytes, when given, is where the session publishes the settled
	// bytes of the command it is running now, memory and spool, for the
	// scoreboard's target row: stored as bytes
	// settle, 0 when no command is in flight. Nothing is sent per chunk;
	// the scoreboard's snapshot reads it at its interval.
	InFlightBytes *atomic.Int64
}

// Command is one command to send.
//
// Blind declares the tolerance: the prompt may not return after the
// command, so it is awaited for Timeout, the blind wait (0 waiting not at
// all), and a wait that passes or a stream that ends is a success carrying
// the notice prompt_not_observed_after_blind_send. Without Blind, Timeout is
// the command's own and the prompt's absence is command_timeout.
// BlindReturns carriage returns follow the text in the same write, without a
// prompt match between them; a count above zero is always sent with Blind
// set, as the execution plan requires.
//
// Expectations are the operator's declared responses to the prompts the
// device asks during the command: tried in declared order on the last line
// received since the previous answer, each consumed once, a match answered
// with its Response and one carriage return. A command carries blind returns
// or expectations, never both; a blind command may carry expectations.
type Command struct {
	Text         string
	Timeout      time.Duration
	Blind        bool
	BlindReturns int
	Expectations []Expectation
}

// Expectation is one expect-and-send declaration, compiled: Pattern is
// matched against the last line of the device's output since the previous
// answer, with carriage returns and control sequences removed and the
// device's trailing space kept; Response is sent as written, followed by a
// carriage return, empty for a bare return.
type Expectation struct {
	Pattern  *regexp.Regexp
	Response string
}

// Notice is a typed non-error notice on a command's record.
type Notice struct {
	Code    string
	Message string
	Details map[string]any
}

// Spool is a response that passed the spool threshold: every recorded byte
// is in the file at Path, 0600 under spooldir, in
// the order and values the record takes, and the result's Output is empty.
// Bytes is the settled count; SHA256 the hex digest of the bytes as the
// reader hashed them, which the record's writer verifies when it streams
// the file; UTF8 whether they are valid UTF-8, the record's
// encoding utf-8, else base64. The executor removes the file once the
// record is durable, on every ending of the command.
type Spool struct {
	Path   string
	Bytes  int64
	SHA256 string
	UTF8   bool
}

type Result struct {
	// Output is the recorded bytes when they stayed in memory; Spool their
	// file when they passed the threshold (one of the two, never both). A
	// command cut short (a timeout, a lost session, the limit, a cancel)
	// hands back what settled by the cut the same way; a spool that could
	// not be written hands back nothing.
	Output []byte
	Spool  *Spool
	// Exec is an exec command's outcome: nil for a shell command and for an
	// exec command that did not run (its channel never opened).
	Exec *ExecResult
	// PromptBefore is the prompt the device showed when the statement was
	// sent, as matched from the device's own bytes (never inferred); Prompt
	// is the one that came back after it, empty when none did.
	PromptBefore     string
	Prompt           string
	PromptSource     string
	PromptObserved   *bool
	ConnectionReused *bool
	DeviceError      bool
	Notices          []Notice
	StartedAt        time.Time
	EndedAt          time.Time
	ErrorCode        string
	ErrorCategory    string
	External         bool
	Retryable        bool
	Err              error
}

// ExecResult is what an exec channel gave back beside stdout (the result's
// Output or Spool): stderr, in memory or in its own spool, and how the
// command ended. ExitStatus is nil when no status came back; ExitSignal is
// the signal's name, "unnamed" where the transport names none, "" for none.
type ExecResult struct {
	Stderr      []byte
	StderrSpool *Spool
	ExitStatus  *int
	ExitSignal  string
}

// SetupLine is one statement of karvi's own session set-up (the escalate
// command and the paging commands a driver sends in Prepare), as the device
// session saw it: the prompt it was sent at, matched from the device's own
// bytes and never inferred, the statement, and what the device answered.
// These statements have no command record; a job's output.TARGET.txt shows
// them before the device's first recorded statement.
//
// The enable secret is never in a SetupLine. It is not a statement, and the
// bytes the device sends between the secret and the next prompt are not
// kept either, so a device that echoes the secret cannot put it here.
type SetupLine struct {
	PromptBefore string
	Statement    string
	Output       string
}

// SetupReporter is implemented by a Driver that can say what set-up it
// sent. It is asked after Prepare, whether Prepare succeeded or failed: a
// failed enable is shown up to the statement that failed. A driver that
// does not implement it (a test's fake) has no set-up lines.
type SetupReporter interface {
	SetupLines() []SetupLine
}

// AuthReporter is implemented by a Driver that can say how the device
// authenticated it: "publickey", "keyboard-interactive", or "password"
// (OpenSSH's names), "" when it cannot tell. It is asked after a Prepare
// that succeeded.
type AuthReporter interface {
	AuthMethod() string
}
type Capabilities struct {
	Reusable      bool
	Interactive   bool
	ProcessBacked bool
}
type DriverFactory interface {
	Open(ctx context.Context, req OpenRequest) (Driver, error)
}
type Driver interface {
	Prepare(ctx context.Context) error
	Execute(ctx context.Context, command Command) Result
	// Usable reports whether the device session can take another command.
	// A write failure, a command timeout, a lost or failed read, or output
	// over the limit before the prompt returned ends it; a device error and
	// output over the limit after the prompt leave it usable. The executor
	// sends nothing more on an unusable session under any setting.
	Usable() bool
	Close() error
}

// The prompt levels and failure patterns of the built-in platforms are
// scrapligo-v1's platform definitions (assets/platforms/*.yaml in the
// module, read at v1.4.1; the pin is v1.4.2, whose one
// change is elsewhere, and the assets are no longer vendored since nothing
// imports them),
// the driver's vocabulary for level names.
// GenericPromptPattern is scrapligo's generic prompt; BroadPromptPattern is
// karvi's original prompt shape for a platform without a definition of its
// own (linux).
const (
	GenericPromptPattern = `(?i)^[a-z\d.\-@()/:_]{1,48}[#>$]\s*$`
	BroadPromptPattern   = `^[^\r\n]{1,240}[>#$][ \t]*$`
)

var ciscoIOSXELevels = []PrivilegeLevel{
	{Name: "exec", Pattern: `(?i)^[\w.\-@/:]{1,63}>$`},
	{Name: "privilege-exec", Pattern: `(?i)^[\w.\-@/:]{1,63}#$`, Previous: "exec", Escalate: "enable", EscalateAuth: true, EscalatePrompt: `(?i)^(?:enable\s){0,1}password:\s?$`, Deescalate: "disable"},
	{Name: "configuration", Pattern: `(?i)^[\w.\-@/:]{1,63}\([\+\w.\-@/:+]{0,32}\)#$`, Previous: "privilege-exec", Escalate: "configure terminal", Deescalate: "end"},
	{Name: "tclsh", Pattern: `(?i)^([\w.\-@/+>:]+\(tcl\)[>#]|\+>)$`, Previous: "privilege-exec", Escalate: "tclsh", Deescalate: "tclquit"},
}

var ciscoIOSXRLevels = []PrivilegeLevel{
	{Name: "exec", Pattern: `(?i)^[\w.\-@/:]{1,63}#\s?$`},
	{Name: "configuration", Pattern: `(?i)^[\w.\-@/:]{1,63}\(config[\+\w.\-@/:]{0,32}\)#\s?$`, Previous: "exec", Escalate: "configure terminal", Deescalate: "end"},
	{Name: "configuration-exclusive", Pattern: `(?i)^[\w.\-@/:]{1,63}\(config[\w.\-@/:]{0,32}\)#\s?$`, Previous: "exec", Escalate: "configure exclusive", Deescalate: "end"},
	{Name: "run", Pattern: `(?i)^\[[^]]+\]\$\s*$`, Previous: "exec", Escalate: "run", Deescalate: "logout"},
}

var ciscoNXOSLevels = []PrivilegeLevel{
	{Name: "exec", Pattern: `(?i)^[\w.\-]{1,63}>\s?$`},
	{Name: "privilege-exec", Pattern: `(?i)^[\w.\-]{1,63}#\s?$`, Previous: "exec", Escalate: "enable", EscalateAuth: true, EscalatePrompt: `(?i)^[pP]assword:\s?$`, Deescalate: "disable"},
	{Name: "configuration", Pattern: `(?i)^[\w.\-]{1,63}\(config[\+\w.\-@/:]{0,32}\)#\s?$`, Previous: "privilege-exec", Escalate: "configure terminal", Deescalate: "end"},
	{Name: "tclsh", Pattern: `(?i)(^[\w.\-@/:]{1,63}\-tcl#\s?$)|(^[\w.\-@/:]{1,63}\(config\-tcl\)#\s?$)|(^>\s?$)`, Previous: "privilege-exec", Escalate: "tclsh", Deescalate: "tclquit"},
}

var juniperJunosLevels = []PrivilegeLevel{
	{Name: "exec", Pattern: `(?i)^[\w\-@()/:\.]{1,63}>\s?$`},
	{Name: "configuration", Pattern: `(?i)^[\w\-@()/:\.]{1,63}#\s?$`, Previous: "exec", Escalate: "configure", Deescalate: "exit configuration-mode"},
	{Name: "shell", Pattern: `(?i)^.*[%$]\s?$`, Previous: "exec", Escalate: "start shell", Deescalate: "exit"},
}

var aristaEOSLevels = []PrivilegeLevel{
	{Name: "exec", Pattern: `(?i)^[\w.\-@()/: ]{1,63}>\s?$`},
	{Name: "privilege-exec", Pattern: `(?i)^[\w.\-@()/: ]{1,63}#\s?$`, Previous: "exec", Escalate: "enable", EscalateAuth: true, EscalatePrompt: `(?i)^[pP]assword:\s?$`, Deescalate: "disable"},
	{Name: "configuration", Pattern: `(?i)^[\w.\-@()/: ]{1,63}\(config[\w.\-@/:]{0,63}\)#\s?$`, Previous: "privilege-exec", Escalate: "configure terminal", Deescalate: "end"},
}

var (
	ciscoFailures = []string{"% Ambiguous command", "% Incomplete command", "% Invalid input detected", "% Unknown command"}
	nxosFailures  = []string{"% Ambiguous command", "% Incomplete command", "% Invalid input detected", "% Unknown command", "% Invalid command", "ERROR:"}
	junosFailures = []string{"is ambiguous", "No valid completions", "unknown command", "syntax error"}
	eosFailures   = []string{"% Ambiguous command", "% Error", "% Incomplete command", "% Invalid input", "% Cannot commit", "% Unavailable command"}
)

// The built-ins' fallbacks: the network fleet's variables and prompts, and
// a server's operator keys alone (the variables an operator exports for
// routers never reach a server unasked).
var (
	netFallback  = []string{FallbackNetvars, FallbackPrompt}
	keysFallback = []string{FallbackKeys}
)

var builtins = map[string]Definition{
	"generic":       {Name: "generic", Driver: "generic", DefaultTransport: "native", Channel: ChannelShell, Fallback: netFallback, SSHPort: 22, TelnetPort: 23, SessionCap: 3, ExitCommands: []string{"exit"}, PromptPattern: GenericPromptPattern},
	"cisco_iosxe":   {Name: "cisco_iosxe", Driver: "cisco_iosxe", DefaultTransport: "native", Channel: ChannelShell, Fallback: netFallback, SSHPort: 22, TelnetPort: 23, PrivilegedLevel: "privilege-exec", SessionCap: 5, PagingCommands: []string{"terminal length 0", "terminal width 512"}, CrunCommands: []string{"show running-config", "show version"}, CrunFilters: []string{`^Building configuration\.\.\.$`, `^Current configuration : \d+ bytes$`, `^ntp clock-period \d+$`, ` uptime is `, `^Load for five secs`, `^Time source is `}, ExitCommands: []string{"exit"}, PrivilegeLevels: ciscoIOSXELevels, FailurePatterns: ciscoFailures},
	"cisco_iosxr":   {Name: "cisco_iosxr", Driver: "cisco_iosxr", DefaultTransport: "native", Channel: ChannelShell, Fallback: netFallback, SSHPort: 22, TelnetPort: 23, PrivilegedLevel: "exec", SessionCap: 8, PagingCommands: []string{"terminal length 0", "terminal width 512"}, CrunCommands: []string{"show running-config", "show version"}, CrunFilters: []string{`^Building configuration\.\.\.$`, ` uptime is `}, ExitCommands: []string{"exit"}, PrivilegeLevels: ciscoIOSXRLevels, FailurePatterns: ciscoFailures},
	"cisco_nxos":    {Name: "cisco_nxos", Driver: "cisco_nxos", DefaultTransport: "native", Channel: ChannelShell, Fallback: netFallback, SSHPort: 22, TelnetPort: 23, PrivilegedLevel: "privilege-exec", SessionCap: 8, PagingCommands: []string{"terminal length 0", "terminal width 511"}, CrunCommands: []string{"show running-config", "show version"}, CrunFilters: []string{`^!Time: `, ` uptime is `}, ExitCommands: []string{"exit"}, PrivilegeLevels: ciscoNXOSLevels, FailurePatterns: nxosFailures},
	"juniper_junos": {Name: "juniper_junos", Driver: "juniper_junos", DefaultTransport: "native", Channel: ChannelShell, Fallback: netFallback, SSHPort: 22, TelnetPort: 23, PrivilegedLevel: "exec", SessionCap: 8, PagingCommands: []string{"set cli screen-length 0", "set cli screen-width 0"}, CrunCommands: []string{"show configuration", "show version"}, ExitCommands: []string{"exit"}, PrivilegeLevels: juniperJunosLevels, FailurePatterns: junosFailures},
	"arista_eos":    {Name: "arista_eos", Driver: "arista_eos", DefaultTransport: "native", Channel: ChannelShell, Fallback: netFallback, SSHPort: 22, TelnetPort: 23, PrivilegedLevel: "privilege-exec", SessionCap: 8, PagingCommands: []string{"terminal length 0", "terminal width 512"}, CrunCommands: []string{"show running-config", "show version"}, CrunFilters: []string{`^! Time: `, `^Uptime: `, `^Free memory: `}, ExitCommands: []string{"exit"}, PrivilegeLevels: aristaEOSLevels, FailurePatterns: eosFailures},
	"linux":         {Name: "linux", Driver: "linux", DefaultTransport: "native", Channel: ChannelShell, Fallback: keysFallback, SSHPort: 22, TelnetPort: 23, SessionCap: 10, ExitCommands: []string{"exit"}, PromptPattern: BroadPromptPattern},
	// linux_shell is linux's definition on the shell channel, for a server
	// whose security refuses exec: linux is its base, so it is admitted
	// wherever linux is, and its records name linux_shell.
	"linux_shell": {Name: "linux_shell", Driver: "linux_shell", Base: "linux", DefaultTransport: "native", Channel: ChannelShell, Fallback: keysFallback, SSHPort: 22, TelnetPort: 23, SessionCap: 10, ExitCommands: []string{"exit"}, PromptPattern: BroadPromptPattern},
}

// Builtin is the named built-in definition, its Base filled: the built-in's
// own base, else its name.
func Builtin(name string) (Definition, bool) {
	d, ok := builtins[strings.ToLower(name)]
	if ok && d.Base == "" {
		d.Base = d.Name
	}
	return d, ok
}
func Builtins() []Definition {
	order := []string{"generic", "cisco_iosxe", "cisco_iosxr", "cisco_nxos", "juniper_junos", "arista_eos", "linux", "linux_shell"}
	out := make([]Definition, 0, len(order))
	for _, n := range order {
		d, _ := Builtin(n)
		out = append(out, d)
	}
	return out
}
