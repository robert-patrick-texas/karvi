package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/records"
)

// TestSinkMasksStatements: a sink given masks writes every statement an
// event carries, action.command and action.commands, masked, so that the
// journal's line, made from the same record, follows; the caller's map and
// the plan's list stay as sent. The sink here has the file alone, so
// nothing reaches the host's journal.
func TestSinkMasksStatements(t *testing.T) {
	file := filepath.Join(t.TempDir(), "audit.jsonl")
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	sink := &Sink{file: f}
	sink.SetMasks([]*regexp.Regexp{regexp.MustCompile(`(?i)^\s*(?:no\s+)?snmp-s\S*\s+comm\S*\s+(\S+)`)})
	commands := []string{"show clock", "snmp-server community Publ1c RO"}
	start := map[string]any{"mode": "run", "commands": commands}
	command := map[string]any{"mode": "run", "command": "snmp-server community Publ1c RO", "command_index": 2}
	for _, r := range []records.AuditRecord{
		{EventID: "1", EventName: "run.start", Action: start},
		{EventID: "2", EventName: "run.command", Action: command},
		{EventID: "3", EventName: "run.command", Action: map[string]any{"command": "show clock"}},
	} {
		if err := sink.WriteAudit(r); err != nil {
			t.Fatal(err)
		}
	}
	sink.Close()
	if commands[1] != "snmp-server community Publ1c RO" || command["command"] != "snmp-server community Publ1c RO" {
		t.Fatalf("the caller's statements changed: %v %v", commands, command)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Publ1c") {
		t.Fatalf("a secret in the file:\n%s", data)
	}
	var got []records.AuditRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r records.AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if list := got[0].Action["commands"].([]any); list[0] != "show clock" || list[1] != "snmp-server community <removed> RO" {
		t.Fatalf("the start's statements: %v", list)
	}
	if got[1].Action["command"] != "snmp-server community <removed> RO" || got[2].Action["command"] != "show clock" {
		t.Fatalf("the command events: %v %v", got[1].Action, got[2].Action)
	}
	if line := Line(got[1]); !strings.HasSuffix(line, `command="snmp-server community <removed> RO"`) {
		t.Fatalf("the journal's line: %s", line)
	}
	if out := maskAction(command, nil); out["command"] != "snmp-server community Publ1c RO" {
		t.Fatalf("no masks mask nothing: %v", out)
	}
}
