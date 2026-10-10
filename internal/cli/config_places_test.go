package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/testdir"
)

// TestPlaceResolver: the view's resolver names each place key's place by
// the activity's own rule, creating nothing: the scratch root's folder
// passed by with its reason when closed, the private root's folders under
// an absent basedir, a file key only when set, a shared credential file as
// written, a relative path made absolute; any other key has no line.
func TestPlaceResolver(t *testing.T) {
	dir := testdir.Short(t)
	base := filepath.Join(dir, "base")
	op, err := osutil.CurrentOperator()
	if err != nil {
		t.Fatal(err)
	}
	load := func(sets ...string) configload.Snapshot {
		t.Helper()
		snap, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, HomeDir: op.Home, Sets: append([]string{`basedir="` + base + `"`, `sharedroot="none"`}, sets...)})
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	lines := func(snap configload.Snapshot, keys ...string) map[string][]string {
		t.Helper()
		got := map[string][]string{}
		key := ""
		for _, l := range strings.Split(snap.Explain(keys, placeResolver(snap)), "\n") {
			switch {
			case strings.HasPrefix(l, "key:"):
				key = strings.TrimSpace(strings.TrimPrefix(l, "key:"))
			case strings.HasPrefix(l, "resolved:"), strings.HasPrefix(l, "passed:"):
				got[key] = append(got[key], l)
			}
		}
		return got
	}

	got := lines(load(), "basedir", "output.root", "tempdir", "scoreboards", "sessions.shared-capacity-root", "daemon.sockets", "audit.file", "timezone", "sharedroot")
	want := map[string][]string{
		"basedir":                       {"resolved:   " + base},
		"output.root":                   {"resolved:   " + filepath.Join(base, "jobs")},
		"tempdir":                       {"resolved:   " + filepath.Join(osutil.ScratchRoot, op.Username)},
		"scoreboards":                   {"resolved:   " + filepath.Join(osutil.ScratchRoot, "scoreboards")},
		"sessions.shared-capacity-root": {"resolved:   " + filepath.Join(osutil.ScratchRoot, "capacity")},
		"daemon.sockets":                {"resolved:   " + filepath.Join(base, "socket")},
	}
	for k, w := range want {
		if strings.Join(got[k], "\n") != strings.Join(w, "\n") {
			t.Errorf("%s: %q, want %q", k, got[k], w)
		}
	}
	for _, k := range []string{"audit.file", "timezone", "sharedroot"} {
		if len(got[k]) != 0 {
			t.Errorf("%s has lines: %q", k, got[k])
		}
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("the view made basedir: %v", err)
	}

	if os.Geteuid() != 0 {
		own := filepath.Join(osutil.ScratchRoot, op.Username)
		if err := os.MkdirAll(own, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(own, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(own, 0o700); os.RemoveAll(own) })
		got = lines(load(), "tempdir")
		w := []string{"resolved:   " + filepath.Join(base, "tmp"), "passed:     " + own + ": not writable by the operator"}
		if strings.Join(got["tempdir"], "\n") != strings.Join(w, "\n") {
			t.Errorf("a closed scratch folder: %q, want %q", got["tempdir"], w)
		}
	}

	wd, _ := os.Getwd()
	got = lines(load(`audit.file="~/a/audit.jsonl"`, `credential-backend.site.type="cloginrc"`, `credential-backend.site.scope="shared"`, `credential-backend.site.path="/etc/karvi/cloginrc"`, `credential-backend.mine.type="csv"`, `credential-backend.mine.scope="user"`, `credential-backend.mine.path="creds.csv"`),
		"audit.file", "credential-backend.site.path", "credential-backend.mine.path", "credential-backend.mine.type")
	want = map[string][]string{
		"audit.file":                   {"resolved:   " + filepath.Join(op.Home, "a", "audit.jsonl")},
		"credential-backend.site.path": {"resolved:   /etc/karvi/cloginrc"},
		"credential-backend.mine.path": {"resolved:   " + filepath.Join(wd, "creds.csv")},
	}
	for k, w := range want {
		if strings.Join(got[k], "\n") != strings.Join(w, "\n") {
			t.Errorf("%s: %q, want %q", k, got[k], w)
		}
	}
	if len(got["credential-backend.mine.type"]) != 0 {
		t.Errorf("a backend's type has a line: %q", got["credential-backend.mine.type"])
	}
}

// TestPlaceKeysMarked: every key the view has a rule for is one the
// registry marks as a path, the mark the load reads.
func TestPlaceKeysMarked(t *testing.T) {
	for key := range placeKeys {
		if _, ok := configschema.Place(key); !ok {
			t.Errorf("%s: a place rule, not marked in the registry", key)
		}
	}
}
