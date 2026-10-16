package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

// job list: the jobs of the output root from their folders' summaries, read
// newest first, every filter judging a job before the count (EXAMPLES
// chapter 61). It needs no daemon and no credential.

// JobListOptions are job list's options: Last is the count, 0 every job
// kept; Failed, Targets (the --target values as typed), and Operators judge
// each job before it is counted.
type JobListOptions struct {
	Last      int
	Failed    bool
	Targets   []string
	Operators []string
}

// JobListEntry is one row of job list: a job, or under --target one of its
// devices. Err is set for a folder whose summary cannot be read or decoded,
// whose row is marked ?.
type JobListEntry struct {
	JobID string
	// Path is the job's folder; SummaryPath its summary.
	Path        string
	SummaryPath string
	// Operator is the owner of the summary, or of the folder where the
	// summary cannot be reached: the UID, and the user database's name for
	// it, "" where it names none.
	Operator records.Operator
	// Summary is the folder's summary, the one written at acceptance while
	// the job is unfinished; raw is the file as written, for the JSON.
	Summary  records.Summary
	Finished bool
	// NoSummary marks a job folder without a summary, a job cut short
	// before its summary's first write: unfinished, its kind and devices
	// not known, no device reached (EXAMPLES chapter 61, issue 5).
	NoSummary bool
	Err       error
	raw       []byte
	// Device is the row's device under --target.
	Device *records.SummaryDevice
}

// Name is the operator as job list shows it: the name, or the UID where the
// user database names none.
func (e JobListEntry) Name() string {
	if e.Operator.Username != "" {
		return e.Operator.Username
	}
	return strconv.Itoa(e.Operator.UID)
}

// MarshalJSON is the row of job list --format json: the summary's fields as
// the file holds them, then path, operator, and under --target device and
// its outcome (none for an unfinished job's); an unreadable folder's row is
// path, operator, and error.
func (e JobListEntry) MarshalJSON() ([]byte, error) {
	operator := records.Operator{Username: e.Operator.Username, UID: e.Operator.UID}
	if e.Err != nil {
		return json.Marshal(struct {
			Path     string           `json:"path"`
			Operator records.Operator `json:"operator"`
			Error    string           `json:"error"`
		}{e.Path, operator, e.Err.Error()})
	}
	extra, err := json.Marshal(struct {
		Path     string           `json:"path"`
		Operator records.Operator `json:"operator"`
		Device   string           `json:"device,omitempty"`
		Outcome  string           `json:"outcome,omitempty"`
	}{e.Path, operator, e.deviceName(), e.outcome()})
	if err != nil {
		return nil, err
	}
	var fields bytes.Buffer
	if err := json.Compact(&fields, e.raw); err != nil || fields.Len() <= 2 {
		// A folder without a summary: its row is the folder's alone.
		return extra, nil
	}
	// The summary's object, its closing brace replaced by the row's fields.
	b := fields.Bytes()
	return append(append(b[:len(b)-1], ','), extra[1:]...), nil
}

func (e JobListEntry) deviceName() string {
	if e.Device == nil {
		return ""
	}
	return e.Device.Name
}

func (e JobListEntry) outcome() string {
	if e.Device == nil {
		return ""
	}
	return e.Device.Outcome
}

// jobTargets are job list's --target values as run takes them (EXAMPLES
// chapter 61, issue 2): a value that selects keeps a job's devices it
// matches, one beginning with "!" removes those it matches.
type jobTargets struct {
	keep, remove []matching.Pattern
}

// CheckJobListTargets refuses --target values of which none selects, as
// run refuses them, before any folder is read; a malformed selector was
// refused by the parser.
func CheckJobListTargets(values []string) error {
	_, err := compileJobTargets(values)
	return err
}

func compileJobTargets(values []string) (jobTargets, error) {
	var t jobTargets
	for _, v := range values {
		sel, err := parseSelector("--target", strings.ToLower(strings.TrimSpace(v)))
		if err != nil {
			return t, err
		}
		if sel.Negated {
			t.remove = append(t.remove, sel.Pattern)
		} else {
			t.keep = append(t.keep, sel.Pattern)
		}
	}
	if len(values) > 0 && len(t.keep) == 0 {
		return t, errorcodes.Errorf("inventory_positive_selector_missing", "job list --target requires a value that selects (a value beginning with \"!\" only removes devices)")
	}
	return t, nil
}

// devices are the job's devices the values leave: those a selecting value
// matches, less those a "!" value matches.
func (t jobTargets) devices(all []records.SummaryDevice) []records.SummaryDevice {
	var out []records.SummaryDevice
	for _, d := range all {
		if matchesAny(t.keep, d.Name) && !matchesAny(t.remove, d.Name) {
			out = append(out, d)
		}
	}
	return out
}

func matchesAny(patterns []matching.Pattern, name string) bool {
	for _, p := range patterns {
		if p.Match(name) {
			return true
		}
	}
	return false
}

// ListJobs walks the output root's day folders newest first and each one's
// job folders in job-ID order, newest first, judging each job by the
// filters and stopping once Last rows are kept (EXAMPLES chapter 61): in
// shared mode the shared tree, every member's jobs, in individual mode the
// operator's own. A folder whose summary cannot be read or decoded is kept
// under every filter but --operator, which judges it by its owner. An output
// root not yet made lists nothing.
func ListJobs(common CommonOptions, o JobListOptions) ([]JobListEntry, error) {
	targets, err := compileJobTargets(o.Targets)
	if err != nil {
		return nil, err
	}
	cfg, operator := common.Config, common.Operator
	// The list makes nothing: the base folder's path, as watch takes it.
	base, err := osutil.BaseDirPath(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return nil, errorcodes.Ensure(err, "base_directory_unavailable")
	}
	root, _, err := osutil.ResolveOutputTree(cfg.String("output.root"), cfg.String("sharedroot"), base, operator.Home)
	if err != nil {
		return nil, errorcodes.Ensure(err, "output_root_unavailable")
	}
	days, err := folders(root, osutil.IsDayFolder)
	if errors.Is(err, fs.ErrNotExist) {
		return []JobListEntry{}, nil
	}
	if err != nil {
		return nil, errorcodes.Errorf("output_root_unavailable", "read %s: %w", root, err)
	}
	w := listWalk{opts: o, targets: targets, names: map[uint32]string{}, rows: []JobListEntry{}}
	for _, day := range days {
		dir := filepath.Join(root, day)
		ids, err := folders(dir, executionplan.ValidJobID)
		if err != nil {
			// A day folder that cannot be read is one ? row: its jobs
			// cannot be told.
			if w.add(w.unreadable(dir, day, dir, errorcodes.Errorf("run_output_records_unreadable", "read %s: %w", dir, err))) {
				break
			}
			continue
		}
		done := false
		for _, id := range ids {
			if done = w.add(w.read(filepath.Join(dir, id), id)); done {
				break
			}
		}
		if done {
			break
		}
	}
	return w.rows, nil
}

// folders are dir's subfolders whose names pass valid, newest first: the
// names sort as their stamps, so the reverse of their order. A link is
// never followed.
func folders(dir string, valid func(string) bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && valid(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// listWalk is one walk's state: the options, the rows kept, and the owners
// named so far.
type listWalk struct {
	opts    JobListOptions
	targets jobTargets
	names   map[uint32]string
	rows    []JobListEntry
}

// add judges a job and keeps its rows, one or, under --target, one per
// device left; it says whether the count is reached.
func (w *listWalk) add(e JobListEntry) bool {
	if !w.judged(e) {
		return false
	}
	rows := []JobListEntry{e}
	if len(w.opts.Targets) > 0 && e.Err == nil {
		rows = rows[:0]
		for _, d := range w.targets.devices(e.Summary.Devices) {
			r := e
			r.Device = &d
			rows = append(rows, r)
		}
	}
	for _, r := range rows {
		w.rows = append(w.rows, r)
		if w.opts.Last > 0 && len(w.rows) == w.opts.Last {
			return true
		}
	}
	return false
}

// judged applies the filters to one job: --operator to every row, by its
// owner; --failed and --target to a readable one, the finished jobs whose
// exit is not 0 and the jobs a device of which the values leave.
func (w *listWalk) judged(e JobListEntry) bool {
	if len(w.opts.Operators) > 0 {
		found := false
		for _, name := range w.opts.Operators {
			if name == e.Operator.Username || name == strconv.Itoa(e.Operator.UID) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if e.Err != nil {
		return true
	}
	if w.opts.Failed && (!e.Finished || e.Summary.ExitCode == 0) {
		return false
	}
	if len(w.opts.Targets) > 0 && len(w.targets.devices(e.Summary.Devices)) == 0 {
		return false
	}
	return true
}

// read is one job folder's entry. A folder without a summary is a job cut
// short before its summary's first write, unfinished.
func (w *listWalk) read(dir, id string) JobListEntry {
	path := filepath.Join(dir, "summary.json")
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		e := JobListEntry{JobID: id, Path: dir, SummaryPath: path, Operator: w.owner(dir), NoSummary: true}
		e.Summary.JobID = id
		return e
	case err != nil:
		return w.unreadable(dir, id, path, errorcodes.Errorf("run_output_records_unreadable", "read %s: %w", path, err))
	}
	e := JobListEntry{JobID: id, Path: dir, SummaryPath: path, Operator: w.owner(path), raw: raw}
	if err := json.Unmarshal(raw, &e.Summary); err != nil {
		return w.unreadable(dir, id, path, errorcodes.Errorf("run_output_record_decode_failed", "decode %s: %w", path, err))
	}
	e.Finished = records.TerminalStatus(e.Summary.FinalStatus)
	return e
}

// unreadable is the ? row of a folder: its name, the file that could not
// be read, and the owner of that file, or of the folder where the file
// cannot be reached.
func (w *listWalk) unreadable(dir, name, path string, err error) JobListEntry {
	owner := w.owner(path)
	if owner.UID < 0 {
		owner = w.owner(dir)
	}
	return JobListEntry{JobID: name, Path: dir, SummaryPath: path, Operator: owner, Err: err}
}

// owner is the file's owner: its UID and the user database's name for it,
// looked up once per UID; UID -1 when the file cannot be reached.
func (w *listWalk) owner(path string) records.Operator {
	fi, err := os.Lstat(path)
	if err != nil {
		return records.Operator{UID: -1}
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return records.Operator{UID: -1}
	}
	name, seen := w.names[st.Uid]
	if !seen {
		if u, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10)); err == nil {
			name = u.Username
		}
		w.names[st.Uid] = name
	}
	return records.Operator{Username: name, UID: int(st.Uid)}
}
