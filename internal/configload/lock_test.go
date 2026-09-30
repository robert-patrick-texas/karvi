package configload

import "testing"

func TestLockMatch(t *testing.T) {
	cases := []struct {
		p, k string
		ok   bool
	}{{"ssh.*", "ssh.timeout", true}, {"ssh.*", "ssh.foo.bar", true}, {"credential-backend.*.env-indirection", "credential-backend.x.env-indirection", true}, {"credential-backend.*.env-indirection", "credential-backend.x.env-indirection.password", false}}
	for _, c := range cases {
		if got := lockMatches(c.p, c.k); got != c.ok {
			t.Fatalf("%s %s=%v", c.p, c.k, got)
		}
	}
}
func TestEqualSpecificity(t *testing.T) {
	_, err := winningLock([]LockDecl{{Pattern: "*.x", Source: SourceRef{Path: "a", Line: 1}}, {Pattern: "*.x", Source: SourceRef{Path: "b", Line: 2}}}, "a.x")
	if err == nil {
		t.Fatal("expected ambiguity")
	}
}
