package transportselect

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// native and telnet resolve to their compiled-in transports with no slot:
// the run default, and either named.
func TestNativeAndTelnetResolveDirectly(t *testing.T) {
	cfg := lazySystemConfig(t)
	for _, c := range []struct{ mode, requested, kind string }{
		{"run", "default", KindNative},
		{"command", "native", KindNative},
		{"command", "NATIVE", KindNative},
		{"run", "telnet", KindTelnet},
	} {
		got, err := Resolve(cfg, c.mode, c.requested)
		if err != nil {
			t.Fatalf("%s %s: %v", c.mode, c.requested, err)
		}
		if got != (Selection{Selector: c.kind, Kind: c.kind}) {
			t.Fatalf("%s %s: %#v", c.mode, c.requested, got)
		}
	}
}

// preferred and scrapligo-v1 are ordinary slot names, missing unless a slot
// maps them.
func TestRemovedSelectorsAreSlotNames(t *testing.T) {
	cfg := defaultConfig(t)
	for _, selector := range []string{"preferred", "scrapligo-v1"} {
		_, err := Resolve(cfg, "command", selector)
		if errorcodes.Of(err) != "transport_mapping_missing" || !strings.Contains(err.Error(), "ssh.transports."+selector) {
			t.Fatalf("%s: %v", selector, err)
		}
	}
}

// An ssh.transports entry that cannot be used is refused at load: a custom
// slot not exec:PATH, and a slot named for a built-in transport, whatever
// its value. A custom slot mapped to default is refused when selected.
func TestMappingInvalidRefusedAtLoad(t *testing.T) {
	for _, c := range []struct{ set, code, want string }{
		{`ssh.transports.native="exec:/bin/true"`, "transport_mapping_invalid", "ssh.transports.native: native is a built-in transport, not a slot"},
		{`ssh.transports.native="default"`, "transport_mapping_invalid", "ssh.transports.native: native is a built-in transport"},
		{`ssh.transports.telnet="exec:/bin/true"`, "transport_mapping_invalid", "ssh.transports.telnet: telnet is a built-in transport"},
		{`ssh.transports.alt="builtin:native"`, "transport_mapping_invalid", `ssh.transports.alt="builtin:native" is not exec:PATH`},
		{`ssh.transports.alt="scrapligo-v1"`, "transport_mapping_invalid", `ssh.transports.alt="scrapligo-v1" is not exec:PATH`},
	} {
		cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{c.set}})
		if err != nil {
			t.Fatalf("%s: load: %v", c.set, err)
		}
		err = ValidateConfigured(cfg)
		if errorcodes.Of(err) != c.code || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v", c.set, err)
		}
	}

	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`ssh.transports.alt="default"`}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfigured(cfg); err != nil {
		t.Fatalf("an unselected default slot: %v", err)
	}
	_, err = Resolve(cfg, "command", "alt")
	if errorcodes.Of(err) != "transport_custom_slot_default" || !strings.Contains(err.Error(), "ssh.transports.alt may not use default, which only the system slot accepts") {
		t.Fatalf("a selected default slot: %v", err)
	}
}
