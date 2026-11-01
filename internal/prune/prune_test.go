package prune

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/osutil/osutiltest"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestMain keeps the tests off the host (osutiltest.Isolate): the run's
// "auto" shared root consults osutil.SharedRoots, which must never be the
// host's while a test runs.
func TestMain(m *testing.M) {
	done := osutiltest.Isolate()
	code := m.Run()
	done()
	os.Exit(code)
}

// now is the test's clock; the cutoff at the default thirty-one days is
// 2026-08-26, so a job ended on 2026-08-17 is old and one ended on 2026-09-26
// is not.
var now = time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)

func writeJob(t *testing.T, root, day, id, status string, ended time.Time) string {
	t.Helper()
	dir := filepath.Join(root, day, id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(records.Summary{SchemaVersion: records.JobSchemaVersion, JobID: id, ActivityID: id, StartedAt: ended.Add(-time.Second), EndedAt: ended, FinalStatus: status})
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), b, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "commands.jsonl"), []byte("{}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeTranscript(t *testing.T, root, day, stem string, ended time.Time) string {
	t.Helper()
	dir := filepath.Join(root, day)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(dir, stem+".meta.jsonl")
	lines := `{"record":"start","started_at":"` + ended.Add(-time.Second).Format(time.RFC3339Nano) + `"}` + "\n" +
		`{"record":"end","ended_at":"` + ended.Format(time.RFC3339Nano) + `"}` + "\n"
	if err := os.WriteFile(meta, []byte(lines), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, stem+".log"), []byte("fake-iosxe> show clock\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	return meta
}

func writeScoreboard(t *testing.T, root, id, status string, updated time.Time) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(records.ScoreboardSnapshot{ActivityID: id, Status: status, LastUpdatedAt: updated})
	path := filepath.Join(root, id+".json")
	if err := os.WriteFile(path, b, 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

// run prunes the roots in dry-run as the test's own user and returns the
// report's lines.
func run(t *testing.T, opts Options) ([]string, Result) {
	t.Helper()
	return runAs(t, opts, os.Getuid(), true)
}

// runAs prunes as uid (0 is root's run), dry or for real; the day folders
// are read in UTC, the test clock's zone, unless opts names another.
func runAs(t *testing.T, opts Options, uid int, dry bool) ([]string, Result) {
	t.Helper()
	opts.Now = now
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	opts.UID = uid
	opts.DryRun = dry
	var out bytes.Buffer
	res, err := Run(opts, &out)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(out.String()), "\n"), res
}

// has says whether a removal line names path (a transcript line names its
// metadata file after path= and its transcript after transcript=).
func has(lines []string, path string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, "kept ") {
			continue
		}
		if strings.Contains(l, "path="+path+" ") || strings.Contains(l, "transcript="+path+" ") {
			return true
		}
	}
	return false
}

// TestDayFolderLayout covers the day folder layout: the walk is over the
// YYMMDD day folders of both trees; a folder of another name under the
// root, the earlier YYYY-MM-DD layout included, is skipped and
// counted, its contents never examined.
func TestDayFolderLayout(t *testing.T) {
	base := t.TempDir()
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	young := time.Date(2026, 9, 26, 15, 38, 59, 0, time.UTC)
	jobs := filepath.Join(base, "jobs")
	oldJob := writeJob(t, jobs, "260817", "260817-153859-00", "complete", old)
	youngJob := writeJob(t, jobs, "260926", "260926-153859-00", "complete", young)
	legacy := writeJob(t, jobs, "2026-08-17", "260817-100000-00", "complete", old)
	other := writeJob(t, jobs, "archive", "260817-110000-00", "complete", old)
	transcripts := filepath.Join(base, "transcripts")
	oldMeta := writeTranscript(t, transcripts, "260817", "fake-iosxe-153900", old)
	youngMeta := writeTranscript(t, transcripts, "260926", "fake-iosxe-153900", young)
	legacyMeta := writeTranscript(t, transcripts, "2026-08-17", "fake-iosxe-100000", old)

	lines, res := run(t, Options{PrivateRoots: []string{base}})
	if !has(lines, oldJob) {
		t.Fatalf("the old job under a YYMMDD folder is not a candidate:\n%s", strings.Join(lines, "\n"))
	}
	if has(lines, youngJob) {
		t.Fatalf("the young job is a candidate:\n%s", strings.Join(lines, "\n"))
	}
	for _, p := range []string{legacy, other, legacyMeta} {
		if has(lines, p) {
			t.Fatalf("%s under a folder that is not a day folder is a candidate:\n%s", p, strings.Join(lines, "\n"))
		}
	}
	if !has(lines, oldMeta) || has(lines, youngMeta) {
		t.Fatalf("the transcripts: old must be a candidate and young not:\n%s", strings.Join(lines, "\n"))
	}
	// Two jobs and two transcripts examined; the three folders that are not
	// day folders (two in the job tree, one in the transcript tree) skipped.
	if res.Examined != 4 || res.Skipped != 3 || res.Removed != 2 {
		t.Fatalf("result %+v", res)
	}
}

// TestEveryFinalStatus covers the final statuses: an old job of each of
// the six final statuses and an old scoreboard file of each are candidates,
// exercised among them; a running scoreboard file is not, whatever its
// age. A running job's folder is an orphan's (TestOrphansAndStaleSnapshots).
func TestEveryFinalStatus(t *testing.T) {
	base := t.TempDir()
	score := filepath.Join(base, "score")
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	jobs := filepath.Join(base, "jobs")
	var want []string
	for i, status := range records.FinalStatuses {
		id := "260817-15385" + string(rune('0'+i)) + "-00"
		want = append(want, writeJob(t, jobs, "260817", id, status, old), writeScoreboard(t, score, id, status, old))
	}
	running := writeScoreboard(t, score, "260926-160000-00", "running", now.Add(-time.Minute))
	lines, res := run(t, Options{PrivateRoots: []string{base}, ScoreboardRoot: score})
	for _, p := range want {
		if !has(lines, p) {
			t.Fatalf("%s is not a candidate:\n%s", p, strings.Join(lines, "\n"))
		}
	}
	if has(lines, running) {
		t.Fatalf("the running scoreboard is a candidate:\n%s", strings.Join(lines, "\n"))
	}
	if res.Removed != 12 || res.Skipped != 1 {
		t.Fatalf("result %+v", res)
	}
}

// TestSharedTrees covers the shared trees: the shared root's jobs and
// transcripts trees are walked beside the private root's, its crun
// directory never; "none" walks the private root alone; a shared root
// without the trees is passed by; the pressure floor is judged per tree.
func TestSharedTrees(t *testing.T) {
	base := t.TempDir()
	shared := t.TempDir()
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	private := writeJob(t, filepath.Join(base, "jobs"), "260817", "260817-153901-00", "complete", old)
	sharedJob := writeJob(t, filepath.Join(shared, "jobs"), "260817", "260817-153859-00", "complete", old)
	sharedMeta := writeTranscript(t, filepath.Join(shared, "transcripts"), "260817", "fake-iosxe-153900", old)
	crun := filepath.Join(shared, "crun")
	if err := os.MkdirAll(filepath.Join(crun, "260817"), 0o750); err != nil {
		t.Fatal(err)
	}
	crunFile := filepath.Join(crun, "260817", "old-file")
	if err := os.WriteFile(crunFile, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	// crun's file is old by its mtime, the only time it has.
	if err := os.Chtimes(crunFile, old, old); err != nil {
		t.Fatal(err)
	}

	lines, res := run(t, Options{PrivateRoots: []string{base}, SharedRoot: shared})
	for _, p := range []string{private, sharedJob, sharedMeta} {
		if !has(lines, p) {
			t.Fatalf("%s is not a candidate:\n%s", p, strings.Join(lines, "\n"))
		}
	}
	if has(lines, crunFile) || res.Removed != 3 {
		t.Fatalf("the crun directory was walked, or the count is off: %+v\n%s", res, strings.Join(lines, "\n"))
	}

	lines, res = run(t, Options{PrivateRoots: []string{base}, SharedRoot: "none"})
	if !has(lines, private) || has(lines, sharedJob) || res.Removed != 1 {
		t.Fatalf("none: %+v\n%s", res, strings.Join(lines, "\n"))
	}

	// A shared root without the trees: passed by, nothing reported.
	lines, res = run(t, Options{PrivateRoots: []string{base}, SharedRoot: t.TempDir()})
	if res.Removed != 1 || len(lines) != 1 {
		t.Fatalf("an empty shared root: %+v\n%s", res, strings.Join(lines, "\n"))
	}

	// The private root's trees absent: only the shared root's walk.
	lines, res = run(t, Options{PrivateRoots: []string{t.TempDir()}, SharedRoot: shared})
	if has(lines, private) || !has(lines, sharedJob) || res.Removed != 2 {
		t.Fatalf("no private trees: %+v\n%s", res, strings.Join(lines, "\n"))
	}
}

// TestOwnershipAndFailure covers ownership: a run removes what its user
// owns and counts another's as not owned, a root run (UID 0) removes all,
// and a removal that fails is one line and a count while the run goes on,
// the report ending with what could be done.
func TestOwnershipAndFailure(t *testing.T) {
	base := t.TempDir()
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	jobs := filepath.Join(base, "jobs")
	a := writeJob(t, jobs, "260817", "260817-153859-00", "complete", old)
	b := writeJob(t, jobs, "260817", "260817-153859-01", "error", old)
	c := writeJob(t, jobs, "260817", "260817-153859-02", "halt", old)

	// Another user's run: every folder is somebody else's.
	lines, res := runAs(t, Options{PrivateRoots: []string{base}}, os.Getuid()+1, true)
	if len(lines) != 1 || lines[0] != "" || res.NotOwned != 3 || res.Removed != 0 {
		t.Fatalf("another user: %+v\n%s", res, strings.Join(lines, "\n"))
	}
	// Root's run: all three.
	lines, res = runAs(t, Options{PrivateRoots: []string{base}}, 0, true)
	if res.Removed != 3 || res.NotOwned != 0 {
		t.Fatalf("root: %+v\n%s", res, strings.Join(lines, "\n"))
	}
	// A real run with the middle folder unwritable: a and c go, b is one
	// failed line, the count says so.
	if err := os.Chmod(b, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(b, 0o755) })
	lines, res = runAs(t, Options{PrivateRoots: []string{base}}, os.Getuid(), false)
	if res.Removed != 2 || res.Failed != 1 {
		t.Fatalf("the failed removal: %+v\n%s", res, strings.Join(lines, "\n"))
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatalf("%s was not removed", a)
	}
	if _, err := os.Stat(c); !os.IsNotExist(err) {
		t.Fatalf("%s was not removed", c)
	}
	if _, err := os.Stat(filepath.Join(b, "summary.json")); err != nil {
		t.Fatalf("%s did not stay: %v", b, err)
	}
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "removed kind=activity path="+b+" ") {
			t.Errorf("a removed line for the failed item (one line per item): %s", l)
		}
		if strings.HasPrefix(l, "failed kind=activity path="+b+" error=") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no failed line:\n%s", strings.Join(lines, "\n"))
	}
}

// TestEmptyDayFolders covers empty day folders: a day folder that is
// empty and older than the retention age by its name goes, in both trees
// of both roots and in the same pass that emptied it; the day's own
// folder, a young empty folder, an old folder holding a stray entry, and
// another user's stay; a dry run names them and leaves them.
func TestEmptyDayFolders(t *testing.T) {
	base := t.TempDir()
	shared := t.TempDir()
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	mk := func(p string) string {
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
		return p
	}
	oldEmpty := mk(filepath.Join(base, "jobs", "260801"))
	oldEmptyT := mk(filepath.Join(base, "transcripts", "260801"))
	sharedEmpty := mk(filepath.Join(shared, "jobs", "260802"))
	sharedEmptyT := mk(filepath.Join(shared, "transcripts", "260802"))
	today := mk(filepath.Join(base, "jobs", "260926"))
	young := mk(filepath.Join(base, "jobs", "260920"))
	stray := mk(filepath.Join(base, "jobs", "260803"))
	if err := os.WriteFile(filepath.Join(stray, "notes.txt"), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	notDay := mk(filepath.Join(base, "jobs", "archive"))
	// An old job whose removal empties its day folder in the same pass.
	emptied := writeJob(t, filepath.Join(base, "jobs"), "260817", "260817-153859-00", "complete", old)

	lines, res := run(t, Options{PrivateRoots: []string{base}, SharedRoot: shared})
	for _, p := range []string{oldEmpty, oldEmptyT, sharedEmpty, sharedEmptyT} {
		if !has(lines, p) {
			t.Fatalf("dry run: %s is not named:\n%s", p, strings.Join(lines, "\n"))
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("dry run removed %s", p)
		}
	}
	// The dry run cannot see the folder the real run empties.
	if has(lines, filepath.Dir(emptied)) || res.Removed != 5 {
		t.Fatalf("dry run: %+v\n%s", res, strings.Join(lines, "\n"))
	}

	lines, res = runAs(t, Options{PrivateRoots: []string{base}, SharedRoot: shared}, os.Getuid(), false)
	for _, p := range []string{oldEmpty, oldEmptyT, sharedEmpty, sharedEmptyT, emptied, filepath.Dir(emptied)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s stayed", p)
		}
	}
	for _, p := range []string{today, young, stray, notDay} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s went: %v", p, err)
		}
	}
	if res.Removed != 6 || res.Failed != 0 {
		t.Fatalf("real run: %+v\n%s", res, strings.Join(lines, "\n"))
	}
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "removed kind=day path="+filepath.Dir(emptied)+" age=") && strings.HasSuffix(l, " bytes=0") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no day line for the emptied folder:\n%s", strings.Join(lines, "\n"))
	}

	// Another user's empty folder is not owned.
	again := mk(filepath.Join(base, "jobs", "260804"))
	_, res = runAs(t, Options{PrivateRoots: []string{base}, SharedRoot: "none"}, os.Getuid()+1, true)
	if res.NotOwned != 1 || res.Removed != 0 {
		t.Fatalf("another user: %+v", res)
	}
	if _, err := os.Stat(again); err != nil {
		t.Fatal(err)
	}
}

// TestOrphansAndStaleSnapshots covers orphans and stale snapshots: a job folder
// without a summary, or whose summary names no final status (the one the
// job's acceptance wrote), is an orphan and goes under the age rule alone when its
// day folder is older than the retention age, nothing in it is newer than
// the cutoff, and its snapshot, if any, is terminal or older than the age;
// a non-terminal snapshot older than the age is a candidate itself; neither
// is taken under pressure.
func TestOrphansAndStaleSnapshots(t *testing.T) {
	base := t.TempDir()
	score := filepath.Join(base, "score")
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	jobs := filepath.Join(base, "jobs")
	accepted, _ := json.Marshal(records.AcceptedSummary{SchemaVersion: records.JobSchemaVersion, Devices: []records.SummaryDevice{{Name: "r1"}}})
	orphan := func(day, id string, at time.Time, files ...string) string {
		dir := filepath.Join(jobs, day, id)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		for _, name := range append([]string{"commands.jsonl", "manifest.json"}, files...) {
			content := []byte("{}\n")
			if name == "summary.json" {
				content = accepted
			}
			if err := os.WriteFile(filepath.Join(dir, name), content, 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(filepath.Join(dir, name), at, at); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chtimes(dir, at, at); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	aged := orphan("260810", "260810-090000-00", old)                     // no snapshot: goes
	notFinal := orphan("260810", "260810-090001-00", old, "summary.json") // a summary without a final status: goes
	byChoice := orphan("260817", "260817-153859-00", old)                 // a completed snapshot: goes
	writeScoreboard(t, score, "260817-153859-00", "complete", old)
	cutShort := orphan("260817", "260817-153900-00", old) // a running snapshot older than the age: goes, and so does the snapshot
	staleSnap := writeScoreboard(t, score, "260817-153900-00", "running", old)
	held := orphan("260817", "260817-153901-00", old) // a running snapshot updated lately: stays
	liveSnap := writeScoreboard(t, score, "260817-153901-00", "running", now.Add(-time.Minute))
	touched := orphan("260817", "260817-153902-00", old) // a file newer than the cutoff: stays
	if err := os.Chtimes(filepath.Join(touched, "manifest.json"), now, now); err != nil {
		t.Fatal(err)
	}
	today := orphan("260926", "260926-153859-00", now.Add(-time.Hour))                   // the day's own folder: stays
	running := orphan("260926", "260926-153900-00", now.Add(-time.Hour), "summary.json") // a running job: stays

	lines, res := run(t, Options{PrivateRoots: []string{base}, ScoreboardRoot: score})
	for _, p := range []string{aged, notFinal, byChoice, cutShort, staleSnap} {
		if !has(lines, p) {
			t.Fatalf("%s is not a candidate:\n%s", p, strings.Join(lines, "\n"))
		}
	}
	for _, p := range []string{held, liveSnap, touched, today, running} {
		if has(lines, p) {
			t.Fatalf("%s is a candidate:\n%s", p, strings.Join(lines, "\n"))
		}
	}
	kinds := map[string]int{}
	for _, l := range lines {
		if f := strings.Fields(l); len(f) > 1 {
			kinds[f[1]]++
		}
	}
	if kinds["kind=orphan"] != 4 || kinds["kind=stale-scoreboard"] != 1 || kinds["kind=scoreboard"] != 1 {
		t.Fatalf("kinds %v\n%s", kinds, strings.Join(lines, "\n"))
	}
	if res.Removed != 6 {
		t.Fatalf("result %+v", res)
	}

	// Under pressure (a floor no filesystem meets) a young finished job goes
	// and a young orphan or a young non-terminal snapshot does not.
	young := writeJob(t, jobs, "260926", "260926-160000-00", "complete", now.Add(-time.Hour))
	lines, _ = run(t, Options{PrivateRoots: []string{base}, ScoreboardRoot: score, MinFreePercent: 100})
	if !has(lines, young) {
		t.Fatalf("pressure did not take the young finished job:\n%s", strings.Join(lines, "\n"))
	}
	for _, p := range []string{today, running, liveSnap, held, touched} {
		if has(lines, p) {
			t.Fatalf("pressure took %s:\n%s", p, strings.Join(lines, "\n"))
		}
	}
}

// TestReportForm covers the report's text form: ages in days, hours, or
// minutes; the transcript line naming both files without a NUL; bytes=;
// --verbose adding a kept line with its reason for a young job, a folder
// that is not a day folder, a live orphan, a live snapshot, and another
// user's job.
func TestReportForm(t *testing.T) {
	for d, want := range map[time.Duration]string{40 * 24 * time.Hour: "40d", 24 * time.Hour: "1d", 23*time.Hour + 59*time.Minute: "23h", 59 * time.Minute: "59m", 0: "0m"} {
		if got := ageString(d); got != want {
			t.Fatalf("ageString(%s) = %s, want %s", d, got, want)
		}
	}
	base := t.TempDir()
	score := filepath.Join(base, "score")
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	jobs := filepath.Join(base, "jobs")
	oldJob := writeJob(t, jobs, "260817", "260817-153859-00", "complete", old)
	young := writeJob(t, jobs, "260926", "260926-153859-00", "complete", now.Add(-time.Hour))
	legacy := writeJob(t, jobs, "2026-08-17", "260817-100000-00", "complete", old)
	meta := writeTranscript(t, filepath.Join(base, "transcripts"), "260817", "fake-iosxe-153900", old)
	liveOrphan := filepath.Join(jobs, "260817", "260817-153900-00")
	if err := os.MkdirAll(liveOrphan, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(liveOrphan, old, old); err != nil {
		t.Fatal(err)
	}
	liveSnap := writeScoreboard(t, score, "260817-153900-00", "running", now.Add(-time.Minute))

	lines, _ := runAs(t, Options{PrivateRoots: []string{base}, ScoreboardRoot: score, Verbose: true}, os.Getuid(), true)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"would-remove kind=activity path=" + oldJob + " age=40d bytes=",
		"would-remove kind=transcript path=" + meta + " transcript=" + strings.TrimSuffix(meta, ".meta.jsonl") + ".log age=40d bytes=",
		"kept kind=activity path=" + young + " reason=young",
		"kept kind=activity path=" + filepath.Dir(legacy) + " reason=not-a-day-folder",
		"kept kind=orphan path=" + liveOrphan + " reason=live",
		"kept kind=scoreboard path=" + liveSnap + " reason=live",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("no line %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "\x00") || strings.Contains(joined, "bytes_estimated") {
		t.Fatalf("the old form:\n%s", joined)
	}
	lines, _ = runAs(t, Options{PrivateRoots: []string{base}, ScoreboardRoot: score, Verbose: true}, os.Getuid()+1, true)
	if !strings.Contains(strings.Join(lines, "\n"), "kept kind=activity path="+oldJob+" reason=not-owned") {
		t.Fatalf("no not-owned line:\n%s", strings.Join(lines, "\n"))
	}
	// Without --verbose no kept line.
	lines, _ = run(t, Options{PrivateRoots: []string{base}, ScoreboardRoot: score})
	for _, l := range lines {
		if strings.HasPrefix(l, "kept ") {
			t.Fatalf("a kept line without --verbose: %s", l)
		}
	}
}

// TestReportJSONL covers the report's jsonl form: --format jsonl writes the
// same lines as JSON documents, the event word under "event", the age as
// "age_days", an error as its text, and the summary as the last document.
func TestReportJSONL(t *testing.T) {
	base := t.TempDir()
	score := filepath.Join(base, "score")
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	jobs := filepath.Join(base, "jobs")
	oldJob := writeJob(t, jobs, "260817", "260817-153859-00", "complete", old)
	young := writeJob(t, jobs, "260926", "260926-153859-00", "complete", now.Add(-time.Hour))
	meta := writeTranscript(t, filepath.Join(base, "transcripts"), "260817", "fake-iosxe-153900", old)
	opts := Options{PrivateRoots: []string{base}, ScoreboardRoot: score, Verbose: true, Format: FormatJSONL, DryRun: true}
	lines, result := runAs(t, opts, os.Getuid(), true)
	var buf strings.Builder
	Summary(&buf, opts, result)
	lines = append(lines, strings.TrimSuffix(buf.String(), "\n"))
	docs := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		var doc map[string]any
		if err := json.Unmarshal([]byte(l), &doc); err != nil {
			t.Fatalf("not a JSON document: %q: %v", l, err)
		}
		docs = append(docs, doc)
	}
	find := func(event, path string) map[string]any {
		for _, d := range docs {
			if d["event"] == event && d["path"] == path {
				return d
			}
		}
		t.Fatalf("no document %s %s in:\n%s", event, path, strings.Join(lines, "\n"))
		return nil
	}
	if d := find("would-remove", oldJob); d["kind"] != "activity" || d["age_days"] != float64(40) || d["bytes"] == nil || d["age"] != nil {
		t.Fatalf("the old job's document: %v", d)
	}
	if d := find("would-remove", meta); d["kind"] != "transcript" || d["transcript"] != strings.TrimSuffix(meta, ".meta.jsonl")+".log" {
		t.Fatalf("the transcript's document: %v", d)
	}
	if d := find("kept", young); d["reason"] != "young" {
		t.Fatalf("the young job's document: %v", d)
	}
	if d := find("walk", jobs); d["kind"] != "activity" {
		t.Fatalf("the walk document: %v", d)
	}
	last := docs[len(docs)-1]
	if last["event"] != "summary" || last["examined"] != float64(result.Examined) || last["removed"] != float64(2) || last["dry_run"] != true {
		t.Fatalf("the summary document: %v", last)
	}
	// The text summary keeps its shape, no event word.
	buf.Reset()
	Summary(&buf, Options{DryRun: true}, Result{Examined: 3, Removed: 1, BytesEstimated: 7})
	if buf.String() != "examined=3 removed=1 skipped=0 not_owned=0 failed=0 bytes=7 dry_run=true\n" {
		t.Fatalf("the text summary: %q", buf.String())
	}
	// A skipped tree's error is the error's text.
	buf.Reset()
	report(&buf, Options{Format: FormatJSONL}, "skipped", f("kind", "tree"), f("path", "/x"), f("error", os.ErrPermission))
	if !strings.Contains(buf.String(), `"error":"permission denied"`) {
		t.Fatalf("the error field: %q", buf.String())
	}
}

// TestSiteRoots covers the site's roots: a root run walks the site's
// provisioned private roots in place of its own basedir, then the shared
// trees, then each root's scoreboards; --verbose lists the places walked
// in sequence; an old scoreboard under a private root goes.
func TestSiteRoots(t *testing.T) {
	site := t.TempDir()
	shared := t.TempDir()
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	u1 := filepath.Join(site, "users", "1000")
	u2 := filepath.Join(site, "users", "1001")
	j1 := writeJob(t, filepath.Join(u1, "jobs"), "260817", "260817-153859-00", "complete", old)
	m2 := writeTranscript(t, filepath.Join(u2, "transcripts"), "260817", "fake-iosxe-153900", old)
	js := writeJob(t, filepath.Join(shared, "jobs"), "260817", "260817-153859-01", "complete", old)
	if err := os.MkdirAll(filepath.Join(shared, "transcripts"), 0o770); err != nil {
		t.Fatal(err)
	}
	b2 := writeScoreboard(t, filepath.Join(u2, "state", "scoreboards"), "260817-153900-00", "complete", old)

	lines, res := runAs(t, Options{PrivateRoots: []string{u1, u2}, SharedRoot: shared, Verbose: true}, 0, true)
	for _, p := range []string{j1, m2, js, b2} {
		if !has(lines, p) {
			t.Fatalf("%s is not a candidate:\n%s", p, strings.Join(lines, "\n"))
		}
	}
	if res.Removed != 4 {
		t.Fatalf("result %+v", res)
	}
	var walks []string
	for _, l := range lines {
		if strings.HasPrefix(l, "walk ") {
			walks = append(walks, l)
		}
	}
	want := []string{
		"walk kind=activity path=" + filepath.Join(u1, "jobs"),
		"walk kind=activity path=" + filepath.Join(u2, "jobs"),
		"walk kind=activity path=" + filepath.Join(shared, "jobs"),
		"walk kind=transcript path=" + filepath.Join(u1, "transcripts"),
		"walk kind=transcript path=" + filepath.Join(u2, "transcripts"),
		"walk kind=transcript path=" + filepath.Join(shared, "transcripts"),
		"walk kind=scoreboard path=" + filepath.Join(u1, "state", "scoreboards"),
		"walk kind=scoreboard path=" + filepath.Join(u2, "state", "scoreboards"),
	}
	if strings.Join(walks, "\n") != strings.Join(want, "\n") {
		t.Fatalf("walks:\n%s\nwant:\n%s", strings.Join(walks, "\n"), strings.Join(want, "\n"))
	}
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

// TestLongRetentionKeepsYoungWork covers the cutoff at the longest
// retention ages: it lies that many calendar days back, never wrapping into
// the future, so a job that ended an hour ago, a running job's folder, and
// its live snapshot stay.
func TestLongRetentionKeepsYoungWork(t *testing.T) {
	base := t.TempDir()
	score := filepath.Join(base, "score")
	jobs := filepath.Join(base, "jobs")
	young := writeJob(t, jobs, "260926", "260926-150000-00", "complete", now.Add(-time.Hour))
	running := filepath.Join(jobs, "260926", "260926-150001-00")
	if err := os.MkdirAll(running, 0o750); err != nil {
		t.Fatal(err)
	}
	snapshot := writeScoreboard(t, score, "260926-150001-00", "running", now.Add(-time.Minute))
	for _, days := range []int{999, 106752, 999999} {
		lines, res := run(t, Options{PrivateRoots: []string{base}, ScoreboardRoot: score, Days: days})
		for _, p := range []string{young, running, snapshot} {
			if has(lines, p) {
				t.Errorf("--days %d: %s is a candidate:\n%s", days, p, strings.Join(lines, "\n"))
			}
		}
		if res.Removed != 0 {
			t.Errorf("--days %d: %+v", days, res)
		}
	}
}

// TestUnreadableRootFailsTheTree covers a tree that cannot be walked: a
// root that cannot be read is a failed line and a count, so the exit says
// the tree is not as the policy wants it; a root that is absent is passed by
// without a line.
func TestUnreadableRootFailsTheTree(t *testing.T) {
	base := t.TempDir()
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	jobs := filepath.Join(base, "jobs")
	if err := os.MkdirAll(jobs, 0o750); err != nil {
		t.Fatal(err)
	}
	meta := writeTranscript(t, filepath.Join(base, "transcripts"), "260817", "fake-iosxe-153900", old)
	unreadable(t, jobs)
	lines, res := run(t, Options{PrivateRoots: []string{base}})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "failed kind=tree path="+jobs+" error=") || res.Failed != 1 || res.Skipped != 0 {
		t.Fatalf("the unreadable root: %+v\n%s", res, joined)
	}
	// The other tree is walked all the same.
	if !has(lines, meta) {
		t.Fatalf("the transcript tree was not walked:\n%s", joined)
	}
}

// TestUnreadableFolderIsPassedBy covers a folder below a root that cannot
// be read: a day folder or a job folder of the job tree, a day folder of
// the transcript tree. Each is a skipped line with its error and a count,
// and the walk goes on, so the tree's other old items go, and the day
// folders they leave empty (two here).
func TestUnreadableFolderIsPassedBy(t *testing.T) {
	base := t.TempDir()
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	jobs := filepath.Join(base, "jobs")
	transcripts := filepath.Join(base, "transcripts")
	closedDay := filepath.Join(jobs, "260816")
	closedJob := writeJob(t, jobs, "260817", "260817-153858-00", "complete", old)
	oldJob := writeJob(t, jobs, "260817", "260817-153859-00", "complete", old)
	laterJob := writeJob(t, jobs, "260818", "260818-153859-00", "complete", old.Add(24*time.Hour))
	closedTDay := filepath.Join(transcripts, "260816")
	oldMeta := writeTranscript(t, transcripts, "260817", "fake-iosxe-153900", old)
	for _, p := range []string{closedDay, closedTDay} {
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{closedDay, closedJob, closedTDay} {
		unreadable(t, p)
	}
	lines, res := runAs(t, Options{PrivateRoots: []string{base}}, os.Getuid(), false)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"skipped kind=activity path=" + closedDay + " error=",
		"skipped kind=activity path=" + closedJob + " error=",
		"skipped kind=transcript path=" + closedTDay + " error=",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("no line %q in:\n%s", want, joined)
		}
	}
	for _, p := range []string{oldJob, laterJob, oldMeta} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s stayed:\n%s", p, joined)
		}
	}
	if res.Failed != 0 || res.Skipped != 3 || res.Removed != 5 {
		t.Errorf("result %+v\n%s", res, joined)
	}
}

// TestDayFoldersInTheirZone covers the day folders' zone: a folder's name
// is a date in opts.Location, and an empty one goes only when the whole day
// it names ended before the cutoff. At 19:30 in Chicago on 2026-10-31
// (00:30 UTC the next day) with one day's retention, today's and
// yesterday's empty folders stay and the one of two days back goes; an
// orphan in today's folder whose files are older than the cutoff stays,
// young by its day in the zone.
func TestDayFoldersInTheirZone(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	jobs := filepath.Join(base, "jobs")
	mk := func(p string) string {
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
		return p
	}
	before := mk(filepath.Join(jobs, "261029"))
	yesterday := mk(filepath.Join(jobs, "261030"))
	today := mk(filepath.Join(base, "transcripts", "261031"))
	orphan := mk(filepath.Join(jobs, "261031", "261031-120000-00"))
	stale := time.Date(2026, 10, 1, 12, 0, 0, 0, chicago)
	if err := os.Chtimes(orphan, stale, stale); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	res, err := Run(Options{PrivateRoots: []string{base}, Days: 1, DryRun: true, UID: os.Getuid(), Now: time.Date(2026, 10, 31, 19, 30, 0, 0, chicago), Location: chicago}, &out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if !has(lines, before) {
		t.Errorf("the folder of two days back is not a candidate:\n%s", out.String())
	}
	for _, p := range []string{yesterday, today, orphan} {
		if has(lines, p) {
			t.Errorf("%s is a candidate:\n%s", p, out.String())
		}
	}
	if res.Removed != 1 {
		t.Errorf("result %+v\n%s", res, out.String())
	}
}

// TestSharedScoreboardsJudgedAsSharedTrees covers the shared scoreboards:
// judged as the shared jobs and transcripts trees are, a folder the
// operator cannot use or a link is a skipped line with its reason and is
// passed by, not a failure; the private root's scoreboards are walked, and
// a private one that cannot be read fails its tree.
func TestSharedScoreboardsJudgedAsSharedTrees(t *testing.T) {
	base := t.TempDir()
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	private := filepath.Join(base, "state", "scoreboards")
	privateOld := writeScoreboard(t, private, "260817-153859-00", "complete", old)
	shared := filepath.Join(base, "shared-scoreboards")
	writeScoreboard(t, shared, "260817-153900-00", "complete", old)
	unreadable(t, shared)
	lines, res := run(t, Options{PrivateRoots: []string{base}, ScoreboardRoot: shared, Verbose: true})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "skipped kind=tree path="+shared+" error=scoreboard_root_invalid: the shared scoreboards tree "+shared+" exists and the operator cannot create files in it") || res.Failed != 0 {
		t.Fatalf("the shared scoreboards the operator cannot use: %+v\n%s", res, joined)
	}
	if strings.Contains(joined, "walk kind=scoreboard path="+shared+"\n") || !has(lines, privateOld) {
		t.Fatalf("the shared folder was walked, or the private one not:\n%s", joined)
	}

	link := filepath.Join(base, "linked-scoreboards")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	lines, res = run(t, Options{PrivateRoots: []string{t.TempDir()}, ScoreboardRoot: link})
	joined = strings.Join(lines, "\n")
	if !strings.Contains(joined, "skipped kind=tree path="+link+" error=scoreboard_root_invalid: the shared scoreboards tree "+link+" exists and is not a real directory") || has(lines, privateOld) || res.Failed != 0 {
		t.Fatalf("the shared scoreboards a link: %+v\n%s", res, joined)
	}

	// An absent shared folder: not walked, no line.
	lines, res = run(t, Options{PrivateRoots: []string{t.TempDir()}, ScoreboardRoot: filepath.Join(base, "absent")})
	if len(lines) != 1 || lines[0] != "" || res.Failed != 0 {
		t.Fatalf("the shared scoreboards absent: %+v\n%s", res, strings.Join(lines, "\n"))
	}

	unreadable(t, private)
	lines, res = run(t, Options{PrivateRoots: []string{base}})
	if !strings.Contains(strings.Join(lines, "\n"), "failed kind=tree path="+private+" error=") || res.Failed != 1 {
		t.Fatalf("the private scoreboards unreadable: %+v\n%s", res, strings.Join(lines, "\n"))
	}
}

// TestTranscriptOfADeviceNamedWithMeta: a device name holding ".meta."
// pairs the session's transcript with its metadata, and the transcript is
// never examined as a metadata file: a real run removes both.
func TestTranscriptOfADeviceNamedWithMeta(t *testing.T) {
	base := t.TempDir()
	old := time.Date(2026, 8, 17, 15, 38, 59, 0, time.UTC)
	meta := writeTranscript(t, filepath.Join(base, "transcripts"), "260817", "router1.meta.example.net-153900", old)
	log := strings.TrimSuffix(meta, ".meta.jsonl") + ".log"
	lines, res := runAs(t, Options{PrivateRoots: []string{base}, Verbose: true}, os.Getuid(), false)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "removed kind=transcript path="+meta+" transcript="+log+" ") || strings.Contains(joined, "path="+log) {
		t.Fatalf("the pair:\n%s", joined)
	}
	if res.Examined != 1 {
		t.Fatalf("result %+v", res)
	}
	for _, p := range []string{meta, log} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s remains: %v", p, err)
		}
	}
}
