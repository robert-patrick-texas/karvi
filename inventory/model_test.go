package inventory

import "testing"

// TestPlatformNotSetStaysBlank: a direct
// target without a platform and a device validated without one keep a blank
// platform (not set); the planner resolves it, not the model.
func TestPlatformNotSetStaysBlank(t *testing.T) {
	d := Direct("r1", "", "", 0)
	if d.Platform != "" || d.Transport != "system" {
		t.Fatalf("direct: %+v", d)
	}
	if err := d.Validate(); err != nil || d.Platform != "" {
		t.Fatalf("validate: %v %+v", err, d)
	}
	if got := Direct("r1", " Cisco_IOSXE ", "", 0).Platform; got != "cisco_iosxe" {
		t.Fatalf("set platform: %q", got)
	}
}
