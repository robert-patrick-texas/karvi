package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run1 runs the reader for args, standard output and standard error into
// files, and returns the exit and both texts.
func run1(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	out, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	errf, err := os.Create(filepath.Join(dir, "err"))
	if err != nil {
		t.Fatal(err)
	}
	code := run(args, out, errf)
	out.Close()
	errf.Close()
	o, _ := os.ReadFile(out.Name())
	e, _ := os.ReadFile(errf.Name())
	return code, string(o), string(e)
}

// TestUsage: -h and --help to standard output, exit 0; the reader's own
// refusals, an unknown option, a positional argument, and a count that is
// not a number, each its line then the help on standard error, exit 2,
// before journalctl runs; the hidden word's candidates.
func TestUsage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, h := range []string{"-h", "--help"} {
		if code, out, errs := run1(t, h); code != 0 || !strings.HasPrefix(out, "karvi-audit - show karvi's audit trail") || errs != "" {
			t.Errorf("%s: %d %q %q", h, code, out, errs)
		}
	}
	for args, msg := range map[string]string{
		"--bogus":   "karvi-audit: flag provided but not defined: -bogus\n",
		"extra":     "karvi-audit: no positional argument is taken\n",
		"-n x":      "karvi-audit: invalid value \"x\" for flag -n: parse error\n",
		"--lines=x": "karvi-audit: invalid value \"x\" for flag -lines: parse error\n",
	} {
		code, out, errs := run1(t, strings.Fields(args)...)
		if code != 2 || out != "" || !strings.HasPrefix(errs, msg+"karvi-audit - show") {
			t.Errorf("%s: %d %q %q", args, code, out, errs)
		}
	}
	if code, out, _ := run1(t, "__complete", "1", "karvi-audit", "--fo"); code != 0 || out != "--follow\n" {
		t.Errorf("completion: %d %q", code, out)
	}
}
