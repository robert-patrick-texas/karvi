package executionplan

import (
	"strings"
	"testing"
)

// TestPlatformMasksValidate covers the masks at the plan: a list needs no
// collection, every pattern compiles and holds a capturing group.
func TestPlatformMasksValidate(t *testing.T) {
	p := fixtureDraftPlan(t)
	p.PlatformMasks = map[string][]string{"cisco_ios": {`(?i)^\s*snmp-s\S*\s+comm\S*\s+(\S+)`}}
	if err := p.Validate(Draft); err != nil {
		t.Fatalf("a good list without a collection: %v", err)
	}
	p.PlatformMasks["cisco_ios"] = append(p.PlatformMasks["cisco_ios"], "(")
	if err := p.Validate(Draft); err == nil || !strings.Contains(err.Error(), "platform_masks") || !strings.Contains(err.Error(), "pattern 2 does not compile") {
		t.Fatalf("a pattern that does not compile: %v", err)
	}
	p.PlatformMasks["cisco_ios"][1] = `^enable secret \S+`
	if err := p.Validate(Draft); err == nil || !strings.Contains(err.Error(), "pattern 2 holds no capturing group") {
		t.Fatalf("a pattern with no group: %v", err)
	}
}
