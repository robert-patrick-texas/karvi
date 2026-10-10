package jobexec

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestPlanConfigurationIsTheClientsConfiguration: a loaded configuration,
// the built-in defaults and the shipped example, written into a plan's
// block and carried as JSON, builds back to the same values and the same
// digest: each value's encoding equal, and each fixed key equal through
// its kind's reader (a number key's integral value may come back an int64,
// which Float reads as the load's float64); the in-process job runs under
// the loaded one, the daemon's under the rebuilt one.
func TestPlanConfigurationIsTheClientsConfiguration(t *testing.T) {
	for name, roots := range map[string][]string{"defaults": nil, "example": {"../../examples/config.toml"}} {
		loaded, err := configload.Load(configload.Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, ExplicitRoots: roots})
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(executionplan.ExecutionPlan{Configuration: loaded.ValueMap(), Sources: executionplan.SourceDigests{ConfigDigest: loaded.Digest}})
		if err != nil {
			t.Fatal(err)
		}
		var plan executionplan.ExecutionPlan
		if err := json.Unmarshal(data, &plan); err != nil {
			t.Fatal(err)
		}
		built, err := PlanConfiguration(&plan)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if built.Digest != loaded.Digest {
			t.Errorf("%s: digest %s, loaded %s", name, built.Digest, loaded.Digest)
		}
		if len(built.Values) != len(loaded.Values) {
			t.Errorf("%s: %d values, loaded %d", name, len(built.Values), len(loaded.Values))
		}
		for k, v := range loaded.Values {
			got, _ := json.Marshal(built.Values[k].Data)
			want, _ := json.Marshal(v.Data)
			if string(got) != string(want) {
				t.Errorf("%s: %s: %s, loaded %s", name, k, got, want)
			}
		}
		for _, e := range configschema.Entries() {
			var got, want any
			switch e.Kind {
			case configschema.Integer:
				got, want = built.Int64(e.Path), loaded.Int64(e.Path)
			case configschema.Number:
				got, want = built.Float(e.Path), loaded.Float(e.Path)
			case configschema.Boolean:
				got, want = built.Bool(e.Path), loaded.Bool(e.Path)
			case configschema.StringArray:
				got, want = built.Strings(e.Path), loaded.Strings(e.Path)
			case configschema.ObjectArray:
				got, want = built.Values[e.Path].Data, loaded.Values[e.Path].Data
			default:
				got, want = built.String(e.Path), loaded.String(e.Path)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: %s read as %#v, loaded %#v", name, e.Path, got, want)
			}
		}
	}
}

// TestPlanConfigurationRefusals: a block whose digest is not the plan's,
// a key the registry does not know, and an integer key holding a fraction
// are execution_plan_invalid.
func TestPlanConfigurationRefusals(t *testing.T) {
	loaded, err := configload.Load(configload.Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	changed := loaded.ValueMap()
	changed["ssh.host-key-policy"] = "insecure"
	unknown := loaded.ValueMap()
	unknown["no.such-key"] = "x"
	fraction := loaded.ValueMap()
	fraction["daemon.max-accepted-jobs"] = 1.5
	for name, c := range map[string]struct {
		block executionplan.Configuration
		want  string
	}{
		"changed":  {changed, "configuration: digest"},
		"unknown":  {unknown, `unknown key "no.such-key"`},
		"fraction": {fraction, "1.5 is not an integer"},
	} {
		plan := executionplan.ExecutionPlan{Configuration: c.block, Sources: executionplan.SourceDigests{ConfigDigest: loaded.Digest}}
		_, err := PlanConfiguration(&plan)
		if errorcodes.Of(err) != "execution_plan_invalid" || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
