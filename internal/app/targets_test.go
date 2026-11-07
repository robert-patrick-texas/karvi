package app

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/planner"
	"github.com/robert-patrick-texas/karvi/inventory"
)

func invDevice(name, site, platform string, groups ...string) inventory.Device {
	d := inventory.Direct(name, platform, "", 0)
	d.ID = "inv:" + strings.ToLower(name)
	d.Site = site
	d.Groups = append([]string{}, groups...)
	d.Source = inventory.SourceRef{Name: "test", Path: "inventory.csv"}
	return d
}

// setPlatformAsIs is the matching rule under the default configuration:
// the set platform as named, blank for not set (planner.SetPlatformFunc).
var setPlatformAsIs = planner.SetPlatformFunc(configload.Snapshot{})

// testRules are the target rules the tests assemble under: the default
// targets.max-created-names, targets.inventory-only false.
var testRules = targetRules{maxCreated: 1000}

func names(devices []inventory.Device) []string {
	out := []string{}
	for _, d := range devices {
		out = append(out, d.CanonicalName)
	}
	return out
}

var testInventory = []inventory.Device{
	invDevice("Core-1", "hq", "cisco_ios", "core"),
	invDevice("core-2", "hq", "cisco_ios", "core"),
	invDevice("edge-1", "branch", "cisco_ios", "edge"),
	invDevice("lab-1", "lab", "generic", "lab"),
}

// TestAssembleKeepsCommandLineOrder covers the assembly order: every
// input contributes at its position, inventory matches in inventory order.
func TestAssembleKeepsCommandLineOrder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inputs []TargetInput
		want   []string
	}{
		// An inventory device keeps its inventory spelling (Core-1); its
		// identity is the lowercase form.
		{"direct before selector (defect fixed)",
			[]TargetInput{{Kind: "target", Value: "r9"}, {Kind: "site", Value: "hq"}, {Kind: "target", Value: "r8"}},
			[]string{"r9", "Core-1", "core-2", "r8"}},
		{"glob at its position, inventory order",
			[]TargetInput{{Kind: "target", Value: "edge-1"}, {Kind: "target", Value: "CORE-*"}},
			[]string{"edge-1", "Core-1", "core-2"}},
		{"file names at the file's position",
			[]TargetInput{{Kind: "names", Names: []string{"B", "a"}, Source: "list.txt"}, {Kind: "target", Value: "c"}},
			[]string{"b", "a", "c"}},
		{"device-group then platform then all",
			[]TargetInput{{Kind: "device-group", Value: "edge"}, {Kind: "platform", Value: "gen*"}, {Kind: "all"}},
			[]string{"edge-1", "lab-1", "Core-1", "core-2"}},
		{"a later spelling of an inventory device is the same device",
			[]TargetInput{{Kind: "target", Value: "CORE-1"}, {Kind: "site", Value: "hq"}, {Kind: "target", Value: "core-1"}},
			[]string{"Core-1", "core-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assemble(testInventory, tc.inputs, nil, setPlatformAsIs, testRules)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(names(got), tc.want) {
				t.Fatalf("order=%q, want %q", names(got), tc.want)
			}
		})
	}
}

// TestAssembleIdentityIsLowercase covers device identity:
// names are lowercase, the first occurrence keeps its position, and the token
// as supplied survives for input_target.
func TestAssembleIdentityIsLowercase(t *testing.T) {
	got, err := assemble(testInventory, []TargetInput{
		{Kind: "target", Value: "R9"}, {Kind: "names", Names: []string{"r9", "Edge-1"}}, {Kind: "target", Value: "EDGE-1"}, {Kind: "target", Value: " r9 "},
	}, nil, setPlatformAsIs, testRules)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names(got), []string{"r9", "edge-1"}) {
		t.Fatalf("names=%q", names(got))
	}
	if got[0].Name != "r9" || got[0].ID != "name:r9" || got[0].SuppliedName() != "R9" {
		t.Fatalf("direct device is lowercase and keeps the supplied token: %+v", got[0])
	}
	if got[1].SuppliedName() != "edge-1" {
		t.Fatalf("inventory device reports its inventory name: %q", got[1].SuppliedName())
	}
	if got[1].ID != "inv:edge-1" || got[1].Site != "branch" {
		t.Fatalf("an exact inventory match uses the inventory device: %+v", got[1])
	}
}

// TestAssembleExcludeAppliesToWholeList: --exclude removes
// direct targets too, after duplicate removal, without regard to letter case.
func TestAssembleExcludeAppliesToWholeList(t *testing.T) {
	got, err := assemble(testInventory, []TargetInput{
		{Kind: "target", Value: "r9"}, {Kind: "target", Value: "r8"}, {Kind: "site", Value: "hq"},
	}, []string{"R8", "core-1"}, setPlatformAsIs, testRules)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names(got), []string{"r9", "core-2"}) {
		t.Fatalf("names=%q", names(got))
	}
	got, err = assemble(testInventory, []TargetInput{{Kind: "all"}}, []string{"core-*", "edge-?"}, setPlatformAsIs, testRules)
	if err != nil || !reflect.DeepEqual(names(got), []string{"lab-1"}) {
		t.Fatalf("glob exclude: %q %v", names(got), err)
	}
}

// TestAssembleSelectorGrammar covers the selector grammar:
// every selector folds case and uses the shared grammar, and a value
// beginning with "!" removes the devices whose field matches wherever it
// stands but never selects.
func TestAssembleSelectorGrammar(t *testing.T) {
	inv := append(append([]inventory.Device{}, testInventory...),
		invDevice("NYC-1", "NYC", "cisco_ios", "Edge"),
		invDevice("nyc-2", "nyc/dc1", "CISCO_IOS", "edge", "lab"),
	)
	for _, tc := range []struct {
		name     string
		inputs   []TargetInput
		excludes []string
		want     []string
	}{
		{"site folds case", []TargetInput{{Kind: "site", Value: "nyc"}}, nil, []string{"NYC-1"}},
		{"group folds case", []TargetInput{{Kind: "device-group", Value: "EDGE"}}, nil, []string{"edge-1", "NYC-1", "nyc-2"}},
		{"* spans /", []TargetInput{{Kind: "site", Value: "nyc*"}}, nil, []string{"NYC-1", "nyc-2"}},
		{"class negation", []TargetInput{{Kind: "site", Value: "[!bh]*"}}, nil, []string{"lab-1", "NYC-1", "nyc-2"}},
		{"group less a site", []TargetInput{{Kind: "device-group", Value: "edge"}, {Kind: "site", Value: "!nyc*"}}, nil, []string{"edge-1"}},
		{"a not selector before the selector", []TargetInput{{Kind: "site", Value: "!NYC"}, {Kind: "device-group", Value: "edge"}}, nil, []string{"edge-1", "nyc-2"}},
		{"a not group removes on any group", []TargetInput{{Kind: "all"}, {Kind: "device-group", Value: "!lab"}}, nil, []string{"Core-1", "core-2", "edge-1", "NYC-1"}},
		{"a not platform", []TargetInput{{Kind: "site", Value: "nyc*"}, {Kind: "platform", Value: "!cisco_*"}}, nil, nil},
		{"a platform by spelling", []TargetInput{{Kind: "platform", Value: "cisco_iosxe"}, {Kind: "site", Value: "!nyc*"}}, nil, []string{"Core-1", "core-2", "edge-1"}},
		{"a not platform by spelling", []TargetInput{{Kind: "site", Value: "nyc*"}, {Kind: "platform", Value: "!Cisco_XE"}}, nil, nil},
		{"a not target removes by name", []TargetInput{{Kind: "target", Value: "r9"}, {Kind: "site", Value: "hq"}, {Kind: "target", Value: "!CORE-1"}}, nil, []string{"r9", "core-2"}},
		{"a direct target has no site to remove", []TargetInput{{Kind: "target", Value: "r9"}, {Kind: "site", Value: "hq"}, {Kind: "site", Value: "!*"}}, nil, []string{"r9"}},
		{"file names skip ! comments", []TargetInput{{Kind: "names", Names: []string{"r8", "!core-2"}, Source: "list.txt"}, {Kind: "site", Value: "hq"}}, nil, []string{"r8", "Core-1", "core-2"}},
		{"escaped ! is literal", []TargetInput{{Kind: "all"}}, []string{`\!x`, "nyc-?"}, []string{"Core-1", "core-2", "edge-1", "lab-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assemble(inv, tc.inputs, tc.excludes, setPlatformAsIs, testRules)
			if tc.want == nil {
				if errorcodes.Of(err) != "target_set_empty" {
					t.Fatalf("got %q %v, want target_set_empty", names(got), err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(names(got), tc.want) {
				t.Fatalf("names=%q err=%v, want %q", names(got), err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		inputs   []TargetInput
		excludes []string
	}{
		{[]TargetInput{{Kind: "site", Value: "ny[c"}}, nil},
		{[]TargetInput{{Kind: "target", Value: "^core"}}, nil},
		{[]TargetInput{{Kind: "names", Names: []string{"core$"}, Source: "list.txt"}}, nil},
		{[]TargetInput{{Kind: "all"}}, []string{"!core-1"}},
	} {
		if _, err := assemble(inv, tc.inputs, tc.excludes, setPlatformAsIs, testRules); errorcodes.Of(err) != "target_selector_pattern_invalid" {
			t.Errorf("%+v %q: %v", tc.inputs, tc.excludes, err)
		}
	}
	_, err := assembleTargets(context.Background(), configload.Snapshot{}, credentials.Operator{}, []TargetInput{{Kind: "site", Value: "!nyc"}, {Kind: "target", Value: " !r1"}}, nil, io.Discard)
	if errorcodes.Of(err) != "inventory_positive_selector_missing" {
		t.Fatalf("only not selectors: %v", err)
	}
}

// TestAssembleNumericRange: --site, --device-group, --exclude, and a "!"
// target holding a numeric range select the inventory's devices among the
// names the range spells: --site 'pod[1-12]' takes pod10 to pod12 as well as
// pod1 to pod9, which a one-character class would miss. (A --target holding
// a range spells its names: TestAssembleCreatesRangeNames.)
func TestAssembleNumericRange(t *testing.T) {
	var inv []inventory.Device
	for _, n := range []string{"1", "2", "3", "9", "10", "11", "12", "13"} {
		inv = append(inv, invDevice("router"+n, "pod"+n, "cisco_ios", "ring"+n))
	}
	inv = append(inv, invDevice("router01", "pod01", "cisco_ios", "ring01"))
	for _, tc := range []struct {
		name   string
		inputs []TargetInput
		want   []string
	}{
		{"site pod[1-12]", []TargetInput{{Kind: "site", Value: "pod[1-12]"}}, []string{"router1", "router2", "router3", "router9", "router10", "router11", "router12"}},
		{"site pod[01-03]", []TargetInput{{Kind: "site", Value: "pod[01-03]"}}, []string{"router01"}},
		{"group ring[10,13]", []TargetInput{{Kind: "device-group", Value: "ring[10,13]"}}, []string{"router10", "router13"}},
		{"target router[10-12]", []TargetInput{{Kind: "target", Value: "router[10-12]"}}, []string{"router10", "router11", "router12"}},
		{"a not target range", []TargetInput{{Kind: "all"}, {Kind: "target", Value: "!router[2-12]"}}, []string{"router1", "router13", "router01"}},
		{"a not site range", []TargetInput{{Kind: "site", Value: "pod*"}, {Kind: "site", Value: "!pod[1-11]"}}, []string{"router12", "router13", "router01"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assemble(inv, tc.inputs, nil, setPlatformAsIs, testRules)
			if err != nil || !reflect.DeepEqual(names(got), tc.want) {
				t.Fatalf("names=%q err=%v, want %q", names(got), err, tc.want)
			}
		})
	}
	if got, err := assemble(inv, []TargetInput{{Kind: "all"}}, []string{"router[1-11]"}, setPlatformAsIs, testRules); err != nil || !reflect.DeepEqual(names(got), []string{"router12", "router13", "router01"}) {
		t.Errorf("--exclude router[1-11]: %q %v", names(got), err)
	}
	for _, v := range []string{"pod[1-]", "pod[12-1]"} {
		if _, err := assemble(inv, []TargetInput{{Kind: "site", Value: v}}, nil, setPlatformAsIs, testRules); errorcodes.Of(err) != "target_selector_pattern_invalid" {
			t.Errorf("--site %s: %v", v, err)
		}
	}
}

// TestAssembleEmptyCodes covers the empty-set codes and the more specific
// inventory_empty_selection.
func TestAssembleEmptyCodes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		inputs   []TargetInput
		excludes []string
		code     string
	}{
		{"no inputs", nil, nil, "inventory_positive_selector_missing"},
		{"selectors match nothing", []TargetInput{{Kind: "site", Value: "nowhere"}, {Kind: "target", Value: "zz-*"}}, nil, "inventory_empty_selection"},
		{"everything excluded", []TargetInput{{Kind: "target", Value: "r9"}}, []string{"r9"}, "target_set_empty"},
		{"selector matched then excluded", []TargetInput{{Kind: "site", Value: "lab"}}, []string{"lab-*"}, "target_set_empty"},
		{"blank name", []TargetInput{{Kind: "names", Names: []string{"  "}}}, nil, "device_name_blank"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.inputs == nil {
				// The no-input rule runs before configuration or inventory is touched.
				_, err = assembleTargets(context.Background(), configload.Snapshot{}, credentials.Operator{}, nil, nil, io.Discard)
			} else {
				_, err = assemble(testInventory, tc.inputs, tc.excludes, setPlatformAsIs, testRules)
			}
			if errorcodes.Of(err) != tc.code {
				t.Fatalf("code=%q err=%v, want %s", errorcodes.Of(err), err, tc.code)
			}
		})
	}
}

// TestApplyManagementAddress covers the literal --management-address.
func TestApplyManagementAddress(t *testing.T) {
	set := TargetSet{Devices: []inventory.Device{invDevice("a", "", ""), invDevice("b", "", "")}}
	if err := applyManagementAddress(&set, "192.0.2.1"); errorcodes.Of(err) != "management_address_scope_error" {
		t.Fatalf("two devices: %v", err)
	}
	set = TargetSet{Devices: []inventory.Device{invDevice("a", "", "")}}
	if err := applyManagementAddress(&set, "not-an-ip"); errorcodes.Of(err) != "management_address_invalid" {
		t.Fatalf("bad address: %v", err)
	}
	if err := applyManagementAddress(&set, "::ffff:192.0.2.1"); err != nil || set.Devices[0].ManagementAddress.String() != "192.0.2.1" || set.Devices[0].Addresses[0].Source != "cli" {
		t.Fatalf("address not applied: %v %+v", err, set.Devices[0])
	}
	if err := applyManagementAddress(&set, ""); err != nil {
		t.Fatal(err)
	}
}

// TestFirstDeviceOverrides covers the direct-mode overrides: --platform names the
// definition, --transport and --port replace the device's own values.
func TestFirstDeviceOverrides(t *testing.T) {
	set := TargetSet{Devices: []inventory.Device{invDevice("core-1", "hq", "cisco_ios"), invDevice("core-2", "hq", "cisco_ios")}}
	d := firstDevice(set, "", "", 0)
	if d.CanonicalName != "core-1" || d.Platform != "cisco_ios" || d.TransportExplicit {
		t.Fatalf("%+v", d)
	}
	d = firstDevice(set, "Generic", "Telnet", 2323)
	if d.Platform != "generic" || d.Transport != "telnet" || !d.TransportExplicit || d.Port != 2323 {
		t.Fatalf("%+v", d)
	}
	if set.CandidateCount() != 2 {
		t.Fatalf("candidates=%d", set.CandidateCount())
	}
}

// TestOverridePlatform covers run's --platform: every device of the set
// runs as the named platform,
// an inventory row and a direct target alike, and an empty name changes
// nothing.
func TestOverridePlatform(t *testing.T) {
	set := TargetSet{Devices: []inventory.Device{invDevice("core-1", "hq", "cisco_ios"), invDevice("direct-1", "", "")}}
	overridePlatform(&set, "")
	if set.Devices[0].Platform != "cisco_ios" || set.Devices[1].Platform != "" {
		t.Fatalf("an empty override changed the set: %+v", set.Devices)
	}
	overridePlatform(&set, "Cisco_NXOS")
	for _, d := range set.Devices {
		if d.Platform != "cisco_nxos" {
			t.Fatalf("%s runs as %q, want cisco_nxos", d.CanonicalName, d.Platform)
		}
	}
}

// TestResolveOrder covers dispatch.order and its recorded values:
// the alias is canonical, the key is present only for shuffle and random,
// and random uses the epoch seconds at planning time.
func TestResolveOrder(t *testing.T) {
	load := func(sets ...string) configload.Snapshot {
		snap, err := configload.Load(configload.Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: sets})
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	fixed := func() time.Time { return time.Unix(1757800000, 0) }
	for _, tc := range []struct {
		sets  []string
		order string
		key   *string
	}{
		{nil, "default", nil},
		{[]string{`dispatch.order="name"`}, "sorted", nil},
		{[]string{`dispatch.order="sorted"`}, "sorted", nil},
		{[]string{`dispatch.order="shuffle"`}, "shuffle", ptr("")},
		{[]string{`dispatch.order="shuffle"`, `dispatch.shuffle-key="k"`}, "shuffle", ptr("k")},
		{[]string{`dispatch.order="random"`, `dispatch.shuffle-key="ignored"`}, "random", ptr("1757800000")},
	} {
		order, key := resolveOrder(load(tc.sets...), fixed)
		if order != tc.order || (key == nil) != (tc.key == nil) || (key != nil && *key != *tc.key) {
			t.Fatalf("%v: order=%s key=%v, want %s %v", tc.sets, order, deref(key), tc.order, deref(tc.key))
		}
	}
}

func ptr(s string) *string { return &s }
func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// TestV0100AuthorityOverridesAndLiteralAlternates covers run's
// TARGET=client|daemon values against the assembled set, and the
// command-line literal keeping the inventory alternates behind it.
func TestV0100AuthorityOverridesAndLiteralAlternates(t *testing.T) {
	core := inventory.Device{Name: "Core-A", ManagementAddress: netip.MustParseAddr("127.0.0.1"), Addresses: []inventory.Address{{Address: netip.MustParseAddr("10.0.0.2"), Role: inventory.RoleAlternate}}}
	edge := inventory.Device{Name: "edge-b", ManagementAddress: netip.MustParseAddr("127.0.0.1")}
	for _, d := range []*inventory.Device{&core, &edge} {
		if err := d.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	set := TargetSet{Devices: []inventory.Device{core, edge}}
	overrides, err := applyAuthorityOverrides(&set, []string{"CORE-A=daemon", "name:edge-b=Client", "core-a=client"})
	if err != nil {
		t.Fatal(err)
	}
	if overrides["name:core-a"] != "client" || overrides["name:edge-b"] != "client" || set.Devices[0].AddressAuthority != "client" || set.Devices[1].AddressAuthority != "client" {
		t.Fatalf("overrides=%v devices=%+v", overrides, set.Devices)
	}
	for _, tc := range []struct{ value, code string }{
		{"ghost=daemon", "address_authority_target_unknown"},
		{"core-*=daemon", "address_authority_target_unknown"},
		{"core-a", "cli_option_value_invalid"},
		{"core-a=executor", "cli_option_value_invalid"},
		{"=daemon", "cli_option_value_invalid"},
	} {
		if _, err := applyAuthorityOverrides(&set, []string{tc.value}); errorcodes.Of(err) != tc.code {
			t.Errorf("%q: code=%q err=%v, want %s", tc.value, errorcodes.Of(err), err, tc.code)
		}
	}
	single := TargetSet{Devices: []inventory.Device{core}}
	if err := applyManagementAddress(&single, "192.0.2.5"); err != nil {
		t.Fatal(err)
	}
	d := single.Devices[0]
	if d.ManagementAddress != netip.MustParseAddr("192.0.2.5") || len(d.Alternates()) != 1 || d.Alternates()[0] != netip.MustParseAddr("10.0.0.2") || d.Addresses[0].Source != "cli" {
		t.Fatalf("literal applied: %+v", d)
	}
}

// TestAssemblePlatformSelectorKnown: a --select-platform selector must
// reach a known platform, checked
// in the assembly before the inventory is read. A literal must be a known
// name after normalisation, a glob must match at least one known name, and a
// "!" value is held to the same rule; the refusal is platform_selector_unknown
// (usage, exit 4) naming the value and the known platforms. With no
// inventory source configured, a value that passes reaches
// the loader and fails there, under a different code.
func TestAssemblePlatformSelectorKnown(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.C9300.driver="cisco_ios"`}})
	if err != nil {
		t.Fatal(err)
	}
	known := "generic, cisco_ios, cisco_xr, cisco_nxos, juniper_junos, arista_eos, linux, linux_shell, c9300"
	for _, tc := range []struct {
		value   string
		refused bool
	}{
		{"cisco_ios", false},
		{"C9300", false},       // the alias, whatever the table's spelling
		{" Cisco_IOS ", false}, // normalised before the lookup
		{"cisco_iosxe", false}, // a built-in's other spelling
		{"!CISCO_XE", false},
		{"cisco*", false},
		{"!cisco*", false}, // a not value over a known set
		{"[cg]*", false},
		{"cisco_iosx", true},
		{"nexus*", true},
		{"!cisco_iosx", true},
		{"!nexus*", true},
		{`\!c9300`, true}, // an escaped "!" is a literal rune of a glob matching no name
	} {
		t.Run(tc.value, func(t *testing.T) {
			inputs := []TargetInput{{Kind: "all"}, {Kind: "platform", Value: tc.value}}
			err := checkPlatformSelectors(cfg, inputs)
			_, assembled := assembleTargets(context.Background(), cfg, credentials.Operator{}, inputs, nil, io.Discard)
			if !tc.refused {
				if err != nil || errorcodes.Of(assembled) == "platform_selector_unknown" {
					t.Fatalf("refused: %v / %v", err, assembled)
				}
				return
			}
			msg := errorcodes.Message(err)
			if errorcodes.Of(err) != "platform_selector_unknown" || !strings.Contains(msg, fmt.Sprintf("--select-platform %q", tc.value)) || !strings.Contains(msg, "known platforms: "+known) || errorcodes.ExitAt(err, "platform_selector_unknown") != 4 {
				t.Fatalf("%v", err)
			}
			if errorcodes.Of(assembled) != "platform_selector_unknown" {
				t.Fatalf("assembly: %v", assembled)
			}
		})
	}
	// A malformed pattern keeps its own code, and a selector on another
	// field is not checked against the platforms.
	if err := checkPlatformSelectors(cfg, []TargetInput{{Kind: "platform", Value: "cis[co"}}); errorcodes.Of(err) != "target_selector_pattern_invalid" {
		t.Fatalf("malformed: %v", err)
	}
	if err := checkPlatformSelectors(cfg, []TargetInput{{Kind: "site", Value: "nexus*"}, {Kind: "target", Value: "cisco_iosx"}}); err != nil {
		t.Fatalf("other kinds: %v", err)
	}
}

// TestAssemblePlatformNotSet: a direct target from the target list has no
// platform, and a device
// whose platform is blank is never matched by a --select-platform selector, generic
// included, while it stays reachable by name and --all.
func TestAssemblePlatformNotSet(t *testing.T) {
	blank := invDevice("blank-1", "lab", "")
	generic := invDevice("gen-1", "lab", "generic")
	inv := []inventory.Device{blank, generic}
	got, err := assemble(inv, []TargetInput{{Kind: "target", Value: "r9"}, {Kind: "platform", Value: "generic"}, {Kind: "platform", Value: "*"}}, nil, setPlatformAsIs, testRules)
	if err != nil || !reflect.DeepEqual(names(got), []string{"r9", "gen-1"}) {
		t.Fatalf("names=%q err=%v", names(got), err)
	}
	if got[0].Platform != "" {
		t.Fatalf("direct target platform %q, want blank", got[0].Platform)
	}
	got, err = assemble(inv, []TargetInput{{Kind: "all"}, {Kind: "platform", Value: "!*"}}, nil, setPlatformAsIs, testRules)
	if err != nil || !reflect.DeepEqual(names(got), []string{"blank-1"}) {
		t.Fatalf("a not selector removes only set platforms: names=%q err=%v", names(got), err)
	}
	got, err = assemble(inv, []TargetInput{{Kind: "target", Value: "blank-1"}}, nil, setPlatformAsIs, testRules)
	if err != nil || len(got) != 1 || got[0].Platform != "" {
		t.Fatalf("by name: %+v %v", got, err)
	}
}

// TestAssemblePlatformFallbackNotMatched: a row naming an unknown
// platform is set as
// named under on-unknown = "fail", so a selector reaches it (and planning
// refuses it), and is not set for matching under "warn", so no --select-platform
// value selects or removes it; it stays reachable by name and --all.
func TestAssemblePlatformFallbackNotMatched(t *testing.T) {
	inv := []inventory.Device{invDevice("sw-ios", "lab", "cisco_ios"), invDevice("sw-typo", "lab", "cisco_iosx"), invDevice("sw-c9300", "lab", "c9300")}
	fail, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_ios"`}})
	if err != nil {
		t.Fatal(err)
	}
	warn, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_ios"`, `platform-resolution.on-unknown="warn"`}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		cfg    configload.Snapshot
		inputs []TargetInput
		want   []string
	}{
		{"cisco* under fail", fail, []TargetInput{{Kind: "platform", Value: "cisco*"}}, []string{"sw-ios", "sw-typo"}},
		{"cisco* under warn", warn, []TargetInput{{Kind: "platform", Value: "cisco*"}}, []string{"sw-ios"}},
		{"* under warn", warn, []TargetInput{{Kind: "platform", Value: "*"}}, []string{"sw-ios", "sw-c9300"}},
		{"not cisco* under warn", warn, []TargetInput{{Kind: "all"}, {Kind: "platform", Value: "!cisco*"}}, []string{"sw-typo", "sw-c9300"}},
		{"by name under warn", warn, []TargetInput{{Kind: "target", Value: "sw-typo"}}, []string{"sw-typo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assemble(inv, tc.inputs, nil, planner.SetPlatformFunc(tc.cfg), testRules)
			if err != nil || !reflect.DeepEqual(names(got), tc.want) {
				t.Fatalf("names=%q err=%v, want %q", names(got), err, tc.want)
			}
			for _, d := range got {
				if d.CanonicalName == "sw-typo" && d.Platform != "cisco_iosx" {
					t.Fatalf("the set platform changed: %q", d.Platform)
				}
			}
		})
	}
}

// TestAssembleCreatesRangeNames: a target name holding a numeric range
// stands for the words it spells, in written order, each as if typed in its
// place: an inventory device, a direct target recorded under the word as
// typed (input_target), or, for a word holding a glob, the inventory's
// matches; duplicates go, and "!" and --exclude still match.
func TestAssembleCreatesRangeNames(t *testing.T) {
	var inv []inventory.Device
	for _, n := range []string{"router01", "router02", "router03", "router05", "router10", "router11", "router12"} {
		inv = append(inv, invDevice(n, "nyc", "cisco_ios"))
	}
	direct := func(names ...string) map[string]bool {
		out := map[string]bool{}
		for _, n := range names {
			out[n] = true
		}
		return out
	}
	for _, tc := range []struct {
		name     string
		inputs   []TargetInput
		excludes []string
		want     []string
		supplied []string // SuppliedName, the records' input_target
		direct   map[string]bool
	}{
		{"switch0[1-3]", []TargetInput{{Kind: "target", Value: "switch0[1-3]"}}, nil,
			[]string{"switch01", "switch02", "switch03"}, []string{"switch01", "switch02", "switch03"}, direct("switch01", "switch02", "switch03")},
		{"the typed case kept", []TargetInput{{Kind: "target", Value: " Switch0[1-2] "}}, nil,
			[]string{"switch01", "switch02"}, []string{"Switch01", "Switch02"}, direct("switch01", "switch02")},
		{"router[1-12]", []TargetInput{{Kind: "target", Value: "router[1-12]"}}, nil,
			[]string{"router1", "router2", "router3", "router4", "router5", "router6", "router7", "router8", "router9", "router10", "router11", "router12"},
			[]string{"router1", "router2", "router3", "router4", "router5", "router6", "router7", "router8", "router9", "router10", "router11", "router12"},
			direct("router1", "router2", "router3", "router4", "router5", "router6", "router7", "router8", "router9")},
		{"inventory names among them", []TargetInput{{Kind: "target", Value: "ROUTER0[1-5]"}}, nil,
			[]string{"router01", "router02", "router03", "router04", "router05"}, []string{"router01", "router02", "router03", "ROUTER04", "router05"}, direct("router04")},
		{"a list, written order", []TargetInput{{Kind: "target", Value: "router[4,1]"}}, nil,
			[]string{"router4", "router1"}, []string{"router4", "router1"}, direct("router4", "router1")},
		{"a word holding a glob selects", []TargetInput{{Kind: "target", Value: "router[1,0]?"}}, nil,
			[]string{"router10", "router11", "router12", "router01", "router02", "router03", "router05"}, nil, direct()},
		{"duplicates removed", []TargetInput{{Kind: "target", Value: "r[1-2,2]"}, {Kind: "target", Value: "r1"}}, nil,
			[]string{"r1", "r2"}, nil, direct("r1", "r2")},
		{"a target file entry", []TargetInput{{Kind: "names", Names: []string{"edge[1-2]", "router10"}, Source: "f.txt"}}, nil,
			[]string{"edge1", "edge2", "router10"}, nil, direct("edge1", "edge2")},
		{"! and --exclude match", []TargetInput{{Kind: "target", Value: "router[1-12]"}, {Kind: "target", Value: "!router[2-9]"}}, []string{"router[10-11]"},
			[]string{"router1", "router12"}, nil, direct("router1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assemble(inv, tc.inputs, tc.excludes, setPlatformAsIs, testRules)
			if err != nil || !reflect.DeepEqual(names(got), tc.want) {
				t.Fatalf("names=%q err=%v, want %q", names(got), err, tc.want)
			}
			for i, d := range got {
				if tc.supplied != nil && d.SuppliedName() != tc.supplied[i] {
					t.Errorf("%s: input_target %q, want %q", d.CanonicalName, d.SuppliedName(), tc.supplied[i])
				}
				if isDirect := d.Source.Name == "direct"; isDirect != tc.direct[d.CanonicalName] {
					t.Errorf("%s: direct %v, want %v", d.CanonicalName, isDirect, !isDirect)
				}
			}
		})
	}
}

// TestCreatedNamesLimit: targets.max-created-names bounds the words one
// target name spells, brackets multiplying, counted without spelling them;
// the refusal names the value and stacks the count over the limit; a "!"
// name, a name without a range, and a selector option create nothing.
func TestCreatedNamesLimit(t *testing.T) {
	err := CheckCreatedNames("--tl", "box[78000-99999]", 1000)
	const want = "--tl box[78000-99999] creates too many names\n  names: 22000\n  limit: 1000 (targets.max-created-names)"
	if errorcodes.Of(err) != "target_created_names_exceeded" || !strings.HasSuffix(err.Error(), want) {
		t.Fatalf("CheckCreatedNames = %v, want target_created_names_exceeded ending %q", err, want)
	}
	for _, tc := range []struct {
		value string
		limit int
		ok    bool
	}{
		{"box[1-1000]", 1000, true},
		{"box[1-1001]", 1000, false},
		{"r[1-10][0-100]", 1000, false}, // 10 x 101
		{"r[1-10][1-100]", 1000, true},
		{"box[0-99999999999999999999999]", 100000, false},
		{"!box[1-99999]", 1, true},
		{"box*", 1, true},
		{"box01", 1, true},
		{"box[1-", 1, true}, // the selector check refuses it
	} {
		err := CheckCreatedNames("DEVICE", tc.value, tc.limit)
		if (err == nil) != tc.ok {
			t.Errorf("CheckCreatedNames(%q, %d) = %v, want ok %v", tc.value, tc.limit, err, tc.ok)
		}
	}
	if _, err := assemble(testInventory, []TargetInput{{Kind: "target", Value: "box[1-6]"}}, nil, setPlatformAsIs, targetRules{maxCreated: 5}); errorcodes.Of(err) != "target_created_names_exceeded" || !strings.Contains(err.Error(), "--target box[1-6] creates too many names") {
		t.Errorf("assemble over the limit: %v", err)
	}
	if got, err := assemble(testInventory, []TargetInput{{Kind: "target", Value: "box[1-5]"}}, nil, setPlatformAsIs, targetRules{maxCreated: 5}); err != nil || len(got) != 5 {
		t.Errorf("assemble at the limit: %q %v", names(got), err)
	}
	if _, err := assemble(testInventory, []TargetInput{{Kind: "names", Names: []string{"box[1-6]"}, Source: "f.txt"}}, nil, setPlatformAsIs, targetRules{maxCreated: 5}); !strings.Contains(fmt.Sprint(err), "target file f.txt entry box[1-6] creates too many names") {
		t.Errorf("a target file entry over the limit: %v", err)
	}
	if got, err := assemble(testInventory, []TargetInput{{Kind: "all"}, {Kind: "target", Value: "!core-[1-99]"}, {Kind: "site", Value: "pod[1-99]"}}, []string{"x[1-99]"}, setPlatformAsIs, targetRules{maxCreated: 5}); err != nil || len(got) != 2 {
		t.Errorf("matchers under a limit of 5: %q %v", names(got), err)
	}
}

// TestAssembleInventoryOnly: under targets.inventory-only nothing is made. A
// name holding a range or a glob selects the inventory's devices it matches,
// in inventory order as a glob does, by number and at any size (the limit
// not applied); a plain name the
// inventory lacks is gathered, once, as first typed, from every input, and
// after every input the gathered names refuse the set,
// target_not_in_inventory, before any empty-set code. A "!" name and
// --exclude only remove. Without the key the same names are direct targets
// (TestAssembleCreatesRangeNames).
func TestAssembleInventoryOnly(t *testing.T) {
	var inv []inventory.Device
	for _, n := range []string{"router01", "router02", "router03", "router05", "router10", "router11", "router12"} {
		inv = append(inv, invDevice(n, "nyc", "cisco_ios"))
	}
	only := targetRules{maxCreated: 5, inventoryOnly: true}
	for _, tc := range []struct {
		name     string
		inputs   []TargetInput
		excludes []string
		want     []string
	}{
		{"a range selects among its names", []TargetInput{{Kind: "target", Value: "router0[1-5]"}}, nil, []string{"router01", "router02", "router03", "router05"}},
		{"by number and width", []TargetInput{{Kind: "target", Value: "ROUTER[1-12]"}}, nil, []string{"router10", "router11", "router12"}},
		{"at any size", []TargetInput{{Kind: "target", Value: "router[1-99999999]"}}, nil, []string{"router10", "router11", "router12"}},
		{"in inventory order, as a glob", []TargetInput{{Kind: "target", Value: "router[11,5,01,10]"}}, nil, []string{"router01", "router10", "router11"}},
		{"a range and a glob", []TargetInput{{Kind: "target", Value: "router[1,0]?"}}, nil, []string{"router01", "router02", "router03", "router05", "router10", "router11", "router12"}},
		{"a plain inventory name", []TargetInput{{Kind: "names", Names: []string{"Router05", "router01"}, Source: "f.txt"}}, nil, []string{"router05", "router01"}},
		{"! and --exclude are not checked", []TargetInput{{Kind: "target", Value: "router0[1-3]"}, {Kind: "target", Value: "!router04"}, {Kind: "target", Value: "!router02"}}, []string{"router09", "router03"}, []string{"router01"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assemble(inv, tc.inputs, tc.excludes, setPlatformAsIs, only)
			if err != nil || !reflect.DeepEqual(names(got), tc.want) {
				t.Fatalf("names=%q err=%v, want %q", names(got), err, tc.want)
			}
			for _, d := range got {
				if d.Source.Name == "direct" {
					t.Errorf("%s is a direct target", d.CanonicalName)
				}
			}
		})
	}
	twelve := []string{}
	for i := 1; i <= 12; i++ {
		twelve = append(twelve, fmt.Sprintf("b%d", i))
	}
	for _, tc := range []struct {
		name   string
		inputs []TargetInput
		code   string
		want   string
	}{
		{"absent names, every input", []TargetInput{{Kind: "target", Value: "router04"}, {Kind: "target", Value: "router01"}, {Kind: "names", Names: []string{"Router07", "router04"}, Source: "f.txt"}}, "target_not_in_inventory",
			"2 target names are not in the inventory (targets.inventory-only)\n  router04\n  Router07"},
		{"one name, as first typed", []TargetInput{{Kind: "target", Value: "Router04"}, {Kind: "target", Value: "ROUTER04"}}, "target_not_in_inventory",
			"1 target name is not in the inventory (targets.inventory-only)\n  Router04"},
		{"after ten, the count left", []TargetInput{{Kind: "names", Names: twelve, Source: "f.txt"}}, "target_not_in_inventory",
			"12 target names are not in the inventory (targets.inventory-only)\n  b1\n  b2\n  b3\n  b4\n  b5\n  b6\n  b7\n  b8\n  b9\n  b10\n  and 2 more"},
		{"before an empty selection", []TargetInput{{Kind: "target", Value: "router04"}, {Kind: "site", Value: "nowhere"}}, "target_not_in_inventory",
			"1 target name is not in the inventory (targets.inventory-only)\n  router04"},
		{"a range matching none", []TargetInput{{Kind: "target", Value: "switch0[1-3]"}}, "inventory_empty_selection", "the target selectors matched no devices"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assemble(inv, tc.inputs, nil, setPlatformAsIs, only)
			if errorcodes.Of(err) != tc.code || !strings.HasSuffix(fmt.Sprint(err), tc.want) {
				t.Fatalf("names=%q err=%v, want %s ending %q", names(got), err, tc.code, tc.want)
			}
		})
	}
}
