package resolver

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/inventory"
)

func testConfig(t *testing.T, sets ...string) configload.Snapshot {
	t.Helper()
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: sets})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func mustAddrs(list ...string) []netip.Addr {
	out := []netip.Addr{}
	for _, s := range list {
		out = append(out, netip.MustParseAddr(s))
	}
	return out
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

// TestSelectOrdersDNSCandidatesAscending: three answers in shuffled order give
// candidates ascending, the first selected and the rest alternates.
func TestSelectOrdersDNSCandidatesAscending(t *testing.T) {
	cfg := testConfig(t, "name.address-family-preference=ipv4")
	calls := 0
	lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
		calls++
		if network != "ip4" || host != "r1" {
			t.Fatalf("lookup %s %s", network, host)
		}
		return mustAddrs("192.0.2.30", "192.0.2.10", "192.0.2.20", "192.0.2.10"), nil
	}
	sel, err := Select(context.Background(), cfg, inventory.Direct("r1", "generic", "system", 0), Capabilities{IPv4: true, IPv6: true}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	want := mustAddrs("192.0.2.10", "192.0.2.20", "192.0.2.30")
	if !equalAddrs(sel.Candidates, want) || sel.Selected != want[0] || !equalAddrs(sel.Alternates, want[1:]) || sel.Source != "dns" || sel.QueryName != "r1" || sel.Family != "ipv4" || calls != 1 {
		t.Fatalf("selection=%+v calls=%d", sel, calls)
	}
}

// TestSelectLiteralNeedsNoLookup: a literal, with the inventory alternates
// behind it in row order, never queries DNS and is recorded whatever its
// family (the client never contacts the device).
func TestSelectLiteralNeedsNoLookup(t *testing.T) {
	cfg := testConfig(t)
	d := inventory.Direct("r1", "generic", "system", 0)
	d.ManagementAddress = netip.MustParseAddr("2001:db8::1")
	d.Addresses = []inventory.Address{
		{Address: d.ManagementAddress, Role: inventory.RoleManagement},
		{Address: netip.MustParseAddr("10.0.0.9"), Role: inventory.RoleAlternate},
		{Address: netip.MustParseAddr("10.0.0.8"), Role: inventory.RoleAlternate},
	}
	lookup := func(context.Context, string, string) ([]netip.Addr, error) { t.Fatal("lookup called"); return nil, nil }
	sel, err := Select(context.Background(), cfg, d, Capabilities{IPv4: true}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Source != executionplan.SourceInventory || sel.Selected != d.ManagementAddress || !equalAddrs(sel.Alternates, mustAddrs("10.0.0.9", "10.0.0.8")) || !equalAddrs(sel.Candidates, mustAddrs("2001:db8::1", "10.0.0.9", "10.0.0.8")) || sel.QueryName != "" {
		t.Fatalf("selection=%+v", sel)
	}
}

func TestSelectReportsFamilyFallbackAndDNSFailure(t *testing.T) {
	cfg := testConfig(t, "name.address-family-preference=ipv6")
	lookup := func(_ context.Context, network, _ string) ([]netip.Addr, error) {
		if network != "ip4" {
			t.Fatalf("ipv6 must be skipped without sockets, got %s", network)
		}
		return mustAddrs("192.0.2.1"), nil
	}
	sel, err := Select(context.Background(), cfg, inventory.Direct("r1", "generic", "system", 0), Capabilities{IPv4: true}, lookup)
	if err != nil || len(sel.Notices) != 1 || !strings.Contains(sel.Notices[0], "ipv6 is unavailable") {
		t.Fatalf("sel=%+v err=%v", sel, err)
	}
	nx := func(_ context.Context, _, host string) ([]netip.Addr, error) {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	_, err = Select(context.Background(), cfg, inventory.Direct("ghost", "generic", "system", 0), Capabilities{IPv4: true, IPv6: true}, nx)
	if re, ok := err.(*Error); !ok || re.Code != "dns_nxdomain" {
		t.Fatalf("err=%v", err)
	}
}

// TestPrepareDaemonUsesHintsWithoutLookup: a daemon-authority target carrying
// client hints is prepared from the hints, source inventory, no lookup
// call.
func TestPrepareDaemonUsesHintsWithoutLookup(t *testing.T) {
	cfg := testConfig(t)
	target := plantest.DaemonTarget()
	target.AddressPlan.ClientCandidates = mustAddrs("10.0.0.1", "10.0.0.2")
	client := plantest.InventoryTarget()
	lookup := func(context.Context, string, string) ([]netip.Addr, error) { t.Fatal("lookup called"); return nil, nil }
	evidence, err := PrepareDaemonWith(context.Background(), cfg, []executionplan.ExecutionTarget{client, target}, Capabilities{IPv4: true, IPv6: true}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 {
		t.Fatalf("evidence for %d targets, want the daemon target only", len(evidence))
	}
	e := evidence[0]
	if e.TargetID != target.TargetID || e.Selected != netip.MustParseAddr("10.0.0.1") || !equalAddrs(e.Alternates, mustAddrs("10.0.0.2")) || len(e.DaemonCandidates) != 0 || e.SelectedSource != executionplan.SourceInventory || e.ResolverContext != executionplan.EndpointLocal {
		t.Fatalf("evidence=%+v", e)
	}
	// The digest is the one IncorporatePreparation recomputes.
	filled := target
	filled.AddressPlan.DaemonCandidates, filled.AddressPlan.Selected, filled.AddressPlan.Alternates = e.DaemonCandidates, e.Selected, e.Alternates
	filled.AddressPlan.SelectedSource, filled.AddressPlan.ResolverContext = e.SelectedSource, e.ResolverContext
	if sum, _ := executionplan.SumResolution(filled); sum != e.ResolutionDigest {
		t.Fatal("resolution digest does not cover the filled fields")
	}
}

// TestPrepareDaemonResolvesAndIncorporates: with no hint the daemon queries
// the plan's query name in the plan's family preference, reports dns-daemon,
// and the evidence incorporates into the draft and validates as committed
// after binding.
func TestPrepareDaemonResolvesAndIncorporates(t *testing.T) {
	cfg := testConfig(t, "name.address-family-preference=ipv4") // the plan's ipv6 preference must win
	draft := plantest.DraftPlan()
	calls := []string{}
	lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
		calls = append(calls, network+" "+host)
		return mustAddrs("2001:db8::11", "2001:db8::10"), nil
	}
	evidence, err := PrepareDaemonWith(context.Background(), cfg, draft.Targets, Capabilities{IPv4: true, IPv6: true}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "ip6 core-a.example.gov" {
		t.Fatalf("calls=%v", calls)
	}
	e := evidence[0]
	if e.SelectedSource != executionplan.SourceDNSDaemon || e.Selected != netip.MustParseAddr("2001:db8::10") || !equalAddrs(e.DaemonCandidates, mustAddrs("2001:db8::10", "2001:db8::11")) {
		t.Fatalf("evidence=%+v", e)
	}
	prepared, err := executionplan.IncorporatePreparation(draft, executionplan.PreparationEvidence{ExecutionEndpoint: executionplan.EndpointLocal, PreparationID: plantest.PreparationID, PreparationDigest: executionplan.Sum([]byte("p")), PreparedAt: plantest.PreparedAt, Addresses: evidence})
	if err != nil {
		t.Fatal(err)
	}
	for i := range prepared.Targets {
		prepared.Targets[i].CredentialBindingID = plantest.JobID
		prepared.Targets[i].SessionInitProfile = executionplan.SessionInitNone
	}
	final, err := executionplan.Finalize(prepared, plantest.PreparedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := final.Validate(executionplan.Committed); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareDaemonAbortsWithTheTargetList(t *testing.T) {
	cfg := testConfig(t)
	targets := []executionplan.ExecutionTarget{}
	for i := 0; i < 12; i++ {
		x := plantest.DaemonTarget()
		x.TargetID = "name:ghost-" + string(rune('a'+i))
		x.Device.ID = x.TargetID
		targets = append(targets, x)
	}
	ok := plantest.DaemonTarget()
	ok.AddressPlan.ClientCandidates = mustAddrs("10.0.0.1")
	targets = append(targets, ok)
	lookup := func(_ context.Context, _, host string) ([]netip.Addr, error) {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	_, err := PrepareDaemonWith(context.Background(), cfg, targets, Capabilities{IPv4: true, IPv6: true}, lookup)
	te, ok2 := err.(*TargetErrors)
	if !ok2 || len(te.Failures) != 12 || te.Total != 13 || te.ErrorCode() != "dns_nxdomain" {
		t.Fatalf("err=%v", err)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "dns_nxdomain: 12 of 13 targets failed address resolution: name:ghost-a (") || !strings.Contains(msg, "name:ghost-j (") || strings.Contains(msg, "name:ghost-k") || !strings.HasSuffix(msg, "and 2 more") {
		t.Fatalf("message=%q", msg)
	}
}
