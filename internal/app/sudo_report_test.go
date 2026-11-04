package app

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestReportShowsEachRootCommand: the dry-run text lists the plan's root
// commands by their place on a top sudo: line, a device's own list shows
// each as its --sudo in its place, and a device with a --sudo command has
// a sudo: line naming where its password comes from, or none, sent as
// sudo -n (EXAMPLES chapter 96); none, nothing.
func TestReportShowsEachRootCommand(t *testing.T) {
	if got := describeSudo([]bool{false, true, false, true}); got != "2, 4" {
		t.Fatalf("describeSudo: %q", got)
	}
	if got := describeSudo([]bool{}); got != "" {
		t.Fatalf("no root command: %q", got)
	}
	list := executionplan.CommandList{Commands: []string{"uptime", "systemctl restart nginx"}, SleepsNS: []int64{0, 5e9}, Sudo: []bool{false, true}}
	if got := joinCommands(list); got != "uptime; --sleep 5s; --sudo systemctl restart nginx" {
		t.Fatalf("joinCommands: %q", got)
	}
	for source, want := range map[string]string{
		credentials.SudoEnable:    "password from the enable field (backend=site-csv; value not displayed)",
		credentials.SudoNETSUDO:   "password from NETSUDO (value not displayed)",
		credentials.SudoNETENABLE: "password from NETENABLE (value not displayed)",
		credentials.SudoNone:      "none, sudo -n",
	} {
		if got := describeSudoSource(source, "site-csv"); got != want {
			t.Errorf("%s: %q, want %q", source, got, want)
		}
	}

	bound := func(id, sudo string) records.TargetReport {
		return records.TargetReport{TargetID: id, Readiness: "ready", CredentialBinding: records.CredentialBindingReport{Status: records.BindingBound, CredentialID: "c-" + id, Policy: "default", Backend: "builtin-env-fallback", DeviceUsername: "netops", Sudo: sudo}}
	}
	r := records.PlanReport{
		ActivityID: "a1",
		Plan: executionplan.ExecutionPlan{
			Commands: []string{"uptime", "systemctl restart nginx", "iptables-save"}, Sudo: []bool{false, true, true},
			Targets: []executionplan.ExecutionTarget{{TargetID: "srv1", Channel: executionplan.ChannelExec}, {TargetID: "srv2", Channel: executionplan.ChannelExec}, {TargetID: "r1", Channel: executionplan.ChannelShell}},
		},
		Targets: []records.TargetReport{bound("srv1", credentials.SudoNETSUDO), bound("srv2", credentials.SudoNone), bound("r1", "")},
	}
	r.Counts.Commands = 3
	var b strings.Builder
	if err := renderInspection(&b, "text", r, "", "parallel", "0640"); err != nil {
		t.Fatal(err)
	}
	text := b.String()
	for _, want := range []string{
		"commands: 3\nsudo: commands 2, 3\ndispatch: ",
		"- srv1: ready\n  address: ",
		"(policy=default backend=builtin-env-fallback user=netops; value not displayed)\n  sudo: password from NETSUDO (value not displayed)\n  intended: ",
		"(policy=default backend=builtin-env-fallback user=netops; value not displayed)\n  sudo: none, sudo -n\n  intended: ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the text lacks %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "  sudo: "); n != 2 {
		t.Errorf("%d device sudo lines, want 2 (r1 has none):\n%s", n, text)
	}
	r.Plan.Sudo, r.Targets = []bool{}, []records.TargetReport{bound("srv1", ""), bound("srv2", ""), bound("r1", "")}
	b.Reset()
	if err := renderInspection(&b, "text", r, "", "parallel", "0640"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "sudo:") {
		t.Errorf("no root command, yet a sudo line:\n%s", b.String())
	}
}
