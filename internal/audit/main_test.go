package audit

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the journal socket at a path where nothing listens, so
// that a test reaches journald only through its fake (fakeJournal), and no
// run writes to the host's journal.
func TestMain(m *testing.M) {
	journalSocket = filepath.Join(os.TempDir(), "karvi-test-no-journal", "socket")
	os.Exit(m.Run())
}
