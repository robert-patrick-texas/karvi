package resolver

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// The planning side of the resolver: the transform-only
// step the client runs under either authority, the one selection rule that
// whichever actor holds authority applies, and PrepareDaemon, the daemon's
// application of that rule at prepare_job. Resolve above keeps the
// execute-time path until the executor consumes execution targets.

// PreparePool bounds the concurrent lookups of PrepareDaemon.
const PreparePool = 16

// Transformed is the name-transform step alone: the name after the profile,
// the suffix action the trace shows, and the profile applied.
type Transformed struct {
	Name         string
	SuffixAction string
	Profile      string
}

// Transform applies the device's name-transform profile (or the configured
// default) to its name without touching DNS. It fills transformed_name and
// suffix_action for every target, whichever actor resolves it.
func Transform(cfg configload.Snapshot, d inventory.Device) (Transformed, error) {
	profiles, err := profiles(cfg)
	if err != nil {
		return Transformed{}, err
	}
	profile := d.NameTransform
	if profile == "" {
		profile = cfg.String("name.default-transform")
	}
	seq, ok := profiles[profile]
	if !ok {
		return Transformed{}, errorcodes.Errorf("name_transform_profile_unknown", "unknown name-transform profile %q", profile)
	}
	name, trace, err := seq.Apply(d.Name)
	if err != nil {
		return Transformed{}, err
	}
	out := Transformed{Name: name, SuffixAction: executionplan.SuffixActionNone, Profile: profile}
	for _, s := range trace {
		if s.Operation == "add-suffix" && s.Before != s.After {
			out.SuffixAction = executionplan.SuffixActionPrefix + strings.TrimPrefix(s.After, s.Before)
		}
	}
	return out, nil
}

// Selection is the outcome of the selection rule for one device.
type Selection struct {
	Transformed
	// QueryName is the DNS query, set only when a lookup ran.
	QueryName string
	// Family is the family of the selected address.
	Family string
	// Candidates are the literal management address then the alternates, or
	// the winning family's DNS answers ascending.
	Candidates []netip.Addr
	Selected   netip.Addr
	Alternates []netip.Addr
	// Source is "inventory" for a literal and "dns" for a lookup; the caller
	// maps "dns" to dns-client or dns-daemon.
	Source  string
	Notices []string
}

// Select applies the one selection rule: a literal
// wins, the command-line --address already having replaced the management
// address, then the inventory management address, then the inventory
// alternates in row order; any literal means no DNS. With no literal the
// winning family's candidates are ordered ascending, the first selected and
// the rest alternates. The client never contacts a device, so a literal is
// recorded as a fact whatever its family; the capabilities order DNS only.
func Select(ctx context.Context, cfg configload.Snapshot, d inventory.Device, caps Capabilities, lookup LookupFunc) (Selection, error) {
	tr, err := Transform(cfg, d)
	if err != nil {
		return Selection{}, err
	}
	sel := Selection{Transformed: tr, Candidates: []netip.Addr{}, Alternates: []netip.Addr{}}
	if d.ManagementAddress.IsValid() {
		addr := d.ManagementAddress.Unmap()
		sel.Selected = addr
		sel.Alternates = d.Alternates()
		sel.Candidates = append([]netip.Addr{addr}, sel.Alternates...)
		sel.Source = executionplan.SourceInventory
		sel.Family = family(addr)
		return sel, nil
	}
	if lookup == nil {
		lookup = net.DefaultResolver.LookupNetIP
	}
	order, notice, err := familyOrder(cfg.String("name.address-family-preference"), caps)
	if err != nil {
		return sel, err
	}
	if notice != "" {
		sel.Notices = append(sel.Notices, notice)
	}
	sel.QueryName = tr.Name
	fam, valid, err := queryDNS(ctx, tr.Name, order, dnsTimeout(cfg), lookup)
	if err != nil {
		return sel, err
	}
	sel.Family = fam
	sel.Candidates = valid
	sel.Selected = valid[0]
	sel.Alternates = append([]netip.Addr{}, valid[1:]...)
	sel.Source = "dns"
	return sel, nil
}

// TargetError is one target's resolution failure.
type TargetError struct {
	TargetID string
	Err      error
}

// TargetErrors aborts planning or preparation when any target fails:
// the code is the first failing target's, the
// message lists failing targets up to ten with the total.
type TargetErrors struct {
	Failures []TargetError
	Total    int
	// What names the operation, "address resolution" when empty; the
	// credential planner says "credential resolution".
	What string
}

// ListedFailures is how many failing targets the message names.
const ListedFailures = 10

func (e *TargetErrors) Error() string {
	parts := []string{}
	for i, f := range e.Failures {
		if i == ListedFailures {
			parts = append(parts, fmt.Sprintf("and %d more", len(e.Failures)-ListedFailures))
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", f.TargetID, f.Err.Error()))
	}
	what := e.What
	if what == "" {
		what = "address resolution"
	}
	return fmt.Sprintf("%s: %d of %d targets failed %s: %s", e.ErrorCode(), len(e.Failures), e.Total, what, strings.Join(parts, "; "))
}

// ErrorCode is the first failing target's registered code.
func (e *TargetErrors) ErrorCode() string {
	if len(e.Failures) == 0 {
		return "name_resolution_error"
	}
	code := errorcodes.Of(e.Failures[0].Err)
	if code == "" {
		code = "name_resolution_error"
	}
	return code
}

func (e *TargetErrors) Unwrap() error {
	if len(e.Failures) == 0 {
		return nil
	}
	return e.Failures[0].Err
}

// PrepareDaemon applies the selection rule for every daemon-authority target
// at prepare_job: a client hint present gives the
// first hint selected, the rest alternates, empty daemon_candidates, and
// source inventory; no hint gives daemon DNS with source dns-daemon. The
// resolver context is the target's execution endpoint and the resolution
// digest is SumResolution over the filled fields. Direct command mode shares
// it, the local process being the endpoint. Other targets are skipped.
func PrepareDaemon(ctx context.Context, cfg configload.Snapshot, targets []executionplan.ExecutionTarget) ([]executionplan.AddressEvidence, error) {
	return PrepareDaemonWith(ctx, cfg, targets, ProbeFamilies(), net.DefaultResolver.LookupNetIP)
}

// PrepareDaemonWith is PrepareDaemon with the socket capabilities and the
// lookup supplied, for tests. The family preference is the plan's; the DNS
// timeout is the daemon's configuration.
func PrepareDaemonWith(ctx context.Context, cfg configload.Snapshot, targets []executionplan.ExecutionTarget, caps Capabilities, lookup LookupFunc) ([]executionplan.AddressEvidence, error) {
	if lookup == nil {
		lookup = net.DefaultResolver.LookupNetIP
	}
	timeout := dnsTimeout(cfg)
	type slot struct {
		evidence executionplan.AddressEvidence
		err      error
		daemon   bool
	}
	slots := make([]slot, len(targets))
	var wg sync.WaitGroup
	pool := make(chan struct{}, PreparePool)
	for i := range targets {
		t := targets[i]
		if t.AddressPlan.Authority != executionplan.AddressByDaemon {
			continue
		}
		slots[i].daemon = true
		wg.Add(1)
		pool <- struct{}{}
		go func(i int, t executionplan.ExecutionTarget) {
			defer wg.Done()
			defer func() { <-pool }()
			slots[i].evidence, slots[i].err = prepareTarget(ctx, t, timeout, caps, lookup)
		}(i, t)
	}
	wg.Wait()
	out := []executionplan.AddressEvidence{}
	failures := &TargetErrors{}
	for i := range slots {
		if !slots[i].daemon {
			continue
		}
		failures.Total++
		if slots[i].err != nil {
			failures.Failures = append(failures.Failures, TargetError{TargetID: targets[i].TargetID, Err: slots[i].err})
			continue
		}
		out = append(out, slots[i].evidence)
	}
	if len(failures.Failures) > 0 {
		return nil, failures
	}
	return out, nil
}

func prepareTarget(ctx context.Context, t executionplan.ExecutionTarget, timeout time.Duration, caps Capabilities, lookup LookupFunc) (executionplan.AddressEvidence, error) {
	filled := t
	a := &filled.AddressPlan
	if hints := t.AddressPlan.ClientCandidates; len(hints) > 0 {
		a.DaemonCandidates = []netip.Addr{}
		a.Selected = hints[0]
		a.Alternates = append([]netip.Addr{}, hints[1:]...)
		a.SelectedSource = executionplan.SourceInventory
	} else {
		order, _, err := familyOrder(string(t.AddressPlan.FamilyPreference), caps)
		if err != nil {
			return executionplan.AddressEvidence{}, err
		}
		_, valid, err := queryDNS(ctx, t.AddressPlan.QueryName, order, timeout, lookup)
		if err != nil {
			return executionplan.AddressEvidence{}, err
		}
		a.DaemonCandidates = valid
		a.Selected = valid[0]
		a.Alternates = append([]netip.Addr{}, valid[1:]...)
		a.SelectedSource = executionplan.SourceDNSDaemon
	}
	a.ResolverContext = t.ExecutionEndpoint
	digest, err := executionplan.SumResolution(filled)
	if err != nil {
		return executionplan.AddressEvidence{}, err
	}
	return executionplan.AddressEvidence{
		TargetID: t.TargetID, DaemonCandidates: a.DaemonCandidates, Selected: a.Selected, Alternates: a.Alternates,
		SelectedSource: a.SelectedSource, ResolverContext: a.ResolverContext, ResolutionDigest: digest,
	}, nil
}
