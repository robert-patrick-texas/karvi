package configload

import (
	"errors"
	"strings"
	"testing"
)

// The explain view prints a resolved line for a key the resolver names, the
// error where it cannot, and nothing for any other key or a nil resolver.
func TestExplainResolvedLine(t *testing.T) {
	snap, err := Load(Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(key string) (string, bool, error) {
		switch key {
		case "basedir":
			return "/opt/karvi/users/op", true, nil
		case "ssh.known-hosts-file":
			return "", true, errors.New("no private root")
		}
		return "", false, nil
	}
	if got := snap.Explain("basedir", resolve); !strings.Contains(got, "\nresolved:   /opt/karvi/users/op\n") {
		t.Fatalf("basedir:\n%s", got)
	}
	if got := snap.Explain("ssh.known-hosts-file", resolve); !strings.Contains(got, "\nresolved:   error: no private root\n") {
		t.Fatalf("an unresolved key:\n%s", got)
	}
	if got := snap.Explain("tempdir", resolve); strings.Contains(got, "resolved:") {
		t.Fatalf("a key the resolver does not name:\n%s", got)
	}
	if got := snap.Explain("basedir", nil); strings.Contains(got, "resolved:") {
		t.Fatalf("a nil resolver:\n%s", got)
	}
}
