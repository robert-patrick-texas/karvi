package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// TestJobListParses: job list takes its five options and no positional,
// --target as run's with its aliases, and an abbreviation of the word.
func TestJobListParses(t *testing.T) {
	inv, err := Parse([]string{"j", "l", "--last", "3", "--failed", "-t", "dead*", "--target", "!core*", "--operator", "netops", "--op", "1001", "--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Path != "job list" || inv.Int(optLast) != 3 || !inv.Flag(optFailed) || strings.Join(targetValues(inv), " ") != "target=dead* target=!core*" || strings.Join(inv.Strings(optOperator), " ") != "netops 1001" || inv.String(optFormatTJ) != "json" {
		t.Fatalf("parsed %+v", inv)
	}
	for args, code := range map[string]string{"job list x": "cli_positional_unexpected", "job list --last x": "cli_option_value_invalid", "job list --format jsonl": "cli_option_value_invalid", "job list --target": "cli_option_value_missing"} {
		if got, msg := parseCode(t, strings.Fields(args)...); got != code {
			t.Errorf("%s: %s (%s), want %s", args, got, msg, code)
		}
	}
}

// listGlobal is a configuration whose output root is root, every other
// path under dir.
func listGlobal(dir, root string) globalOptions {
	return globalOptions{sets: []string{`basedir="` + filepath.Join(dir, "base") + `"`, `sharedroot="none"`, `output.root="` + root + `"`, `display.color="always"`}}
}

func runList(t *testing.T, g globalOptions, args ...string) (int, string, string) {
	t.Helper()
	inv, err := Parse(append([]string{"job", "list"}, args...))
	if err != nil {
		t.Fatal(err)
	}
	inv.Global = g
	var stdout, stderr bytes.Buffer
	code := jobList(inv, app.IO{Stdout: &stdout, Stderr: &stderr})
	return code, stdout.String(), stderr.String()
}

// TestJobListText: the table to a pipe, every name whole and coloured
// under display.color = always; an empty root says so; --last below 0 is a
// usage error, and --target values of which none selects are refused as
// run refuses them, before any folder is read; the JSON is an array, [] for
// none.
func TestJobListText(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "jobs")
	g := listGlobal(dir, root)
	if code, out, errs := runList(t, g); code != 0 || out != "(no jobs)\n" || errs != "" {
		t.Fatalf("empty: %d %q %q", code, out, errs)
	}
	if code, out, _ := runList(t, g, "--format", "json"); code != 0 || out != "[]\n" {
		t.Fatalf("empty json: %d %q", code, out)
	}
	job := filepath.Join(root, "261016", "261016-002853-00")
	if err := os.MkdirAll(job, 0o700); err != nil {
		t.Fatal(err)
	}
	summary := `{"schema_version":4,"job_id":"261016-002853-00","kind":"run","devices":[{"name":"fake-iosxe","outcome":"succeeded"},{"name":"dead-iosxe","outcome":"failed"}],"final_status":"errored","primary_exit_code":101,"duration_ns":4000000000,"device_counts":{"total":2,"completed":2,"failed":1}}`
	if err := os.WriteFile(filepath.Join(job, "summary.json"), []byte(summary), 0o640); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runList(t, g)
	lines := strings.Split(strings.TrimSuffix(display.StripANSI(out), "\n"), "\n")
	if code != 0 || errs != "" || len(lines) != 2 || !strings.HasPrefix(lines[1], "  261016-002853-00  0m04s     errored     ") || !strings.HasSuffix(lines[1], "  run     2/2       1  fake-iosxe dead-iosxe") || !strings.Contains(out, "\x1b[") {
		t.Fatalf("text: %d %q %q", code, out, errs)
	}
	code, out, _ = runList(t, g, "--target", "dead*")
	if lines := strings.Split(display.StripANSI(out), "\n"); code != 0 || !strings.HasPrefix(lines[0], "  JOB-ID            TIME      OPERATOR  ") || !strings.HasSuffix(lines[1], "run   failed       dead-iosxe") {
		t.Fatalf("--target: %d %q", code, out)
	}
	code, out, _ = runList(t, g, "--format", "json")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); code != 0 || err != nil || len(rows) != 1 || rows[0]["path"] != job || rows[0]["kind"] != "run" {
		t.Fatalf("json: %d %v %q", code, err, out)
	}
	if code, _, errs := runList(t, g, "--last", "-1"); code != exitcode.ExitUsageError || !strings.Contains(errs, "cli_option_value_invalid: --last takes 0 or more, not -1") {
		t.Fatalf("--last -1: %d %q", code, errs)
	}
	// The refusal comes before the configuration is read: an unreadable
	// root would have answered otherwise.
	bad := listGlobal(dir, root)
	bad.sets = append(bad.sets, `timezone="Nowhere/Nothing"`)
	if code, _, errs := runList(t, bad, "--target", "!dead*"); code != exitcode.ExitInventoryError || !strings.Contains(errs, "inventory_positive_selector_missing: job list --target requires a value that selects") {
		t.Fatalf("only !: %d %q", code, errs)
	}
}
