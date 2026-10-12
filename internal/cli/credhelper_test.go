package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var boundUser = regexp.MustCompile(`backend=[a-z-]* user=([^ ;]*)`)

// helperRuntime is a dry run's configuration with a credential helper that
// counts its runs and sets NETUSER to helperN on its Nth; count reads the
// runs so far.
func helperRuntime(t *testing.T) (sets []string, count func() int) {
	t.Helper()
	base := t.TempDir()
	runs := filepath.Join(base, "runs")
	helper := filepath.Join(base, "credh")
	body := fmt.Sprintf("#!/bin/sh\necho x >>%s\nn=$(wc -l <%s)\necho \"export NETUSER=helper$n NETPASS=fromhelper\"\n", runs, runs)
	if err := os.WriteFile(helper, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETUSER", "exported")
	t.Setenv("NETPASS", "p")
	sets = append(dryRunSets(t, base), "--set", fmt.Sprintf("creds.helper=%q", helper), "--set", `creds.helper-unavailable="fail"`)
	return sets, func() int {
		b, _ := os.ReadFile(runs)
		return strings.Count(string(b), "x")
	}
}

func boundUsers(stdout string) []string {
	var users []string
	for _, m := range boundUser.FindAllStringSubmatch(stdout, -1) {
		users = append(users, m[1])
	}
	return users
}

// TestStreamHoldsTheCredentialHelper is EXAMPLES chapter 56, issue 8: a
// stream runs the helper at its first job's credential step and holds its
// values for every later job, its NETUSER overriding the exported one;
// --restart lets the next job run it again; --no-credh, on the stream's
// command line or as a draft line, neither runs nor reads it.
func TestStreamHoldsTheCredentialHelper(t *testing.T) {
	job := "--target 127.0.0.1\n--transport system\n--dry-run\n--no-daemon\nshow clock\n--go\n"
	for _, tc := range []struct {
		name, flag, in string
		users          string
		runs           int
	}{
		{"held", "", job + "show version\n--go\nshow ip route\n--go\n--end\n", "helper1 helper1 helper1", 1},
		{"restart", "", job + "show version\n--go\n--restart\n" + job + "--end\n", "helper1 helper1 helper2", 2},
		{"stream --no-credh", "--no-credh", job + "--restart\n" + job + "--end\n", "exported exported", 0},
		{"draft line", "", job + "--no-credh\nshow version\n--go\n--reset\n" + job + "--end\n", "helper1 exported helper1", 1},
	} {
		sets, count := helperRuntime(t)
		args := append(append([]string{}, sets...), "stream")
		if tc.flag != "" {
			args = append(args, tc.flag)
		}
		var stdout, stderr bytes.Buffer
		if got := Main(args, strings.NewReader(tc.in), &stdout, &stderr); got != 0 {
			t.Fatalf("%s: exit %d stderr=%q", tc.name, got, stderr.String())
		}
		if got := strings.Join(boundUsers(stdout.String()), " "); got != tc.users || count() != tc.runs {
			t.Errorf("%s: users %q after %d runs, want %q after %d\n%s", tc.name, got, count(), tc.users, tc.runs, stderr.String())
		}
	}
}

// TestCredentialHelperRefusesTheRun: a helper that fails refuses the
// invocation at planning with its code; --no-credh on run reads none.
func TestCredentialHelperRefusesTheRun(t *testing.T) {
	sets, count := helperRuntime(t)
	broken := filepath.Join(t.TempDir(), "broken")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\necho 'NETPASS=s3cretX'\nexit 4\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	args := append(append([]string{}, sets...), "--set", fmt.Sprintf("creds.helper=%q", broken), "run", "--dry-run", "--no-daemon", "--target", "127.0.0.1", "--transport", "system", "show", "clock")
	var stdout, stderr bytes.Buffer
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 6 || !strings.Contains(stderr.String(), "credential_helper_failed: ") || !strings.Contains(stderr.String(), broken+" exited 4; run it by hand to see its message") || strings.Contains(stderr.String(), "s3cretX") {
		t.Errorf("exit %d stderr=%q", got, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	args = append(append([]string{}, sets...), "run", "--dry-run", "--no-daemon", "--no-credh", "--target", "127.0.0.1", "--transport", "system", "show", "clock")
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 || strings.Join(boundUsers(stdout.String()), " ") != "exported" || count() != 0 {
		t.Errorf("--no-credh: exit %d users %q runs %d stderr=%q", got, boundUsers(stdout.String()), count(), stderr.String())
	}
}
