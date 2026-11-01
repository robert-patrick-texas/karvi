package scoreboard

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// oddEntries puts beside a scoreboard in dir, under the names given, a FIFO,
// a symbolic link to a readable snapshot elsewhere, and a sparse regular
// file one byte past maxSnapshotBytes. A FIFO a read still waits on is
// released at the test's end.
func oddEntries(t *testing.T, dir, fifo, link, big string) {
	t.Helper()
	if err := syscall.Mkfifo(filepath.Join(dir, fifo), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if f, err := os.OpenFile(filepath.Join(dir, fifo), os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			f.Close()
		}
	})
	elsewhere := t.TempDir()
	w := &Writer{Enabled: true, Directory: elsewhere, Mode: 0o640}
	w.Use("260926-235612-09")
	if err := w.Write(snapshot("260926-235612-09")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(w.Path, filepath.Join(dir, link)); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, big), os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxSnapshotBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// within runs f and fails the test when it has not returned in ten seconds.
func within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s still waits after 10s", what)
	}
}

// TestReadTakesRegularFilesOnly: of the folder's *.json entries a FIFO and
// a symbolic link are passed by, neither rows nor counted, and the read
// never waits on the FIFO; a regular file larger than a snapshot may be is
// a row in error naming its size, unread.
func TestReadTakesRegularFilesOnly(t *testing.T) {
	dir := t.TempDir()
	const id = "260926-235612-00"
	w := &Writer{Enabled: true, Directory: dir, Mode: 0o640}
	w.Use(id)
	if err := w.Write(snapshot(id)); err != nil {
		t.Fatal(err)
	}
	oddEntries(t, dir, "fifo.json", "link.json", "big.json")
	var rows []Row
	var err error
	within(t, "Read", func() { rows, err = Read(dir, 2, 0) })
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %+v err %v, want the snapshot and the large file", rows, err)
	}
	for _, r := range rows {
		switch filepath.Base(r.Path) {
		case id + ".json":
			if r.Error != "" || r.Snapshot.ActivityID != id {
				t.Errorf("snapshot row %+v", r)
			}
		case "big.json":
			if want := "67108865 bytes, more than"; !strings.HasPrefix(r.Error, want) {
				t.Errorf("large file's error %q, want it to begin %q", r.Error, want)
			}
		default:
			t.Errorf("row of %s", r.Path)
		}
	}
}

// TestFindTakesRegularFilesOnly: Find passes by a FIFO, a symbolic link,
// and a file larger than a snapshot may be, never waiting on the FIFO.
func TestFindTakesRegularFilesOnly(t *testing.T) {
	for _, name := range []string{"fifo", "link", "big"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			oddEntries(t, dir, "fifo.json", "link.json", "big.json")
			var ok bool
			within(t, "Find", func() { _, ok = Find([]string{dir}, name) })
			if ok {
				t.Errorf("found the %s", name)
			}
		})
	}
}
