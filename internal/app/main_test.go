package app

import (
	"os"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/osutil/osutiltest"
)

// TestMain keeps the tests off the host (osutiltest.Isolate): no test
// consults the host's shared roots, and every
// activity a test starts writes its scoreboard under a directory of this
// run, not the host's shared one.
func TestMain(m *testing.M) {
	done := osutiltest.Isolate()
	code := m.Run()
	done()
	os.Exit(code)
}
