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

func TestDigestMetadataIsAllowed(t *testing.T) {
	r := records.AuditRecord{EventID: "x", EventName: "test", Outcome: "success", Severity: "info", Operator: records.Operator{Username: "tester", UID: 1000}, Process: map[string]any{}, Action: map[string]any{"command_count": 1, "command_sha256_array": []string{"abc"}}, Policy: map[string]any{}, Result: map[string]any{}, Source: map[string]any{}, Details: map[string]any{}}
	if err := screen(r); err != nil {
		t.Fatal(err)
	}
}

// TestAuditFilePath: audit.file's ~ is the home given, the password
// database's, never $HOME; ~user is refused.
func TestAuditFilePath(t *testing.T) {
	home, other := t.TempDir(), t.TempDir()
	t.Setenv("HOME", other)
	load := func(path string) configload.Snapshot {
		t.Helper()
		cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`audit.file="` + path + `"`, `audit.journald-required=false`}})
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	sink, err := New(load("~/a/audit.jsonl"), home)
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
	if _, err := New(load("~other/audit.jsonl"), home); errorcodes.Of(err) != "path_other_user_home_unsupported" {
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
