package osutil

import (
	"os"
	"strings"
)

// ChildEnvironment is the one environment filter for every process karvi
// starts: HOME, USER, LOGNAME, and PATH, the names
// in allow (the configured security.child-environment-allowlist), and
// nothing else of the parent's environment; extra entries are appended as
// given. The system transport applies it to ssh and the launcher to
// daemon serve, so an operator's shell secret never reaches a child.
func ChildEnvironment(allow []string, extra ...string) []string {
	keep := map[string]bool{"HOME": true, "USER": true, "LOGNAME": true, "PATH": true}
	for _, n := range allow {
		keep[n] = true
	}
	out := []string{}
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && keep[name] {
			out = append(out, entry)
		}
	}
	return append(out, extra...)
}
