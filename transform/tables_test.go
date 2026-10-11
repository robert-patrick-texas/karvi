package transform

import (
	"errors"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestTables is EXAMPLES chapter 55, issue 8: the one builder of the named
// tables, which config validate and the run share. Each kind has a
// "default" that changes nothing unless defined, a list a table leaves out
// is no operation, and a list that does not build names its key and carries
// the code.
func TestTables(t *testing.T) {
	names, err := Names(map[string]map[string]any{"lab": {"operations": []any{map[string]any{"op": "lowercase"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := names["lab"].Apply("SW1"); got != "sw1" {
		t.Fatalf("lab: %q", got)
	}
	if got, _, _ := names["default"].Apply("SW1"); got != "SW1" {
		t.Fatalf("default: %q", got)
	}
	creds, err := Credentials(map[string]map[string]any{"adm": {"operator": []any{map[string]any{"op": "add-suffix", "suffix": "-adm"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := creds["adm"].Operator.Apply("netops"); got != "netops-adm" {
		t.Fatalf("adm operator: %q", got)
	}
	if len(creds["adm"].Username) != 0 || len(creds["default"].Operator) != 0 {
		t.Fatalf("a list left out is no operation: %+v", creds)
	}
	for _, tc := range []struct {
		build     func() error
		code, key string
	}{
		{func() error {
			_, err := Names(map[string]map[string]any{"lab": {"operations": "lowercase"}})
			return err
		}, "name_transform_operations_not_array", "name-transform.lab.operations"},
		{func() error {
			_, err := Names(map[string]map[string]any{"lab": {"operations": []any{"lowercase"}}})
			return err
		}, "name_transform_operation_not_table", "name-transform.lab.operations"},
		{func() error {
			_, err := Credentials(map[string]map[string]any{"adm": {"username": "lowercase"}})
			return err
		}, "credential_transform_operations_not_array", "credential-transform.adm.username"},
		{func() error {
			_, err := Credentials(map[string]map[string]any{"adm": {"operator": []any{"lowercase"}}})
			return err
		}, "credential_transform_operation_not_table", "credential-transform.adm.operator"},
		{func() error {
			_, err := Credentials(map[string]map[string]any{"adm": {"operator": []any{map[string]any{"op": "add-suffix"}}}})
			return err
		}, "transform_suffix_empty", "credential-transform.adm.operator"},
	} {
		err := tc.build()
		var te *TableError
		if !errors.As(err, &te) || te.Key != tc.key || errorcodes.Of(err) != tc.code {
			t.Errorf("err=%v, want %s at %s", err, tc.code, tc.key)
		}
	}
}
