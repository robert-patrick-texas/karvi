package prune

import (
	"bytes"
	"flag"
	"io"
	"reflect"
	"strings"
	"testing"
)

// TestDefineFlags pins the flag set to Usage and the completion rows: every
// flag is named on the usage line, every flagValues row names a flag, and
// the flag set's defaults are the documented ones (docs/PRUNE.md).
func TestDefineFlags(t *testing.T) {
	fs := flag.NewFlagSet("karvi-prune", flag.ContinueOnError)
	f := DefineFlags(fs)
	var names []string
	fs.VisitAll(func(fl *flag.Flag) {
		names = append(names, fl.Name)
		if !strings.Contains(Usage, "--"+fl.Name) {
			t.Errorf("--%s is not on the usage line", fl.Name)
		}
	})
	if len(names) != 8 {
		t.Errorf("flags: %q", names)
	}
	for name := range flagValues {
		if fs.Lookup(name) == nil {
			t.Errorf("flagValues names %q, no such flag", name)
		}
	}
	if *f.Basedir != "auto" || *f.Sharedroot != "auto" || *f.Scoreboards != "/dev/shm/karvi/scoreboards" || *f.Days != 31 || *f.MinFree != 5 || *f.DryRun || *f.Verbose || *f.Format != FormatText {
		t.Error("the defaults")
	}
}

// TestComplete covers the helper's candidates: the flags at an empty word
// or a dash, a flag's words after it, the path directive once no word
// matches, an inline value with the flag spelled before it, a number's
// silence, and nothing after -- or a positional.
func TestComplete(t *testing.T) {
	all := []string{"--basedir", "--days", "--dry-run", "--format", "--minfree", "--scoreboards", "--sharedroot", "--verbose"}
	cases := []struct {
		line    []string
		current string
		want    []string
		files   bool
	}{
		{nil, "", all, false},
		{nil, "-", all, false},
		{nil, "--d", []string{"--days", "--dry-run"}, false},
		{nil, "x", nil, false},
		{[]string{"--dry-run"}, "", all, false},
		{[]string{"--dry-run"}, "--v", []string{"--verbose"}, false},
		{[]string{"--days", "7"}, "--f", []string{"--format"}, false},
		{[]string{"--days=7", "--verbose"}, "", all, false},
		{[]string{"--format"}, "", []string{"jsonl", "text"}, false},
		{[]string{"-format"}, "j", []string{"jsonl"}, false},
		{[]string{"--sharedroot"}, "", []string{"auto", "none"}, true},
		{[]string{"--basedir"}, "/opt", nil, true},
		{[]string{"--scoreboards"}, "", nil, true},
		{[]string{"--days"}, "", nil, false},
		{[]string{"--minfree"}, "1", nil, false},
		{nil, "--format=", []string{"--format=jsonl", "--format=text"}, false},
		{nil, "--format=js", []string{"--format=jsonl"}, false},
		{nil, "--basedir=/o", nil, true},
		{nil, "--dry-run=", nil, false},
		{nil, "--nothing=x", nil, false},
		{[]string{"--nothing"}, "--v", []string{"--verbose"}, false},
		{[]string{"--"}, "", nil, false},
		{[]string{"--dry-run", "extra"}, "", nil, false},
		{[]string{"--days", "7", "--"}, "--v", nil, false},
	}
	for _, c := range cases {
		got, files := complete(c.line, c.current)
		if !reflect.DeepEqual(got, c.want) || files != c.files {
			t.Errorf("%q %q: got %q %v, want %q %v", c.line, c.current, got, files, c.want, c.files)
		}
	}
}

// TestCompleteWord drives the hidden word as the bash function calls it:
// the candidates one per line with the directive last, and silence with
// exit 0 on arguments that are not a line.
func TestCompleteWord(t *testing.T) {
	run := func(args ...string) (int, string) {
		var out bytes.Buffer
		return Complete(args, &out), out.String()
	}
	if code, out := run("2", "karvi-prune", "--sharedroot", ""); code != 0 || out != "auto\nnone\n:files\n" {
		t.Errorf("--sharedroot: %d %q", code, out)
	}
	if code, out := run("3", "karvi-prune", "--sharedroot", "none"); code != 0 || out != strings.Join(append([]string{}, "--basedir\n--days\n--dry-run\n--format\n--minfree\n--scoreboards\n--sharedroot\n--verbose\n"), "") {
		t.Errorf("after the value: %d %q", code, out)
	}
	for _, bad := range [][]string{{}, {"x", "karvi-prune"}, {"0", "karvi-prune"}, {"2", "karvi-prune"}} {
		if code, out := run(bad...); code != 0 || out != "" {
			t.Errorf("%q: %d %q", bad, code, out)
		}
	}
	if code := Complete([]string{"1", "karvi-prune"}, failingWriter{}); code != 0 {
		t.Errorf("a writer that fails: %d", code)
	}
}

// failingWriter stands in for a closed standard output.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
