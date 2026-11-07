package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestInventoryOption: --inventory[=FILE], --inv, --i, and -i alike. Every
// =FILE in command-line order is the run's whole inventory, the configured
// sources unread, each a source named as typed; a file reached twice (named
// again, or by a glob) is read once, under its first name, with no coalesce
// warning; a bare --inv is targets.inventory-only over the configured
// sources, and beside a file adds nothing. Refused: a plain name the files
// lack (the configured sources unread), a missing file or a glob matching
// none (inventory_missing), an empty value, and a value as a separate word.
// Without --i the created-names refusal says what --i does. login, command,
// and crun read the files as run does.
func TestInventoryOption(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	t.Setenv(loginTranscriptChildEnv, "")
	dir := t.TempDir()
	t.Chdir(dir)
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.csv", "name,management_address,platform\nbox01,127.0.0.1,cisco_ios\nbox02,127.0.0.1,cisco_ios\n")
	write("b.csv", "name,management_address,platform\npod01,127.0.0.1,cisco_ios\n")
	write(filepath.Join("conf", "inv.csv"), "name,management_address,platform\nrouter01,127.0.0.1,cisco_ios\n")
	config := filepath.Join(dir, "conf", "karvi.toml")
	write(config, "[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+filepath.Join(dir, "conf", "inv.csv")+"\"\nrequired = true\n")
	main := func(words ...string) (int, string, string) {
		t.Helper()
		args := append(append([]string{"--config", config}, sets...), words...)
		var stdout, stderr bytes.Buffer
		code := Main(args, strings.NewReader(""), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	run := func(inputs ...string) (int, string, string) {
		t.Helper()
		words := append([]string{"run", "--dry-run", "--no-daemon", "--transport", "system", "--format", "jsonl"}, inputs...)
		return main(append(words, "--cmd", "show clock")...)
	}
	planned := func(stdout string) string {
		t.Helper()
		var report records.PlanReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("report: %v\n%s", err, stdout)
		}
		out := []string{}
		for _, tr := range report.Plan.Targets {
			out = append(out, tr.Device.Name+":"+tr.Device.Source.Name)
		}
		return strings.Join(out, " ")
	}
	for _, tc := range []struct {
		name   string
		inputs []string
		want   string
	}{
		{"two files, -i and --i alike", []string{"-i=a.csv", "--i=b.csv", "--tl", "box0[1-3],pod01"}, "box01:a.csv box02:a.csv pod01:b.csv"},
		{"the files in command-line order", []string{"--inventory=b.csv", "--inv=a.csv", "--all"}, "pod01:b.csv box01:a.csv box02:a.csv"},
		{"a file reached twice is read once", []string{"--inv=a.csv", "--inv=./a.csv", "--inv=*.csv", "--target", "box*"}, "box01:a.csv box02:a.csv"},
		{"a bare --inv reads the configured sources", []string{"--inv", "--tl", "router01"}, "router01:lab"},
		{"a bare --inv beside a file adds nothing", []string{"--inv", "--inv=a.csv", "--target", "box01"}, "box01:a.csv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := run(tc.inputs...)
			if code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			if got := planned(stdout); got != tc.want {
				t.Fatalf("planned %q, want %q", got, tc.want)
			}
			if strings.Contains(stderr, "coalesced") {
				t.Fatalf("a file was read twice: %q", stderr)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		words  []string
		exit   int
		prefix string
	}{
		{"the configured sources unread", []string{"--inv=a.csv", "--tl", "router01"}, exitcode.ExitInventoryError,
			"target_not_in_inventory: client planning: 1 target name is not in the inventory (targets.inventory-only)\n  router01\n"},
		{"a bare --inv refuses an absent name", []string{"--inv", "--tl", "router04"}, exitcode.ExitInventoryError,
			"target_not_in_inventory: client planning: 1 target name is not in the inventory (targets.inventory-only)\n  router04\n"},
		{"a missing file", []string{"--inv=none.csv", "--tl", "box01"}, exitcode.ExitInventoryError,
			"inventory_missing: client planning: inventory source none.csv matched no files: " + filepath.Join(dir, "none.csv") + "\n"},
		{"a glob matching none", []string{"--inv=a.csv", "--inv=*.tsv", "--tl", "box01"}, exitcode.ExitInventoryError,
			"inventory_missing: client planning: inventory source *.tsv matched no files: " + filepath.Join(dir, "*.tsv") + "\n"},
		{"an empty value", []string{"--inv=", "--tl", "box01"}, exitcode.ExitUsageError,
			"cli_option_value_missing: --inventory= names no file; --inventory alone uses the configured inventory\n"},
		{"white space alone", []string{"-i= ", "--tl", "box01"}, exitcode.ExitUsageError,
			"cli_option_value_missing: --inventory= names no file; --inventory alone uses the configured inventory\n"},
		{"a detached value", []string{"--inv", "./a.csv", "--tl", "box01"}, exitcode.ExitUsageError,
			"cli_option_value_detached: --inventory takes its FILE with =: --inventory=./a.csv\n"},
		{"the created-names hint", []string{"--target", "box[1-2001]"}, exitcode.ExitUsageError,
			"target_created_names_exceeded: --target box[1-2001] creates too many names\n  names: 2001\n  limit: 1000 (targets.max-created-names)\n  with --i it selects from the inventory\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := run(tc.words...)
			if code != tc.exit || !strings.HasPrefix(stderr, tc.prefix) {
				t.Fatalf("exit %d, want %d; stderr\n%s\nwant it to begin\n%s", code, tc.exit, stderr, tc.prefix)
			}
		})
	}
	missing := "inventory_missing: "
	for _, tc := range []struct {
		name  string
		words []string
		want  string
	}{
		{"crun", []string{"crun", "--dry-run", "--no-daemon", "--transport", "system", "--format", "jsonl", "--inv=b.csv", "--target", "pod01", "--cmd", "show clock"}, ""},
		{"command", []string{"command", "--inv=none.csv", "--transport", "system", "box01", "show clock"}, missing},
		{"login", []string{"login", "--inv=none.csv", "--transport", "system", "--target", "box01"}, missing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := main(tc.words...)
			if tc.want == "" {
				if code != 0 || planned(stdout) != "pod01:b.csv" {
					t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
				}
				return
			}
			if code != exitcode.ExitInventoryError || !strings.Contains(stderr, tc.want+"inventory source none.csv matched no files") {
				t.Fatalf("exit %d, stderr %q, want %s", code, stderr, tc.want)
			}
		})
	}
}

// TestStreamInventoryLine: an --inv line stays in the draft from one job to
// the next and --reset clears it; a value after a space and an empty value
// are refused and the line dropped.
func TestStreamInventoryLine(t *testing.T) {
	var runs [][]string
	execute := func(_ int, argv []string) int {
		runs = append(runs, append([]string{}, argv...))
		return 0
	}
	in := strings.NewReader(strings.Join([]string{
		"--inv=a.csv",
		"--target box01",
		"show clock",
		"--go",
		"--inv ./b.csv",
		"--inv=",
		"show version",
		"--go",
		"--reset",
		"--target box01",
		"show users",
		"--go",
	}, "\n") + "\n")
	var stderr bytes.Buffer
	streamLoop(context.Background(), streamScanner(in), globalOptions{}, &stderr, execute, restartNone)
	want := [][]string{
		{"run", "--inv=a.csv", "--target", "box01", "--cmd", "show clock"},
		{"run", "--inv=a.csv", "--target", "box01", "--cmd", "show version"},
		{"run", "--target", "box01", "--cmd", "show users"},
	}
	if !reflect.DeepEqual(runs, want) {
		t.Errorf("runs:\n%q\nwant:\n%q", runs, want)
	}
	for _, m := range []string{
		"stream line 5 dropped: cli_option_value_detached: --inventory takes its FILE with =: --inventory=./b.csv",
		"stream line 6 dropped: cli_option_value_missing: --inventory= names no file; --inventory alone uses the configured inventory",
	} {
		if !strings.Contains(stderr.String(), m) {
			t.Errorf("stderr lacks %q:\n%s", m, stderr.String())
		}
	}
}

// TestInventoryOptionSetsTheKey: --inventory in any form, bare or with a
// file, is targets.inventory-only = true in the lock-aware cli layer, its
// source the option's long name, so a site's lock refuses it as it refuses
// --order; without it the layer holds no such key.
func TestInventoryOptionSetsTheKey(t *testing.T) {
	for _, tc := range []struct {
		args []string
		set  bool
	}{
		{[]string{"run", "--inv", "--target", "r1", "--cmd", "c"}, true},
		{[]string{"run", "-i=a.csv", "--target", "r1", "--cmd", "c"}, true},
		{[]string{"login", "--inventory=a.csv", "--target", "r1"}, true},
		{[]string{"command", "--i", "r1", "show clock"}, true},
		{[]string{"run", "--target", "r1", "--cmd", "c"}, false},
	} {
		inv, err := Parse(tc.args)
		if err != nil {
			t.Fatalf("%q: %v", tc.args, err)
		}
		v, ok := inv.flags()["targets.inventory-only"]
		if ok != tc.set || (ok && (v.Value != true || v.Option != "--inventory")) {
			t.Errorf("%q: %+v %v, want set %v from --inventory", tc.args, v, ok, tc.set)
		}
	}
}
