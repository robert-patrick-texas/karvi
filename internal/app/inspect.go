package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/icmpgate"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

// probeTimeout bounds the dry-run's control-plane probe; it is the timeout
// ensureDaemon uses for its own probe.
const probeTimeout = 500 * time.Millisecond

// InspectRun is run --dry-run: the shared client half, then a
// control-plane probe of
// the daemon when probe is set, then the inspection report to stdout.
// Nothing is prepared, sent, launched, or written under the jobs tree. A
// failure before the draft exists aborts as a live run would, with the
// stage named after the code; a complete draft always yields a report.
func InspectRun(ctx context.Context, opts RunOptions, probe bool, streams IO) ActivityResult {
	start := time.Now()
	if opts.ActivityID == "" {
		// A dry run writes no job directory, so it reserves none: its ID is
		// the unreserved ID of its second, in the job
		// form every activity ID takes. The configuration is loaded twice
		// on this path, once for the zone and once by the draft.
		cfg, _, err := prepareConfig(opts.CommonOptions)
		if err != nil {
			return failedResult("config_load_failed", fmt.Errorf("client planning: %w", err))
		}
		location, err := display.Location(cfg.String("timezone"))
		if err != nil {
			return failedResult("config_load_failed", fmt.Errorf("client planning: %w", err))
		}
		opts.ActivityID = osutil.UnreservedJobID(start, location)
	}
	cd, code, err := draftClient(ctx, opts, streams)
	if err != nil {
		return failedResult(code, fmt.Errorf("client planning: %w", err))
	}
	defer cd.planner.Destroy()
	var (
		daemons  = []executionplan.DaemonReadiness{}
		findings = []executionplan.Finding{}
		probeNS  *int64
		socket   string
	)
	if probe {
		t0 := time.Now()
		row, rowFindings, socketPath, err := probeDaemon(ctx, opts.CommonOptions)
		if err != nil {
			return failedResult("base_directory_unavailable", fmt.Errorf("client planning: %w", err))
		}
		d := time.Since(t0).Nanoseconds()
		probeNS, socket = &d, socketPath
		daemons = append(daemons, row)
		findings = append(findings, rowFindings...)
	}
	report, err := cd.inspectionReport(opts.ActivityID, daemons, findings, probeNS, start)
	if err != nil {
		return failedResult("plan_report_invalid", err)
	}
	if err := report.Validate(); err != nil {
		return failedResult("plan_report_invalid", err)
	}
	dispatchLine := jobexec.DispatchPlan(cd.cfg, "run", report.Plan.Dispatch).Describe()
	if err := renderInspection(streams.Stdout, opts.Format, report, socket, dispatchLine); err != nil {
		return failedResult("terminal_write_failed", err)
	}
	result := ActivityResult{ExitCode: exitcode.ExitSuccess, ExitName: exitcode.ExitName(exitcode.ExitSuccess), ActivityID: opts.ActivityID}
	if report.Outcome == records.OutcomeInvalid {
		for _, f := range report.Findings {
			if f.Severity != executionplan.SeverityError {
				continue
			}
			exit := exitcode.ExitGenericError
			if e, ok := errorcodes.Lookup(f.Code); ok && e.Exit != 0 {
				exit = e.Exit
			}
			result.ExitCode, result.ExitName, result.Error = exit, exitcode.ExitName(exit), f.Code+": "+f.Message
			break
		}
	}
	return result
}

// probeDaemon is the dry-run's only contact with the daemon (decision
// 1.4): ping through daemon.Probe under probeTimeout, never ensureDaemon.
// The readiness row and its findings describe what answered; the findings
// are returned separately because the report's outcome rule counts the
// report's findings, not the row's.
func probeDaemon(ctx context.Context, common CommonOptions) (executionplan.DaemonReadiness, []executionplan.Finding, string, error) {
	rt, err := ResolveDaemonRuntime(common)
	if err != nil {
		return executionplan.DaemonReadiness{}, nil, "", err
	}
	row := executionplan.DaemonReadiness{ExecutionEndpoint: executionplan.EndpointLocal, Capabilities: []string{}, Findings: []executionplan.Finding{}}
	findings := []executionplan.Finding{}
	// Each finding is built from a coded error so the registry's source
	// scan sees the code emitted; the finding carries the code and the
	// bare message.
	finding := func(severity string, err error) {
		message := err.Error()
		if inner := errors.Unwrap(err); inner != nil {
			message = inner.Error()
		}
		findings = append(findings, executionplan.Finding{Code: errorcodes.Of(err), Severity: severity, Stage: "daemon_probe", Message: message, Details: map[string]string{"socket": rt.Socket}})
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	probe, err := daemon.Probe(pctx, rt.Socket, rt.MaxFrame)
	var mismatch *ipc.SchemaMismatchError
	switch {
	case err == nil && probe.Compatible:
		row.Reachable, row.Status = true, probe.Status.Status
		row.PID, row.Version, row.IPCSchemaVersion = probe.Status.PID, probe.Status.Version, probe.DaemonSchema
		row.Capabilities = []string{ipc.OpPrepareJob, ipc.OpProvideCredentials, ipc.OpCommitJob}
		if row.Status != executionplan.DaemonRunning && row.Status != executionplan.DaemonDraining {
			row.Status = executionplan.DaemonRunning
		}
		if row.Status == executionplan.DaemonDraining {
			finding(executionplan.SeverityError, errorcodes.Errorf("daemon_draining", "the daemon is draining and accepts no new jobs"))
		}
	case err == nil:
		row.Reachable, row.Status = true, executionplan.DaemonIncompatible
		row.PID, row.Version, row.IPCSchemaVersion = probe.Status.PID, probe.Status.Version, probe.DaemonSchema
		row.Capabilities = []string{"ping", "status", "stop"}
		finding(executionplan.SeverityError, errorcodes.Errorf("daemon_incompatible", "the running daemon version %s uses IPC schema %d; this client is %s at schema %d, and both must match; a live run would not replace it", probe.Status.Version, probe.DaemonSchema, buildinfo.Version, ipc.SchemaVersion))
	case errors.As(err, &mismatch):
		row.Reachable, row.Status = true, executionplan.DaemonIncompatible
		row.IPCSchemaVersion = mismatch.DaemonSchema
		finding(executionplan.SeverityError, errorcodes.Errorf("daemon_incompatible", "the running daemon uses IPC schema %d; this client requires schema %d; a live run would not replace it", mismatch.DaemonSchema, ipc.SchemaVersion))
	default:
		row.Reachable, row.Status = false, executionplan.DaemonAbsent
		finding(executionplan.SeverityInfo, errorcodes.Errorf("daemon_absent", "no daemon answers at the socket; a live run would launch one"))
	}
	return row, findings, rt.Socket, nil
}

// inspectionReport builds the kind=inspection PlanReport from the draft:
// the draft plan, one target report per plan target in
// plan order, the readiness rows, the report findings, counts, and timing.
func (cd *clientDraft) inspectionReport(activityID string, daemons []executionplan.DaemonReadiness, findings []executionplan.Finding, probeNS *int64, start time.Time) (records.PlanReport, error) {
	draft := cd.draft
	sum, err := executionplan.SumPlan(draft)
	if err != nil {
		return records.PlanReport{}, err
	}
	reportID, err := osutil.NewID(time.Now())
	if err != nil {
		return records.PlanReport{}, errorcodes.Ensure(err, "activity_id_generation_failed")
	}
	// The ICMP gate's capability once per report when the gate is enabled:
	// detection sends no packet, and
	// the client's answer stands for the local daemon's. Unavailable is a
	// warning on every target; readiness and outcome are unchanged.
	intendedPing := records.IntendedPing{Enabled: draft.Ping.Enabled, Probes: draft.Ping.Probes, TimeoutNS: draft.Ping.TimeoutNS, Capability: records.CheckNotChecked}
	var pingFinding *executionplan.Finding
	if draft.Ping.Enabled {
		capability := icmpgate.Detect(icmpgate.OptionsFrom(cd.cfg))
		if capability.Available {
			intendedPing.Capability, intendedPing.Method = records.CheckAvailable, capability.Method
		} else {
			intendedPing.Capability = records.CheckUnavailable
			err := errorcodes.Errorf("icmp_capability_unavailable", "the ICMP gate is enabled but %s; a live run would refuse the job at commit", capability.Reason)
			pingFinding = &executionplan.Finding{Code: errorcodes.Of(err), Severity: executionplan.SeverityWarning, Stage: "ping", Message: strings.TrimPrefix(err.Error(), errorcodes.Of(err)+": ")}
		}
	}
	targets := make([]records.TargetReport, 0, len(draft.Targets))
	byReadiness, byAuthority := map[string]int{}, map[string]int{}
	for _, t := range draft.Targets {
		tr := records.TargetReport{
			TargetID: t.TargetID, Findings: []executionplan.Finding{},
			Address:           records.AddressReport{Authority: t.AddressPlan.Authority, ClientCandidates: t.AddressPlan.ClientCandidates, DaemonCandidates: t.AddressPlan.DaemonCandidates, Selected: t.AddressPlan.Selected},
			IntendedPing:      intendedPing,
			IntendedTransport: records.IntendedTransport{Transport: t.Device.Transport, Port: effectivePort(t), Available: records.CheckNotChecked},
		}
		if id, grant, ok := cd.planner.Binding(t.TargetID); ok {
			tr.Readiness = records.ReadinessPlanned
			tr.Address.ResolutionActor = t.AddressPlan.ResolverContext
			if tr.Address.ResolutionActor == "" {
				tr.Address.ResolutionActor = executionplan.ResolverContextClient
			}
			matched := grant.MatchedOn
			tr.CredentialBinding = records.CredentialBindingReport{Status: records.BindingBound, CredentialID: id, Policy: grant.Policy, Backend: grant.Backend, DeviceUsername: grant.DeviceUsername, MatchedOn: &matched}
			tr.IntendedTransport.SessionInitProfile, _ = cd.planner.SessionInitProfile(t.TargetID)
		} else {
			tr.Readiness = records.ReadinessDeferred
			tr.Address.ResolutionActor = records.Deferred
			tr.CredentialBinding = records.CredentialBindingReport{Status: records.Deferred}
			tr.IntendedTransport.SessionInitProfile = records.Deferred
		}
		if pingFinding != nil {
			f := *pingFinding
			f.TargetID = t.TargetID
			tr.Findings = append(tr.Findings, f)
		}
		// The target's planning notices (a not-set or fallen-back platform)
		// as warning findings; readiness unchanged.
		tr.Findings = append(tr.Findings, jobexec.NoticeFindings(t)...)
		byReadiness[tr.Readiness]++
		byAuthority[string(t.AddressPlan.Authority)]++
		targets = append(targets, tr)
	}
	errorsN, warningsN := 0, 0
	for _, t := range targets {
		for _, f := range t.Findings {
			if f.Severity == executionplan.SeverityWarning {
				warningsN++
			}
		}
	}
	for _, f := range findings {
		switch f.Severity {
		case executionplan.SeverityError:
			errorsN++
		case executionplan.SeverityWarning:
			warningsN++
		}
	}
	outcome := records.OutcomePlanned
	if errorsN > 0 {
		outcome = records.OutcomeInvalid
	}
	ns := func(d time.Duration) *int64 { v := d.Nanoseconds(); return &v }
	return records.PlanReport{
		SchemaVersion: records.PlanReportSchemaVersion, Kind: records.ReportInspection, ReportID: reportID, GeneratedAt: time.Now(),
		Producer: producerInfo(), ProducerRole: records.ProducerClient, Operator: osutil.RecordOperator(cd.operator), ActivityID: activityID,
		Plan: draft, PlanDigest: sum, CommandPlanDigest: draft.CommandPlanDigest,
		Daemons: daemons, Preparations: []executionplan.PreparationReport{},
		Targets:  targets,
		Counts:   records.ReportCounts{Targets: len(targets), ByReadiness: byReadiness, ByAuthority: byAuthority, Commands: draft.CommandCount(), Errors: errorsN, Warnings: warningsN},
		Timing:   records.ReportTiming{ClientConfigNS: ns(cd.timing.config), ClientInventoryNS: ns(cd.timing.inventory), ClientDNSNS: ns(cd.timing.dns), ClientCredentialNS: ns(cd.timing.credential), DaemonProbeNS: probeNS, TotalNS: time.Since(start).Nanoseconds()},
		Findings: findings, Outcome: outcome,
		JobSubmitted: false, TargetDataSubmitted: false, DeviceContacted: false, NextOperation: records.NextCommitLiveJob,
	}, nil
}

func effectivePort(t executionplan.ExecutionTarget) uint16 {
	if t.Device.Port != 0 {
		return t.Device.Port
	}
	if t.Device.Transport == "telnet" {
		return 23
	}
	return 22
}

// renderInspection writes the report in the run's --format: json
// indented, jsonl on one line, text in the inspection report's shape, its
// dispatch line dispatchLine, the dispatch as the job would run it
// (dispatch.Plan.Describe), which the plan's settings alone cannot say.
func renderInspection(out io.Writer, format string, r records.PlanReport, socket, dispatchLine string) error {
	switch format {
	case "json":
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s\n", b)
		return err
	case "jsonl":
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s\n", b)
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "dry-run %s\n", r.ActivityID)
	fmt.Fprintf(&b, "outcome: %s\n", r.Outcome)
	fmt.Fprintf(&b, "job_submitted: %t\ntarget_data_submitted: %t\ndevice_contacted: %t\n", r.JobSubmitted, r.TargetDataSubmitted, r.DeviceContacted)
	if len(r.Daemons) == 0 {
		b.WriteString("daemon: not used (--no-daemon)\n")
	}
	for _, d := range r.Daemons {
		switch d.Status {
		case executionplan.DaemonAbsent:
			fmt.Fprintf(&b, "daemon %s: absent (no daemon answers; a live run would launch one)", d.ExecutionEndpoint)
		default:
			fmt.Fprintf(&b, "daemon %s: %s pid=%d version=%s ipc_schema=%d", d.ExecutionEndpoint, d.Status, d.PID, d.Version, d.IPCSchemaVersion)
		}
		if socket != "" {
			fmt.Fprintf(&b, " socket=%s", socket)
		}
		b.WriteString("\n")
	}
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "finding: %s %s %s\n", f.Severity, f.Code, f.Message)
	}
	fmt.Fprintf(&b, "commands: %d (command_plan_digest %s)\n", r.Counts.Commands, shortDigest(r.CommandPlanDigest.String()))
	fmt.Fprintf(&b, "dispatch: %s order=%s\n", dispatchLine, r.Plan.Dispatch.DispatchOrder)
	if c := r.Plan.Output.Collection; c != nil {
		suffix := ""
		if c.Suffix != "" {
			suffix = ", suffix " + c.Suffix
		}
		fmt.Fprintf(&b, "collection: %s (file mode %s%s)\n", c.Directory, c.FileMode, suffix)
	}
	fmt.Fprintf(&b, "targets: %d (client %d, daemon %d)\n", r.Counts.Targets, r.Counts.ByAuthority[string(executionplan.AddressByClient)], r.Counts.ByAuthority[string(executionplan.AddressByDaemon)])
	for i, t := range r.Targets {
		pt := r.Plan.Targets[i]
		fmt.Fprintf(&b, "- %s: %s\n", t.TargetID, t.Readiness)
		if t.Address.ResolutionActor == records.Deferred {
			fmt.Fprintf(&b, "  address: %s query=%s selected=<%s>\n", t.Address.Authority, pt.AddressPlan.QueryName, records.Deferred)
		} else {
			fmt.Fprintf(&b, "  address: %s %s (candidates %s)\n", t.Address.Authority, t.Address.Selected, joinAddrs(t.Address.ClientCandidates))
		}
		if t.CredentialBinding.Status == records.BindingBound {
			fmt.Fprintf(&b, "  credential: bound %s (policy=%s backend=%s user=%s; value not displayed)\n", t.CredentialBinding.CredentialID, t.CredentialBinding.Policy, t.CredentialBinding.Backend, t.CredentialBinding.DeviceUsername)
		} else {
			fmt.Fprintf(&b, "  credential: <%s>\n", t.CredentialBinding.Status)
		}
		// The platform is the plan's: the row's, the default, or --platform's,
		// so an override is seen before anything is sent.
		fmt.Fprintf(&b, "  intended: ping=%s transport=%s port=%d platform=%s\n", describeIntendedPing(t.IntendedPing), t.IntendedTransport.Transport, t.IntendedTransport.Port, pt.Device.Platform)
		// A crun's device runs its platform's list: shown
		// per device, since the lists differ across platforms.
		if list, ok := r.Plan.PlatformCommands[pt.Device.Platform]; ok {
			fmt.Fprintf(&b, "  commands: %s\n", strings.Join(list, "; "))
		}
		for _, f := range t.Findings {
			fmt.Fprintf(&b, "  finding: %s %s %s\n", f.Severity, f.Code, f.Message)
		}
	}
	fmt.Fprintf(&b, "next: %s (the same scope without --dry-run, or with --exercise)\n", r.NextOperation)
	_, err := io.WriteString(out, b.String())
	return err
}

func shortDigest(d string) string {
	if len(d) <= 16 {
		return d
	}
	return d[:8] + "…" + d[len(d)-7:]
}

func joinAddrs[T fmt.Stringer](addrs []T) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		parts = append(parts, a.String())
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// describeIntendedPing renders the intended_ping block for text reports:
// "disabled", or "enabled (2 probes,
// 500ms, capability=available via socket)".
func describeIntendedPing(p records.IntendedPing) string {
	if !p.Enabled {
		return "disabled"
	}
	capability := "capability=" + p.Capability
	if p.Method != "" {
		capability += " via " + p.Method
	}
	return fmt.Sprintf("enabled (%d probes, %s, %s)", p.Probes, time.Duration(p.TimeoutNS), capability)
}
