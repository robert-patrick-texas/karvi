package configload

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestV0100AddressAuthorityKeys: the two address-authority keys load with
// their defaults, the authority enum takes client and daemon
// only, and the gate is a boolean.
func TestV0100AddressAuthorityKeys(t *testing.T) {
	load := func(sets ...string) (Snapshot, error) {
		return Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: sets})
	}
	snap, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.String("name.default-address-authority"); got != "client" {
		t.Fatalf("name.default-address-authority default=%q, want client", got)
	}
	if got := snap.Bool("name.allow-daemon-resolution"); got {
		t.Fatal("name.allow-daemon-resolution default=true, want false")
	}
	for _, value := range []string{"client", "daemon"} {
		snap, err := load(`name.default-address-authority="` + value + `"`)
		if err != nil || snap.String("name.default-address-authority") != value {
			t.Fatalf("%s: %v %q", value, err, snap.String("name.default-address-authority"))
		}
	}
	if snap, err := load(`name.allow-daemon-resolution=true`); err != nil || !snap.Bool("name.allow-daemon-resolution") {
		t.Fatalf("gate enable: %v", err)
	}
	for _, tc := range []struct{ set, code string }{
		{`name.default-address-authority="executor"`, "config_enum_value_invalid"},
		{`name.default-address-authority="bogus"`, "config_enum_value_invalid"},
		{`name.allow-daemon-resolution="yes"`, "config_type_error"},
		{`name.allow-executor-resolution=true`, "config_unknown_key"},
	} {
		_, err := load(tc.set)
		if errorcodes.Of(err) != tc.code {
			t.Fatalf("%s: code=%q err=%v, want %s", tc.set, errorcodes.Of(err), err, tc.code)
		}
	}
}
