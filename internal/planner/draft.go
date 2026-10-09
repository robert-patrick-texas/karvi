package planner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
	"github.com/robert-patrick-texas/karvi/internal/transportselect"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/records"
)

// TargetSet is the assembled, deduplicated, excluded, and ordered device
// list. Order and ShuffleKey are recorded so any order can be
// reproduced: Order is the canonical name, and ShuffleKey is
// set for shuffle and random (for random, the epoch seconds at planning
// time). The set's order is the dispatch order and the plan's target order.
type TargetSet struct {
	Devices    []inventory.Device
	Provenance inventory.Provenance
	Order      string
	ShuffleKey *string
}

// CandidateCount is the number of devices in the ordered set.
func (s TargetSet) CandidateCount() int { return len(s.Devices) }

// DraftOptions carry what the command line decided.
// A zero numeric value means "not given", as the CLI passes it, and the
// configuration or CPU default applies.
type DraftOptions struct {
	// ActivityType is command or run; it selects the transport mode default
	// and the echo key.
	ActivityType string
	Commands     []string
	// Inputs are the invocation's target inputs as given and CommandsFile
	// the --cf path; the plan carries them for the scoreboard as
	// sources.inputs and commands_file.
	Inputs       []records.TargetInput
	CommandsFile string
	// PlatformCommands makes the plan carry each target platform's
	// crun-commands list when Commands is empty; a platform without a
	// list refuses the draft.
	// Collection is the word that asks for a collection (crun, or run or
	// command given --cd), empty for none: the plan's output then carries
	// the collection sub-block, crun.directory resolved, crun.file-mode,
	// and the word. A crun's collection replaces output.NAME.txt and
	// takes its platforms' crun-filters; another word's does neither.
	PlatformCommands bool
	Collection       string
	// Suffix is --fs, carried in the collection sub-block and appended to
	// each device's collection file name.
	Suffix string
	// BlindReturns is empty or one count per command, the client's
	// interpretation of the trailing \r escapes and --blind-return
	// flags. Blind is empty or one flag per
	// command, the tolerance --blind declares or a count above zero
	// implies; Expectations is empty or one list per command, the --expect
	// declarations in the order given. The
	// planner copies them into the plan with every list present, since the
	// plan's lists are never null, and the plan's Validate applies the
	// rules (the bounds, the patterns, no count with a declaration).
	BlindReturns []int
	Blind        []bool
	Expectations [][]executionplan.Expectation
	// Timeouts and MaxBytes are each empty or one entry per command, the
	// --timeout in nanoseconds and the --maxbytes in bytes, 0 the job's
	// value; the parser checked the ranges and the blind conflict, and
	// Draft refuses a timeout above a set execution.device-timeout and a
	// limit above output.max-job-bytes.
	Timeouts []int64
	MaxBytes []int64

	// The dispatch settings are the configuration's dispatch.* keys alone:
	// run's Dispatch options reach them as overrides in the lock-aware cli
	// layer.
	ContinueDeviceOnError bool

	// Transport is the --transport override applied to every target.
	Transport string

	// Warn receives the planning warning lines (a not-set or fallen-back
	// platform), printed once by the client on
	// every path, --detach and --quiet included; nil drops them.
	Warn func(string)

	Format        string
	Echo          bool
	DynamicBorder bool
	NoBorder      bool
	Follow        bool

	// Address carries the authority overrides and the test injections.
	Address AddressOptions

	// PlanID replaces the fresh identifier, for golden tests.
	PlanID string
	// CPUs replaces the effective CPU count, for golden tests.
	CPUs int
}

// Draft builds the execution plan for the set:
// one target per device in the set's order with the projection's transport
// fixed to the selection kind and its port to the effective port, the
// address plan, and every setting as the effective value after
// configuration, locks, and the command line. The transport preflight,
// Telnet gate included, runs here and fails before a job exists.
func Draft(ctx context.Context, cfg configload.Snapshot, operator credentials.Operator, set TargetSet, opts DraftOptions, now time.Time) (executionplan.ExecutionPlan, error) {
	// A crun with no command takes each platform's list, so
	// its scope is the devices alone.
	if len(set.Devices) == 0 || (len(opts.Commands) == 0 && !opts.PlatformCommands) {
		return executionplan.ExecutionPlan{}, errorcodes.Errorf("activity_scope_empty", "at least one device and one command are required")
	}
	if opts.ActivityType == "" {
		opts.ActivityType = "run"
	}
	if err := checkCommandBounds(cfg, opts.Timeouts, opts.MaxBytes); err != nil {
		return executionplan.ExecutionPlan{}, err
	}
	// The platforms first: inventory validation
	// with no lookups, one pass over the whole set, refusing every unknown
	// value at once before the daemon gate, the transport preflight, and
	// the address plan.
	platforms, err := ResolvePlatforms(cfg, set.Devices, opts.Warn)
	if err != nil {
		return executionplan.ExecutionPlan{}, err
	}
	if err := GateDaemonResolution(cfg, set.Devices, opts.Address.Overrides); err != nil {
		return executionplan.ExecutionPlan{}, err
	}
	transports, err := preflightTransports(cfg, opts.ActivityType, opts.Transport, set.Devices)
	if err != nil {
		return executionplan.ExecutionPlan{}, err
	}
	addresses, err := PlanAddresses(ctx, cfg, set.Devices, opts.Address)
	if err != nil {
		return executionplan.ExecutionPlan{}, err
	}
	tables := cfg.NamedTables("platform")
	targets := make([]executionplan.ExecutionTarget, len(set.Devices))
	rootChecked := false
	for i, d := range set.Devices {
		sel := transports[i]
		// The platform used was resolved above: the plan's device.platform
		// and the effective port carry it, while the set's device keeps the
		// set platform for the maps.
		used := d
		used.Platform = platforms[i].Platform
		device := executionplan.ProjectDevice(used)
		device.Transport = sel.Kind
		device.TransportSelector = sel.Selector
		device.Port = resolver.EffectivePort(used, sel.Kind, cfg)
		def := platform.Resolve(used.Platform, tables)
		if def.Channel == platform.ChannelExec && sel.Kind == transportselect.KindSystem && !rootChecked {
			// The first exec target over the system transport checks the
			// control-path root its master's socket goes under.
			if err := checkControlPathRoot(cfg, operator); err != nil {
				return executionplan.ExecutionPlan{}, err
			}
			rootChecked = true
		}
		channel, err := targetChannel(def, d.CanonicalName, sel)
		if err != nil {
			return executionplan.ExecutionPlan{}, err
		}
		t := executionplan.ExecutionTarget{TargetID: d.ID, InputTarget: d.SuppliedName(), Device: device, AddressPlan: addresses[i], Channel: channel, ExecutionEndpoint: executionplan.EndpointLocal}
		if n := platforms[i].Notice; n != nil {
			// The notice rides the target to the daemon for the device's
			// first record; absent when there is none.
			t.Notices = []executionplan.TargetNotice{*n}
		}
		sum, err := executionplan.SumTarget(t)
		if err != nil {
			return executionplan.ExecutionPlan{}, err
		}
		t.SourceDigest = sum
		targets[i] = t
	}
	planID := opts.PlanID
	if planID == "" {
		if planID, err = osutil.NewID(now); err != nil {
			return executionplan.ExecutionPlan{}, errorcodes.Ensure(err, "activity_id_generation_failed")
		}
	}
	provenance := set.Provenance
	if provenance.Sources == nil {
		provenance.Sources = []inventory.SourceRef{}
	}
	commandsFile := ""
	if opts.CommandsFile != "" {
		commandsFile = filepath.Base(opts.CommandsFile)
	}
	out, err := outputSettings(cfg, operator, opts)
	if err != nil {
		return executionplan.ExecutionPlan{}, err
	}
	if out.Persist && out.Files.OutputTxt || out.Collection != nil {
		if err := checkFileNames(targets, out.CropToDot, out.Collection); err != nil {
			return executionplan.ExecutionPlan{}, err
		}
	}
	var lists map[string][]string
	if opts.PlatformCommands && len(opts.Commands) == 0 {
		if lists, err = platformCommandLists(targets, cfg.NamedTables("platform")); err != nil {
			return executionplan.ExecutionPlan{}, err
		}
	}
	var filters map[string][]string
	if opts.Collection == "crun" {
		filters = platformFilterLists(targets, cfg.NamedTables("platform"))
	}
	plan := executionplan.ExecutionPlan{
		SchemaVersion: executionplan.SchemaVersion, PlanID: planID,
		Operator: executionplan.Operator{Username: operator.Username, UID: operator.UID, PrimaryGID: operator.PrimaryGID, Groups: append([]string(nil), operator.Groups...)},
		Targets:  targets,
		Commands: append([]string{}, opts.Commands...), CommandsFile: commandsFile, PlatformCommands: lists, PlatformFilters: filters, CommandPlanDigest: executionplan.SumCommandPlan(opts.Commands, lists),
		BlindReturns: append([]int{}, opts.BlindReturns...), BlindWaitNS: cfg.Duration("execution.blind-wait").Nanoseconds(),
		Blind: append([]bool{}, opts.Blind...), Expectations: copyExpectations(opts.Expectations),
		TimeoutsNS: append([]int64{}, opts.Timeouts...), MaxBytes: append([]int64{}, opts.MaxBytes...),
		SessionInit: map[string]executionplan.SessionInitProfile{},
		Dispatch:    dispatchSettings(cfg, set, opts),
		Execution:   ExecutionSettings(cfg),
		Output:      out,
		Ping:        pingSettings(cfg),
		Sources:     executionplan.SourceDigests{ConfigDigest: cfg.Digest, Selectors: provenance, Inputs: ScopeInputs(opts.Inputs)},
		Planning:    executionplan.PlanningTimestamps{DraftedAt: now},
		Preparation: []executionplan.PreparationEvidence{},
	}
	if err := plan.Validate(executionplan.Draft); err != nil {
		return executionplan.ExecutionPlan{}, err
	}
	return plan, nil
}

// checkCommandBounds refuses a command's own bound above the ceiling the
// site set for it: a --timeout above a set execution.device-timeout (0 is no
// ceiling), which the device's whole list could never give it, and a
// --maxbytes above output.max-job-bytes, as the configuration refuses a
// command limit above the job's. Each names both values and the --set that
// raises the ceiling, so a ceiling is raised in the open, never by a
// declaration.
func checkCommandBounds(cfg configload.Snapshot, timeouts, maxBytes []int64) error {
	if ceiling := cfg.Duration("execution.device-timeout"); ceiling > 0 {
		for i, ns := range timeouts {
			if d := time.Duration(ns); d > ceiling {
				return errorcodes.Errorf("timeout_over_device_timeout", "command %d: --timeout %s is above execution.device-timeout %s, the bound on the device's whole list; lower the --timeout or raise the ceiling with --set execution.device-timeout=%s", i+1, d, ceiling, d)
			}
		}
	}
	limit := cfg.Int64("output.max-job-bytes")
	for i, n := range maxBytes {
		if n > limit {
			return errorcodes.Errorf("maxbytes_over_job_limit", "command %d: --maxbytes %d is above output.max-job-bytes %d, the bound on the job's whole output; lower the --maxbytes or raise the limit with --set output.max-job-bytes=%d", i+1, n, limit, n)
		}
	}
	return nil
}

// targetChannel is a target's channel, its platform's, resolved once here
// and carried in the plan. A platform that says exec over telnet is refused,
// naming both: telnet has no exec channel; both SSH transports have it.
func targetChannel(def platform.Definition, name string, sel transportselect.Selection) (string, error) {
	channel := def.Channel
	if channel == "" {
		channel = platform.ChannelShell
	}
	if channel == platform.ChannelExec && sel.Kind == transportselect.KindTelnet {
		return "", errorcodes.Errorf("channel_exec_over_telnet", "%s: platform %s asks for an exec channel and the transport is telnet, which has none; choose an SSH transport or a platform on the shell channel", name, def.Name)
	}
	return channel, nil
}

// checkControlPathRoot refuses a control-path root too long for a control
// socket, where the activity would resolve it; nothing is made.
func checkControlPathRoot(cfg configload.Snapshot, operator credentials.Operator) error {
	base, err := osutil.BaseDirPath(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return err
	}
	// A root that names a path and would be refused is the activity's
	// refusal, at the same point before any device; only its length is
	// checked here.
	root, err := osutil.ControlPathRootPlace(cfg.String("ssh.control-path-root"), base, operator.Home, operator.Username, operator.UID)
	if root.Path == "" {
		return err
	}
	return osutil.CheckControlPathRoot(root.Path)
}

// copyExpectations copies the per-command declaration lists so the plan owns
// them and none is null: a nil outer list becomes empty, and a nil inner
// list (a command with no declaration) becomes an empty list, the shape the
// schema and Validate require.
func copyExpectations(lists [][]executionplan.Expectation) [][]executionplan.Expectation {
	out := make([][]executionplan.Expectation, 0, len(lists))
	for _, l := range lists {
		out = append(out, append([]executionplan.Expectation{}, l...))
	}
	return out
}

// preflightTransports resolves each device's transport selection under the
// mode's rules (a CLI override governs every target; an explicit inventory
// selector its device; otherwise the mode default) and applies the Telnet
// gate.
func preflightTransports(cfg configload.Snapshot, mode, override string, devices []inventory.Device) ([]transportselect.Selection, error) {
	cache := map[string]transportselect.Selection{}
	out := make([]transportselect.Selection, len(devices))
	for i, d := range devices {
		selector := override
		if selector == "" && d.TransportExplicit && d.Transport != "" {
			selector = d.Transport
		}
		key := strings.ToLower(strings.TrimSpace(selector))
		sel, ok := cache[key]
		if !ok {
			var err error
			sel, err = transportselect.Resolve(cfg, mode, selector)
			if err != nil {
				return nil, errorcodes.Ensure(errorcodes.Errorf(errorcodes.Of(err), "device %s transport: %v", d.CanonicalName, err), "transport_unavailable")
			}
			if sel.Kind == transportselect.KindTelnet && !cfg.Bool("security.allow-telnet") {
				return nil, errorcodes.Errorf("telnet_not_allowed", "device %s selects Telnet but security.allow-telnet is false", d.CanonicalName)
			}
			cache[key] = sel
		}
		out[i] = sel
	}
	return out, nil
}

// dispatchSettings computes the effective dispatch values exactly as
// ExecuteRunLocal does today: the CLI value, then the
// configuration, then the CPU-derived default. command is serial, width 1.
func dispatchSettings(cfg configload.Snapshot, set TargetSet, opts DraftOptions) executionplan.DispatchSettings {
	cpus := opts.CPUs
	if cpus <= 0 {
		cpus = osutil.EffectiveCPU().EffectiveCPUs
	}
	mode := cfg.String("dispatch.default")
	width := firstPositive(cfg.Int("dispatch.parallel-workers"), cpus, 1)
	startWidth := firstPositive(cfg.Int("dispatch.wave-start-width"), minInt(64, maxInt(16, 4*cpus)))
	maxWidth := firstPositive(cfg.Int("dispatch.wave-max-width"), minInt(256, maxInt(32, 8*cpus)))
	if opts.ActivityType == "command" {
		mode, width = executionplan.DispatchSerial, 1
	}
	order := set.Order
	if order == "" {
		order = executionplan.OrderDefault
	}
	return executionplan.DispatchSettings{
		Mode: mode, Width: width, StartWidth: startWidth, MaxWidth: maxWidth,
		DispatchOrder: order, ShuffleKey: set.ShuffleKey,
		HaltErrorCount: cfg.Int("dispatch.halt-on-error-count"), HaltErrorPercent: cfg.Int("dispatch.halt-on-error-percent"),
		WaveGateErrorCount: cfg.Int("dispatch.wave-gate-error-count"), WaveGateErrorPercent: cfg.Int("dispatch.wave-gate-error-percent"),
		WaveGateTimedDelayNS: int64(cfg.Duration("dispatch.wave-gate-timed-delay")),
		// The effective halt: --continue-device-on-error or the key set
		// false, so a daemon's job halts as its invocation said.
		ContinueDeviceOnError: opts.ContinueDeviceOnError || !cfg.Bool("execution.halt-device-on-command-error"),
	}
}

// ExecutionSettings is the plan's timeouts block from the effective
// configuration, which the executor and the transports read on every path.
func ExecutionSettings(cfg configload.Snapshot) executionplan.ExecutionSettings {
	return executionplan.ExecutionSettings{
		CommandTimeoutNS: cfg.Duration("execution.command-timeout").Nanoseconds(), DeviceTimeoutNS: cfg.Duration("execution.device-timeout").Nanoseconds(),
		PromptTimeoutNS: cfg.Duration("execution.prompt-timeout").Nanoseconds(), EnableTimeoutNS: cfg.Duration("execution.enable-timeout").Nanoseconds(),
		TelnetReadTimeoutNS: cfg.Duration("telnet.read-timeout").Nanoseconds(),
	}
}

// pingSettings is the plan's ICMP gate block from the effective configuration
// the plan governs at execution, so --ping and
// --noping reach the executor through it; probes are the v1 constant.
func pingSettings(cfg configload.Snapshot) executionplan.PingSettings {
	return executionplan.PingSettings{Enabled: cfg.Bool("network.ping-targets"), Probes: executionplan.PingProbes, TimeoutNS: cfg.Duration("network.ping-timeout").Nanoseconds()}
}

// outputSettings is the invocation's output as the plan carries it
// the display values, the two byte limits,
// and the files, from output.persist-command, the eight output.files keys,
// and output.root resolved here against the client's basedir and home, so
// that the daemon writes where this client's `job follow` will look. A
// root that cannot be resolved fails the draft as output_root_unavailable,
// before any daemon is asked.
func outputSettings(cfg configload.Snapshot, operator credentials.Operator, opts DraftOptions) (executionplan.OutputSettings, error) {
	format := opts.Format
	if format == "" {
		format = executionplan.FormatText
	}
	echoKey := "display.run.echo"
	if opts.ActivityType == "command" {
		echoKey = "display.command.echo"
	}
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return executionplan.OutputSettings{}, errorcodes.Ensure(err, "base_directory_unavailable")
	}
	root, err := osutil.ResolveOutputRoot(cfg.String("output.root"), cfg.String("sharedroot"), base, operator.Home)
	if err != nil {
		return executionplan.OutputSettings{}, errorcodes.Ensure(err, "output_root_unavailable")
	}
	files := outputFiles(cfg)
	var collection *executionplan.CollectionSettings
	if opts.Collection != "" {
		if opts.Collection == "crun" {
			// The collection file is the text rendered once more, so a
			// crun writes no output.NAME.txt whatever the switch says; a
			// run's folder stays as it is without --cd, since a kept
			// collection file is the previous one.
			files.OutputTxt = false
		}
		dir, err := osutil.ResolveCrunDirectory(cfg.String("crun.directory"), cfg.String("sharedroot"), base, operator.Home)
		if err != nil {
			return executionplan.OutputSettings{}, errorcodes.Ensure(err, "crun_directory_not_writable")
		}
		collection = &executionplan.CollectionSettings{Directory: dir, FileMode: cfg.String("crun.file-mode"), Word: opts.Collection, Suffix: opts.Suffix}
	}
	return executionplan.OutputSettings{
		Format: format, Echo: opts.Echo || cfg.Bool(echoKey), DynamicBorder: opts.DynamicBorder, NoBorder: opts.NoBorder, Follow: opts.Follow,
		MaxCommandBytes: cfg.Int64("output.max-command-bytes"), MaxJobBytes: cfg.Int64("output.max-job-bytes"),
		Persist:    cfg.Bool("output.persist-command"),
		Files:      files,
		Root:       root,
		CropToDot:  cfg.Bool("output.crop-to-dot"),
		Collection: collection,
	}, nil
}

// platformCommandLists is the plan's list per platform for a crun that
// names no command: each distinct target platform's
// crun-commands from its definition (the built-in's, or a table's
// replacing it). A platform without a list refuses the draft before any
// device is contacted, naming the platform and its first device.
func platformCommandLists(targets []executionplan.ExecutionTarget, tables map[string]map[string]any) (map[string][]string, error) {
	lists := map[string][]string{}
	for _, t := range targets {
		name := t.Device.Platform
		if _, done := lists[name]; done {
			continue
		}
		list := platform.Resolve(name, tables).CrunCommands
		if len(list) == 0 {
			return nil, errorcodes.Errorf("crun_platform_commands_missing", "platform %s has no crun-commands list, so %s cannot be collected without --cmd or --cf; set [platform.%s] crun-commands", name, t.TargetID, name)
		}
		lists[name] = append([]string{}, list...)
	}
	return lists, nil
}

// platformFilterLists is the plan's drop list per platform for a crun
// each distinct target platform's
// crun-filters from its definition (the built-in's, or a table's replacing
// it), the platforms without one left out; nil when none has one.
func platformFilterLists(targets []executionplan.ExecutionTarget, tables map[string]map[string]any) map[string][]string {
	lists := map[string][]string{}
	for _, t := range targets {
		name := t.Device.Platform
		if _, done := lists[name]; done {
			continue
		}
		if list := platform.Resolve(name, tables).CrunFilters; len(list) > 0 {
			lists[name] = append([]string{}, list...)
		}
	}
	if len(lists) == 0 {
		return nil
	}
	return lists
}

// checkFileNames refuses a target set in which two devices would write one
// file: core.example.net and core.example.com
// both write output.core.txt under the crop. Refused at planning, before any
// device is contacted, naming both devices and the file; only when the
// activity writes the text file, since a job without one has no file to
// collide on (a collection adds its own file to the rule, and names it,
// suffix and all, since a suffix appended to every name keeps two names
// two and one name one).
func checkFileNames(targets []executionplan.ExecutionTarget, crop bool, collection *executionplan.CollectionSettings) error {
	seen := map[string]string{}
	for _, t := range targets {
		name := output.FileName(t.Device.CanonicalName, crop)
		if prior, ok := seen[name]; ok {
			file := output.TextFileName(t.Device.CanonicalName, crop)
			if collection != nil {
				file = name + collection.Suffix
			}
			return errorcodes.Errorf("output_file_name_collision", "devices %s and %s would both write the file %s; under output.crop-to-dot the first label of a name is its file name, and two devices need two", prior, t.Device.CanonicalName, file)
		}
		seen[name] = t.Device.CanonicalName
	}
	return nil
}

// outputFiles is the eight output.files switches as the plan carries them.
func outputFiles(cfg configload.Snapshot) executionplan.OutputFiles {
	on := func(file string) bool { return cfg.Bool("output.files." + file) }
	return executionplan.OutputFiles{
		CommandsJSONL: on("commands-jsonl"), CommandsTxt: on("commands-txt"), ErrorsJSONL: on("errors-jsonl"), FailedDevicesTxt: on("failed-devices-txt"),
		ManifestJSON: on("manifest-json"), MetricsJSON: on("metrics-json"), SummaryJSON: on("summary-json"), OutputTxt: on("output-txt"),
	}
}

// OutputFilesOn reports whether an activity under cfg writes any file, and
// so has a job directory: output.persist-command on and at least one
// output.files key on (every file off is one case with `--nof`, no
// folder). The reservation of the job ID asks this before drafting.
func OutputFilesOn(cfg configload.Snapshot) bool {
	return cfg.Bool("output.persist-command") && outputFiles(cfg) != (executionplan.OutputFiles{})
}

// Header builds the public job header for plan: job_id and
// idempotency_key are both the activity ID, the client identity is this
// executable and process, and the plan digest is the final digest when the
// plan is finalized, else the draft's payload digest.
func Header(plan executionplan.ExecutionPlan, jobID string, mode executionplan.Mode) (executionplan.PublicJobHeader, error) {
	digest := plan.PlanDigest
	if digest.IsZero() {
		sum, err := executionplan.SumPlan(plan)
		if err != nil {
			return executionplan.PublicJobHeader{}, err
		}
		digest = sum
	}
	hostname, _ := os.Hostname()
	return executionplan.PublicJobHeader{
		SchemaVersion: executionplan.SchemaVersion, IdempotencyKey: jobID, JobID: jobID,
		Operator: plan.Operator,
		Client:   executionplan.ClientIdentity{AppName: buildinfo.AppName, Version: buildinfo.Version, Commit: buildinfo.Commit, PID: os.Getpid(), Hostname: hostname},
		Mode:     mode, ExecutionDomain: executionplan.ExecutionDomainLocal, Priority: 0,
		CommandPlanDigest: plan.CommandPlanDigest, PlanDigest: digest,
	}, nil
}

// CommitHeader is Header over the final plan with the package reference.
func CommitHeader(plan executionplan.ExecutionPlan, jobID string, mode executionplan.Mode, ref executionplan.PackageReference) (executionplan.PublicJobHeader, error) {
	h, err := Header(plan, jobID, mode)
	if err != nil {
		return h, err
	}
	h.CredentialPackage = &ref
	return h, nil
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 1
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ScopeInputs is the invocation's target inputs as the scoreboard shows
// them: target=NAME as {target, NAME}, a
// names file as {tf, its base name}, --all as {all}, and any other selector
// as its kind and value. Never null.
func ScopeInputs(inputs []records.TargetInput) []executionplan.ScopeInput {
	out := make([]executionplan.ScopeInput, 0, len(inputs))
	for _, in := range inputs {
		switch in.Kind {
		case "names":
			out = append(out, executionplan.ScopeInput{Kind: "tf", Value: filepath.Base(in.Source)})
		case "all":
			out = append(out, executionplan.ScopeInput{Kind: "all"})
		default:
			out = append(out, executionplan.ScopeInput{Kind: in.Kind, Value: in.Value})
		}
	}
	return out
}
