package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestCheckPlatformOption:
// --platform in login and command is a known platform name, normalised; an
// empty value passes; a glob character, a leading "!", or an unknown name is
// platform_option_unknown with the value and the known platforms named.
func TestCheckPlatformOption(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.C9300.driver="cisco_iosxe"`}})
	if err != nil {
		t.Fatal(err)
	}
	known := "generic, cisco_iosxe, cisco_iosxr, cisco_nxos, juniper_junos, arista_eos, linux, c9300"
	for _, tc := range []struct {
		value, want, refusal string
	}{
		{"", "", ""},
		{"   ", "", ""},
		{"cisco_iosxe", "cisco_iosxe", ""},
		{" Cisco_IOSXE ", "cisco_iosxe", ""}, // [2] P6: trimmed and lowercased
		{"C9300", "c9300", ""},               // [2] P7: the alias, whatever the table's spelling
		{"generic", "generic", ""},
		{"cisco_iosx", "", "is not a known platform"},                      // [2] P3
		{"*", "", "a platform name is required, not a pattern"},            // [2] P4
		{"!cisco_iosxe", "", "a platform name is required, not a pattern"}, // [2] P5
		{"cisco?", "", "not a pattern"},
		{"c[9]300", "", "not a pattern"},
		{`c9300\`, "", "not a pattern"},
	} {
		got, err := CheckPlatformOption(cfg, tc.value)
		if tc.refusal == "" {
			if err != nil || got != tc.want {
				t.Fatalf("%q: got %q, %v; want %q", tc.value, got, err, tc.want)
			}
			continue
		}
		if err == nil || got != "" {
			t.Fatalf("%q: accepted as %q", tc.value, got)
		}
		msg := errorcodes.Message(err)
		if errorcodes.Of(err) != "platform_option_unknown" || !strings.Contains(msg, tc.refusal) || !strings.Contains(msg, fmt.Sprintf("--platform %q", tc.value)) || !strings.Contains(msg, "known platforms: "+known) {
			t.Fatalf("%q: %s", tc.value, msg)
		}
		if errorcodes.ExitAt(err, "platform_option_unknown") != 4 {
			t.Fatalf("%q: exit %d", tc.value, errorcodes.ExitAt(err, "platform_option_unknown"))
		}
	}
}
