// Package inventory contains reusable, nonsecret inventory models and selection
// contracts. It intentionally has no dependency on CLI or daemon packages.
package inventory

import (
	"context"
	"net/netip"
	"regexp"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

const SchemaVersion = 1

var transportSelectorPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// Address-authority values of the inventory column, the command line, and
// the configuration default: who
// selects the address, the client at planning or the daemon at prepare.
const (
	AuthorityClient = "client"
	AuthorityDaemon = "daemon"
)

// Address roles and sources.
const (
	RoleManagement = "management"
	RoleAlternate  = "alternate"
)

type SourceRef struct {
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	Digest string `json:"digest,omitempty"`
}

type Address struct {
	Address    netip.Addr `json:"address"`
	Role       string     `json:"role"`
	Source     string     `json:"source"`
	Preference int        `json:"preference"`
	Discovered bool       `json:"discovered"`
}

type Device struct {
	SchemaVersion     int        `json:"schema_version"`
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	CanonicalName     string     `json:"canonical_name"`
	ManagementAddress netip.Addr `json:"management_address,omitempty"`
	Addresses         []Address  `json:"addresses"`
	// AddressAuthority is the address-authority column: "", client, or daemon.
	// It is an input to the address plan, not a projected device fact.
	AddressAuthority string `json:"address_authority,omitempty"`
	Platform         string `json:"platform"`
	// PlatformUsed is the platform the session uses, filled by the planner's
	// device view from the plan target and by login after its resolution,
	// for the credential resolver's
	// enable rule only; empty outside planning. Platform stays the set
	// platform, which the run selector and the maps match.
	PlatformUsed      string `json:"-"`
	Transport         string `json:"transport"`
	TransportExplicit bool   `json:"-"`
	// InputToken is the target token as the operator supplied it, before the
	// lowercase conversion; records report it as input_target.
	// Empty means Name (an inventory device).
	InputToken     string   `json:"-"`
	Port           uint16   `json:"port,omitempty"`
	Site           string   `json:"site,omitempty"`
	Groups         []string `json:"groups"`
	NameTransform  string   `json:"name_transform"`
	SessionCap     *int     `json:"session_cap,omitempty"`
	RiskTier       string   `json:"risk_tier,omitempty"`
	DeploymentRing string   `json:"deployment_ring,omitempty"`
	TopologyDomain string   `json:"topology_domain,omitempty"`
	// CredKeyRef is the inventory's `credkeyref` column: the `credkey` of the
	// credential row this device is pinned to, a plain nonsecret literal,
	// blank for an unpinned device and for every direct target. The
	// credential resolver reads it as the device's sixth selector field and
	// walks the policy's sequence for that key alone. Omitted from output when
	// blank, so a device without a pin encodes as it did before the field.
	// It is not part of Digest, which covers identity and connection facts
	// only (site and groups, the other selection inputs, are not in it
	// either); a changed pin shows in the source file's own digest.
	CredKeyRef string            `json:"credkeyref,omitempty"`
	Attributes map[string]string `json:"attributes"`
	Source     SourceRef         `json:"source"`
}

type Provenance struct {
	Sources []SourceRef `json:"sources"`
}

type Source interface {
	Load(ctx context.Context) ([]Device, Provenance, error)
}

type Query struct {
	Targets   []string
	Sites     []string
	Groups    []string
	Platforms []string
	Excludes  []string
	All       bool
}

type Selector interface {
	Select(ctx context.Context, devices []Device, query Query) ([]Device, error)
}

// Direct constructs the deterministic ad-hoc device required for direct modes.
// An empty platformName stays blank: a direct target without --platform is
// not set, and the planner resolves it at planning.
func Direct(target, platformName, transport string, port uint16) Device {
	canonical := strings.TrimSpace(target)
	if transport == "" {
		transport = "system"
	}
	d := Device{
		SchemaVersion: SchemaVersion, ID: "name:" + strings.ToLower(canonical),
		Name: target, CanonicalName: canonical, Platform: strings.ToLower(strings.TrimSpace(platformName)),
		Transport: strings.ToLower(transport), Port: port, Groups: []string{},
		NameTransform: "default", Attributes: map[string]string{},
		Source: SourceRef{Name: "direct", Path: "cli"},
	}
	if addr, err := netip.ParseAddr(canonical); err == nil {
		addr = addr.Unmap()
		d.ManagementAddress = addr
		d.Addresses = []Address{{Address: addr, Role: "management", Source: "inventory"}}
	}
	return d
}

// SuppliedName returns the target token as supplied (records: input_target).
func (d Device) SuppliedName() string {
	if d.InputToken != "" {
		return d.InputToken
	}
	return d.Name
}

// Validate performs schema-level validation independent of any specific source.
func (d *Device) Validate() error {
	if d.SchemaVersion == 0 {
		d.SchemaVersion = SchemaVersion
	}
	if d.SchemaVersion != SchemaVersion {
		return errorcodes.Errorf("device_schema_unsupported", "unsupported device schema %d", d.SchemaVersion)
	}
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" {
		return errorcodes.Errorf("device_name_blank", "device name is blank")
	}
	for _, r := range d.Name {
		if r < 0x20 || r == 0x7f {
			return errorcodes.Errorf("device_name_control_character", "device name contains a control character")
		}
	}
	if d.CanonicalName == "" {
		d.CanonicalName = d.Name
	}
	if d.ID == "" {
		d.ID = "name:" + strings.ToLower(d.CanonicalName)
	}
	// A blank platform is valid and stays blank (not set): the execution
	// plan, not the device, requires the
	// resolved platform (executionplan.ExecutionTarget.Validate).
	if d.Transport == "" {
		d.Transport = "native"
	}
	d.Transport = strings.ToLower(strings.TrimSpace(d.Transport))
	if d.Transport != "default" && d.Transport != "preferred" && d.Transport != "telnet" && !transportSelectorPattern.MatchString(d.Transport) {
		return errorcodes.Errorf("device_transport_selector_invalid", "invalid transport selector %q", d.Transport)
	}
	if d.NameTransform == "" {
		d.NameTransform = "default"
	}
	d.AddressAuthority = strings.ToLower(strings.TrimSpace(d.AddressAuthority))
	switch d.AddressAuthority {
	case "", AuthorityClient, AuthorityDaemon:
	default:
		return errorcodes.Errorf("inventory_address_authority_invalid", "address_authority %q must be %s or %s", d.AddressAuthority, AuthorityClient, AuthorityDaemon)
	}
	if d.Groups == nil {
		d.Groups = []string{}
	}
	seen := map[string]bool{}
	out := d.Groups[:0]
	for _, g := range d.Groups {
		g = strings.TrimSpace(g)
		if g != "" && !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	d.Groups = out
	if d.Attributes == nil {
		d.Attributes = map[string]string{}
	}
	for k := range d.Attributes {
		if strings.HasPrefix(k, "karvi_") {
			return errorcodes.Errorf("device_attribute_reserved", "reserved attribute key %q", k)
		}
	}
	if d.SessionCap != nil && (*d.SessionCap < 1 || *d.SessionCap > 32) {
		return errorcodes.Errorf("device_session_cap_out_of_range", "session_cap must be 1..32")
	}
	if d.ManagementAddress.IsValid() {
		d.ManagementAddress = d.ManagementAddress.Unmap()
		found := false
		for i := range d.Addresses {
			d.Addresses[i].Address = d.Addresses[i].Address.Unmap()
			if d.Addresses[i].Address == d.ManagementAddress && d.Addresses[i].Role == "management" {
				found = true
			}
		}
		if !found {
			d.Addresses = append(d.Addresses, Address{Address: d.ManagementAddress, Role: RoleManagement, Source: "inventory"})
		}
	}
	return nil
}

// Alternates returns the device's alternate addresses in inventory row order,
// unmapped, unique, and distinct from the management address. They are
// recorded in the plan, never retried.
func (d Device) Alternates() []netip.Addr {
	out := []netip.Addr{}
	seen := map[netip.Addr]bool{}
	for _, a := range d.Addresses {
		addr := a.Address.Unmap()
		if a.Role != RoleAlternate || !addr.IsValid() || addr == d.ManagementAddress || seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	return out
}
