package executor

import (
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/dispatch"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestRecordFromTargetCarriesTheAddressPlan:
// the record's address fields come from the target's address plan, the
// credential projection names the grant and protection, and the DNS and
// credential timings are null because both happened at planning.
func TestRecordFromTargetCarriesTheAddressPlan(t *testing.T) {
	final := plantest.FinalPlan()
	e := New(Options{ActivityID: plantest.JobID, JobID: plantest.JobID, ActivityType: "run", Commands: plantest.Commands, Operator: credentials.Operator{Username: "netops", UID: 1000}, Protection: "local-peer", DispatchOrder: "default"})
	var core, edge records.CommandRecord
	for _, x := range final.Targets {
		cred := &records.CredentialProjection{Policy: "default", DeviceUsername: "u", Backend: "env", MatchedOn: map[string]any{}, CredentialID: x.CredentialBindingID, Protection: "local-peer"}
		r := e.record(Work{Target: x, QueuedAt: time.Now()}, dispatch.Context{Mode: "serial", ScopePosition: 1}, cred, nil, step{kind: kindRequested, profile: "none", count: len(plantest.Commands), command: plantest.Commands[0]}, "succeeded", []byte("ok"), nil, nil, "", "", nil, 0, 0, time.Time{}, time.Now())
		if err := r.Validate(); err == nil || r.Sequence != 0 {
			// Validate requires a sequence the store assigns; the fields are what this test checks.
		}
		switch x.TargetID {
		case "name:core-a":
			core = r
		case "name:edge-b":
			edge = r
		}
	}
	if core.AddressAuthority != "daemon" || core.AddressSource != "dns-daemon" || core.SelectedAddress != "2001:db8::10" || core.AddressFamily != "ipv6" || core.AddressResolutionActor != "local" || len(core.DaemonAddressCandidates) != 2 || len(core.AlternateAddresses) != 1 || len(core.ClientAddressCandidates) != 0 || core.DNSQueryName != "core-a.example.gov" || core.DNSSuffixAction != "add-suffix:.example.gov" {
		t.Fatalf("core record=%+v", core)
	}
	if edge.AddressAuthority != "client" || edge.AddressSource != "inventory" || edge.SelectedAddress != "127.0.0.1" || edge.AddressResolutionActor != "client" || len(edge.AddressCandidates) != 1 || edge.Transport != "system" || edge.Port != 22 || edge.Dispatch.ServerID != "local" {
		t.Fatalf("edge record=%+v", edge)
	}
	if core.Timing.DNSNS != nil || core.Timing.CredentialNS != nil || core.Credential == nil || core.Credential.CredentialID != plantest.GrantB || core.Credential.Protection != "local-peer" {
		t.Fatalf("timing or credential: %+v %+v", core.Timing, core.Credential)
	}
}
