package executionplan_test

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestExecTargetRefusesTerminalDeclarations: a command for an exec target
// may carry no blind send and no expectation, the client's plan and the
// daemon's check alike, naming the target and the command's index; the same
// declarations on a shell target are the plan's own business.
func TestExecTargetRefusesTerminalDeclarations(t *testing.T) {
	exec := func() executionplan.ExecutionPlan {
		plan := plantest.DraftPlan()
		target := plan.Targets[1]
		target.Channel, target.Device.Transport = executionplan.ChannelExec, executionplan.TransportNative
		sum, err := executionplan.SumTarget(target)
		if err != nil {
			t.Fatal(err)
		}
		target.SourceDigest = sum
		plan.Targets[1] = target
		return plan
	}
	n := len(plantest.Commands)
	plan := exec()
	if err := plan.Validate(executionplan.Draft); err != nil {
		t.Fatalf("an exec target with no declaration: %v", err)
	}
	for name, edit := range map[string]func(*executionplan.ExecutionPlan){
		"blind": func(p *executionplan.ExecutionPlan) {
			p.Blind = make([]bool, n)
			p.Blind[n-1] = true
		},
		"blind returns": func(p *executionplan.ExecutionPlan) {
			p.Blind, p.BlindReturns = make([]bool, n), make([]int, n)
			p.Blind[n-1], p.BlindReturns[n-1] = true, 2
		},
		"expect": func(p *executionplan.ExecutionPlan) {
			p.Expectations = make([][]executionplan.Expectation, n)
			for i := range p.Expectations {
				p.Expectations[i] = []executionplan.Expectation{}
			}
			p.Expectations[n-1] = []executionplan.Expectation{{Pattern: `\[confirm\]`}}
		},
	} {
		plan := exec()
		edit(&plan)
		err := plan.Validate(executionplan.Draft)
		if errorcodes.Of(err) != "channel_exec_declaration_refused" || !strings.Contains(err.Error(), "edge-b: command "+string(rune('0'+n))) {
			t.Errorf("%s: %v", name, err)
		}
		shell := plantest.DraftPlan()
		edit(&shell)
		if err := shell.Validate(executionplan.Draft); err != nil {
			t.Errorf("%s on shell targets: %v", name, err)
		}
	}
}

// TestShellTargetRefusesSudo: a --sudo command reaches exec targets alone:
// a true flag for a shell target is channel_shell_sudo_refused, the client's
// plan and the daemon's check alike, naming the target and the command's
// index, in the plan's own list, a target's, or a platform's; on an exec
// target the same flag is accepted. A flag list of the wrong length is the
// plan's own refusal (EXAMPLES chapter 96).
func TestShellTargetRefusesSudo(t *testing.T) {
	n := len(plantest.Commands)
	flags := func() []bool {
		f := make([]bool, n)
		f[n-1] = true
		return f
	}
	plan := plantest.DraftPlan()
	plan.Sudo = flags()
	err := plan.Validate(executionplan.Draft)
	if errorcodes.Of(err) != "channel_shell_sudo_refused" || !strings.Contains(err.Error(), ": command "+string(rune('0'+n))+" is a --sudo command") {
		t.Fatalf("the plan's list: %v", err)
	}
	// Every target on exec: accepted.
	exec := plantest.DraftPlan()
	for i := range exec.Targets {
		target := exec.Targets[i]
		target.Channel, target.Device.Transport = executionplan.ChannelExec, executionplan.TransportNative
		sum, err := executionplan.SumTarget(target)
		if err != nil {
			t.Fatal(err)
		}
		target.SourceDigest = sum
		exec.Targets[i] = target
	}
	exec.Sudo = flags()
	if err := exec.Validate(executionplan.Draft); err != nil {
		t.Fatalf("exec targets: %v", err)
	}
	// A target's own list, and a platform's.
	lists := plantest.DraftPlan()
	lists.Commands, lists.TargetCommands = []string{}, map[string]executionplan.CommandList{}
	for _, x := range lists.Targets {
		list := executionplan.NewCommandList(append([]string{}, plantest.Commands...))
		list.Sudo = flags()
		lists.TargetCommands[x.TargetID] = list
	}
	if err := lists.Validate(executionplan.Draft); errorcodes.Of(err) != "channel_shell_sudo_refused" {
		t.Fatalf("target lists: %v", err)
	}
	platforms := plantest.DraftPlan()
	platforms.Commands, platforms.PlatformCommands = []string{}, map[string]executionplan.CommandList{}
	for _, x := range platforms.Targets {
		list := executionplan.NewCommandList([]string{"show version"})
		list.Sudo = []bool{true}
		platforms.PlatformCommands[x.Device.Platform] = list
	}
	if err := platforms.Validate(executionplan.Draft); errorcodes.Of(err) != "channel_shell_sudo_refused" || !strings.Contains(err.Error(), "command 1 is") {
		t.Fatalf("platform lists: %v", err)
	}
	// The wrong length, and a missing list.
	short := plantest.DraftPlan()
	short.Sudo = []bool{false}
	if err := short.Validate(executionplan.Draft); errorcodes.Of(err) != "execution_plan_invalid" || !strings.Contains(err.Error(), "sudo: 1 entries for") {
		t.Fatalf("one flag for %d commands: %v", n, err)
	}
	missing := plantest.DraftPlan()
	missing.Sudo = nil
	if err := missing.Validate(executionplan.Draft); errorcodes.Of(err) != "execution_plan_invalid" || !strings.Contains(err.Error(), "sudo: must be present") {
		t.Fatalf("no list: %v", err)
	}
}
