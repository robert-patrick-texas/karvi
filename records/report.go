package records

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// PlanReportSchemaVersion is the plan-inspection/exercise report counter:
// 2 removes the daemon row's execution_policy_digest.
const PlanReportSchemaVersion = 2

// SensitivePropertyPattern is executionplan.SensitivePropertyPattern, the
// one definition the audit screen and finding validation share.
var SensitivePropertyPattern = executionplan.SensitivePropertyPattern

// ReportKind and NextOperation discriminate the two reports.
type ReportKind string
type NextOperation string

const (
	ReportInspection ReportKind = "inspection"
	ReportExercise   ReportKind = "exercise"

	NextCommitLiveJob    NextOperation = "commit_live_job"
	NextSubmitNewLiveJob NextOperation = "submit_new_live_job"

	ProducerClient = "client"
	ProducerDaemon = "daemon"

	// Inspection readiness and outcomes.
	ReadinessPlanned  = "planned"
	ReadinessDeferred = "deferred"
	ReadinessInvalid  = "invalid"
	OutcomePlanned    = "planned"
	OutcomeInvalid    = "invalid"

	// Exercise readiness and outcomes.
	ReadinessReady            = "ready"
	ReadinessNotReady         = "not_ready"
	ReadinessResolutionFailed = "resolution_failed"
	OutcomeExercised          = "exercised"
	OutcomeNotReady           = "not_ready"

	// Field-level label of a deferred check.
	Deferred = "deferred_to_exercise_or_live_prepare"

	BindingBound      = "bound"
	BindingUnresolved = "unresolved"

	CheckAvailable   = "available"
	CheckUnavailable = "unavailable"
	CheckNotChecked  = "not_checked"
)

// PlanReport is the dry-run (inspection) or exercise report.
type PlanReport struct {
	SchemaVersion       int                                      `json:"schema_version"`
	Kind                ReportKind                               `json:"kind"`
	ReportID            string                                   `json:"report_id"`
	GeneratedAt         time.Time                                `json:"generated_at"`
	Producer            Producer                                 `json:"producer"`
	ProducerRole        string                                   `json:"producer_role"`
	Operator            Operator                                 `json:"operator"`
	ActivityID          string                                   `json:"activity_id"`
	JobID               string                                   `json:"job_id,omitempty"`
	Plan                executionplan.ExecutionPlan              `json:"plan"`
	PlanDigest          executionplan.Digest                     `json:"plan_digest"`
	CommandPlanDigest   executionplan.Digest                     `json:"command_plan_digest"`
	Daemons             []executionplan.DaemonReadiness          `json:"daemons"`
	Preparations        []executionplan.PreparationReport        `json:"preparations"`
	CredentialPackage   *credentialpackage.SafePackageProjection `json:"credential_package,omitempty"`
	Targets             []TargetReport                           `json:"targets"`
	Counts              ReportCounts                             `json:"counts"`
	Timing              ReportTiming                             `json:"timing"`
	Findings            []executionplan.Finding                  `json:"findings"`
	Outcome             string                                   `json:"outcome"`
	JobSubmitted        bool                                     `json:"job_submitted"`
	TargetDataSubmitted bool                                     `json:"target_data_submitted"`
	DeviceContacted     bool                                     `json:"device_contacted"`
	NextOperation       NextOperation                            `json:"next_operation"`
}

// TargetReport is one safe target projection with readiness and intents.
type TargetReport struct {
	TargetID          string                  `json:"target_id"`
	Readiness         string                  `json:"readiness"`
	Address           AddressReport           `json:"address"`
	CredentialBinding CredentialBindingReport `json:"credential_binding"`
	IntendedPing      IntendedPing            `json:"intended_ping"`
	IntendedTransport IntendedTransport       `json:"intended_transport"`
	Findings          []executionplan.Finding `json:"findings"`
}

// AddressReport duplicates the plan's address summary on purpose;
// Validate requires the two to agree.
type AddressReport struct {
	Authority        executionplan.AddressAuthority `json:"authority"`
	ResolutionActor  string                         `json:"resolution_actor"`
	ClientCandidates []netip.Addr                   `json:"client_candidates"`
	DaemonCandidates []netip.Addr                   `json:"daemon_candidates"`
	Selected         netip.Addr                     `json:"selected,omitzero"`
}

// CredentialBindingReport is the binding's safe provenance.
type CredentialBindingReport struct {
	Status         string             `json:"status"`
	CredentialID   string             `json:"credential_id,omitempty"`
	Policy         string             `json:"policy,omitempty"`
	Backend        string             `json:"backend,omitempty"`
	DeviceUsername string             `json:"device_username,omitempty"`
	MatchedOn      *credentials.Match `json:"matched_on,omitempty"`
	// Keys are the keys the credential offers, each with its fingerprint
	// as seen at planning.
	Keys []credentials.KeyRef `json:"keys,omitempty"`
}

type IntendedPing struct {
	Enabled    bool   `json:"enabled"`
	Probes     int    `json:"probes"`
	TimeoutNS  int64  `json:"timeout_ns"`
	Capability string `json:"capability"`
	// Method is the detected pinger, socket or system, when the gate is
	// enabled and the capability was checked.
	Method string `json:"method,omitempty"`
}

type IntendedTransport struct {
	Transport          string `json:"transport"`
	Port               uint16 `json:"port"`
	HostKeyPolicy      string `json:"host_key_policy,omitempty"`
	SessionInitProfile string `json:"session_init_profile"`
	Available          string `json:"available"`
}

type ReportCounts struct {
	Targets     int            `json:"targets"`
	ByReadiness map[string]int `json:"by_readiness"`
	ByAuthority map[string]int `json:"by_authority"`
	Commands    int            `json:"commands"`
	Errors      int            `json:"errors"`
	Warnings    int            `json:"warnings"`
}

// ReportTiming: null means the stage did not run.
type ReportTiming struct {
	ClientConfigNS     *int64 `json:"client_config_ns"`
	ClientInventoryNS  *int64 `json:"client_inventory_ns"`
	ClientDNSNS        *int64 `json:"client_dns_ns"`
	ClientCredentialNS *int64 `json:"client_credential_ns"`
	DaemonProbeNS      *int64 `json:"daemon_probe_ns"`
	DaemonPrepareNS    *int64 `json:"daemon_prepare_ns"`
	DaemonDNSNS        *int64 `json:"daemon_dns_ns"`
	PackageTransferNS  *int64 `json:"package_transfer_ns"`
	CommitNS           *int64 `json:"commit_ns"`
	DaemonValidationNS *int64 `json:"daemon_validation_ns"`
	TotalNS            int64  `json:"total_ns"`
}

func reportInvalid(field, format string, args ...any) error {
	return errorcodes.Errorf("plan_report_invalid", "%s: %s", field, fmt.Sprintf(format, args...))
}

var inspectionReadiness = map[string]bool{ReadinessPlanned: true, ReadinessDeferred: true, ReadinessInvalid: true}
var exerciseReadiness = map[string]bool{ReadinessReady: true, ReadinessNotReady: true, ReadinessResolutionFailed: true}
var checkValues = map[string]bool{CheckAvailable: true, CheckUnavailable: true, CheckNotChecked: true}

// Validate applies the shared and kind-specific report rules.
func (r *PlanReport) Validate() error {
	if r.SchemaVersion != PlanReportSchemaVersion {
		return reportInvalid("schema_version", "%d is not the supported version %d", r.SchemaVersion, PlanReportSchemaVersion)
	}
	var stage executionplan.Stage
	var readiness map[string]bool
	switch r.Kind {
	case ReportInspection:
		stage, readiness = executionplan.Draft, inspectionReadiness
	case ReportExercise:
		stage, readiness = executionplan.Committed, exerciseReadiness
	default:
		return reportInvalid("kind", "%q is not inspection or exercise", string(r.Kind))
	}
	if !executionplan.ValidID(r.ReportID) {
		return reportInvalid("report_id", "%q is not a valid identifier", r.ReportID)
	}
	if r.GeneratedAt.IsZero() {
		return reportInvalid("generated_at", "is required")
	}
	if r.Operator.Username == "" {
		return reportInvalid("operator.username", "is required")
	}
	if !executionplan.ValidJobID(r.ActivityID) {
		return reportInvalid("activity_id", "%q is not a job ID (YYMMDD-HHMMSS-xx)", r.ActivityID)
	}
	if err := r.Plan.Validate(stage); err != nil {
		return fmt.Errorf("plan_report_invalid: plan: %w", err)
	}
	sum, err := executionplan.SumPlan(r.Plan)
	if err != nil {
		return reportInvalid("plan", "cannot encode: %v", err)
	}
	if r.PlanDigest != sum {
		return reportInvalid("plan_digest", "recorded %s, plan hashes to %s", r.PlanDigest, sum)
	}
	if r.CommandPlanDigest != r.Plan.CommandPlanDigest {
		return reportInvalid("command_plan_digest", "differs from the plan's")
	}
	if r.Daemons == nil {
		return reportInvalid("daemons", "must be present (empty allowed)")
	}
	for i, d := range r.Daemons {
		if err := d.Validate(fmt.Sprintf("daemons[%d]", i)); err != nil {
			return err
		}
	}
	if r.Preparations == nil {
		return reportInvalid("preparations", "must be present (empty allowed)")
	}
	for i, p := range r.Preparations {
		if err := p.Validate(fmt.Sprintf("preparations[%d]", i)); err != nil {
			return err
		}
	}
	if len(r.Targets) != len(r.Plan.Targets) {
		return reportInvalid("targets", "%d reports for %d plan targets", len(r.Targets), len(r.Plan.Targets))
	}
	errors, warnings := 0, 0
	if r.Findings == nil {
		return reportInvalid("findings", "must be present (empty allowed)")
	}
	for i, f := range r.Findings {
		if err := f.Validate(fmt.Sprintf("findings[%d]", i)); err != nil {
			return err
		}
		errors, warnings = tally(f, errors, warnings)
	}
	byReadiness, byAuthority := map[string]int{}, map[string]int{}
	invalidTargets := 0
	for i, t := range r.Targets {
		field := fmt.Sprintf("targets[%d]", i)
		pt := r.Plan.Targets[i]
		if t.TargetID != pt.TargetID {
			return reportInvalid(field+".target_id", "%q is not plan target %d (%q); reports follow plan order", t.TargetID, i, pt.TargetID)
		}
		if !readiness[t.Readiness] {
			return reportInvalid(field+".readiness", "%q is not a %s readiness", t.Readiness, r.Kind)
		}
		if why := t.Address.disagreement(pt.AddressPlan); why != "" {
			return reportInvalid(field+".address", "%s", why)
		}
		switch t.CredentialBinding.Status {
		case BindingBound:
			if t.CredentialBinding.CredentialID == "" {
				return reportInvalid(field+".credential_binding.credential_id", "is required when bound")
			}
			if stage == executionplan.Committed && t.CredentialBinding.CredentialID != pt.CredentialBindingID {
				return reportInvalid(field+".credential_binding.credential_id", "differs from the plan target's binding")
			}
		case Deferred, BindingUnresolved:
			if t.CredentialBinding.CredentialID != "" || t.CredentialBinding.DeviceUsername != "" {
				return reportInvalid(field+".credential_binding", "carries provenance without a binding")
			}
		default:
			return reportInvalid(field+".credential_binding.status", "%q is not bound, deferred, or unresolved", t.CredentialBinding.Status)
		}
		if t.IntendedPing.Probes != executionplan.PingProbes || !checkValues[t.IntendedPing.Capability] {
			return reportInvalid(field+".intended_ping", "probes must be %d and capability a check value", executionplan.PingProbes)
		}
		if t.IntendedTransport.Transport != pt.Device.Transport || !checkValues[t.IntendedTransport.Available] {
			return reportInvalid(field+".intended_transport", "transport must match the plan and available be a check value")
		}
		// An inspection embeds the draft, which carries no selection yet: the
		// planner's choice or deferred stands there.
		if t.IntendedTransport.SessionInitProfile == "" {
			return reportInvalid(field+".intended_transport.session_init_profile", "is required")
		}
		if stage == executionplan.Committed && t.IntendedTransport.SessionInitProfile != pt.SessionInitProfile {
			return reportInvalid(field+".intended_transport.session_init_profile", "%q differs from the plan target's %q", t.IntendedTransport.SessionInitProfile, pt.SessionInitProfile)
		}
		if t.Findings == nil {
			return reportInvalid(field+".findings", "must be present (empty allowed)")
		}
		for j, f := range t.Findings {
			if err := f.Validate(fmt.Sprintf("%s.findings[%d]", field, j)); err != nil {
				return err
			}
			if f.TargetID != "" && f.TargetID != t.TargetID {
				return reportInvalid(fmt.Sprintf("%s.findings[%d].target_id", field, j), "names another target")
			}
			errors, warnings = tally(f, errors, warnings)
		}
		byReadiness[t.Readiness]++
		byAuthority[string(pt.AddressPlan.Authority)]++
		if t.Readiness == ReadinessInvalid || t.Readiness == ReadinessNotReady || t.Readiness == ReadinessResolutionFailed {
			invalidTargets++
		}
	}
	if r.Counts.Targets != len(r.Targets) || r.Counts.Commands != r.Plan.CommandCount() || r.Counts.Errors != errors || r.Counts.Warnings != warnings {
		return reportInvalid("counts", "targets %d, commands %d, errors %d, warnings %d do not match the report", r.Counts.Targets, r.Counts.Commands, r.Counts.Errors, r.Counts.Warnings)
	}
	if !equalCounts(r.Counts.ByReadiness, byReadiness) || !equalCounts(r.Counts.ByAuthority, byAuthority) {
		return reportInvalid("counts", "by_readiness or by_authority does not match the targets")
	}
	if r.Timing.TotalNS < 0 {
		return reportInvalid("timing.total_ns", "must not be negative")
	}
	notReady := errors > 0 || invalidTargets > 0
	switch r.Kind {
	case ReportInspection:
		if r.ProducerRole != ProducerClient {
			return reportInvalid("producer_role", "an inspection is produced by the client")
		}
		if r.JobID != "" {
			return reportInvalid("job_id", "an inspection has no job")
		}
		if len(r.Preparations) != 0 || r.CredentialPackage != nil {
			return reportInvalid("preparations", "an inspection sends nothing to a daemon")
		}
		if r.JobSubmitted || r.TargetDataSubmitted || r.DeviceContacted {
			return reportInvalid("job_submitted", "an inspection submits nothing and contacts nothing")
		}
		if r.NextOperation != NextCommitLiveJob {
			return reportInvalid("next_operation", "%q is not %q", string(r.NextOperation), NextCommitLiveJob)
		}
		if want := outcomeFor(notReady, OutcomeInvalid, OutcomePlanned); r.Outcome != want {
			return reportInvalid("outcome", "%q; the findings and readiness give %q", r.Outcome, want)
		}
	case ReportExercise:
		if r.ProducerRole != ProducerDaemon {
			return reportInvalid("producer_role", "an exercise report is produced by the daemon")
		}
		if !executionplan.ValidJobID(r.JobID) {
			return reportInvalid("job_id", "%q is not a job ID (YYMMDD-HHMMSS-xx)", r.JobID)
		}
		if r.CredentialPackage == nil {
			return reportInvalid("credential_package", "is required for an exercise")
		}
		if r.CredentialPackage.JobID != r.JobID || r.CredentialPackage.PlanDigest != r.Plan.PlanDigest {
			return reportInvalid("credential_package", "names another job or plan")
		}
		endpoints := map[string]bool{}
		for i, p := range r.Preparations {
			if endpoints[p.ExecutionEndpoint] {
				return reportInvalid(fmt.Sprintf("preparations[%d]", i), "endpoint %q appears twice", p.ExecutionEndpoint)
			}
			endpoints[p.ExecutionEndpoint] = true
		}
		for _, t := range r.Plan.Targets {
			if t.AddressPlan.Authority == executionplan.AddressByDaemon && !endpoints[t.ExecutionEndpoint] {
				return reportInvalid("preparations", "daemon-authority target %q has no preparation report", t.TargetID)
			}
		}
		if !r.JobSubmitted || !r.TargetDataSubmitted || r.DeviceContacted {
			return reportInvalid("job_submitted", "an exercise submits a job and target data and contacts no device")
		}
		if r.NextOperation != NextSubmitNewLiveJob {
			return reportInvalid("next_operation", "%q is not %q", string(r.NextOperation), NextSubmitNewLiveJob)
		}
		if want := outcomeFor(notReady, OutcomeNotReady, OutcomeExercised); r.Outcome != want {
			return reportInvalid("outcome", "%q; the findings and readiness give %q", r.Outcome, want)
		}
	}
	return nil
}

// disagreement says why the address report does not match the plan's
// address plan, or "" when it does; the caller attaches the code.
func (a AddressReport) disagreement(p executionplan.AddressPlan) string {
	if a.Authority != p.Authority {
		return fmt.Sprintf("authority %q differs from the plan's %q", a.Authority, p.Authority)
	}
	if a.Selected != p.Selected || !equalAddrs(a.ClientCandidates, p.ClientCandidates) || !equalAddrs(a.DaemonCandidates, p.DaemonCandidates) {
		return "addresses differ from the plan's"
	}
	switch {
	case a.ResolutionActor == Deferred:
		if p.Authority != executionplan.AddressByDaemon || p.Selected.IsValid() {
			return "only an unresolved daemon-authority target is deferred"
		}
	case a.ResolutionActor == executionplan.ResolverContextClient:
		if p.Authority != executionplan.AddressByClient {
			return "resolution_actor client under daemon authority"
		}
	case a.ResolutionActor == p.ResolverContext && p.ResolverContext != "":
	default:
		return fmt.Sprintf("resolution_actor %q does not match the plan", a.ResolutionActor)
	}
	return ""
}

func tally(f executionplan.Finding, errors, warnings int) (int, int) {
	switch f.Severity {
	case executionplan.SeverityError:
		errors++
	case executionplan.SeverityWarning:
		warnings++
	}
	return errors, warnings
}

func outcomeFor(notReady bool, bad, good string) string {
	if notReady {
		return bad
	}
	return good
}

func equalCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func equalAddrs(a, b []netip.Addr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
