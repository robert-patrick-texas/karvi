package scoreboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/records"
)

var reserveTestNow = time.Date(2026, 9, 26, 23, 56, 12, 0, time.FixedZone("EDT", -4*3600))

func snapshot(id string) records.ScoreboardSnapshot {
	return records.ScoreboardSnapshot{SchemaVersion: records.ScoreboardSchemaVersion, ActivityID: id, ActivityType: "login", Mode: "login", Status: "running", Counts: records.Counts{Total: 1, InFlight: 1}, StartedAt: reserveTestNow, LastUpdatedAt: reserveTestNow}
}

// TestReserveSequencesWithinOneSecond: three logins in one second take 00,
// 01, and 02 by their scoreboard files, each created empty and
// exclusively; the first Write replaces the empty file.
func TestReserveSequencesWithinOneSecond(t *testing.T) {
	dir := t.TempDir()
	loc := reserveTestNow.Location()
	var ids []string
	var writers []*Writer
	for i := 0; i < 3; i++ {
		w, err := NewWriter(dir, "", true, nil)
		if err != nil {
			t.Fatal(err)
		}
		id, err := w.Reserve(reserveTestNow, loc)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, id+".json"); w.Path != want {
			t.Errorf("path %s, want %s", w.Path, want)
		}
		fi, err := os.Stat(w.Path)
		if err != nil || fi.Size() != 0 || fi.Mode().Perm() != 0o640 {
			t.Errorf("%s: %v %v", w.Path, fi, err)
		}
		ids = append(ids, id)
		writers = append(writers, w)
	}
	if want := []string{"260926-235612-00", "260926-235612-01", "260926-235612-02"}; ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Errorf("ids %v, want %v", ids, want)
	}
	// The stamp is the effective timezone's wall clock.
	w, _ := NewWriter(dir, "", true, nil)
	if id, err := w.Reserve(reserveTestNow, time.UTC); err != nil || id != "260927-035612-00" {
		t.Errorf("UTC: %s %v", id, err)
	}
	if err := writers[0].Write(snapshot(ids[0])); err != nil {
		t.Fatal(err)
	}
	rows, err := Read(dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The written one is a row; the three empty reservations are not rows
	// and not invalid.
	if len(rows) != 1 || rows[0].Snapshot.ActivityID != ids[0] || rows[0].Error != "" {
		t.Errorf("rows %+v", rows)
	}
}

// TestReserveDisabledGivesTheUnreservedID: a login under watch.enabled
// false writes no scoreboard and takes sequence 00 with no file behind it,
// as an activity that writes no files does.
func TestReserveDisabledGivesTheUnreservedID(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := w.Reserve(reserveTestNow, reserveTestNow.Location())
	if err != nil || id != "260926-235612-00" {
		t.Errorf("id %s err %v", id, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 || w.Path != "" {
		t.Errorf("a disabled writer left %v, path %q", entries, w.Path)
	}
	w.Release()
}

// TestReleaseRemovesOnlyAnEmptyReservation: Release takes an unwritten
// reservation away and leaves a written scoreboard alone.
func TestReleaseRemovesOnlyAnEmptyReservation(t *testing.T) {
	dir := t.TempDir()
	loc := reserveTestNow.Location()
	empty, _ := NewWriter(dir, "", true, nil)
	if _, err := empty.Reserve(reserveTestNow, loc); err != nil {
		t.Fatal(err)
	}
	written, _ := NewWriter(dir, "", true, nil)
	id, err := written.Reserve(reserveTestNow, loc)
	if err != nil {
		t.Fatal(err)
	}
	if err := written.Write(snapshot(id)); err != nil {
		t.Fatal(err)
	}
	empty.Release()
	written.Release()
	if _, err := os.Stat(empty.Path); !os.IsNotExist(err) {
		t.Errorf("the empty reservation stays: %v", err)
	}
	if fi, err := os.Stat(written.Path); err != nil || fi.Size() == 0 {
		t.Errorf("the written scoreboard was touched: %v %v", fi, err)
	}
	// A second Release of a removed file is nothing to do.
	empty.Release()
}

// TestUseNamesTheFileOfAReservedID: a run or cmd, or the child of the
// recording wrapper, names its file from an ID reserved elsewhere.
func TestUseNamesTheFileOfAReservedID(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, "", true, nil)
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
