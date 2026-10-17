package app

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// listTree is an output root of job folders, each with the summary given
// ("" for none), and the options to list it with.
type listTree struct {
	t    *testing.T
	root string
	cfg  configload.Snapshot
	me   *user.User
}

func newListTree(t *testing.T) *listTree {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "jobs")
	cfg, err := configload.Load(configload.Options{HomeDir: dir, SkipAuto: true, Environment: []string{}, Sets: []string{`basedir="` + filepath.Join(dir, "base") + `"`, `sharedroot="none"`, `output.root="` + root + `"`}})
	if err != nil {
		t.Fatal(err)
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	return &listTree{t: t, root: root, cfg: cfg, me: me}
}

// job writes a job folder under its day; summary is the file's text.
func (l *listTree) job(id, summary string) string {
	l.t.Helper()
	dir := filepath.Join(l.root, id[:6], id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		l.t.Fatal(err)
	}
	if summary != "" {
		if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte(summary), 0o640); err != nil {
			l.t.Fatal(err)
		}
	}
	return dir
}

func (l *listTree) list(o JobListOptions) []JobListEntry {
	l.t.Helper()
	entries, err := ListJobs(CommonOptions{Config: l.cfg, Operator: credentials.Operator{Username: l.me.Username, Home: l.t.TempDir()}}, o)
	if err != nil {
		l.t.Fatal(err)
	}
	return entries
}

// finished is a finished job's summary; devices are name:outcome pairs.
func finished(id, kind, status string, exit int, devices ...string) string {
	var list []string
	for _, d := range devices {
		name, outcome, _ := strings.Cut(d, ":")
		list = append(list, `{"name":"`+name+`","outcome":"`+outcome+`"}`)
	}
	return `{"schema_version":5,"job_id":"` + id + `","kind":"` + kind + `","devices":[` + strings.Join(list, ",") + `],"final_status":"` + status + `","primary_exit_code":` + strconv.Itoa(exit) + `,"device_counts":{"total":` + strconv.Itoa(len(devices)) + `}}`
}

// unfinished is an accepted summary: devices without their outcomes.
func unfinished(id string, devices ...string) string {
	var list []string
	for _, d := range devices {
		list = append(list, `{"name":"`+d+`"}`)
	}
	return `{"schema_version":5,"job_id":"` + id + `","kind":"run","devices":[` + strings.Join(list, ",") + `]}`
}

func ids(entries []JobListEntry) string {
	var out []string
	for _, e := range entries {
		id := e.JobID
		if e.Device != nil {
			id += "/" + e.Device.Name
		}
		if e.Err != nil {
			id = "?" + id
		}
		out = append(out, id)
	}
	return strings.Join(out, " ")
}

// TestListJobsOrderAndCount: day folders newest first and each one's jobs
// in job-ID order, newest first, whatever was written when; a name that is
// not a day folder or a job ID, a file, and a link are passed over; --last
// is the count, 0 every job; a root not yet made lists nothing (EXAMPLES
// chapter 61, issue 1).
func TestListJobsOrderAndCount(t *testing.T) {
	l := newListTree(t)
	if got := l.list(JobListOptions{Last: 20}); got == nil || len(got) != 0 {
		t.Fatalf("a root not yet made: %v", got)
	}
	l.job("261016-091500-00", finished("261016-091500-00", "run", "complete", 0, "a:success"))
	l.job("261015-151448-00", finished("261015-151448-00", "cmd", "complete", 0, "a:success"))
	l.job("261015-151533-00", finished("261015-151533-00", "crun", "complete", 0, "a:success"))
	l.job("261015-151449-01", finished("261015-151449-01", "run", "error", 101, "a:success", "b:fail"))
	l.job("261015-151449-00", "")
	os.MkdirAll(filepath.Join(l.root, "notaday", "261015-151449-02"), 0o700)
	os.MkdirAll(filepath.Join(l.root, "261015", "scratch"), 0o700)
	os.WriteFile(filepath.Join(l.root, "261015", "261015-151450-00"), []byte("{}"), 0o640)
	os.Symlink(filepath.Join(l.root, "261015", "261015-151448-00"), filepath.Join(l.root, "261015", "261015-151451-00"))
	all := "261016-091500-00 261015-151533-00 261015-151449-01 261015-151449-00 261015-151448-00"
	if got := ids(l.list(JobListOptions{})); got != all {
		t.Errorf("--last 0: %s, want %s", got, all)
	}
	if got := ids(l.list(JobListOptions{Last: 2})); got != "261016-091500-00 261015-151533-00" {
		t.Errorf("--last 2: %s", got)
	}
	got := l.list(JobListOptions{})
	if e := got[3]; e.Finished || e.Err != nil || !e.NoSummary || e.Summary.JobID != "261015-151449-00" || e.Operator.Username != l.me.Username {
		t.Errorf("a folder without a summary: %+v", e)
	}
	// It reached no device: --target leaves it out.
	if s := ids(l.list(JobListOptions{Targets: []string{"*"}})); strings.Contains(s, "261015-151449-00") {
		t.Errorf("--target * keeps the folder without a summary: %s", s)
	}
	if e := got[2]; e.NoSummary || !e.Finished || e.Summary.Kind != "run" || e.Summary.ExitCode != 101 || e.Path != filepath.Join(l.root, "261015", "261015-151449-01") {
		t.Errorf("the errored run: %+v", e)
	}
}

// TestListJobsFilters: --failed keeps the finished jobs whose exit is not
// 0; --target is run's over each job's devices, a row per device left and
// --last counting rows, "!" removing, and refused with no value that
// selects; --operator keeps a name's or a UID's jobs; every filter judges a
// job before the count (EXAMPLES chapter 61, issues 2 and 4).
func TestListJobsFilters(t *testing.T) {
	l := newListTree(t)
	l.job("261016-003707-00", unfinished("261016-003707-00", "fake-iosxe", "dead-iosxe"))
	l.job("261016-002853-00", finished("261016-002853-00", "run", "error", 101, "fake-iosxe:success", "dead-iosxe:fail"))
	l.job("261016-002800-00", finished("261016-002800-00", "run", "cancel", 113, "core-1:cancel"))
	l.job("261016-002700-00", finished("261016-002700-00", "cmd", "complete", 0, "dead-iosxe:success"))
	if got := ids(l.list(JobListOptions{Failed: true})); got != "261016-002853-00 261016-002800-00" {
		t.Errorf("--failed: %s", got)
	}
	if got := ids(l.list(JobListOptions{Failed: true, Last: 1})); got != "261016-002853-00" {
		t.Errorf("--failed --last 1: %s", got)
	}
	if got := ids(l.list(JobListOptions{Targets: []string{"DEAD*"}})); got != "261016-003707-00/dead-iosxe 261016-002853-00/dead-iosxe 261016-002700-00/dead-iosxe" {
		t.Errorf("--target DEAD*: %s", got)
	}
	rows := l.list(JobListOptions{Targets: []string{"*-iosxe", "!dead*"}})
	if got := ids(rows); got != "261016-003707-00/fake-iosxe 261016-002853-00/fake-iosxe" {
		t.Errorf("--target *-iosxe --target !dead*: %s", got)
	}
	if rows[0].Device.Outcome != "" || rows[1].Device.Outcome != "success" {
		t.Errorf("outcomes: %+v %+v", rows[0].Device, rows[1].Device)
	}
	if got := ids(l.list(JobListOptions{Targets: []string{"*"}, Last: 3})); got != "261016-003707-00/fake-iosxe 261016-003707-00/dead-iosxe 261016-002853-00/fake-iosxe" {
		t.Errorf("--target * --last 3 counts rows: %s", got)
	}
	if got := ids(l.list(JobListOptions{Targets: []string{"core-1"}, Failed: true})); got != "261016-002800-00/core-1" {
		t.Errorf("--target core-1 --failed: %s", got)
	}
	_, err := ListJobs(CommonOptions{Config: l.cfg}, JobListOptions{Targets: []string{"!dead*"}})
	if errorcodes.Of(err) != "inventory_positive_selector_missing" || CheckJobListTargets([]string{"!a", "!b"}) == nil || CheckJobListTargets(nil) != nil {
		t.Errorf("only ! values: %v", err)
	}
	if got := ids(l.list(JobListOptions{Operators: []string{"nobody-at-all"}})); got != "" {
		t.Errorf("--operator nobody: %s", got)
	}
	if got := ids(l.list(JobListOptions{Operators: []string{"nobody-at-all", l.me.Uid}, Last: 2})); got != "261016-003707-00 261016-002853-00" {
		t.Errorf("--operator UID: %s", got)
	}
	if got := ids(l.list(JobListOptions{Operators: []string{l.me.Username}})); len(strings.Fields(got)) != 4 {
		t.Errorf("--operator %s: %s", l.me.Username, got)
	}
}

// TestListJobsUnreadable: a summary of mode 000, one cut short, and a
// folder of mode 000 are ? rows naming the owner, of the summary or of the
// folder where the summary cannot be reached; --failed and --target keep
// them, --operator judges them (EXAMPLES chapter 61, issue 4).
func TestListJobsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	l := newListTree(t)
	a := l.job("261016-003305-00", finished("261016-003305-00", "run", "complete", 0, "a:success"))
	b := l.job("261016-003307-00", `{"schema_version":5,"job_id":`)
	c := l.job("261016-003309-00", finished("261016-003309-00", "run", "complete", 0, "a:success"))
	l.job("261016-003000-00", finished("261016-003000-00", "run", "complete", 0, "a:success"))
	os.Chmod(filepath.Join(a, "summary.json"), 0)
	os.Chmod(c, 0)
	t.Cleanup(func() { os.Chmod(c, 0o700) })
	got := l.list(JobListOptions{})
	if s := ids(got); s != "?261016-003309-00 ?261016-003307-00 ?261016-003305-00 261016-003000-00" {
		t.Fatalf("rows: %s", s)
	}
	for i, code := range []string{"run_output_records_unreadable", "run_output_record_decode_failed", "run_output_records_unreadable"} {
		e := got[i]
		if errorcodes.Of(e.Err) != code || e.Operator.Username != l.me.Username || strconv.Itoa(e.Operator.UID) != l.me.Uid {
			t.Errorf("row %d: %v, operator %+v", i, e.Err, e.Operator)
		}
	}
	if got[0].Path != c || got[1].SummaryPath != filepath.Join(b, "summary.json") {
		t.Errorf("paths: %s %s", got[0].Path, got[1].SummaryPath)
	}
	if s := ids(l.list(JobListOptions{Failed: true, Targets: []string{"zzz"}})); s != "?261016-003309-00 ?261016-003307-00 ?261016-003305-00" {
		t.Errorf("--failed --target zzz: %s", s)
	}
	if s := ids(l.list(JobListOptions{Operators: []string{"nobody-at-all"}})); s != "" {
		t.Errorf("--operator nobody: %s", s)
	}
}

// TestJobListEntryJSON: a row is the summary's fields as the file holds
// them, in its order, then path and operator {username, uid}; under
// --target device and its outcome, none for an unfinished job's; a ? row
// path, operator, and error; a folder without a summary path and operator
// (EXAMPLES chapter 61, issue 3).
func TestJobListEntryJSON(t *testing.T) {
	l := newListTree(t)
	l.job("261016-003707-00", unfinished("261016-003707-00", "fake-iosxe"))
	l.job("261016-002853-00", "{\n  \"schema_version\": 5,\n  \"job_id\": \"261016-002853-00\",\n  \"kind\": \"run\",\n  \"devices\": [{\"name\": \"fake-iosxe\", \"outcome\": \"fail\"}],\n  \"final_status\": \"error\",\n  \"primary_exit_code\": 101\n}\n")
	l.job("261016-002800-00", "")
	me := `"operator":{"username":"` + l.me.Username + `","uid":` + l.me.Uid + `}`
	b, err := json.Marshal(l.list(JobListOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	dir := func(id string) string { return filepath.Join(l.root, "261016", id) }
	want := `[{"schema_version":5,"job_id":"261016-003707-00","kind":"run","devices":[{"name":"fake-iosxe"}],"path":"` + dir("261016-003707-00") + `",` + me + `},` +
		`{"schema_version":5,"job_id":"261016-002853-00","kind":"run","devices":[{"name":"fake-iosxe","outcome":"fail"}],"final_status":"error","primary_exit_code":101,"path":"` + dir("261016-002853-00") + `",` + me + `},` +
		`{"path":"` + dir("261016-002800-00") + `",` + me + `}]`
	if string(b) != want {
		t.Errorf("rows:\n%s\nwant\n%s", b, want)
	}
	b, _ = json.Marshal(l.list(JobListOptions{Targets: []string{"fake*"}, Last: 2}))
	if s := string(b); !strings.Contains(s, me+`,"device":"fake-iosxe"}`) || !strings.Contains(s, me+`,"device":"fake-iosxe","outcome":"fail"}`) {
		t.Errorf("device rows: %s", s)
	}
	e := JobListEntry{Path: "/jobs/x", Operator: l.list(JobListOptions{Last: 1})[0].Operator, Err: errorcodes.Errorf("run_output_records_unreadable", "read /jobs/x/summary.json: permission denied")}
	b, _ = json.Marshal(e)
	if want := `{"path":"/jobs/x",` + me + `,"error":"run_output_records_unreadable: read /jobs/x/summary.json: permission denied"}`; string(b) != want {
		t.Errorf("? row: %s, want %s", b, want)
	}
}
