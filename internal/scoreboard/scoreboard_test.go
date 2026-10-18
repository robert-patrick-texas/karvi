package scoreboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

var snapshotTestNow = time.Date(2026, 9, 26, 23, 56, 12, 0, time.FixedZone("EDT", -4*3600))

func snapshot(id string) records.ScoreboardSnapshot {
	return records.ScoreboardSnapshot{SchemaVersion: records.ScoreboardSchemaVersion, ActivityID: id, ActivityType: "login", Mode: "login", Status: "running", Counts: records.Counts{Total: 1, InFlight: 1}, StartedAt: snapshotTestNow, LastUpdatedAt: snapshotTestNow}
}

// TestFolderWhateverWatchSays: Folder is the writer's folder and mode,
// and resolves it for a disabled writer too, which names none: every
// activity claims its ID there whatever watch.enabled says.
func TestFolderWhateverWatchSays(t *testing.T) {
	base := withScratchRoot(t, filepath.Join(t.TempDir(), "absent"))
	dir, mode, err := Folder("auto", "", base, nil)
	if want := filepath.Join(base, "state", "scoreboards"); err != nil || dir != want || mode != 0o600 {
		t.Errorf("private: %s %o %v, want %s 0600", dir, mode, err, want)
	}
	explicit := filepath.Join(t.TempDir(), "sb")
	dir, mode, err = Folder(explicit, "", base, nil)
	if err != nil || dir != explicit || mode != 0o640 {
		t.Errorf("explicit: %s %o %v", dir, mode, err)
	}
	w, err := NewWriter(explicit, "", base, true, nil)
	if err != nil || w.Directory != explicit || w.Mode != 0o640 {
		t.Errorf("writer: %+v %v", w, err)
	}
	if w, err := NewWriter(explicit, "", base, false, nil); err != nil || w.Directory != "" {
		t.Errorf("disabled writer: %+v %v", w, err)
	}
}

// TestReadPassesClaimsBy: the claims beside the scoreboards
// (osutil.ClaimExtension) are neither rows nor counted against the folder's
// maximum, and an empty scoreboard file is a row in error like any file
// that does not decode.
func TestReadPassesClaimsBy(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"260926-235612-00", "260926-235612-01"} {
		w := &Writer{Enabled: true, Directory: dir, Mode: 0o640}
		w.Use(id)
		if err := w.Write(snapshot(id)); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"260926-235612-00", "260926-235612-01", "260926-235612-02"} {
		if err := os.WriteFile(filepath.Join(dir, id+osutil.ClaimExtension), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := Read(dir, 2, 0)
	if err != nil || len(rows) != 2 || rows[0].Error != "" || rows[1].Error != "" {
		t.Fatalf("rows %+v err %v", rows, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "260926-235612-02.json"), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir, 2, 0); errorcodes.Of(err) != "scoreboard_file_limit_exceeded" {
		t.Errorf("three scoreboards over two: %v", err)
	}
	rows, err = Read(dir, 3, 0)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows %+v err %v", rows, err)
	}
	empty := 0
	for _, r := range rows {
		if r.Error != "" {
			empty++
		}
	}
	if empty != 1 {
		t.Errorf("rows in error %d, want the empty file's alone", empty)
	}
}

// TestUseNamesTheFileOfAClaimedID: an activity, or the child of the
// recording wrapper, names its file from the ID it claimed.
func TestUseNamesTheFileOfAClaimedID(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, "", "", true, nil)
	w.Use("260926-235612-07")
	if want := filepath.Join(dir, "260926-235612-07.json"); w.Path != want {
		t.Errorf("path %s, want %s", w.Path, want)
	}
	if err := w.Write(snapshot("260926-235612-07")); err != nil {
		t.Fatal(err)
	}
	if rows, err := Read(dir, 0, 0); err != nil || len(rows) != 1 || rows[0].Snapshot.ActivityID != "260926-235612-07" {
		t.Errorf("rows %+v err %v", rows, err)
	}
}

// TestFindAndStale: Find reads the one activity's file across the folders,
// the newer where two hold it, and passes by an absent, empty, or
// undecodable file; Stale is watch's rule, a terminal status never stale
// and a zero stale-after judging none.
func TestFindAndStale(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	const id = "260926-235612-00"
	if _, ok := Find([]string{a, b}, id); ok {
		t.Fatal("found in empty folders")
	}
	older, newer := snapshot(id), snapshot(id)
	newer.LastUpdatedAt = older.LastUpdatedAt.Add(time.Minute)
	w := &Writer{Enabled: true, Directory: a, Mode: 0640}
	w.Use(id)
	if err := w.Write(older); err != nil {
		t.Fatal(err)
	}
	w = &Writer{Enabled: true, Directory: b, Mode: 0640}
	w.Use(id)
	if err := w.Write(newer); err != nil {
		t.Fatal(err)
	}
	if s, ok := Find([]string{a, b}, id); !ok || !s.LastUpdatedAt.Equal(newer.LastUpdatedAt) {
		t.Fatalf("found %v %v", s.LastUpdatedAt, ok)
	}
	if err := os.WriteFile(filepath.Join(b, id+".json"), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, id+".json"), []byte("{"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, ok := Find([]string{a, b}, id); ok {
		t.Fatal("found an empty or undecodable file")
	}
	now := snapshotTestNow.Add(11 * time.Second)
	done := snapshot(id)
	done.Status = "complete"
	for _, tc := range []struct {
		s     records.ScoreboardSnapshot
		after time.Duration
		want  bool
	}{
		{snapshot(id), 10 * time.Second, true},
		{snapshot(id), 12 * time.Second, false},
		{snapshot(id), 0, false},
		{done, 10 * time.Second, false},
	} {
		if got := Stale(tc.s, tc.after, now); got != tc.want {
			t.Errorf("%s after %s: %v, want %v", tc.s.Status, tc.after, got, tc.want)
		}
	}
}
