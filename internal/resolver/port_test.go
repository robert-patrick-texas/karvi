package resolver

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// TestEffectivePortResolvesAliases: an alias
// has its built-in's ports unless its own table sets one.
func TestEffectivePortResolvesAliases(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
		`platform.c9300.driver="cisco_iosxe"`, `platform.c9300.ssh-port=2222`,
		`platform.edge.driver="cisco_iosxe"`, `platform.cisco_iosxe.ssh-port=2200`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		platform, transport string
		port                uint16
	}{
		{"c9300", "system", 2222},
		{"edge", "system", 22},
		{"edge", "telnet", 23},
		{"cisco_iosxe", "system", 2200},
		{"unknown", "system", 22},
	} {
		d := inventory.Direct("192.0.2.1", tc.platform, tc.transport, 0)
		if got := EffectivePort(d, tc.transport, cfg); got != tc.port {
			t.Fatalf("%s over %s: %d, want %d", tc.platform, tc.transport, got, tc.port)
		}
	}
}
