package executor

import (
	"reflect"
	"testing"

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
