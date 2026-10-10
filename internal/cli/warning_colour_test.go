package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestWarningLineColour: a warning line takes the warning role's colour, the
// whole line, when colour is on by the display's rule, and is plain when it
// is off, the words unchanged in both: the load's warnings (config show) and
// a target file yielding no names (a dry run), under display.color always,
// never, and auto off a terminal.
func TestWarningLineColour(t *testing.T) {
	cfg, sets := warnConfig(t)
	empty := filepath.Join(filepath.Dir(cfg), "empty.txt")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var loads []string
	for _, line := range loadWarningLines(cfg) {
		loads = append(loads, strings.TrimSuffix(line, "\n"))
	}
	emptyLine := "warning: target file " + empty + " yields no targets; continuing with the other inputs"
	sgr := regexp.MustCompile("\x1b\\[[0-9;]*m")
	for _, mode := range []string{"always", "never", "auto"} {
		for _, tc := range []struct {
			args []string
			want []string
		}{
			{[]string{"config", "show", "basedir"}, loads},
			{[]string{"--set", "targets.empty-source=warn", "run", "--dry-run", "--no-daemon", "--tf", empty, "--target", "127.0.0.1", "--transport", "system", "show", "clock"}, append(append([]string{}, loads...), emptyLine)},
		} {
			args := append(append(append([]string{}, sets...), "--set", "display.color="+mode), tc.args...)
			var stdout, stderr bytes.Buffer
			if code := Main(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
				t.Fatalf("%s %v: exit %d stderr=%q", mode, tc.args, code, stderr.String())
			}
			lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
			for _, want := range tc.want {
				found := false
				for _, line := range lines {
					if sgr.ReplaceAllString(line, "") != want {
						continue
					}
					found = true
					coloured := strings.HasPrefix(line, "\x1b[") && strings.HasSuffix(line, "\x1b[0m")
					if coloured != (mode == "always") {
						t.Errorf("%s %v: %q coloured=%t", mode, tc.args, line, coloured)
					}
				}
				if !found {
					t.Errorf("%s %v: no line %q in %q", mode, tc.args, want, stderr.String())
				}
			}
		}
	}
}
