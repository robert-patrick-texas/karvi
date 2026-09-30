package planner

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// fakeBackend answers every device with one account, records what it was
// asked, and can leave named devices unfound.
type fakeBackend struct {
	password string
	missing  map[string]bool
	calls    int32
	match    func(inventory.Device) credentials.Match
}

func (b *fakeBackend) Name() string                  { return "fake" }
func (b *fakeBackend) Mode() credentials.BackendMode { return credentials.OperatorKeyed }
func (b *fakeBackend) Resolve(_ context.Context, req credentials.ResolveRequest) credentials.BackendResult {
	atomic.AddInt32(&b.calls, 1)
	if b.missing[req.Device.CanonicalName] {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	match := credentials.Match{Category: "operator", SafeValue: req.Operator.Username, Source: "fake"}
	if b.match != nil {
		match = b.match(req.Device)
	}
	return credentials.BackendResult{Outcome: credentials.Success, Credential: credentials.Credential{Material: secrets.NewMaterial("svc", b.password, ""), Backend: "fake", Policy: req.Policy, MatchedOn: match}}
}

// fakeInput is an operator whose environment holds NETUSER and whose
// terminal answers every prompt with one password, counting the prompts.
type fakeInput struct {
	prompts []string
	noEnv   bool
}

func (f *fakeInput) LookupEnv(_ context.Context, name string) (string, bool, error) {
	if name == "NETUSER" && !f.noEnv {
		return "typed", true, nil
	}
	return "", false, nil
}

func (f *fakeInput) Prompt(_ context.Context, req credentialbackend.PromptRequest) (string, error) {
	f.prompts = append(f.prompts, req.Label()+" for "+req.Target)
	return "pw-" + req.Field, nil
}

func newResolver(t *testing.T, cfg configload.Snapshot, backend *fakeBackend) *credentialbackend.Resolver {
	t.Helper()
	r, err := credentialbackend.New(cfg, operator, nil)
	if err != nil {
		t.Fatal(err)
	}
	if backend != nil {
		r.RegisterBackend("fake", backend)
	}
	return r
}

func direct(name string) inventory.Device {
	d := inventory.Direct(name, "generic", "system", 0)
	d.TransportExplicit = true
	return d
}

func planFor(t *testing.T, cfg configload.Snapshot, devices []inventory.Device, overrides map[string]string) executionplan.ExecutionPlan {
	t.Helper()
	opts := draftOptions(plantest.Commands)
	opts.Address.Overrides = overrides
	draft, err := Draft(context.Background(), cfg, operator, TargetSet{Devices: devices, Order: executionplan.OrderDefault}, opts, plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func finish(t *testing.T, p *CredentialPlanner, plan executionplan.ExecutionPlan) (executionplan.ExecutionPlan, executionplan.PublicJobHeader, credentialpackage.CredentialPackage) {
	t.Helper()
	bound, err := p.Bind(plan)
	if err != nil {
		t.Fatal(err)
	}
	final, err := executionplan.Finalize(bound, plantest.FinalizedAt)
	if err != nil {
		t.Fatal(err)
	}
	header, err := Header(final, plantest.JobID, executionplan.ModeLive)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := p.Package(final, header, "daemon:ops01:1000", "ops01", plantest.FinalizedAt)
	if err != nil {
		t.Fatal(err)
	}
	ref, projection, err := PackageReference(pkg)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := CommitHeader(final, plantest.JobID, executionplan.ModeLive, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := commit.Validate(executionplan.Committed); err != nil {
		t.Fatal(err)
	}
	if err := pkg.Validate(&final, commit, "daemon:ops01:1000", plantest.FinalizedAt); err != nil {
		t.Fatal(err)
	}
	if projection.PackageDigest != ref.Digest || len(projection.Bindings) != len(final.Targets) {
		t.Fatalf("projection=%+v ref=%+v", projection, ref)
	}
	return final, commit, pkg
}

// TestSharedAccountGivesOneGrant: three targets sharing an
// account give one grant and three bindings; the same account over Telnet
// gives a second grant; the package validates against the final plan and
// commit header with the audience; the canary harness finds no secret
// byte in any sink of the planner's package.
func TestSharedAccountGivesOneGrant(t *testing.T) {
	seed := canarytest.Seed(t)
	cfg := testConfig(t, `creds.backend-sequence=["fake"]`, "security.allow-telnet=true")
	backend := &fakeBackend{password: seed.Raw}
	devices := []inventory.Device{direct("10.0.0.1"), direct("10.0.0.2"), direct("10.0.0.3"), direct("10.0.0.4")}
	devices[3].Transport = "telnet"
	plan := planFor(t, cfg, devices, nil)
	p, err := NewCredentialPlanner(cfg, operator, devices, plantest.DraftedAt, CredentialOptions{Resolver: newResolver(t, cfg, backend)})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	if err := p.Resolve(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if p.Grants() != 2 || backend.calls != 4 {
		t.Fatalf("grants=%d calls=%d", p.Grants(), backend.calls)
	}
	final, _, pkg := finish(t, p, plan)
	ssh, telnet := pkg.Grants[0], pkg.Grants[1]
	if len(ssh.Scope.TargetIDs) != 3 || ssh.Scope.Transports[0] != "system" || ssh.Scope.Ports[0] != 22 || len(telnet.Scope.TargetIDs) != 1 || telnet.Scope.Transports[0] != "telnet" || telnet.Scope.Ports[0] != 23 || !ssh.Equivalent(telnet) {
		t.Fatalf("grants: %+v %+v", ssh.Scope, telnet.Scope)
	}
	if ssh.NotBefore != plantest.DraftedAt || ssh.NotAfter != plantest.DraftedAt.Add(credentialpackage.MaxGrantLifetime) || pkg.ExpiresAt != plantest.FinalizedAt.Add(credentialpackage.MaxPackageWindow) {
		t.Fatalf("windows: %v %v %v", ssh.NotBefore, ssh.NotAfter, pkg.ExpiresAt)
	}
	for _, x := range final.Targets {
		g, ok := pkg.GrantForTarget(x.TargetID)
		if !ok || g.CredentialID != x.CredentialBindingID {
			t.Fatalf("%s: grant %v bound %s", x.TargetID, ok, x.CredentialBindingID)
		}
	}
	canarytest.Exercise(t, pkg, canary.Refusing, seed)
	canarytest.Exercise(t, struct {
		P credentialpackage.CredentialPackage
	}{pkg}, canary.Refusing, seed)
	p.Destroy()
	if pkg.Grants[0].Password.IsSet() {
		t.Fatal("destroyed grant still reads")
	}
}

// TestDifferentMatchLinesGiveSeparateGrants: equal secrets under different
// match lines are different grants.
func TestDifferentMatchLinesGiveSeparateGrants(t *testing.T) {
	cfg := testConfig(t, `creds.backend-sequence=["fake"]`)
	backend := &fakeBackend{password: "same", match: func(d inventory.Device) credentials.Match {
		return credentials.Match{Category: "device", Pattern: d.CanonicalName[:strings.LastIndex(d.CanonicalName, ".")] + ".*", Source: "fake"}
	}}
	devices := []inventory.Device{direct("10.0.0.1"), direct("10.0.0.2"), direct("10.0.1.1")}
	plan := planFor(t, cfg, devices, nil)
	p, err := NewCredentialPlanner(cfg, operator, devices, plantest.DraftedAt, CredentialOptions{Resolver: newResolver(t, cfg, backend)})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	if err := p.Resolve(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	_, _, pkg := finish(t, p, plan)
	if len(pkg.Grants) != 2 || !pkg.Grants[0].Equivalent(pkg.Grants[1]) || pkg.Grants[0].MatchedOn == pkg.Grants[1].MatchedOn {
		t.Fatalf("grants=%d", len(pkg.Grants))
	}
}

// TestPromptOnceForThreeTargets: the built-in fallback asks once and every
// later target reuses the answer; the prompt names the count.
func TestPromptOnceForThreeTargets(t *testing.T) {
	cfg := testConfig(t, `creds.backend-sequence=[]`, "ssh.pubkey-authentication=false", "creds.interactive-prompt=true")
	input := &fakeInput{}
	devices := []inventory.Device{direct("10.0.0.1"), direct("10.0.0.2"), direct("10.0.0.3")}
	plan := planFor(t, cfg, devices, nil)
	p, err := NewCredentialPlanner(cfg, operator, devices, plantest.DraftedAt, CredentialOptions{Resolver: newResolver(t, cfg, nil), Input: input})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	if err := p.Resolve(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if len(input.prompts) != 1 || input.prompts[0] != "Password for 3 targets" {
		t.Fatalf("prompts=%v", input.prompts)
	}
	_, _, pkg := finish(t, p, plan)
	if len(pkg.Grants) != 1 || len(pkg.Grants[0].Scope.TargetIDs) != 3 || pkg.Grants[0].Backend != "interactive-tty" {
		t.Fatalf("grants=%+v", pkg.Grants[0].Scope)
	}
	// A single target is prompted by name, as today.
	one := &fakeInput{}
	p1, _ := NewCredentialPlanner(cfg, operator, devices[:1], plantest.DraftedAt, CredentialOptions{Resolver: newResolver(t, cfg, nil), Input: one})
	defer p1.Destroy()
	if err := p1.Resolve(context.Background(), planFor(t, cfg, devices[:1], nil)); err != nil || len(one.prompts) != 1 || one.prompts[0] != "Password for 10.0.0.1" {
		t.Fatalf("prompts=%v err=%v", one.prompts, err)
	}
}

// TestDaemonAuthorityBindsThePolicyOfTheDaemonAddress: an address-cidr rule
// sees the daemon-selected address after the evidence is incorporated, not
// the hint or the query name.
func TestDaemonAuthorityBindsThePolicyOfTheDaemonAddress(t *testing.T) {
	dir := t.TempDir()
	toml := "name.allow-daemon-resolution = true\ncreds.backend-sequence = [\"fake\"]\n[credential-backend.fake]\ntype = \"env\"\n[credential-policy.branch]\nbackend-sequence = [\"fake\"]\n[[credential-policy-map]]\npolicy = \"branch\"\naddress-cidr = \"198.51.100.0/24\"\n[[credential-policy-map]]\npolicy = \"default\"\nname = \"*\"\n"
	path := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{ExplicitRoots: []string{path}, HomeDir: dir, SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{password: "p"}
	devices := []inventory.Device{direct("10.0.0.1"), direct("branch.example")}
	overrides := map[string]string{"name:branch.example": "daemon"}
	draft := planFor(t, cfg, devices, overrides)
	p, err := NewCredentialPlanner(cfg, operator, devices, plantest.DraftedAt, CredentialOptions{Resolver: newResolver(t, cfg, backend)})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	// Phase one: the client-authority target only.
	if err := p.Resolve(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	if backend.calls != 1 || p.Grants() != 1 {
		t.Fatalf("phase one: calls=%d grants=%d", backend.calls, p.Grants())
	}
	if _, err := p.Bind(draft); errorcodes.Of(err) != "credential_resolution_failed" {
		t.Fatalf("binding before the daemon target resolved: %v", err)
	}
	lookup := func(_ context.Context, _, host string) ([]netip.Addr, error) { return mustAddrs("198.51.100.7"), nil }
	evidence, err := resolver.PrepareDaemonWith(context.Background(), cfg, draft.Targets, dual, lookup)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := executionplan.IncorporatePreparation(draft, executionplan.PreparationEvidence{ExecutionEndpoint: executionplan.EndpointLocal, PreparationID: plantest.PreparationID, PreparationDigest: executionplan.Sum([]byte("p")), PreparedAt: plantest.PreparedAt, Addresses: evidence})
	if err != nil {
		t.Fatal(err)
	}
	// Phase two: the daemon-authority target, with the daemon's address.
	if err := p.Resolve(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if backend.calls != 2 || p.Grants() != 2 {
		t.Fatalf("phase two: calls=%d grants=%d", backend.calls, p.Grants())
	}
	_, _, pkg := finish(t, p, prepared)
	g, _ := pkg.GrantForTarget("name:branch.example")
	if g.Policy != "branch" {
		t.Fatalf("daemon target bound policy %q, want branch (address 198.51.100.7)", g.Policy)
	}
	if g, _ := pkg.GrantForTarget("name:10.0.0.1"); g.Policy != "default" {
		t.Fatalf("client target bound policy %q", g.Policy)
	}
}

// TestCredentialFailureAbortsWithTheTargetList: any failure aborts and
// names the bounded target list.
func TestCredentialFailureAbortsWithTheTargetList(t *testing.T) {
	cfg := testConfig(t, `creds.backend-sequence=["fake"]`, "creds.interactive-prompt=false")
	backend := &fakeBackend{password: "p", missing: map[string]bool{"10.0.0.2": true}}
	devices := []inventory.Device{direct("10.0.0.1"), direct("10.0.0.2"), direct("10.0.0.3")}
	plan := planFor(t, cfg, devices, nil)
	p, err := NewCredentialPlanner(cfg, operator, devices, plantest.DraftedAt, CredentialOptions{Resolver: newResolver(t, cfg, backend), Input: &fakeInput{noEnv: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	err = p.Resolve(context.Background(), plan)
	if errorcodes.Of(err) != "credential_username_missing" || !strings.Contains(err.Error(), "1 of 3 targets failed credential resolution: name:10.0.0.2 (") {
		t.Fatalf("err=%v", err)
	}
	if p.Grants() != 0 {
		t.Fatalf("grants kept after an abort: %d", p.Grants())
	}
}

// TestBindingReportsTheGrantProjection: Binding
// names a bound target's credential ID with the grant's safe projection
// and reports false for a target Resolve has not bound.
func TestBindingReportsTheGrantProjection(t *testing.T) {
	seed := canarytest.Seed(t)
	cfg := testConfig(t, `creds.backend-sequence=["fake"]`)
	backend := &fakeBackend{password: seed.Raw}
	devices := []inventory.Device{direct("10.0.0.1"), direct("10.0.0.2")}
	plan := planFor(t, cfg, devices, nil)
	p, err := NewCredentialPlanner(cfg, operator, devices, plantest.DraftedAt, CredentialOptions{Resolver: newResolver(t, cfg, backend)})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	if _, _, ok := p.Binding(plan.Targets[0].TargetID); ok {
		t.Fatal("bound before Resolve")
	}
	if err := p.Resolve(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	id, grant, ok := p.Binding(plan.Targets[0].TargetID)
	if !ok || id == "" || grant.CredentialID != id || grant.DeviceUsername != "svc" || grant.Backend != "fake" || grant.MatchedOn.Category != "operator" {
		t.Fatalf("binding: ok=%t id=%q grant=%+v", ok, id, grant)
	}
	if id2, _, ok := p.Binding(plan.Targets[1].TargetID); !ok || id2 != id {
		t.Fatalf("the shared account gives one grant: %q %q", id, id2)
	}
	if _, _, ok := p.Binding("name:unbound"); ok {
		t.Fatal("an unknown target reports a binding")
	}
	if b, _ := json.Marshal(grant); canary.Found(b, seed) {
		t.Fatal("the projection carries the secret")
	}
}

// TestCloginrcLineSharedByDevicesGivesOneGrant (formerly three grants for
// three devices): .cloginrc evidence no
// longer echoes the device, so devices that take one password line and one
// user line share one grant. A device whose user line differs keeps its own
// grant, since the planner compares the secrets, the username among them.
func TestCloginrcLineSharedByDevicesGivesOneGrant(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, "cloginrc")
	body := "add user sw-bos-* {svc.bos}\nadd user * {svc.all}\nadd password sw-* {shared-pass}\n"
	if err := os.WriteFile(rc, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	me := operator
	me.UID, me.Home = os.Getuid(), home
	cfg := testConfig(t, `credential-backend.rancid.type="cloginrc"`, `credential-backend.rancid.path="`+rc+`"`, `creds.backend-sequence=["rancid"]`)
	devices := []inventory.Device{}
	for i, n := range []string{"sw-nyc-01", "sw-nyc-02", "sw-nyc-03", "sw-bos-01"} {
		d := inventory.Device{Name: n, Platform: "generic", Transport: "system", TransportExplicit: true, ManagementAddress: netip.MustParseAddr("10.0.0." + strconv.Itoa(i+1)), Source: inventory.SourceRef{Name: "inv", Path: "/tmp/inv.csv", Line: i + 2, Digest: strings.Repeat("ab", 32)}}
		if err := d.Validate(); err != nil {
			t.Fatal(err)
		}
		devices = append(devices, d)
	}
	plan := planFor(t, cfg, devices, nil)
	r, err := credentialbackend.New(cfg, me, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewCredentialPlanner(cfg, me, devices, plantest.DraftedAt, CredentialOptions{Resolver: r})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	if err := p.Resolve(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	_, _, pkg := finish(t, p, plan)
	if len(pkg.Grants) != 2 {
		t.Fatalf("grants=%d, want 2 (three devices on one line, one on its own user line)", len(pkg.Grants))
	}
	nyc, bos := pkg.Grants[0], pkg.Grants[1]
	name := func(g credentialpackage.CredentialGrant) string {
		var u string
		_ = g.Username.WithBytes(func(b []byte) error { u = string(b); return nil })
		return u
	}
	if len(nyc.Scope.TargetIDs) != 3 || len(bos.Scope.TargetIDs) != 1 || name(nyc) != "svc.all" || name(bos) != "svc.bos" {
		t.Fatalf("scopes: %+v %s, %+v %s", nyc.Scope.TargetIDs, name(nyc), bos.Scope.TargetIDs, name(bos))
	}
	// The password line is the evidence for both; neither names a device.
	if nyc.MatchedOn != bos.MatchedOn || nyc.MatchedOn.SafeValue != "" || nyc.MatchedOn.Pattern != "sw-*" || nyc.MatchedOn.Line != 3 {
		t.Fatalf("evidence: %+v %+v", nyc.MatchedOn, bos.MatchedOn)
	}
}

// TestCredentialCSVRowSharedByDevicesGivesOneGrant goes through the planner
// with the real csv backend: devices that take one row
// share one grant, whose evidence carries the row's credkey and survives the
// package's wire form; a device on another row keeps its own grant.
func TestCredentialCSVRowSharedByDevicesGivesOneGrant(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "credentials.csv")
	body := "device_name,credkey,username,password\nsw-bos-*,bos,svc.bos,bos-pass\nsw-*,,svc.all,shared-pass\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	me := operator
	me.UID, me.Home = os.Getuid(), home
	cfg := testConfig(t, `credential-backend.creds.type="csv"`, `credential-backend.creds.scope="user"`, `credential-backend.creds.path="`+file+`"`, `creds.backend-sequence=["creds"]`)
	devices := []inventory.Device{}
	for i, n := range []string{"sw-nyc-01", "sw-nyc-02", "sw-nyc-03", "sw-bos-01"} {
		d := inventory.Device{Name: n, Platform: "generic", Transport: "system", TransportExplicit: true, ManagementAddress: netip.MustParseAddr("10.0.0." + strconv.Itoa(i+1)), Source: inventory.SourceRef{Name: "inv", Path: "/tmp/inv.csv", Line: i + 2, Digest: strings.Repeat("ab", 32)}}
		if err := d.Validate(); err != nil {
			t.Fatal(err)
		}
		devices = append(devices, d)
	}
	plan := planFor(t, cfg, devices, nil)
	r, err := credentialbackend.New(cfg, me, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewCredentialPlanner(cfg, me, devices, plantest.DraftedAt, CredentialOptions{Resolver: r})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	if err := p.Resolve(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	_, _, pkg := finish(t, p, plan)
	if len(pkg.Grants) != 2 {
		t.Fatalf("grants=%d, want 2 (three devices on one row, one on its own)", len(pkg.Grants))
	}
	nyc, bos := pkg.Grants[0], pkg.Grants[1]
	if len(nyc.Scope.TargetIDs) != 3 || len(bos.Scope.TargetIDs) != 1 {
		t.Fatalf("scopes: %+v %+v", nyc.Scope.TargetIDs, bos.Scope.TargetIDs)
	}
	want := credentials.Match{Category: "csv_row", Pattern: "device_name=sw-*", Source: file, Line: 3, CredKey: "creds:3"}
	if nyc.MatchedOn != want {
		t.Fatalf("evidence %+v, want %+v", nyc.MatchedOn, want)
	}
	if bos.MatchedOn.CredKey != "bos" || bos.MatchedOn.Pattern != "device_name=sw-bos-* credkey=bos" || bos.MatchedOn.Line != 2 {
		t.Fatalf("evidence %+v", bos.MatchedOn)
	}
}
