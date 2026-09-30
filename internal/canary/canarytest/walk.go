// Package canarytest holds the test helpers that prove the secret-handling
// contracts: the structural walker for non-secret types, the refusal
// harness, and the schema-parity check.
package canarytest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Allow lists what the walker accepts beyond the basic scalar kinds.
type Allow struct {
	// Types accepted as leaves by their full name, e.g. "net/netip.Addr".
	Leaves []string
	// Fields accepted by "<Type>.<Field>" even though their type would fail,
	// e.g. a []byte that carries ciphertext by design.
	Fields []string
}

var basicKinds = map[reflect.Kind]bool{
	reflect.Bool: true, reflect.String: true,
	reflect.Int: true, reflect.Int8: true, reflect.Int16: true, reflect.Int32: true, reflect.Int64: true,
	reflect.Uint: true, reflect.Uint8: true, reflect.Uint16: true, reflect.Uint32: true, reflect.Uint64: true,
	reflect.Float32: true, reflect.Float64: true,
}

func typeName(t reflect.Type) string {
	if t.PkgPath() == "" {
		return t.String()
	}
	return t.PkgPath() + "." + t.Name()
}

// Walk fails the test when any field reachable from typ is an interface,
// any, a function, a channel, an unsafe pointer, a []byte, a type from
// internal/secrets, or a type that exposes WithBytes (the signature of every
// secret-bearing value), unless the allow-list names it. Maps must have
// string keys. It is the structural proof that a contract cannot carry a
// secret.
func Walk(t testing.TB, typ reflect.Type, allow Allow) {
	t.Helper()
	leaves := map[string]bool{}
	for _, l := range allow.Leaves {
		leaves[l] = true
	}
	fields := map[string]bool{}
	for _, f := range allow.Fields {
		fields[f] = true
	}
	seen := map[reflect.Type]bool{}
	var walk func(t reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		if seen[rt] {
			return
		}
		name := typeName(rt)
		if leaves[name] {
			return
		}
		if strings.HasSuffix(rt.PkgPath(), "/internal/secrets") || isSecretBearing(rt) {
			t.Errorf("%s: type %s is secret-bearing", path, name)
			return
		}
		switch rt.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			if rt.Kind() == reflect.Slice && rt.Elem().Kind() == reflect.Uint8 && !fields[path] {
				t.Errorf("%s: []byte is not an allowed leaf", path)
				return
			}
			walk(rt.Elem(), path+"[]")
		case reflect.Map:
			if rt.Key().Kind() != reflect.String {
				t.Errorf("%s: map key must be a string", path)
			}
			walk(rt.Elem(), path+"[]")
		case reflect.Struct:
			seen[rt] = true
			for i := 0; i < rt.NumField(); i++ {
				f := rt.Field(i)
				fieldPath := rt.Name() + "." + f.Name
				if fields[fieldPath] {
					continue
				}
				walk(f.Type, fieldPath)
			}
		case reflect.Interface, reflect.Func, reflect.Chan, reflect.UnsafePointer:
			t.Errorf("%s: kind %s cannot appear in a non-secret contract", path, rt.Kind())
		default:
			if !basicKinds[rt.Kind()] {
				t.Errorf("%s: kind %s is not an allowed leaf", path, rt.Kind())
			}
		}
	}
	walk(typ, typ.Name())
}

// isSecretBearing reports whether rt or *rt has a WithBytes method.
func isSecretBearing(rt reflect.Type) bool {
	if _, ok := rt.MethodByName("WithBytes"); ok {
		return true
	}
	if rt.Kind() != reflect.Pointer {
		_, ok := reflect.PointerTo(rt).MethodByName("WithBytes")
		return ok
	}
	return false
}

// Imports returns the import paths of the non-test Go files in dir.
func Imports(t testing.TB, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// Reachable returns every module-internal package the non-test Go files in
// dir import, at any depth, as import paths (the proof mechanism of the
// transitive import exclusion). dir is
// relative to the calling test's directory or absolute.
func Reachable(t testing.TB, dir string) []string {
	t.Helper()
	root, module := moduleRoot(t)
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var out []string
	var walk func(string)
	walk = func(d string) {
		for _, imp := range Imports(t, d) {
			if !strings.HasPrefix(imp, module+"/") || seen[imp] {
				continue
			}
			seen[imp] = true
			out = append(out, imp)
			walk(filepath.Join(root, strings.TrimPrefix(imp, module+"/")))
		}
	}
	walk(abs)
	sort.Strings(out)
	return out
}

// AssertUnreachable fails when any forbidden package, or a package under a
// forbidden prefix ending in "/", is reachable from dir.
func AssertUnreachable(t testing.TB, dir string, forbidden ...string) {
	t.Helper()
	for _, imp := range Reachable(t, dir) {
		for _, f := range forbidden {
			if imp == f || (strings.HasSuffix(f, "/") && strings.HasPrefix(imp, f)) {
				t.Errorf("%s reaches %s", dir, imp)
			}
		}
	}
}

// moduleRoot finds go.mod above the working directory and reads the module
// path from it.
func moduleRoot(t testing.TB) (root, module string) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "module ") {
					return dir, strings.TrimSpace(strings.TrimPrefix(line, "module "))
				}
			}
			t.Fatalf("%s/go.mod has no module line", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the working directory")
		}
		dir = parent
	}
}
