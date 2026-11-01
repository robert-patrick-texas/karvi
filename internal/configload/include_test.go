package configload

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestIncludesWrittenInlineTakeTheIncludeRules: config.includes and
// config.schema-version written in an inline table (config = { ... }) are
// judged as the keys written out: a graph root's includes = false stops its
// directives, an included file's is refused as
// config_includes_not_at_file_root, one after the first directive as
// config_includes_too_late, and a file's own schema version is checked in
// that file although a later file writes the right one.
func TestIncludesWrittenInlineTakeTheIncludeRules(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	load := func(roots ...string) (Snapshot, error) {
		return Load(Options{HomeDir: dir, SkipAuto: true, Environment: []string{}, ExplicitRoots: roots})
	}
	inc := write("inc.toml", "[dispatch]\nparallel-workers = 7\n")

	off := write("off.toml", "config = { schema-version = 6, includes = false }\n@include "+inc+"\n")
	snap, err := load(off)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Bool("config.includes") || snap.Int("dispatch.parallel-workers") == 7 {
		t.Errorf("includes %v, parallel-workers %d: the root's directive was followed", snap.Bool("config.includes"), snap.Int("dispatch.parallel-workers"))
	}

	for _, tc := range []struct {
		name, root, code, at string
	}{
		{"in an included file", write("parent.toml", "@include "+write("child.toml", "config = { includes = false }\n")+"\n"), "config_includes_not_at_file_root", filepath.Join(dir, "child.toml") + ":1"},
		{"after the first directive", write("late.toml", "@include "+inc+"\nconfig = { includes = true }\n"), "config_includes_too_late", filepath.Join(dir, "late.toml") + ":2"},
	} {
		_, err := load(tc.root)
		var ce *Error
		if !errors.As(err, &ce) || ce.Code != tc.code || ce.Key != "config.includes" || ce.Source.String() != tc.at {
			t.Errorf("%s: err=%v, want %s for config.includes at %s", tc.name, err, tc.code, tc.at)
		}
	}

	old := write("old.toml", "config = { schema-version = 5 }\n")
	right := write("right.toml", "[config]\nschema-version = 6\n")
	_, err = load(old, right)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != "config_schema_version" || ce.Source.String() != old+":1" {
		t.Errorf("schema version inline: err=%v, want config_schema_version at %s:1", err, old)
	}
}
