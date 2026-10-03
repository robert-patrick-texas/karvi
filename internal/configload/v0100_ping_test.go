package configload

import (
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestV0100PingKeys: the five
// keys with their defaults, the timeout range rule, and the rule that at
// least one ICMP method remains while pinging is enabled.
func TestV0100PingKeys(t *testing.T) {
	load := func(sets ...string) (Snapshot, error) {
		return Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: sets})
	}
	snap, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Bool("network.ping-targets") || snap.Duration("network.ping-timeout") != 500*time.Millisecond || !snap.Bool("network.ping-socket") || !snap.Bool("network.ping-system") || snap.String("display.ping.header") != "! <target> [<address>] ping(1) <rtt1>, ping(2) <rtt2>, <result>" {
		t.Errorf("defaults: targets=%v timeout=%s socket=%v system=%v display=%v", snap.Bool("network.ping-targets"), snap.Duration("network.ping-timeout"), snap.Bool("network.ping-socket"), snap.Bool("network.ping-system"), snap.String("display.ping.header"))
	}
	for _, sets := range [][]string{
		{`network.ping-targets=true`},
		{`network.ping-timeout="10s"`},
		{`network.ping-timeout="2ms"`},
		{`network.ping-targets=true`, `network.ping-socket=false`},
		{`network.ping-targets=true`, `network.ping-system=false`},
		{`network.ping-socket=false`, `network.ping-system=false`}, // harmless while pinging is off
		{`display.ping.header=""`},
	} {
		if _, err := load(sets...); err != nil {
			t.Errorf("%v: %v", sets, err)
		}
	}
	for _, tc := range []struct {
		sets []string
		code string
	}{
		{[]string{`network.ping-timeout="0s"`}, "config_value_out_of_range"},
		{[]string{`network.ping-timeout="10.001s"`}, "config_value_out_of_range"},
		{[]string{`network.ping-timeout="-1s"`}, "config_duration_negative"},
		{[]string{`network.ping-targets=true`, `network.ping-socket=false`, `network.ping-system=false`}, "config_ping_methods_disabled"},
		{[]string{`network.ping-targets="yes"`}, "config_type_error"},
	} {
		_, err := load(tc.sets...)
		if got := errorcodes.Of(err); got != tc.code {
			t.Errorf("%v: code %q, want %s (%v)", tc.sets, got, tc.code, err)
		}
	}
	// The environment mapping and the flag layer reach the same keys.
	snap, err = Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{"KARVI__NETWORK__PING_TARGETS=true"}, FlagValues: cliFlags(map[string]any{"network.ping-targets": false})})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Bool("network.ping-targets") {
		t.Error("the cli layer did not outrank the environment")
	}
	if src := snap.Values["network.ping-targets"].Source.Layer; src != "cli" {
		t.Errorf("source layer %q, want cli", src)
	}
}
