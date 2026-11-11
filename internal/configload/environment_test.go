package configload

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestUnknownEnvironmentVariable: a KARVI__ variable that names no key is
// refused, the variable named, under the default config.reject-unknown-env;
// set false, it is ignored with a warning naming it.
func TestUnknownEnvironmentVariable(t *testing.T) {
	env := []string{"KARVI__NO__SUCH_KEY=1"}
	_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: env})
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != "config_unknown_environment" || ce.Source.Layer != "environment" || ce.Source.Path != "KARVI__NO__SUCH_KEY" {
		t.Fatalf("default: %v", err)
	}
	path := filepath.Join(t.TempDir(), "karvi.toml")
	if err := os.WriteFile(path, []byte("[config]\nreject-unknown-env = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: env, ExplicitRoots: []string{path}})
	if err != nil {
		t.Fatalf("tolerated: %v", err)
	}
	if !slices.Contains(snap.Warnings, "ignored unknown environment variable KARVI__NO__SUCH_KEY") {
		t.Fatalf("tolerated: warnings %q", snap.Warnings)
	}
}
