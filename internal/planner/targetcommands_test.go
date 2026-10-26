package planner

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestTargetCommandLists covers each target's list under a --cf folder
// (EXAMPLES chapter 79): built by its name in the folder once the targets
// are known; the targets a folder holds no file for refused together, ten
// named and the rest counted; two targets with one name refused before any
// list is built; any other failure naming its target; and each list's own
// bounds checked.
func TestTargetCommandLists(t *testing.T) {
	cfg := testConfig(t, "output.max-command-bytes=2048", "output.max-job-bytes=4096")
	target := func(id, name string) executionplan.ExecutionTarget {
		return executionplan.ExecutionTarget{TargetID: id, Device: executionplan.DeviceProjection{CanonicalName: name}}
	}
	list := func(commands ...string) executionplan.CommandList {
		return executionplan.CommandList{Commands: commands, BlindReturns: []int{}, Blind: []bool{}, Expectations: [][]executionplan.Expectation{}, TimeoutsNS: []int64{}, MaxBytes: []int64{}}
	}
	var asked []string
	files := map[string]executionplan.CommandList{"core-nyc-01": list("show clock"), "10-1-2-3": list("show version")}
	build := func(name string) (executionplan.CommandList, error) {
		asked = append(asked, name)
		if l, ok := files[name]; ok {
			return l, nil
		}
		return executionplan.CommandList{}, &MissingCommandsFile{Folder: "lists", Name: name}
	}
	targets := []executionplan.ExecutionTarget{target("a", "core-nyc-01.example.net"), target("b", "10.1.2.3")}
	lists, err := targetCommandLists(cfg, targets, true, []string{"lists"}, build)
	if err != nil || lists["a"].Commands[0] != "show clock" || lists["b"].Commands[0] != "show version" || strings.Join(asked, " ") != "core-nyc-01 10-1-2-3" {
		t.Fatalf("lists %+v asked %q: %v", lists, asked, err)
	}
	// Twelve devices without their files: one refusal, ten named.
	var many []executionplan.ExecutionTarget
	for i := 1; i <= 12; i++ {
		many = append(many, target(fmt.Sprint(i), fmt.Sprintf("edge-%02d.example.net", i)))
	}
	_, err = targetCommandLists(cfg, append([]executionplan.ExecutionTarget{targets[0]}, many...), true, []string{"lists"}, build)
	want := "commands_file_missing: lists has no file for 12 devices\n  edge-01.example.net (edge-01)\n"
	if errorcodes.Of(err) != "commands_file_missing" || !strings.HasPrefix(err.Error(), want) || !strings.HasSuffix(err.Error(), "\n  edge-10.example.net (edge-10)\n  and 2 more") {
		t.Fatalf("twelve missing: %v", err)
	}
	if _, err = targetCommandLists(cfg, many[:1], true, []string{"lists"}, build); err == nil || err.Error() != "commands_file_missing: lists has no file for 1 device\n  edge-01.example.net (edge-01)" {
		t.Fatalf("one missing: %v", err)
	}
	// Two targets with one name: refused before any list is built.
	asked = nil
	pair := []executionplan.ExecutionTarget{target("a", "core-nyc-01.example.net"), target("c", "Core-NYC-01.example.org")}
	_, err = targetCommandLists(cfg, pair, true, []string{"lists", "more"}, build)
	if errorcodes.Of(err) != "commands_file_name_collision" || err.Error() != "commands_file_name_collision: lists/core-nyc-01 is the file of 2 devices\n  core-nyc-01.example.net\n  Core-NYC-01.example.org" || len(asked) != 0 {
		t.Fatalf("the pair: %v (asked %q)", err, asked)
	}
	if _, err = targetCommandLists(cfg, pair, false, []string{"lists"}, build); errorcodes.Of(err) != "commands_file_missing" {
		t.Fatalf("the pair by whole names reads two files: %v", err)
	}
	// Another failure names its target, its code kept.
	failing := func(string) (executionplan.CommandList, error) {
		return executionplan.CommandList{}, errorcodes.Errorf("commands_file_empty", "commands file lists/core-nyc-01 has zero bytes")
	}
	if _, err = targetCommandLists(cfg, targets[:1], true, []string{"lists"}, failing); err == nil || err.Error() != "commands_file_empty: core-nyc-01.example.net: commands file lists/core-nyc-01 has zero bytes" {
		t.Fatalf("an empty file: %v", err)
	}
	// A list's own bound above the job's.
	over := list("show tech")
	over.MaxBytes = []int64{8192}
	if _, err = targetCommandLists(cfg, targets[:1], true, []string{"lists"}, func(string) (executionplan.CommandList, error) { return over, nil }); errorcodes.Of(err) != "maxbytes_over_job_limit" {
		t.Fatalf("a bound over the job's: %v", err)
	}
}

// TestDraftCarriesTargetLists: a draft with a --cf folder's builder carries
// a list for every target and no commands of its own, and validates.
func TestDraftCarriesTargetLists(t *testing.T) {
	cfg := testConfig(t)
	opts := draftOptions(nil)
	opts.CommandFolders = []string{"lists"}
	opts.TargetCommands = func(name string) (executionplan.CommandList, error) {
		return executionplan.CommandList{Commands: []string{"show clock", "show " + name}, BlindReturns: []int{}, Blind: []bool{}, Expectations: [][]executionplan.Expectation{}, TimeoutsNS: []int64{}, MaxBytes: []int64{}}, nil
	}
	draft, err := Draft(context.Background(), cfg, operator, k03Set(t), opts, plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Commands) != 0 || len(draft.TargetCommands) != len(draft.Targets) {
		t.Fatalf("commands %q, %d lists for %d targets", draft.Commands, len(draft.TargetCommands), len(draft.Targets))
	}
	for i := range draft.Targets {
		if got := draft.CommandsFor(&draft.Targets[i]); len(got) != 2 || got[0] != "show clock" {
			t.Fatalf("target %s: %q", draft.Targets[i].TargetID, got)
		}
	}
	if err := draft.Validate(executionplan.Draft); err != nil {
		t.Fatal(err)
	}
}
