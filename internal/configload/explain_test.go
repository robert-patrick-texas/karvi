package configload

import (
	"errors"
	"strings"
	"testing"
)

// The explain view prints a resolved line for a key the resolver names, the
// error where it cannot, a passed line per candidate passed by right after
// it, and nothing for any other key or a nil resolver.
func TestExplainResolvedLine(t *testing.T) {
	snap, err := Load(Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(key string) (Place, bool) {
		switch key {
		case "basedir":
			return Place{Path: "/opt/karvi/users/op"}, true
		case "ssh.known-hosts-file":
			return Place{Err: errors.New("no private root")}, true
		case "scoreboards":
			return Place{Path: "/opt/karvi/users/op/state/scoreboards", Passed: []PassedCandidate{{"/dev/shm/karvi/scoreboards", "not writable by the operator"}}}, true
		}
		return Place{}, false
	}
	if got := snap.Explain([]string{"basedir"}, resolve); !strings.Contains(got, "\nresolved:   /opt/karvi/users/op\n") {
		t.Fatalf("basedir:\n%s", got)
	}
	if got := snap.Explain([]string{"ssh.known-hosts-file"}, resolve); !strings.Contains(got, "\nresolved:   error: no private root\n") {
		t.Fatalf("an unresolved key:\n%s", got)
	}
	want := "\nresolved:   /opt/karvi/users/op/state/scoreboards\npassed:     /dev/shm/karvi/scoreboards: not writable by the operator\nenvironment:"
	if got := snap.Explain([]string{"scoreboards"}, resolve); !strings.Contains(got, want) {
		t.Fatalf("a candidate passed by:\n%s", got)
	}
	if got := snap.Explain([]string{"timezone"}, resolve); strings.Contains(got, "resolved:") {
		t.Fatalf("a key the resolver does not name:\n%s", got)
	}
	if got := snap.Explain([]string{"basedir"}, nil); strings.Contains(got, "resolved:") {
		t.Fatalf("a nil resolver:\n%s", got)
	}
}

// Several keys render in the order given, one block each, a blank line
// between; an unknown one is `error: not found` in its turn; none named is
// every key.
func TestExplainSeveralKeys(t *testing.T) {
	snap, err := Load(Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	got := snap.Explain([]string{"tempdir", "ssh", "basedir"}, nil)
	blocks := strings.Split(got, "\n\n")
	if len(blocks) != 3 || !strings.HasPrefix(blocks[0], "key:        tempdir\n") || blocks[1] != "key: ssh\nerror: not found" || !strings.HasPrefix(blocks[2], "key:        basedir\n") {
		t.Fatalf("blocks:\n%s", got)
	}
	if all := snap.Explain(nil, nil); strings.Count(all, "\nkey:        ")+1 != len(snap.Values) {
		t.Fatalf("every key: %d blocks for %d keys", strings.Count(all, "\nkey:        ")+1, len(snap.Values))
	}
}
