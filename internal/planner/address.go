package planner

import (
	"context"
	"fmt"
	"net/netip"
	"sync"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// PlanningPool bounds the client's concurrent lookups.
const PlanningPool = 16

// AddressOptions are the command-line and test inputs of the address plan.
type AddressOptions struct {
	// Overrides maps a device ID to the authority the command line gave it
	// (command and login: the first device; run: each TARGET=... value).
	Overrides map[string]string
	// Capabilities orders DNS families; nil probes the host.
	Capabilities *resolver.Capabilities
	// Lookup is the DNS lookup; nil is the system resolver.
	Lookup resolver.LookupFunc
	// Warn receives planning warnings, such as the family fallback.
	Warn func(string)
}

// EffectiveAuthority is the four-level precedence of the address authority:
// the command-line override, then the device's inventory field, which the
// loader already filled from the source's defaults.address-authority, then
// name.default-address-authority.
func EffectiveAuthority(cfg configload.Snapshot, d inventory.Device, override string) executionplan.AddressAuthority {
	for _, v := range []string{override, d.AddressAuthority, cfg.String("name.default-address-authority")} {
		if v != "" {
			return executionplan.AddressAuthority(v)
		}
	}
	return executionplan.AddressByClient
}

// GateDaemonResolution refuses any daemon-authority target while
// name.allow-daemon-resolution is false, naming the
// first such target and the count.
func GateDaemonResolution(cfg configload.Snapshot, devices []inventory.Device, overrides map[string]string) error {
	if cfg.Bool("name.allow-daemon-resolution") {
		return nil
	}
	first, count := "", 0
	for _, d := range devices {
		if EffectiveAuthority(cfg, d, overrides[d.ID]) == executionplan.AddressByDaemon {
			if first == "" {
				first = d.CanonicalName
			}
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return errorcodes.Errorf("daemon_resolution_not_allowed", "%d target(s) have daemon address authority, first %s, while name.allow-daemon-resolution is false", count, first)
}

// PlanAddresses builds one address plan per device in the set's order.
// Every target's authority is fixed first and
// the gate applied; daemon-authority targets get the transform fields, their
// literals as client hints, and no lookup; client-authority targets run the
// selection rule in a bounded pool with results placed back in order. Any
// failure aborts with the first failing target's code and the bounded list.
func PlanAddresses(ctx context.Context, cfg configload.Snapshot, devices []inventory.Device, opts AddressOptions) ([]executionplan.AddressPlan, error) {
	if err := GateDaemonResolution(cfg, devices, opts.Overrides); err != nil {
		return nil, err
	}
	caps := resolver.ProbeFamilies()
	if opts.Capabilities != nil {
		caps = *opts.Capabilities
	}
	pref := executionplan.AddressFamily(cfg.String("name.address-family-preference"))
	plans := make([]executionplan.AddressPlan, len(devices))
	errs := make([]error, len(devices))
	notices := make([][]string, len(devices))
	var wg sync.WaitGroup
	pool := make(chan struct{}, PlanningPool)
	for i := range devices {
		d := devices[i]
		authority := EffectiveAuthority(cfg, d, opts.Overrides[d.ID])
		if authority == executionplan.AddressByDaemon {
			tr, err := resolver.Transform(cfg, d)
			if err != nil {
				return nil, err
			}
			plans[i] = daemonDraft(d, tr, pref)
			continue
		}
		wg.Add(1)
		pool <- struct{}{}
		go func(i int, d inventory.Device) {
			defer wg.Done()
			defer func() { <-pool }()
			sel, err := resolver.Select(ctx, cfg, d, caps, opts.Lookup)
			if err != nil {
				errs[i] = err
				return
			}
			plans[i] = clientPlan(sel, pref)
			notices[i] = sel.Notices
		}(i, d)
	}
	wg.Wait()
	failures := &resolver.TargetErrors{Total: len(devices)}
	for i, err := range errs {
		if err == nil {
			continue
		}
		// A resolution failure is one target's; anything else (an unknown
		// transform profile) is a configuration error and aborts as itself.
		if _, resolution := err.(*resolver.Error); !resolution {
			return nil, err
		}
		failures.Failures = append(failures.Failures, resolver.TargetError{TargetID: devices[i].ID, Err: err})
	}
	if len(failures.Failures) > 0 {
		return nil, failures
	}
	if opts.Warn != nil {
		for i := range devices {
			for _, n := range notices[i] {
				opts.Warn(fmt.Sprintf("resolution_notice: %s: %s", devices[i].CanonicalName, n))
			}
		}
	}
	return plans, nil
}

// daemonDraft is the client's half of a daemon-authority target (decision
// 3.9): transform fields filled, the literals copied as hints, every daemon
// field empty, and query_name always set because the daemon may need it.
func daemonDraft(d inventory.Device, tr resolver.Transformed, pref executionplan.AddressFamily) executionplan.AddressPlan {
	hints := []netip.Addr{}
	if d.ManagementAddress.IsValid() {
		hints = append([]netip.Addr{d.ManagementAddress.Unmap()}, d.Alternates()...)
	}
	return executionplan.AddressPlan{
		Authority: executionplan.AddressByDaemon, FamilyPreference: pref,
		TransformedName: tr.Name, QueryName: tr.Name, SuffixAction: tr.SuffixAction,
		ClientCandidates: hints, DaemonCandidates: []netip.Addr{}, Alternates: []netip.Addr{},
	}
}

// clientPlan maps a selection to the plan.
func clientPlan(sel resolver.Selection, pref executionplan.AddressFamily) executionplan.AddressPlan {
	source := executionplan.SourceInventory
	if sel.Source == "dns" {
		source = executionplan.SourceDNSClient
	}
	return executionplan.AddressPlan{
		Authority: executionplan.AddressByClient, FamilyPreference: pref,
		TransformedName: sel.Name, QueryName: sel.QueryName, SuffixAction: sel.SuffixAction,
		ClientCandidates: sel.Candidates, DaemonCandidates: []netip.Addr{},
		Selected: sel.Selected, Alternates: sel.Alternates, SelectedSource: source,
		ResolverContext: executionplan.ResolverContextClient,
	}
}
