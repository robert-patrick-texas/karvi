package audit

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/records"
)

func TestSecretNamedPropertyIsRejected(t *testing.T) {
	r := records.AuditRecord{SchemaVersion: 1, EventID: "x", EventName: "test", Outcome: "failure", Severity: "error", Operator: records.Operator{Username: "tester", UID: 1000}, Process: map[string]any{}, Action: map[string]any{}, Policy: map[string]any{}, Result: map[string]any{}, Source: map[string]any{}, Details: map[string]any{"password": "must-not-pass"}}
	if err := screen(r); err == nil {
		t.Fatal("expected secret-named property rejection")
	}
}

func TestDigestMetadataIsAllowed(t *testing.T) {
	r := records.AuditRecord{SchemaVersion: 1, EventID: "x", EventName: "test", Outcome: "success", Severity: "info", Operator: records.Operator{Username: "tester", UID: 1000}, Process: map[string]any{}, Action: map[string]any{"command_count": 1, "command_sha256_array": []string{"abc"}}, Policy: map[string]any{}, Result: map[string]any{}, Source: map[string]any{}, Details: map[string]any{}}
	if err := screen(r); err != nil {
		t.Fatal(err)
	}
}
