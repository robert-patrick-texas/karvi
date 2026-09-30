package records

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
)

const (
	reportSchema = "../schema/plan-report.schema.json"
	reportID     = "20260914T120007.000000+0000-0123456789abcdefghjk"
	activityID   = plantest.JobID
	packageID    = "20260914T120005.000000+0000-0123456789abcdefghjk"
	audience     = "daemon:ops01:1000"
)

func i64(v int64) *int64 { return &v }

func producer() Producer {
	return Producer{Hostname: "ops01", BootID: "boot-1", PID: 4242, ProcessStartIdentity: "4242:1", AppVersion: "0.9.2"}
}

func readiness() executionplan.DaemonReadiness {
	return executionplan.DaemonReadiness{ExecutionEndpoint: executionplan.EndpointLocal, Audience: audience, Reachable: true, Status: executionplan.DaemonRunning, PID: 4100, Version: "0.9.2", IPCSchemaVersion: 5, Capabilities: []string{"prepare_job", "commit_job"}, Findings: []executionplan.Finding{}}
}

func targetReport(t executionplan.ExecutionTarget, readiness string, binding CredentialBindingReport, findings ...executionplan.Finding) TargetReport {
	actor := t.AddressPlan.ResolverContext
	if actor == "" {
		actor = Deferred
	}
	if findings == nil {
		findings = []executionplan.Finding{}
	}
	// A committed target carries its selection; in a draft a bound target
	// shows the planner's (none here) and a deferred one deferred.
	profile := t.SessionInitProfile
	if profile == "" {
		profile = executionplan.SessionInitNone
		if binding.Status == Deferred {
			profile = Deferred
		}
	}
	return TargetReport{
		TargetID: t.TargetID, Readiness: readiness,
		Address:           AddressReport{Authority: t.AddressPlan.Authority, ResolutionActor: actor, ClientCandidates: t.AddressPlan.ClientCandidates, DaemonCandidates: t.AddressPlan.DaemonCandidates, Selected: t.AddressPlan.Selected},
		CredentialBinding: binding,
		IntendedPing:      IntendedPing{Enabled: false, Probes: 2, TimeoutNS: int64(500 * time.Millisecond), Capability: CheckNotChecked},
		IntendedTransport: IntendedTransport{Transport: t.Device.Transport, Port: 22, HostKeyPolicy: "accept-new", SessionInitProfile: profile, Available: CheckAvailable},
		Findings:          findings,
	}
}

func bound(id string) CredentialBindingReport {
	return CredentialBindingReport{Status: BindingBound, CredentialID: id, Policy: "default", Backend: "env", DeviceUsername: "u", MatchedOn: &credentials.Match{Category: "operator", SafeValue: "netops"}}
}

func fixtureInspection() PlanReport {
	draft := plantest.DraftPlan()
	sum, _ := executionplan.SumPlan(draft)
	return PlanReport{
		SchemaVersion: PlanReportSchemaVersion, Kind: ReportInspection, ReportID: reportID, GeneratedAt: plantest.DraftedAt.Add(time.Second),
		Producer: producer(), ProducerRole: ProducerClient, Operator: Operator{Username: "netops", UID: 1000}, ActivityID: activityID,
		Plan: draft, PlanDigest: sum, CommandPlanDigest: draft.CommandPlanDigest,
		Daemons: []executionplan.DaemonReadiness{readiness()}, Preparations: []executionplan.PreparationReport{},
		Targets: []TargetReport{
			targetReport(draft.Targets[0], ReadinessPlanned, bound(plantest.GrantA)),
			targetReport(draft.Targets[1], ReadinessPlanned, bound(plantest.GrantA)),
			targetReport(draft.Targets[2], ReadinessDeferred, CredentialBindingReport{Status: Deferred}),
		},
		Counts:   ReportCounts{Targets: 3, ByReadiness: map[string]int{ReadinessPlanned: 2, ReadinessDeferred: 1}, ByAuthority: map[string]int{"client": 2, "daemon": 1}, Commands: 4},
		Timing:   ReportTiming{ClientConfigNS: i64(2_000_000), ClientInventoryNS: i64(1_500_000), ClientDNSNS: i64(0), ClientCredentialNS: i64(800_000), DaemonProbeNS: i64(3_000_000), TotalNS: 7_300_000},
		Findings: []executionplan.Finding{}, Outcome: OutcomePlanned, NextOperation: NextCommitLiveJob,
	}
}

func fixtureExercise() PlanReport {
	final := plantest.FinalPlan()
	draft := plantest.DraftPlan()
	draftSum, _ := executionplan.SumPlan(draft)
	prep, _ := executionplan.Seal(executionplan.PreparationReport{
		PreparationID: plantest.PreparationID, ExecutionEndpoint: executionplan.EndpointLocal, Audience: audience,
		PreparedAt: plantest.PreparedAt, ExpiresAt: plantest.PreparedAt.Add(10 * time.Minute), DraftDigest: draftSum, Accepted: true,
		Evidence: plantest.Evidence(draft), Daemon: readiness(), Findings: []executionplan.Finding{},
	})
	final.Preparation[0].PreparationDigest = prep.PreparationDigest
	final, _ = executionplan.Finalize(final, plantest.FinalizedAt)
	pkg := credentialpackage.SafePackageProjection{
		SchemaVersion: 1, PackageID: packageID, JobID: plantest.JobID, PlanDigest: final.PlanDigest,
		Issuer: credentialpackage.Principal{Kind: "operator", Username: "netops", UID: 1000, Hostname: "ops01"}, Audience: []string{audience},
		Protection: credentialpackage.ProtectionLocalPeer, IssuedAt: plantest.FinalizedAt, ExpiresAt: plantest.FinalizedAt.Add(10 * time.Minute),
		Grants: []credentialpackage.GrantProjection{
			{CredentialID: plantest.GrantA, Method: "embedded-secret", DeviceUsername: "u", Policy: "default", Backend: "env", MatchedOn: credentials.Match{Category: "operator", SafeValue: "netops"}, Scope: credentialpackage.CredentialScope{TargetIDs: []string{"name:127.0.0.1", "name:edge-b"}, Transports: []string{"system"}, Ports: []uint16{22}}, NotBefore: plantest.DraftedAt, NotAfter: plantest.DraftedAt.Add(12 * time.Hour)},
			{CredentialID: plantest.GrantB, Method: "embedded-secret", DeviceUsername: "admin", Policy: "core", Backend: "cloginrc", MatchedOn: credentials.Match{Category: "device", Pattern: "core-*"}, Scope: credentialpackage.CredentialScope{TargetIDs: []string{"name:core-a"}, Transports: []string{"system"}, Ports: []uint16{22}}, NotBefore: plantest.DraftedAt, NotAfter: plantest.DraftedAt.Add(12 * time.Hour)},
		},
		Bindings: []credentialpackage.TargetCredentialBinding{{TargetID: "name:127.0.0.1", CredentialID: plantest.GrantA}, {TargetID: "name:edge-b", CredentialID: plantest.GrantA}, {TargetID: "name:core-a", CredentialID: plantest.GrantB}},
	}
	pkg.PackageDigest, _ = pkg.Sum()
	notReady := executionplan.Finding{Code: "transport_unavailable", Severity: executionplan.SeverityError, Stage: "transport", TargetID: "name:edge-b", Message: "system ssh binary is not executable", Details: map[string]string{"binary": "/usr/bin/ssh"}}
	coreBinding := CredentialBindingReport{Status: BindingBound, CredentialID: plantest.GrantB, Policy: "core", Backend: "cloginrc", DeviceUsername: "admin", MatchedOn: &credentials.Match{Category: "device", Pattern: "core-*"}}
	targets := []TargetReport{
		targetReport(final.Targets[0], ReadinessReady, bound(plantest.GrantA)),
		targetReport(final.Targets[1], ReadinessNotReady, bound(plantest.GrantA), notReady),
		targetReport(final.Targets[2], ReadinessReady, coreBinding),
	}
	targets[1].IntendedTransport.Available = CheckUnavailable
	return PlanReport{
		SchemaVersion: PlanReportSchemaVersion, Kind: ReportExercise, ReportID: reportID, GeneratedAt: plantest.FinalizedAt.Add(2 * time.Second),
		Producer: producer(), ProducerRole: ProducerDaemon, Operator: Operator{Username: "netops", UID: 1000}, ActivityID: activityID, JobID: plantest.JobID,
		Plan: final, PlanDigest: final.PlanDigest, CommandPlanDigest: final.CommandPlanDigest,
		Daemons: []executionplan.DaemonReadiness{readiness()}, Preparations: []executionplan.PreparationReport{prep}, CredentialPackage: &pkg,
		Targets:  targets,
		Counts:   ReportCounts{Targets: 3, ByReadiness: map[string]int{ReadinessReady: 2, ReadinessNotReady: 1}, ByAuthority: map[string]int{"client": 2, "daemon": 1}, Commands: 4, Errors: 1},
		Timing:   ReportTiming{ClientConfigNS: i64(2_000_000), ClientInventoryNS: i64(1_500_000), ClientDNSNS: i64(0), ClientCredentialNS: i64(800_000), DaemonProbeNS: i64(3_000_000), DaemonPrepareNS: i64(40_000_000), DaemonDNSNS: i64(35_000_000), PackageTransferNS: i64(900_000), CommitNS: i64(12_000_000), DaemonValidationNS: i64(60_000_000), TotalNS: 155_200_000},
		Findings: []executionplan.Finding{}, Outcome: OutcomeNotReady,
		JobSubmitted: true, TargetDataSubmitted: true, DeviceContacted: false, NextOperation: NextSubmitNewLiveJob,
	}
}

func TestReportsValidateAndMatchTheSchema(t *testing.T) {
	for _, r := range []PlanReport{fixtureInspection(), fixtureExercise()} {
		if err := r.Validate(); err != nil {
			t.Fatalf("%s: %v", r.Kind, err)
		}
		canarytest.SchemaParity(t, reportSchema, r)
		data, _ := json.MarshalIndent(r, "", "  ")
		t.Logf("%s report (%d bytes):\n%s", r.Kind, len(data), truncate(data, 1600))
		var back PlanReport
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatal(err)
		}
		if err := back.Validate(); err != nil {
			t.Fatalf("%s after round trip: %v", r.Kind, err)
		}
		if r.Kind == ReportExercise {
			// The embedded final plan re-hashes to its recorded digest.
			if err := back.Plan.Verify(); err != nil {
				t.Fatal(err)
			}
		}
		s := string(data)
		for _, forbidden := range []string{`"password"`, `"q"`, `"en"`, `"succeeded"`} {
			if strings.Contains(s, forbidden) {
				t.Errorf("%s report contains %s", r.Kind, forbidden)
			}
		}
	}
}

func TestReportValidationVectors(t *testing.T) {
	cases := []struct {
		name  string
		fix   func() PlanReport
		edit  func(*PlanReport)
		field string
	}{
		{"schema", fixtureInspection, func(r *PlanReport) { r.SchemaVersion = 2 }, "schema_version"},
		{"kind", fixtureInspection, func(r *PlanReport) { r.Kind = "dry-run" }, "kind"},
		{"report id", fixtureInspection, func(r *PlanReport) { r.ReportID = "r1" }, "report_id"},
		{"plan digest", fixtureInspection, func(r *PlanReport) {
			r.Plan.Commands[0] = "show clock detail"
			r.Plan.CommandPlanDigest = executionplan.SumCommands(r.Plan.Commands)
		}, "plan_digest"},
		{"target order", fixtureInspection, func(r *PlanReport) { r.Targets[0], r.Targets[1] = r.Targets[1], r.Targets[0] }, "targets[0].target_id"},
		{"exercise readiness in inspection", fixtureInspection, func(r *PlanReport) { r.Targets[0].Readiness = ReadinessReady }, "targets[0].readiness"},
		{"address disagrees", fixtureInspection, func(r *PlanReport) { r.Targets[0].Address.Selected = netip.MustParseAddr("10.0.0.9") }, "targets[0].address"},
		{"deferred with provenance", fixtureInspection, func(r *PlanReport) { r.Targets[2].CredentialBinding.CredentialID = plantest.GrantB }, "targets[2].credential_binding"},
		{"probes", fixtureInspection, func(r *PlanReport) { r.Targets[0].IntendedPing.Probes = 1 }, "targets[0].intended_ping"},
		{"transport differs", fixtureInspection, func(r *PlanReport) { r.Targets[0].IntendedTransport.Transport = "native" }, "targets[0].intended_transport"},
		{"sensitive detail", fixtureInspection, func(r *PlanReport) {
			r.Findings = []executionplan.Finding{{Code: "x_y", Severity: "info", Stage: "client_config", Message: "m", Details: map[string]string{"token": "t"}}}
		}, "findings[0].details"},
		{"counts", fixtureInspection, func(r *PlanReport) { r.Counts.Commands = 3 }, "counts"},
		{"by_readiness", fixtureInspection, func(r *PlanReport) { r.Counts.ByReadiness[ReadinessPlanned] = 3 }, "counts"},
		{"inspection producer", fixtureInspection, func(r *PlanReport) { r.ProducerRole = ProducerDaemon }, "producer_role"},
		{"inspection job id", fixtureInspection, func(r *PlanReport) { r.JobID = plantest.JobID }, "job_id"},
		{"inspection submitted", fixtureInspection, func(r *PlanReport) { r.JobSubmitted = true }, "job_submitted"},
		{"inspection next", fixtureInspection, func(r *PlanReport) { r.NextOperation = NextSubmitNewLiveJob }, "next_operation"},
		{"inspection outcome", fixtureInspection, func(r *PlanReport) { r.Outcome = OutcomeInvalid }, "outcome"},
		{"exercise producer", fixtureExercise, func(r *PlanReport) { r.ProducerRole = ProducerClient }, "producer_role"},
		{"exercise no package", fixtureExercise, func(r *PlanReport) { r.CredentialPackage = nil }, "credential_package"},
		{"exercise package job", fixtureExercise, func(r *PlanReport) { r.CredentialPackage.JobID = plantest.PlanID }, "credential_package"},
		{"exercise no preparation", fixtureExercise, func(r *PlanReport) { r.Preparations = []executionplan.PreparationReport{} }, "preparations"},
		{"exercise contacted", fixtureExercise, func(r *PlanReport) { r.DeviceContacted = true }, "job_submitted"},
		{"exercise outcome", fixtureExercise, func(r *PlanReport) { r.Outcome = OutcomeExercised }, "outcome"},
		{"inspection profile missing", fixtureInspection, func(r *PlanReport) { r.Targets[2].IntendedTransport.SessionInitProfile = "" }, "targets[2].intended_transport.session_init_profile"},
		{"exercise profile missing", fixtureExercise, func(r *PlanReport) { r.Targets[0].IntendedTransport.SessionInitProfile = "" }, "targets[0].intended_transport.session_init_profile"},
		{"exercise profile differs from plan", fixtureExercise, func(r *PlanReport) { r.Targets[2].IntendedTransport.SessionInitProfile = executionplan.SessionInitNone }, "targets[2].intended_transport.session_init_profile"},
		{"exercise profile deferred", fixtureExercise, func(r *PlanReport) { r.Targets[0].IntendedTransport.SessionInitProfile = Deferred }, "targets[0].intended_transport.session_init_profile"},
		{"exercise binding differs from plan", fixtureExercise, func(r *PlanReport) { r.Targets[0].CredentialBinding.CredentialID = plantest.GrantB }, "targets[0].credential_binding.credential_id"},
	}
	for _, c := range cases {
		r := c.fix()
		c.edit(&r)
		err := r.Validate()
		if err == nil || !strings.HasPrefix(err.Error(), "plan_report_invalid: "+c.field) {
			t.Errorf("%s: want %s, got %v", c.name, c.field, err)
		}
	}
	// A draft plan inside an exercise report is refused through the plan.
	r := fixtureExercise()
	r.Plan = plantest.DraftPlan()
	if err := r.Validate(); err == nil || !strings.HasPrefix(err.Error(), "plan_report_invalid: plan:") {
		t.Errorf("draft plan in exercise: %v", err)
	}
}

func TestReportTypesAreStructurallyNonSecret(t *testing.T) {
	allow := canarytest.Allow{Leaves: []string{"net/netip.Addr", "time.Time", "github.com/robert-patrick-texas/karvi/executionplan.Digest", "github.com/robert-patrick-texas/karvi/credentials.Match"}}
	for _, rt := range []reflect.Type{reflect.TypeOf(PlanReport{}), reflect.TypeOf(TargetReport{})} {
		canarytest.Walk(t, rt, allow)
	}
	// records must reference only the safe projection of the package.
	for i := 0; i < reflect.TypeOf(PlanReport{}).NumField(); i++ {
		f := reflect.TypeOf(PlanReport{}).Field(i)
		for _, forbidden := range []string{"CredentialGrant", "CredentialPackage", "Envelope"} {
			if strings.HasSuffix(f.Type.String(), "."+forbidden) {
				t.Errorf("PlanReport.%s has type %s", f.Name, f.Type)
			}
		}
	}
}

func TestAuditScreenSharesThePattern(t *testing.T) {
	if !SensitivePropertyPattern.MatchString("device_password") || SensitivePropertyPattern.MatchString("command_count") {
		t.Fatal("pattern")
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "\n  ..."
}

// TestTerminalStatus: the six final statuses are terminal, exercised
// among them, and no running state is.
func TestTerminalStatus(t *testing.T) {
	if len(FinalStatuses) != 6 {
		t.Fatalf("FinalStatuses = %v", FinalStatuses)
	}
	for _, s := range []string{"completed", "halted", "errored", "cancelled", "incomplete", "exercised"} {
		if !TerminalStatus(s) {
			t.Fatalf("%s must be terminal", s)
		}
	}
	for _, s := range []string{"initializing", "queued", "running", "draining", "cancelling", "not_ready", "", "Completed"} {
		if TerminalStatus(s) {
			t.Fatalf("%s must not be terminal", s)
		}
	}
}
