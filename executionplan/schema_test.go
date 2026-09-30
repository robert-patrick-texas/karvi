package executionplan

import (
	"reflect"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
)

const planSchema = "../schema/execution-plan.schema.json"

func reflectType(v any) reflect.Type { return reflect.TypeOf(v) }

func TestPlanAndHeaderMatchTheSchema(t *testing.T) {
	draft := fixtureDraftPlan(t)
	canarytest.SchemaParity(t, planSchema, draft)
	final := fixtureFinalPlan(t)
	canarytest.SchemaParity(t, planSchema, final)
}

// Header parity uses a wrapper schema whose root is the header definition.
func TestHeaderMatchesTheSchema(t *testing.T) {
	final := fixtureFinalPlan(t)
	h := fixtureHeader(t, final, Committed)
	canarytest.SchemaParityAt(t, planSchema, "#/$defs/public_job_header", h)
	hd := fixtureHeader(t, fixtureDraftPlan(t), Draft)
	canarytest.SchemaParityAt(t, planSchema, "#/$defs/public_job_header", hd)
}

func TestPlanAndHeaderAreStructurallyNonSecret(t *testing.T) {
	allow := canarytest.Allow{Leaves: append(walkAllow.Leaves, "time.Time")}
	canarytest.Walk(t, reflect.TypeOf(ExecutionPlan{}), allow)
	canarytest.Walk(t, reflect.TypeOf(PublicJobHeader{}), allow)
}
