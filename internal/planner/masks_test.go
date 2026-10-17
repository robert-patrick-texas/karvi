package planner

import (
	"context"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/platform"
)

// TestDraftPlatformMasks covers the masks in the planner: every job's plan
// carries each target platform's masks, a run without a collection among
// them (a site's table replacing the built-in list, an empty array or a
// platform without a list left out); a job across platforms carries each
// one's list; the draft validates.
func TestDraftPlatformMasks(t *testing.T) {
	operator := credentials.Operator{Username: "netops", UID: 1000, PrimaryGID: 1000, Home: t.TempDir()}
	opts := draftOptions(plantest.Commands[:1])
	draft, err := Draft(context.Background(), testConfig(t), operator, k03Set(t), opts, plantest.DraftedAt)
	if err != nil || draft.PlatformMasks != nil {
		t.Fatalf("generic has no list: %v %v", draft.PlatformMasks, err)
	}
	cfg := testConfig(t, `platform.generic.masks=['^enable secret (\S+)']`)
	if draft, err = Draft(context.Background(), cfg, operator, k03Set(t), opts, plantest.DraftedAt); err != nil {
		t.Fatal(err)
	}
	if got := draft.PlatformMasks["generic"]; len(got) != 1 || got[0] != `^enable secret (\S+)` || draft.Output.Collection != nil {
		t.Fatalf("a run's plan carries the site's list: %v", draft.PlatformMasks)
	}
	if err := draft.Validate(executionplan.Draft); err != nil {
		t.Fatal(err)
	}
	cfg = testConfig(t, `platform.generic.masks=[]`)
	if draft, err = Draft(context.Background(), cfg, operator, k03Set(t), opts, plantest.DraftedAt); err != nil || draft.PlatformMasks != nil {
		t.Fatalf("an empty array leaves the platform out: %v %v", draft.PlatformMasks, err)
	}
	// A job across platforms: each built-in list, generic's none.
	set := k03Set(t)
	set.Devices[0].Platform, set.Devices[1].Platform = "cisco_iosxe", "juniper_junos"
	if draft, err = Draft(context.Background(), testConfig(t), operator, set, opts, plantest.DraftedAt); err != nil {
		t.Fatal(err)
	}
	iosxe, _ := platform.Builtin("cisco_iosxe")
	junos, _ := platform.Builtin("juniper_junos")
	if len(draft.PlatformMasks) != 2 || len(draft.PlatformMasks["cisco_iosxe"]) != len(iosxe.Masks) || len(draft.PlatformMasks["juniper_junos"]) != len(junos.Masks) {
		t.Fatalf("each target platform's list: %v", draft.PlatformMasks)
	}
	if err := draft.Validate(executionplan.Draft); err != nil {
		t.Fatal(err)
	}
}
