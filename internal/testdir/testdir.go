// Package testdir gives a test a short directory of its own. A test whose
// scratch, or whose basedir under the scratch's auto chain, must fit the
// askpass socket's bound (osutil.MaxScratchDir, 69 bytes) takes its directory
// from Short and not from t.TempDir, which lies under TMPDIR and appends the
// test's name: under the release's 25-byte TMPDIR such a path was already 70
// bytes or more, so auto passed <basedir>/tmp by and made the host's
// /tmp/karvi-<uid> (docs/EXAMPLES.md chapter 44). The package imports nothing
// of karvi, so osutil's own tests use it too.
package testdir

import (
	"os"
	"testing"
)

// Short returns a new directory under /tmp, not TMPDIR, so its length does
// not depend on TMPDIR's, removed when the test ends.
func Short(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "kt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
