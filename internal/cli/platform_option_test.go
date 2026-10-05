package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// TestPlatformOptionRefusedBeforeInventory covers the three sites:
// command, login, and the login --record wrapper refuse a
// --platform value that is not a known platform with platform_option_unknown
// (exit 4) after the configuration and before the inventory is read. A
// required inventory source that matches no file is the control: without
// --platform the same invocation is inventory_missing (exit 5), with it the
// option is refused first. No terminal is needed and the wrapper
// creates no transcript file.
func TestPlatformOptionRefusedBeforeInventory(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(config, []byte("[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+filepath.Join(dir, "missing.csv")+"\"\nrequired = true\n[platform.c9300]\ndriver = \"cisco_iosxe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordDir := filepath.Join(dir, "transcripts")
	run := func(args ...string) (int, string) {
		var stdout, stderr bytes.Buffer
		got := Main(append([]string{"--config", config}, args...), strings.NewReader(""), &stdout, &stderr)
		return got, stderr.String()
	}
	if got, stderr := run("command", "r1", "show", "clock"); got != exitcode.ExitInventoryError || !strings.HasPrefix(stderr, "inventory_missing: ") {
		t.Fatalf("control: exit=%d stderr=%q", got, stderr)
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"command_unknown", []string{"command", "--platform", "cisco_iosx", "r1", "show", "clock"}},
		{"command_glob", []string{"command", "--platform", "*", "r1", "show", "clock"}},
		{"command_not", []string{"command", "--platform", "!cisco_iosxe", "r1", "show", "clock"}},
		{"login_unknown", []string{"login", "--platform", "cisco_iosx", "r1"}},
		// run: --platform is the same option under the same check, on both
		// of run's paths (the local one and the client's draft).
		{"run_unknown", []string{"run", "--no-daemon", "--target", "r1", "--platform", "cisco_iosx", "--cmd", "show clock"}},
		{"run_glob", []string{"run", "--no-daemon", "--target", "r1", "--platform", "cisco*", "--cmd", "show clock"}},
		{"run_dry_run_unknown", []string{"run", "--dry-run", "--target", "r1", "--platform", "cisco_iosx", "--cmd", "show clock"}},
		{"login_record", []string{"login", "--record=" + recordDir, "--platform", "cisco_iosx", "r1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, stderr := run(tc.args...)
			if got != exitcode.ExitUsageError || !strings.HasPrefix(stderr, "platform_option_unknown: ") {
				t.Fatalf("exit=%d stderr=%q", got, stderr)
			}
			if !strings.Contains(stderr, "known platforms: generic, cisco_iosxe, cisco_iosxr, cisco_nxos, juniper_junos, arista_eos, linux, linux_shell, c9300") {
				t.Fatalf("known platforms not named: %q", stderr)
			}
		})
	}
	if _, err := os.Stat(recordDir); !os.IsNotExist(err) {
		t.Fatalf("the wrapper created the record folder: %v", err)
	}
	// run: the --select-platform selector must reach a known platform,
	// checked in the assembly before the inventory is read;
	// a glob over the known set passes and then meets the inventory fault.
	for _, tc := range []struct {
		name string
		args []string
		code string
		exit int
	}{
		{"run_literal_unknown", []string{"run", "--no-daemon", "--select-platform", "cisco_iosx", "--cmd", "show clock"}, "platform_selector_unknown", exitcode.ExitUsageError},
		{"run_glob_unknown", []string{"run", "--no-daemon", "--select-platform", "nexus*", "--cmd", "show clock"}, "platform_selector_unknown", exitcode.ExitUsageError},
		{"run_not_unknown", []string{"run", "--no-daemon", "--all", "--select-platform", "!nexus*", "--cmd", "show clock"}, "platform_selector_unknown", exitcode.ExitUsageError},
		{"run_dry_run_unknown", []string{"run", "--no-daemon", "--dry-run", "--select-platform", "cisco_iosx", "--cmd", "show clock"}, "platform_selector_unknown", exitcode.ExitUsageError},
		{"run_glob_known", []string{"run", "--no-daemon", "--select-platform", "cisco*", "--cmd", "show clock"}, "inventory_missing", exitcode.ExitInventoryError},
		{"run_alias_known", []string{"run", "--no-daemon", "--select-platform", "C9300", "--cmd", "show clock"}, "inventory_missing", exitcode.ExitInventoryError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, stderr := run(tc.args...)
			if got != tc.exit || !strings.HasPrefix(stderr, tc.code+": ") {
				t.Fatalf("exit=%d stderr=%q, want %d %s", got, stderr, tc.exit, tc.code)
			}
		})
	}
}
