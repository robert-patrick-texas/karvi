package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestInventoryOnlyRunsInventoryDevices: under targets.inventory-only only
// the inventory's devices run. A range selects the inventory's devices among
// the names it spells, at any size, targets.max-created-names not applied
// (box[1-2001] over a limit of 1000 reaches the inventory, which holds none
// of them); a plain name the inventory lacks refuses the job before planning,
// target_not_in_inventory, naming every such name once, as first typed, from
// every input, a target file's lines among them; it comes before
// inventory_empty_selection. A "!" name and --exclude only remove and are not
// checked.
func TestInventoryOnlyRunsInventoryDevices(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	t.Setenv(loginTranscriptChildEnv, "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "inv.csv"), []byte("name,management_address,platform,site\nrouter01,127.0.0.1,cisco_ios,nyc\nrouter02,127.0.0.1,cisco_ios,nyc\nrouter03,127.0.0.1,cisco_ios,nyc\nrouter05,127.0.0.1,cisco_ios,iad\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(config, []byte("[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+filepath.Join(dir, "inv.csv")+"\"\nrequired = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "targets.txt")
	if err := os.WriteFile(file, []byte("router01\nbox[1-2001]\nrouter09\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(inputs ...string) (int, string, string) {
		t.Helper()
		args := append([]string{"--config", config}, sets...)
		args = append(args, "--set", "targets.inventory-only=true", "run", "--dry-run", "--no-daemon", "--transport", "system", "--format", "jsonl")
		args = append(args, inputs...)
		args = append(args, "--cmd", "show clock")
		var stdout, stderr bytes.Buffer
		code := Main(args, strings.NewReader(""), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
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
		{"a range selects the inventory's devices", []string{"--tl", "router0[1-5]"}, "router01:lab router02:lab router03:lab router05:lab"},
		{"a range of any size", []string{"--target", "router[01-99999]"}, "router01:lab router02:lab router03:lab router05:lab"},
		{"a ! name is not checked", []string{"--tl", "router01", "--target", "!router04"}, "router01:lab"},
		{"--exclude is not checked", []string{"--tl", "router01", "--exclude", "router04"}, "router01:lab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := run(tc.inputs...)
			if code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			if got := planned(stdout); got != tc.want {
				t.Fatalf("planned %q, want %q", got, tc.want)
			}
		})
	}
	const absent = "target_not_in_inventory: client planning: "
	for _, tc := range []struct {
		name   string
		inputs []string
		want   string
	}{
		{"two absent names", []string{"--tl", "router01,router04,router07"},
			absent + "2 target names are not in the inventory (targets.inventory-only)\n  router04\n  router07\n"},
		{"one name, as first typed", []string{"--target", "Router04", "--tl", "router04,router01"},
			absent + "1 target name is not in the inventory (targets.inventory-only)\n  Router04\n"},
		{"after ten, the count left", []string{"--tl", "b1,b2,b3,b4,b5,b6,b7,b8,b9,b10,b11,b12"},
			absent + "12 target names are not in the inventory (targets.inventory-only)\n  b1\n  b2\n  b3\n  b4\n  b5\n  b6\n  b7\n  b8\n  b9\n  b10\n  and 2 more\n"},
		{"a target file's line, its range over the limit", []string{"--tf", file},
			absent + "1 target name is not in the inventory (targets.inventory-only)\n  router09\n"},
		{"before an empty selection", []string{"--tl", "router04", "--site", "nyx"},
			absent + "1 target name is not in the inventory (targets.inventory-only)\n  router04\n"},
		{"a range matching none, over the limit", []string{"--target", "box[1-2001]"},
			"inventory_empty_selection: client planning: the target selectors matched no devices\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := run(tc.inputs...)
			if code != exitcode.ExitInventoryError || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit %d, want %d; stderr %q, want it to hold %q", code, exitcode.ExitInventoryError, stderr, tc.want)
			}
		})
	}
}
