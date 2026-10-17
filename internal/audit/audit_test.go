package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

func TestSecretNamedPropertyIsRejected(t *testing.T) {
	r := records.AuditRecord{EventID: "x", EventName: "test", Outcome: "failure", Severity: "error", Operator: records.Operator{Username: "tester", UID: 1000}, Process: map[string]any{}, Action: map[string]any{}, Policy: map[string]any{}, Result: map[string]any{}, Source: map[string]any{}, Details: map[string]any{"password": "must-not-pass"}}
	if err := screen(r); err == nil {
		t.Fatal("expected secret-named property rejection")
	}
}

// TestStatementKeysPass: the screen judges keys alone, and the statements'
// keys are none it rejects.
func TestStatementKeysPass(t *testing.T) {
	r := records.AuditRecord{EventID: "x", EventName: "test", Outcome: "success", Severity: "info", Operator: records.Operator{Username: "tester", UID: 1000}, Process: map[string]any{}, Action: map[string]any{"command": "show clock", "commands": []string{"show clock"}, "command_index": 1, "command_count": 1, "command_kind": "requested"}, Policy: map[string]any{}, Result: map[string]any{}, Source: map[string]any{}, Details: map[string]any{}}
	if err := screen(r); err != nil {
		t.Fatal(err)
	}
}

// TestAuditFilePath: audit.file's ~ is the load's home, never $HOME; ~user
// is refused at the load.
func TestAuditFilePath(t *testing.T) {
	home, other := t.TempDir(), t.TempDir()
	t.Setenv("HOME", other)
	load := func(path string) (configload.Snapshot, error) {
		return configload.Load(configload.Options{HomeDir: home, InternalOnly: true, Environment: []string{}, Sets: []string{`audit.file="` + path + `"`, `audit.journald-required=false`}})
	}
	cfg, err := load("~/a/audit.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	sink, err := New(cfg, home)
	if err != nil {
		t.Fatal(err)
	}
	sink.Close()
	if _, err := os.Stat(filepath.Join(home, "a", "audit.jsonl")); err != nil {
		t.Fatalf("not in the home given: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "a")); !os.IsNotExist(err) {
		t.Fatalf("written under $HOME: %v", err)
	}
	if _, err := load("~other/audit.jsonl"); errorcodes.Of(err) != "path_other_user_home_unsupported" {
		t.Fatalf("~other: %v", err)
	}
}

// TestSinkStampsTheSchemaVersion: the line carries the audit's schema
// version whatever the record held.
func TestSinkStampsTheSchemaVersion(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "audit.jsonl")
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`audit.file="` + file + `"`, `audit.journald-required=false`}})
	if err != nil {
		t.Fatal(err)
	}
	sink, err := New(cfg, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []int{0, records.AuditSchemaVersion + 6} {
		if err := sink.WriteAudit(records.AuditRecord{SchemaVersion: v, EventID: "x", EventName: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	sink.Close()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r records.AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil || r.SchemaVersion != records.AuditSchemaVersion {
			t.Fatalf("line %s: %v", line, err)
		}
	}
}

// TestLine: the message is key=value pairs in one order, karvi.audit first,
// a pair left out where the event has no value; a value that would end its
// pair early is quoted, and a statement always, last.
func TestLine(t *testing.T) {
	op := records.Operator{Username: "netops", UID: 1000}
	dev := &records.AuditDevice{ID: "name:core-rtr-1", CanonicalName: "core-rtr-1", Platform: "cisco_iosxe"}
	for _, tc := range []struct {
		r    records.AuditRecord
		want string
	}{
		{records.AuditRecord{EventName: "run.start", Operator: op, JobID: "261016-085318-00"},
			"karvi.audit=run.start operator=netops job=261016-085318-00"},
		{records.AuditRecord{EventName: "run.command", Operator: op, Outcome: "success", JobID: "261016-085318-00", Device: dev},
			"karvi.audit=run.command operator=netops outcome=success job=261016-085318-00 device=core-rtr-1"},
		{records.AuditRecord{EventName: "login.end", Operator: records.Operator{Username: "noc"}, LoginUser: "jsmith", Outcome: "error", LoginID: "261017-101500-00", Device: dev},
			"karvi.audit=login.end operator=noc loginuser=jsmith outcome=error login=261017-101500-00 device=core-rtr-1"},
		{records.AuditRecord{EventName: "cmd.command", Operator: op, Outcome: "success", JobID: "x", Device: &records.AuditDevice{CanonicalName: `a b="c"`}},
			`karvi.audit=cmd.command operator=netops outcome=success job=x device="a b=\"c\""`},
		{records.AuditRecord{EventName: "run.command", Operator: op, Outcome: "success", JobID: "261016-085318-00", Device: dev, Action: map[string]any{"mode": "run", "command": "show clock", "command_index": 1}},
			`karvi.audit=run.command operator=netops outcome=success job=261016-085318-00 device=core-rtr-1 command="show clock"`},
		{records.AuditRecord{EventName: "cmd.command", Operator: op, Outcome: "success", JobID: "x", Device: dev, Action: map[string]any{"command": "terse"}},
			`karvi.audit=cmd.command operator=netops outcome=success job=x device=core-rtr-1 command="terse"`},
		{records.AuditRecord{EventName: "cmd.command", Operator: op, Outcome: "success", JobID: "x", Device: dev, Action: map[string]any{"command": "banner motd \"a=b\\c\"\nend"}},
			`karvi.audit=cmd.command operator=netops outcome=success job=x device=core-rtr-1 command="banner motd \"a=b\\c\"\nend"`},
		{records.AuditRecord{EventName: "run.start", Operator: op, JobID: "x", Action: map[string]any{"commands": []string{"show clock"}}},
			"karvi.audit=run.start operator=netops job=x"},
	} {
		if got := Line(tc.r); got != tc.want {
			t.Errorf("Line:\n got %s\nwant %s", got, tc.want)
		}
	}
}

// TestJournalEntry: the entry carries a field a match names whole only where
// the event has its value; no activity ID.
func TestJournalEntry(t *testing.T) {
	fields := func(r records.AuditRecord) map[string]string {
		m := map[string]string{}
		for _, l := range strings.Split(strings.TrimSuffix(string(journalEntry(r, []byte(`{}`))), "\n"), "\n") {
			k, v, _ := strings.Cut(l, "=")
			m[k] = v
		}
		return m
	}
	cmd := fields(records.AuditRecord{EventName: "run.command", Severity: "info", Operator: records.Operator{Username: "netops"}, JobID: "j", Device: &records.AuditDevice{CanonicalName: "core-rtr-1"}})
	if cmd["KARVI_EVENT_NAME"] != "run.command" || cmd["KARVI_OPERATOR"] != "netops" || cmd["KARVI_JOB_ID"] != "j" || cmd["KARVI_DEVICE"] != "core-rtr-1" || cmd["PRIORITY"] != "6" || cmd["SYSLOG_IDENTIFIER"] != "karvi" || cmd["KARVI_AUDIT_JSON"] != "{}" {
		t.Errorf("command event: %v", cmd)
	}
	for _, absent := range []string{"KARVI_LOGINUSER", "KARVI_LOGIN_ID", "KARVI_ACTIVITY_ID"} {
		if _, ok := cmd[absent]; ok {
			t.Errorf("command event carries %s: %v", absent, cmd)
		}
	}
	login := fields(records.AuditRecord{EventName: "login.start", Severity: "info", Operator: records.Operator{Username: "noc"}, LoginUser: "jsmith", LoginID: "l"})
	if login["KARVI_LOGINUSER"] != "jsmith" || login["KARVI_LOGIN_ID"] != "l" || login["MESSAGE"] != "karvi.audit=login.start operator=noc loginuser=jsmith login=l" {
		t.Errorf("login event: %v", login)
	}
	for _, absent := range []string{"KARVI_JOB_ID", "KARVI_DEVICE"} {
		if _, ok := login[absent]; ok {
			t.Errorf("login event carries %s: %v", absent, login)
		}
	}
}
