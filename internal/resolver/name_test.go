package resolver

import (
	"context"
	"net/netip"
	"reflect"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/inventory"
)

func TestResolveHonorsAddressFamilyPreference(t *testing.T) {
	for _, tc := range []struct {
		name       string
		preference string
		want       string
		wantCalls  []string
	}{
		{name: "ipv4", preference: "ipv4", want: "192.0.2.8", wantCalls: []string{"ip4"}},
		{name: "ipv6", preference: "ipv6", want: "2001:db8::8", wantCalls: []string{"ip6"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
				"name.address-family-preference=" + tc.preference,
			}})
			if err != nil {
				t.Fatal(err)
			}
			calls := []string{}
			lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
				calls = append(calls, network)
				if host != "dual.example" {
					t.Fatalf("lookup host=%q, want dual.example", host)
				}
				if network == "ip4" {
					return []netip.Addr{netip.MustParseAddr("192.0.2.9"), netip.MustParseAddr("192.0.2.8")}, nil
				}
				return []netip.Addr{netip.MustParseAddr("2001:db8::9"), netip.MustParseAddr("2001:db8::8")}, nil
			}
			d := inventory.Direct("dual.example", "generic", "system", 0)
			got, err := resolveWithLookup(context.Background(), cfg, d, Capabilities{IPv4: true, IPv6: true}, lookup)
			if err != nil {
				t.Fatal(err)
			}
			if got.SelectedAddress.String() != tc.want {
				t.Fatalf("selected=%s, want %s", got.SelectedAddress, tc.want)
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("lookup order=%v, want %v", calls, tc.wantCalls)
			}
		})
	}
}

func TestResolveFallsBackWhenPreferredFamilyHasNoRecord(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
		"name.address-family-preference=ipv6",
	}})
	if err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	lookup := func(_ context.Context, network, _ string) ([]netip.Addr, error) {
		calls = append(calls, network)
		if network == "ip6" {
			return nil, nil
		}
		return []netip.Addr{netip.MustParseAddr("192.0.2.10")}, nil
	}
	d := inventory.Direct("fallback.example", "generic", "system", 0)
	got, err := resolveWithLookup(context.Background(), cfg, d, Capabilities{IPv4: true, IPv6: true}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if got.SelectedAddress.String() != "192.0.2.10" || !reflect.DeepEqual(calls, []string{"ip6", "ip4"}) {
		t.Fatalf("resolution=%s calls=%v", got.SelectedAddress, calls)
	}
}
