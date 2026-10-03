package planner

import (
	"context"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
)

// TestDraftPlatformFilters covers the crun-filters lists in the planner:
// a crun's plan carries each target platform's crun-filters (a
// site's table replacing the built-in list, an empty array leaving the
// platform out), a run without a collection or with one carries none, and
// the draft validates.
func TestDraftPlatformFilters(t *testing.T) {
	operator := credentials.Operator{Username: "netops", UID: 1000, PrimaryGID: 1000, Home: t.TempDir()}
	opts := draftOptions(nil)
	opts.PlatformCommands, opts.Collection = true, "crun"
	cfg := testConfig(t, `platform.generic.crun-commands=["show version"]`, `platform.generic.crun-filters=['^Building configuration\.\.\.$', ' uptime is ']`)
	draft, err := Draft(context.Background(), cfg, operator, k03Set(t), opts, plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	if got := draft.PlatformFilters["generic"]; len(got) != 2 || got[1] != " uptime is " {
		t.Fatalf("the plan's list: %v", draft.PlatformFilters)
	}
	if err := draft.Validate(executionplan.Draft); err != nil {
		t.Fatal(err)
	}
	cfg = testConfig(t, `platform.generic.crun-commands=["show version"]`, `platform.generic.crun-filters=[]`)
	if draft, err = Draft(context.Background(), cfg, operator, k03Set(t), opts, plantest.DraftedAt); err != nil || draft.PlatformFilters != nil {
		t.Fatalf("an empty array leaves the platform out: %v %v", draft.PlatformFilters, err)
	}
	opts.Collection = ""
	cfg = testConfig(t, `platform.generic.crun-commands=["show version"]`, `platform.generic.crun-filters=[' uptime is ']`)
	if draft, err = Draft(context.Background(), cfg, operator, k03Set(t), opts, plantest.DraftedAt); err != nil || draft.PlatformFilters != nil {
		t.Fatalf("a run without a collection carries none: %v %v", draft.PlatformFilters, err)
	}
	// A run's collection (--cd) carries none either: the filters are a
	// crun's.
	run := draftOptions(plantest.Commands[:1])
	run.Collection = "run"
	if draft, err = Draft(context.Background(), cfg, operator, k03Set(t), run, plantest.DraftedAt); err != nil || draft.PlatformFilters != nil || draft.Output.Collection == nil {
		t.Fatalf("a run's collection carries no filters: %v %v %+v", draft.PlatformFilters, err, draft.Output.Collection)
	}
}
