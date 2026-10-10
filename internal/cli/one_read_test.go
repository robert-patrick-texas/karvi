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

// chunkReader gives one chunk a Read, calling between before the second: the
// stream asks for a line only after the job before it ran, so between runs
// after the first chunk's jobs.
type chunkReader struct {
	chunks  []string
	between func()
	n       int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.n == len(c.chunks) {
		return 0, io.EOF
	}
	if c.n == 1 {
		c.between()
	}
	k := copy(p, c.chunks[c.n])
	c.n++
	return k, nil
}

// TestOneReadStream: a stream reads its configuration once, as it starts. A
// configuration that does not load is refused there, once, before a line is
// read; an edit after the first job reaches no later job of the stream; and a
// job's own option lines set their keys over the one reading.
func TestOneReadStream(t *testing.T) {
	base := t.TempDir()
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	file := filepath.Join(base, "config.toml")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	job := "--no-daemon\n--dry-run\n--target 127.0.0.1\n--transport system\n--dispatch parallel\nshow clock\n--go\n"
	stream := func(stdin io.Reader) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Main(append(append([]string{}, sets...), "--config", file, "stream"), stdin, &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}

	write("[dispatch]\nnope = 1\n")
	code, stdout, stderr := stream(strings.NewReader(job + job))
	if code != exitcode.ExitConfigValidationError || stdout != "" || strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "config_unknown_key: ") {
		t.Fatalf("a broken configuration: exit %d stdout=%q stderr=%q", code, stdout, stderr)
	}

	write("[dispatch]\nparallel-workers = 4\n")
	in := &chunkReader{chunks: []string{job, "show version\n--go\n--workers 2\nshow clock\n--go\n--end\n"}, between: func() { write("[dispatch]\nparallel-workers = 8\n") }}
	code, stdout, stderr = stream(in)
	var dispatch []string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "dispatch: ") {
			dispatch = append(dispatch, line)
		}
	}
	want := []string{"dispatch: parallel width=4 order=default", "dispatch: parallel width=4 order=default", "dispatch: parallel width=2 order=default"}
	if code != 0 || in.n != 2 || strings.Join(dispatch, "\n") != strings.Join(want, "\n") {
		t.Fatalf("exit %d, %d chunks read, dispatch %q, want %q; stderr=%q", code, in.n, dispatch, want, stderr)
	}
}
