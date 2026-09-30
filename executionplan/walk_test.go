package executionplan

import (
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
)

// walkAllow is the leaf set the structural walker accepts.
var walkAllow = canarytest.Allow{Leaves: []string{"net/netip.Addr", "github.com/robert-patrick-texas/karvi/executionplan.Digest"}}

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
