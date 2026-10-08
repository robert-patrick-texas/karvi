package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestAuditIdentity: every command_completed names the address and, from
// the record's credential, the device username, the backend, and the method
// that authenticated; a session that never authenticated names no method,
// and a record without a credential the address alone.
func TestAuditIdentity(t *testing.T) {
	r := records.CommandRecord{SelectedAddress: "192.0.2.10", Credential: &records.CredentialProjection{DeviceUsername: "netops", Backend: "builtin-operator-keys", Auth: "publickey"}}
	if got, want := auditIdentity(r), map[string]any{"selected_address": "192.0.2.10", "device_username": "netops", "backend": "builtin-operator-keys", "auth": "publickey"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("authenticated: %v", got)
	}
	r.Credential.Auth = ""
	if got := auditIdentity(r); got["auth"] != nil || got["backend"] != "builtin-operator-keys" {
		t.Fatalf("never authenticated: %v", got)
	}
	r.Credential = nil
	if got := auditIdentity(r); !reflect.DeepEqual(got, map[string]any{"selected_address": "192.0.2.10"}) {
		t.Fatalf("no credential: %v", got)
	}
}

// TestCommandCompletedProcess: a command_completed line names the process
// that wrote it, as the job's other events do, and the sink stamps the
// audit's schema version.
func TestCommandCompletedProcess(t *testing.T) {
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
	e := &DeviceExecutor{opts: Options{Config: cfg, Audit: sink}}
	e.afterRecord(records.CommandRecord{RecordID: "r1", Status: "succeeded"}, output.Source{})
	sink.Close()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var line struct {
		SchemaVersion int            `json:"schema_version"`
		Process       map[string]any `json:"process"`
	}
	if err := json.Unmarshal(data, &line); err != nil {
		t.Fatal(err)
	}
	if line.SchemaVersion != records.AuditSchemaVersion || line.Process["pid"] != float64(os.Getpid()) {
		t.Fatalf("schema_version %d, process %v", line.SchemaVersion, line.Process)
	}
}
