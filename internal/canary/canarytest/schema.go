package canarytest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SchemaParity marshals value and checks its JSON against the schema file:
// every emitted key must be a declared property, every required key must be
// present, enum and const values must match, and arrays and nested objects
// recurse. It resolves "$ref" to "#/$defs/..." in the same file and to
// "<file>.json#/$defs/..." relative to the schema's directory. It is a
// producer-drift test, not a validator: additionalProperties is ignored on
// purpose, so an emitted key the schema does not name always fails.
func SchemaParity(t testing.TB, schemaPath string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	loader := &schemaLoader{files: map[string]map[string]any{}}
	root := loader.load(t, schemaPath)
	loader.check(t, schemaPath, root, root, decoded, "$")
}

type schemaLoader struct{ files map[string]map[string]any }

func (l *schemaLoader) load(t testing.TB, path string) map[string]any {
	t.Helper()
	if s, ok := l.files[path]; ok {
		return s
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("schema %s: %v", path, err)
	}
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("schema %s: %v", path, err)
	}
	l.files[path] = s
	return s
}

// resolve follows a $ref and returns the node and the file it lives in.
func (l *schemaLoader) resolve(t testing.TB, file string, doc map[string]any, ref string) (map[string]any, string, map[string]any) {
	t.Helper()
	target, fragment, _ := strings.Cut(ref, "#")
	if target != "" {
		file = filepath.Join(filepath.Dir(file), target)
		doc = l.load(t, file)
	}
	node := any(doc)
	for _, part := range strings.Split(strings.TrimPrefix(fragment, "/"), "/") {
		if part == "" {
			continue
		}
		m, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("%s: $ref %q does not resolve", file, ref)
		}
		node, ok = m[part]
		if !ok {
			t.Fatalf("%s: $ref %q does not resolve", file, ref)
		}
	}
	m, ok := node.(map[string]any)
	if !ok {
		t.Fatalf("%s: $ref %q is not an object", file, ref)
	}
	return m, file, doc
}

func (l *schemaLoader) check(t testing.TB, file string, doc, schema map[string]any, value any, path string) {
	t.Helper()
	if ref, ok := schema["$ref"].(string); ok {
		s, f, d := l.resolve(t, file, doc, ref)
		l.check(t, f, d, s, value, path)
		return
	}
	if c, ok := schema["const"]; ok && fmt.Sprint(c) != fmt.Sprint(value) {
		t.Errorf("%s: value %v is not const %v", path, value, c)
	}
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if fmt.Sprint(e) == fmt.Sprint(value) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: value %v is not in enum %v", path, value, enum)
		}
	}
	switch v := value.(type) {
	case map[string]any:
		props, _ := schema["properties"].(map[string]any)
		if _, additional := schema["additionalProperties"].(map[string]any); props == nil && !additional {
			if schema["type"] == "object" {
				t.Errorf("%s: schema declares no properties for an object", path)
			}
			return
		}
		for key, sub := range v {
			ps, ok := props[key].(map[string]any)
			if !ok {
				if additional, ok := schema["additionalProperties"].(map[string]any); ok {
					l.check(t, file, doc, additional, sub, path+"."+key)
					continue
				}
				t.Errorf("%s: emitted key %q is not declared in the schema", path, key)
				continue
			}
			l.check(t, file, doc, ps, sub, path+"."+key)
		}
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				if _, present := v[r.(string)]; !present {
					t.Errorf("%s: required key %q is absent", path, r)
				}
			}
		}
	case []any:
		items, ok := schema["items"].(map[string]any)
		if !ok {
			t.Errorf("%s: schema declares no items for an array", path)
			return
		}
		for i, e := range v {
			l.check(t, file, doc, items, e, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

// SchemaParityAt is SchemaParity against the definition at ref (for example
// "#/$defs/public_job_header") instead of the schema root.
func SchemaParityAt(t testing.TB, schemaPath, ref string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	loader := &schemaLoader{files: map[string]map[string]any{}}
	root := loader.load(t, schemaPath)
	loader.check(t, schemaPath, root, map[string]any{"$ref": ref}, decoded, "$")
}
