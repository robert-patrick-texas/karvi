package planner

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
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

var dual = resolver.Capabilities{IPv4: true, IPv6: true}

// k03 mirrors the smoke inventory: Core-A and edge-b, both 127.0.0.1.
func k03(t *testing.T) []inventory.Device {
	t.Helper()
	out := []inventory.Device{}
	for i, row := range []struct{ name, site string }{{"Core-A", "hq"}, {"edge-b", "branch"}} {
		d := inventory.Device{Name: row.name, Platform: "generic", Transport: "system", Site: row.site, ManagementAddress: netip.MustParseAddr("127.0.0.1"), Source: inventory.SourceRef{Name: "smoke", Path: "/tmp/inv.csv", Line: i + 2, Digest: strings.Repeat("ab", 32)}}
		if err := d.Validate(); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func mustAddrs(list ...string) []netip.Addr {
	out := []netip.Addr{}
	for _, s := range list {
		out = append(out, netip.MustParseAddr(s))
	}
	return out
}

// TestEffectiveAuthorityPrecedence covers every override point of the
// address authority: command line, device field (which the loader fills from the
// source default), configuration default, built-in client.
func TestEffectiveAuthorityPrecedence(t *testing.T) {
	for _, tc := range []struct{ name, cfgDefault, device, override, want string }{
		{"built-in", "", "", "", "client"},
		{"configuration default", "daemon", "", "", "daemon"},
		{"device field beats configuration", "daemon", "client", "", "client"},
		{"device field alone", "", "daemon", "", "daemon"},
		{"command line beats device", "client", "daemon", "client", "client"},
		{"command line beats configuration", "daemon", "", "client", "client"},
		{"command line daemon", "", "", "daemon", "daemon"},
	} {
		sets := []string{}
		if tc.cfgDefault != "" {
			sets = append(sets, "name.default-address-authority="+tc.cfgDefault)
		}
		d := inventory.Direct("r1", "generic", "system", 0)
		d.AddressAuthority = tc.device
		if got := EffectiveAuthority(testConfig(t, sets...), d, tc.override); string(got) != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestGateRefusesDaemonAuthorityUnlessAllowed(t *testing.T) {
	devices := k03(t)
	devices[0].AddressAuthority = "daemon"
	devices[1].AddressAuthority = "daemon"
	err := GateDaemonResolution(testConfig(t), devices, nil)
	if errorcodes.Of(err) != "daemon_resolution_not_allowed" || !strings.Contains(err.Error(), "2 target(s)") || !strings.Contains(err.Error(), "first Core-A") {
		t.Fatalf("err=%v", err)
	}
	if err := GateDaemonResolution(testConfig(t, "name.allow-daemon-resolution=true"), devices, nil); err != nil {
		t.Fatal(err)
	}
	// The gate sees the command line, in both directions.
	if err := GateDaemonResolution(testConfig(t), k03(t), map[string]string{"name:edge-b": "daemon"}); errorcodes.Of(err) != "daemon_resolution_not_allowed" {
		t.Fatalf("override not gated: %v", err)
	}
	if err := GateDaemonResolution(testConfig(t), devices, map[string]string{"name:core-a": "client", "name:edge-b": "client"}); err != nil {
		t.Fatalf("client override still gated: %v", err)
	}
	if _, err := PlanAddresses(context.Background(), testConfig(t), devices, AddressOptions{Capabilities: &dual}); errorcodes.Of(err) != "daemon_resolution_not_allowed" {
		t.Fatalf("planning did not apply the gate: %v", err)
	}
}

// TestSplitLookupKeepsDaemonTargetsOffTheClient: a client-authority target
// resolves with the client's answer while the daemon-authority target's
// client fields hold no address; the daemon then resolves it with its own
// answer at prepare.
func TestSplitLookupKeepsDaemonTargetsOffTheClient(t *testing.T) {
	cfg := testConfig(t, "name.allow-daemon-resolution=true", "name.address-family-preference=ipv4")
	client := inventory.Direct("client.example", "generic", "system", 0)
	daemon := inventory.Direct("daemon.example", "generic", "system", 0)
	daemon.AddressAuthority = "daemon"
	clientCalls := []string{}
	clientLookup := func(_ context.Context, _, host string) ([]netip.Addr, error) {
		clientCalls = append(clientCalls, host)
		return mustAddrs("192.0.2.1"), nil
	}
	plans, err := PlanAddresses(context.Background(), cfg, []inventory.Device{client, daemon}, AddressOptions{Capabilities: &dual, Lookup: clientLookup})
	if err != nil {
		t.Fatal(err)
	}
	if len(clientCalls) != 1 || clientCalls[0] != "client.example" {
		t.Fatalf("client looked up %v", clientCalls)
	}
	c, d := plans[0], plans[1]
	if c.Authority != executionplan.AddressByClient || c.Selected != netip.MustParseAddr("192.0.2.1") || c.SelectedSource != executionplan.SourceDNSClient || c.ResolverContext != executionplan.ResolverContextClient || c.QueryName != "client.example" || c.FamilyPreference != executionplan.FamilyIPv4 {
		t.Fatalf("client plan=%+v", c)
	}
	if d.Authority != executionplan.AddressByDaemon || d.Selected.IsValid() || len(d.ClientCandidates) != 0 || len(d.DaemonCandidates) != 0 || d.SelectedSource != "" || d.ResolverContext != "" || d.QueryName != "daemon.example" {
		t.Fatalf("daemon plan holds client-side facts: %+v", d)
	}
	targets := []executionplan.ExecutionTarget{}
	for i, dev := range []inventory.Device{client, daemon} {
		targets = append(targets, target(t, dev, plans[i]))
	}
	for _, x := range targets {
		if err := x.Validate(executionplan.Draft); err != nil {
			t.Fatal(err)
		}
	}
	daemonLookup := func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host != "daemon.example" {
			t.Fatalf("daemon looked up %s", host)
		}
		return mustAddrs("198.51.100.7"), nil
	}
	evidence, err := resolver.PrepareDaemonWith(context.Background(), cfg, targets, dual, daemonLookup)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].TargetID != "name:daemon.example" || evidence[0].Selected != netip.MustParseAddr("198.51.100.7") || evidence[0].SelectedSource != executionplan.SourceDNSDaemon {
		t.Fatalf("evidence=%+v", evidence)
	}
}

// TestDaemonAuthorityLiteralIsAHint: a literal combined with daemon authority
// is valid; the client copies it as a hint and runs no lookup.
func TestDaemonAuthorityLiteralIsAHint(t *testing.T) {
	cfg := testConfig(t, "name.allow-daemon-resolution=true")
	devices := k03(t)
	devices[0].Addresses = append(devices[0].Addresses, inventory.Address{Address: netip.MustParseAddr("10.0.0.2"), Role: inventory.RoleAlternate})
	lookup := func(context.Context, string, string) ([]netip.Addr, error) { t.Fatal("lookup called"); return nil, nil }
	plans, err := PlanAddresses(context.Background(), cfg, devices, AddressOptions{Capabilities: &dual, Lookup: lookup, Overrides: map[string]string{"name:core-a": "daemon"}})
	if err != nil {
		t.Fatal(err)
	}
	if p := plans[0]; p.Authority != executionplan.AddressByDaemon || len(p.ClientCandidates) != 2 || p.ClientCandidates[0] != netip.MustParseAddr("127.0.0.1") || p.ClientCandidates[1] != netip.MustParseAddr("10.0.0.2") || p.Selected.IsValid() {
		t.Fatalf("hint plan=%+v", p)
	}
	if p := plans[1]; p.Authority != executionplan.AddressByClient || p.Selected != netip.MustParseAddr("127.0.0.1") || p.SelectedSource != executionplan.SourceInventory {
		t.Fatalf("client plan=%+v", p)
	}
}

// TestOneFailingLookupAbortsPlanning: one failing target among three aborts
// with its code, lists it, and produces no plan.
func TestOneFailingLookupAbortsPlanning(t *testing.T) {
	cfg := testConfig(t)
	devices := []inventory.Device{inventory.Direct("a.example", "generic", "system", 0), inventory.Direct("ghost.example", "generic", "system", 0), inventory.Direct("c.example", "generic", "system", 0)}
	lookup := func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host == "ghost.example" {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		return mustAddrs("192.0.2.1"), nil
	}
	plans, err := PlanAddresses(context.Background(), cfg, devices, AddressOptions{Capabilities: &dual, Lookup: lookup})
	if plans != nil || errorcodes.Of(err) != "dns_nxdomain" || !strings.Contains(err.Error(), "1 of 3 targets") || !strings.Contains(err.Error(), "name:ghost.example") {
		t.Fatalf("plans=%v err=%v", plans, err)
	}
	if errorcodes.ExitFor(err, 1) != errorcodes.ExitAt(err, "dns_nxdomain") {
		t.Fatalf("exit does not follow the code: %v", err)
	}
	// The family-fallback notice is a planning warning, not a failure.
	warnings := []string{}
	_, err = PlanAddresses(context.Background(), testConfig(t, "name.address-family-preference=ipv6"), devices[:1], AddressOptions{Capabilities: &resolver.Capabilities{IPv4: true}, Lookup: lookup, Warn: func(s string) { warnings = append(warnings, s) }})
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "resolution_notice: a.example: preferred address family ipv6 is unavailable") {
		t.Fatalf("warnings=%v err=%v", warnings, err)
	}
}

func target(t *testing.T, d inventory.Device, plan executionplan.AddressPlan) executionplan.ExecutionTarget {
	t.Helper()
	x := executionplan.ExecutionTarget{TargetID: d.ID, InputTarget: d.SuppliedName(), Device: executionplan.ProjectDevice(d), AddressPlan: plan, Channel: executionplan.ChannelShell, ExecutionEndpoint: executionplan.EndpointLocal}
	sum, err := executionplan.SumTarget(x)
	if err != nil {
		t.Fatal(err)
	}
	x.SourceDigest = sum
	return x
}

// Golden digests over the k03 inventory: Core-A under client authority and
// edge-b as a daemon draft.
const (
	goldenK03Client      = "dab3ed4f22f9f23a1860fd70f8bb12d7fd1cd3d29ff352d45e3459ba026d51da"
	goldenK03DaemonDraft = "d29a43b13455683ad1a142b820683dc7e7f1d09b90e33493bcb3a53d4d13e5ef"
)

func TestK03AddressPlansPinDigests(t *testing.T) {
	cfg := testConfig(t, "name.allow-daemon-resolution=true")
	devices := k03(t)
	plans, err := PlanAddresses(context.Background(), cfg, devices, AddressOptions{Capabilities: &dual, Overrides: map[string]string{"name:edge-b": "daemon"}})
	if err != nil {
		t.Fatal(err)
	}
	for i, golden := range []string{goldenK03Client, goldenK03DaemonDraft} {
		x := target(t, devices[i], plans[i])
		if err := x.Validate(executionplan.Draft); err != nil {
			t.Fatal(err)
		}
		data, _ := json.MarshalIndent(x, "", "  ")
		t.Logf("%s:\n%s", x.TargetID, data)
		if x.SourceDigest.String() != golden {
			t.Errorf("%s: source_digest %s, golden %s", x.TargetID, x.SourceDigest, golden)
		}
	}
}
