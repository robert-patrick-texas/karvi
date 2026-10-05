package executionplan

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/robert-patrick-texas/karvi/inventory"
)

// AddressAuthority names who selects a target's address.
type AddressAuthority string

const (
	AddressByClient AddressAuthority = "client"
	AddressByDaemon AddressAuthority = "daemon"
)

// AddressFamily is the preferred IP family of an address plan.
type AddressFamily string

const (
	FamilyIPv4 AddressFamily = "ipv4"
	FamilyIPv6 AddressFamily = "ipv6"
)

// Selected-address sources (Address.source).
const (
	SourceInventory = "inventory"
	SourceDNSClient = "dns-client"
	SourceDNSDaemon = "dns-daemon"
)

// EndpointLocal is the only execution endpoint in v1: the same-UID daemon,
// the value the executor records as server_id.
const EndpointLocal = "local"

// ResolverContextClient marks an address plan resolved by the client.
const ResolverContextClient = "client"

// SuffixActionPrefix begins every suffix_action value:
// "add-suffix:<suffix>" or "add-suffix:none".
const SuffixActionPrefix = "add-suffix:"

// SuffixActionNone is the suffix_action of a literal address or an
// unchanged name.
const SuffixActionNone = SuffixActionPrefix + "none"

// Stage selects which fields Validate requires.
type Stage int

const (
	// Draft is the client's plan before prepare_job and credential binding.
	Draft Stage = iota
	// Committed is the final immutable plan at commit_job.
	Committed
)

// DeviceProjection is inventory.Device without the address facts, which live
// in AddressPlan, and without its schema version, which the plan carries.
type DeviceProjection struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CanonicalName string `json:"canonical_name"`
	Platform      string `json:"platform"`
	Transport     string `json:"transport"`
	Port          uint16 `json:"port,omitempty"`
	// TransportSelector is the slot the planner resolved Transport (the
	// selection kind) from, so the daemon opens the same configured slot.
	TransportSelector string              `json:"transport_selector,omitempty"`
	Site              string              `json:"site,omitempty"`
	Groups            []string            `json:"groups"`
	NameTransform     string              `json:"name_transform"`
	SessionCap        *int                `json:"session_cap,omitempty"`
	RiskTier          string              `json:"risk_tier,omitempty"`
	DeploymentRing    string              `json:"deployment_ring,omitempty"`
	TopologyDomain    string              `json:"topology_domain,omitempty"`
	Attributes        map[string]string   `json:"attributes"`
	Source            inventory.SourceRef `json:"source"`
}

// AddressPlan is the per-target address contract.
type AddressPlan struct {
	Authority        AddressAuthority `json:"authority"`
	FamilyPreference AddressFamily    `json:"family_preference"`
	TransformedName  string           `json:"transformed_name"`
	QueryName        string           `json:"query_name,omitempty"`
	SuffixAction     string           `json:"suffix_action"`
	ClientCandidates []netip.Addr     `json:"client_candidates"`
	DaemonCandidates []netip.Addr     `json:"daemon_candidates"`
	Selected         netip.Addr       `json:"selected,omitzero"`
	Alternates       []netip.Addr     `json:"alternates"`
	SelectedSource   string           `json:"selected_source,omitempty"`
	ResolverContext  string           `json:"resolver_context,omitempty"`
	ResolutionDigest Digest           `json:"resolution_digest,omitzero"`
}

// ExecutionTarget is one immutable target of an execution plan.
type ExecutionTarget struct {
	TargetID            string           `json:"target_id"`
	InputTarget         string           `json:"input_target"`
	Device              DeviceProjection `json:"device"`
	AddressPlan         AddressPlan      `json:"address_plan"`
	CredentialBindingID string           `json:"credential_binding_id,omitempty"`
	SessionInitProfile  string           `json:"session_init_profile,omitempty"`
	// Channel is what the session asks of the SSH session channel for this
	// target, resolved from its platform at planning (schema 11): ChannelShell
	// or ChannelExec. The daemon evaluates no platform: it reads this word,
	// and refuses exec over telnet, which has no exec.
	Channel           string `json:"channel"`
	ExecutionEndpoint string `json:"execution_endpoint"`
	SourceDigest      Digest `json:"source_digest"`
	// Notices are the planning notices the daemon writes on the device's
	// first record: present only when nonempty, so a plan without one is
	// unchanged; part of the target's source digest, since the client
	// decided them. Execution plan 3 carries them without a bump, as it
	// did for blind and expectations.
	Notices []TargetNotice `json:"notices,omitempty"`
}

// The words of a target's channel, the platform package's, repeated here
// since this package imports nothing of karvi's platforms; and the
// transport kind that has no exec.
const (
	ChannelShell    = "shell"
	ChannelExec     = "exec"
	TransportTelnet = "telnet"
	TransportNative = "native"
)

// TargetNotice is a planning notice about one target that the plan carries
// to the daemon for the device's first record: the code, the operator
// message, and string details.
// The planner's platform resolution emits platform_not_set and
// platform_unknown_fallback; the target's notices carry them.
type TargetNotice struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// ProjectDevice builds the safe projection of an inventory device. Slices
// and maps are never nil so the JSON form is "[]" and "{}", never null.
func ProjectDevice(d inventory.Device) DeviceProjection {
	p := DeviceProjection{
		ID: d.ID, Name: d.Name, CanonicalName: d.CanonicalName, Platform: d.Platform,
		Transport: d.Transport, Port: d.Port, Site: d.Site,
		Groups: append([]string{}, d.Groups...), NameTransform: d.NameTransform,
		RiskTier: d.RiskTier, DeploymentRing: d.DeploymentRing, TopologyDomain: d.TopologyDomain,
		Attributes: map[string]string{}, Source: d.Source,
	}
	if d.SessionCap != nil {
		cap := *d.SessionCap
		p.SessionCap = &cap
	}
	for k, v := range d.Attributes {
		p.Attributes[k] = v
	}
	return p
}

// SumTarget returns the digest of a target's client-authoritative content:
// the JSON encoding with source_digest (its own output), credential_binding_id,
// session_init_profile, and resolution_digest cleared. It is the target's
// source_digest and
// the per-target part of the plan digest.
func SumTarget(t ExecutionTarget) (Digest, error) {
	t.SourceDigest = Digest{}
	t.CredentialBindingID = ""
	t.SessionInitProfile = ""
	t.AddressPlan.ResolutionDigest = Digest{}
	return SumJSON(t)
}

// resolutionFields are the address fields prepare_job may fill for a
// daemon-authority target; nothing else enters the digest.
type resolutionFields struct {
	TargetID         string       `json:"target_id"`
	DaemonCandidates []netip.Addr `json:"daemon_candidates"`
	Selected         netip.Addr   `json:"selected,omitzero"`
	Alternates       []netip.Addr `json:"alternates"`
	SelectedSource   string       `json:"selected_source,omitempty"`
	ResolverContext  string       `json:"resolver_context"`
}

// SumResolution returns the digest of the daemon-filled address fields.
func SumResolution(t ExecutionTarget) (Digest, error) {
	a := t.AddressPlan
	return SumJSON(resolutionFields{TargetID: t.TargetID, DaemonCandidates: nonNil(a.DaemonCandidates), Selected: a.Selected, Alternates: nonNil(a.Alternates), SelectedSource: a.SelectedSource, ResolverContext: a.ResolverContext})
}

func nonNil(a []netip.Addr) []netip.Addr {
	if a == nil {
		return []netip.Addr{}
	}
	return a
}

// invalid builds the coded contract error. The message prefix is the
// registered code, which errorcodes.Of reads, so this package needs no
// import from internal/.
func invalid(field, format string, args ...any) error {
	return fmt.Errorf("execution_target_invalid: %s: %s", field, fmt.Sprintf(format, args...))
}

// Validate checks the target against the address contract at the given stage.
// Every error carries execution_target_invalid and names the field first.
func (t *ExecutionTarget) Validate(stage Stage) error {
	if err := identifier("target_id", t.TargetID); err != nil {
		return err
	}
	if t.InputTarget == "" {
		return invalid("input_target", "is required")
	}
	if err := t.Device.validate(); err != nil {
		return err
	}
	if t.TargetID != t.Device.ID {
		return invalid("target_id", "%q must equal device.id %q", t.TargetID, t.Device.ID)
	}
	for i, n := range t.Notices {
		field := fmt.Sprintf("notices[%d]", i)
		if err := identifier(field+".code", n.Code); err != nil {
			return err
		}
		if strings.TrimSpace(n.Message) == "" {
			return invalid(field+".message", "is required")
		}
		for k := range n.Details {
			if SensitivePropertyPattern.MatchString(k) {
				return invalid(field+".details", "key %q names sensitive content", k)
			}
		}
	}
	switch t.Channel {
	case ChannelShell:
	case ChannelExec:
		if t.Device.Transport == TransportTelnet {
			return invalid("channel", "exec over telnet: telnet has no exec channel")
		}
		// Until the exec channel is built on the system transport no
		// session there runs one, so a plan that asks for it is refused here
		// as the client refuses it.
		if t.Device.Transport != TransportNative {
			return invalid("channel", "exec channels are not built on the %s transport", t.Device.Transport)
		}
	default:
		return invalid("channel", "%q is neither %q nor %q", t.Channel, ChannelShell, ChannelExec)
	}
	if t.ExecutionEndpoint != EndpointLocal {
		return invalid("execution_endpoint", "%q is not supported; v1 accepts %q", t.ExecutionEndpoint, EndpointLocal)
	}
	if t.SourceDigest.IsZero() {
		return invalid("source_digest", "is required")
	}
	if t.CredentialBindingID != "" {
		if err := identifier("credential_binding_id", t.CredentialBindingID); err != nil {
			return err
		}
	}
	if err := t.AddressPlan.validate(stage, t.ExecutionEndpoint); err != nil {
		return err
	}
	if stage == Committed && t.CredentialBindingID == "" {
		return invalid("credential_binding_id", "is required before commit")
	}
	if stage == Committed && t.SessionInitProfile == "" {
		return invalid("session_init_profile", "is required before commit")
	}
	return nil
}

func (d *DeviceProjection) validate() error {
	if err := identifier("device.id", d.ID); err != nil {
		return err
	}
	for field, v := range map[string]string{"device.name": d.Name, "device.canonical_name": d.CanonicalName, "device.platform": d.Platform, "device.transport": d.Transport, "device.name_transform": d.NameTransform} {
		if strings.TrimSpace(v) == "" {
			return invalid(field, "is required")
		}
	}
	if d.SessionCap != nil && (*d.SessionCap < 1 || *d.SessionCap > 32) {
		return invalid("device.session_cap", "must be 1..32, got %d", *d.SessionCap)
	}
	if d.Groups == nil {
		return invalid("device.groups", "must be present (empty allowed)")
	}
	if d.Attributes == nil {
		return invalid("device.attributes", "must be present (empty allowed)")
	}
	for k := range d.Attributes {
		if strings.HasPrefix(k, "karvi_") {
			return invalid("device.attributes", "reserved key %q", k)
		}
	}
	return nil
}

func (a *AddressPlan) validate(stage Stage, endpoint string) error {
	switch a.Authority {
	case AddressByClient, AddressByDaemon:
	default:
		return invalid("address_plan.authority", "%q must be %q or %q", string(a.Authority), AddressByClient, AddressByDaemon)
	}
	switch a.FamilyPreference {
	case FamilyIPv4, FamilyIPv6:
	default:
		return invalid("address_plan.family_preference", "%q must be %q or %q", string(a.FamilyPreference), FamilyIPv4, FamilyIPv6)
	}
	if strings.TrimSpace(a.TransformedName) == "" {
		return invalid("address_plan.transformed_name", "is required")
	}
	if !strings.HasPrefix(a.SuffixAction, SuffixActionPrefix) || len(a.SuffixAction) == len(SuffixActionPrefix) {
		return invalid("address_plan.suffix_action", "%q must be %q<suffix> or %q", a.SuffixAction, SuffixActionPrefix, SuffixActionNone)
	}
	for field, list := range map[string][]netip.Addr{"address_plan.client_candidates": a.ClientCandidates, "address_plan.daemon_candidates": a.DaemonCandidates, "address_plan.alternates": a.Alternates} {
		if list == nil {
			return invalid(field, "must be present (empty allowed)")
		}
		if err := canonicalUnique(field, list); err != nil {
			return err
		}
	}
	if a.Selected.IsValid() && a.Selected != a.Selected.Unmap() {
		return invalid("address_plan.selected", "%s must be unmapped", a.Selected)
	}
	switch a.SelectedSource {
	case "", SourceInventory, SourceDNSClient, SourceDNSDaemon:
	default:
		return invalid("address_plan.selected_source", "%q is not %q, %q, or %q", a.SelectedSource, SourceInventory, SourceDNSClient, SourceDNSDaemon)
	}
	if a.Selected.IsValid() != (a.SelectedSource != "") {
		return invalid("address_plan.selected_source", "must be set exactly when selected is set")
	}
	candidates := append(append([]netip.Addr{}, a.ClientCandidates...), a.DaemonCandidates...)
	if a.Selected.IsValid() && !contains(candidates, a.Selected) {
		return invalid("address_plan.selected", "%s is not a candidate", a.Selected)
	}
	for _, alt := range a.Alternates {
		if !contains(candidates, alt) {
			return invalid("address_plan.alternates", "%s is not a candidate", alt)
		}
		if alt == a.Selected {
			return invalid("address_plan.alternates", "%s is the selected address", alt)
		}
	}
	switch a.Authority {
	case AddressByClient:
		if a.QueryName == "" && len(a.ClientCandidates) == 0 {
			return invalid("address_plan", "client authority needs a query_name or a literal client candidate")
		}
		if len(a.DaemonCandidates) != 0 {
			return invalid("address_plan.daemon_candidates", "must be empty under client authority")
		}
		if a.SelectedSource == SourceDNSDaemon {
			return invalid("address_plan.selected_source", "%q is not valid under client authority", a.SelectedSource)
		}
		if a.ResolverContext != "" && a.ResolverContext != ResolverContextClient {
			return invalid("address_plan.resolver_context", "%q must be %q under client authority", a.ResolverContext, ResolverContextClient)
		}
		if !a.ResolutionDigest.IsZero() {
			return invalid("address_plan.resolution_digest", "must be unset under client authority")
		}
	case AddressByDaemon:
		if a.QueryName == "" {
			return invalid("address_plan.query_name", "is required under daemon authority")
		}
		if a.SelectedSource == SourceDNSClient {
			return invalid("address_plan.selected_source", "%q is not valid under daemon authority", a.SelectedSource)
		}
		if stage == Draft {
			if len(a.DaemonCandidates) != 0 || a.Selected.IsValid() || len(a.Alternates) != 0 || a.ResolverContext != "" || !a.ResolutionDigest.IsZero() {
				return invalid("address_plan", "daemon-filled fields must be empty in a draft")
			}
		} else {
			if a.ResolverContext != endpoint {
				return invalid("address_plan.resolver_context", "%q must be the execution endpoint %q", a.ResolverContext, endpoint)
			}
			if a.ResolutionDigest.IsZero() {
				return invalid("address_plan.resolution_digest", "is required before commit")
			}
		}
	}
	if stage == Committed {
		if !a.Selected.IsValid() {
			return invalid("address_plan.selected", "is required before commit")
		}
		if a.Authority == AddressByClient && a.ResolverContext != ResolverContextClient {
			return invalid("address_plan.resolver_context", "is required before commit")
		}
	}
	return nil
}

func canonicalUnique(field string, list []netip.Addr) error {
	seen := map[netip.Addr]bool{}
	for _, addr := range list {
		if !addr.IsValid() {
			return invalid(field, "contains an invalid address")
		}
		if addr != addr.Unmap() {
			return invalid(field, "%s must be unmapped", addr)
		}
		if seen[addr] {
			return invalid(field, "%s appears twice", addr)
		}
		seen[addr] = true
	}
	return nil
}

func contains(list []netip.Addr, addr netip.Addr) bool {
	for _, a := range list {
		if a == addr {
			return true
		}
	}
	return false
}

// identifier enforces the identifier rule: printable ASCII without whitespace.
func identifier(field, v string) error {
	if v == "" {
		return invalid(field, "is required")
	}
	for _, r := range v {
		if r <= 0x20 || r >= 0x7f {
			return invalid(field, "%q must be printable ASCII without whitespace", v)
		}
	}
	return nil
}
