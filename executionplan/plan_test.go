package executionplan

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/inventory"
)

const (
	fixturePlanID  = "20260914T120000.000000+0000-0123456789abcdefghjk"
	fixtureJobID   = "260914-120001-00"
	fixturePrepID  = "20260914T120002.000000+0000-0123456789abcdefghjk"
	goldenDraft    = "c5255dd9d9faefe7730f2022f4ac088b9c16d01188536ef44e2cee6f2d2e3f5c"
	goldenPrepared = "6c39955cc287e9db5a61957196e0ec2d124d47b8952daa82e8af674ee188210d"
	// The three pins moved at plan schema 5: the schema number is in every
	// stage's digest; goldenFinal moved once more at the same release when
	// fixtureJobID took the job form, since the package reference binds
	// the job ID.
	// The three moved again at the rename to karvi: the plan's output root
	// and its configuration digest carry the executable's name; no counter
	// moved.
	goldenFinal = "794ecdded7403f314bb9f79085512389f4d477e69a67048c0c1069d283dcbee7"
)

var fixtureCommands = []string{"show clock", "show version", "show ip interface brief", "show running-config | include hostname"}

func fixtureDraftPlan(t *testing.T) ExecutionPlan {
	t.Helper()
	return ExecutionPlan{
		SchemaVersion: SchemaVersion, PlanID: fixturePlanID,
		Operator: Operator{Username: "netops", UID: 1000, PrimaryGID: 1000, Groups: []string{"netops"}},
		Targets:  []ExecutionTarget{fixtureDirect(t), fixtureInventory(t), fixtureDaemonDraft(t)},
		Commands: fixtureCommands, CommandPlanDigest: SumCommands(fixtureCommands),
		BlindReturns: []int{}, BlindWaitNS: int64(10 * time.Second), Blind: []bool{}, Expectations: [][]Expectation{},
		SessionInit: map[string]SessionInitProfile{},
		Dispatch:    DispatchSettings{Mode: DispatchSerial, Width: 1, DispatchOrder: OrderDefault},
		Output:      OutputSettings{Format: FormatText, Follow: true, MaxCommandBytes: 67108864, MaxJobBytes: 17179869184, Persist: true, Files: AllOutputFiles, Root: "/tmp/karvi/jobs"},
		Ping:        PingSettings{Enabled: false, Probes: PingProbes, TimeoutNS: int64(500 * time.Millisecond)},
		Sources: SourceDigests{ConfigDigest: strings.Repeat("cd", 32),
			Selectors: inventory.Provenance{Sources: []inventory.SourceRef{{Name: "smoke", Path: "/tmp/inv.csv", Digest: strings.Repeat("ab", 32)}}}},
		Planning:    PlanningTimestamps{DraftedAt: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)},
		Preparation: []PreparationEvidence{},
	}
}

func fixtureEvidence(t *testing.T, plan ExecutionPlan) PreparationEvidence {
	t.Helper()
	target := *plan.target("name:core-a")
	v6a, v6b := netip.MustParseAddr("2001:db8::10"), netip.MustParseAddr("2001:db8::11")
	target.AddressPlan.DaemonCandidates = []netip.Addr{v6a, v6b}
	target.AddressPlan.Selected = v6a
	target.AddressPlan.Alternates = []netip.Addr{v6b}
	target.AddressPlan.SelectedSource = SourceDNSDaemon
	target.AddressPlan.ResolverContext = EndpointLocal
	rd, err := SumResolution(target)
	if err != nil {
		t.Fatal(err)
	}
	return PreparationEvidence{
		ExecutionEndpoint: EndpointLocal, PreparationID: fixturePrepID, PreparationDigest: Sum([]byte("preparation")),
		PreparedAt: time.Date(2026, 9, 14, 12, 0, 5, 0, time.UTC),
		Addresses:  []AddressEvidence{{TargetID: "name:core-a", DaemonCandidates: []netip.Addr{v6a, v6b}, Selected: v6a, Alternates: []netip.Addr{v6b}, SelectedSource: SourceDNSDaemon, ResolverContext: EndpointLocal, ResolutionDigest: rd}},
	}
}

func fixtureFinalPlan(t *testing.T) ExecutionPlan {
	t.Helper()
	prepared, err := IncorporatePreparation(fixtureDraftPlan(t), fixtureEvidence(t, fixtureDraftPlan(t)))
	if err != nil {
		t.Fatal(err)
	}
	for i := range prepared.Targets {
		prepared.Targets[i].CredentialBindingID = fixtureJobID
		prepared.Targets[i].SessionInitProfile = SessionInitNone
		if prepared.Targets[i].TargetID == "name:core-a" {
			prepared.Targets[i].SessionInitProfile = "iosxe-init"
		}
	}
	prepared.SessionInit = map[string]SessionInitProfile{"iosxe-init": fixtureProfile()}
	// Command 2 is a blind send with one return, command 3 carries one
	// declaration, so the final digest covers every interactive field.
	prepared.BlindReturns = []int{0, 1, 0, 0}
	prepared.Blind = []bool{false, true, false, false}
	prepared.Expectations = [][]Expectation{{}, {}, {{Pattern: `\[confirm\]`, Response: ""}}, {}}
	final, err := Finalize(prepared, time.Date(2026, 9, 14, 12, 0, 9, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return final
}

func fixtureProfile() SessionInitProfile {
	return SessionInitProfile{Commands: []string{"terminal width 511", "show clock"}, OnError: SessionInitFailDevice, CommandTimeoutNS: int64(30 * time.Second)}
}

func fixtureHeader(t *testing.T, plan ExecutionPlan, stage Stage) PublicJobHeader {
	t.Helper()
	sum, err := SumPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	h := PublicJobHeader{
		SchemaVersion: SchemaVersion, IdempotencyKey: "idem-1", JobID: fixtureJobID,
		Operator: plan.Operator, Client: ClientIdentity{AppName: "karvi", Version: "0.9.2", Commit: "development", PID: 4242, Hostname: "ops01"},
		Mode: ModeLive, ExecutionDomain: ExecutionDomainLocal, CommandPlanDigest: plan.CommandPlanDigest, PlanDigest: sum,
	}
	if stage == Committed {
		h.CredentialPackage = &PackageReference{Protection: ProtectionLocalPeer, Digest: Sum([]byte("package"))}
	}
	return h
}

func TestPlanLifecycleAndPinnedDigests(t *testing.T) {
	draft := fixtureDraftPlan(t)
	if err := draft.Validate(Draft); err != nil {
		t.Fatal(err)
	}
	if draft.Validate(Committed) == nil {
		t.Fatal("a draft must not validate as committed")
	}
	draftSum, _ := SumPlan(draft)
	if draftSum.String() != goldenDraft {
		t.Errorf("draft digest %s, golden %s", draftSum, goldenDraft)
	}
	prepared, err := IncorporatePreparation(draft, fixtureEvidence(t, draft))
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Preparation) != 0 || draft.Targets[2].AddressPlan.Selected.IsValid() {
		t.Fatal("IncorporatePreparation modified its input")
	}
	preparedSum, _ := SumPlan(prepared)
	if preparedSum.String() != goldenPrepared {
		t.Errorf("prepared digest %s, golden %s", preparedSum, goldenPrepared)
	}
	if preparedSum == draftSum {
		t.Error("daemon evidence must move the plan digest")
	}
	final := fixtureFinalPlan(t)
	if err := final.Validate(Committed); err != nil {
		t.Fatal(err)
	}
	if final.PlanDigest.String() != goldenFinal {
		t.Errorf("final digest %s, golden %s", final.PlanDigest, goldenFinal)
	}
	data, err := json.MarshalIndent(final, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("final plan:\n%s", data)
	// Clearing the digest and re-hashing reproduces it; a JSON round trip too.
	cleared := final
	cleared.PlanDigest = Digest{}
	again, _ := SumPlan(cleared)
	if again != final.PlanDigest {
		t.Error("re-hashing the cleared plan does not reproduce the digest")
	}
	var back ExecutionPlan
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if err := back.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := back.Validate(Committed); err != nil {
		t.Fatal(err)
	}
	// Any mutation after finalization is caught.
	back.Commands[0] = "show clock detail"
	if err := back.Verify(); err == nil || !strings.HasPrefix(err.Error(), "plan_digest_mismatch") {
		t.Fatalf("mutated plan must fail verification, got %v", err)
	}
}

func TestIncorporatePreparationCopiesExactlyTheDelegatedFields(t *testing.T) {
	draft := fixtureDraftPlan(t)
	before := *draft.target("name:core-a")
	ev := fixtureEvidence(t, draft)
	prepared, err := IncorporatePreparation(draft, ev)
	if err != nil {
		t.Fatal(err)
	}
	after := *prepared.target("name:core-a")
	a := ev.Addresses[0]
	if after.AddressPlan.ResolverContext != a.ResolverContext || after.AddressPlan.Selected != a.Selected || after.AddressPlan.ResolutionDigest != a.ResolutionDigest || len(after.AddressPlan.DaemonCandidates) != 2 || len(after.AddressPlan.Alternates) != 1 || after.AddressPlan.SelectedSource != SourceDNSDaemon {
		t.Fatalf("delegated fields not copied: %+v", after.AddressPlan)
	}
	// Everything the client authored is untouched.
	after.AddressPlan.DaemonCandidates, after.AddressPlan.Selected, after.AddressPlan.Alternates = nil, netip.Addr{}, nil
	after.AddressPlan.ResolverContext, after.AddressPlan.ResolutionDigest, after.AddressPlan.SelectedSource = "", Digest{}, ""
	before.AddressPlan.DaemonCandidates, before.AddressPlan.Alternates = nil, nil
	if bj, aj := mustJSON(t, before), mustJSON(t, after); bj != aj {
		t.Fatalf("client-authored fields changed:\n%s\n%s", bj, aj)
	}
	// Tampered evidence is refused by its own digest.
	bad := ev
	bad.Addresses = []AddressEvidence{ev.Addresses[0]}
	bad.Addresses[0].Selected = netip.MustParseAddr("2001:db8::11")
	if _, err := IncorporatePreparation(draft, bad); err == nil || !strings.Contains(err.Error(), "resolution_digest") {
		t.Fatalf("tampered evidence accepted: %v", err)
	}
	// A client-authority target cannot receive evidence.
	bad = ev
	bad.Addresses = []AddressEvidence{ev.Addresses[0]}
	bad.Addresses[0].TargetID = "name:edge-b"
	if _, err := IncorporatePreparation(draft, bad); err == nil || !strings.Contains(err.Error(), "daemon-authority") {
		t.Fatalf("client target accepted evidence: %v", err)
	}
	// The same endpoint cannot prepare twice.
	if _, err := IncorporatePreparation(prepared, ev); err == nil {
		t.Fatal("duplicate endpoint evidence accepted")
	}
}

func TestAddressEvidenceMirrorsTheDelegatedAddressPlanFields(t *testing.T) {
	delegated := map[string]bool{"DaemonCandidates": true, "Selected": true, "Alternates": true, "SelectedSource": true, "ResolverContext": true, "ResolutionDigest": true}
	evidence := structFields(AddressEvidence{})
	plan := structFields(AddressPlan{})
	for name := range delegated {
		if evidence[name] == "" {
			t.Errorf("AddressEvidence lacks %s", name)
		}
		if evidence[name] != plan[name] {
			t.Errorf("%s: evidence type %s, plan type %s", name, evidence[name], plan[name])
		}
	}
	for name := range evidence {
		if name != "TargetID" && !delegated[name] {
			t.Errorf("AddressEvidence.%s is not a delegated field", name)
		}
	}
}

func TestPlanValidationVectors(t *testing.T) {
	key := "k"
	cases := []struct {
		name string
		edit func(*ExecutionPlan)
		code string
	}{
		{"schema version", func(p *ExecutionPlan) { p.SchemaVersion = 1 }, "execution_plan_invalid: schema_version"},
		{"plan id", func(p *ExecutionPlan) { p.PlanID = "plan-1" }, "execution_plan_invalid: plan_id"},
		{"operator", func(p *ExecutionPlan) { p.Operator.Username = "" }, "execution_plan_invalid: operator.username"},
		{"no targets", func(p *ExecutionPlan) { p.Targets = nil }, "plan_not_enumerated"},
		{"duplicate target", func(p *ExecutionPlan) { p.Targets = append(p.Targets, p.Targets[0]) }, "execution_plan_invalid: targets"},
		{"invalid target", func(p *ExecutionPlan) { p.Targets[0].ExecutionEndpoint = "remote" }, "execution_plan_invalid: targets[0]: execution_target_invalid"},
		{"no commands", func(p *ExecutionPlan) { p.Commands = []string{} }, "execution_plan_invalid: commands"},
		{"command digest", func(p *ExecutionPlan) { p.Commands = append(p.Commands, "show ip route") }, "execution_plan_invalid: command_plan_digest"},
		{"nul in command", func(p *ExecutionPlan) {
			p.Commands = []string{"show\x00clock"}
			p.CommandPlanDigest = SumCommands(p.Commands)
		}, "execution_plan_invalid: commands"},
		{"nil blind returns", func(p *ExecutionPlan) { p.BlindReturns = nil }, "execution_plan_invalid: blind_returns: must be present"},
		{"blind returns short", func(p *ExecutionPlan) { p.BlindReturns = []int{1} }, "execution_plan_invalid: blind_returns: 1 entries for 4 commands"},
		{"blind return negative", func(p *ExecutionPlan) { p.BlindReturns = []int{0, -1, 0, 0} }, "execution_plan_invalid: blind_returns: command 2: -1 must be 0..20"},
		{"blind return too many", func(p *ExecutionPlan) { p.BlindReturns = []int{0, 0, 0, 21} }, "execution_plan_invalid: blind_returns: command 4: 21 must be 0..20"},
		{"blind wait negative", func(p *ExecutionPlan) { p.BlindWaitNS = -1 }, "execution_plan_invalid: blind_wait_ns: -1 must be 0..10m"},
		{"blind wait too long", func(p *ExecutionPlan) { p.BlindWaitNS = int64(10*time.Minute + 1) }, "execution_plan_invalid: blind_wait_ns"},
		{"nil blind", func(p *ExecutionPlan) { p.Blind = nil }, "execution_plan_invalid: blind: must be present"},
		{"blind short", func(p *ExecutionPlan) { p.Blind = []bool{true} }, "execution_plan_invalid: blind: 1 entries for 4 commands"},
		{"returns without flags", func(p *ExecutionPlan) { p.BlindReturns = []int{0, 1, 0, 0} }, "execution_plan_invalid: blind: command 2: 1 blind returns require the flag"},
		{"returns without the flag", func(p *ExecutionPlan) {
			p.BlindReturns, p.Blind = []int{0, 0, 0, 2}, []bool{false, false, false, false}
		}, "execution_plan_invalid: blind: command 4: 2 blind returns require the flag"},
		{"nil expectations", func(p *ExecutionPlan) { p.Expectations = nil }, "execution_plan_invalid: expectations: must be present"},
		{"expectations short", func(p *ExecutionPlan) { p.Expectations = [][]Expectation{{}} }, "execution_plan_invalid: expectations: 1 entries for 4 commands"},
		{"null expectation list", func(p *ExecutionPlan) { p.Expectations = [][]Expectation{{}, nil, {}, {}} }, "execution_plan_invalid: expectations: command 2: must be present"},
		{"too many expectations", func(p *ExecutionPlan) {
			p.Expectations = [][]Expectation{{}, {}, {}, make([]Expectation, ExpectationsMax+1)}
			for i := range p.Expectations[3] {
				p.Expectations[3][i] = Expectation{Pattern: "x"}
			}
		}, "execution_plan_invalid: expectations: command 4: 21 declarations; at most 20"},
		{"empty pattern", func(p *ExecutionPlan) { p.Expectations = [][]Expectation{{{Pattern: "", Response: "y"}}, {}, {}, {}} }, "execution_plan_invalid: expectations: command 1 declaration 1: the pattern is required"},
		{"invalid pattern", func(p *ExecutionPlan) {
			p.Expectations = [][]Expectation{{}, {{Pattern: "ok"}, {Pattern: "(unclosed"}}, {}, {}}
		}, "execution_plan_invalid: expectations: command 2 declaration 2: pattern \"(unclosed\": error parsing regexp"},
		{"returns and declaration", func(p *ExecutionPlan) {
			p.BlindReturns = []int{0, 1, 0, 0}
			p.Blind = []bool{false, true, false, false}
			p.Expectations = [][]Expectation{{}, {{Pattern: `confirm\]`}}, {}, {}}
		}, "execution_plan_invalid: expectations: command 2: carries 1 blind returns and a declaration"},
		{"dispatch mode", func(p *ExecutionPlan) { p.Dispatch.Mode = "burst" }, "execution_plan_invalid: dispatch.mode"},
		{"order", func(p *ExecutionPlan) { p.Dispatch.DispatchOrder = "name" }, "execution_plan_invalid: dispatch.dispatch_order"},
		{"key without shuffle", func(p *ExecutionPlan) { p.Dispatch.ShuffleKey = &key }, "execution_plan_invalid: dispatch.shuffle_key"},
		{"shuffle without key", func(p *ExecutionPlan) { p.Dispatch.DispatchOrder = OrderShuffle }, "execution_plan_invalid: dispatch.shuffle_key"},
		{"percent", func(p *ExecutionPlan) { p.Dispatch.HaltErrorPercent = 101 }, "execution_plan_invalid: dispatch.halt_error_percent"},
		{"format", func(p *ExecutionPlan) { p.Output.Format = "yaml" }, "execution_plan_invalid: output.format"},
		{"limits", func(p *ExecutionPlan) { p.Output.MaxJobBytes = 0 }, "execution_plan_invalid: output"},
		{"probes", func(p *ExecutionPlan) { p.Ping.Probes = 1 }, "execution_plan_invalid: ping.probes"},
		{"config digest", func(p *ExecutionPlan) { p.Sources.ConfigDigest = "" }, "execution_plan_invalid: sources.config_digest"},
		{"drafted at", func(p *ExecutionPlan) { p.Planning.DraftedAt = time.Time{} }, "execution_plan_invalid: planning.drafted_at"},
		{"nil preparation", func(p *ExecutionPlan) { p.Preparation = nil }, "execution_plan_invalid: preparation"},
		{"evidence for unknown target", func(p *ExecutionPlan) {
			p.Preparation = []PreparationEvidence{{ExecutionEndpoint: EndpointLocal, PreparationID: fixturePrepID, PreparationDigest: Sum([]byte("p")), PreparedAt: p.Planning.DraftedAt, Addresses: []AddressEvidence{{TargetID: "name:ghost", DaemonCandidates: []netip.Addr{}, Alternates: []netip.Addr{}, ResolverContext: EndpointLocal, ResolutionDigest: Sum([]byte("r"))}}}}
		}, "execution_plan_invalid: preparation[0].addresses[0].target_id"},
	}
	for _, c := range cases {
		p := fixtureDraftPlan(t)
		c.edit(&p)
		err := p.Validate(Draft)
		if err == nil || !strings.HasPrefix(err.Error(), c.code) {
			t.Errorf("%s: want %q, got %v", c.name, c.code, err)
		}
	}
	// Committed-stage rules.
	final := fixtureFinalPlan(t)
	noEvidence := final
	noEvidence.Preparation = []PreparationEvidence{}
	if err := noEvidence.Validate(Committed); err == nil || !strings.Contains(err.Error(), "no address evidence") && !strings.Contains(err.Error(), "resolver_context") {
		t.Errorf("committed plan without evidence: %v", err)
	}
	unfinalized := final
	unfinalized.Planning.FinalizedAt = time.Time{}
	if err := unfinalized.Validate(Committed); err == nil || !strings.Contains(err.Error(), "finalized_at") {
		t.Errorf("committed plan without finalized_at: %v", err)
	}
}

func TestHeaderValidationAndMatching(t *testing.T) {
	draft := fixtureDraftPlan(t)
	h := fixtureHeader(t, draft, Draft)
	if err := h.Validate(Draft); err != nil {
		t.Fatal(err)
	}
	if err := h.Matches(&draft, Draft); err != nil {
		t.Fatal(err)
	}
	if h.Validate(Committed) == nil {
		t.Fatal("a prepare header must not validate for commit")
	}
	final := fixtureFinalPlan(t)
	hc := fixtureHeader(t, final, Committed)
	if err := hc.Validate(Committed); err != nil {
		t.Fatal(err)
	}
	if err := hc.Matches(&final, Committed); err != nil {
		t.Fatal(err)
	}
	if err := hc.Matches(&draft, Committed); err == nil || !strings.HasPrefix(err.Error(), "plan_digest_mismatch") {
		t.Fatalf("commit header against the draft: %v", err)
	}
	cases := []struct {
		name string
		edit func(*PublicJobHeader)
		code string
	}{
		{"schema", func(h *PublicJobHeader) { h.SchemaVersion = 0 }, "job_header_invalid: schema_version"},
		{"idempotency", func(h *PublicJobHeader) { h.IdempotencyKey = "" }, "job_header_invalid: idempotency_key"},
		{"job id", func(h *PublicJobHeader) { h.JobID = "job" }, "job_header_invalid: job_id"},
		{"client", func(h *PublicJobHeader) { h.Client.Version = "" }, "job_header_invalid: client"},
		{"mode", func(h *PublicJobHeader) { h.Mode = "dry-run" }, "job_header_invalid: mode"},
		{"domain", func(h *PublicJobHeader) { h.ExecutionDomain = "site-b" }, "job_header_invalid: execution_domain"},
		{"priority", func(h *PublicJobHeader) { h.Priority = 1 }, "job_header_invalid: priority"},
		{"package at prepare", func(h *PublicJobHeader) {
			h.CredentialPackage = &PackageReference{Protection: ProtectionLocalPeer, Digest: Sum(nil)}
		}, "job_header_invalid: credential_package"},
	}
	for _, c := range cases {
		h := fixtureHeader(t, draft, Draft)
		c.edit(&h)
		err := h.Validate(Draft)
		if err == nil || !strings.HasPrefix(err.Error(), c.code) {
			t.Errorf("%s: want %q, got %v", c.name, c.code, err)
		}
	}
	bad := fixtureHeader(t, final, Committed)
	bad.CredentialPackage.Protection = "none"
	if err := bad.Validate(Committed); err == nil || !strings.HasPrefix(err.Error(), "job_header_invalid: credential_package.protection") {
		t.Errorf("protection none: %v", err)
	}
}

func TestHeaderDomainCoversEveryTarget(t *testing.T) {
	draft := fixtureDraftPlan(t)
	h := fixtureHeader(t, draft, Draft)
	draft.Targets[1].ExecutionEndpoint = "worker-7"
	sum, _ := SumPlan(draft)
	h.PlanDigest = sum
	if err := h.Matches(&draft, Draft); err == nil || !strings.HasPrefix(err.Error(), "job_header_invalid: execution_domain") {
		t.Fatalf("foreign endpoint accepted: %v", err)
	}
}

func TestSumCommandsMatchesTheManifestRule(t *testing.T) {
	// The manifest's command_plan_digest is SHA-256 over each command followed
	// by NUL, so this vector is sha256("show clock\x00").
	if got := SumCommands([]string{"show clock"}).String(); got != "969208752962a3a36418bd94ba7b6d848d8b6ad9bcce1982457e1c662a51a2e6" {
		t.Fatalf("SumCommands = %s", got)
	}
	if SumCommands(nil) == SumCommands([]string{""}) {
		t.Fatal("an empty command must differ from no command")
	}
	// NUL inside a command would collide with two commands, so the plan
	// refuses it (validated in TestPlanValidationVectors).
}

func structFields(v any) map[string]string {
	out := map[string]string{}
	rt := reflectType(v)
	for i := 0; i < rt.NumField(); i++ {
		out[rt.Field(i).Name] = rt.Field(i).Type.String()
	}
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestIncorporatePreparationCopiesTheSelectedSource: the daemon reports
// which branch of the selection rule it
// took, IncorporatePreparation copies it instead of assuming DNS, and the
// resolution digest covers it.
func TestIncorporatePreparationCopiesTheSelectedSource(t *testing.T) {
	draft := fixtureDraftPlan(t)
	hint := netip.MustParseAddr("2001:db8::10")
	target := draft.target("name:core-a")
	target.AddressPlan.ClientCandidates = []netip.Addr{hint}
	sum, err := SumTarget(*target)
	if err != nil {
		t.Fatal(err)
	}
	target.SourceDigest = sum
	if err := draft.Validate(Draft); err != nil {
		t.Fatal(err)
	}
	evidence := func(source string) PreparationEvidence {
		filled := *target
		filled.AddressPlan.DaemonCandidates = []netip.Addr{}
		filled.AddressPlan.Selected = hint
		filled.AddressPlan.Alternates = []netip.Addr{}
		filled.AddressPlan.SelectedSource = source
		filled.AddressPlan.ResolverContext = EndpointLocal
		rd, err := SumResolution(filled)
		if err != nil {
			t.Fatal(err)
		}
		return PreparationEvidence{
			ExecutionEndpoint: EndpointLocal, PreparationID: fixturePrepID, PreparationDigest: Sum([]byte("preparation")),
			PreparedAt: time.Date(2026, 9, 14, 12, 0, 5, 0, time.UTC),
			Addresses:  []AddressEvidence{{TargetID: "name:core-a", DaemonCandidates: []netip.Addr{}, Selected: hint, Alternates: []netip.Addr{}, SelectedSource: source, ResolverContext: EndpointLocal, ResolutionDigest: rd}},
		}
	}
	// A hint taken by the daemon is recorded as inventory, not as a lookup.
	prepared, err := IncorporatePreparation(draft, evidence(SourceInventory))
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared.target("name:core-a").AddressPlan.SelectedSource; got != SourceInventory {
		t.Fatalf("selected_source %q, want %q", got, SourceInventory)
	}
	for i := range prepared.Targets {
		prepared.Targets[i].CredentialBindingID = fixtureJobID
		prepared.Targets[i].SessionInitProfile = SessionInitNone
	}
	final, err := Finalize(prepared, time.Date(2026, 9, 14, 12, 0, 9, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := final.Validate(Committed); err != nil {
		t.Fatal(err)
	}
	// The source is inside the resolution digest: changing the claim after
	// hashing is caught, so the daemon cannot report one branch and sign another.
	tampered := evidence(SourceInventory)
	tampered.Addresses[0].SelectedSource = SourceDNSDaemon
	if _, err := IncorporatePreparation(draft, tampered); err == nil || !strings.Contains(err.Error(), "resolution_digest") {
		t.Fatalf("tampered selected_source accepted: %v", err)
	}
	// Evidence that selects without saying how fails the committed plan.
	unsourced, err := IncorporatePreparation(draft, evidence(""))
	if err != nil {
		t.Fatal(err)
	}
	for i := range unsourced.Targets {
		unsourced.Targets[i].CredentialBindingID = fixtureJobID
		unsourced.Targets[i].SessionInitProfile = SessionInitNone
	}
	unsourcedFinal, err := Finalize(unsourced, time.Date(2026, 9, 14, 12, 0, 9, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := unsourcedFinal.Validate(Committed); err == nil || !strings.Contains(err.Error(), "selected_source") {
		t.Fatalf("unsourced selection validated as committed: %v", err)
	}
}

// TestPlatformCommands covers the platform command lists at schema 7: a
// plan may carry a list per platform in place of commands,
// every target's platform must have one then, the digest covers the lists,
// an empty list is refused, and the collection sub-block is validated.
func TestPlatformCommands(t *testing.T) {
	base := fixtureDraftPlan(t)
	lists := map[string][]string{}
	for _, t := range base.Targets {
		lists[t.Device.Platform] = []string{"show running-config", "show version"}
	}
	one := ""
	for name := range lists {
		one = name
	}
	p := base
	p.Commands, p.PlatformCommands = nil, lists
	p.CommandPlanDigest = SumCommandPlan(nil, lists)
	if err := p.Validate(Draft); err != nil {
		t.Fatalf("lists in place of commands: %v", err)
	}
	if got := p.CommandsFor(one); len(got) != 2 || got[0] != "show running-config" {
		t.Fatalf("CommandsFor: %q", got)
	}
	if got := p.CommandsFor("no-such-platform"); len(got) != 0 {
		t.Fatalf("a platform without a list falls back to the plan's commands, which are none: %q", got)
	}
	if p.CommandCount() != 2 {
		t.Fatalf("CommandCount %d", p.CommandCount())
	}
	if SumCommandPlan(nil, map[string][]string{one: {"a", "b"}}) == SumCommandPlan(nil, map[string][]string{one: {"b", "a"}}) {
		t.Fatal("the digest does not see the order of a list")
	}
	if SumCommandPlan([]string{"show clock"}, nil) != SumCommands([]string{"show clock"}) {
		t.Fatal("a plan without lists sums as it did")
	}
	// A target whose platform has no list, with no commands to fall back on.
	q := p
	q.PlatformCommands = map[string][]string{"no-such-platform": {"show version"}}
	q.CommandPlanDigest = SumCommandPlan(nil, q.PlatformCommands)
	if err := q.Validate(Draft); err == nil || !strings.Contains(err.Error(), "has no list") {
		t.Fatalf("a target without a list: %v", err)
	}
	// Commands beside lists: the target falls back, the digest covers both.
	r := base
	r.PlatformCommands = lists
	r.CommandPlanDigest = SumCommandPlan(r.Commands, lists)
	if err := r.Validate(Draft); err != nil {
		t.Fatalf("commands beside lists: %v", err)
	}
	if r.CommandPlanDigest == SumCommands(r.Commands) {
		t.Fatal("the digest ignores the lists")
	}
	// An empty list is refused; the old digest rule does not cover a list.
	e := p
	e.PlatformCommands = map[string][]string{one: {}}
	e.CommandPlanDigest = SumCommandPlan(nil, e.PlatformCommands)
	if err := e.Validate(Draft); err == nil || !strings.Contains(err.Error(), "empty list") {
		t.Fatalf("an empty list: %v", err)
	}
	// The collection sub-block.
	c := base
	c.Output.Collection = &CollectionSettings{Directory: "/srv/karvi/crun", FileMode: "0660"}
	if err := c.Validate(Draft); err != nil {
		t.Fatalf("a collection: %v", err)
	}
	c.Output.Collection = &CollectionSettings{Directory: "crun", FileMode: "0660"}
	if err := c.Validate(Draft); err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Fatalf("a relative directory: %v", err)
	}
	c.Output.Collection = &CollectionSettings{Directory: "/srv/karvi/crun", FileMode: "0600"}
	if err := c.Validate(Draft); err == nil || !strings.Contains(err.Error(), "file_mode") {
		t.Fatalf("a mode outside the enum: %v", err)
	}
}
