package buildinfo

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestSchemaCountersMatchTheirOwners ties the counters `karvi version`
// reports to the packages that own the schemas, so a schema that moves
// without its counter fails here. The daemon IPC counter has its own check
// in internal/ipc.
func TestSchemaCountersMatchTheirOwners(t *testing.T) {
	for _, c := range []struct {
		name        string
		got, owners int
	}{
		{"config schema", ConfigSchemaVersion, configschema.ConfigSchemaVersion},
		{"config registry schema", ConfigRegistrySchema, configschema.RegistrySchemaVersion},
		{"command record schema", CommandRecordSchema, records.CommandSchemaVersion},
		{"scoreboard schema", ScoreboardSchema, records.ScoreboardSchemaVersion},
		{"audit schema", AuditSchema, records.AuditSchemaVersion},
		{"job schema", JobSchema, records.JobSchemaVersion},
	} {
		if c.got != c.owners {
			t.Errorf("buildinfo's %s is %d; its package's is %d", c.name, c.got, c.owners)
		}
	}
}

func TestCurrentReportsRegisteredSSHTransports(t *testing.T) {
	const testID = "test-scrapligo-v2"
	registerSSHTransport(SSHTransport{ID: testID, Name: "scrapligo", Version: "2.0.0", Linkage: "shared-library"})
	defer func() {
		transportRegistry.Lock()
		delete(transportRegistry.items, testID)
		transportRegistry.Unlock()
	}()
	got := Current().SSHTransports
	foundSystem := false
	foundTest := false
	for _, item := range got {
		if item.ID == "system" && item.Linkage == "external-executable" {
			foundSystem = true
		}
		if item.ID == testID && item.Name == "scrapligo" && item.Version == "2.0.0" && item.Linkage == "shared-library" {
			foundTest = true
		}
	}
	if !foundSystem || !foundTest {
		t.Fatalf("registered transports missing: %#v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].ID > got[i].ID {
			t.Fatalf("transports not deterministically sorted: %#v", got)
		}
	}
}
