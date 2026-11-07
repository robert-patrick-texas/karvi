package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/testdir"
)

// TestCreatedNamesRefusedBeforeAnyRead: targets.max-created-names bounds the
// names one target value's ranges create. A command-line value over it is
// refused, usage, after the configuration is read and before any target
// file or the inventory is read (a missing --tf earlier on the line and a
// missing required inventory source are never reached), naming where the
// value was given and stacking the count over the limit; a target file's
// or standard input's line is refused as it is read. A value at the limit,
// a "!" value, and an inventory selector pass the check and reach the
// inventory, whose absence then stops the run.
func TestCreatedNamesRefusedBeforeAnyRead(t *testing.T) {
	dir := testdir.Short(t)
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "k.toml")
	inv := filepath.Join(dir, "absent.csv")
	if err := os.WriteFile(cfg, []byte("[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+inv+"\"\nrequired = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "targets.txt")
	if err := os.WriteFile(file, []byte("edge1\nbox[1-6]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	t.Setenv(loginTranscriptChildEnv, "")
	sets := append([]string{"--config", cfg}, dryRunSets(t, base)...)
	missing := filepath.Join(dir, "missing.txt")
	over := func(where string) string {
		return "target_created_names_exceeded: " + where + " box[1-6] creates too many names\n  names: 6\n  limit: 5 (targets.max-created-names)\n"
	}
	for _, tc := range []struct {
		name  string
		limit string
		args  []string
		stdin string
		code  int
		want  string
	}{
		{"--tl after a missing --tf", "5", []string{"run", "--dry-run", "--no-daemon", "--tf", missing, "--tl", "edge1,box[1-6]", "show", "clock"}, "", exitcode.ExitUsageError, over("--tl")},
		{"--target", "5", []string{"run", "--dry-run", "--no-daemon", "--target", "box[1-6]", "show", "clock"}, "", exitcode.ExitUsageError, over("--target")},
		{"brackets multiply", "5", []string{"run", "--dry-run", "--no-daemon", "--target", "box[1-2][1-3]", "show", "clock"}, "", exitcode.ExitUsageError,
			"target_created_names_exceeded: --target box[1-2][1-3] creates too many names\n  names: 6\n  limit: 5 (targets.max-created-names)\n"},
		{"the positional device", "5", []string{"command", "box[1-6]", "show", "clock"}, "", exitcode.ExitUsageError, over("DEVICE")},
		{"a target file line", "5", []string{"run", "--dry-run", "--no-daemon", "--tf", file, "show", "clock"}, "", exitcode.ExitUsageError, over("target file " + file + " entry")},
		{"a standard input line", "5", []string{"run", "--dry-run", "--no-daemon", "--tf", "-", "show", "clock"}, "edge1\nbox[1-6]\n", exitcode.ExitUsageError, over("target file standard input entry")},
		{"at the limit", "6", []string{"run", "--dry-run", "--no-daemon", "--tl", "box[1-6]", "show", "clock"}, "", exitcode.ExitInventoryError, "inventory_"},
		{"a ! value and a selector", "1", []string{"run", "--dry-run", "--no-daemon", "--site", "pod[1-99]", "--target", "!box[1-99]", "show", "clock"}, "", exitcode.ExitInventoryError, "inventory_"},
		{"above the key's maximum", "100001", []string{"run", "--dry-run", "--no-daemon", "--target", "box1", "show", "clock"}, "", exitcode.ExitConfigValidationError, "targets.max-created-names"},
		{"below its minimum", "0", []string{"run", "--dry-run", "--no-daemon", "--target", "box1", "show", "clock"}, "", exitcode.ExitConfigValidationError, "targets.max-created-names"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append(append(append([]string{}, sets...), "--set", "targets.max-created-names="+tc.limit), tc.args...)
			var stdout, stderr bytes.Buffer
			code := Main(args, strings.NewReader(tc.stdin), &stdout, &stderr)
			if code != tc.code || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("exit %d, want %d; stderr %q, want it to hold %q", code, tc.code, stderr.String(), tc.want)
			}
		})
	}
}
