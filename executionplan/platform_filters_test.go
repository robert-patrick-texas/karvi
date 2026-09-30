package executionplan

import (
	"strings"
	"testing"
)

// TestPlatformFiltersValidate covers the platform filters at the
// plan: platform_filters needs a collection, every pattern compiles, and a
// good list on a collection passes.
func TestPlatformFiltersValidate(t *testing.T) {
	p := fixtureDraftPlan(t)
	p.PlatformFilters = map[string][]string{"cisco_iosxe": {`^Building configuration\.\.\.$`}}
	if err := p.Validate(Draft); err == nil || !strings.Contains(err.Error(), "platform_filters") || !strings.Contains(err.Error(), "without a collection") {
		t.Fatalf("filters without a collection: %v", err)
	}
	p.Output.Collection = &CollectionSettings{Directory: "/srv/karvi/crun", FileMode: "0660"}
	if err := p.Validate(Draft); err != nil {
		t.Fatalf("a good list on a collection: %v", err)
	}
	p.PlatformFilters["cisco_iosxe"] = append(p.PlatformFilters["cisco_iosxe"], "(")
	if err := p.Validate(Draft); err == nil || !strings.Contains(err.Error(), "pattern 2 does not compile") {
		t.Fatalf("a pattern that does not compile: %v", err)
	}
}
