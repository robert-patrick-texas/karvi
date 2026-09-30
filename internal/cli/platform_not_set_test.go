package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/records"
)

// TestPlatformNotSetDryRun: an inventory row without a platform is not set. run
// --select-platform generic does not select it, a session-init-map rule with
// platform = "generic" does not match it (only the catch-all does), and the
// plan carries the shipped default, cisco_iosxe, as the platform used.
// The row with platform generic is selected
// and matched by both.
func TestPlatformNotSetDryRun(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "inv.csv"), []byte("name,management_address,platform\nblank-1,127.0.0.1,\ngen-1,127.0.0.1,generic\nxe-1,127.0.0.1,cisco_iosxe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(config, []byte("[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+filepath.Join(dir, "inv.csv")+"\"\nrequired = true\n"+
		"[session-init.gen]\ncommands = []\n[session-init.catch]\ncommands = []\n"+
		"[[session-init-map]]\nprofile = \"gen\"\nplatform = \"generic\"\n[[session-init-map]]\nprofile = \"catch\"\nname = \"*\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(inputs ...string) records.PlanReport {
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
		return report
	}
	summary := func(r records.PlanReport) string {
		out := []string{}
		for i, tr := range r.Targets {
			out = append(out, r.Plan.Targets[i].Device.Name+":"+r.Plan.Targets[i].Device.Platform+":"+tr.IntendedTransport.SessionInitProfile)
		}
		return strings.Join(out, " ")
	}
	if got := summary(run("--select-platform", "generic")); got != "gen-1:generic:gen" {
		t.Fatalf("--select-platform generic selected %q, want the generic row only", got)
	}
	if got := summary(run("--all")); got != "blank-1:cisco_iosxe:catch gen-1:generic:gen xe-1:cisco_iosxe:catch" {
		t.Fatalf("--all: %q", got)
	}
	if got := summary(run("--target", "blank-1", "--target", "127.0.0.2")); got != "blank-1:cisco_iosxe:catch 127.0.0.2:cisco_iosxe:catch" {
		t.Fatalf("by name: %q", got)
	}
	// run --platform (here by its shortcut) acts on every device of the set
	// after the set is assembled: --select-platform matched the inventory's
	// own value, the generic row and the direct target both run as NX-OS, and
	// the map's platform rule sees the platform the device runs as.
	if got := summary(run("--select-platform", "generic", "--target", "127.0.0.2", "--pn")); got != "gen-1:cisco_nxos:catch 127.0.0.2:cisco_nxos:catch" {
		t.Fatalf("--platform over the set: %q", got)
	}
}
