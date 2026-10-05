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
