package configload

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestReadingSnapshots: one reading gives a snapshot per call under its own
// options and --set, each equal to Load's under the same; a file changed
// after the reading changes none of them, no call changes the reading or
// another snapshot, the reading's warnings are in each, and a lock the global
// file declares refuses an option at the snapshot that takes it.
func TestReadingSnapshots(t *testing.T) {
	dir := t.TempDir()
	saved := globalRoots
	t.Cleanup(func() { globalRoots = saved })
	global := filepath.Join(dir, "global.toml")
	globalRoots = []string{global}
	if err := os.WriteFile(global, []byte("[config-lock]\n\"dispatch.wave-max-width\" = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.toml")
	text := "@include? " + filepath.Join(dir, "absent.toml") + "\n[dispatch]\nparallel-workers = 4\n"
	if err := os.WriteFile(cfg, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{ExplicitRoots: []string{cfg}, HomeDir: dir, Environment: []string{}}
	order := map[string]FlagValue{"dispatch.order": {Value: "sorted", Option: "--order"}}
	shuffle := []string{`dispatch.order="shuffle"`}
	loadPlain, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	loadSorted, err := Load(Options{ExplicitRoots: opts.ExplicitRoots, HomeDir: dir, Environment: []string{}, FlagValues: order})
	if err != nil {
		t.Fatal(err)
	}

	r, err := Read(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("[dispatch]\nparallel-workers = 8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := func(flags map[string]FlagValue, sets []string) Snapshot {
		t.Helper()
		s, err := r.Snapshot(flags, sets)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	plain, sorted, shuffled := snapshot(nil, nil), snapshot(order, nil), snapshot(order, shuffle)
	if _, err := r.Snapshot(map[string]FlagValue{"dispatch.wave-max-width": {Value: int64(64), Option: "--max-width"}}, nil); errorcodes.Of(err) != "config_lock_violation" || !strings.HasSuffix(err.Error(), "at --max-width") {
		t.Errorf("lock: %v", err)
	}
	again := snapshot(nil, nil)

	for name, c := range map[string]struct {
		s     Snapshot
		order string
	}{"plain": {plain, "default"}, "sorted": {sorted, "sorted"}, "shuffled": {shuffled, "shuffle"}, "again": {again, "default"}} {
		if got := c.s.String("dispatch.order"); got != c.order {
			t.Errorf("%s: order %q, want %q", name, got, c.order)
		}
		if got := c.s.Int("dispatch.parallel-workers"); got != 4 {
			t.Errorf("%s: parallel-workers %d, the file read again", name, got)
		}
		if len(c.s.Warnings) != 1 || !strings.HasPrefix(c.s.Warnings[0], "optional include missing: ") {
			t.Errorf("%s: warnings %q", name, c.s.Warnings)
		}
	}
	if !reflect.DeepEqual(plain, loadPlain) || !reflect.DeepEqual(sorted, loadSorted) || !reflect.DeepEqual(again, plain) {
		t.Errorf("a snapshot differs from Load's under the same options: digests %s %s %s, Load's %s %s", plain.Digest, sorted.Digest, again.Digest, loadPlain.Digest, loadSorted.Digest)
	}
	if plain.Digest == sorted.Digest || sorted.Digest == shuffled.Digest {
		t.Errorf("one digest for two configurations: %s %s %s", plain.Digest, sorted.Digest, shuffled.Digest)
	}
	if v := r.snap.Values["dispatch.order"]; v.Data != "default" || v.Source.Layer != "builtin" {
		t.Errorf("the reading changed: dispatch.order %+v", v)
	}
}
