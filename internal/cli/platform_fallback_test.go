package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestPlatformFallbackNotMatchedDryRun: under on-unknown = "warn" a row naming
// an unknown platform falls back to generic and is neither selected by
// --select-platform 'cisco*' nor matched by a session-init rule on platform
// ("generic" or "cisco*"); only the catch-all reaches it. A blank row under
// a configured default is planned as that default and is not selected by
// --select-platform naming it.
func TestPlatformFallbackNotMatchedDryRun(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "inv.csv"), []byte("name,management_address,platform\nsw-ios,127.0.0.1,cisco_iosxe\nsw-typo,127.0.0.1,cisco_iosx\nsw-empty,127.0.0.1,\nsw-generic,127.0.0.1,generic\nsw-c9300,127.0.0.1,c9300\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(config, []byte("[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+filepath.Join(dir, "inv.csv")+"\"\nrequired = true\n"+
		"[platform.c9300]\ndriver = \"cisco_iosxe\"\n[platform-resolution]\non-unknown = \"warn\"\ndefault = \"cisco_iosxe\"\n"+
		"[session-init.gen]\ncommands = []\n[session-init.cisco]\ncommands = []\n[session-init.rest]\ncommands = []\n"+
		"[[session-init-map]]\nprofile = \"gen\"\nplatform = \"generic\"\n[[session-init-map]]\nprofile = \"cisco\"\nplatform = \"cisco*\"\n[[session-init-map]]\nprofile = \"rest\"\nname = \"*\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(inputs ...string) string {
		t.Helper()
		args := append([]string{"--config", config}, sets...)
		args = append(args, "run", "--dry-run", "--no-daemon", "--transport", "system", "--format", "jsonl")
		args = append(args, inputs...)
		args = append(args, "--cmd", "show clock")
		var stdout, stderr bytes.Buffer
		if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
			t.Fatalf("%q: exit=%d stderr=%q", inputs, got, stderr.String())
		}
		var report records.PlanReport
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("report: %v\n%s", err, stdout.String())
		}
		out := []string{}
		for i, tr := range report.Targets {
			out = append(out, report.Plan.Targets[i].Device.Name+":"+report.Plan.Targets[i].Device.Platform+":"+tr.IntendedTransport.SessionInitProfile)
		}
		return strings.Join(out, " ")
	}
	if got := run("--select-platform", "cisco*"); got != "sw-ios:cisco_iosxe:cisco" {
		t.Fatalf("cisco* selected %q, want sw-ios only", got)
	}
	if got := run("--select-platform", "cisco_iosxe"); got != "sw-ios:cisco_iosxe:cisco" {
		t.Fatalf("cisco_iosxe selected %q: the blank row under the default is not set", got)
	}
	if got := run("--select-platform", "generic"); got != "sw-generic:generic:gen" {
		t.Fatalf("generic selected %q: the fallen-back row is not set", got)
	}
	if got := run("--all"); got != "sw-ios:cisco_iosxe:cisco sw-typo:generic:rest sw-empty:cisco_iosxe:rest sw-generic:generic:gen sw-c9300:c9300:rest" {
		t.Fatalf("--all: %q", got)
	}
}

// TestPlatformNoticesInTheDryRunReport: the dry-run report carries each
// planning notice as
// a warning finding on its target, stage platform, with the notice's
// details; the report validates and matches the schema; readiness and the
// outcome are unchanged.
func TestPlatformNoticesInTheDryRunReport(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "inv.csv"), []byte("name,management_address,platform\nsw-ios,127.0.0.1,cisco_iosxe\nsw-typo,127.0.0.1,cisco_iosx\nsw-empty,127.0.0.1,\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(config, []byte("[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+filepath.Join(dir, "inv.csv")+"\"\nrequired = true\n[platform-resolution]\non-unknown = \"warn\"\ndefault = \"cisco_iosxe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--config", config}, sets...)
	args = append(args, "run", "--dry-run", "--no-daemon", "--transport", "system", "--format", "jsonl", "--all", "--cmd", "show clock")
	var stdout, stderr bytes.Buffer
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	var report records.PlanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report: %v\n%s", err, stdout.String())
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParity(t, planReportSchema, report)
	findings := []string{}
	for _, tr := range report.Targets {
		if tr.Readiness != records.ReadinessPlanned {
			t.Fatalf("%s readiness %s", tr.TargetID, tr.Readiness)
		}
		for _, f := range tr.Findings {
			findings = append(findings, tr.TargetID+":"+f.Code+"/"+f.Severity+"/"+f.Stage+"/"+f.Details["supplied"]+"/"+f.Details["source"]+"/"+f.Details["used"])
		}
	}
	if got := strings.Join(findings, " "); got != "name:sw-typo:platform_unknown_fallback/warning/platform/cisco_iosx/lab line 3/generic name:sw-empty:platform_not_set/warning/platform//lab line 4/cisco_iosxe" {
		t.Fatalf("findings %q", got)
	}
	if report.Outcome != records.OutcomePlanned || report.Counts.Warnings != 2 {
		t.Fatalf("outcome %s warnings %d", report.Outcome, report.Counts.Warnings)
	}
}

// TestPlatformNoticesInTheExerciseReport is the same for the exercise:
// the daemon's report carries the committed plan's platform
// notice as a warning finding on the target, stage platform, and the target
// stays ready.
func TestPlatformNoticesInTheExerciseReport(t *testing.T) {
	_, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "inv.csv"), []byte("name,management_address,platform\nlab-1,127.0.0.1,\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(config, []byte("[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+filepath.Join(dir, "inv.csv")+"\"\nrequired = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--config", config}, sets...)
	args = append(args, "run", "--exercise", "--target", "lab-1", "--transport", "system", "--format", "json", "show", "clock")
	var stdout, stderr bytes.Buffer
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	var report records.PlanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report: %v\n%s", err, stdout.String())
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParity(t, planReportSchema, report)
	if len(report.Targets) != 1 || report.Targets[0].Readiness != records.ReadinessReady || report.Outcome != records.OutcomeExercised {
		t.Fatalf("report: outcome=%s targets=%+v", report.Outcome, report.Targets)
	}
	found := false
	for _, f := range report.Targets[0].Findings {
		if f.Code == "platform_not_set" && f.Severity == "warning" && f.Stage == "platform" && f.Details["used"] == "cisco_iosxe" && f.Details["source"] == "lab line 2" {
			found = true
		}
	}
	if !found || !strings.Contains(stderr.String(), "warning: platform_not_set: inventory source lab: 1 device has no platform") {
		t.Fatalf("findings %+v stderr %q", report.Targets[0].Findings, stderr.String())
	}
}

// TestEnableRuleOnThePlatformUsed: a
// blank row under platform-resolution.default = c9300, whose table has
// requires-enable = true, needs an enable secret before any contact
// (credential_enable_missing without a prompt); the same row under no
// default runs as generic and needs none, so it reaches the connection.
func TestEnableRuleOnThePlatformUsed(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := append(dryRunSets(t, base), "--set", "creds.interactive-prompt=false")
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	os.Unsetenv("NETENABLE")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "inv.csv"), []byte("name,management_address,platform\nsw-empty,127.0.0.1,\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configFor := func(extra string) string {
		config := filepath.Join(t.TempDir(), "karvi.toml")
		if err := os.WriteFile(config, []byte("[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+filepath.Join(dir, "inv.csv")+"\"\nrequired = true\n[platform.c9300]\ndriver = \"cisco_iosxe\"\nrequires-enable = true\n"+extra), 0o600); err != nil {
			t.Fatal(err)
		}
		return config
	}
	run := func(config string) (int, string) {
		var stdout, stderr bytes.Buffer
		args := append([]string{"--config", config}, sets...)
		args = append(args, "command", "--transport", "system", "sw-empty", "show", "clock")
		return Main(args, strings.NewReader(""), &stdout, &stderr), stderr.String()
	}
	if got, stderr := run(configFor("[platform-resolution]\ndefault = \"c9300\"\n")); got != exitcode.ExitCredentialResolutionError || !strings.Contains(stderr, "credential_enable_missing") {
		t.Fatalf("under the default: exit=%d stderr=%q", got, stderr)
	}
	if got, stderr := run(configFor("")); strings.Contains(stderr, "credential_enable_missing") || got == exitcode.ExitCredentialResolutionError {
		t.Fatalf("under no default: exit=%d stderr=%q", got, stderr)
	}
}

// TestExerciseRefusesPlatformUnknownToTheDaemon is the daemon's backstop
// seen from the client's side: the client's configuration has an
// alias table the daemon's lacks, so the exercise report carries an error
// finding platform_unknown, stage platform, and the target is not ready; a
// live job would refuse the device before any connection.
func TestExerciseRefusesPlatformUnknownToTheDaemon(t *testing.T) {
	_, sets, _, _, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "inv.csv"), []byte("name,management_address,platform\nsw-c9300,127.0.0.1,c9300\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(config, []byte("[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+filepath.Join(dir, "inv.csv")+"\"\nrequired = true\n[platform.c9300]\ndriver = \"cisco_iosxe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--config", config}, sets...)
	args = append(args, "run", "--exercise", "--target", "sw-c9300", "--transport", "system", "--format", "json", "show", "clock")
	var stdout, stderr bytes.Buffer
	got := Main(args, strings.NewReader(""), &stdout, &stderr)
	var report records.PlanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("exit=%d report: %v\n%s\n%s", got, err, stdout.String(), stderr.String())
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParity(t, planReportSchema, report)
	found := false
	for _, f := range report.Targets[0].Findings {
		if f.Code == "platform_unknown" && f.Severity == "error" && f.Stage == "platform" && f.Details["platform"] == "c9300" {
			found = true
		}
	}
	if !found || report.Targets[0].Readiness != records.ReadinessNotReady || report.Outcome != records.OutcomeNotReady {
		t.Fatalf("exit=%d outcome=%s target=%+v", got, report.Outcome, report.Targets[0])
	}
}
