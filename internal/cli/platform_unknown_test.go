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

// TestPlatformUnknownAndNotSet: an inventory row naming
// an unknown platform refuses command, run, and login at planning with
// platform_unknown (exit 5) before any connection or credential prompt;
// under on-unknown = "warn" the device proceeds as the fallback with a
// warning line; a blank row proceeds as the default with its warning line;
// rows outside the target set are not examined; the warning lines group
// per distinct value per source with the count and the first three devices.
func TestPlatformUnknownAndNotSet(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	// The login control below expects the askpass helper to be missing
	// (exit 8); a karvi-askpass on the developer's PATH would let the
	// login reach ssh instead, so the test runs with an empty PATH.
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	inventory := "name,management_address,platform\nsw-ios,127.0.0.1,cisco_iosxe\nsw-typo,127.0.0.1,cisco_iosx\nsw-empty,127.0.0.1,\n" +
		"core1,127.0.0.1,cisco_iosx\ncore2,127.0.0.1,cisco_iosx\ncore3,127.0.0.1,cisco_iosx\nedge1,127.0.0.1,junos\nlab2,127.0.0.1,\n"
	if err := os.WriteFile(filepath.Join(dir, "inv.csv"), []byte(inventory), 0o600); err != nil {
		t.Fatal(err)
	}
	configFor := func(t *testing.T, extra string) string {
		t.Helper()
		config := filepath.Join(t.TempDir(), "karvi.toml")
		body := "[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \"" + filepath.Join(dir, "inv.csv") + "\"\nrequired = true\n" + extra
		if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return config
	}
	run := func(config string, args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		all := append([]string{"--config", config}, sets...)
		all = append(all, args...)
		got := Main(all, strings.NewReader(""), &stdout, &stderr)
		return got, stdout.String(), stderr.String()
	}
	planned := func(t *testing.T, stdout string) string {
		t.Helper()
		var report records.PlanReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("report: %v\n%s", err, stdout)
		}
		out := []string{}
		for _, tr := range report.Plan.Targets {
			out = append(out, tr.Device.Name+":"+tr.Device.Platform)
		}
		return strings.Join(out, " ")
	}
	warnings := func(stderr string) []string {
		out := []string{}
		for _, line := range strings.Split(stderr, "\n") {
			if strings.HasPrefix(line, "warning: platform_") {
				out = append(out, line)
			}
		}
		return out
	}
	plain := configFor(t, "")
	dryRun := []string{"run", "--dry-run", "--no-daemon", "--transport", "system", "--format", "jsonl"}

	t.Run("refused under fail", func(t *testing.T) {
		// command, run, and login refuse before any connection; the
		// fallback is not read under fail; every unknown value is named
		// once.
		withFallback := configFor(t, "[platform-resolution]\nunknown-fallback = \"cisco_iosxe\"\n")
		for _, tc := range []struct {
			name   string
			config string
			args   []string
			text   string
		}{
			{"command", plain, []string{"command", "--transport", "system", "sw-typo", "show", "clock"}, `inventory source lab line 3: platform "cisco_iosx" is not a known platform; 1 device: sw-typo (known: generic`},
			{"command with a fallback under fail", withFallback, []string{"command", "--transport", "system", "sw-typo", "show", "clock"}, `1 device: sw-typo`},
			{"run --all", plain, append(append([]string{}, dryRun...), "--all", "--cmd", "show clock"), `inventory source lab line 3: platform "cisco_iosx" is not a known platform; 4 devices: sw-typo, core1, core2, …; inventory source lab line 8: platform "junos" is not a known platform; 1 device: edge1 (known: generic, cisco_iosxe, cisco_iosxr, cisco_nxos, juniper_junos, arista_eos, linux, linux_shell)`},
			{"run --select-platform cisco*", plain, append(append([]string{}, dryRun...), "--select-platform", "cisco*", "--cmd", "show clock"), `4 devices: sw-typo, core1, core2, …`},
			{"login", plain, []string{"login", "--transport", "system", "sw-typo"}, `1 device: sw-typo`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got, _, stderr := run(tc.config, tc.args...)
				if got != exitcode.ExitInventoryError || !strings.HasPrefix(stderr, "platform_unknown: ") || !strings.Contains(stderr, tc.text) {
					t.Fatalf("exit=%d stderr=%q", got, stderr)
				}
				if len(warnings(stderr)) != 0 {
					t.Fatalf("a refusal printed warnings: %q", stderr)
				}
			})
		}
		// The control for login: a known row passes the platform check and
		// meets the next dependency, the askpass helper (exit 8), so the
		// refusal above came first.
		if got, _, stderr := run(plain, "login", "--transport", "system", "sw-ios"); got != exitcode.ExitDependencyError || !strings.HasPrefix(stderr, "dependency_askpass_unavailable: ") {
			t.Fatalf("login control: exit=%d stderr=%q", got, stderr)
		}
		// A row outside the target set is not examined.
		if got, stdout, stderr := run(plain, append(append([]string{}, dryRun...), "--target", "sw-ios", "--cmd", "show clock")...); got != 0 || planned(t, stdout) != "sw-ios:cisco_iosxe" || len(warnings(stderr)) != 0 {
			t.Fatalf("outside the set: exit=%d stderr=%q", got, stderr)
		}
	})

	t.Run("warn and fall back", func(t *testing.T) {
		// The device proceeds as the fallback with one warning
		// line; the plan carries the platform used.
		warn := configFor(t, "[platform-resolution]\non-unknown = \"warn\"\n")
		got, stdout, stderr := run(warn, append(append([]string{}, dryRun...), "--target", "sw-typo", "--cmd", "show clock")...)
		if got != 0 || planned(t, stdout) != "sw-typo:generic" {
			t.Fatalf("warn: exit=%d planned=%q stderr=%q", got, stdout, stderr)
		}
		if w := warnings(stderr); len(w) != 1 || w[0] != `warning: platform_unknown_fallback: inventory source lab line 3: platform "cisco_iosx" is not a known platform; 1 device proceeds as generic (platform-resolution.unknown-fallback): sw-typo` {
			t.Fatalf("warnings %q", w)
		}
		fallback := configFor(t, "[platform-resolution]\non-unknown = \"warn\"\nunknown-fallback = \"cisco_iosxe\"\n")
		got, stdout, stderr = run(fallback, append(append([]string{}, dryRun...), "--target", "sw-typo", "--cmd", "show clock")...)
		if got != 0 || planned(t, stdout) != "sw-typo:cisco_iosxe" || len(warnings(stderr)) != 1 || !strings.Contains(stderr, "proceeds as cisco_iosxe") {
			t.Fatalf("fallback: exit=%d planned=%q stderr=%q", got, stdout, stderr)
		}
	})

	t.Run("not set", func(t *testing.T) {
		// A blank row proceeds as the default (cisco_iosxe as shipped;
		// generic when the site clears the key) with its warning line; a
		// direct target takes no warning.
		got, stdout, stderr := run(plain, append(append([]string{}, dryRun...), "--target", "sw-empty", "--target", "127.0.0.2", "--cmd", "show clock")...)
		if got != 0 || planned(t, stdout) != "sw-empty:cisco_iosxe 127.0.0.2:cisco_iosxe" {
			t.Fatalf("blank: exit=%d planned=%q stderr=%q", got, stdout, stderr)
		}
		if w := warnings(stderr); len(w) != 1 || w[0] != `warning: platform_not_set: inventory source lab: 1 device has no platform; it proceeds as cisco_iosxe (platform-resolution.default): sw-empty` {
			t.Fatalf("warnings %q", w)
		}
		cleared := configFor(t, "[platform-resolution]\ndefault = \"\"\n")
		got, stdout, stderr = run(cleared, append(append([]string{}, dryRun...), "--target", "sw-empty", "--target", "127.0.0.2", "--cmd", "show clock")...)
		if got != 0 || planned(t, stdout) != "sw-empty:generic 127.0.0.2:generic" {
			t.Fatalf("cleared: exit=%d planned=%q stderr=%q", got, stdout, stderr)
		}
		if w := warnings(stderr); len(w) != 1 || w[0] != `warning: platform_not_set: inventory source lab: 1 device has no platform; it proceeds as generic (platform-resolution.default is empty): sw-empty` {
			t.Fatalf("cleared warnings %q", w)
		}
		withDefault := configFor(t, "[platform-resolution]\ndefault = \"cisco_iosxe\"\n")
		got, stdout, stderr = run(withDefault, append(append([]string{}, dryRun...), "--target", "sw-empty", "--target", "127.0.0.2", "--cmd", "show clock")...)
		if got != 0 || planned(t, stdout) != "sw-empty:cisco_iosxe 127.0.0.2:cisco_iosxe" {
			t.Fatalf("default: exit=%d planned=%q stderr=%q", got, stdout, stderr)
		}
		if w := warnings(stderr); len(w) != 1 || !strings.HasSuffix(w[0], "it proceeds as cisco_iosxe (platform-resolution.default): sw-empty") {
			t.Fatalf("warnings %q", w)
		}
	})

	t.Run("grouped warning lines", func(t *testing.T) {
		// --all under warn with a default: one line per distinct
		// unknown value, one for the blank rows, in inventory order.
		config := configFor(t, "[platform-resolution]\non-unknown = \"warn\"\ndefault = \"cisco_iosxe\"\n")
		got, stdout, stderr := run(config, append(append([]string{}, dryRun...), "--all", "--cmd", "show clock")...)
		if got != 0 {
			t.Fatalf("exit=%d stderr=%q", got, stderr)
		}
		if p := planned(t, stdout); p != "sw-ios:cisco_iosxe sw-typo:generic sw-empty:cisco_iosxe core1:generic core2:generic core3:generic edge1:generic lab2:cisco_iosxe" {
			t.Fatalf("planned %q", p)
		}
		want := []string{
			`warning: platform_unknown_fallback: inventory source lab line 3: platform "cisco_iosx" is not a known platform; 4 devices proceed as generic (platform-resolution.unknown-fallback): sw-typo, core1, core2, …`,
			`warning: platform_unknown_fallback: inventory source lab line 8: platform "junos" is not a known platform; 1 device proceeds as generic (platform-resolution.unknown-fallback): edge1`,
			`warning: platform_not_set: inventory source lab: 2 devices have no platform; they proceed as cisco_iosxe (platform-resolution.default): sw-empty, lab2`,
		}
		if w := warnings(stderr); strings.Join(w, "\n") != strings.Join(want, "\n") {
			t.Fatalf("warnings\n got %s\nwant %s", strings.Join(w, "\n"), strings.Join(want, "\n"))
		}
	})
}
