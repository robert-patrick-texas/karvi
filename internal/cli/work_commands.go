package cli

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/targetsource"
)

// targetInputs converts the parsed target inputs into the assembly inputs:
// each keeps its command-line position, and a --tf or --tfr source is read
// here so the daemon never opens an operator file. Standard input may feed
// one input per invocation.
func targetInputs(inv *Invocation, streams app.IO) ([]app.TargetInput, int, bool) {
	stdinUses, sources := 0, 0
	if inv.CommandsFile == "-" {
		stdinUses++
	}
	for _, t := range inv.Targets {
		switch t.Kind {
		case "tf":
			sources++
			if t.Value == "-" {
				stdinUses++
			}
		case "tfr":
			sources++
			if t.Value == "-" {
				return nil, usageError(streams.Stderr, "target_source_stdin_invalid", "--tfr - is not accepted; --tf - reads standard input"), false
			}
		}
	}
	if stdinUses > 1 {
		return nil, usageError(streams.Stderr, "stdin_source_repeated", "standard input may be read by one input: --cf - or one --tf -"), false
	}
	// The source rules come from configuration: targets.empty-source and
	// targets.recursion-max-depth.
	emptyRule, maxDepth := "error", 3
	if sources > 0 {
		cfg, err := app.LoadConfig(inv.common())
		if err != nil {
			return nil, reportError(streams.Stderr, "config_load_failed", err), false
		}
		emptyRule, maxDepth = cfg.String("targets.empty-source"), cfg.Int("targets.recursion-max-depth")
	}
	inputs := make([]app.TargetInput, 0, len(inv.Targets))
	for _, t := range inv.Targets {
		switch t.Kind {
		case "tf", "tfr":
			var names []string
			var err error
			switch {
			case t.Value == "-":
				names, err = targetsource.Lines(streams.Stdin, "standard input")
			case t.Kind == "tf":
				names, err = targetsource.Read(t.Value, targetsource.Options{})
			default:
				names, err = targetsource.Read(t.Value, targetsource.Options{Recursive: true, MaxDepth: maxDepth})
			}
			if err != nil {
				return nil, reportError(streams.Stderr, "target_source_unreadable", err), false
			}
			source := t.Value
			if source == "-" {
				source = "standard input"
			}
			for _, name := range names {
				if err := checkSelector("target file "+source+" entry", name, false); err != nil {
					return nil, reportError(streams.Stderr, "target_selector_pattern_invalid", err), false
				}
			}
			if len(names) == 0 {
				what := "target file"
				if t.Kind == "tfr" {
					what = "target folder tree"
				} else if t.Value == "-" {
					what = "standard input"
				} else if info, statErr := os.Stat(t.Value); statErr == nil && info.IsDir() {
					what = "target folder"
				}
				if emptyRule == "error" {
					return nil, reportError(streams.Stderr, "target_source_empty", errorcodes.Errorf("target_source_empty", "%s %s yields no targets (targets.empty-source is error)", what, t.Value)), false
				}
				fmt.Fprintf(streams.Stderr, "warning: %s %s yields no targets; continuing with the other inputs\n", what, t.Value)
			}
			inputs = append(inputs, app.TargetInput{Kind: "names", Names: names, Source: t.Value})
		case "all":
			inputs = append(inputs, app.TargetInput{Kind: "all"})
		default: // target, site, device-group, platform
			inputs = append(inputs, app.TargetInput{Kind: t.Kind, Value: t.Value})
		}
	}
	return inputs, 0, true
}

func checkPort(inv *Invocation, w io.Writer) (int, bool) {
	port := inv.Int(optPort)
	if inv.Set(optPort) && (port < 1 || port > 65535) {
		return usageError(w, "port_out_of_range", "--port must be 1..65535, got %d", port), false
	}
	return 0, true
}

// checkExclusive refuses two flags given together under code: --border with
// --noborder (border_options_conflict), --of with --nof
// (output_options_conflict).
func checkExclusive(inv *Invocation, w io.Writer, code string, a, b *option) (int, bool) {
	if inv.Flag(a) && inv.Flag(b) {
		return usageError(w, code, "--%s and --%s are mutually exclusive", a.name, b.name), false
	}
	return 0, true
}

func checkBorders(inv *Invocation, w io.Writer) (int, bool) {
	return checkExclusive(inv, w, "border_options_conflict", optBorder, optNoBorder)
}

// collectionOptionsError is the check of the collection options' values,
// shared by the handlers and by stream mode when a line is read: a bare
// --cd or --fs (or one given = and nothing) is refused, the value
// attaching with = alone, and a suffix that cannot be appended to a file
// name is crun_suffix_invalid.
func collectionOptionsError(inv *Invocation) error {
	if inv.Set(optCd) && inv.String(optCd) == "" {
		return errorcodes.Errorf("cli_option_value_missing", "--cd takes its PATH with =: --cd=PATH (the next word may be a device)")
	}
	if inv.Set(optFs) {
		// A bare --fs and --fs= are one to the parser, as --cd's are.
		suffix := inv.String(optFs)
		if suffix == "" {
			return errorcodes.Errorf("cli_option_value_missing", "--fs takes its SUFFIX with =: --fs=SUFFIX (the next word may be a device)")
		}
		if problem := executionplan.SuffixProblem(suffix); problem != "" {
			return errorcodes.Errorf("crun_suffix_invalid", "--fs=%q %s; the suffix is appended as written to each collection file's name, so it is not empty and holds no /, NUL, or control character", suffix, problem)
		}
	}
	return nil
}

// collection is what the collection options ask for: the word (a crun
// always, a run or command given --cd or --fs, empty otherwise), the
// suffix, and whether --fs implied --cd=. (named in a directory failure).
type collection struct {
	word, suffix string
	implied      bool
}

// collectionOptions applies --cd=PATH as crun.directory, a flag-origin
// value as --of=PATH is output.root (a site that locks the key refuses
// it); on run and command, --fs without --cd is --cd=., the working
// directory, while a crun's --fs alone keeps crun.directory.
func collectionOptions(inv *Invocation, word string, flags map[string]configload.FlagValue) collection {
	c := collection{suffix: inv.String(optFs)}
	path, source := inv.String(optCd), longName(optCd)
	if path == "" && c.suffix != "" && word != "crun" {
		path, c.implied, source = ".", true, longName(optFs)
	}
	if path != "" {
		setKey(flags, source, "crun.directory", path)
	}
	if word == "crun" || path != "" {
		c.word = word
	}
	return c
}

// impliedDirectory names the directory --fs implied in a failure to
// resolve or prepare it, so the operator sees which option asked for it.
func (c collection) impliedDirectory(result *app.ActivityResult) {
	if c.implied && (strings.HasPrefix(result.Error, "crun_directory_unavailable:") || strings.HasPrefix(result.Error, "crun_directory_not_writable:")) {
		result.Error += " (the working directory, implied by --fs)"
	}
}

// outputOptions applies --nof and --of[=PATH] as flag-origin configuration
// values, so a site that locks a key refuses the option as it refuses any
// other origin
// (config_lock_violation). --nof is output.persist-command=false and --of
// is the opposite, for command and run alike, on every path: the plan
// carries the value to whoever runs the job. --of=PATH is output.root for
// the invocation, resolved as the key is: ~ expanded, a relative path from
// the working directory.
func outputOptions(inv *Invocation, flags map[string]configload.FlagValue) {
	if inv.Flag(optNof) {
		setKey(flags, longName(optNof), "output.persist-command", false)
	}
	if inv.Set(optOf) {
		setKey(flags, longName(optOf), "output.persist-command", true)
	}
	if path := inv.String(optOf); path != "" {
		setKey(flags, longName(optOf), "output.root", path)
	}
}

// continueOptions writes execution.halt-device-on-command-error false
// when the device's later commands run past an error: for
// --continue-device-on-error, or for the crun word, which always does,
// each named as the source a lock's refusal or config show gives.
func continueOptions(inv *Invocation, word string, flags map[string]configload.FlagValue) {
	switch {
	case word == "crun":
		setKey(flags, "crun", "execution.halt-device-on-command-error", false)
	case inv.Flag(optContinue):
		setKey(flags, longName(optContinue), "execution.halt-device-on-command-error", false)
	}
}

func commandCommand(ctx context.Context, inv *Invocation, streams app.IO) int {
	if code, ok := checkPort(inv, streams.Stderr); !ok {
		return code
	}
	if code, ok := checkBorders(inv, streams.Stderr); !ok {
		return code
	}
	if code, ok := checkExclusive(inv, streams.Stderr, "output_options_conflict", optOf, optNof); !ok {
		return code
	}
	if err := collectionOptionsError(inv); err != nil {
		return reportError(streams.Stderr, errorcodes.Of(err), err)
	}
	inputs, code, ok := targetInputs(inv, streams)
	if !ok {
		return code
	}
	commands, decl, code, ok := commandPlan(inv, streams)
	if !ok {
		return code
	}
	common := inv.common()
	if inv.Set(optBlindWait) {
		setKey(common.ConfigFlags, longName(optBlindWait), "execution.blind-wait", inv.Duration(optBlindWait).String())
	}
	outputOptions(inv, common.ConfigFlags)
	continueOptions(inv, "command", common.ConfigFlags)
	collection := collectionOptions(inv, "command", common.ConfigFlags)
	format := inv.String(optFormat)
	if format == "" {
		format = "text"
	}
	result := app.ExecuteCommand(ctx, app.CommandOptions{Collection: collection.word, Suffix: collection.suffix, CommonOptions: common, Targets: inputs, Excludes: inv.Strings(optExclude), Address: inv.String(optAddress), Platform: inv.String(optPlatform), Transport: inv.String(optTransport), Port: inv.Int(optPort), AddressAuthority: inv.String(optAddrAuthority), Commands: commands, CommandsFile: commandsFileName(inv), BlindReturns: decl.returns, Blind: decl.blind, Expectations: decl.expect, Timeouts: decl.timeouts, MaxBytes: decl.maxBytes, Format: format, Echo: inv.Flag(optEcho), DynamicBorder: inv.Flag(optBorder), NoBorder: inv.Flag(optNoBorder), ContinueDeviceOnError: inv.Flag(optContinue)}, streams)
	collection.impliedDirectory(&result)
	printResultError(result, streams.Stderr)
	return result.ExitCode
}

// declarations are the per-command interactive-prompt lists the client
// interprets once and the plan carries: the blind returns, the tolerance
// flags, and the
// expect-and-send declarations, and each command's own timeout and byte
// limit (0 the job's), each empty when no command has any.
type declarations struct {
	returns  []int
	blind    []bool
	expect   [][]executionplan.Expectation
	timeouts []int64
	maxBytes []int64
}

// commandPlan returns the device commands, the explicit --cmd values in
// order then the lines of --cf, and their declarations after
// declarationLists. The parser has already ensured at least one source and
// placed every declaration on a --cmd (or the freeform command), so the
// file's lines, appended after the --cmd values, carry none.
func commandPlan(inv *Invocation, streams app.IO) ([]string, declarations, int, bool) {
	commands := append([]string(nil), inv.Commands...)
	if inv.Set(optCf) {
		lines, err := loadCommandsFile(inv.CommandsFile, streams.Stdin)
		if err != nil {
			return nil, declarations{}, reportError(streams.Stderr, "commands_file_unreadable", err), false
		}
		commands = append(commands, lines...)
	}
	decl, err := declarationLists(commands, inv.Declarations, inv.Flag(optLiteral))
	if err != nil {
		return nil, declarations{}, reportError(streams.Stderr, errorcodes.Of(err), err), false
	}
	return commands, decl, 0, true
}

// commandsFileName is the --cf path as given, or empty: the planner keeps
// its base name for the scoreboard.
func commandsFileName(inv *Invocation) string {
	if inv.Set(optCf) {
		return inv.CommandsFile
	}
	return ""
}

// blindEscape is the only escape karvi interprets in device text.
const blindEscape = `\r`

// declarationLists applies the escape rule and the declarations to the
// command list and returns each command's blind returns, its tolerance flag,
// its expectations, its timeout, and its byte limit, each empty when no
// command has any. The commands
// are edited in place: a command's trailing \r sequences are removed and
// counted; nothing else in the text is interpreted, so a malformed escape
// has no case; literal sends every command as written. Each declaration has
// been placed by the parser on a command (Declaration.Command, one-based).
//
// The rules, one command at a time, each a usage error before any
// connection: a trailing escape and --blind-return on one command, or
// --blind-return twice, contradict (blind_return_conflict); a count outside
// 0..executionplan.BlindReturnsMax, from the option or the escapes, is
// blind_return_out_of_range; more than executionplan.ExpectationsMax
// declarations is expect_too_many; and blind returns beside a declaration is
// expect_with_blind_return, since typed-ahead returns and answered prompts on
// one command reproduce the miscount hazard, and the expectations cover
// every case the returns do. --timeout or --maxbytes twice on one command
// is declaration_repeated; --timeout on a blind command (the flag, a count,
// or the escape) is timeout_with_blind, the blind wait being that command's
// own; the parser checked each bound's range. A count above zero, or a
// trailing escape, implies the tolerance, so the flag is set beside it (the
// plan's Validate refuses a count without its flag); --blind sets the flag
// alone, the command then awaited for the blind wait with no return written.
// --blind-return 0 declares nothing, as at D3.
func declarationLists(commands []string, decls []Declaration, literal bool) (declarations, error) {
	counts := make([]int, len(commands))
	flags := make([]bool, len(commands))
	expect := make([][]executionplan.Expectation, len(commands))
	for i := range commands {
		expect[i] = []executionplan.Expectation{}
	}
	escaped := make([]bool, len(commands)) // the command ended in \r sequences
	if !literal {
		for i, c := range commands {
			n := 0
			for strings.HasSuffix(c, blindEscape) {
				c = strings.TrimSuffix(c, blindEscape)
				n++
			}
			if n > executionplan.BlindReturnsMax {
				return declarations{}, errorcodes.Errorf("blind_return_out_of_range", "command %d ends in %d %s sequences; at most %d blind returns", i+1, n, blindEscape, executionplan.BlindReturnsMax)
			}
			commands[i], counts[i], escaped[i] = c, n, n > 0
		}
	}
	timeouts := make([]int64, len(commands)) // nanoseconds; 0 the job's
	maxBytes := make([]int64, len(commands)) // 0 the job's
	optioned := make([]bool, len(commands))  // the command was given --blind-return
	for _, d := range decls {
		i := d.Command - 1
		if i < 0 || i >= len(commands) {
			// The parser places every declaration; a record outside the
			// list is a programming error, reported rather than dropped.
			return declarations{}, errorcodes.Errorf("declaration_before_command", "--%s is not placed on a command", d.Option)
		}
		switch d.Option {
		case "blind-return":
			n, _ := strconv.Atoi(d.Value) // the parser checked the integer
			switch {
			case escaped[i]:
				return declarations{}, errorcodes.Errorf("blind_return_conflict", "command %d ends in %s sequences and is given --blind-return; write one or the other", i+1, blindEscape)
			case optioned[i]:
				return declarations{}, errorcodes.Errorf("blind_return_conflict", "command %d is given --blind-return more than once", i+1)
			case n < 0 || n > executionplan.BlindReturnsMax:
				return declarations{}, errorcodes.Errorf("blind_return_out_of_range", "--blind-return must be 0..%d, got %d", executionplan.BlindReturnsMax, n)
			}
			counts[i], optioned[i] = n, true
		case "blind":
			flags[i] = true
		case "expect":
			pattern, response, _ := splitExpect(d.Value) // the parser checked the grammar
			if len(expect[i]) == executionplan.ExpectationsMax {
				return declarations{}, errorcodes.Errorf("expect_too_many", "command %d is given more than %d --expect declarations", i+1, executionplan.ExpectationsMax)
			}
			expect[i] = append(expect[i], executionplan.Expectation{Pattern: pattern, Response: response})
		case "timeout":
			if timeouts[i] != 0 {
				return declarations{}, errorcodes.Errorf("declaration_repeated", "command %d is given --timeout more than once", i+1)
			}
			dur, _ := time.ParseDuration(d.Value) // the parser checked the form and range
			timeouts[i] = int64(dur)
		case "maxbytes":
			if maxBytes[i] != 0 {
				return declarations{}, errorcodes.Errorf("declaration_repeated", "command %d is given --maxbytes more than once", i+1)
			}
			maxBytes[i], _ = strconv.ParseInt(d.Value, 10, 64) // the parser checked the form and range
		}
	}
	// Each list is one entry per command, or empty when no command has
	// any: the counts and the flags travel together (a blind command with
	// no returns still needs its flag), the expectations on their own.
	blindUsed, expectUsed, timeoutUsed, maxBytesUsed := false, false, false, false
	for i := range commands {
		if (escaped[i] || optioned[i]) && len(expect[i]) > 0 {
			return declarations{}, errorcodes.Errorf("expect_with_blind_return", "command %d carries blind returns and an --expect declaration; a command takes one or the other, and --blind alone declares the tolerance beside --expect", i+1)
		}
		flags[i] = flags[i] || counts[i] > 0
		if flags[i] && timeouts[i] != 0 {
			return declarations{}, errorcodes.Errorf("timeout_with_blind", "command %d is blind (--blind, --blind-return, or a trailing %s) and is given --timeout; a blind command waits for execution.blind-wait (--blind-wait), not a timeout", i+1, blindEscape)
		}
		blindUsed = blindUsed || flags[i]
		expectUsed = expectUsed || len(expect[i]) > 0
		timeoutUsed = timeoutUsed || timeouts[i] != 0
		maxBytesUsed = maxBytesUsed || maxBytes[i] != 0
	}
	out := declarations{returns: []int{}, blind: []bool{}, expect: [][]executionplan.Expectation{}, timeouts: []int64{}, maxBytes: []int64{}}
	if blindUsed {
		out.returns, out.blind = counts, flags
	}
	if expectUsed {
		out.expect = expect
	}
	if timeoutUsed {
		out.timeouts = timeouts
	}
	if maxBytesUsed {
		out.maxBytes = maxBytes
	}
	return out, nil
}

func commandRun(ctx context.Context, inv *Invocation, streams app.IO) int {
	// The word: run, or crun, the collection run: the same handler with the
	// collection switched on, as --cd switches it on for a run.
	word := inv.Path
	crun := word == "crun"
	if code, ok := checkBorders(inv, streams.Stderr); !ok {
		return code
	}
	if err := collectionOptionsError(inv); err != nil {
		return reportError(streams.Stderr, errorcodes.Of(err), err)
	}
	if code, ok := checkExclusive(inv, streams.Stderr, "output_options_conflict", optOf, optNof); !ok {
		return code
	}
	address := inv.String(optAddress)
	if address != "" {
		if _, err := netip.ParseAddr(address); err != nil {
			return reportError(streams.Stderr, "management_address_invalid", fmt.Errorf("invalid --management-address %q: %w", address, err))
		}
	}
	authorities := inv.Strings(optAddrAuthorityRun)
	for _, raw := range authorities {
		target, value, ok := strings.Cut(raw, "=")
		if !ok || strings.TrimSpace(target) == "" || (!strings.EqualFold(value, "client") && !strings.EqualFold(value, "daemon")) {
			return usageError(streams.Stderr, "cli_option_value_invalid", "--address-authority %q must be TARGET=client|daemon", raw)
		}
	}
	if len(inv.Targets) == 0 {
		// run's --platform was once the selector; say where it went.
		hint := ""
		if inv.Set(optPlatform) {
			hint = " (--platform names the platform the devices run as and selects none; --select-platform selects by platform)"
		}
		return reportError(streams.Stderr, "inventory_positive_selector_missing", errorcodes.Errorf("inventory_positive_selector_missing", "%s requires a target input: --target, --tf, --site, --device-group, --select-platform, or --all%s", word, hint))
	}
	inputs, code, ok := targetInputs(inv, streams)
	if !ok {
		return code
	}
	commands, decl, code, ok := commandPlan(inv, streams)
	if !ok {
		return code
	}
	common := inv.common()
	if inv.Set(optBlindWait) {
		setKey(common.ConfigFlags, longName(optBlindWait), "execution.blind-wait", inv.Duration(optBlindWait).String())
	}
	outputOptions(inv, common.ConfigFlags)
	continueOptions(inv, word, common.ConfigFlags)
	collection := collectionOptions(inv, word, common.ConfigFlags)
	format := inv.String(optFormat)
	if format == "" {
		format = "text"
	}
	follow := !inv.Set(optFollow) || inv.Flag(optFollow)
	echo, dynamicBorder, noBorder := inv.Flag(optEcho), inv.Flag(optBorder), inv.Flag(optNoBorder)
	opts := app.RunOptions{CommonOptions: common, Follow: follow, Exercise: inv.Flag(optExercise), Detach: inv.Flag(optDetach), Targets: inputs, Excludes: inv.Strings(optExclude), ManagementAddress: address, Platform: inv.String(optPlatform), AddressAuthorities: authorities, Commands: commands, CommandsFile: commandsFileName(inv), BlindReturns: decl.returns, Blind: decl.blind, Expectations: decl.expect, Timeouts: decl.timeouts, MaxBytes: decl.maxBytes, ContinueDeviceOnError: inv.Flag(optContinue), Transport: inv.String(optTransport), Format: format, Echo: echo, DynamicBorder: dynamicBorder, NoBorder: noBorder}
	opts.Collection, opts.Suffix = collection.word, collection.suffix
	if crun {
		// A crun runs the device's whole list past a rejected statement;
		// with no command on the line, each device runs its platform's
		// list. A run's --continue stays its own.
		opts.ContinueDeviceOnError, opts.PlatformCommands = true, len(commands) == 0
	}
	// The rehearsal flags exclude one another, neither detaches, and an
	// exercise needs a
	// daemon. While --detach is pending, the parser's refusal comes first.
	switch {
	case inv.Flag(optDryRun) && inv.Flag(optExercise):
		return usageError(streams.Stderr, "run_mode_conflict", "--dry-run and --exercise are mutually exclusive")
	case (inv.Flag(optDryRun) || inv.Flag(optExercise)) && inv.Flag(optDetach):
		return usageError(streams.Stderr, "run_mode_conflict", "--dry-run and --exercise cannot be combined with --detach")
	case inv.Flag(optExercise) && inv.Flag(optNoDaemon):
		return usageError(streams.Stderr, "run_mode_conflict", "--exercise is daemon-side validation and cannot be combined with --no-daemon; use --dry-run for the in-process rehearsal")
	case inv.Flag(optDetach) && inv.Set(optFollow) && inv.Flag(optFollow):
		return usageError(streams.Stderr, "run_mode_conflict", "--detach and --follow contradict each other; follow is true unless detached")
	case inv.Flag(optDetach) && inv.Flag(optNoDaemon):
		return usageError(streams.Stderr, "run_mode_conflict", "--detach needs a daemon to leave the job with and cannot be combined with --no-daemon")
	case inv.Flag(optNof) && inv.Flag(optDetach):
		// Nothing persisted and nobody following; --nof --follow=false is
		// allowed, a run for its exit code.
		return usageError(streams.Stderr, "run_mode_conflict", "--nof with --detach would leave a job that persists nothing and that nobody follows")
	case inv.Flag(optNof) && inv.Flag(optExercise):
		return usageError(streams.Stderr, "run_mode_conflict", "--nof with --exercise contradict each other; the exercise report is a file in the job folder")
	}
	if inv.Flag(optDryRun) {
		result := app.InspectRun(ctx, opts, !inv.Flag(optNoDaemon), streams)
		collection.impliedDirectory(&result)
		printResultError(result, streams.Stderr)
		return result.ExitCode
	}
	if inv.Flag(optNoDaemon) {
		// The run executes in this process and is displayed as command is:
		// each record rendered from memory as its device answers, then the
		// footer (display.run.footer) and a collection's line. Nothing is read
		// back from commands.jsonl (through v0.12.1 the display was discarded
		// during the job and the file rendered at its end). A broken standard
		// output must be a write error that the job outlives, never a SIGPIPE
		// death with devices mid-list, so the signal is ignored as on the
		// daemon-backed path.
		signal.Ignore(syscall.SIGPIPE)
		runStreams := streams
		if !follow {
			runStreams.Stdout = io.Discard
		}
		result := app.ExecuteRunLocal(ctx, opts, runStreams)
		collection.impliedDirectory(&result)
		printResultError(result, streams.Stderr)
		if crun {
			// The hook after the display's end, a crun's alone.
			app.RunCollectionHook(ctx, common, result, streams.Stderr)
		}
		return result.ExitCode
	}
	// The daemon is probed, and launched when absent, just before the first
	// request, inside RunViaDaemon; here only the paths are resolved.
	rt, e := app.ResolveDaemonRuntime(common)
	if e != nil {
		return reportError(streams.Stderr, "daemon_start_failed", e)
	}
	ensure := func(ctx context.Context) error { _, err := ensureDaemon(ctx, inv.Global, streams.Stderr); return err }
	// The records are rendered as the daemon makes them durable, through
	// the follow stream inside RunViaDaemon. A broken stdout must surface
	// as a write error, not a SIGPIPE death, so the signal is ignored for
	// the daemon-backed run.
	signal.Ignore(syscall.SIGPIPE)
	result := app.RunViaDaemon(ctx, opts, rt.Socket, rt.MaxFrame, ensure, streams)
	if !common.Quiet && opts.Detach && result.JobID != "" && result.ExitCode == 0 {
		fmt.Fprintf(streams.Stderr, "%s %s accepted artifacts=%s\n", word, result.JobID, app.ArtifactsLabel(result.ArtifactDir))
	}
	collection.impliedDirectory(&result)
	printResultError(result, streams.Stderr)
	if crun && !opts.Detach {
		// The hook after the display's end (the footer and the collection
		// line), on the daemon path as on the in-process one; a detached
		// run has no client at its end.
		app.RunCollectionHook(ctx, common, result, streams.Stderr)
	}
	return result.ExitCode
}

func commandLogin(ctx context.Context, inv *Invocation, streams app.IO) int {
	if code, ok := checkPort(inv, streams.Stderr); !ok {
		return code
	}
	inputs, code, ok := targetInputs(inv, streams)
	if !ok {
		return code
	}
	// Recording is performed by the script(1) wrapper around this process
	// (login_record.go); the child never records in-process.
	result := app.ExecuteLogin(ctx, app.LoginOptions{CommonOptions: inv.common(), Targets: inputs, Excludes: inv.Strings(optExclude), Address: inv.String(optAddress), Platform: inv.String(optPlatform), Transport: inv.String(optTransport), Port: inv.Int(optPort), AddressAuthority: inv.String(optAddrAuthority)}, streams)
	printResultError(result, streams.Stderr)
	return result.ExitCode
}

// loadCommandsFile reads a commands file as written: each
// line is one device command after removing only its LF or CRLF terminator;
// blank and comment lines are sent; a final terminator adds no command.
func loadCommandsFile(path string, stdin io.Reader) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, errorcodes.Errorf("commands_file_unreadable", "commands file %s: %w", path, err)
		}
		defer f.Close()
		if info, err := f.Stat(); err == nil && info.IsDir() {
			return nil, errorcodes.Errorf("commands_file_is_directory", "commands file %s is a folder; --cf names one file", path)
		}
		r = f
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, errorcodes.Errorf("commands_file_unreadable", "commands file %s: %w", path, err)
	}
	if len(data) == 0 {
		return nil, errorcodes.Errorf("commands_file_empty", "commands file %s has zero bytes", path)
	}
	text := string(data)
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines, nil
}
