package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// The job directory is derived from the ID's instant
// in the effective timezone, the path the runner wrote.
func TestJobDirectoryForIDAcrossMidnight(t *testing.T) {
	load := func(sets ...string) configload.Snapshot {
		snap, err := configload.Load(configload.Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: sets})
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	// The stamp is read in the effective timezone: one
	// ID names the same wall-clock second in every zone, and the day folder
	// is the stamp's date.
	id := "260915-233000-07"
	at, err := JobIDTime(id, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !at.Equal(time.Date(2026, 9, 15, 23, 30, 0, 0, time.UTC)) {
		t.Fatalf("instant %v", at)
	}
	for _, zone := range []string{"America/New_York", "UTC", "Asia/Tokyo"} {
		dir, _, _, err := jobDirectoryFor(load(`timezone="`+zone+`"`), "/base", "/home/x", id)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("/base", "jobs", "260915", id); dir != want {
			t.Errorf("%s: %s, want %s", zone, dir, want)
		}
	}
	dir, _, _, err := jobDirectoryFor(load(`output.root="/var/karvi/out"`, `timezone="UTC"`), "/base", "/home/x", id)
	if err != nil || dir != filepath.Join("/var/karvi/out", "260915", id) {
		t.Fatalf("output.root: %s %v", dir, err)
	}
	for _, bad := range []string{"", "x", "260915-233000-0", "261315-233000-00", "20260915T233000.123456-0400-0123456789abcdefghjk"} {
		if _, err := JobIDTime(bad, time.UTC); err == nil || errorcodes.Of(err) != "job_request_malformed" {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// TestLocateJobDirectoryUnreachable: a job folder whose day folder cannot
// be read is found, and InspectJobDirectory names why,
// run_output_records_unreadable, never a job that does not exist (EXAMPLES
// chapter 61, issue 4); an absent one is not found.
func TestLocateJobDirectoryUnreachable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "jobs")
	cfg, err := configload.Load(configload.Options{HomeDir: dir, SkipAuto: true, Environment: []string{}, Sets: []string{`basedir="` + dir + `"`, `sharedroot="none"`, `output.root="` + root + `"`, `timezone="UTC"`}})
	if err != nil {
		t.Fatal(err)
	}
	common := CommonOptions{Config: cfg, Operator: credentials.Operator{Home: dir}}
	id := "261016-015356-00"
	day := filepath.Join(root, "261016")
	if err := os.MkdirAll(filepath.Join(day, id), 0o700); err != nil {
		t.Fatal(err)
	}
	if f, err := LocateJobDirectory(common, "261016-015357-00"); err != nil || f.Found {
		t.Fatalf("absent: %+v %v", f, err)
	}
	if err := os.Chmod(day, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(day, 0o700) })
	f, err := LocateJobDirectory(common, id)
	if err != nil || !f.Found || f.Path != filepath.Join(day, id) {
		t.Fatalf("unreachable: %+v %v", f, err)
	}
	if state, err := InspectJobDirectory(f.Path); state != JobDirectoryAbsent || errorcodes.Of(err) != "run_output_records_unreadable" || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("inspect: %v %v", state, err)
	}
}
