package configload

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// TestAbsolutePlaces: the load makes a path-valued key absolute, `~` the
// load's home and a relative path from the working directory, a list's
// every entry and a dynamic table's file fields included; an empty value
// and the key's own words stay as written, a word of another key does not;
// the source is the layer that set the value; a key that is no path, an
// executable specification among them, is kept as written.
func TestAbsolutePlaces(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	t.Chdir(wd)
	snap, err := Load(Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{
		`crun.directory="rel/c"`,
		`output.root="~/jobs"`,
		`transcript.root="/abs/../t"`,
		`sharedroot="none"`,
		`basedir="none"`,
		`audit.file="auto"`,
		`ssh.identities=["~/.ssh/id_ops", "/etc/karvi/keys/ops"]`,
		`credential-backend.mine.type="csv"`,
		`credential-backend.mine.scope="user"`,
		`credential-backend.mine.path="creds.csv"`,
		`credential-backend.mine.ca-file="~/ca.pem"`,
		`inventory-source.0.type="csv"`,
		`inventory-source.0.name="i"`,
		`inventory-source.0.path="inv.csv"`,
		`ssh.transports.system="bin/ssh-wrapper"`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"crun.directory":                  filepath.Join(wd, "rel/c"),
		"output.root":                     filepath.Join(home, "jobs"),
		"transcript.root":                 "/t",
		"sharedroot":                      "none",
		"basedir":                         filepath.Join(wd, "none"),
		"audit.file":                      filepath.Join(wd, "auto"),
		"tempdir":                         "auto",
		"ssh.identities":                  []any{filepath.Join(home, ".ssh/id_ops"), "/etc/karvi/keys/ops"},
		"credential-backend.mine.path":    filepath.Join(wd, "creds.csv"),
		"credential-backend.mine.ca-file": filepath.Join(home, "ca.pem"),
		"credential-backend.mine.type":    "csv",
		"inventory-source.0.path":         filepath.Join(wd, "inv.csv"),
		"inventory-source.0.name":         "i",
		"ssh.transports.system":           "bin/ssh-wrapper",
	} {
		v := snap.Values[key]
		got := v.Data
		if l, ok := got.([]string); ok {
			got = toAny(l)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %#v, want %#v", key, v.Data, want)
		}
	}
	if src := snap.Values["crun.directory"].Source; src.Layer != "set" || src.Path != "--set[1]" {
		t.Errorf("crun.directory's source: %+v", src)
	}

	// An empty audit.file is unset, kept empty.
	snap, err = Load(Options{HomeDir: home, SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.String("audit.file"); got != "" {
		t.Errorf("audit.file unset: %q", got)
	}

	// ~user fails the load under its code, naming the key and its source.
	for key, sets := range map[string][]string{
		"scoreboards":                     {`scoreboards="~other/sb"`},
		"credential-backend.mine.ca-file": {`credential-backend.mine.type="csv"`, `credential-backend.mine.scope="user"`, `credential-backend.mine.path="/c.csv"`, `credential-backend.mine.ca-file="~other/ca.pem"`},
	} {
		_, err := Load(Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: sets})
		var ce *Error
		if !errors.As(err, &ce) || ce.Code != "path_other_user_home_unsupported" || ce.Key != key || ce.Source.Layer != "set" {
			t.Errorf("%s: %v", key, err)
		}
	}
}

// TestAbsolutePlacesWorkingDirectoryGone: a relative path from a working
// directory that cannot be read fails the load under its own code, naming
// the key and its source; an absolute path and a word need no directory.
func TestAbsolutePlacesWorkingDirectoryGone(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{`crun.directory="rel"`}})
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != "config_working_directory_unavailable" || ce.Key != "crun.directory" || ce.Source.Path != "--set[1]" {
		t.Errorf("relative: %v", err)
	}
	if _, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{`crun.directory="/abs"`}}); err != nil {
		t.Errorf("absolute: %v", err)
	}
}

// TestAbsolutePathIsResolvePath: the loader's rule and the readers'
// (osutil.ResolvePath) give one answer, path or code, for every form.
func TestAbsolutePathIsResolvePath(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	for _, h := range []string{home, ""} {
		for _, raw := range []string{"~", "~/", "~/a/b", "~other/x", "/abs/../c", "rel/d", "./e", "f"} {
			got, gerr := absolutePath(raw, h)
			want, werr := osutil.ResolvePath(raw, h)
			if got != want || errorcodes.Of(gerr) != errorcodes.Of(werr) || (gerr == nil) != (werr == nil) {
				t.Errorf("%q home %q: %q %v, ResolvePath %q %v", raw, h, got, gerr, want, werr)
			}
		}
	}
}

func toAny(l []string) []any {
	out := make([]any, len(l))
	for i, s := range l {
		out[i] = s
	}
	return out
}
