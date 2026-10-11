package configload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTransformsCheckedAtLoad is EXAMPLES chapter 55, issue 8: every
// [credential-transform.NAME] and [name-transform.NAME] table is built at
// load with the builder the run uses, a table no backend or source names
// included, and a list that does not build is refused under the run's own
// code at the list's place; a credential backend's transform and an
// inventory source's name-transform must name a table, or "default".
func TestTransformsCheckedAtLoad(t *testing.T) {
	const backend = "[credential-backend.b]\ntype = \"csv\"\nscope = \"user\"\npath = \"~/c.csv\"\n"
	const source = "[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \"/srv/inventory.csv\"\n"
	load := func(t *testing.T, body string) error {
		t.Helper()
		path := filepath.Join(t.TempDir(), "karvi.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, ExplicitRoots: []string{path}})
		return err
	}
	for _, body := range []string{
		backend + "transform = \"default\"\n",
		backend + "transform = \"adm\"\n[credential-transform.adm]\noperator = [{op = \"add-suffix\", suffix = \"-adm\"}]\nusername = [{op = \"lowercase\"}]\n",
		backend + "[credential-transform.b]\nusername = [{op = \"strip-suffix\", suffix = \"-x\"}]\n",
		source + "name-transform = \"default\"\n",
		source + "name-transform = \"lab\"\n[name-transform.lab]\noperations = [{op = \"crop-to-dot\"}]\n",
	} {
		if err := load(t, body); err != nil {
			t.Errorf("%q: %v", body, err)
		}
	}
	for _, tc := range []struct {
		name, body, code, key string
	}{
		{"a backend's transform naming no table", backend + "transform = \"nope\"\n", "credential_transform_unknown", "credential-backend.b.transform"},
		{"an unknown operation", backend + "transform = \"adm\"\n[credential-transform.adm]\noperator = [{op = \"add-sufix\", suffix = \"-adm\"}]\n", "transform_operation_unknown", "credential-transform.adm.operator"},
		{"add-suffix with no suffix", backend + "transform = \"adm\"\n[credential-transform.adm]\noperator = [{op = \"add-suffix\"}]\n", "transform_suffix_empty", "credential-transform.adm.operator"},
		{"a table nothing names", backend + "[credential-transform.spare]\nusername = [{op = \"bogus\"}]\n", "transform_operation_unknown", "credential-transform.spare.username"},
		{"two bad tables, the first by name", backend + "[credential-transform.zz]\nusername = [{op = \"bogus\"}]\n[credential-transform.aa]\noperator = [{op = \"bogus\"}]\n", "transform_operation_unknown", "credential-transform.aa.operator"},
		{"an operation that is not a table", backend + "[credential-transform.adm]\noperator = [\"lowercase\"]\n", "credential_transform_operation_not_table", "credential-transform.adm.operator"},
		{"a credential list that is not an array", backend + "[credential-transform.adm]\noperator = \"lowercase\"\n", "credential_transform_operations_not_array", "credential-transform.adm.operator"},
		{"a name transform's unknown operation", "[name-transform.default]\noperations = [{op = \"bogus\"}]\n", "transform_operation_unknown", "name-transform.default.operations"},
		{"a name transform's operation that is not a table", "[name-transform.lab]\noperations = [\"lowercase\"]\n", "name_transform_operation_not_table", "name-transform.lab.operations"},
		{"a name transform's list that is not an array", "[name-transform.lab]\noperations = \"lowercase\"\n", "name_transform_operations_not_array", "name-transform.lab.operations"},
		{"an inventory source's name-transform naming no table", source + "name-transform = \"nope\"\n", "name_transform_profile_unknown", "inventory-source.0.name-transform"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := load(t, tc.body)
			var ce *Error
			if !errors.As(err, &ce) || ce.Code != tc.code || ce.Key != tc.key {
				t.Fatalf("err=%v, want %s at %q", err, tc.code, tc.key)
			}
			if !strings.Contains(ce.Error(), "karvi.toml:") {
				t.Fatalf("the message names no place: %v", err)
			}
		})
	}
}
