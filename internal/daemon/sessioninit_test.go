package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// TestCommitRefusesAnEditedSessionInitTable: the daemon reads no
// session-init configuration and checks the
// plan's table at commit_job, naming the field; the unedited plan then
// commits and completes.
func TestCommitRefusesAnEditedSessionInitTable(t *testing.T) {
	f := newV5Fixture(t)
	sub := f.prepareAndPackage(mustID(t), []string{"show clock"})
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	for _, target := range sub.final.Targets {
		if target.SessionInitProfile != executionplan.SessionInitNone {
			t.Fatalf("%s: profile %q without a map", target.TargetID, target.SessionInitProfile)
		}
	}
	profile := executionplan.SessionInitProfile{Commands: []string{"show clock"}, OnError: executionplan.SessionInitFailDevice}
	cases := []struct {
		name string
		edit func(*executionplan.ExecutionPlan)
		want string
	}{
		{"unselected entry", func(p *executionplan.ExecutionPlan) { p.SessionInit["spare"] = profile }, `session_init["spare"]: no target selects it`},
		{"unknown profile", func(p *executionplan.ExecutionPlan) { p.Targets[1].SessionInitProfile = "ghost" }, `targets[1].session_init_profile: "ghost" is not none or a profile in session_init`},
		{"on_error", func(p *executionplan.ExecutionPlan) {
			bad := profile
			bad.OnError = "abort"
			p.SessionInit["p"] = bad
			p.Targets[0].SessionInitProfile = "p"
		}, `session_init["p"].on_error: "abort" is not fail-device or continue`},
		{"missing profile", func(p *executionplan.ExecutionPlan) { p.Targets[0].SessionInitProfile = "" }, "session_init_profile: is required before commit"},
	}
	for _, c := range cases {
		request := sub.request
		plan := sub.final
		plan.Targets = append([]executionplan.ExecutionTarget(nil), sub.final.Targets...)
		plan.SessionInit = map[string]executionplan.SessionInitProfile{}
		c.edit(&plan)
		refinalized, err := executionplan.Finalize(plan, plan.Planning.FinalizedAt.Add(time.Nanosecond))
		if err != nil {
			t.Fatal(err)
		}
		request.Plan = refinalized
		_, err = CommitJob(f.ctx(), f.socket, 1<<20, request)
		if errorcodes.Of(err) != "execution_plan_invalid" || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want execution_plan_invalid naming %s, got %v", c.name, c.want, err)
		}
	}
	result, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if err != nil {
		t.Fatalf("the unedited plan: %v", err)
	}
	if terminal := f.followToEnd(result.Receipt.JobID); terminal.Outcome.ExitCode != 0 {
		t.Fatalf("the unedited plan did not complete: %+v", terminal.Outcome)
	}
}

// TestExerciseReportsTheSelectedProfiles is the session-init exercise
// over the daemon: the client selects from its map (the daemon-authority target
// after prepare_job), the committed plan carries the table, the report shows
// each target's profile, and nothing is sent.
func TestExerciseReportsTheSelectedProfiles(t *testing.T) {
	seed := canarytest.Seed(t)
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true, Sets: []string{
		`security.child-environment-allowlist=["KARVI_TEST_FAKE_DIR"]`,
		`session-init.core-init.commands=["terminal width 511", "show clock"]`, `session-init.core-init.on-error="continue"`, `session-init.core-init.command-timeout="20s"`,
		`session-init.quiet.commands=[]`,
		`session-init-map.0.profile="core-init"`, `session-init-map.0.name="core-*"`,
		`session-init-map.1.profile="quiet"`, `session-init-map.1.name="*"`,
	}})
	fakeDir := t.TempDir()
	t.Setenv("KARVI_TEST_FAKE_DIR", fakeDir)
	sub, result, terminal, report := f.exercise(mustID(t), []string{"show version"}, []inventory.Device{direct("127.0.0.1"), direct("core-a.example")}, seed)
	want := map[string]string{"name:127.0.0.1": "quiet", "name:core-a.example": "core-init"}
	for i, tr := range report.Targets {
		if tr.IntendedTransport.SessionInitProfile != want[tr.TargetID] || report.Plan.Targets[i].SessionInitProfile != want[tr.TargetID] {
			t.Errorf("%s: report %q, plan %q, want %q", tr.TargetID, tr.IntendedTransport.SessionInitProfile, report.Plan.Targets[i].SessionInitProfile, want[tr.TargetID])
		}
	}
	table := map[string]executionplan.SessionInitProfile{
		"core-init": {Commands: []string{"terminal width 511", "show clock"}, OnError: executionplan.SessionInitContinue, CommandTimeoutNS: int64(20 * time.Second)},
		"quiet":     {Commands: []string{}, OnError: executionplan.SessionInitFailDevice},
	}
	if !reflect.DeepEqual(report.Plan.SessionInit, table) || !reflect.DeepEqual(sub.final.SessionInit, table) {
		t.Fatalf("table: report %+v, plan %+v", report.Plan.SessionInit, sub.final.SessionInit)
	}
	if terminal.Outcome.ExitCode != 0 || report.DeviceContacted {
		t.Fatalf("outcome %+v contacted=%v", terminal.Outcome, report.DeviceContacted)
	}
	if b, _ := os.ReadFile(filepath.Join(result.Receipt.ArtifactDir, "commands.jsonl")); len(b) != 0 {
		t.Fatalf("commands.jsonl is not empty: %s", b)
	}
	if _, err := os.Stat(filepath.Join(fakeDir, "log")); err == nil {
		t.Fatal("the fake device saw a session")
	}
}
