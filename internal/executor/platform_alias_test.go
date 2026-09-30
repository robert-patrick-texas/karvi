package executor

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestAliasRunsAsItsBuiltIn: a
// [platform.c9300] alias of cisco_iosxe gets the built-in's paging commands
// and failure patterns, and without an enable secret a device already at
// the privileged prompt is sent no enable.
func TestAliasRunsAsItsBuiltIn(t *testing.T) {
	h := newSessionHarness(t, []string{"show clock", "show bogus"}, false)
	h.withAlgorithmConfig(`platform.c9300.driver="cisco_iosxe"`, `execution.command-timeout="1s"`)
	h.target.Device.Platform = "c9300"
	res, recs := h.run(context.Background())
	if res.Success || res.ErrorCode != "device_command_error" {
		t.Fatalf("result %+v", res)
	}
	if recs[0].Platform != "c9300" || recs[0].Status != "succeeded" || recs[1].Error == nil || recs[1].Error.Code != "device_command_error" {
		t.Fatalf("records %+v / %+v", recs[0], recs[1].Error)
	}
	data, _ := os.ReadFile(h.log)
	if got := strings.Join(strings.Fields(strings.ReplaceAll(string(data), "\n", "|")), " "); !strings.HasPrefix(got, "terminal length 0|terminal width 512|show clock|show bogus") || strings.Contains(got, "enable") {
		t.Fatalf("device saw %q", got)
	}
}
