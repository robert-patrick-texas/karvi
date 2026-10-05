package executionplan

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/inventory"
)

// fixtures mirror the k03 smoke inventory: a direct literal target, an
// inventory device under client authority, and one under daemon authority.
func fixtureDirect(t *testing.T) ExecutionTarget {
	t.Helper()
	d := inventory.Direct("127.0.0.1", "generic", "system", 0)
	d.InputToken = "127.0.0.1"
	addr := netip.MustParseAddr("127.0.0.1")
	tgt := ExecutionTarget{
		TargetID: d.ID, InputTarget: d.SuppliedName(), Device: ProjectDevice(d),
		AddressPlan: AddressPlan{
			Authority: AddressByClient, FamilyPreference: FamilyIPv4, TransformedName: "127.0.0.1",
			SuffixAction: SuffixActionNone, ClientCandidates: []netip.Addr{addr}, DaemonCandidates: []netip.Addr{},
			Selected: addr, Alternates: []netip.Addr{}, SelectedSource: SourceInventory, ResolverContext: ResolverContextClient,
		},
		Channel: ChannelShell, ExecutionEndpoint: EndpointLocal,
	}
	return withSourceDigest(t, tgt)
}

func fixtureInventory(t *testing.T) ExecutionTarget {
	t.Helper()
	d := inventory.Device{Name: "edge-b", Platform: "generic", Transport: "system", Site: "branch", ManagementAddress: netip.MustParseAddr("127.0.0.1"), Source: inventory.SourceRef{Name: "smoke", Path: "/tmp/inv.csv", Line: 3, Digest: strings.Repeat("ab", 32)}}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddr("127.0.0.1")
	tgt := ExecutionTarget{
		TargetID: d.ID, InputTarget: "edge-b", Device: ProjectDevice(d),
		AddressPlan: AddressPlan{
			Authority: AddressByClient, FamilyPreference: FamilyIPv4, TransformedName: "edge-b",
			SuffixAction: SuffixActionNone, ClientCandidates: []netip.Addr{addr}, DaemonCandidates: []netip.Addr{},
			Selected: addr, Alternates: []netip.Addr{}, SelectedSource: SourceInventory, ResolverContext: ResolverContextClient,
		},
		Channel: ChannelShell, ExecutionEndpoint: EndpointLocal,
	}
	return withSourceDigest(t, tgt)
}

func fixtureDaemonDraft(t *testing.T) ExecutionTarget {
	t.Helper()
	d := inventory.Device{Name: "Core-A", CanonicalName: "core-a", Platform: "generic", Transport: "system", Site: "hq", Groups: []string{"core"}, Source: inventory.SourceRef{Name: "smoke", Path: "/tmp/inv.csv", Line: 2, Digest: strings.Repeat("ab", 32)}}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	d.InputToken = "Core-A"
	tgt := ExecutionTarget{
		TargetID: d.ID, InputTarget: d.SuppliedName(), Device: ProjectDevice(d),
		AddressPlan: AddressPlan{
			Authority: AddressByDaemon, FamilyPreference: FamilyIPv6, TransformedName: "core-a",
			QueryName: "core-a.example.gov", SuffixAction: SuffixActionPrefix + ".example.gov",
			ClientCandidates: []netip.Addr{}, DaemonCandidates: []netip.Addr{}, Alternates: []netip.Addr{},
		},
		Channel: ChannelShell, ExecutionEndpoint: EndpointLocal,
	}
	return withSourceDigest(t, tgt)
}

func withSourceDigest(t *testing.T, tgt ExecutionTarget) ExecutionTarget {
	t.Helper()
	d, err := SumTarget(tgt)
	if err != nil {
		t.Fatal(err)
	}
	tgt.SourceDigest = d
	return tgt
}

// Pinned digests: a change here means the wire form changed.
const (
	goldenDirect      = "1105d3ff8665a6934af233dba7916e6f9a32fa0804573e1f95bcb94804d9d6a2"
	goldenInventory   = "616d086bde1d6a524df386019b3a4bb94fe2a808e4a4353158077f5100521d8a"
	goldenDaemonDraft = "4d3907ceca90f0be831e2f282c9ac8d123ed799f464714beaf2edd74accf0fdb"
)

func TestFixturesValidateAndPinDigests(t *testing.T) {
	cases := []struct {
		name   string
		target ExecutionTarget
		stage  Stage
		golden string
	}{
		{"direct", fixtureDirect(t), Draft, goldenDirect},
		{"inventory", fixtureInventory(t), Draft, goldenInventory},
		{"daemon-draft", fixtureDaemonDraft(t), Draft, goldenDaemonDraft},
	}
	for _, c := range cases {
		if err := c.target.Validate(c.stage); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		data, err := json.MarshalIndent(c.target, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s:\n%s", c.name, data)
		if got := c.target.SourceDigest.String(); got != c.golden {
			t.Errorf("%s: source_digest %s, golden %s", c.name, got, c.golden)
		}
		// The digest ignores the binding and the resolution digest.
		bound := c.target
		bound.CredentialBindingID = "cred-1"
		bound.AddressPlan.ResolutionDigest = Sum([]byte("x"))
		again, _ := SumTarget(bound)
		if again != c.target.SourceDigest {
			t.Errorf("%s: digest moved when binding or resolution digest changed", c.name)
		}
		var back ExecutionTarget
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatal(err)
		}
		round, _ := SumTarget(back)
		if round != c.target.SourceDigest {
			t.Errorf("%s: digest differs after a JSON round trip", c.name)
		}
	}
}

func TestJSONFormOmitsUnsetOptionalFields(t *testing.T) {
	data, err := json.Marshal(fixtureDaemonDraft(t))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, absent := range []string{`"selected"`, `"resolution_digest"`, `"credential_binding_id"`, `"selected_source"`, `"resolver_context"`, `"port"`, `"session_cap"`} {
		if strings.Contains(s, absent) {
			t.Errorf("draft daemon target must omit %s: %s", absent, s)
		}
	}
	for _, present := range []string{`"client_candidates":[]`, `"daemon_candidates":[]`, `"alternates":[]`, `"attributes":{}`, `"groups":["core"]`, `"query_name":"core-a.example.gov"`} {
		if !strings.Contains(s, present) {
			t.Errorf("draft daemon target must contain %s: %s", present, s)
		}
	}
}

func TestCommittedStageRequiresSelectionAndBinding(t *testing.T) {
	tgt := fixtureDirect(t)
	if err := tgt.Validate(Committed); err == nil || !strings.Contains(err.Error(), "credential_binding_id") {
		t.Fatalf("want credential_binding_id error, got %v", err)
	}
	tgt.CredentialBindingID = "20260914T120000.000000+0000-0123456789abcdefghjk"
	if err := tgt.Validate(Committed); err == nil || !strings.Contains(err.Error(), "session_init_profile: is required before commit") {
		t.Fatalf("want session_init_profile error, got %v", err)
	}
	tgt.SessionInitProfile = SessionInitNone
	if err := tgt.Validate(Committed); err != nil {
		t.Fatal(err)
	}
	ex := fixtureDaemonDraft(t)
	ex.CredentialBindingID, ex.SessionInitProfile = tgt.CredentialBindingID, SessionInitNone
	if err := ex.Validate(Committed); err == nil || !strings.Contains(err.Error(), "resolver_context") {
		t.Fatalf("daemon target without evidence must fail on resolver_context, got %v", err)
	}
	v6 := netip.MustParseAddr("2001:db8::10")
	ex.AddressPlan.DaemonCandidates = []netip.Addr{v6, netip.MustParseAddr("2001:db8::11")}
	ex.AddressPlan.Selected = v6
	ex.AddressPlan.Alternates = []netip.Addr{netip.MustParseAddr("2001:db8::11")}
	ex.AddressPlan.SelectedSource = SourceDNSDaemon
	ex.AddressPlan.ResolverContext = EndpointLocal
	rd, err := SumResolution(ex)
	if err != nil {
		t.Fatal(err)
	}
	ex.AddressPlan.ResolutionDigest = rd
	if err := ex.Validate(Committed); err != nil {
		t.Fatal(err)
	}
	// The source digest is unchanged by the daemon's additions to the
	// resolution digest, but moves with the daemon-filled address fields,
	// which is why the plan digest is recomputed after preparation.
	if ex.Validate(Draft) == nil {
		t.Fatal("a target carrying daemon evidence is not a valid draft")
	}
}

func TestValidationVectors(t *testing.T) {
	v4 := netip.MustParseAddr("127.0.0.1")
	mapped := netip.MustParseAddr("::ffff:127.0.0.1")
	cases := []struct {
		name  string
		edit  func(*ExecutionTarget)
		stage Stage
		field string
	}{
		{"empty target id", func(x *ExecutionTarget) { x.TargetID = "" }, Draft, "target_id"},
		{"target id differs from device id", func(x *ExecutionTarget) { x.TargetID = "name:other" }, Draft, "target_id"},
		{"target id with space", func(x *ExecutionTarget) { x.TargetID = "name:a b"; x.Device.ID = "name:a b" }, Draft, "target_id"},
		{"empty input target", func(x *ExecutionTarget) { x.InputTarget = "" }, Draft, "input_target"},
		{"empty platform", func(x *ExecutionTarget) { x.Device.Platform = " " }, Draft, "device.platform"},
		{"empty transport", func(x *ExecutionTarget) { x.Device.Transport = "" }, Draft, "device.transport"},
		{"session cap 0", func(x *ExecutionTarget) { z := 0; x.Device.SessionCap = &z }, Draft, "device.session_cap"},
		{"nil groups", func(x *ExecutionTarget) { x.Device.Groups = nil }, Draft, "device.groups"},
		{"reserved attribute", func(x *ExecutionTarget) { x.Device.Attributes["karvi_x"] = "1" }, Draft, "device.attributes"},
		{"remote endpoint", func(x *ExecutionTarget) { x.ExecutionEndpoint = "worker-7" }, Draft, "execution_endpoint"},
		{"zero source digest", func(x *ExecutionTarget) { x.SourceDigest = Digest{} }, Draft, "source_digest"},
		{"bad authority", func(x *ExecutionTarget) { x.AddressPlan.Authority = "executor" }, Draft, "address_plan.authority"},
		{"bad family", func(x *ExecutionTarget) { x.AddressPlan.FamilyPreference = "any" }, Draft, "address_plan.family_preference"},
		{"bad suffix action", func(x *ExecutionTarget) { x.AddressPlan.SuffixAction = "none" }, Draft, "address_plan.suffix_action"},
		{"nil alternates", func(x *ExecutionTarget) { x.AddressPlan.Alternates = nil }, Draft, "address_plan.alternates"},
		{"mapped candidate", func(x *ExecutionTarget) {
			x.AddressPlan.ClientCandidates = []netip.Addr{mapped}
			x.AddressPlan.Selected = mapped
		}, Draft, "address_plan.client_candidates"},
		{"duplicate candidate", func(x *ExecutionTarget) { x.AddressPlan.ClientCandidates = []netip.Addr{v4, v4} }, Draft, "address_plan.client_candidates"},
		{"selected without source", func(x *ExecutionTarget) { x.AddressPlan.SelectedSource = "" }, Draft, "address_plan.selected_source"},
		{"bad selected source", func(x *ExecutionTarget) { x.AddressPlan.SelectedSource = "dns" }, Draft, "address_plan.selected_source"},
		{"selected not a candidate", func(x *ExecutionTarget) { x.AddressPlan.Selected = netip.MustParseAddr("10.0.0.1") }, Draft, "address_plan.selected"},
		{"alternate equals selected", func(x *ExecutionTarget) { x.AddressPlan.Alternates = []netip.Addr{v4} }, Draft, "address_plan.alternates"},
		{"client with daemon candidates", func(x *ExecutionTarget) {
			x.AddressPlan.DaemonCandidates = []netip.Addr{netip.MustParseAddr("10.0.0.2")}
		}, Draft, "address_plan.daemon_candidates"},
		{"client with daemon source", func(x *ExecutionTarget) { x.AddressPlan.SelectedSource = SourceDNSDaemon }, Draft, "address_plan.selected_source"},
		{"client with foreign resolver", func(x *ExecutionTarget) { x.AddressPlan.ResolverContext = "local" }, Draft, "address_plan.resolver_context"},
		{"client with resolution digest", func(x *ExecutionTarget) { x.AddressPlan.ResolutionDigest = Sum([]byte("x")) }, Draft, "address_plan.resolution_digest"},
		{"client without query or literal", func(x *ExecutionTarget) {
			x.AddressPlan.ClientCandidates = []netip.Addr{}
			x.AddressPlan.Selected = netip.Addr{}
			x.AddressPlan.SelectedSource = ""
		}, Draft, "address_plan"},
		{"binding id with control char", func(x *ExecutionTarget) { x.CredentialBindingID = "a\tb" }, Draft, "credential_binding_id"},
	}
	for _, c := range cases {
		tgt := fixtureDirect(t)
		c.edit(&tgt)
		err := tgt.Validate(c.stage)
		if err == nil {
			t.Errorf("%s: expected an error", c.name)
			continue
		}
		if !strings.HasPrefix(err.Error(), "execution_target_invalid: "+c.field+": ") && !strings.HasPrefix(err.Error(), "execution_target_invalid: "+c.field+" ") {
			t.Errorf("%s: want field %q, got %v", c.name, c.field, err)
		}
	}
	daemonCases := []struct {
		name  string
		edit  func(*ExecutionTarget)
		field string
	}{
		{"daemon without query name", func(x *ExecutionTarget) { x.AddressPlan.QueryName = "" }, "address_plan.query_name"},
		{"daemon draft with candidates", func(x *ExecutionTarget) { x.AddressPlan.DaemonCandidates = []netip.Addr{v4} }, "address_plan"},
		{"daemon draft with selection", func(x *ExecutionTarget) {
			x.AddressPlan.ClientCandidates = []netip.Addr{v4}
			x.AddressPlan.Selected = v4
			x.AddressPlan.SelectedSource = SourceInventory
		}, "address_plan"},
		{"daemon with client source", func(x *ExecutionTarget) {
			x.AddressPlan.ClientCandidates = []netip.Addr{v4}
			x.AddressPlan.Selected = v4
			x.AddressPlan.SelectedSource = SourceDNSClient
		}, "address_plan.selected_source"},
	}
	for _, c := range daemonCases {
		tgt := fixtureDaemonDraft(t)
		c.edit(&tgt)
		err := tgt.Validate(Draft)
		if err == nil || !strings.HasPrefix(err.Error(), "execution_target_invalid: "+c.field) {
			t.Errorf("%s: want field %q, got %v", c.name, c.field, err)
		}
	}
}

func TestDigestTextForm(t *testing.T) {
	d := Sum([]byte("karvi"))
	text, _ := d.MarshalText()
	if len(text) != 64 || strings.ToLower(string(text)) != string(text) {
		t.Fatalf("digest text %q", text)
	}
	back, err := ParseDigest(string(text))
	if err != nil || back != d {
		t.Fatalf("round trip: %v", err)
	}
	for _, bad := range []string{"", strings.Repeat("A", 64), strings.Repeat("0", 63), strings.Repeat("g", 64)} {
		if _, err := ParseDigest(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	var wrapper struct {
		Required Digest `json:"required"`
		Optional Digest `json:"optional,omitzero"`
	}
	wrapper.Required = d
	data, _ := json.Marshal(wrapper)
	if strings.Contains(string(data), "optional") || !strings.Contains(string(data), `"required":"`+d.String()+`"`) {
		t.Fatalf("json %s", data)
	}
}

// TestTargetNoticesValidateAndDigest: a target's planning notices are
// present in the JSON only when
// nonempty, so a plan without one is unchanged; they are inside the source
// digest; and Validate holds each to a code, a message, and non-sensitive
// detail keys.
func TestTargetNoticesValidateAndDigest(t *testing.T) {
	plain := fixtureInventory(t)
	if b, _ := json.Marshal(plain); strings.Contains(string(b), `"notices"`) {
		t.Fatalf("a target without notices serialises the key: %s", b)
	}
	noted := fixtureInventory(t)
	noted.Notices = []TargetNotice{{Code: "platform_not_set", Message: "no platform in inventory source smoke line 3; proceeding as generic (no platform-resolution.default)", Details: map[string]string{"supplied": "", "source": "smoke line 3", "used": "generic"}}}
	if err := noted.Validate(Draft); err != nil {
		t.Fatal(err)
	}
	sum, err := SumTarget(noted)
	if err != nil {
		t.Fatal(err)
	}
	if sum == plain.SourceDigest {
		t.Fatal("the notice is outside the source digest")
	}
	if b, _ := json.Marshal(noted); !strings.Contains(string(b), `"notices":[{"code":"platform_not_set"`) {
		t.Fatalf("serialised: %s", b)
	}
	for name, n := range map[string]TargetNotice{
		"blank code":    {Code: "", Message: "m"},
		"blank message": {Code: "platform_not_set", Message: " "},
		"sensitive key": {Code: "platform_not_set", Message: "m", Details: map[string]string{"password": "x"}},
	} {
		bad := fixtureInventory(t)
		bad.Notices = []TargetNotice{n}
		if err := bad.Validate(Draft); err == nil || !strings.Contains(err.Error(), "notices[0]") {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

// A target's channel is shell or exec; exec over telnet is refused, and
// until the exec channel is built exec on any transport is too.
func TestTargetChannelValidation(t *testing.T) {
	for _, tc := range []struct{ channel, transport, want string }{
		{ChannelShell, "system", ""},
		{"", "system", `channel: "" is neither`},
		{"pty", "system", `channel: "pty" is neither`},
		{ChannelExec, TransportTelnet, "exec over telnet"},
		{ChannelExec, "native", "exec channels are not built on the native transport"},
	} {
		tgt := fixtureDirect(t)
		tgt.Channel, tgt.Device.Transport = tc.channel, tc.transport
		err := tgt.Validate(Draft)
		if (tc.want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q over %s: %v", tc.channel, tc.transport, err)
		}
	}
}
