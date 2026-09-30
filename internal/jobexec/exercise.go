package jobexec

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/dispatch"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/capacity"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/executor"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/internal/icmpgate"
	"github.com/robert-patrick-texas/karvi/internal/metrics"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/internal/transport/systemssh"
	"github.com/robert-patrick-texas/karvi/internal/transportselect"
	"github.com/robert-patrick-texas/karvi/records"
)

// ExerciseReportName is the exercise artifact in the job directory.
const ExerciseReportName = "exercise.json"

// exerciseState is what Run built before the branch: the accepted job's
// store, sinks, and managers, none of which has touched a device.
type exerciseState struct {
	id, jobID, artifact string
	started             time.Time
	store               *output.Store
	auditSink           *audit.Sink
	scoreboardWriter    *scoreboard.Writer
	initial             records.ScoreboardSnapshot
	capManager          *capacity.Manager
	sampler             *metrics.Sampler
}

// runExercise is the exercise branch of Run: every
// check is local and read-only, the executor is never built, and the job
// finalizes with exercise.json, the summary, the scoreboard, and the audit
// record. Grants are read only through ForTarget and SafeProjection.
//
// The context is read once, for the summary's cancellation block, and
// never observed: an exercise never ends cancelled. Its work is local and
// takes milliseconds, so a
// cancel_job the daemon accepted while it ran changes no outcome, no exit
// code, and nothing in exercise.json; the summary names the request as a
// live job's does when a cancel reached it too late to interrupt anything.
func runExercise(ctx context.Context, req Request, st exerciseState) ActivityResult {
	plan, cfg := req.Plan, req.Config
	validationStart := time.Now()
	result := ActivityResult{ActivityID: st.id, JobID: st.jobID, ArtifactDir: st.artifact}
	// The heartbeat for an exercise as for a run:
	// its snapshot is the initial one until the final write.
	stopHeartbeat := st.scoreboardWriter.Heartbeat(cfg.Duration("watch.refresh"), func() records.ScoreboardSnapshot { return st.initial })

	// Host-key inputs once per job, the transport's own call.
	hk := exerciseHostKey(cfg, req.Operator.Home)
	// The askpass helper once per job, located and never run.
	askpass, askpassErr := systemssh.FindAskpass("")
	// The ICMP gate's capability once per job when the gate is enabled:
	// detected, never probed. Unavailable is an error on every target, so
	// the exercise
	// predicts the live refusal at commit.
	var ping *icmpgate.Capability
	if plan.Ping.Enabled {
		c := icmpgate.Detect(icmpgate.OptionsFrom(cfg))
		ping = &c
	}

	targets := make([]records.TargetReport, 0, len(plan.Targets))
	caps := map[string]int{}
	byReadiness, byAuthority := map[string]int{}, map[string]int{}
	for _, t := range plan.Targets {
		tr := exerciseTarget(req, t, hk, askpass, askpassErr, ping)
		def, known := executor.Definition(cfg, t.Device.Platform)
		if !known {
			// The executor would refuse this target before any connection:
			// the daemon's configuration lacks the client's platform table.
			tr.Findings = append(tr.Findings, exerciseFinding("platform", executionplan.SeverityError, t.TargetID, errorcodes.Errorf("platform_unknown", "platform %q is not a known platform here", t.Device.Platform), map[string]string{"platform": t.Device.Platform}))
			tr.Readiness = records.ReadinessNotReady
		}
		targets = append(targets, tr)
		byReadiness[tr.Readiness]++
		byAuthority[string(t.AddressPlan.Authority)]++
		cap := def.SessionCap
		if t.Device.SessionCap != nil {
			cap = *t.Device.SessionCap
		}
		caps[t.Device.CanonicalName] = cap
	}
	findings := []executionplan.Finding{}
	// Dispatch arithmetic: the dispatcher's own plan, checked, not executed.
	tasks := make([]dispatch.Task, len(plan.Targets))
	for i, t := range plan.Targets {
		tasks[i] = dispatch.Task{Key: t.TargetID, Position: i + 1}
	}
	dp := buildPlan(cfg, "run", plan.Dispatch, tasks)
	if err := dp.Check(); err != nil {
		findings = append(findings, exerciseFinding("capacity", executionplan.SeverityError, "", err, nil))
	} else {
		eff := dp.Effective()
		findings = append(findings, exerciseFinding("capacity", executionplan.SeverityInfo, "", errorcodes.Errorf("dispatch_assessment", "dispatch %s width %d", eff.Mode, eff.Width), map[string]string{
			"mode": eff.Mode, "width": strconv.Itoa(eff.Width), "wave_start_width": strconv.Itoa(eff.WaveStartWidth), "wave_max_width": strconv.Itoa(eff.WaveMaxWidth), "absolute_max_width": strconv.Itoa(eff.AbsoluteMaxWidth),
		}))
	}
	// Capacity: the ledgers read under a shared lock, no lease taken.
	if a, err := st.capManager.Assess(caps); err != nil {
		findings = append(findings, exerciseFinding("capacity", executionplan.SeverityWarning, "", errorcodes.Ensure(err, "capacity_ledger_malformed"), nil))
	} else {
		details := map[string]string{"server_limit": strconv.Itoa(a.ServerLimit), "server_in_use": strconv.Itoa(a.ServerInUse), "devices": strconv.Itoa(len(a.Devices))}
		names := make([]string, 0, len(a.Devices))
		for name := range a.Devices {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			d := a.Devices[name]
			details["device."+name] = fmt.Sprintf("cap=%d in_use=%d", d.Cap, d.InUse)
		}
		severity := executionplan.SeverityInfo
		msg := fmt.Sprintf("server ledger %d of %d in use", a.ServerInUse, a.ServerLimit)
		if a.ServerInUse >= a.ServerLimit {
			severity = executionplan.SeverityWarning
			msg += "; a live run would wait for a slot"
		}
		findings = append(findings, exerciseFinding("capacity", severity, "", errorcodes.Errorf("capacity_assessment", "%s", msg), details))
	}

	// The report.
	errorsN, warningsN := 0, 0
	tally := func(fs []executionplan.Finding) {
		for _, f := range fs {
			switch f.Severity {
			case executionplan.SeverityError:
				errorsN++
			case executionplan.SeverityWarning:
				warningsN++
			}
		}
	}
	tally(findings)
	for _, t := range targets {
		tally(t.Findings)
	}
	outcome := records.OutcomeExercised
	if errorsN > 0 || byReadiness[records.ReadinessNotReady] > 0 {
		outcome = records.OutcomeNotReady
	}
	daemons := []executionplan.DaemonReadiness{}
	seen := map[string]bool{}
	for _, p := range req.Preparations {
		if !seen[p.ExecutionEndpoint] {
			seen[p.ExecutionEndpoint] = true
			daemons = append(daemons, p.Daemon)
		}
	}
	preparations := req.Preparations
	if preparations == nil {
		preparations = []executionplan.PreparationReport{}
	}
	pkg := req.Package
	timing := req.Timing
	validation := time.Since(validationStart).Nanoseconds()
	timing.DaemonValidationNS = &validation
	timing.TotalNS = 0
	for _, v := range []*int64{timing.DaemonPrepareNS, timing.DaemonDNSNS, timing.PackageTransferNS, timing.CommitNS, timing.DaemonValidationNS} {
		if v != nil {
			timing.TotalNS += *v
		}
	}
	reportID, err := osutil.NewID(time.Now())
	if err != nil {
		return FailedResult("activity_id_generation_failed", err)
	}
	report := records.PlanReport{
		SchemaVersion: records.PlanReportSchemaVersion, Kind: records.ReportExercise, ReportID: reportID, GeneratedAt: time.Now(),
		Producer: producerInfo(), ProducerRole: records.ProducerDaemon, Operator: osutil.RecordOperator(req.Operator), ActivityID: st.id, JobID: st.jobID,
		Plan: plan, PlanDigest: plan.PlanDigest, CommandPlanDigest: plan.CommandPlanDigest,
		Daemons: daemons, Preparations: preparations, CredentialPackage: &pkg,
		Targets: targets,
		Counts:  records.ReportCounts{Targets: len(targets), ByReadiness: byReadiness, ByAuthority: byAuthority, Commands: plan.CommandCount(), Errors: errorsN, Warnings: warningsN},
		Timing:  timing, Findings: findings, Outcome: outcome,
		JobSubmitted: true, TargetDataSubmitted: true, DeviceContacted: false, NextOperation: records.NextSubmitNewLiveJob,
	}
	if err := report.Validate(); err != nil {
		return FailedResult("plan_report_invalid", err)
	}
	reportPath := filepath.Join(st.artifact, ExerciseReportName)
	if err := osutil.AtomicJSON(reportPath, report, osutil.OutputFileMode); err != nil {
		return FailedResult("output_manifest_write_failed", err)
	}

	// Exit and error text: the first error
	// finding's registry exit; exercise_not_ready names the outcome.
	exit := exitcode.ExitSuccess
	var errText string
	if outcome == records.OutcomeNotReady {
		exit = exitcode.ExitDependencyError
		notReady := []string{}
		first := ""
		for _, t := range targets {
			for _, f := range t.Findings {
				if f.Severity != executionplan.SeverityError {
					continue
				}
				if first == "" {
					first = f.Code
				}
				if len(notReady) < 5 {
					notReady = append(notReady, fmt.Sprintf("%s: %s (%s)", t.TargetID, f.Message, f.Code))
				}
			}
		}
		for _, f := range findings {
			if f.Severity == executionplan.SeverityError {
				if first == "" {
					first = f.Code
				}
				if len(notReady) < 5 {
					notReady = append(notReady, fmt.Sprintf("%s (%s)", f.Message, f.Code))
				}
			}
		}
		if e, ok := errorcodes.Lookup(first); ok && e.Exit != 0 {
			exit = e.Exit
		}
		errText = errorcodes.Message(errorcodes.Errorf("exercise_not_ready", "%d of %d targets not ready; %s", byReadiness[records.ReadinessNotReady], len(targets), strings.Join(notReady, "; ")))
	}

	// Finalize: the empty record files, metrics, scoreboard, summary, audit.
	if err := st.store.Close(); err != nil && exit == exitcode.ExitSuccess {
		exit = exitcode.ExitOutputFailure
		errText = errorcodes.Message(errorcodes.Ensure(err, "output_store_close_failed"))
	}
	ended := time.Now()
	finalSnap := st.initial
	finalSnap.Status = "exercised"
	finalSnap.LastUpdatedAt, finalSnap.EndedAt, finalSnap.ElapsedNS = ended, &ended, ended.Sub(st.started).Nanoseconds()
	finalSnap.Counts = records.Counts{Total: len(plan.Targets), Completed: len(plan.Targets)}
	stopHeartbeat()
	if err := st.scoreboardWriter.Write(finalSnap); err != nil && exit == exitcode.ExitSuccess {
		exit = exitcode.ExitOutputFailure
		errText = errorcodes.Message(errorcodes.Ensure(err, "scoreboard_write_failed"))
	}
	metric := st.sampler.Final(map[string]any{"devices": len(plan.Targets), "commands": plan.CommandCount(), "dispatch": plan.Dispatch.Mode, "output_bytes": int64(0), "mode": "exercise"}, map[string]any{}, map[string]any{"bytes": int64(0)}, []string{"exercise: no device was contacted"})
	if err := st.store.WriteMetrics(metric); err != nil && exit == exitcode.ExitSuccess {
		exit = exitcode.ExitOutputFailure
		errText = errorcodes.Message(errorcodes.Ensure(err, "output_metrics_write_failed"))
	}
	summary := buildExerciseSummary(req, st, reportPath, st.started, ended, exit, outcome, byReadiness, cancellationOf(ctx), st.auditSink.Status(), metric.Bottleneck, ping)
	if err := st.store.WriteSummary(summary); err != nil {
		exit = exitcode.ExitOutputFailure
		errText = errorcodes.Message(errorcodes.Ensure(err, "output_summary_write_failed"))
		summary.ExitCode, summary.ExitName = exit, exitcode.ExitName(exit)
	}
	if err := writeActivityAudit(st.auditSink, req, st.id, st.jobID, "exercised", outcome, exit, "no_device_contact", map[string]any{"device_contacted": false, "ready": byReadiness[records.ReadinessReady], "not_ready": byReadiness[records.ReadinessNotReady], "report": reportPath}); err != nil && exit == exitcode.ExitSuccess {
		exit = exitcode.ExitOutputFailure
		errText = errorcodes.Message(errorcodes.Ensure(err, "audit_write_failed"))
		summary.ExitCode, summary.ExitName = exit, exitcode.ExitName(exit)
		_ = st.store.WriteSummary(summary)
	}
	result.ExitCode, result.ExitName, result.Summary, result.Error = exit, exitcode.ExitName(exit), summary, errText
	return result
}

// hostKeyState is the job's host-key policy resolution.
type hostKeyState struct {
	policy hostkey.Policy
	err    error
}

func exerciseHostKey(cfg interface {
	String(string) string
}, home string) hostKeyState {
	p, err := hostkey.Resolve(cfg.String("ssh.host-key-policy"), cfg.String("ssh.known-hosts-file"), home)
	return hostKeyState{policy: p, err: err}
}

// exerciseTarget runs the per-target checks.
func exerciseTarget(req Request, t executionplan.ExecutionTarget, hk hostKeyState, askpass string, askpassErr error, ping *icmpgate.Capability) records.TargetReport {
	cfg := req.Config
	tr := records.TargetReport{
		TargetID: t.TargetID, Findings: []executionplan.Finding{},
		Address:           records.AddressReport{Authority: t.AddressPlan.Authority, ResolutionActor: t.AddressPlan.ResolverContext, ClientCandidates: t.AddressPlan.ClientCandidates, DaemonCandidates: t.AddressPlan.DaemonCandidates, Selected: t.AddressPlan.Selected},
		CredentialBinding: records.CredentialBindingReport{Status: records.BindingUnresolved},
		IntendedPing:      records.IntendedPing{Enabled: req.Plan.Ping.Enabled, Probes: req.Plan.Ping.Probes, TimeoutNS: req.Plan.Ping.TimeoutNS, Capability: records.CheckNotChecked},
		IntendedTransport: records.IntendedTransport{Transport: t.Device.Transport, Port: exercisePort(t), SessionInitProfile: t.SessionInitProfile, Available: records.CheckNotChecked},
	}
	if tr.Address.ResolutionActor == "" {
		tr.Address.ResolutionActor = executionplan.ResolverContextClient
	}
	add := func(stage, severity string, err error, details map[string]string) {
		tr.Findings = append(tr.Findings, exerciseFinding(stage, severity, t.TargetID, err, details))
	}
	// The ICMP gate's capability, from the once-per-job
	// detection; nil when the gate is disabled.
	if ping != nil {
		if ping.Available {
			tr.IntendedPing.Capability, tr.IntendedPing.Method = records.CheckAvailable, ping.Method
			add("ping", executionplan.SeverityInfo, errorcodes.Errorf("icmp_assessment", "ICMP gate available via %s", ping.Method), map[string]string{"method": ping.Method})
		} else {
			tr.IntendedPing.Capability = records.CheckUnavailable
			add("ping", executionplan.SeverityError, errorcodes.Errorf("icmp_capability_unavailable", "the ICMP gate is enabled but %s", ping.Reason), nil)
		}
	}
	// The target's planning notices (a not-set or fallen-back platform) as
	// warning findings; readiness unchanged.
	tr.Findings = append(tr.Findings, NoticeFindings(t)...)
	// Credential binding: present in the package, provenance from the
	// projection only.
	if grant, ok := req.Grants.ForTarget(t.TargetID); !ok {
		add("credential_package", executionplan.SeverityError, errorcodes.Errorf("credential_resolution_failed", "target %s has no credential grant in the package", t.TargetID), nil)
	} else if proj, err := grant.SafeProjection(); err != nil {
		add("credential_package", executionplan.SeverityError, errorcodes.Ensure(err, "credential_material_error"), nil)
	} else {
		matched := proj.MatchedOn
		tr.CredentialBinding = records.CredentialBindingReport{Status: records.BindingBound, CredentialID: proj.CredentialID, Policy: proj.Policy, Backend: proj.Backend, DeviceUsername: proj.DeviceUsername, MatchedOn: &matched}
	}
	// Transport: the executor's own selection, plus the helper for the
	// system kind, neither opened nor run.
	sel, err := transportselect.Resolve(cfg, "run", t.Device.TransportSelector)
	switch {
	case err != nil:
		tr.IntendedTransport.Available = records.CheckUnavailable
		add("transport", executionplan.SeverityError, errorcodes.Ensure(err, "transport_unavailable"), nil)
	case sel.Kind == transportselect.KindTelnet && !cfg.Bool("security.allow-telnet"):
		tr.IntendedTransport.Available = records.CheckUnavailable
		add("transport", executionplan.SeverityError, errorcodes.Errorf("telnet_not_allowed", "device %s selects Telnet but security.allow-telnet is false", t.Device.CanonicalName), nil)
	case sel.Kind == transportselect.KindSystem && askpassErr != nil:
		tr.IntendedTransport.Available = records.CheckUnavailable
		add("transport", executionplan.SeverityError, errorcodes.Errorf("dependency_askpass_unavailable", "locate karvi-askpass: %w", askpassErr), map[string]string{"implementation": sel.Implementation, "binary": sel.Binary})
	default:
		tr.IntendedTransport.Available = records.CheckAvailable
		details := map[string]string{"kind": sel.Kind, "implementation": sel.Implementation}
		if sel.Binary != "" {
			details["binary"] = sel.Binary
		}
		if sel.Kind == transportselect.KindSystem {
			details["helper"] = askpass
		}
		add("transport", executionplan.SeverityInfo, errorcodes.Errorf("transport_assessment", "%s transport %s available", sel.Kind, sel.Implementation), details)
	}
	// Host key, SSH kinds only.
	if err == nil && sel.Kind != transportselect.KindTelnet {
		if hk.err != nil {
			add("host_key", executionplan.SeverityError, errorcodes.Ensure(hk.err, "host_key_trust_store_unavailable"), nil)
		} else {
			tr.IntendedTransport.HostKeyPolicy = string(hk.policy.Mode)
			enrolled := hostkey.HasEnrolledHost(hk.policy.KnownHostsFile, t.Device.CanonicalName, int(tr.IntendedTransport.Port))
			details := map[string]string{"enrolled": strconv.FormatBool(enrolled), "file": hk.policy.KnownHostsFile}
			if hk.policy.Mode == hostkey.Secure && !enrolled {
				add("host_key", executionplan.SeverityError, errorcodes.Errorf("host_key_not_enrolled", "secure policy: no host key is enrolled for %s [%s]:%d", t.Device.CanonicalName, t.AddressPlan.Selected, tr.IntendedTransport.Port), details)
			} else {
				add("host_key", executionplan.SeverityInfo, errorcodes.Errorf("host_key_enrollment", "host key enrolled=%t under %s", enrolled, hk.policy.Mode), details)
			}
		}
	}
	tr.Readiness = records.ReadinessReady
	for _, f := range tr.Findings {
		if f.Severity == executionplan.SeverityError {
			tr.Readiness = records.ReadinessNotReady
			break
		}
	}
	return tr
}

// NoticeFindings is one warning finding per planning notice on the target,
// stage platform, for the dry-run and exercise reports: the notice's code,
// message, and details, so the
// report shows why a device runs as the platform it does.
func NoticeFindings(t executionplan.ExecutionTarget) []executionplan.Finding {
	out := []executionplan.Finding{}
	for _, n := range t.Notices {
		details := map[string]string{}
		for k, v := range n.Details {
			details[k] = v
		}
		out = append(out, executionplan.Finding{Code: n.Code, Severity: executionplan.SeverityWarning, Stage: "platform", TargetID: t.TargetID, Message: n.Message, Details: details})
	}
	return out
}

// exerciseFinding builds a finding from a coded error so the registry's
// source scan sees the code emitted; the message is the bare text.
func exerciseFinding(stage, severity, targetID string, err error, details map[string]string) executionplan.Finding {
	message := err.Error()
	if inner := errors.Unwrap(err); inner != nil {
		message = inner.Error()
	}
	code := errorcodes.Of(err)
	if code == "" {
		code = "transport_unavailable"
	}
	return executionplan.Finding{Code: code, Severity: severity, Stage: stage, TargetID: targetID, Message: message, Details: details}
}

func exercisePort(t executionplan.ExecutionTarget) uint16 {
	if t.Device.Port != 0 {
		return t.Device.Port
	}
	if t.Device.Transport == transportselect.KindTelnet {
		return 23
	}
	return 22
}

// exercisePingSummary is the exercise summary's ping block:
// the settings, the detected method and capability, counters at zero; nil
// when the gate is disabled.
func exercisePingSummary(plan executionplan.ExecutionPlan, ping *icmpgate.Capability) *records.PingSummary {
	if ping == nil {
		return nil
	}
	capability := records.CheckUnavailable
	if ping.Available {
		capability = records.CheckAvailable
	}
	return &records.PingSummary{Enabled: true, Method: ping.Method, Capability: capability, Probes: plan.Ping.Probes, TimeoutNS: plan.Ping.TimeoutNS,
		Devices: map[string]int{records.PingDevicesGated: 0, records.PingDevicesProceeded: 0, records.PingDevicesDegraded: 0, records.PingDevicesSkipped: 0, records.PingDevicesCapabilityFailed: 0}}
}

// buildExerciseSummary is the exercise's summary. cancellation is the block
// of the request for a cancel_job accepted before this point,
// nil otherwise; it is a record of the request only, and the final status
// stays exercised. A cancel accepted after this point is in the
// run.cancel_requested audit record alone.
func buildExerciseSummary(req Request, st exerciseState, reportPath string, start, end time.Time, code int, outcome string, byReadiness map[string]int, cancellation *records.Cancellation, auditStatus audit.Status, bottleneck map[string]any, ping *icmpgate.Capability) records.Summary {
	causes := []string{}
	if outcome == records.OutcomeNotReady {
		causes = append(causes, "exercise_not_ready")
	}
	authorities := map[string]int{}
	for _, t := range req.Plan.Targets {
		authorities[string(t.AddressPlan.Authority)]++
	}
	files, out := summaryFiles(st.store.Paths(), 0)
	files["exercise"] = reportPath
	return records.Summary{SchemaVersion: records.JobSchemaVersion, JobID: st.jobID, ActivityID: st.id, StartedAt: start, EndedAt: end, DurationNS: end.Sub(start).Nanoseconds(), FinalStatus: "exercised", ExitCode: code, ExitName: exitcode.ExitName(code), TerminalCauses: causes, DispatchOrder: req.Plan.Dispatch.DispatchOrder, ShuffleKey: req.Plan.Dispatch.ShuffleKey,
		PlanID: req.Plan.PlanID, PlanDigest: req.Plan.PlanDigest.String(), Mode: string(executionplan.ModeExercise), AddressAuthorityCounts: authorities, Ping: exercisePingSummary(req.Plan, ping), Cancellation: cancellation,
		DeviceCounts: map[string]int{"total": len(req.Plan.Targets), "ready": byReadiness[records.ReadinessReady], "not_ready": byReadiness[records.ReadinessNotReady]}, RequestedCommandCounts: map[string]int{}, SessionInitCounts: map[string]int{}, Halt: map[string]any{"run_wide": "", "wave_gate": ""}, Output: out, AuditSinkStatus: map[string]any{"journald": auditStatus.Journald, "file": auditStatus.File, "warnings": auditStatus.Warnings}, Bottleneck: bottleneck, Recovery: map[string]any{"status": "not_required"}, Paths: files}
}
