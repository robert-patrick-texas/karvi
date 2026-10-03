package scoreboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewWriterFallback: a shared directory whose parent is absent (a host
// without the scratch root) takes the private fallback without a word and
// makes nothing there; one that exists but cannot be written takes it with
// one warning naming the directory.
func TestNewWriterFallback(t *testing.T) {
	dir := t.TempDir()
	var warned []string
	warn := func(s string) { warned = append(warned, s) }

	absent := filepath.Join(dir, "shm", "scoreboards")
	w, err := NewWriter(absent, filepath.Join(dir, "private1"), true, warn)
	if err != nil {
		t.Fatal(err)
	}
	if w.Directory != filepath.Join(dir, "private1") || len(warned) != 0 {
		t.Fatalf("absent: directory %s, warnings %q", w.Directory, warned)
	}
	if _, err := os.Stat(filepath.Dir(absent)); !os.IsNotExist(err) {
		t.Fatalf("the scratch root was made: %v", err)
	}

	if os.Geteuid() == 0 {
		t.Skip("root writes every directory")
	}
	closed := filepath.Join(dir, "closed")
	if err := os.Mkdir(closed, 0o500); err != nil {
		t.Fatal(err)
	}
	w, err = NewWriter(closed, filepath.Join(dir, "private2"), true, warn)
	if err != nil {
		t.Fatal(err)
	}
	if w.Directory != filepath.Join(dir, "private2") || len(warned) != 1 || !strings.Contains(warned[0], closed) {
		t.Fatalf("closed: directory %s, warnings %q", w.Directory, warned)
	}
}
