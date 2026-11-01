package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/osutil/osutiltest"
	"github.com/robert-patrick-texas/karvi/records"
)

// helperEnv, set to 1 in a child's environment, makes the test binary the
// helper: TestMain hands its arguments to run. A test re-executes the binary
// for a zone, which a process takes from TZ at its start.
const helperEnv = "KARVI_PRUNE_TEST_HELPER"

// TestMain keeps the tests off the host (osutiltest.Isolate), and runs the
// helper when helperEnv says so.
func TestMain(m *testing.M) {
	done := osutiltest.Isolate()
	var code int
	if os.Getenv(helperEnv) == "1" {
		code = run(os.Args[1:], os.Stdout, os.Stderr)
	} else {
		code = m.Run()
	}
	done()
	os.Exit(code)
}

// invoke runs the helper over base alone (its private root, no shared root,
// its own scoreboards) with args after, and returns the exit, standard
// output, and standard error.
func invoke(t *testing.T, base string, args ...string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	stderr, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	code := run(append(placeArgs(base), args...), stdout, stderr)
	out, _ := os.ReadFile(stdout.Name())
	errOut, _ := os.ReadFile(stderr.Name())
	return code, string(out), string(errOut)
}

// placeArgs are the flags that keep a run to base: its private root, no
// shared root, and the scoreboards under it.
func placeArgs(base string) []string {
	return []string{"--basedir", base, "--sharedroot", "none", "--scoreboards", filepath.Join(base, "score")}
}

// finishedJob writes a job folder under jobs whose summary says complete,
// ended at ended, in the day folder of the local zone, and returns it.
func finishedJob(t *testing.T, jobs string, ended time.Time) string {
	t.Helper()
	id := ended.Format("060102-150405") + "-00"
	dir := filepath.Join(jobs, osutil.DayFolder(ended, time.Local), id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(records.Summary{SchemaVersion: records.JobSchemaVersion, JobID: id, ActivityID: id, StartedAt: ended.Add(-time.Second), EndedAt: ended, FinalStatus: "complete"})
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), b, 0o640); err != nil {
		t.Fatal(err)
	}
	return dir
}

// unreadable makes path mode 000 until the test ends; a root's run reads it
// all the same, so the test is skipped as root.
func unreadable(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads a folder of mode 000")
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o750) })
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestDaysAboveTheRangeIsAUsageError covers --days: a value from one to 999
// runs, and one outside is a usage error, exit 2, the tree untouched.
func TestDaysAboveTheRangeIsAUsageError(t *testing.T) {
	base := t.TempDir()
	job := finishedJob(t, filepath.Join(base, "jobs"), time.Now().Add(-time.Hour))
	for _, days := range []string{"1000", "999999", "0"} {
		code, out, errOut := invoke(t, base, "--days", days)
		if code != 2 || !strings.HasPrefix(errOut, "karvi-prune: --days takes one to 999, not "+days+"\n") || out != "" {
			t.Errorf("--days %s: exit %d\nstdout:\n%s\nstderr:\n%s", days, code, out, errOut)
		}
		if !exists(job) {
			t.Errorf("--days %s removed %s", days, job)
		}
	}
	code, out, errOut := invoke(t, base, "--days", "999")
	if code != 0 || !exists(job) || !strings.Contains(out, "removed=0 ") {
		t.Fatalf("--days 999: exit %d, the job there %v\nstdout:\n%s\nstderr:\n%s", code, exists(job), out, errOut)
	}
}

// TestMinfreeNotANumberIsAUsageError covers --minfree: NaN, which no range
// check catches, and the infinities are usage errors, exit 2.
func TestMinfreeNotANumberIsAUsageError(t *testing.T) {
	base := t.TempDir()
	for _, value := range []string{"NaN", "nan", "Inf", "-Inf"} {
		code, out, errOut := invoke(t, base, "--minfree", value)
		if code != 2 || !strings.HasPrefix(errOut, "karvi-prune: --minfree takes a percentage from 0 to 100, not ") || out != "" {
			t.Errorf("--minfree %s: exit %d\nstdout:\n%s\nstderr:\n%s", value, code, out, errOut)
		}
	}
}

// TestUnreadableTreeExitsOne covers a tree that cannot be walked: its root
// unreadable, the run reports it as failed and exits 1.
func TestUnreadableTreeExitsOne(t *testing.T) {
	base := t.TempDir()
	jobs := filepath.Join(base, "jobs")
	if err := os.MkdirAll(jobs, 0o750); err != nil {
		t.Fatal(err)
	}
	unreadable(t, jobs)
	code, out, errOut := invoke(t, base)
	if code != 1 || !strings.Contains(out, "failed kind=tree path="+jobs+" error=") || !strings.Contains(out, " failed=1 ") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

// TestUnreadableFolderIsPassedBy covers a folder below a root that cannot
// be read: it is a skipped line with its error, the rest of the tree is
// pruned, and the exit is 0.
func TestUnreadableFolderIsPassedBy(t *testing.T) {
	base := t.TempDir()
	jobs := filepath.Join(base, "jobs")
	old := time.Now().AddDate(0, 0, -60)
	job := finishedJob(t, jobs, old)
	closed := filepath.Join(jobs, osutil.DayFolder(old.AddDate(0, 0, 1), time.Local))
	if err := os.MkdirAll(closed, 0o750); err != nil {
		t.Fatal(err)
	}
	unreadable(t, closed)
	code, out, errOut := invoke(t, base)
	if code != 0 || exists(job) || !strings.Contains(out, "skipped kind=activity path="+closed+" error=") {
		t.Fatalf("exit %d, the old job there %v\nstdout:\n%s\nstderr:\n%s", code, exists(job), out, errOut)
	}
}

// TestDayFoldersReadInTheLocalZone covers the day folders' zone: the
// helper reads a folder's name in its own zone (TZ, as karvi's timezone
// auto names it), and an empty one goes only when the whole day it names
// ended before the cutoff. Under America/Chicago with --days 1, the folder
// of two days back goes and yesterday's and today's stay, at any hour.
func TestDayFoldersReadInTheLocalZone(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A run across Chicago's midnight would name other days: try again.
	for attempt := 0; attempt < 3; attempt++ {
		day := time.Now().In(chicago)
		base := t.TempDir()
		folder := func(back int) string {
			p := filepath.Join(base, "jobs", day.AddDate(0, 0, -back).Format(osutil.DayFolderLayout))
			if err := os.MkdirAll(p, 0o750); err != nil {
				t.Fatal(err)
			}
			return p
		}
		today, yesterday, before := folder(0), folder(1), folder(2)
		cmd := exec.Command(exe, append(placeArgs(base), "--days", "1")...)
		cmd.Env = append(os.Environ(), helperEnv+"=1", "TZ=America/Chicago")
		out, err := cmd.CombinedOutput()
		if time.Now().In(chicago).YearDay() != day.YearDay() {
			continue
		}
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if exists(before) || !exists(yesterday) || !exists(today) {
			t.Fatalf("two days back there %v (want false), yesterday %v, today %v\n%s", exists(before), exists(yesterday), exists(today), out)
		}
		return
	}
	t.Fatal("every attempt crossed Chicago's midnight")
}

// TestUnusableSharedScoreboardsExitZero covers the shared scoreboards a
// colleague's group keeps closed: a skipped line with the reason and exit 0;
// the private root's scoreboards unreadable still exit 1.
func TestUnusableSharedScoreboardsExitZero(t *testing.T) {
	base := t.TempDir()
	shared := filepath.Join(base, "score")
	if err := os.MkdirAll(shared, 0o750); err != nil {
		t.Fatal(err)
	}
	unreadable(t, shared)
	code, out, errOut := invoke(t, base)
	if code != 0 || !strings.Contains(out, "skipped kind=tree path="+shared+" error=") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	private := filepath.Join(base, "state", "scoreboards")
	if err := os.MkdirAll(private, 0o750); err != nil {
		t.Fatal(err)
	}
	unreadable(t, private)
	code, out, errOut = invoke(t, base)
	if code != 1 || !strings.Contains(out, "failed kind=tree path="+private+" error=") {
		t.Fatalf("the private scoreboards: exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}
