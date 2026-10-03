package planner

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// CredentialOptions configure the credential planner.
type CredentialOptions struct {
	// Resolver replaces the configured backend resolver, for tests.
	Resolver *credentialbackend.Resolver
	// Input is the operator's environment and terminal; nil is TTYInput.
	Input credentialbackend.InputProvider
	// Warn receives backend warnings.
	Warn func(string)
	// NewID replaces the identifier generator, for golden tests.
	NewID func(time.Time) (string, error)
}

// CredentialPlanner resolves one credential per target after its address is
// known, deduplicates equivalent credentials into grants, and assembles the
// package. Resolve may run twice, before prepare_job for client-authority
// targets and after the evidence is incorporated for daemon-authority ones;
// each call handles the targets that have a selected address and no grant
// yet. At the same step it selects each target's session-init profile,
// and Bind writes both into the plan.
type CredentialPlanner struct {
	cfg       configload.Snapshot
	operator  credentials.Operator
	resolver  *credentialbackend.Resolver
	devices   map[string]inventory.Device
	draftedAt time.Time
	newID     func(time.Time) (string, error)
	prompts   *promptOnce
	sessions  *sessionInitSelector
	// setPlatform is the platform a device is matched on (SetPlatformFunc):
	// the maps see blank for a not-set or
	// fallen-back device.
	setPlatform func(inventory.Device) string

	mu       sync.Mutex
	grants   []credentialpackage.CredentialGrant
	bindings map[string]string // target ID -> credential ID
	profiles map[string]string // target ID -> session-init profile name
}

// NewCredentialPlanner prepares the planner for the set's devices. The
// grant window opens at draftedAt.
func NewCredentialPlanner(cfg configload.Snapshot, operator credentials.Operator, devices []inventory.Device, draftedAt time.Time, opts CredentialOptions) (*CredentialPlanner, error) {
	r := opts.Resolver
	if r == nil {
		var err error
		if r, err = credentialbackend.New(cfg, operator, opts.Warn); err != nil {
			return nil, errorcodes.Ensure(err, "credential_resolution_failed")
		}
	}
	var inner credentialbackend.InputProvider = credentialbackend.TTYInput{}
	if opts.Input != nil {
		inner = opts.Input
	}
	prompts := &promptOnce{inner: inner, targets: len(devices), answers: map[string]string{}, failed: map[string]error{}}
	r.SetInput(prompts)
	newID := opts.NewID
	if newID == nil {
		newID = osutil.NewID
	}
	sessions, err := newSessionInitSelector(cfg)
	if err != nil {
		return nil, err
	}
	p := &CredentialPlanner{cfg: cfg, operator: operator, resolver: r, devices: map[string]inventory.Device{}, draftedAt: draftedAt, newID: newID, prompts: prompts, sessions: sessions, setPlatform: SetPlatformFunc(cfg), bindings: map[string]string{}, profiles: map[string]string{}}
	for _, d := range devices {
		p.devices[d.ID] = d
	}
	return p, nil
}

// promptOnce asks for each field once per invocation and reuses the answer
// for every later target; prompts are serialized. A prompt that failed
// (Ctrl-C, Ctrl-D, no terminal) fails every later target's ask of the
// field the same way, so the targets resolving beside the first are not
// asked again after the operator has stopped.
type promptOnce struct {
	inner   credentialbackend.InputProvider
	targets int
	mu      sync.Mutex
	answers map[string]string
	failed  map[string]error
}

func (p *promptOnce) LookupEnv(ctx context.Context, name string) (string, bool, error) {
	return p.inner.LookupEnv(ctx, name)
}

func (p *promptOnce) Prompt(ctx context.Context, req credentialbackend.PromptRequest) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := p.answers[req.Field]; ok {
		return v, nil
	}
	if err, ok := p.failed[req.Field]; ok {
		return "", err
	}
	if p.targets > 1 {
		req.Target = fmt.Sprintf("%d targets", p.targets)
	}
	v, err := p.inner.Prompt(ctx, req)
	if err != nil {
		p.failed[req.Field] = err
		return "", err
	}
	p.answers[req.Field] = v
	return v, nil
}

// Resolve binds every target of plan that has a selected address and no
// grant yet. It first selects each such target's
// session-init profile, so a failing map fails the plan before any backend
// asks for a secret. Backend calls run in a bounded pool with results placed
// by index; any failure aborts with the resolver's code and the bounded
// target list, after destroying what was resolved in this
// call.
func (p *CredentialPlanner) Resolve(ctx context.Context, plan executionplan.ExecutionPlan) error {
	type slot struct {
		resolved credentials.Resolved
		err      error
		active   bool
		profile  string
	}
	slots := make([]slot, len(plan.Targets))
	selection := &resolver.TargetErrors{What: "session-init selection"}
	for i := range plan.Targets {
		t := plan.Targets[i]
		if !t.AddressPlan.Selected.IsValid() {
			continue
		}
		p.mu.Lock()
		_, bound := p.bindings[t.TargetID]
		p.mu.Unlock()
		if bound {
			continue
		}
		slots[i].active = true
		selection.Total++
		profile, err := p.sessions.selectProfile(p.deviceView(t))
		if err != nil {
			selection.Failures = append(selection.Failures, resolver.TargetError{TargetID: t.TargetID, Err: err})
		}
		slots[i].profile = profile
	}
	if len(selection.Failures) > 0 {
		return selection
	}
	var wg sync.WaitGroup
	pool := make(chan struct{}, PlanningPool)
	for i := range plan.Targets {
		t := plan.Targets[i]
		if !slots[i].active {
			continue
		}
		wg.Add(1)
		pool <- struct{}{}
		go func(i int, t executionplan.ExecutionTarget) {
			defer wg.Done()
			defer func() { <-pool }()
			slots[i].resolved, slots[i].err = p.resolver.Resolve(ctx, p.operator, p.deviceView(t))
		}(i, t)
	}
	wg.Wait()
	failures := &resolver.TargetErrors{What: "credential resolution"}
	for i := range slots {
		if !slots[i].active {
			continue
		}
		failures.Total++
		if slots[i].err != nil {
			failures.Failures = append(failures.Failures, resolver.TargetError{TargetID: plan.Targets[i].TargetID, Err: slots[i].err})
		}
	}
	if len(failures.Failures) > 0 {
		for i := range slots {
			if slots[i].active && slots[i].err == nil && slots[i].resolved.Credential.Material != nil {
				slots[i].resolved.Credential.Material.Destroy()
			}
		}
		return failures
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range slots {
		if !slots[i].active {
			continue
		}
		t := plan.Targets[i]
		if err := p.bind(t, slots[i].resolved); err != nil {
			return err
		}
		p.profiles[t.TargetID] = slots[i].profile
	}
	return nil
}

// deviceView is the inventory device with the plan's authoritative address,
// transport kind, and effective port, so address-cidr rules see the
// selected address and the password rule sees the kind.
func (p *CredentialPlanner) deviceView(t executionplan.ExecutionTarget) inventory.Device {
	d, ok := p.devices[t.TargetID]
	if !ok {
		d = inventory.Direct(t.InputTarget, t.Device.Platform, t.Device.Transport, t.Device.Port)
		d.ID, d.Name, d.CanonicalName, d.Site, d.Groups = t.Device.ID, t.Device.Name, t.Device.CanonicalName, t.Device.Site, t.Device.Groups
	}
	d.ManagementAddress = t.AddressPlan.Selected
	d.Addresses = []inventory.Address{{Address: t.AddressPlan.Selected, Role: inventory.RoleManagement, Source: t.AddressPlan.SelectedSource}}
	d.Transport, d.TransportExplicit, d.Port = t.Device.Transport, true, t.Device.Port
	// The maps match the set platform (blank for not set or fallen back);
	// the enable rule reads the platform used, the plan's.
	d.Platform = p.setPlatform(d)
	d.PlatformUsed = t.Device.Platform
	return d
}

// bind folds a resolved credential into the grants: one grant per distinct
// key, the secret equivalence plus policy, backend, match, transport kind,
// and effective port. The caller holds the mutex.
func (p *CredentialPlanner) bind(t executionplan.ExecutionTarget, r credentials.Resolved) error {
	defer func() {
		if r.Credential.Material != nil {
			r.Credential.Material.Destroy()
		}
	}()
	candidate := credentialpackage.CredentialGrant{
		Method: credentialpackage.MethodEmbeddedSecret,
		Policy: r.Credential.Policy, Backend: r.Credential.Backend, MatchedOn: r.Credential.MatchedOn,
		Scope:     credentialpackage.CredentialScope{TargetIDs: []string{t.TargetID}, Transports: []string{t.Device.Transport}, Ports: []uint16{t.Device.Port}},
		NotBefore: p.draftedAt, NotAfter: p.draftedAt.Add(credentialpackage.MaxGrantLifetime),
	}
	m := r.Credential.Material
	if m == nil {
		return errorcodes.Errorf("credential_material_missing", "target %s resolved without material", t.TargetID)
	}
	var err error
	if m.UsernameSet() {
		err = m.WithUsername(func(b []byte) error { candidate.Username = credentials.NewSecretStringFromBytes(b); return nil })
	}
	if err == nil && m.PasswordSet() {
		err = m.WithPassword(func(b []byte) error { candidate.Password = credentials.NewSecretStringFromBytes(b); return nil })
	}
	if err == nil && m.EnablePasswordSet() {
		err = m.WithEnablePassword(func(b []byte) error { candidate.EnablePassword = credentials.NewSecretStringFromBytes(b); return nil })
	}
	if err != nil {
		candidate.Destroy()
		return errorcodes.Ensure(err, "credential_material_error")
	}
	for i := range p.grants {
		g := &p.grants[i]
		if g.Policy == candidate.Policy && g.Backend == candidate.Backend && g.MatchedOn == candidate.MatchedOn &&
			g.Scope.Transports[0] == t.Device.Transport && g.Scope.Ports[0] == t.Device.Port && g.Equivalent(candidate) {
			candidate.Destroy()
			g.Scope.TargetIDs = append(g.Scope.TargetIDs, t.TargetID)
			sort.Strings(g.Scope.TargetIDs)
			p.bindings[t.TargetID] = g.CredentialID
			return nil
		}
	}
	id, err := p.newID(time.Now())
	if err != nil {
		candidate.Destroy()
		return errorcodes.Ensure(err, "activity_id_generation_failed")
	}
	candidate.CredentialID = id
	if err := candidate.Validate(); err != nil {
		candidate.Destroy()
		return err
	}
	p.grants = append(p.grants, candidate)
	p.bindings[t.TargetID] = id
	return nil
}

// Bind writes each target's credential_binding_id and session_init_profile
// into a copy of plan (bound before commit) and the session-init
// table of the selected profiles; a target without a grant is
// credential_resolution_failed, since Finalize would otherwise commit an
// unbound target.
func (p *CredentialPlanner) Bind(plan executionplan.ExecutionPlan) (executionplan.ExecutionPlan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := plan
	out.Targets = append([]executionplan.ExecutionTarget(nil), plan.Targets...)
	names := []string{}
	for i := range out.Targets {
		id, ok := p.bindings[out.Targets[i].TargetID]
		if !ok {
			return plan, errorcodes.Errorf("credential_resolution_failed", "target %s has no credential grant", out.Targets[i].TargetID)
		}
		profile, ok := p.profiles[out.Targets[i].TargetID]
		if !ok {
			return plan, errorcodes.Errorf("credential_resolution_failed", "target %s has no session-init selection", out.Targets[i].TargetID)
		}
		out.Targets[i].CredentialBindingID = id
		out.Targets[i].SessionInitProfile = profile
		names = append(names, profile)
	}
	out.SessionInit = p.sessions.table(names)
	return out, nil
}

// Package assembles the credential package over the final plan and commit
// header: a fresh package ID, the job ID and final plan
// digest from the header, the operator as issuer, the audience prepare
// returned, local-peer protection, the grants, one binding per target in
// plan order, and a ten-minute window from now. It validates the package
// against the plan and header with that audience before returning it, so a
// malformed package is caught before the frame.
func (p *CredentialPlanner) Package(plan executionplan.ExecutionPlan, header executionplan.PublicJobHeader, audience, hostname string, now time.Time) (credentialpackage.CredentialPackage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id, err := p.newID(now)
	if err != nil {
		return credentialpackage.CredentialPackage{}, errorcodes.Ensure(err, "activity_id_generation_failed")
	}
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	pkg := credentialpackage.CredentialPackage{
		SchemaVersion: credentialpackage.SchemaVersion, PackageID: id, JobID: header.JobID, PlanDigest: header.PlanDigest,
		Issuer:   credentialpackage.Principal{Kind: credentialpackage.IssuerOperator, Username: p.operator.Username, UID: p.operator.UID, Hostname: hostname},
		Audience: []string{audience}, Protection: credentialpackage.ProtectionLocalPeer,
		IssuedAt: now, ExpiresAt: now.Add(credentialpackage.MaxPackageWindow),
		Grants:   append([]credentialpackage.CredentialGrant(nil), p.grants...),
		Bindings: make([]credentialpackage.TargetCredentialBinding, 0, len(plan.Targets)),
	}
	for _, t := range plan.Targets {
		pkg.Bindings = append(pkg.Bindings, credentialpackage.TargetCredentialBinding{TargetID: t.TargetID, CredentialID: p.bindings[t.TargetID]})
	}
	if err := pkg.Validate(&plan, header, audience, now); err != nil {
		return credentialpackage.CredentialPackage{}, err
	}
	return pkg, nil
}

// Binding reports the credential a target is bound to and the grant's safe
// projection, so a report's provenance never touches
// a secret field. ok is false for a target Resolve has not bound.
func (p *CredentialPlanner) Binding(targetID string) (credentialID string, grant credentialpackage.GrantProjection, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id, bound := p.bindings[targetID]
	if !bound {
		return "", credentialpackage.GrantProjection{}, false
	}
	for _, g := range p.grants {
		if g.CredentialID != id {
			continue
		}
		proj, err := g.SafeProjection()
		if err != nil {
			return id, credentialpackage.GrantProjection{}, false
		}
		return id, proj, true
	}
	return id, credentialpackage.GrantProjection{}, false
}

// SessionInitProfile reports the session-init profile Resolve selected for a
// target, the name or none; ok is false for a target Resolve has not bound
// (an inspection shows it as deferred).
func (p *CredentialPlanner) SessionInitProfile(targetID string) (profile string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	profile, ok = p.profiles[targetID]
	return profile, ok
}

// Grants reports how many grants exist, for diagnostics and tests.
func (p *CredentialPlanner) Grants() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.grants)
}

// Destroy wipes every grant's secrets.
func (p *CredentialPlanner) Destroy() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, g := range p.grants {
		g.Destroy()
	}
}

// PackageReference is the header's pointer to the package: its safe
// projection's digest under local-peer protection.
func PackageReference(pkg credentialpackage.CredentialPackage) (executionplan.PackageReference, credentialpackage.SafePackageProjection, error) {
	projection, err := pkg.SafeProjection()
	if err != nil {
		return executionplan.PackageReference{}, projection, err
	}
	digest, err := projection.Sum()
	if err != nil {
		return executionplan.PackageReference{}, projection, err
	}
	projection.PackageDigest = digest
	return executionplan.PackageReference{Protection: executionplan.ProtectionLocalPeer, Digest: digest}, projection, nil
}
