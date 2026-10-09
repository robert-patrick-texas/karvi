// Package plantest builds execution-plan fixtures for tests in other
// packages: three targets mirroring the k03 smoke inventory (a direct
// literal, an inventory device under client authority, one under daemon
// authority), a draft, evidence, a final plan, and headers.
package plantest

import (
	"net/netip"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// Fixed identifiers in the timestamp-prefixed format.
const (
	PlanID        = "20260914T120000.000000+0000-0123456789abcdefghjk"
	JobID         = "260914-120001-00"
	PreparationID = "20260914T120002.000000+0000-0123456789abcdefghjk"
	GrantA        = "20260914T120003.000000+0000-0123456789abcdefghjk"
	GrantB        = "20260914T120004.000000+0000-0123456789abcdefghjk"
)

// Commands is the smoke suite's four-command list.
var Commands = []string{"show clock", "show version", "show ip interface brief", "show running-config | include hostname"}

var (
	DraftedAt   = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	PreparedAt  = time.Date(2026, 9, 14, 12, 0, 5, 0, time.UTC)
	FinalizedAt = time.Date(2026, 9, 14, 12, 0, 9, 0, time.UTC)
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func digested(t executionplan.ExecutionTarget) executionplan.ExecutionTarget {
	d, err := executionplan.SumTarget(t)
	must(err)
	t.SourceDigest = d
	return t
}

// DirectTarget is 127.0.0.1 typed on the command line.
func DirectTarget() executionplan.ExecutionTarget {
	d := inventory.Direct("127.0.0.1", "generic", "system", 0)
	d.InputToken = "127.0.0.1"
	addr := netip.MustParseAddr("127.0.0.1")
	return digested(executionplan.ExecutionTarget{
		TargetID: d.ID, InputTarget: d.SuppliedName(), Device: executionplan.ProjectDevice(d),
		AddressPlan: executionplan.AddressPlan{
			Authority: executionplan.AddressByClient, FamilyPreference: executionplan.FamilyIPv4, TransformedName: "127.0.0.1",
			SuffixAction: executionplan.SuffixActionNone, ClientCandidates: []netip.Addr{addr}, DaemonCandidates: []netip.Addr{},
			Selected: addr, Alternates: []netip.Addr{}, SelectedSource: executionplan.SourceInventory, ResolverContext: executionplan.ResolverContextClient,
		},
		Channel: executionplan.ChannelShell, ExecutionEndpoint: executionplan.EndpointLocal,
	})
}

// InventoryTarget is edge-b from the smoke inventory under client authority.
func InventoryTarget() executionplan.ExecutionTarget {
	d := inventory.Device{Name: "edge-b", Platform: "generic", Transport: "system", Site: "branch", ManagementAddress: netip.MustParseAddr("127.0.0.1"), Source: inventory.SourceRef{Name: "smoke", Path: "/tmp/inv.csv", Line: 3, Digest: strings.Repeat("ab", 32)}}
	must(d.Validate())
	addr := netip.MustParseAddr("127.0.0.1")
	return digested(executionplan.ExecutionTarget{
		TargetID: d.ID, InputTarget: "edge-b", Device: executionplan.ProjectDevice(d),
		AddressPlan: executionplan.AddressPlan{
			Authority: executionplan.AddressByClient, FamilyPreference: executionplan.FamilyIPv4, TransformedName: "edge-b",
			SuffixAction: executionplan.SuffixActionNone, ClientCandidates: []netip.Addr{addr}, DaemonCandidates: []netip.Addr{},
			Selected: addr, Alternates: []netip.Addr{}, SelectedSource: executionplan.SourceInventory, ResolverContext: executionplan.ResolverContextClient,
		},
		Channel: executionplan.ChannelShell, ExecutionEndpoint: executionplan.EndpointLocal,
	})
}

// DaemonTarget is Core-A from the smoke inventory under daemon authority,
// as a draft: the daemon-filled fields are empty.
func DaemonTarget() executionplan.ExecutionTarget {
	d := inventory.Device{Name: "Core-A", CanonicalName: "core-a", Platform: "generic", Transport: "system", Site: "hq", Groups: []string{"core"}, Source: inventory.SourceRef{Name: "smoke", Path: "/tmp/inv.csv", Line: 2, Digest: strings.Repeat("ab", 32)}}
	must(d.Validate())
	d.InputToken = "Core-A"
	return digested(executionplan.ExecutionTarget{
		TargetID: d.ID, InputTarget: d.SuppliedName(), Device: executionplan.ProjectDevice(d),
		AddressPlan: executionplan.AddressPlan{
			Authority: executionplan.AddressByDaemon, FamilyPreference: executionplan.FamilyIPv6, TransformedName: "core-a",
			QueryName: "core-a.example.gov", SuffixAction: executionplan.SuffixActionPrefix + ".example.gov",
			ClientCandidates: []netip.Addr{}, DaemonCandidates: []netip.Addr{}, Alternates: []netip.Addr{},
		},
		Channel: executionplan.ChannelShell, ExecutionEndpoint: executionplan.EndpointLocal,
	})
}

// Configuration is the draft's configuration block: one value, so the
// pinned plan digests do not move with the registry's defaults, and its
// digest computed as the daemon computes it (ConfigDigest). A test whose
// job reads the configuration plans through planner.Draft instead.
func Configuration() executionplan.Configuration {
	return executionplan.Configuration{"ssh.host-key-policy": "accept-new"}
}

// ConfigDigest is Configuration's digest, the draft's
// sources.config_digest.
func ConfigDigest() string {
	snap, err := configload.FromValues(Configuration())
	if err != nil {
		panic(err)
	}
	return snap.Digest
}

// DraftPlan is the client's plan before preparation.
func DraftPlan() executionplan.ExecutionPlan {
	return executionplan.ExecutionPlan{
		SchemaVersion: executionplan.SchemaVersion, PlanID: PlanID,
		Operator: executionplan.Operator{Username: "netops", UID: 1000, PrimaryGID: 1000, Groups: []string{"netops"}},
		Targets:  []executionplan.ExecutionTarget{DirectTarget(), InventoryTarget(), DaemonTarget()},
		Commands: append([]string{}, Commands...), CommandPlanDigest: executionplan.SumCommands(Commands),
		BlindReturns: []int{}, Blind: []bool{}, Expectations: [][]executionplan.Expectation{}, TimeoutsNS: []int64{}, MaxBytes: []int64{},
		SessionInit: map[string]executionplan.SessionInitProfile{},
		Dispatch:    executionplan.DispatchSettings{Mode: executionplan.DispatchSerial, Width: 1, DispatchOrder: executionplan.OrderDefault},
		Output:      executionplan.OutputSettings{Format: executionplan.FormatText, Follow: true, Root: "/tmp/karvi/jobs"},
		Sources: executionplan.SourceDigests{ConfigDigest: ConfigDigest(),
			Selectors: inventory.Provenance{Sources: []inventory.SourceRef{{Name: "smoke", Path: "/tmp/inv.csv", Digest: strings.Repeat("ab", 32)}}}},
		Configuration: Configuration(),
		Planning:      executionplan.PlanningTimestamps{DraftedAt: DraftedAt},
		Preparation:   []executionplan.PreparationEvidence{},
	}
}

// Evidence is the daemon's answer for core-a: two IPv6 candidates.
func Evidence(plan executionplan.ExecutionPlan) executionplan.PreparationEvidence {
	var target executionplan.ExecutionTarget
	for _, t := range plan.Targets {
		if t.TargetID == "name:core-a" {
			target = t
		}
	}
	v6a, v6b := netip.MustParseAddr("2001:db8::10"), netip.MustParseAddr("2001:db8::11")
	target.AddressPlan.DaemonCandidates = []netip.Addr{v6a, v6b}
	target.AddressPlan.Selected = v6a
	target.AddressPlan.Alternates = []netip.Addr{v6b}
	target.AddressPlan.SelectedSource = executionplan.SourceDNSDaemon
	target.AddressPlan.ResolverContext = executionplan.EndpointLocal
	rd, err := executionplan.SumResolution(target)
	must(err)
	return executionplan.PreparationEvidence{
		ExecutionEndpoint: executionplan.EndpointLocal, PreparationID: PreparationID, PreparationDigest: executionplan.Sum([]byte("preparation")),
		PreparedAt: PreparedAt,
		Addresses:  []executionplan.AddressEvidence{{TargetID: "name:core-a", DaemonCandidates: []netip.Addr{v6a, v6b}, Selected: v6a, Alternates: []netip.Addr{v6b}, SelectedSource: executionplan.SourceDNSDaemon, ResolverContext: executionplan.EndpointLocal, ResolutionDigest: rd}},
	}
}

// SessionInitProfile is the profile the final plan selects for core-a.
const SessionInitProfile = "iosxe-init"

// Profile is SessionInitProfile's entry in the final plan's table.
func Profile() executionplan.SessionInitProfile {
	return executionplan.SessionInitProfile{Commands: []string{"terminal width 511", "show clock"}, OnError: executionplan.SessionInitFailDevice, CommandTimeoutNS: int64(30 * time.Second)}
}

// FinalPlan is the committed plan: evidence incorporated, the two
// client-authority targets bound to GrantA with no session-init profile,
// core-a bound to GrantB with SessionInitProfile.
func FinalPlan() executionplan.ExecutionPlan {
	draft := DraftPlan()
	prepared, err := executionplan.IncorporatePreparation(draft, Evidence(draft))
	must(err)
	for i := range prepared.Targets {
		if prepared.Targets[i].TargetID == "name:core-a" {
			prepared.Targets[i].CredentialBindingID = GrantB
			prepared.Targets[i].SessionInitProfile = SessionInitProfile
		} else {
			prepared.Targets[i].CredentialBindingID = GrantA
			prepared.Targets[i].SessionInitProfile = executionplan.SessionInitNone
		}
	}
	prepared.SessionInit = map[string]executionplan.SessionInitProfile{SessionInitProfile: Profile()}
	final, err := executionplan.Finalize(prepared, FinalizedAt)
	must(err)
	return final
}

// Header is the public job header for plan at stage; at Committed it
// carries a placeholder package reference the caller may replace.
func Header(plan executionplan.ExecutionPlan, stage executionplan.Stage) executionplan.PublicJobHeader {
	sum, err := executionplan.SumPlan(plan)
	must(err)
	h := executionplan.PublicJobHeader{
		SchemaVersion: executionplan.SchemaVersion, IdempotencyKey: "idem-1", JobID: JobID,
		Operator: plan.Operator, Client: executionplan.ClientIdentity{AppName: "karvi", Version: "0.9.2", Commit: "development", PID: 4242, Hostname: "ops01"},
		Mode: executionplan.ModeLive, ExecutionDomain: executionplan.ExecutionDomainLocal, CommandPlanDigest: plan.CommandPlanDigest, PlanDigest: sum,
	}
	if stage == executionplan.Committed {
		h.CredentialPackage = &executionplan.PackageReference{Protection: executionplan.ProtectionLocalPeer, Digest: executionplan.Sum([]byte("package"))}
	}
	return h
}
