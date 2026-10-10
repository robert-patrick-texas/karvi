package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/testdir"
	"github.com/robert-patrick-texas/karvi/records"
)

// loadWarningLines are the two lines a load of warnConfig says under
// KARVI__NOPE: the missing optional include and the ignored variable.
func loadWarningLines(cfg string) []string {
	return []string{
		fmt.Sprintf("warning: optional include missing: %s:1 -> %s\n", cfg, filepath.Join(filepath.Dir(cfg), "absent.toml")),
		"warning: ignored unknown environment variable KARVI__NOPE\n",
	}
}

// warnConfig writes a configuration whose load raises both warnings, with
// KARVI__NOPE set for the test, and returns its path and the dry-run sets
// under a base directory of the test.
func warnConfig(t *testing.T) (string, []string) {
	t.Helper()
	dir := testdir.Short(t)
	cfg := filepath.Join(dir, "warn.toml")
	if err := os.WriteFile(cfg, []byte("@include? absent.toml\n[config]\nreject-unknown-env = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KARVI__NOPE", "1")
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	t.Setenv(loginTranscriptChildEnv, "")
	return cfg, append([]string{"--config", cfg}, dryRunSets(t, base)...)
}

// saidOnce fails unless stderr holds each of want exactly once.
func saidOnce(t *testing.T, name, stderr string, want []string) {
	t.Helper()
	for _, w := range want {
		if n := strings.Count(stderr, w); n != 1 {
			t.Errorf("%s: %q said %d times, want once; stderr:\n%s", name, strings.TrimSuffix(w, "\n"), n, stderr)
		}
	}
}

// TestLoadWarningsSaidOncePerInvocation: every invocation that loads its
// configuration says the load's warnings once on standard error, under
// --quiet and a machine format too, at its one read, a stream's at its
// start; a second read in a process would say them again. config validate
// keeps its report on standard output; --help says nothing.
func TestLoadWarningsSaidOncePerInvocation(t *testing.T) {
	cfg, sets := warnConfig(t)
	want := loadWarningLines(cfg)
	main := func(stdin string, args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Main(append(append([]string{}, sets...), args...), strings.NewReader(stdin), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"config show", []string{"config", "show", "basedir"}},
		{"config show --quiet", []string{"--quiet", "config", "show", "basedir"}},
		{"config colors", []string{"config", "colors"}},
		{"watch", []string{"watch", "--format", "json", "--once"}},
		{"run --dry-run --no-daemon", []string{"run", "--dry-run", "--no-daemon", "--target", "127.0.0.1", "--transport", "system", "show", "clock"}},
		{"run --dry-run --no-daemon --format json", []string{"run", "--dry-run", "--no-daemon", "--format", "json", "--target", "127.0.0.1", "--transport", "system", "show", "clock"}},
	} {
		code, stdout, stderr := main("", tc.args...)
		if code != 0 {
			t.Fatalf("%s: exit %d stdout=%q stderr=%q", tc.name, code, stdout, stderr)
		}
		saidOnce(t, tc.name, stderr, want)
		if strings.Contains(stdout, "warning: ") {
			t.Errorf("%s: a warning line on standard output:\n%s", tc.name, stdout)
		}
	}

	code, stdout, stderr := main("", "config", "validate")
	if code != 0 || !strings.Contains(stdout, "\nwarnings: 2\n") {
		t.Fatalf("config validate: exit %d stdout=%q", code, stdout)
	}
	saidOnce(t, "config validate", stderr, want)
	code, stdout, stderr = main("", "config", "validate", "--format", "json")
	var report struct{ Warnings []string }
	if err := json.Unmarshal([]byte(stdout), &report); code != 0 || err != nil || len(report.Warnings) != 2 {
		t.Fatalf("config validate --format json: exit %d %v stdout=%q", code, err, stdout)
	}
	saidOnce(t, "config validate --format json", stderr, want)

	// Two jobs, one reading at the stream's start: each warning once.
	in := "--no-daemon\n--dry-run\n--target 127.0.0.1\n--transport system\nshow clock\n--go\nshow version\n--go\n--end\n"
	code, stdout, stderr = main(in, "stream")
	if code != 0 || strings.Count(stdout, "outcome: planned") != 2 {
		t.Fatalf("stream: exit %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	saidOnce(t, "stream", stderr, want)

	// A finished job's folder, no daemon: the follow's one read.
	g := globalsFor(sets)
	g.configs = []string{cfg}
	finishedDirectory(t, g, fixtureJobID, records.Summary{FinalStatus: "completed", ExitCode: 0, ExitName: "ExitSuccess"})
	if code, stdout, stderr = main("", "job", "follow", fixtureJobID, "--format", "jsonl"); code != 0 {
		t.Fatalf("job follow: exit %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	saidOnce(t, "job follow", stderr, want)

	if code, _, stderr := main("", "--help"); code != 0 || stderr != "" {
		t.Errorf("--help: exit %d stderr=%q", code, stderr)
	}
}

// TestLoadWarningsRecordedLogin: a login says the load's warnings beside a
// refusal after its load; a recorded login's wrapper says them and refuses
// before its child; the child, marked by the wrapper's variable, says none.
func TestLoadWarningsRecordedLogin(t *testing.T) {
	cfg, sets := warnConfig(t)
	want := loadWarningLines(cfg)
	login := func(args ...string) (int, string) {
		var stdout, stderr bytes.Buffer
		code := Main(append(append(append([]string{}, sets...), "login", "--platform", "nosuch"), args...), strings.NewReader(""), &stdout, &stderr)
		return code, stderr.String()
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"login", []string{"127.0.0.1"}},
		{"login --record", []string{"--record", "127.0.0.1"}},
	} {
		code, stderr := login(tc.args...)
		if code == 0 || !strings.Contains(stderr, "platform_option_unknown") {
			t.Fatalf("%s: exit %d stderr=%q", tc.name, code, stderr)
		}
		saidOnce(t, tc.name, stderr, want)
	}
	t.Setenv(loginTranscriptChildEnv, "1")
	code, stderr := login("127.0.0.1")
	if code == 0 || !strings.Contains(stderr, "platform_option_unknown") || strings.Contains(stderr, "warning: ") {
		t.Fatalf("the recorded login's child: exit %d stderr=%q", code, stderr)
	}
}

// TestLoadWarningsDaemonServeLog: daemon serve logs its own load's warnings
// as slog WARN lines at its start, before anything else; a socket
// directory too long ends it after the load, making nothing.
func TestLoadWarningsDaemonServeLog(t *testing.T) {
	cfg, sets := warnConfig(t)
	long := filepath.Join(testdir.Short(t), strings.Repeat("x", 100))
	var stdout, stderr bytes.Buffer
	args := append(append([]string{}, sets...), "--set", fmt.Sprintf("daemon.sockets=%q", long), "daemon", "serve")
	if code := Main(args, strings.NewReader(""), &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "daemon_socket_too_long") {
		t.Fatalf("exit %d stderr=%q", code, stderr.String())
	}
	for _, w := range []string{
		fmt.Sprintf(`level=WARN msg="configuration warning" warning="optional include missing: %s:1 -> %s"`, cfg, filepath.Join(filepath.Dir(cfg), "absent.toml")),
		`level=WARN msg="configuration warning" warning="ignored unknown environment variable KARVI__NOPE"`,
	} {
		if n := strings.Count(stderr.String(), w); n != 1 {
			t.Errorf("%q logged %d times, want once:\n%s", w, n, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), "warning: ") {
		t.Errorf("a client's warning line in the daemon's log:\n%s", stderr.String())
	}
}
