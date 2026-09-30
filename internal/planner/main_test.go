package planner

import (
	"os"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/osutil/osutiltest"
)

// fixtureHome is the fixture operator's home (draft_test.go): a fixed path
// of the tests' own, made here and removed after, so the drafts' output
// roots, and the K03 pin with them, are the same on every host and no
// draft creates anything under the real operator's home (with the real
// home the pin encoded this host's layout, and the resolver made the
// operator's XDG root as a side effect).
const fixtureHome = "/tmp/karvi-k03-home"

// TestMain keeps the tests off the host (osutiltest.Isolate): no test
// consults the host's shared roots, so the drafts'
// output roots, and the K03 pin with them, do not depend on a `sudo karvi
// setup shared` run on this host; and it makes the fixture home.
func TestMain(m *testing.M) {
	done := osutiltest.Isolate()
	os.RemoveAll(fixtureHome)
	if err := os.MkdirAll(fixtureHome, 0o700); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(fixtureHome)
	done()
	os.Exit(code)
}
