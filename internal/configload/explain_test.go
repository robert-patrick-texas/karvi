package configload

import (
	"errors"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
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
	explain := func(key string, resolve Resolver) string {
		t.Helper()
		got, err := snap.Explain([]string{key}, resolve)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := explain("basedir", resolve); !strings.Contains(got, "\nresolved:   /opt/karvi/users/op\n") {
		t.Fatalf("basedir:\n%s", got)
	}
	if got := explain("ssh.known-hosts-file", resolve); !strings.Contains(got, "\nresolved:   error: no private root\n") {
		t.Fatalf("an unresolved key:\n%s", got)
	}
	want := "\nresolved:   /opt/karvi/users/op/state/scoreboards\npassed:     /dev/shm/karvi/scoreboards: not writable by the operator\nenvironment:"
	if got := explain("scoreboards", resolve); !strings.Contains(got, want) {
		t.Fatalf("a candidate passed by:\n%s", got)
	}
	if got := explain("timezone", resolve); strings.Contains(got, "resolved:") {
		t.Fatalf("a key the resolver does not name:\n%s", got)
	}
	if got := explain("basedir", nil); strings.Contains(got, "resolved:") {
		t.Fatalf("a nil resolver:\n%s", got)
	}
}

// Several keys render in the order given, one block each, a blank line
// between; none named is every key.
func TestExplainSeveralKeys(t *testing.T) {
	snap, err := Load(Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := snap.Explain([]string{"tempdir", "timezone", "basedir"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	blocks := strings.Split(got, "\n\n")
	if len(blocks) != 3 || !strings.HasPrefix(blocks[0], "key:        tempdir\n") || !strings.HasPrefix(blocks[1], "key:        timezone\n") || !strings.HasPrefix(blocks[2], "key:        basedir\n") {
		t.Fatalf("blocks:\n%s", got)
	}
	all, err := snap.Explain(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(all, "\nkey:        ")+1 != len(snap.Values) {
		t.Fatalf("every key: %d blocks for %d keys", strings.Count(all, "\nkey:        ")+1, len(snap.Values))
	}
}

// A name Explain cannot show refuses the whole view before any block: one
// that is no key as the load refuses it (a table, a removed key, an unknown
// name, a blank), and a key a table could hold but the configuration does
// not set as config_key_unset; the first such name in the order given.
func TestExplainRefusesANameItCannotShow(t *testing.T) {
	snap, err := Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{`ssh.transports.alt="exec:/usr/bin/ssh"`}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := snap.Explain([]string{"ssh.transports.alt"}, nil); err != nil || !strings.HasPrefix(got, "key:        ssh.transports.alt\n") {
		t.Fatalf("a slot set: %v\n%s", err, got)
	}
	for _, c := range []struct {
		names      []string
		code, text string
	}{
		{[]string{"no.such.key"}, "config_unknown_key", "config_unknown_key: unknown configuration key for no.such.key"},
		{[]string{"ssh"}, "config_unknown_key", "config_unknown_key: unknown configuration key for ssh"},
		{[]string{"sessions.idle-timeout"}, "config_key_removed", "config_key_removed: removed in v0.14.0; the key was read by nothing and has no replacement; remove it from the configuration for sessions.idle-timeout"},
		{[]string{""}, "config_key_blank", "config_key_blank: blank configuration key"},
		{[]string{"ssh.transports.other"}, "config_key_unset", "config_key_unset: ssh.transports.other is not set"},
		{[]string{"ssh.transports.native"}, "config_key_unset", "config_key_unset: ssh.transports.native is not set"},
		{[]string{"basedir", "macros.SITE", "no.such.key"}, "config_key_unset", "config_key_unset: macros.SITE is not set"},
	} {
		got, err := snap.Explain(c.names, nil)
		if got != "" || err == nil || errorcodes.Of(err) != c.code || err.Error() != c.text {
			t.Errorf("%q: %q, %v", c.names, got, err)
		}
	}
}
