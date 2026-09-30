package canarytest

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/canary"
)

// Seed returns a fresh canary and logs it so a failure can be traced.
func Seed(t testing.TB) canary.Value {
	t.Helper()
	v := canary.New()
	t.Logf("canary %s", v.Raw)
	return v
}

// Exercise runs canary.Exercise and reports every finding as a test error.
func Exercise(t testing.TB, value any, profile canary.Profile, seeds ...canary.Value) {
	t.Helper()
	for _, f := range canary.Exercise(value, profile, seeds...) {
		t.Error(f)
	}
}

const (
	panicEnv = "KARVI_CANARY_PANIC"
	valueEnv = "KARVI_CANARY_VALUE"
)

// PanicRequested is called by a package's panic helper test: it returns the
// canary to panic with when PanicOutput re-executed the test binary.
func PanicRequested() (canary.Value, bool) {
	if os.Getenv(panicEnv) != "1" {
		return canary.Value{}, false
	}
	return canary.Value{Raw: os.Getenv(valueEnv)}, true
}

// PanicOutput re-runs the current test binary on helperTest with the canary
// in the environment, expects the helper to panic, and scans everything the
// process printed. Panic output cannot return an error, so this sink is
// proven by scan.
func PanicOutput(t testing.TB, helperTest string, seed canary.Value) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+helperTest+"$", "-test.v")
	cmd.Env = append(os.Environ(), panicEnv+"=1", valueEnv+"="+seed.Raw)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s did not panic; output:\n%s", helperTest, out)
	}
	if !strings.Contains(string(out), "panic:") {
		t.Fatalf("%s exited without a panic; output:\n%s", helperTest, out)
	}
	for _, h := range canary.Scan("panic output", out, seed) {
		t.Errorf("panic output leaks the canary (%s at %d):\n%s", h.Encoding, h.Offset, out)
	}
}
