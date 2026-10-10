package app

import (
	"context"
	"fmt"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/icmpgate"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	credentialresolver "github.com/robert-patrick-texas/karvi/internal/credentialbackend"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/executor"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/planner"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/internal/transport/systemssh"
	"github.com/robert-patrick-texas/karvi/internal/transportselect"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/records"
)

// ExecuteLogin opens a real interactive OpenSSH terminal. The process-level
// wrapper in cmd/karvi supplies the controlling PTY when transcript recording
// is requested; askpass remains outside the recorded terminal data path.
func ExecuteLogin(ctx context.Context, opts LoginOptions, streams IO) ActivityResult {
	cfg, operator := opts.Config, opts.Operator
	debug := jobexec.DebugLogger(opts.CommonOptions.Debug, cfg, streams.Stderr)
	debug(fmt.Sprintf("activity=login config_digest=%s config_sources=%q", cfg.Digest, strings.Join(cfg.Sources, ",")))
	// --platform is checked once, after the configuration and before the
	// inventory is read, so a bad value fails here,
	// before a terminal or a credential is needed.
	platformName, err := CheckPlatformOption(cfg, opts.Platform)
	if err != nil {
		return failedResult("platform_option_unknown", err)
	}
	selection, err := transportselect.Resolve(cfg, "login", opts.Transport)
	if err != nil {
		return failedResult("transport_unavailable", err)
	}
	debug(fmt.Sprintf("login transport selector=%q implementation=%q kind=%s", selection.Selector, selection.Implementation, selection.Kind))
	if selection.Kind != transportselect.KindSystem {
		return failedResult("login_transport_not_interactive", fmt.Errorf("transport %s is not system-compatible SSH; login supports only system-compatible SSH transports", selection.Implementation))
	}
	// The target set is assembled as run does; login connects to
	// its first device. The interactive implementation replaces the device's
	// transport because login resolved it above.
	set, err := assembleTargets(ctx, cfg, operator, opts.Targets, opts.Excludes, streams.Stderr)
	if err != nil {
		return failedResult("inventory_load_failed", err)
	}
	if err := applyManagementAddress(&set, opts.Address); err != nil {
		return failedResult("management_address_invalid", err)
	}
	candidates := set.CandidateCount()
	d := firstDevice(set, platformName, selection.Implementation, opts.Port)
	overrides := applyFirstAuthority(&d, opts.AddressAuthority)
	if err := d.Validate(); err != nil {
		return failedResult("device_invalid", err)
	}
	if err := planner.GateDaemonResolution(cfg, []inventory.Device{d}, overrides); err != nil {
		return failedResult("daemon_resolution_not_allowed", err)
	}
	shuffleKey := ""
	if set.ShuffleKey != nil {
		shuffleKey = *set.ShuffleKey
	}
	debug(fmt.Sprintf("login target set candidates=%d selected=%q dispatch_order=%s shuffle_key=%q", candidates, d.CanonicalName, set.Order, shuffleKey))
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return failedResult("base_directory_unavailable", err)
	}
	if err := osutil.EnsureStateTree(base, operator.UID, osutil.DirectoryMode(cfg.String("output.directory-mode"))); err != nil {
		return failedResult("state_tree_create_failed", err)
	}
	// The login's ID takes the job form, YYMMDD-HHMMSS-xx, reserved by its
	// scoreboard file, the one file every login writes (a run or cmd
	// reserves by its job directory).
	// Under the recording wrapper the ID is the wrapper's, which reserved
	// it the same way, so the metadata file and the child's audit and
	// scoreboard records agree; the wrapper releases it if this process
	// never writes.
	sb, err := scoreboard.NewWriter(cfg.String("scoreboards"), operator.Home, base, cfg.Bool("watch.enabled"), func(s string) { jobexec.WriteWarning(streams.Stderr, cfg, s) })
	if err != nil {
		return failedResult("scoreboard_directory_unavailable", err)
	}
	id := strings.TrimSpace(os.Getenv(RecorderSessionIDEnv))
	if id == "" {
		location, err := display.Location(cfg.String("timezone"))
		if err != nil {
			return failedResult("activity_id_generation_failed", err)
		}
		if id, err = sb.Reserve(time.Now(), location); err != nil {
			return failedResult("activity_id_generation_failed", err)
		}
		// A refusal before the first snapshot leaves no empty file behind.
		defer sb.Release()
	} else {
		sb.Use(id)
	}
	result := ActivityResult{ActivityID: id}
	scratch, err := osutil.ResolveScratch(cfg.String("tempdir"), base, operator.Home, operator.Username, operator.UID)
	if err != nil {
		return failedResult("scratch_directory_unavailable", err)
	}
	// A login has no admission, so the scratch sweep runs here.
	osutil.SweepScratch(scratch, func(name string) {
		debug("scratch_abandoned_removed: removed the abandoned scratch file " + filepath.Join(scratch, name))
	})
	controlRoot, err := osutil.ControlPathRoot(cfg.String("ssh.control-path-root"), base, operator.Home, operator.Username, operator.UID)
	if err != nil {
		return failedResult("control_path_root_unavailable", err)
	}
	auditSink, err := audit.New(cfg, operator.Home)
	if err != nil {
		return failedResult("audit_sink_open_failed", err)
	}
	defer auditSink.Close()
	resolution, err := resolver.Resolve(ctx, cfg, d, resolver.ProbeFamilies())
	if err != nil {
		return failedResult("name_resolution_error", err)
	}
	d = resolution.Device
	debug(fmt.Sprintf("login resolved target=%q address=%s source=%s candidates=%d duration=%s", d.CanonicalName, resolution.SelectedAddress.String(), resolution.AddressSource, len(resolution.AddressCandidates), resolution.Duration))
	// The platform used is resolved once, before the definition is fetched:
	// an unknown row platform refuses the login here, before credentials,
	// and a fallen-back or not-set one prints its warning line, login's
	// notice since login has no records. The device keeps its set
	// platform until the credential resolver has matched its maps on it;
	// after that the device carries the platform used, so the
	// port, the SSH algorithms map, the display, and the records see it.
	platforms, err := planner.ResolvePlatforms(cfg, []inventory.Device{d}, func(s string) { jobexec.WriteWarning(streams.Stderr, cfg, s) })
	if err != nil {
		return failedResult("platform_unknown", err)
	}
	platformUsed := platforms[0].Platform
	definition, err := platformFor(cfg, platformUsed)
	if err != nil {
		return failedResult("platform_definition_invalid", err)
	}
	// The ICMP gate's capability, once, before credentials: an enabled
	// gate with no allowed method is refused here.
	var pinger icmpgate.Pinger
	if cfg.Bool("network.ping-targets") {
		capability := icmpgate.Detect(icmpgate.Options{Socket: cfg.Bool("network.ping-socket"), System: cfg.Bool("network.ping-system")})
		if !capability.Available {
			return failedResult("icmp_capability_unavailable", errorcodes.Errorf("icmp_capability_unavailable", "the ICMP gate is enabled but %s", capability.Reason))
		}
		pinger = capability.Pinger()
	}
	credResolver, err := credentialresolver.New(cfg, operator, func(s string) { jobexec.WriteWarning(streams.Stderr, cfg, s) })
	if err != nil {
		return failedResult("credential_resolution_failed", err)
	}
	d.PlatformUsed = platformUsed // the enable rule reads the platform used
	resolved, err := credResolver.Resolve(ctx, operator, d)
	if err != nil {
		return failedResult("credential_resolution_failed", err)
	}
	defer resolved.Credential.Material.Destroy()
	debug(fmt.Sprintf("login credential target=%q device_user=%q backend=%q policy=%q matched_category=%q matched_pattern=%q matched_source=%q", d.CanonicalName, resolved.DeviceUsername, resolved.Credential.Backend, resolved.Credential.Policy, resolved.Credential.MatchedOn.Category, resolved.Credential.MatchedOn.Pattern, resolved.Credential.MatchedOn.Source))
	d.Platform = platformUsed
	port := resolver.EffectivePort(d, selection.Kind, cfg)
	// The gate: after credential resolution, mirroring the
	// executor, and before the open, so a skipped target never reaches the
	// transport or askpass; only the selected address is probed.
	// The display's formatter, style, and width serve the ping line, the
	// header, and the footer alike.
	formatter, err := display.NewFormatter(cfg.String("display.timestamp"), cfg.String("timezone"))
	if err != nil {
		return failedResult("config_display_timestamp_invalid", err)
	}
	lineStyle := jobexec.DisplayLineStyle(cfg, jobexec.DisplayTerminal(streams.Stdout))
	terminalWidth := jobexec.DisplayTerminalWidth(streams.Stdout)
	if pinger != nil {
		gate := loginGate(ctx, pinger, resolution.SelectedAddress, cfg.Duration("network.ping-timeout"), debug)
		if ctx.Err() != nil {
			return failedResult("cancelled", ctx.Err())
		}
		// The one-line result and, under --debug, the details and the
		// packet-loss notice, to stderr since login has no records; --quiet
		// and an empty display.ping.header suppress the line, which is the
		// template rendered as the header is.
		if template := cfg.String("display.ping.header"); !opts.Quiet && template != "" {
			var notices []records.Notice
			if gate.Decision == records.PingDecisionProceedDegraded {
				notices = append(notices, records.Notice{Code: "icmp_packet_loss", Message: "ICMP packet loss 50%; proceeding because one validated reply was received"})
			}
			lines, renderErr := formatter.RenderStyledLines(template, jobexec.PingValues(d.CanonicalName, resolution.SelectedAddress.String(), &gate), lineStyle, terminalWidth)
			if renderErr != nil {
				return failedResult("config_display_template_invalid", renderErr)
			}
			if opts.Debug {
				lines = append(lines, jobexec.PingDebugLines(&gate, notices)...)
			}
			if err := writeGeneratedLines(streams.Stderr, lines); err != nil {
				return failedResult("terminal_write_failed", err)
			}
		}
		if gate.Replies == 0 {
			return failedResult("icmp_unreachable", errorcodes.Errorf("icmp_unreachable", "ICMP gate enabled: no validated reply from %s to %d probes (%s); target skipped because ping gating is enabled, no transport opened", resolution.SelectedAddress, len(gate.Outcomes), describePingOutcomes(gate.Outcomes)))
		}
	}
	// The device's SSH algorithm lists.
	algorithms, err := cfg.SelectSSHAlgorithms(matching.Fields{Name: d.CanonicalName, Address: resolution.SelectedAddress, Platform: d.Platform, Site: d.Site, Groups: d.Groups})
	if err != nil {
		return failedResult(errorcodes.Of(err), err)
	}
	if algorithms.Rule < 0 {
		debug(fmt.Sprintf("login ssh algorithms target=%q profile=global %s", d.CanonicalName, algorithms.Lists.Describe()))
	} else {
		debug(fmt.Sprintf("login ssh algorithms target=%q profile=%s rule=ssh-algorithms-map.%d %s", d.CanonicalName, algorithms.Profile, algorithms.Rule, algorithms.Lists.Describe()))
	}
	factory := systemssh.Factory{Binary: selection.Binary, Config: cfg, ScratchDir: scratch, ControlRoot: controlRoot, Home: operator.Home, BaseDir: base, MaxOutputBytes: cfg.Int64("output.max-command-bytes"), Debug: debug, Algorithms: algorithms.Lists}
	// Under insecure the policy's two lines, once, before any contact; what
	// the transport finds of the device's key (a key differing from the
	// stored one, or one not compared) on the terminal as it is found.
	if cfg.String("ssh.host-key-policy") == "insecure" {
		jobexec.WriteAdmissionWarnings(streams.Stderr, cfg, []string{jobexec.PolicyInsecureWarning()})
	}
	keyStyle := jobexec.DisplayLineStyle(cfg, jobexec.DisplayTerminal(streams.Stderr))
	debug(fmt.Sprintf("login transport opening target=%q address=%s port=%d platform=%q", d.CanonicalName, resolution.SelectedAddress.String(), port, definition.Name))
	openReq := platform.OpenRequest{Address: resolution.SelectedAddress.String(), Port: port, Username: resolved.DeviceUsername, EnablePassword: func(fn func([]byte) error) error { return resolved.Credential.Material.WithEnablePassword(fn) }, Definition: definition, Timeout: cfg.Duration("ssh.connect-timeout"), Metadata: map[string]string{"canonical_name": d.CanonicalName, "activity_type": "login", "transport_selector": selection.Selector}}
	// The notices are kept for the login's end event; the transport reports
	// them from its own goroutines (the askpass broker's among them).
	var keyNotices struct {
		sync.Mutex
		list []records.Notice
	}
	openReq.HostKeyNotice = func(n platform.HostKeyNotice) {
		notices := executor.HostKeyNotices(d.CanonicalName, []platform.HostKeyNotice{n})
		keyNotices.Lock()
		keyNotices.list = append(keyNotices.list, notices...)
		keyNotices.Unlock()
		for _, line := range jobexec.HostKeyLines(d.CanonicalName, notices, keyStyle) {
			fmt.Fprintln(streams.Stderr, line)
		}
	}
	// A credential with keys and no password offers no password method.
	if resolved.Credential.Material.PasswordSet() || len(resolved.Credential.Keys) == 0 {
		openReq.Password = func(fn func([]byte) error) error { return resolved.Credential.Material.WithPassword(fn) }
	}
	for _, k := range resolved.Credential.Keys {
		openReq.Keys = append(openReq.Keys, k.Path)
	}
	driver, err := factory.Open(ctx, openReq)
	if err != nil {
		return failedResult("connection_open_failed", err)
	}
	defer driver.Close()
	debug(fmt.Sprintf("login transport ready target=%q", d.CanonicalName))
	interactive, ok := driver.(interface {
		Interactive(context.Context, io.Reader, io.Writer, io.Writer) error
	})
	if !ok {
		return failedResult("login_driver_not_interactive", fmt.Errorf("selected transport does not support interactive sessions"))
	}
	started := time.Now()
	mode := "serial"
	externalTranscript := strings.TrimSpace(os.Getenv("KARVI_LOGIN_TRANSCRIPT_PATH"))
	snap := records.ScoreboardSnapshot{SchemaVersion: records.ScoreboardSchemaVersion, ActivityID: id, Operator: osutil.RecordOperator(operator), ActivityType: "login", Mode: "login", Status: "running", DispatchMode: &mode, Width: 1, Counts: records.Counts{Total: 1, InFlight: 1}, Target: map[string]any{"name": d.CanonicalName, "address": resolution.SelectedAddress.String()}, Targets: []records.ScoreboardTarget{{Name: d.CanonicalName, State: records.TargetRunning}}, Inputs: planner.ScopeInputs(opts.Targets), Recording: externalTranscript != "", StartedAt: started, LastUpdatedAt: started, Producer: producerInfo()}
	if err := sb.Write(snap); err != nil {
		return failedResult("scoreboard_write_failed", err)
	}
	// The heartbeat: the login's snapshot rewritten
	// every watch.refresh while the session runs, so a login longer than
	// watch.stale-after is not shown stale.
	stopHeartbeat := sb.Heartbeat(cfg.Duration("watch.refresh"), func() records.ScoreboardSnapshot { return snap })
	if err := writeLoginAudit(auditSink, operator, cfg, id, d, resolved.DeviceUsername, resolved.Credential.Backend, "started", 0, map[string]any{"diagnostic": ""}); err != nil {
		stopHeartbeat()
		return failedResult("audit_write_failed", err)
	}
	// Generated lines (header, footer) go to the terminal. Under the recording
	// wrapper the terminal is the recorder's pseudo-terminal, so they go to the
	// diagnostics stream instead and the transcript holds only the device
	// stream.
	generated := streams.Stdout
	if externalTranscript != "" {
		result.ArtifactDir = filepath.Dir(externalTranscript)
		generated = streams.Stderr
	}
	displayValues := display.Values{
		Timestamp: started, Target: d.CanonicalName, Address: resolution.SelectedAddress.String(),
		Platform: d.Platform, User: resolved.DeviceUsername,
		AuthBackend: resolved.Credential.Backend, Transport: selection.Implementation, ReferenceID: id,
		Artifacts: result.ArtifactDir, Status: "running",
	}
	if !opts.CommonOptions.Quiet {
		header, renderErr := formatter.RenderStyledLines(cfg.String("display.login.header"), displayValues, lineStyle, terminalWidth)
		if renderErr != nil {
			stopHeartbeat()
			return failedResult("config_display_template_invalid", renderErr)
		}
		if writeErr := writeGeneratedLines(generated, header); writeErr != nil {
			stopHeartbeat()
			return failedResult("terminal_write_failed", writeErr)
		}
	}
	debug(fmt.Sprintf("login interactive session starting target=%q", d.CanonicalName))
	err = interactive.Interactive(ctx, streams.Stdin, streams.Stdout, streams.Stderr)
	stopHeartbeat()
	ended := time.Now()
	debug(fmt.Sprintf("login interactive session ended target=%q elapsed=%s error=%t", d.CanonicalName, ended.Sub(started), err != nil))
	exit := exitcode.ExitSuccess
	status := "completed"
	if err != nil {
		status = "errored"
		exit = classifyLoginExit(err)
		result.Error = codedText("ssh_process_failed", err)
	}
	snap.Status, snap.LastUpdatedAt, snap.EndedAt, snap.ElapsedNS = status, ended, &ended, ended.Sub(started).Nanoseconds()
	snap.Counts = records.Counts{Total: 1, Completed: 1}
	if exit == 0 {
		snap.Counts.Succeeded = 1
		snap.Targets[0].State = records.TargetSucceeded
	} else {
		snap.Counts.Failed = 1
		snap.Targets[0].State = records.TargetFailed
		summaryCode := errorcodes.Of(errorcodes.Ensure(err, "ssh_process_failed"))
		entry, _ := errorcodes.Lookup(summaryCode)
		snap.ErrorSummary = map[string]any{"code": summaryCode, "category": entry.Category, "count": 1}
	}
	if werr := sb.Write(snap); werr != nil && exit == 0 {
		exit, result.Error = siteFailure("scoreboard_write_failed", werr)
	}
	keyNotices.Lock()
	details := executor.AuditDetails(keyNotices.list)
	keyNotices.Unlock()
	details["diagnostic"] = safeError(err)
	if aerr := writeLoginAudit(auditSink, operator, cfg, id, d, resolved.DeviceUsername, resolved.Credential.Backend, status, exit, details); aerr != nil && exit == 0 {
		exit, result.Error = siteFailure("audit_write_failed", aerr)
	}
	// The footer is the final operator-facing line. Render it after scoreboard
	// and audit finalization so <exit-status> represents the actual command
	// result, not merely the interactive child's status.
	if !opts.CommonOptions.Quiet {
		displayValues.Timestamp = ended
		displayValues.ExitStatus = fmt.Sprintf("%s(%d)", exitcode.ExitName(exit), exit)
		displayValues.ExitCode = exit
		displayValues.Status = finalLoginStatus(exit, status)
		displayValues.Elapsed = ended.Sub(started)
		footer, renderErr := formatter.RenderStyledLines(cfg.String("display.login.footer"), displayValues, lineStyle, terminalWidth)
		if renderErr != nil && exit == exitcode.ExitSuccess {
			exit, result.Error = siteFailure("config_display_template_invalid", renderErr)
		} else if renderErr == nil {
			if writeErr := writeGeneratedLines(generated, footer); writeErr != nil && exit == exitcode.ExitSuccess {
				exit, result.Error = siteFailure("terminal_write_failed", writeErr)
			}
		}
	}
	result.ExitCode, result.ExitName = exit, exitcode.ExitName(exit)
	return result
}

// loginGate runs the two probes for login and returns the ping report the
// records would carry (login has none, so it is logged and rendered only);
// a pinger error is a capability failure with two error outcomes.
func loginGate(ctx context.Context, pinger icmpgate.Pinger, address netip.Addr, timeout time.Duration, debug func(string)) records.PingReport {
	started := time.Now()
	outcomes, err := pinger.Probe(ctx, address, executionplan.PingProbes, timeout)
	report := records.PingReport{Address: address.String(), Family: icmpgate.Family(address), Method: pinger.Method(), ExecutionEndpoint: executionplan.EndpointLocal, Probes: executionplan.PingProbes, TimeoutNS: timeout.Nanoseconds(), Outcomes: []records.PingOutcome{}}
	for len(outcomes) < executionplan.PingProbes {
		detail := "not sent"
		if err != nil {
			detail = err.Error()
		}
		outcomes = append(outcomes, icmpgate.Outcome{Sequence: len(outcomes) + 1, SentAt: started, Status: icmpgate.StatusError, Detail: detail})
	}
	for _, o := range outcomes[:executionplan.PingProbes] {
		report.Outcomes = append(report.Outcomes, records.PingOutcome{Sequence: o.Sequence, SentAt: o.SentAt, Status: o.Status, RTTNS: o.RTTNS, From: o.From, Detail: o.Detail})
		debug(fmt.Sprintf("login ping address=%s method=%s seq=%d status=%s detail=%q", address, report.Method, o.Sequence, o.Status, o.Detail))
	}
	report.Replies = icmpgate.Replies(outcomes)
	report.Losses = executionplan.PingProbes - report.Replies
	report.TotalNS = time.Since(started).Nanoseconds()
	switch {
	case err != nil:
		report.Decision = records.PingDecisionCapabilityUnavailable
	case report.Replies == executionplan.PingProbes:
		report.Decision = records.PingDecisionProceed
	case report.Replies > 0:
		report.Decision = records.PingDecisionProceedDegraded
	default:
		report.Decision = records.PingDecisionSkip
	}
	debug(fmt.Sprintf("login ping gate address=%s replies=%d losses=%d decision=%s duration=%s", address, report.Replies, report.Losses, report.Decision, time.Duration(report.TotalNS)))
	return report
}

// describePingOutcomes renders the probes for a message: "1: 2.1ms; 2: timeout".
func describePingOutcomes(outcomes []records.PingOutcome) string {
	parts := make([]string, 0, len(outcomes))
	for _, o := range outcomes {
		switch {
		case o.Status == icmpgate.StatusReply && o.RTTNS != nil:
			parts = append(parts, fmt.Sprintf("%d: %.1fms", o.Sequence, float64(*o.RTTNS)/float64(time.Millisecond)))
		case o.Status == icmpgate.StatusError && o.From != "":
			parts = append(parts, fmt.Sprintf("%d: error %s from %s", o.Sequence, o.Detail, o.From))
		case o.Status == icmpgate.StatusError:
			parts = append(parts, fmt.Sprintf("%d: error %s", o.Sequence, o.Detail))
		default:
			parts = append(parts, fmt.Sprintf("%d: timeout", o.Sequence))
		}
	}
	return strings.Join(parts, "; ")
}

func finalLoginStatus(exit int, interactiveStatus string) string {
	if exit == exitcode.ExitSuccess {
		return interactiveStatus
	}
	if exit == exitcode.ExitCancelled {
		return "cancelled"
	}
	return "errored"
}

// platformFor is the definition for the platform used, refused when the
// name is not known here: a backstop behind
// the planner's resolution, which never passes an unknown name.
func platformFor(cfg configload.Snapshot, name string) (platform.Definition, error) {
	d, known := platform.Lookup(name, cfg.NamedTables("platform"))
	if !known {
		return d, errorcodes.Errorf("platform_unknown", "platform %q is not a known platform (known: %s)", name, strings.Join(platform.KnownNames(cfg.NamedTables("platform")), ", "))
	}
	return d, d.Validate()
}

func writeLoginAudit(s *audit.Sink, operator credentials.Operator, cfg configload.Snapshot, id string, d inventory.Device, username, backend, outcome string, code int, details map[string]any) error {
	eventID, _ := osutil.NewID(time.Now())
	return s.WriteAudit(records.AuditRecord{EventID: eventID, EventName: "login." + outcome, Timestamp: time.Now(), Outcome: outcome, Severity: func() string {
		if code == 0 {
			return "info"
		}
		return "warning"
	}(), Operator: osutil.RecordOperator(operator), Process: map[string]any{"pid": os.Getpid(), "version": buildinfo.Version, "host": hostname()}, ActivityID: id, Device: map[string]any{"id": d.ID, "name": d.CanonicalName, "platform": d.Platform}, DeviceIdentity: map[string]any{"device_username": username, "backend": backend}, Action: map[string]any{"mode": "login", "operation": "interactive", "command_count": 0, "command_sha256_array": []string{}}, Policy: map[string]any{"config_digest": cfg.Digest, "telnet": false, "fips_required": cfg.Bool("security.require-fips"), "ssh_host_key_policy": cfg.String("ssh.host-key-policy")}, Result: map[string]any{"exit_code": code, "code": exitcode.ExitName(code)}, Source: map[string]any{"client": "karvi"}, Details: details})
}

func writeGeneratedLine(w io.Writer, line string) error {
	if line == "" {
		return nil
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

func writeGeneratedLines(w io.Writer, lines []string) error {
	for _, line := range lines {
		if err := writeGeneratedLine(w, line); err != nil {
			return err
		}
	}
	return nil
}
func classifyLoginExit(err error) int {
	if exit := errorcodes.ExitFor(err, 0); exit != 0 {
		return exit
	}
	s := strings.ToLower(safeError(err))
	switch {
	case strings.Contains(s, "host key"):
		return exitcode.ExitHostKeyFailure
	case strings.Contains(s, "permission denied"), strings.Contains(s, "authentication"):
		return exitcode.ExitAuthenticationFailure
	case strings.Contains(s, "canceled"):
		return exitcode.ExitCancelled
	default:
		return exitcode.ExitConnectionFailure
	}
}
func safeError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// RecorderSessionIDEnv carries the session ID the recording wrapper chose, so
// the child's records use the same ID as the metadata file.
const RecorderSessionIDEnv = "KARVI_LOGIN_SESSION_ID"
