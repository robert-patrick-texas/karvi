package configload

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/configschema"
)

// TestReferenceLoads: the rendered reference configuration loads, and every
// registry key is read from it, at its default, from the line that assigns
// it under its own table (a top-level key before every table). A table opened
// twice refuses the file; a top-level key after a table header is read as
// that table's key.
func TestReferenceLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reference.toml")
	text := RenderReference()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	snap, err := Load(Options{HomeDir: home, SkipAuto: true, Environment: []string{}, ExplicitRoots: []string{path}})
	if err != nil {
		t.Fatalf("the reference does not load: %v", err)
	}
	lines := strings.Split(text, "\n")
	for _, e := range configschema.Entries() {
		v, ok := snap.Values[e.Path]
		if !ok {
			t.Errorf("%s: not loaded", e.Path)
			continue
		}
		if v.Source.Path != path || v.Source.Line < 1 || v.Source.Line > len(lines) {
			t.Errorf("%s: read from %s, not the reference", e.Path, v.Source)
			continue
		}
		table, key := "", e.Path
		if i := strings.LastIndexByte(e.Path, '.'); i >= 0 {
			table, key = e.Path[:i], e.Path[i+1:]
		}
		if !strings.HasPrefix(lines[v.Source.Line-1], quoteKey(key)+" = ") {
			t.Errorf("%s: line %d is %q", e.Path, v.Source.Line, lines[v.Source.Line-1])
		}
		under := ""
		for i := v.Source.Line - 2; i >= 0; i-- {
			if strings.HasPrefix(lines[i], "[") {
				under = strings.Trim(lines[i], "[]")
				break
			}
		}
		if under != table {
			t.Errorf("%s: under [%s]", e.Path, under)
		}
		want := v.Default
		if words, ok := configschema.Place(e.Path); ok {
			if want, err = absolutePlace(want, words, home); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(v.Data, want) {
			t.Errorf("%s: %v, not its default %v", e.Path, v.Data, want)
		}
	}
}
