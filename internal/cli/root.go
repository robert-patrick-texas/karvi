// Package cli implements the karvi process boundary. It deliberately keeps
// parsing separate from app use cases so daemon requests do not depend on CLI
// types or mutable global state.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/completion"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/helplayout"
	"github.com/robert-patrick-texas/karvi/internal/icmpgate"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

type globalOptions struct {
	configs, sets                          []string
	quiet, debug, debugShow, help, version bool
	ipv4, ipv6                             bool
	timezone, ansi                         string
}

func (g globalOptions) common() app.CommonOptions {
	values := map[string]configload.FlagValue{}
	if g.timezone != "" {
		setKey(values, "--timezone", "timezone", g.timezone)
	}
	if g.ansi != "" {
		setKey(values, "--ansi", "output.ansi", g.ansi)
	}
	if g.ipv4 {
		setKey(values, "--ipv4", "name.address-family-preference", "ipv4")
	} else if g.ipv6 {
		setKey(values, "--ipv6", "name.address-family-preference", "ipv6")
	}
	return app.CommonOptions{ConfigRoots: append([]string(nil), g.configs...), Sets: append([]string(nil), g.sets...), ConfigFlags: values, Quiet: g.quiet, Debug: g.debug, DebugShowSecret: g.debugShow}
}

// Main executes one invocation and returns the stable karvi process exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	code = exitcode.ExitGenericError
	defer func() {
		if v := recover(); v != nil {
			incident, _ := osutil.NewID(now())
			fmt.Fprintf(stderr, "internal panic incident=%s: %v\n", incident, v)
			if os.Getenv("KARVI_DEBUG_PANIC") != "" {
				stderr.Write(debug.Stack())
			}
			code = exitcode.ExitGenericError
		}
	}()
	// The completion word stands outside the grammar (complete.go): it is
	// taken before parsing, since the words it carries are the line being
	// completed, not this invocation's.
	if len(args) > 0 && args[0] == completion.Word {
		return completeMain(args[1:], stdout)
	}
	inv, err := Parse(args)
	if err != nil {
		return reportError(stderr, "cli_option_unknown", err)
	}
	if inv.Help {
		fmt.Fprint(stdout, helplayout.Layout(inv.helpText(), helpStyleFor(inv.Global, stdout)))
		return exitcode.ExitSuccess
	}
	if inv.Global.version {
		return renderVersion("text", stdout, stderr)
	}
	if inv.Path == "" {
		fmt.Fprint(stdout, helplayout.Layout(topHelp, helpStyleFor(inv.Global, stdout)))
		return exitcode.ExitSuccess
	}
	if len(inv.Pending) > 0 {
		o := inv.Pending[0]
		return usageError(stderr, "cli_option_unavailable", "--%s is in the specification but is not available in this executable", o.name)
	}
	if inv.Global.debugShow && !inv.Global.debug {
		return usageError(stderr, "debug_show_secrets_requires_debug", "--debug-show-secrets requires --debug")
	}
	if inv.Flag(optIPv4) && inv.Flag(optIPv6) {
		return usageError(stderr, "address_family_conflict", "--ipv4/--4 and --ipv6/--6 are mutually exclusive")
	}
	// Both ping flags are refused here, before any configuration loads,
	// hence before DNS, credentials, submission, ICMP, or a device.
	if inv.Flag(optPing) && inv.Flag(optNoPing) {
		return usageError(stderr, "ping_flag_conflict", "--ping and --noping are mutually exclusive")
	}
	if inv.Path == "login" && inv.Record != nil && inv.Global.debugShow {
		return usageError(stderr, "record_debug_show_secrets_conflict", "--debug-show-secrets is incompatible with transcript recording")
	}
	if inv.Path == "login" && inv.Record != nil && os.Getenv(loginTranscriptChildEnv) == "" {
		return recordedLogin(inv, args, stdin, stdout, stderr)
	}
	if inv.Path == "login" && os.Getenv(loginTranscriptChildEnv) != "" {
		// Under the recorder every stream is the pseudo-terminal that feeds the
		// transcript; diagnostics go to the wrapper's real stderr, passed as an
		// extra descriptor. script(1) holds that terminal raw for this
		// process's life, so each line feed is written after a carriage
		// return.
		if f := recorderDiagnostics(); f != nil {
			defer f.Close()
			stderr = osutil.RawTerminalLines(f)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	streams := app.IO{Stdin: stdin, Stdout: stdout, Stderr: stderr}
	switch inv.Path {
	case "version":
		return renderVersion(inv.String(optFormatTJ), stdout, stderr)
	case "command":
		return commandCommand(ctx, inv, streams)
	case "run", "crun":
		return commandRun(ctx, inv, streams)
	case "stream":
		return commandStream(ctx, inv, streams)
	case "login":
		return commandLogin(ctx, inv, streams)
	case "config generate":
		return configGenerate(inv, streams)
	case "config validate":
		return configValidate(inv, streams)
	case "config show":
		return configShow(inv, streams)
	case "config colors":
		return configColors(inv, streams)
	case "daemon start":
		return daemonStart(ctx, inv, streams)
	case "daemon serve":
		return serveDaemon(inv.Global, streams.Stderr)
	case "daemon status":
		return daemonStatus(ctx, inv, streams)
	case "daemon stop":
		return daemonStop(ctx, inv, streams)
	case "daemon restart":
		return daemonRestart(ctx, inv, streams)
	case "job cancel":
		return jobCancel(ctx, inv, streams)
	case "job follow":
		return jobFollow(ctx, inv, streams)
	case "setup shared":
		return setupShared(inv, streams)
	case "setup tab":
		return setupTab(inv, streams)
	case "watch":
		return commandWatch(ctx, inv, streams)
	}
	panic("unhandled command path " + inv.Path)
}

// common merges the global options with the same options given after the
// command word (--quiet, --debug, --ipv4, --ipv6 are accepted in both places).
func (inv *Invocation) common() app.CommonOptions {
	g := inv.Global
	g.quiet = g.quiet || inv.Flag(optQuiet)
	g.debug = g.debug || inv.Flag(optDebug)
	g.ipv4 = g.ipv4 || inv.Flag(optIPv4)
	g.ipv6 = g.ipv6 || inv.Flag(optIPv6)
	common := g.common()
	if inv.Set(optHostKeyPolicy) {
		setKey(common.ConfigFlags, longName(optHostKeyPolicy), "ssh.host-key-policy", inv.String(optHostKeyPolicy))
	}
	if inv.Set(optKnownHosts) {
		setKey(common.ConfigFlags, longName(optKnownHosts), "ssh.known-hosts-file", inv.String(optKnownHosts))
	}
	if inv.Set(optOrder) {
		setKey(common.ConfigFlags, longName(optOrder), "dispatch.order", inv.String(optOrder))
	}
	for _, d := range dispatchOptionKeys {
		if !inv.Set(d.opt) {
			continue
		}
		var v any = inv.String(d.opt)
		switch d.opt.typ {
		case typeInt:
			v = int64(inv.Int(d.opt))
		case typeDuration:
			v = inv.Duration(d.opt).String()
		}
		setKey(common.ConfigFlags, longName(d.opt), d.key, v)
	}
	// --ping and --noping set the effective network.ping-targets through the
	// lock-aware cli layer.
	if inv.Flag(optPing) {
		setKey(common.ConfigFlags, longName(optPing), "network.ping-targets", true)
	} else if inv.Flag(optNoPing) {
		setKey(common.ConfigFlags, longName(optNoPing), "network.ping-targets", false)
	}
	return common
}

// setKey records the value an option sets for its configuration key in the
// lock-aware cli layer, sourced to what set it: the option by its long name
// (longName), or the word that implies it (crun), which every message and
// config show then name.
func setKey(flags map[string]configload.FlagValue, source, key string, value any) {
	flags[key] = configload.FlagValue{Value: value, Option: source}
}

// longName is an option as a source names it: --NAME, the long name, which a
// shortcut (--dp) reaches as the option it stands for (--dispatch).
func longName(o *option) string { return "--" + o.name }

// dispatchOptionKeys are run's Dispatch options and the keys they stand
// for. Each is its key's override in the lock-aware cli layer, as --order
// is, so the key's range, its cross-key checks, and a site's lock apply to
// the option as they do to --set; the planner reads the keys alone.
// --dp, --dw, and --ds reach here as --dispatch.
var dispatchOptionKeys = []struct {
	opt *option
	key string
}{
	{optDispatch, "dispatch.default"},
	{optWorkers, "dispatch.parallel-workers"},
	{optStartWidth, "dispatch.wave-start-width"},
	{optMaxWidth, "dispatch.wave-max-width"},
	{optHaltCount, "dispatch.halt-on-error-count"},
	{optHaltPercent, "dispatch.halt-on-error-percent"},
	{optGateCount, "dispatch.wave-gate-error-count"},
	{optGatePercent, "dispatch.wave-gate-error-percent"},
	{optWaveDelay, "dispatch.wave-gate-timed-delay"},
}

func renderVersion(format string, stdout, stderr io.Writer) int {
	if format == "" {
		format = "text"
	}
	info := buildinfo.Current()
	info.ICMPMethod = icmpgate.Detect(icmpgate.DefaultOptions).Method
	switch format {
	case "json":
		b, _ := json.MarshalIndent(info, "", "  ")
		fmt.Fprintf(stdout, "%s\n", b)
	default:
		fmt.Fprintf(stdout, "%s %s\ncommit: %s\nbuild_time: %s\ngo: %s\nconfig_schema: %d\ncommand_record_schema: %d\nscoreboard_schema: %d\naudit_schema: %d\njob_schema: %d\ndaemon_ipc_schema: %d\nconfig_registry_schema: %d\nfips_mode: %s\nicmp_method: %s\nssh_transports:\n", info.Application, info.Version, info.Commit, info.BuildTime, info.GoVersion, info.ConfigSchemaVersion, info.CommandRecordSchemaVersion, info.ScoreboardSchemaVersion, info.AuditSchemaVersion, info.JobSchemaVersion, info.DaemonIPCSchemaVersion, info.ConfigRegistrySchemaVersion, info.FIPSMode, info.ICMPMethod)
		for _, transport := range info.SSHTransports {
			if transport.Version != "" {
				fmt.Fprintf(stdout, "  %s: %s (id=%s, linkage=%s)\n", transport.Name, transport.Version, transport.ID, transport.Linkage)
			} else {
				fmt.Fprintf(stdout, "  %s: external (id=%s, linkage=%s)\n", transport.Name, transport.ID, transport.Linkage)
			}
		}
	}
	return 0
}

func printResultError(result app.ActivityResult, stderr io.Writer) {
	if result.Error != "" {
		fmt.Fprintln(stderr, result.Error)
	}
}

// reportError prints err under the site code that names its cause, unless err
// carries a more specific registered code, and returns the registry exit.
func reportError(w io.Writer, code string, err error) int {
	err = errorcodes.Ensure(err, code)
	fmt.Fprintln(w, errorcodes.Message(err))
	return errorcodes.ExitAt(err, code)
}

// usageError reports an option value or combination check made after parsing
// under its own registered code.
func usageError(w io.Writer, code, format string, args ...any) int {
	return reportError(w, code, errorcodes.Errorf(code, format, args...))
}
