package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/records"
)

// testCommon is g's one read as the common options, the error itself on a
// failure.
func testCommon(g globalOptions) (app.CommonOptions, error) {
	cfg, operator, err := app.ReadConfig(app.ConfigRequest{Roots: g.configs, Sets: g.sets, Flags: g.flags()}, nil)
	return app.CommonOptions{Config: cfg, Operator: operator, Quiet: g.quiet, Debug: g.debug}, err
}

// testRuntime is the daemon's runtime from g's one read.
func testRuntime(g globalOptions) (app.DaemonRuntime, error) {
	common, err := testCommon(g)
	if err != nil {
		return app.DaemonRuntime{}, err
	}
	return app.ResolveDaemonRuntime(common)
}

// ensureTestDaemon launches the daemon as run does: the read, the runtime,
// then ensureDaemon.
func ensureTestDaemon(ctx context.Context, g globalOptions, stderr io.Writer) (app.DaemonRuntime, error) {
	rt, err := testRuntime(g)
	if err != nil {
		return rt, err
	}
	return rt, ensureDaemon(ctx, rt, g, stderr)
}

// TestOneReadRefusalOrder: a mistake on the command line is said before one
// in the configuration, with --tf or without. Under a configuration that does
// not load, run's mode conflict, a commands file that cannot be read, and a
// declaration conflict are refused as they are without --tf, exit 4; job
// follow and job cancel refuse a malformed ID, no daemon running, with
// job_request_malformed; and a command line without a mistake is refused for
// its configuration.
func TestOneReadRefusalOrder(t *testing.T) {
	base := t.TempDir()
	sets := dryRunSets(t, base)
	bad := filepath.Join(base, "bad.toml")
	targets := filepath.Join(base, "t.txt")
	absent := filepath.Join(base, "absent.txt")
	if err := os.WriteFile(bad, []byte("[dispatch]\nnope = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targets, []byte("127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		code string
		exit int
	}{
		{"run --dry-run --exercise --tf", []string{"run", "--dry-run", "--exercise", "--tf", targets, "show clock"}, "run_mode_conflict", exitcode.ExitUsageError},
		{"run --tf --cf", []string{"run", "--no-daemon", "--tf", targets, "--cf", absent}, "commands_file_unreadable", exitcode.ExitUsageError},
		{"command --tf --cf", []string{"command", "--tf", targets, "--cf", absent}, "commands_file_unreadable", exitcode.ExitUsageError},
		{"run --tf --blind --timeout", []string{"run", "--no-daemon", "--tf", targets, "--cmd", "show clock", "--blind", "--timeout", "5s"}, "timeout_with_blind", exitcode.ExitUsageError},
		{"job follow", []string{"job", "follow", "not-an-id"}, "job_request_malformed", exitcode.ExitJobRejected},
		{"job cancel", []string{"job", "cancel", "not-an-id"}, "job_request_malformed", exitcode.ExitJobRejected},
		{"run --tf", []string{"run", "--no-daemon", "--tf", targets, "show clock"}, "config_unknown_key", exitcode.ExitConfigValidationError},
	} {
		var stdout, stderr bytes.Buffer
		code := Main(append(append(append([]string{}, sets...), "--config", bad), tc.args...), strings.NewReader(""), &stdout, &stderr)
		if code != tc.exit || !strings.HasPrefix(stderr.String(), tc.code+": ") {
			t.Errorf("%s: exit %d stderr=%q, want %d %s", tc.name, code, stderr.String(), tc.exit, tc.code)
		}
	}
}

// TestOneReadDryRunTiming: the dry run's report gives the invocation's read
// as client_config_ns, and its total_ns, from the read's start, covers every
// client stage.
func TestOneReadDryRunTiming(t *testing.T) {
	base := t.TempDir()
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	args := append(dryRunSets(t, base), "run", "--dry-run", "--no-daemon", "--format", "json", "--target", "127.0.0.1", "--transport", "system", "show", "clock")
	var stdout, stderr bytes.Buffer
	if code := Main(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d stderr=%q", code, stderr.String())
	}
	var report records.PlanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	tm := report.Timing
	if tm.ClientConfigNS == nil || *tm.ClientConfigNS <= 0 || tm.ClientInventoryNS == nil || tm.ClientDNSNS == nil || tm.ClientCredentialNS == nil {
		t.Fatalf("timing: %+v", tm)
	}
	if stages := *tm.ClientConfigNS + *tm.ClientInventoryNS + *tm.ClientDNSNS + *tm.ClientCredentialNS; tm.TotalNS < stages {
		t.Errorf("total_ns %d is less than the client stages' %d", tm.TotalNS, stages)
	}
}
