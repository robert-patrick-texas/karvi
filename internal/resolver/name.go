// Package resolver performs the deterministic inventory-name-address pipeline.
package resolver

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/transform"
)

type Capabilities struct {
	IPv4 bool `json:"ipv4"`
	IPv6 bool `json:"ipv6"`
}
type Resolution struct {
	Device            inventory.Device
	InputTarget       string
	TransformedName   string
	DNSQueryName      string
	DNSSuffixAction   string
	AddressCandidates []string
	SelectedAddress   netip.Addr
	AddressFamily     string
	AddressSource     string
	Notices           []string
	Duration          time.Duration
}
type Error struct {
	Code, Message, Query string
	Cause                error
}

func (e *Error) Error() string {
	if e.Query != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Query)
	}
	return e.Code + ": " + e.Message
}
func (e *Error) Unwrap() error { return e.Cause }

// ErrorCode returns the registered error code.
func (e *Error) ErrorCode() string { return e.Code }

func ProbeFamilies() Capabilities {
	c := Capabilities{}
	if p, e := net.ListenPacket("udp4", "0.0.0.0:0"); e == nil {
		c.IPv4 = true
		p.Close()
	}
	if p, e := net.ListenPacket("udp6", "[::]:0"); e == nil {
		c.IPv6 = true
		p.Close()
	}
	return c
}

// LookupFunc is the DNS lookup the resolver calls: net.DefaultResolver's
// LookupNetIP in production, an injected answer in tests.
type LookupFunc func(ctx context.Context, network, host string) ([]netip.Addr, error)

type lookupNetIPFunc = LookupFunc

func Resolve(ctx context.Context, cfg configload.Snapshot, d inventory.Device, caps Capabilities) (Resolution, error) {
	return resolveWithLookup(ctx, cfg, d, caps, net.DefaultResolver.LookupNetIP)
}

// resolveWithLookup keeps address-family ordering independently testable while
// the public resolver continues to use the host's configured DNS resolver.
func resolveWithLookup(ctx context.Context, cfg configload.Snapshot, d inventory.Device, caps Capabilities, lookup lookupNetIPFunc) (Resolution, error) {
	started := time.Now()
	r := Resolution{Device: d, InputTarget: d.SuppliedName(), TransformedName: d.CanonicalName, DNSSuffixAction: "add-suffix:none", AddressCandidates: []string{}}
	tr, err := Transform(cfg, d)
	if err != nil {
		return r, err
	}
	name := tr.Name
	r.TransformedName = name
	r.Device.CanonicalName = name
	r.Device.NameTransform = tr.Profile
	r.DNSSuffixAction = tr.SuffixAction
	if d.ManagementAddress.IsValid() {
		addr := d.ManagementAddress.Unmap()
		if !familyAvailable(addr, caps) {
			return r, &Error{Code: "address_family_unavailable", Message: "literal management address uses an unavailable family", Query: addr.String()}
		}
		r.SelectedAddress = addr
		r.AddressCandidates = []string{addr.String()}
		r.AddressSource = "inventory"
		r.AddressFamily = family(addr)
		r.Duration = time.Since(started)
		return r, nil
	}
	order, notice, err := familyOrder(cfg.String("name.address-family-preference"), caps)
	if err != nil {
		return r, err
	}
	if notice != "" {
		r.Notices = append(r.Notices, notice)
	}
	r.DNSQueryName = name
	fam, valid, err := queryDNS(ctx, name, order, dnsTimeout(cfg), lookup)
	if err != nil {
		return r, err
	}
	r.AddressFamily = fam
	r.AddressSource = "dns"
	r.SelectedAddress = valid[0]
	for _, a := range valid {
		r.AddressCandidates = append(r.AddressCandidates, a.String())
	}
	if len(valid) > 1 {
		r.Notices = append(r.Notices, fmt.Sprintf("multiple %s addresses for %s; selected numeric-lowest %s from %s", fam, name, valid[0], strings.Join(r.AddressCandidates, ", ")))
	}
	r.Device.ManagementAddress = valid[0]
	r.Device.Addresses = []inventory.Address{{Address: valid[0], Role: "management", Source: "dns"}}
	r.Duration = time.Since(started)
	return r, nil
}

// familyOrder is the DNS family order for a preference given the host's
// socket capabilities: the preference first, the other family as fallback,
// and a notice when the preferred family has no sockets. Neither family
// usable is address_family_unavailable.
func familyOrder(pref string, caps Capabilities) ([]string, string, error) {
	order := []string{pref}
	if pref == "ipv6" {
		order = append(order, "ipv4")
	} else {
		order = append(order, "ipv6")
	}
	notice := ""
	if !familyUsable(order[0], caps) && familyUsable(order[1], caps) {
		notice = fmt.Sprintf("preferred address family %s is unavailable; using %s", order[0], order[1])
	}
	if !caps.IPv4 && !caps.IPv6 {
		return nil, "", &Error{Code: "address_family_unavailable", Message: "neither IPv4 nor IPv6 sockets are available"}
	}
	usable := []string{}
	for _, fam := range order {
		if familyUsable(fam, caps) {
			usable = append(usable, fam)
		}
	}
	return usable, notice, nil
}

func familyUsable(f string, caps Capabilities) bool {
	if f == "ipv4" {
		return caps.IPv4
	}
	return caps.IPv6
}

func dnsTimeout(cfg configload.Snapshot) time.Duration {
	timeout := cfg.Duration("name.dns-timeout")
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return timeout
}

// queryDNS asks for name in each usable family of order and returns the first
// family with a usable answer, its addresses canonical and ascending
// (numeric-lowest first). The error is the registered DNS code.
func queryDNS(ctx context.Context, name string, order []string, timeout time.Duration, lookup LookupFunc) (string, []netip.Addr, error) {
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last error
	for _, fam := range order {
		network := "ip4"
		if fam == "ipv6" {
			network = "ip6"
		}
		addrs, e := lookup(qctx, network, name)
		if e != nil {
			last = e
			continue
		}
		valid := canonical(addrs, fam)
		if len(valid) == 0 {
			continue
		}
		return fam, valid, nil
	}
	code := "dns_other"
	message := "resolver returned no usable address"
	if last != nil {
		message = last.Error()
		var de *net.DNSError
		if ok := asDNSError(last, &de); ok {
			if de.IsNotFound {
				code = "dns_nxdomain"
			} else if de.IsTimeout {
				code = "dns_timeout"
			} else if de.IsTemporary || strings.Contains(strings.ToLower(de.Err), "server misbehaving") {
				code = "dns_servfail"
			}
		}
	}
	if qctx.Err() == context.DeadlineExceeded {
		code = "dns_timeout"
	}
	return "", nil, &Error{Code: code, Message: message, Query: name, Cause: last}
}
func asDNSError(err error, out **net.DNSError) bool {
	for err != nil {
		if e, ok := err.(*net.DNSError); ok {
			*out = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
func canonical(in []netip.Addr, fam string) []netip.Addr {
	seen := map[netip.Addr]bool{}
	out := []netip.Addr{}
	for _, a := range in {
		a = a.Unmap()
		if !a.IsValid() || a.IsUnspecified() || a.IsMulticast() {
			continue
		}
		if fam == "ipv4" && !a.Is4() {
			continue
		}
		if fam == "ipv6" && !a.Is6() {
			continue
		}
		if a.Zone() != "" {
			continue
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Compare(out[j]) < 0 })
	return out
}
func family(a netip.Addr) string {
	if a.Is4() {
		return "ipv4"
	}
	return "ipv6"
}
func familyAvailable(a netip.Addr, c Capabilities) bool {
	if a.Is4() {
		return c.IPv4
	}
	return c.IPv6
}

func profiles(cfg configload.Snapshot) (map[string]transform.Sequence, error) {
	out := map[string]transform.Sequence{"default": {}}
	for name, t := range cfg.NamedTables("name-transform") {
		raw, _ := t["operations"].([]any)
		items := []map[string]any{}
		for _, v := range raw {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, errorcodes.Errorf("name_transform_operation_not_table", "name-transform.%s operation is not an inline table", name)
			}
			items = append(items, m)
		}
		seq, e := transform.FromMaps(items)
		if e != nil {
			return nil, e
		}
		out[name] = seq
	}
	return out, nil
}

func EffectivePort(d inventory.Device, transport string, cfg configload.Snapshot) uint16 {
	if d.Port != 0 {
		return d.Port
	}
	def := platform.Resolve(d.Platform, cfg.NamedTables("platform"))
	if transport == "telnet" {
		return def.TelnetPort
	}
	return def.SSHPort
}
