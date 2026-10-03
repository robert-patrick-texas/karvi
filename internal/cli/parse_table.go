package cli

// The parser table: every karvi command word, subcommand word, and option.
// The parser in parse.go matches arguments against it and handlers read
// parsed values through the option variables, so a name exists in exactly
// one place.
//
// The table lists every specified option, including options of features
// this executable does not implement yet. Those are marked pending: they
// take part in abbreviation resolution, so an abbreviation that is unique
// today stays unique when the feature ships, and giving one is refused with
// cli_option_unavailable.

type optKind int

const (
	kindFlag     optKind = iota // on/off; an inline value must be true or false
	kindValue                   // takes a value, inline with = or as the next argument
	kindOptional                // value only inline with =, as --record[=PATH]
)

type valueType int

const (
	typeString valueType = iota
	typeInt
	typeDuration
	typeEnum
)

type optRole int

const (
	roleNone        optRole = iota
	roleHelp                // --help / -h
	roleTarget              // --target: a target input that is a device name or glob
	roleTargetList          // --tl: a list of --target values in one argument
	roleTargetInput         // --tf, --tfr, --site, --device-group, --all: other target inputs
	roleCmd                 // --cmd: explicit device command
	roleCf                  // --cf: commands file
	roleDeclaration         // --expect, --blind, --blind-return: attach to the preceding --cmd
)

type option struct {
	name        string
	aliases     []string
	kind        optKind
	typ         valueType
	enum        []string
	repeat      bool
	role        optRole
	pending     bool
	placeholder string
	// input is the target input's kind where it is not the option's name:
	// --select-platform is the "platform" selector of the manifest's
	// selection (records.TargetInput), as --site is "site".
	input string
	// standsFor and standsValue make a flag a shortcut: --pi is --platform
	// cisco_iosxe. The parser hands over the option stood for with that
	// value, so a handler reads one option and never the shortcut.
	standsFor   *option
	standsValue string
}

func flagOpt(name string, aliases ...string) *option {
	return &option{name: name, aliases: aliases, kind: kindFlag}
}

func valueOpt(name, placeholder string, aliases ...string) *option {
	return &option{name: name, aliases: aliases, kind: kindValue, placeholder: placeholder}
}

func intOpt(name string) *option {
	return &option{name: name, kind: kindValue, typ: typeInt, placeholder: "N"}
}

func durationOpt(name string) *option {
	return &option{name: name, kind: kindValue, typ: typeDuration, placeholder: "DURATION"}
}

func enumOpt(name string, values ...string) *option {
	return &option{name: name, kind: kindValue, typ: typeEnum, enum: values}
}

// shortcutOpt is a flag standing for target with value (the platform
// shortcuts). Its name is a whole word of the table, so it matches exactly and
// takes part in abbreviation as every word does.
func shortcutOpt(name string, target *option, value string) *option {
	return &option{name: name, kind: kindFlag, standsFor: target, standsValue: value}
}

// inputKind is the Kind a target input of this option is recorded under.
func (o *option) inputKind() string {
	if o.input != "" {
		return o.input
	}
	return o.name
}

func (o *option) repeatable() *option  { o.repeat = true; return o }
func (o *option) as(r optRole) *option { o.role = r; return o }
func (o *option) notBuilt() *option    { o.pending = true; return o }

// Options shared by several modes. One variable per option, so a handler that
// reads inv.Flag(optEcho) cannot misspell the name.
var (
	optHelp   = flagOpt("help", "h").as(roleHelp)
	optIPv4   = flagOpt("ipv4", "4")
	optIPv6   = flagOpt("ipv6", "6")
	optDebug  = flagOpt("debug")
	optQuiet  = flagOpt("quiet")
	optTarget = valueOpt("target", "DEVICE", "host", "t").repeatable().as(roleTarget)
	optTf     = valueOpt("tf", "FILE|PATH|-").repeatable().as(roleTargetInput)
	optTfr    = valueOpt("tfr", "PATH").repeatable().as(roleTargetInput)
	// --tl LIST is a list of --target values in one argument: names
	// separated by commas or whitespace, the shell's quotes grouping a
	// whitespace list. The parser
	// hands each name to --target at the list's position, so a handler, the
	// plan, and the manifest see target inputs and never a list.
	optTl    = valueOpt("tl", "LIST").repeatable().as(roleTargetList)
	optSite  = valueOpt("site", "GLOB").repeatable().as(roleTargetInput)
	optGroup = valueOpt("device-group", "GLOB").repeatable().as(roleTargetInput)
	optAll   = flagOpt("all").as(roleTargetInput)
	// login, command, and run take the same target inputs and assemble one
	// target set.
	optExclude = valueOpt("exclude", "GLOB").repeatable()
	optOrder   = enumOpt("order", "default", "sorted", "shuffle", "random") // sets dispatch.order through the lock-aware flag layer
	optCmd     = valueOpt("cmd", "TEXT", "command", "c").repeatable().as(roleCmd)
	optCf      = valueOpt("cf", "PATH|-").as(roleCf)
	// --platform names the platform definition the activity's devices run
	// as, in login, command, and run alike; selecting inventory devices by
	// their platform is --select-platform, a target input at its position
	// in all three.
	optPlatform       = valueOpt("platform", "NAME")
	optSelectPlatform = func() *option {
		o := valueOpt("select-platform", "GLOB").repeatable().as(roleTargetInput)
		o.input = "platform"
		return o
	}()
	// The platform shortcuts: one per built-in network platform, and generic.
	// --pi is a whole word, so it is the shortcut and no longer a prefix of
	// --ping (--pin still is).
	optPI            = shortcutOpt("pi", optPlatform, "cisco_iosxe")
	optPN            = shortcutOpt("pn", optPlatform, "cisco_nxos")
	optPR            = shortcutOpt("pr", optPlatform, "cisco_iosxr")
	optPJ            = shortcutOpt("pj", optPlatform, "juniper_junos")
	optPA            = shortcutOpt("pa", optPlatform, "arista_eos")
	optPG            = shortcutOpt("pg", optPlatform, "generic")
	optAddress       = valueOpt("management-address", "IP", "address", "a")
	optPort          = intOpt("port")
	optTransport     = valueOpt("transport", "SELECTOR")
	optHostKeyPolicy = enumOpt("ssh-host-key-policy", "accept-new", "secure", "insecure")
	optKnownHosts    = valueOpt("ssh-known-hosts-file", "PATH|auto")
	optFormat        = enumOpt("format", "text", "jsonl", "json")
	optFormatTJ      = enumOpt("format", "text", "json")
	optEcho          = flagOpt("echo")
	optBorder        = flagOpt("border")
	optNoBorder      = flagOpt("noborder")
	optContinue      = flagOpt("continue-device-on-error")
	optPing          = flagOpt("ping")
	optNoPing        = flagOpt("noping")
	optAddrAuthority = enumOpt("address-authority", "client", "daemon") // command, login: the first device
	// In run the value names the target: TARGET=client|daemon.
	optAddrAuthorityRun = valueOpt("address-authority", "TARGET=client|daemon").repeatable()
)

// Global options, matched before the command word.
var (
	optConfig      = valueOpt("config", "PATH", "cfg").repeatable()
	optSet         = valueOpt("set", "KEY=VALUE").repeatable()
	optDebugShow   = flagOpt("debug-show-secrets")
	optTimezone    = valueOpt("timezone", "ZONE")
	optAnsi        = enumOpt("ansi", "auto", "strip", "preserve")
	optVersionFlag = flagOpt("version")
)

var globalOptionTable = []*option{optConfig, optSet, optQuiet, optDebug, optDebugShow, optTimezone, optAnsi, optIPv4, optIPv6, optVersionFlag, optHelp}

// login options.
var (
	optRecord    = &option{name: "record", aliases: []string{"rec"}, kind: kindOptional, placeholder: "[=PATH]"}
	optSSHOption = valueOpt("ssh-option", "KEY=VALUE").repeatable().notBuilt()
)

// command and run options: the interactive-prompt declarations and the blind
// wait, and command's record directory. The three declarations carry
// roleDeclaration: the parser records each beside the --cmd it follows
// (Invocation.Declarations), and declarationLists in work_commands.go makes
// the per-command lists the execution plan carries. --expect is repeatable
// (up to executionplan.ExpectationsMax per command); --blind-return is not,
// a second one on a command being blind_return_conflict.
var (
	optExpect      = valueOpt("expect", "PATTERN=RESPONSE").repeatable().as(roleDeclaration)
	optBlind       = flagOpt("blind").as(roleDeclaration)
	optBlindReturn = intOpt("blind-return").as(roleDeclaration)
	optBlindWait   = durationOpt("blind-wait")
	optLiteral     = flagOpt("literal")
	optNof         = flagOpt("nof")
	// --of[=PATH] is the opposite of --nof, and names the output root when
	// PATH is given. The value attaches with
	// = only, as --record[=PATH] does: in command the word after a bare
	// --of is the device, and in run it may be device text, so a
	// space-separated PATH could not be told from either.
	optOf = &option{name: "of", kind: kindOptional, placeholder: "[=PATH]"}
	// --cd=PATH names the collection directory for one invocation
	// (crun.directory as a flag-origin value): a crun's in place of the
	// configured one, and on run and command the switch that writes a
	// collection at all. = form only for the reason of --of: the next
	// word may be a device. The handler refuses the bare form.
	optCd = &option{name: "cd", kind: kindOptional, placeholder: "=PATH"}
	// --fs=SUFFIX appends a literal suffix to each collection file's name;
	// on run and command without --cd it is --cd=. as well. = form only,
	// as --cd: the handler refuses the bare form.
	optFs = &option{name: "fs", kind: kindOptional, placeholder: "=SUFFIX"}
)

// run options.
var (
	optDispatch = enumOpt("dispatch", "serial", "parallel", "wave")
	// The dispatch shortcuts: whole words standing for --dispatch with a
	// value, as --pi
	// stands for --platform cisco_iosxe; the last of --dispatch and the
	// shortcuts on a line wins, as a repeated --dispatch does.
	optDP          = shortcutOpt("dp", optDispatch, "parallel")
	optDW          = shortcutOpt("dw", optDispatch, "wave")
	optDS          = shortcutOpt("ds", optDispatch, "serial")
	optWorkers     = intOpt("workers")
	optStartWidth  = intOpt("start-width")
	optMaxWidth    = intOpt("max-width")
	optHaltCount   = intOpt("halt-on-error-count")
	optHaltPercent = intOpt("halt-on-error-percent")
	optGateCount   = intOpt("wave-gate-error-count")
	optGatePercent = intOpt("wave-gate-error-percent")
	optWaveDelay   = durationOpt("wave-delay")
	optNoDaemon    = flagOpt("no-daemon")
	optDetach      = flagOpt("detach")
	optFollow      = flagOpt("follow")
	optReason      = valueOpt("reason", "TEXT")
	optDryRun      = flagOpt("dry-run")
	optExercise    = flagOpt("exercise")
)

// daemon options.
var (
	optForeground   = flagOpt("foreground")
	optStartTimeout = durationOpt("start-timeout").notBuilt()
	optGrace        = flagOpt("grace")
	optAfter        = durationOpt("after")
	optForce        = flagOpt("force")
)

// config options.
var (
	optMinimal     = flagOpt("minimal")
	optFull        = flagOpt("full")
	optInternal    = flagOpt("internal").notBuilt()
	optExplain     = flagOpt("explain")
	optShowSources = flagOpt("show-sources")
	optFormatToml  = enumOpt("format", "toml", "json")
)

// watch options.
var (
	optTheme       = enumOpt("theme", "auto", "dark", "light", "nocolor")
	optColor       = enumOpt("color", "auto", "always", "never")
	optRefresh     = durationOpt("refresh")
	optStaleAfter  = durationOpt("stale-after")
	optFormatWatch = enumOpt("format", "tui", "table", "json")
	optAllRetained = flagOpt("all-retained").notBuilt()
	optOnce        = flagOpt("once")
	// The / filter and s sort's script forms.
	optWatchFilter = valueOpt("filter", "TEXT")
	optWatchSort   = enumOpt("sort", "time", "status", "operator", "mode", "fail", "target") // watchui.SortKeys' order
)

// setup shared options.
var (
	optGroupName  = valueOpt("group", "NAME")
	optSharedMode = enumOpt("mode", "2770", "2775")
)

type cmdShape int

const (
	shapePlain          cmdShape = iota // options before or after positionals; -- makes the rest positional
	shapeFreeformDevice                 // command: device, then freeform
	shapeFreeform                       // run: freeform after the first positional
)

type command struct {
	path    string
	word    string
	aliases []string
	options []*option
	subs    []*command
	shape   cmdShape
	maxPos  int // plain commands: -1 unlimited
	minPos  int // plain commands: required positionals (job cancel JOB-ID)
	// textOptional: a freeform command that may name no command at all
	// (crun, whose devices then run their platform lists).
	textOptional bool
	hidden       bool
	help         func() string
}

var stopOptionTable = []*option{optGrace, optAfter, optForce, optHelp}

// commandTable is the top-level grammar.
var commandTable = []*command{
	{path: "login", word: "login", shape: shapePlain, maxPos: -1, help: func() string { return loginHelp },
		options: []*option{optPlatform, optPI, optPN, optPR, optPJ, optPA, optPG, optTarget, optTf, optTfr, optExclude, optSite, optGroup, optSelectPlatform, optAll, optOrder, optAddress, optPort, optTransport, optHostKeyPolicy, optKnownHosts, optRecord, optSSHOption, optQuiet, optDebug, optIPv4, optIPv6, optPing, optNoPing, optAddrAuthority, optHelp}},
	{path: "command", word: "command", aliases: []string{"cmd"}, shape: shapeFreeformDevice, help: func() string { return commandHelp },
		options: []*option{optTarget, optTl, optTf, optTfr, optSite, optGroup, optSelectPlatform, optAll, optExclude, optOrder, optCmd, optCf, optPlatform, optPI, optPN, optPR, optPJ, optPA, optPG, optAddress, optPort, optTransport, optHostKeyPolicy, optKnownHosts, optFormat, optEcho, optBorder, optNoBorder, optQuiet, optDebug, optContinue, optExpect, optBlind, optBlindReturn, optBlindWait, optLiteral, optNof, optOf, optCd, optFs, optIPv4, optIPv6, optPing, optNoPing, optAddrAuthority, optHelp}},
	{path: "run", word: "run", shape: shapeFreeform, help: runHelpText, options: runOptions},
	// stream, or -, reads a run line by line from standard input: the
	// lines carry run's options and the commands; the word itself takes
	// --help alone.
	{path: "stream", word: "stream", aliases: []string{"-"}, shape: shapePlain, options: []*option{optHelp}, help: func() string { return streamHelp }},
	// crun, the collection run: a command word sharing run's option slice,
	// --cd=PATH among them, so every run option means on crun what it
	// means on run and a new one reaches crun by construction; the handler
	// is run's with the collection on.
	{path: "crun", word: "crun", shape: shapeFreeform, textOptional: true, help: crunHelpText, options: runOptions},
	{path: "daemon", word: "daemon", shape: shapePlain, options: []*option{optHelp}, help: func() string { return daemonHelp },
		subs: []*command{
			{path: "daemon start", word: "start", shape: shapePlain, options: []*option{optForeground, optStartTimeout, optHelp}, help: func() string { return daemonHelp }},
			{path: "daemon stop", word: "stop", shape: shapePlain, options: stopOptionTable, help: func() string { return daemonHelp }},
			{path: "daemon restart", word: "restart", shape: shapePlain, options: stopOptionTable, help: func() string { return daemonHelp }},
			{path: "daemon status", word: "status", shape: shapePlain, options: []*option{optFormatTJ, optHelp}, help: func() string { return daemonHelp }},
			{path: "daemon serve", word: "serve", shape: shapePlain, options: []*option{optHelp}, help: func() string { return daemonHelp }},
			{path: "daemon help", word: "help", shape: shapePlain, hidden: true, options: []*option{optHelp}, help: func() string { return daemonHelp }},
		}},
	// job: operations on one accepted job, named by its receipt's ID.
	// cancel moved here from daemon.
	{path: "job", word: "job", shape: shapePlain, options: []*option{optHelp}, help: func() string { return jobHelp },
		subs: []*command{
			{path: "job cancel", word: "cancel", shape: shapePlain, minPos: 1, maxPos: 1, options: []*option{optReason, optFollow, optFormatTJ, optHelp}, help: func() string { return jobHelp }},
			{path: "job follow", word: "follow", shape: shapePlain, minPos: 1, maxPos: 1, options: []*option{optFormat, optEcho, optBorder, optNoBorder, optHelp}, help: func() string { return jobHelp }},
			{path: "job help", word: "help", shape: shapePlain, hidden: true, options: []*option{optHelp}, help: func() string { return jobHelp }},
		}},
	{path: "config", word: "config", shape: shapePlain, options: []*option{optHelp}, help: func() string { return configHelp },
		subs: []*command{
			{path: "config generate", word: "generate", shape: shapePlain, maxPos: 1, options: []*option{optMinimal, optFull, optForce, optHelp}, help: func() string { return configHelp }},
			{path: "config validate", word: "validate", shape: shapePlain, maxPos: 1, options: []*option{optInternal, optFormatTJ, optHelp}, help: func() string { return configHelp }},
			{path: "config show", word: "show", shape: shapePlain, maxPos: 1, options: []*option{optExplain, optFormatToml, optShowSources, optHelp}, help: func() string { return configHelp }},
			{path: "config colors", word: "colors", shape: shapePlain, options: []*option{optHelp}, help: func() string { return configHelp }},
			{path: "config help", word: "help", shape: shapePlain, hidden: true, options: []*option{optHelp}, help: func() string { return configHelp }},
		}},
	// setup: the site's one-time privileged preparations; shared creates
	// the shared directory and its three trees as root, tab places the
	// bash completion file.
	{path: "setup", word: "setup", shape: shapePlain, options: []*option{optHelp}, help: func() string { return setupHelp },
		subs: []*command{
			{path: "setup shared", word: "shared", shape: shapePlain, options: []*option{optGroupName, optSharedMode, optHelp}, help: func() string { return setupHelp }},
			{path: "setup tab", word: "tab", shape: shapePlain, options: []*option{optHelp}, help: func() string { return setupHelp }},
			{path: "setup help", word: "help", shape: shapePlain, hidden: true, options: []*option{optHelp}, help: func() string { return setupHelp }},
		}},
	{path: "watch", word: "watch", shape: shapePlain, help: func() string { return watchHelp },
		options: []*option{optTheme, optColor, optRefresh, optStaleAfter, optFormatWatch, optWatchFilter, optWatchSort, optAllRetained, optOnce, optHelp}},
	{path: "version", word: "version", shape: shapePlain, options: []*option{optFormatTJ, optHelp}, help: func() string { return versionHelp }},
	// v0.9.1 accepted "karvi help"; kept as a hidden word.
	{path: "help", word: "help", shape: shapePlain, maxPos: -1, hidden: true, options: []*option{optHelp}, help: func() string { return topHelp }},
}

func lookupCommand(path string) *command {
	for _, c := range commandTable {
		if c.path == path {
			return c
		}
		for _, s := range c.subs {
			if s.path == path {
				return s
			}
		}
	}
	return nil
}

// runOptions is run's option slice, shared with crun.
var runOptions = []*option{optTarget, optTl, optTf, optTfr, optSite, optGroup, optSelectPlatform, optAll, optExclude, optOrder, optCmd, optCf, optPlatform, optPI, optPN, optPR, optPJ, optPA, optPG, optDispatch, optDP, optDW, optDS, optWorkers, optStartWidth, optMaxWidth, optHaltCount, optHaltPercent, optGateCount, optGatePercent, optWaveDelay, optContinue, optExpect, optBlind, optBlindReturn, optBlindWait, optLiteral, optTransport, optHostKeyPolicy, optKnownHosts, optAddress, optFormat, optEcho, optBorder, optNoBorder, optNoDaemon, optNof, optOf, optCd, optFs, optDetach, optFollow, optDryRun, optExercise, optDebug, optIPv4, optIPv6, optPing, optNoPing, optAddrAuthorityRun, optHelp}
