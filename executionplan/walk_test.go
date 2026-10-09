package executionplan

import (
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
)

// walkAllow is the leaf set the structural walker accepts. Configuration
// holds the client's resolved values typed any: no key holds a secret (none
// marked sensitive; a credential backend names variables and files, never
// values), the agreed ground of the block (EXAMPLES chapter 47, issue 5).
var walkAllow = canarytest.Allow{Leaves: []string{"net/netip.Addr", "github.com/robert-patrick-texas/karvi/executionplan.Digest", "github.com/robert-patrick-texas/karvi/executionplan.Configuration"}}

func TestExecutionTargetIsStructurallyNonSecret(t *testing.T) {
	canarytest.Walk(t, reflect.TypeOf(ExecutionTarget{}), walkAllow)
}

func TestPackageImportsOnlyInventoryAndStandardLibrary(t *testing.T) {
	for _, imp := range canarytest.Imports(t, ".") {
		if !strings.Contains(imp, ".") {
			continue // standard library
		}
		if imp != "github.com/robert-patrick-texas/karvi/inventory" {
			t.Errorf("executionplan imports %s; only inventory and the standard library are allowed", imp)
		}
	}
}
