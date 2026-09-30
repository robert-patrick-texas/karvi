package records

import (
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

const (
	recordSchema  = "../schema/command-record.schema.json"
	summarySchema = "../schema/job-summary.schema.json"
)

func ns(d time.Duration) *int64 { v := d.Nanoseconds(); return &v }

// fixtureRecord is a command record as the executor writes one, with the
// ping object of a gated device that answered once.
func fixtureRecord() CommandRecord {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	zero := int64(0)
	return CommandRecord{
		SchemaVersion: CommandSchemaVersion, RecordID: "20260914T120000.000000+0000-0123456789abcdefghjk", JobID: "job", ActivityID: "job", ActivityType: "run", Sequence: 1,
		Operator: Operator{Username: "netops", UID: 1000}, Device: DeviceProjection{ID: "name:core-a", Name: "core-a", CanonicalName: "core-a", Groups: []string{}},
		InputTarget: "core-a", TransformedName: "core-a", DNSSuffixAction: "none", AddressCandidates: []string{"10.0.0.1"}, SelectedAddress: "10.0.0.1", AddressFamily: "ipv4", AddressSource: "inventory",
		AddressAuthority: "client", ClientAddressCandidates: []string{"10.0.0.1"}, AddressResolutionActor: "client",
		Platform: "cisco_iosxe", Transport: "system", Port: 22, ConnectionReused: nil,
		Credential:    &CredentialProjection{Policy: "default", DeviceUsername: "u", Backend: "env", MatchedOn: map[string]any{}, CredentialID: "c", Protection: "local-peer"},
		Ping:          fixturePing(),
		Dispatch:      DispatchContext{Mode: "serial", ServerID: "local", WorkerID: "w1", ScopePosition: 1, DesiredWidth: 1, EffectiveInflight: 1},
		DispatchOrder: "default", CommandIndex: 1, CommandCount: 1, CommandKind: "requested", Command: "show clock", CommandSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Status: "succeeded", Output: "", OutputEncoding: "utf-8", OutputBytes: 0, OutputSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Prompt: "", PromptSource: "", PromptObserved: nil, Notices: []Notice{{Code: "icmp_packet_loss", Message: "ICMP packet loss 50%; proceeding because one validated reply was received", Details: map[string]any{"replies": 1}}},
		Timing: Timing{QueuedAt: now, DeviceStartedAt: &now, CommandStartedAt: &now, EndedAt: now, TotalNS: 0, SchedulerWaitNS: &zero, ServerCapacityWaitNS: &zero, PingNS: ns(502 * time.Millisecond), ConnectNS: &zero, DeviceResponseNS: &zero},
		Error:  nil, RetryEligible: false,
	}
}

func fixturePing() *PingReport {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	return &PingReport{
		Address: "10.0.0.1", Family: "ipv4", Method: "socket", ExecutionEndpoint: "local", Probes: PingProbes, TimeoutNS: int64(500 * time.Millisecond),
		Outcomes: []PingOutcome{
			{Sequence: 1, SentAt: now, Status: PingReply, RTTNS: ns(2100 * time.Microsecond), From: "10.0.0.1"},
			{Sequence: 2, SentAt: now.Add(2100 * time.Microsecond), Status: PingTimeout},
		},
		Replies: 1, Losses: 1, Decision: PingDecisionProceedDegraded, TotalNS: int64(502 * time.Millisecond),
	}
}

// TestRecordWithPingValidatesAndMatchesTheSchema:
// the record with a ping object validates, names only keys the schema
// declares, and carries every required key; the two ICMP statuses are
// terminal; a disabled gate is an explicit null.
func TestRecordWithPingValidatesAndMatchesTheSchema(t *testing.T) {
	r := fixtureRecord()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParity(t, recordSchema, r)
	for _, status := range []string{StatusICMPUnreachable, StatusICMPCapabilityUnavailable} {
		r := fixtureRecord()
		r.Status = status
		r.Ping.Outcomes[0] = PingOutcome{Sequence: 1, SentAt: r.Ping.Outcomes[0].SentAt, Status: PingTimeout}
		r.Ping.Replies, r.Ping.Losses, r.Ping.Decision = 0, 2, PingDecisionSkip
		r.Error = &StructuredError{Code: status, Category: "connection", Message: "skipped", Operation: "icmp_gate"}
		if err := r.Validate(); err != nil {
			t.Errorf("%s: %v", status, err)
		}
		canarytest.SchemaParity(t, recordSchema, r)
	}
	r = fixtureRecord()
	r.Ping, r.Timing.PingNS, r.Notices = nil, nil, []Notice{}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParity(t, recordSchema, r)
}

func TestPingReportValidationVectors(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*PingReport)
	}{
		{"one outcome", func(p *PingReport) { p.Outcomes = p.Outcomes[:1] }},
		{"three probes", func(p *PingReport) { p.Probes = 3 }},
		{"out of order", func(p *PingReport) { p.Outcomes[0].Sequence = 2 }},
		{"unknown status", func(p *PingReport) { p.Outcomes[1].Status = "late" }},
		{"timeout with rtt", func(p *PingReport) { p.Outcomes[1].RTTNS = ns(time.Millisecond) }},
		{"reply without rtt", func(p *PingReport) { p.Outcomes[0].RTTNS = nil }},
		{"replies miscounted", func(p *PingReport) { p.Replies = 2 }},
		{"losses miscounted", func(p *PingReport) { p.Losses = 0 }},
		{"unknown decision", func(p *PingReport) { p.Decision = "retry" }},
		{"no timeout", func(p *PingReport) { p.TimeoutNS = 0 }},
		{"no method", func(p *PingReport) { p.Method = "" }},
	} {
		r := fixtureRecord()
		tc.edit(r.Ping)
		if got := errorcodes.Of(r.Validate()); got != "record_ping_invalid" {
			t.Errorf("%s: code %q, want record_ping_invalid", tc.name, got)
		}
	}
}

// TestSummaryPingBlockMatchesTheSchema: a live summary with
// the block, an exercise summary with capability and zero counters, and a
// disabled gate as null all name only declared keys and every required one.
func TestSummaryPingBlockMatchesTheSchema(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	base := func() Summary {
		return Summary{
			SchemaVersion: JobSchemaVersion, JobID: "job", ActivityID: "job", StartedAt: now, EndedAt: now, DurationNS: 0, FinalStatus: "completed", ExitCode: 0, ExitName: "success",
			TerminalCauses: []string{}, DispatchOrder: "default", PlanID: "plan", PlanDigest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Mode: "live",
			AddressAuthorityCounts: map[string]int{"client": 2}, DeviceCounts: map[string]int{"total": 2}, RequestedCommandCounts: map[string]int{}, SessionInitCounts: map[string]int{},
			Halt: nil, Output: map[string]any{}, AuditSinkStatus: map[string]any{}, Bottleneck: map[string]any{}, Recovery: map[string]any{}, Paths: map[string]string{},
		}
	}
	live := base()
	live.Ping = &PingSummary{Enabled: true, Method: "socket", Probes: PingProbes, TimeoutNS: int64(500 * time.Millisecond), Devices: map[string]int{PingDevicesGated: 2, PingDevicesProceeded: 1, PingDevicesDegraded: 0, PingDevicesSkipped: 1, PingDevicesCapabilityFailed: 0}, ProbesSent: 4, Replies: 2, Timeouts: 2, Errors: 0, TotalNS: int64(1002 * time.Millisecond)}
	canarytest.SchemaParity(t, summarySchema, live)
	exercise := base()
	exercise.Mode, exercise.FinalStatus = "exercise", "exercised"
	exercise.Ping = &PingSummary{Enabled: true, Method: "system", Capability: "available", Probes: PingProbes, TimeoutNS: int64(500 * time.Millisecond), Devices: map[string]int{PingDevicesGated: 0, PingDevicesProceeded: 0, PingDevicesDegraded: 0, PingDevicesSkipped: 0, PingDevicesCapabilityFailed: 0}}
	canarytest.SchemaParity(t, summarySchema, exercise)
	disabled := base()
	canarytest.SchemaParity(t, summarySchema, disabled)
}
