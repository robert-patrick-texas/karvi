package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/planner"
	"github.com/robert-patrick-texas/karvi/internal/transportselect"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/records"
)

// ExecuteCommand runs the command workflow on the first device of the
// assembled target set through the same plan-and-package path
// as a run: one target, serial, width 1, the
// activity ID as the job ID of its local package.
func ExecuteCommand(ctx context.Context, opts CommandOptions, streams IO) ActivityResult {
	if opts.Format == "" {
		opts.Format = "text"
	}
	cfg, operator, err := prepareConfig(opts.CommonOptions)
	if err != nil {
		return failedResult("config_load_failed", err)
	}
	// --platform is checked once here, after the configuration and before the
	// inventory is read: a value that is not a known
	// platform is refused before any inventory fault could mask it.
	platformName, err := CheckPlatformOption(cfg, opts.Platform)
	if err != nil {
		return failedResult("platform_option_unknown", err)
	}
	set, err := assembleTargets(ctx, cfg, operator, opts.Targets, opts.Excludes, streams.Stderr)
	if err != nil {
		return failedResult("inventory_load_failed", err)
	}
	if err := applyManagementAddress(&set, opts.Address); err != nil {
		return failedResult("management_address_invalid", err)
	}
	candidates := set.CandidateCount()
	d := firstDevice(set, platformName, opts.Transport, opts.Port)
	overrides := applyFirstAuthority(&d, opts.AddressAuthority)
	if err := d.Validate(); err != nil {
		return failedResult("device_invalid", err)
	}
	one := planner.TargetSet{Devices: []inventory.Device{d}, Provenance: set.Provenance, Order: set.Order, ShuffleKey: set.ShuffleKey}
	// The activity's ID is its job directory, reserved before the draft;
	// the reservation is released when the
	// plan or the package fails before the job runs.
	id, release, err := reserveActivityID(cfg, operator, time.Now())
	if err != nil {
		return failedResult("activity_id_generation_failed", err)
	}
	// The reservation is released whatever ends the activity: the release
	// removes an empty
	// directory alone, so a job that wrote its files keeps them and an
	// admission refused before any file leaves no folder behind.
	defer release()
	draft := planner.DraftOptions{
		ActivityType: "command", Commands: opts.Commands, CommandsFile: opts.CommandsFile, Inputs: opts.Targets, BlindReturns: opts.BlindReturns, Blind: opts.Blind, Expectations: opts.Expectations, Timeouts: opts.Timeouts, MaxBytes: opts.MaxBytes,
		Transport: opts.Transport, ContinueDeviceOnError: opts.ContinueDeviceOnError, Collection: opts.Collection, Suffix: opts.Suffix,
		Format: opts.Format, Echo: opts.Echo, DynamicBorder: opts.DynamicBorder, NoBorder: opts.NoBorder, Follow: true,
		Address: planner.AddressOptions{Overrides: overrides, Warn: func(s string) { warning(streams.Stderr, s) }},
		Warn:    func(s string) { warning(streams.Stderr, s) },
	}
	authorities := []string{}
	if opts.AddressAuthority != "" {
		authorities = append(authorities, opts.AddressAuthority)
	}
	selection := records.Selection{Inputs: append([]records.TargetInput{}, opts.Targets...), Excludes: nonNilStrings(opts.Excludes), ManagementAddress: opts.Address, AddressAuthorities: authorities}
	job, err := planAndPackage(ctx, cfg, operator, one, draft, id, streams)
	if err != nil {
		return failedResult("execution_plan_invalid", err)
	}
	defer job.Destroy()
	return jobexec.Run(ctx, jobexec.Request{
		Plan: job.Plan, Header: job.Header, Package: job.Projection, Grants: job.Package, Protection: executionplan.ProtectionLocalPeer, Mode: executionplan.ModeLive,
		Config: cfg, Operator: operator, Quiet: opts.Quiet, Debug: opts.Debug,
		ActivityType: "command", ActivityID: id,
		Selection: selection, CandidateCount: candidates,
	}, streams)
}

// ExecuteRunLocal plans and executes a run in the current process: the
// no-daemon path, and until the schema 5 switch the daemon's own path after
// peer-UID verification. It accepts no credential values in RunOptions.
func ExecuteRunLocal(ctx context.Context, opts RunOptions, streams IO) ActivityResult {
	if opts.Format == "" {
		opts.Format = "text"
	}
	cfg, operator, err := prepareConfig(opts.CommonOptions)
	if err != nil {
		return failedResult("config_load_failed", err)
	}
	// --platform is checked before the inventory is read, as in command.
	platformName, err := CheckPlatformOption(cfg, opts.Platform)
	if err != nil {
		return failedResult("platform_option_unknown", err)
	}
	set, err := assembleTargets(ctx, cfg, operator, opts.Targets, opts.Excludes, streams.Stderr)
	if err != nil {
		return failedResult("inventory_load_failed", err)
	}
	overridePlatform(&set, platformName)
	if err := applyManagementAddress(&set, opts.ManagementAddress); err != nil {
		return failedResult("management_address_invalid", err)
	}
	overrides, err := applyAuthorityOverrides(&set, opts.AddressAuthorities)
	if err != nil {
		return failedResult("address_authority_target_unknown", err)
	}
	// The run's ID is its job directory, reserved before the draft (chapter
	// 11); a caller that already holds an ID passes it and keeps its own
	// reservation.
	id, release := opts.ActivityID, func() {}
	if id == "" {
		if id, release, err = reserveActivityID(cfg, operator, time.Now()); err != nil {
			return failedResult("activity_id_generation_failed", err)
		}
	}
	// Released whatever ends the run: only an empty directory goes, so a
	// refused admission
	// leaves no folder and a job that wrote files keeps them.
	defer release()
	draft := planner.DraftOptions{
		ActivityType: "run", Commands: opts.Commands, CommandsFile: opts.CommandsFile, Inputs: opts.Targets, BlindReturns: opts.BlindReturns, Blind: opts.Blind, Expectations: opts.Expectations, Timeouts: opts.Timeouts, MaxBytes: opts.MaxBytes,
		ContinueDeviceOnError: opts.ContinueDeviceOnError, Transport: opts.Transport,
		PlatformCommands: opts.PlatformCommands, Collection: opts.Collection, Suffix: opts.Suffix,
		Format: opts.Format, Echo: opts.Echo, DynamicBorder: opts.DynamicBorder, NoBorder: opts.NoBorder, Follow: !opts.Detach,
		Address: planner.AddressOptions{Overrides: overrides, Warn: func(s string) { warning(streams.Stderr, s) }},
		Warn:    func(s string) { warning(streams.Stderr, s) },
	}
	selection := records.Selection{Inputs: append([]records.TargetInput{}, opts.Targets...), Excludes: nonNilStrings(opts.Excludes), ManagementAddress: opts.ManagementAddress, AddressAuthorities: nonNilStrings(opts.AddressAuthorities)}
	job, err := planAndPackage(ctx, cfg, operator, set, draft, id, streams)
	if err != nil {
		return failedResult("execution_plan_invalid", err)
	}
	defer job.Destroy()
	return jobexec.Run(ctx, jobexec.Request{
		Plan: job.Plan, Header: job.Header, Package: job.Projection, Grants: job.Package, Protection: executionplan.ProtectionLocalPeer, Mode: executionplan.ModeLive,
		Config: cfg, Operator: operator, Quiet: opts.Quiet, Debug: opts.Debug,
		ActivityType: "run", ActivityID: id,
		Selection: selection,
	}, streams)
}

// RenderRunOutput renders a job directory's records under the invocation's
// configuration, as the follow step of a daemon-backed run.
func RenderRunOutput(common CommonOptions, artifactDir, format string, echo, dynamicBorder, noBorder bool, summary *records.Summary, out io.Writer) error {
	if format == "" {
		format = "text"
	}
	if format == "jsonl" {
		return jobexec.RenderRunOutput(configload.Snapshot{}, common.Quiet, common.Debug, artifactDir, format, echo, dynamicBorder, noBorder, summary, out)
	}
	cfg, _, err := prepareConfig(common)
	if err != nil {
		return err
	}
	return jobexec.RenderRunOutput(cfg, common.Quiet, common.Debug, artifactDir, format, echo || cfg.Bool("display.run.echo"), dynamicBorder, noBorder, summary, out)
}

// failedResult reports err under the site code that names its cause, unless err
// carries a more specific registered code; the exit comes from the registry.
func failedResult(code string, err error) ActivityResult {
	exit, message := siteFailure(code, err)
	return ActivityResult{ExitCode: exit, ExitName: exitcode.ExitName(exit), Error: message}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// siteFailure returns the registry exit and operator text for err reported at a

// codedText renders err for an operator under code unless err carries a more

func warning(w io.Writer, msg string) {
	if w != nil {
		fmt.Fprintf(w, "warning: %s\n", msg)
	}
}

func hostname() string { h, _ := os.Hostname(); return h }

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

func valueOr(v, d int) int {
	if v != 0 {
		return v
	}
	return d
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// firstDevice returns the first device of the ordered set with the direct-mode
// overrides applied: --platform names the platform definition, and
// --transport and --port replace the device's own values. platformName is the
// known, normalised name CheckPlatformOption returned;
// it is set, so no notice follows.
func firstDevice(set TargetSet, platformName, transport string, port int) inventory.Device {
	d := withPlatform(set.Devices[0], platformName)
	if transport != "" {
		d.Transport = strings.ToLower(transport)
		d.TransportExplicit = true
	}
	if port != 0 {
		d.Port = uint16(port)
	}
	return d
}

// prepareConfig loads the operator and configuration; every error it returns
// carries a registered code.
func prepareConfig(common CommonOptions) (configload.Snapshot, credentials.Operator, error) {
	operator, err := osutil.CurrentOperator()
	if err != nil {
		return configload.Snapshot{}, credentials.Operator{}, errorcodes.Ensure(err, "operator_identity_unavailable")
	}
	cfg, err := configload.Load(configload.Options{ExplicitRoots: common.ConfigRoots, Sets: common.Sets, FlagValues: common.ConfigFlags, HomeDir: operator.Home})
	if err == nil {
		err = transportselect.ValidateConfigured(cfg)
	}
	if err != nil {
		return configload.Snapshot{}, credentials.Operator{}, ConfigLoadError(err)
	}
	return cfg, operator, nil
}

// ConfigLoadError codes a configuration loading or validation failure and fixes
// its exit to the configuration contract: 3 for a lock violation, otherwise 2,
// whatever the specific code's own exit.
func ConfigLoadError(err error) error {
	err = errorcodes.Ensure(err, "config_load_failed")
	if errorcodes.Of(err) == "config_lock_violation" {
		return errorcodes.WithExit(err, exitcode.ExitConfigLockViolation)
	}
	return errorcodes.WithExit(err, exitcode.ExitConfigValidationError)
}

// siteFailure returns the registry exit and operator text for err reported at a
// site whose cause is code.
func siteFailure(code string, err error) (int, string) {
	err = errorcodes.Ensure(err, code)
	return errorcodes.ExitAt(err, code), errorcodes.Message(err)
}

// codedText renders err for an operator under code unless err carries a more
// specific code.
func codedText(code string, err error) string {
	return errorcodes.Message(errorcodes.Ensure(err, code))
}

func producerInfo() records.Producer {
	return records.Producer{Hostname: hostname(), BootID: osutil.BootID(), PID: os.Getpid(), ProcessStartIdentity: osutil.ProcessStartIdentity(os.Getpid()), AppVersion: buildinfo.Version}
}
