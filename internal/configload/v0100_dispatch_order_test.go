package configload

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestV0100DispatchOrderValues: the new values and the empty key are
// accepted; the removed values
// and the removed key are refused with their own codes.
func TestV0100DispatchOrderValues(t *testing.T) {
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
	for _, tc := range []struct{ set, code, mention string }{
		{`dispatch.order="inventory"`, "config_dispatch_order_inventory_removed", "default"},
		{`dispatch.order="random-seeded"`, "config_dispatch_order_random_seeded_removed", "shuffle"},
		{`dispatch.order="bogus"`, "config_enum_value_invalid", ""},
		{`dispatch.random-seed="x"`, "config_unknown_key", ""},
	} {
		_, err := load(tc.set)
		if errorcodes.Of(err) != tc.code {
			t.Fatalf("%s: code=%q err=%v, want %s", tc.set, errorcodes.Of(err), err, tc.code)
		}
		if tc.mention != "" && !containsWord(err.Error(), tc.mention) {
			t.Fatalf("%s: message %q does not name %s", tc.set, err.Error(), tc.mention)
		}
	}
}

func containsWord(s, word string) bool {
	for i := 0; i+len(word) <= len(s); i++ {
		if s[i:i+len(word)] == word {
			return true
		}
	}
	return false
}
