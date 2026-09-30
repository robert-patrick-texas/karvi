package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// The v0.9.0 operator form put options after freeform words. Under the
// current grammar those words are device text; the options belong before
// the device. Both facts are asserted here.
func TestV090TrailingOptionsAreDeviceTextUnderK03(t *testing.T) {
	inv, err := Parse([]string{"command", "vzn-ohio", "show clock", "show clock", "--border", "--transport", "system", "--echo"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(inv.Commands, "|"), "show clock show clock --border --transport system --echo"; got != want {
		t.Fatalf("commands=%q, want %q", got, want)
	}
	if inv.Flag(optEcho) || inv.Flag(optBorder) || inv.Set(optTransport) {
		t.Fatal("options after freeform must not be recognized")
	}
	inv, err = Parse([]string{"command", "--border", "--transport", "system", "--echo", "vzn-ohio", "show clock", "show clock"})
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Flag(optEcho) || !inv.Flag(optBorder) || inv.String(optTransport) != "system" || inv.Targets[0].Value != "vzn-ohio" || inv.Commands[0] != "show clock show clock" {
		t.Fatalf("inv=%+v", inv)
	}
}

func TestV090AddressFamilyAliasesNormalize(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want *option
	}{
		{args: []string{"command", "router1", "--4", "show", "clock"}, want: optIPv4},
		{args: []string{"command", "router1", "-6", "show", "clock"}, want: optIPv6},
		{args: []string{"--4", "command", "router1", "show", "clock"}, want: optIPv4},
	} {
		inv, err := Parse(tc.args)
		if err != nil {
			t.Fatal(err)
		}
		if !inv.Flag(tc.want) {
			t.Fatalf("args=%v: --%s not set", tc.args, tc.want.name)
		}
	}
}

func TestV090AddressFamilyPreferenceMutualExclusionAllConnectionModes(t *testing.T) {
	cases := [][]string{
		{"login", "--host", "router1", "--ipv4", "--ipv6"},
		{"command", "--host", "router1", "--4", "--6", "--cmd", "show clock"},
		{"run", "--no-daemon", "--target", "router1", "--ipv4", "--6", "--cmd", "show clock"},
		{"--4", "--6", "login", "router1"},
		{"--4", "login", "router1", "-6"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := Main(args, strings.NewReader(""), &stdout, &stderr)
			if got != exitcode.ExitUsageError {
				t.Fatalf("exit=%d, want %d; stderr=%q", got, exitcode.ExitUsageError, stderr.String())
			}
			if !strings.HasPrefix(stderr.String(), "address_family_conflict: ") || !strings.Contains(stderr.String(), "mutually exclusive") {
				t.Fatalf("stderr=%q", stderr.String())
			}
		})
	}
}

func TestV090AddressFamilyPreferenceUsesLockAwareConfigFlag(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		preference string
	}{
		{args: []string{"--ipv4", "command", "r1", "show clock"}, preference: "ipv4"},
		{args: []string{"command", "r1", "--ipv4", "show clock"}, preference: "ipv4"},
		{args: []string{"--6", "command", "r1", "show clock"}, preference: "ipv6"},
		{args: []string{"command", "-6", "r1", "show clock"}, preference: "ipv6"},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			inv, err := Parse(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if got := inv.common().ConfigFlags["name.address-family-preference"]; got != tc.preference {
				t.Fatalf("preference=%v, want %s", got, tc.preference)
			}
		})
	}
}

func TestV090BorderFlagsAreMutuallyExclusive(t *testing.T) {
	cases := [][]string{
		{"command", "--host", "router1", "--cmd", "show clock", "--border", "--noborder"},
		{"run", "--target", "router1", "--cmd", "show clock", "--border", "--noborder"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := Main(args, strings.NewReader(""), &stdout, &stderr)
			if got != exitcode.ExitUsageError || !strings.HasPrefix(stderr.String(), "border_options_conflict: ") {
				t.Fatalf("exit=%d stderr=%q", got, stderr.String())
			}
		})
	}
}
