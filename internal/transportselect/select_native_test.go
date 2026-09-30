package transportselect

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
)

// The native executable carries scrapligo-v1: the run default, the
// preferred synonym, the native slot, and the implementation ID all
// resolve to it.
func TestNativeRunDefaultResolvesToScrapliGoV1(t *testing.T) {
	cfg := lazySystemConfig(t)
	got, err := Resolve(cfg, "run", "default")
	if err != nil {
		t.Fatalf("resolve run default: %v", err)
	}
	if got.Kind != KindNative || got.Implementation != "scrapligo-v1" || got.Selector != "native" {
		t.Fatalf("unexpected run default selection: %#v", got)
	}
}

func TestNativePreferredAliasesNativeSlot(t *testing.T) {
	cfg := defaultConfig(t)
	preferred, err := Resolve(cfg, "command", "preferred")
	if err != nil {
		t.Fatalf("resolve preferred: %v", err)
	}
	native, err := Resolve(cfg, "command", "native")
	if err != nil {
		t.Fatalf("resolve native: %v", err)
	}
	if preferred != native {
		t.Fatalf("preferred and native differ: %#v vs %#v", preferred, native)
	}
	if native.Kind != KindNative || native.Implementation != "scrapligo-v1" || native.ConfigKey != "ssh.transports.native" {
		t.Fatalf("unexpected native selection: %#v", native)
	}
}

func TestNativeImplementationSelectableByID(t *testing.T) {
	cfg := defaultConfig(t)
	got, err := Resolve(cfg, "command", "scrapligo-v1")
	if err != nil {
		t.Fatalf("resolve scrapligo-v1: %v", err)
	}
	if got.Kind != KindNative || got.Implementation != "scrapligo-v1" || got.Selector != "scrapligo-v1" {
		t.Fatalf("unexpected selection: %#v", got)
	}
}

// A native slot that names an implementation this executable does not carry
// fails closed, and never falls back to OpenSSH. Until the
// build tag was removed this path was the preview executable's; it remains
// for any identifier without a registered provider, such as a later adapter
// named in a configuration written for a newer karvi.
func TestUnregisteredNativeImplementationFailsClosed(t *testing.T) {
	cfg, err := configload.Load(configload.Options{
		InternalOnly: true,
		Environment:  []string{},
		Sets:         []string{`ssh.transports.native="scrapligo-v9"`},
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	for _, selector := range []string{"native", "preferred"} {
		if _, err := Resolve(cfg, "command", selector); err == nil || !strings.Contains(err.Error(), "native_transport_unavailable") || !strings.Contains(err.Error(), "scrapligo-v9") {
			t.Fatalf("%s: want native_transport_unavailable naming scrapligo-v9, got %v", selector, err)
		}
	}
	// An operator-authored mapping to an absent implementation is never lazy.
	if err := ValidateConfigured(cfg); err == nil || !strings.Contains(err.Error(), "native_transport_unavailable") {
		t.Fatalf("config validation should refuse the mapping, got %v", err)
	}
}
