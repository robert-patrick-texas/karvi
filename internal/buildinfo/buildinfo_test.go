package buildinfo

import (
	"slices"
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

func TestCurrentListsTheTwoTransports(t *testing.T) {
	want := []SSHTransport{
		{ID: "native", Name: "native", Linkage: "compiled-in"},
		{ID: "system", Name: "system", Linkage: "external-executable"},
	}
	if got := Current().SSHTransports; !slices.Equal(got, want) {
		t.Fatalf("transports %#v, want %#v", got, want)
	}
}
