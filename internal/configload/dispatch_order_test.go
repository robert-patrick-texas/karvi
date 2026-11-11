package configload

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestDispatchOrderValues: the key's values and the empty shuffle key are
// accepted; any other value is refused as a value the key does not take.
func TestDispatchOrderValues(t *testing.T) {
	load := func(sets ...string) (Snapshot, error) {
		return Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: sets})
	}
	snap, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.String("dispatch.order"); got != "default" {
		t.Fatalf("dispatch.order default=%q, want default", got)
	}
	if got := snap.String("dispatch.shuffle-key"); got != "" {
		t.Fatalf("dispatch.shuffle-key default=%q, want empty", got)
	}
	for _, value := range []string{"default", "sorted", "name", "shuffle", "random"} {
		if _, err := load("dispatch.order=\"" + value + "\""); err != nil {
			t.Fatalf("%s rejected: %v", value, err)
		}
	}
	if _, err := load(`dispatch.order="shuffle"`, `dispatch.shuffle-key=""`); err != nil {
		t.Fatalf("empty shuffle key rejected: %v", err)
	}
	if snap, err := load(`dispatch.shuffle-key="rollout-7"`); err != nil || snap.String("dispatch.shuffle-key") != "rollout-7" {
		t.Fatalf("shuffle key: %v %q", err, snap.String("dispatch.shuffle-key"))
	}
	if _, err := load(`dispatch.order="bogus"`); errorcodes.Of(err) != "config_enum_value_invalid" {
		t.Fatalf("bogus: code=%q err=%v, want config_enum_value_invalid", errorcodes.Of(err), err)
	}
}
