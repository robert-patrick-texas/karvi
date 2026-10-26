package jobexec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
)

// TestJobEventStatements: a job's start carries its statements in order,
// its cancel and its end none; the lines keep to the published schema,
// which declares no digest.
func TestJobEventStatements(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "audit.jsonl")
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{`audit.file="` + file + `"`, `audit.journald-required=false`}})
	if err != nil {
		t.Fatal(err)
	}
	sink, err := audit.New(cfg, home)
	if err != nil {
		t.Fatal(err)
	}
	commands := []string{"show clock", `banner motd "a=b"`}
	req := Request{Plan: executionplan.ExecutionPlan{PlanID: "p1", Commands: commands}, Config: cfg, Operator: credentials.Operator{Username: "tester", UID: 1001}, ActivityType: "run"}
	for _, e := range [][2]string{{"start", ""}, {"cancel", ""}, {"end", "complete"}} {
		if err := writeActivityAudit(sink, req, "261017-101500-00", e[0], e[1], 0, ""); err != nil {
			t.Fatal(err)
		}
	}
	sink.Close()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityLines(t, "../../schema/audit.schema.json", data)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d lines: %s", len(lines), data)
	}
	for i, name := range []string{"run.start", "run.cancel", "run.end"} {
		var r struct {
			EventName string         `json:"event_name"`
			Action    map[string]any `json:"action"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &r); err != nil {
			t.Fatal(err)
		}
		var got []string
		if list, ok := r.Action["commands"].([]any); ok {
			for _, c := range list {
				got = append(got, c.(string))
			}
		}
		_, listed := r.Action["commands"]
		if r.EventName != name || listed != (name == "run.start") || listed && !slices.Equal(got, commands) || r.Action["plan_id"] != "p1" {
			t.Errorf("line %d: %s", i, lines[i])
		}
	}
}

// TestJobStartBesideLists: beside a list per platform or per device the
// job's start lists no statements, each device's command events naming its
// own (EXAMPLES chapter 79), and counts the longest list, as the metrics do.
func TestJobStartBesideLists(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "audit.jsonl")
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{`audit.file="` + file + `"`, `audit.journald-required=false`}})
	if err != nil {
		t.Fatal(err)
	}
	sink, err := audit.New(cfg, home)
	if err != nil {
		t.Fatal(err)
	}
	plans := []executionplan.ExecutionPlan{
		{PlanID: "p1", PlatformCommands: map[string][]string{"cisco_iosxe": {"show running-config", "show version"}}},
		{PlanID: "p2", TargetCommands: map[string]executionplan.CommandList{"a": {Commands: []string{"show clock"}}, "b": {Commands: []string{"show users", "show clock", "show version"}}}},
	}
	for _, plan := range plans {
		req := Request{Plan: plan, Config: cfg, Operator: credentials.Operator{Username: "tester", UID: 1001}, ActivityType: "run"}
		if err := writeActivityAudit(sink, req, "261026-101500-00", "start", "", 0, ""); err != nil {
			t.Fatal(err)
		}
	}
	sink.Close()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityLines(t, "../../schema/audit.schema.json", data)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(plans) {
		t.Fatalf("%d lines: %s", len(lines), data)
	}
	for i, want := range []float64{2, 3} {
		var r struct {
			Action map[string]any `json:"action"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &r); err != nil {
			t.Fatal(err)
		}
		if _, listed := r.Action["commands"]; listed || r.Action["command_count"] != want {
			t.Errorf("plan %d: %s", i+1, lines[i])
		}
	}
}
