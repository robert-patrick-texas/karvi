package scoreboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

// withScratchRoot points osutil.ScratchRoot at root for one test and
// returns a private root whose state folder exists, as an activity's has.
func withScratchRoot(t *testing.T, root string) string {
	t.Helper()
	saved := osutil.ScratchRoot
	osutil.ScratchRoot = root
	t.Cleanup(func() { osutil.ScratchRoot = saved })
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	return base
}

// TestNewWriterFallback: without the scratch root the private folder is
// taken without a word and the root is not made; with it, its folder is
// made in it; a folder there closed to the operator is passed by with one
// warning naming it. In each case Place names, creating nothing, what the
// writer then takes.
func TestNewWriterFallback(t *testing.T) {
	dir := t.TempDir()
	scratch := filepath.Join(dir, "shm")
	base := withScratchRoot(t, scratch)
	private := filepath.Join(base, "state", "scoreboards")
	var warned []string
	warn := func(s string) { warned = append(warned, s) }
	agree := func(name, want string, passed int) {
		t.Helper()
		pl, err := Place("auto", "", base)
		if err != nil || pl.Path != want || len(pl.Passed) != passed {
			t.Fatalf("%s: place %+v %v, want %s", name, pl, err, want)
		}
		w, err := NewWriter("auto", "", base, true, warn)
		if err != nil || w.Directory != want {
			t.Fatalf("%s: writer %v %v, want %s", name, w, err, want)
		}
	}

	agree("no scratch root", private, 0)
	if len(warned) != 0 {
		t.Fatalf("warnings %q", warned)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("the scratch root was made: %v", err)
	}

	if err := os.Mkdir(scratch, 0o770); err != nil {
		t.Fatal(err)
	}
	agree("in the scratch root", filepath.Join(scratch, "scoreboards"), 0)

	if os.Geteuid() == 0 {
		t.Skip("root writes every directory")
	}
	closed := filepath.Join(scratch, "scoreboards")
	if err := os.Chmod(closed, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(closed, 0o700) })
	agree("closed", private, 1)
	if len(warned) != 1 || !strings.Contains(warned[0], closed) {
		t.Fatalf("closed: warnings %q", warned)
	}

	// An explicit path is used or refused, by both.
	if _, err := NewWriter(closed, "", base, true, warn); errorcodes.Of(err) != "scoreboard_root_invalid" {
		t.Fatalf("explicit, closed: %v", err)
	}
	if _, err := Place(closed, "", base); errorcodes.Of(err) != "scoreboard_root_invalid" {
		t.Fatalf("explicit, closed, place: %v", err)
	}
	// ~ is the home given.
	home := t.TempDir()
	if w, err := NewWriter("~/sb", home, base, true, warn); err != nil || w.Directory != filepath.Join(home, "sb") {
		t.Fatalf("~: %v %v", w, err)
	}
}

// TestDirectoriesAndReadAll: watch reads every place of the chain that
// exists, one row per job, and makes nothing.
func TestDirectoriesAndReadAll(t *testing.T) {
	dir := t.TempDir()
	scratch := filepath.Join(dir, "shm")
	base := withScratchRoot(t, scratch)
	if dirs, err := Directories("auto", "", base); err != nil || len(dirs) != 0 {
		t.Fatalf("a fresh host: %q %v", dirs, err)
	}
	shared, private := filepath.Join(scratch, "scoreboards"), filepath.Join(base, "state", "scoreboards")
	for _, d := range []string{shared, private} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	dirs, err := Directories("auto", "", base)
	if err != nil || len(dirs) != 2 || dirs[0] != shared || dirs[1] != private {
		t.Fatalf("both: %q %v", dirs, err)
	}
	now := time.Now()
	write := func(d, id string, at time.Time) {
		t.Helper()
		s := records.ScoreboardSnapshot{SchemaVersion: records.ScoreboardSchemaVersion, ActivityID: id, JobID: id, Status: "completed", Mode: "cmd", StartedAt: at, LastUpdatedAt: at}
		if err := osutil.AtomicJSON(filepath.Join(d, id+".json"), s, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(shared, "260101-000000-00", now)
	write(private, "260101-000001-00", now)
	write(private, "260101-000000-00", now.Add(-time.Minute))
	rows, err := ReadAll(dirs, 100, 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %d %v", len(rows), err)
	}
	for _, r := range rows {
		if r.Snapshot.JobID == "260101-000000-00" && r.Path != filepath.Join(shared, "260101-000000-00.json") {
			t.Fatalf("the older copy won: %s", r.Path)
		}
	}
	if os.Geteuid() != 0 {
		if err := os.Chmod(shared, 0o300); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(shared, 0o700) })
		if rows, err := ReadAll(dirs, 100, 0); err != nil || len(rows) != 2 {
			t.Fatalf("an unreadable shared folder is passed by: %d %v", len(rows), err)
		}
	}
}
