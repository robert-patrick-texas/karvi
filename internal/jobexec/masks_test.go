package jobexec

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/platform"
)

// TestJobMasks: a job's one list is the plan's masks of every target
// platform together, a pattern two platforms share compiled once, so that
// each platform's statement is masked in every event of a job across
// platforms (docs/EXAMPLES.md, chapter 63, issue 8); no lists are none; a
// pattern the plan should have refused is the plan's fault.
func TestJobMasks(t *testing.T) {
	iosxe, _ := platform.Builtin("cisco_ios")
	iosxr, _ := platform.Builtin("cisco_xr")
	junos, _ := platform.Builtin("juniper_junos")
	res, err := jobMasks(map[string][]string{"cisco_ios": iosxe.Masks, "juniper_junos": junos.Masks})
	if err != nil || len(res) != len(iosxe.Masks)+len(junos.Masks) {
		t.Fatalf("the union: %d %v", len(res), err)
	}
	for _, c := range [][2]string{
		{"show clock", "show clock"},
		{"snmp-server community Publ1c RO", "snmp-server community <removed> RO"},
		{"set snmp community Publ1c authorization read-only", "set snmp community <removed> authorization read-only"},
	} {
		if got := platform.Mask(res, c[0]); got != c[1] {
			t.Fatalf("%q: %q, want %q", c[0], got, c[1])
		}
	}
	// IOS XE and IOS XR share their URL rule.
	if res, err = jobMasks(map[string][]string{"cisco_ios": iosxe.Masks, "cisco_xr": iosxr.Masks}); err != nil || len(res) != len(iosxe.Masks)+len(iosxr.Masks)-1 {
		t.Fatalf("a shared pattern once: %d %v", len(res), err)
	}
	if res, err = jobMasks(nil); res != nil || err != nil {
		t.Fatalf("no lists: %v %v", res, err)
	}
	if _, err = jobMasks(map[string][]string{"linux": {`--password=\S+`}}); err == nil || !strings.Contains(err.Error(), "execution_plan_invalid") || !strings.Contains(err.Error(), "platform linux pattern 1 holds no capturing group") {
		t.Fatalf("a pattern with no group: %v", err)
	}
}
