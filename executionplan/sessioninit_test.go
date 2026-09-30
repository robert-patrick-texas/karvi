package executionplan

import (
	"strings"
	"testing"
	"time"
)

// TestSessionInitValidation covers the daemon's check of the plan's
// session-init table.
func TestSessionInitValidation(t *testing.T) {
	final := fixtureFinalPlan(t)
	if err := final.Validate(Committed); err != nil {
		t.Fatal(err)
	}
	withProfile := func(edit func(*SessionInitProfile)) func(*ExecutionPlan) {
		return func(p *ExecutionPlan) {
			prof := fixtureProfile()
			edit(&prof)
			p.SessionInit["iosxe-init"] = prof
		}
	}
	cases := []struct {
		name string
		edit func(*ExecutionPlan)
		want string // "" accepts
	}{
		{"missing table", func(p *ExecutionPlan) { p.SessionInit = nil }, "execution_plan_invalid: session_init: must be present"},
		{"empty name", func(p *ExecutionPlan) { p.SessionInit[""] = fixtureProfile() }, `execution_plan_invalid: session_init[""]: the name is required`},
		{"reserved name", func(p *ExecutionPlan) { p.SessionInit["none"] = fixtureProfile() }, `execution_plan_invalid: session_init["none"]: the name none is reserved`},
		{"null commands", withProfile(func(s *SessionInitProfile) { s.Commands = nil }), `execution_plan_invalid: session_init["iosxe-init"].commands: must be present`},
		{"no-op profile", withProfile(func(s *SessionInitProfile) { s.Commands = []string{} }), ""},
		{"blank, spaces, tab", withProfile(func(s *SessionInitProfile) { s.Commands = []string{"", "   ", "\tshow"} }), ""},
		{"nul", withProfile(func(s *SessionInitProfile) { s.Commands = []string{"show", "a\x00b"} }), `execution_plan_invalid: session_init["iosxe-init"].commands: command 2 contains a NUL byte`},
		{"on_error", withProfile(func(s *SessionInitProfile) { s.OnError = "abort" }), `execution_plan_invalid: session_init["iosxe-init"].on_error: "abort" is not fail-device or continue`},
		{"on_error empty", withProfile(func(s *SessionInitProfile) { s.OnError = "" }), `execution_plan_invalid: session_init["iosxe-init"].on_error`},
		{"continue", withProfile(func(s *SessionInitProfile) { s.OnError = SessionInitContinue }), ""},
		{"timeout zero", withProfile(func(s *SessionInitProfile) { s.CommandTimeoutNS = 0 }), ""},
		{"timeout bounds", withProfile(func(s *SessionInitProfile) { s.CommandTimeoutNS = int64(12 * time.Hour) }), ""},
		{"timeout short", withProfile(func(s *SessionInitProfile) { s.CommandTimeoutNS = int64(500 * time.Millisecond) }), `execution_plan_invalid: session_init["iosxe-init"].command_timeout_ns`},
		{"timeout long", withProfile(func(s *SessionInitProfile) { s.CommandTimeoutNS = int64(13 * time.Hour) }), `execution_plan_invalid: session_init["iosxe-init"].command_timeout_ns`},
		{"timeout negative", withProfile(func(s *SessionInitProfile) { s.CommandTimeoutNS = -1 }), `execution_plan_invalid: session_init["iosxe-init"].command_timeout_ns`},
		{"unknown profile", func(p *ExecutionPlan) { p.Targets[0].SessionInitProfile = "other" }, `execution_plan_invalid: targets[0].session_init_profile: "other" is not none or a profile in session_init`},
		{"unselected entry", func(p *ExecutionPlan) { p.SessionInit["spare"] = fixtureProfile() }, `execution_plan_invalid: session_init["spare"]: no target selects it`},
		{"table without selection", func(p *ExecutionPlan) { p.Targets[2].SessionInitProfile = SessionInitNone }, `execution_plan_invalid: session_init["iosxe-init"]: no target selects it`},
		{"profile missing at commit", func(p *ExecutionPlan) { p.Targets[1].SessionInitProfile = "" }, "execution_plan_invalid: targets[1]: execution_target_invalid: session_init_profile: is required before commit"},
	}
	for _, c := range cases {
		p := fixtureFinalPlan(t)
		p.Targets = append([]ExecutionTarget(nil), p.Targets...)
		p.SessionInit = map[string]SessionInitProfile{"iosxe-init": fixtureProfile()}
		c.edit(&p)
		p.PlanDigest, _ = SumPlan(p)
		err := p.Validate(Committed)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: want accepted, got %v", c.name, err)
		case c.want != "" && (err == nil || !strings.HasPrefix(err.Error(), c.want)):
			t.Errorf("%s: want %q, got %v", c.name, c.want, err)
		}
	}
	// A draft carries no selection: an empty table and no profiles pass; a
	// table no draft target selects does not.
	draft := fixtureDraftPlan(t)
	if err := draft.Validate(Draft); err != nil {
		t.Fatal(err)
	}
	draft.SessionInit = map[string]SessionInitProfile{"iosxe-init": fixtureProfile()}
	if err := draft.Validate(Draft); err == nil || !strings.Contains(err.Error(), "no target selects it") {
		t.Errorf("draft with an unselected table: %v", err)
	}
}

// TestSessionInitProfileOutsideSourceDigest: the selection is written beside
// the binding and, like it, leaves the target's source digest alone; the plan
// digest covers it and the table.
func TestSessionInitProfileOutsideSourceDigest(t *testing.T) {
	final := fixtureFinalPlan(t)
	target := final.Targets[1]
	target.SessionInitProfile = "other"
	if sum, _ := SumTarget(target); sum != target.SourceDigest {
		t.Errorf("session_init_profile moved the source digest: %s, recorded %s", sum, target.SourceDigest)
	}
	base, _ := SumPlan(final)
	edited := final
	edited.Targets = append([]ExecutionTarget(nil), final.Targets...)
	edited.Targets[2].SessionInitProfile = SessionInitNone
	if sum, _ := SumPlan(edited); sum == base {
		t.Error("the plan digest does not cover session_init_profile")
	}
	edited = final
	edited.SessionInit = map[string]SessionInitProfile{"iosxe-init": {Commands: []string{"show clock"}, OnError: SessionInitFailDevice}}
	if sum, _ := SumPlan(edited); sum == base {
		t.Error("the plan digest does not cover the session_init table")
	}
}
