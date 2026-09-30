package jobexec

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
)

// TestJobexecReachesNoLoaderOrBackend is the transitive import exclusion:
// the runner reaches no inventory loader,
// no tabular reader, no target source, and no credential backend at any
// depth. The daemon's own assertion arrives with the schema 5 switch.
func TestJobexecReachesNoLoaderOrBackend(t *testing.T) {
	const m = "github.com/robert-patrick-texas/karvi/"
	canarytest.AssertUnreachable(t, ".", m+"internal/inventoryload", m+"tabular", m+"internal/targetsource", m+"internal/credentialbackend", m+"internal/credentialbackend/", m+"internal/app", m+"internal/planner")
	reached := canarytest.Reachable(t, ".")
	t.Logf("jobexec reaches %d module packages", len(reached))
}
