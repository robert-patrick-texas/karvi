package osutil

import (
	"os"
	"strings"
)

// ChildEnvironment is the environment filter for ssh and daemon serve:
// HOME, USER, LOGNAME, and PATH, the names in allow (the configured
// security.child-environment-allowlist), and nothing else of the parent's
// environment; extra entries are appended as given. The system transport
// applies it to ssh and the launcher to daemon serve, so an operator's
// shell secret never reaches either. The children it does not filter
// include the recording wrapper's script(1), which runs karvi login, and
// the crun.after hook, which has the operator's environment without
// NETUSER, NETPASS, and NETENABLE.
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
