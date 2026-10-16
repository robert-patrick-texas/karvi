package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/records"
)

// The job directory of a job no daemon of the caller's holds: derived from
// the job ID and the configuration, and followed from its files.

// CheckJobID refuses a string that is not a job ID with
// job_request_malformed, the code the daemon answers. job follow and job
// cancel check the ID with it before their read.
func CheckJobID(jobID string) error {
	if !executionplan.ValidJobID(jobID) {
		return errorcodes.Errorf("job_request_malformed", "%q is not a job ID (YYMMDD-HHMMSS-xx)", jobID)
	}
	return nil
}

// JobIDTime parses the instant a job ID's stamp names, read in loc, the
// effective timezone the stamp was written in; the day folder is that
// instant's date. A string that is not a job ID is refused by CheckJobID.
func JobIDTime(jobID string, loc *time.Location) (time.Time, error) {
	if err := CheckJobID(jobID); err != nil {
		return time.Time{}, err
	}
	return osutil.JobIDTime(jobID, loc)
}

// jobDirectoryFor is the path the runner wrote for jobID under cfg: the
// output root, the ID's instant in the effective timezone, the ID; with the
// root, and whether it is the shared jobs tree.
func jobDirectoryFor(cfg configload.Snapshot, base, home, jobID string) (dir, root string, shared bool, err error) {
	location, err := display.Location(cfg.String("timezone"))
	if err != nil {
		return "", "", false, err
	}
	accepted, err := JobIDTime(jobID, location)
	if err != nil {
		return "", "", false, err
	}
	root, shared, err = osutil.ResolveOutputTree(cfg.String("output.root"), cfg.String("sharedroot"), base, home)
	if err != nil {
		return "", "", false, errorcodes.Ensure(err, "output_root_unavailable")
	}
	return osutil.JobDirectory(root, jobID, accepted, location), root, shared, nil
}

// JobDirectoryFor derives the job directory under the invocation's
// configuration: the ID's instant in the effective timezone
// gives the day folder the runner most likely wrote.
func JobDirectoryFor(common CommonOptions, jobID string) (string, error) {
	cfg, operator := common.Config, common.Operator
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return "", errorcodes.Ensure(err, "base_directory_unavailable")
	}
	dir, _, _, err := jobDirectoryFor(cfg, base, operator.Home, jobID)
	return dir, err
}

// JobFolder is a job's folder as LocateJobDirectory finds it.
type JobFolder struct {
	// Path is the folder, or the derived path where nothing exists.
	Path  string
	Found bool
	// Group is the shared jobs tree's group when the output root is that
	// tree, where a job's files may be another member's (openChecked); -1
	// otherwise.
	Group int
}

// LocateJobDirectory finds the directory of a job the daemon does not hold
// (refined at implementation): the output root is derived
// from the configuration, and the job's directory is the day folder holding
// a subdirectory named by the job ID. The client reserves
// that directory under the day folder of the ID's own stamp,
// so the derived path is the one written; the glob across day folders
// remains for a site whose timezone changed since, where the stamp reads
// as another day. A malformed ID is job_request_malformed. The derived day
// folder is the path when nothing exists, so a caller can name where it
// looked.
func LocateJobDirectory(common CommonOptions, jobID string) (JobFolder, error) {
	cfg, operator := common.Config, common.Operator
	folder := JobFolder{Group: -1}
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return folder, errorcodes.Ensure(err, "base_directory_unavailable")
	}
	derived, root, shared, err := jobDirectoryFor(cfg, base, operator.Home, jobID)
	if err != nil {
		return folder, err
	}
	if fi, err := os.Stat(root); err == nil && shared {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			folder.Group = int(st.Gid)
		}
	}
	folder.Path = derived
	if _, statErr := os.Lstat(derived); statErr == nil {
		folder.Found = true
		return folder, nil
	}
	if matches, err := filepath.Glob(filepath.Join(root, "*", jobID)); err == nil && len(matches) > 0 {
		folder.Path, folder.Found = matches[0], true
	}
	return folder, nil
}

// JobDirectoryState is what the directory path found.
type JobDirectoryState int

const (
	// JobDirectoryAbsent: no directory; the job is unknown here.
	JobDirectoryAbsent JobDirectoryState = iota
	// JobDirectoryFinished: the directory's summary names a final status.
	JobDirectoryFinished
	// JobDirectoryUnfinished: the directory's summary names no final
	// status, or there is none (a job cut short before its first write):
	// the job runs, or its process is gone (FollowJobDirectory judges).
	JobDirectoryUnfinished
)

// InspectJobDirectory reports the directory's state from its summary's
// final status (EXAMPLES chapter 61, issue 2).
func InspectJobDirectory(dir string) (JobDirectoryState, error) {
	if _, err := os.Lstat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return JobDirectoryAbsent, nil
		}
		return JobDirectoryAbsent, errorcodes.Errorf("run_output_records_unreadable", "%s: %w", dir, err)
	}
	summary, err := readSummary(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return JobDirectoryUnfinished, nil
	case err != nil:
		return JobDirectoryAbsent, err
	case !records.TerminalStatus(summary.FinalStatus):
		return JobDirectoryUnfinished, nil
	}
	return JobDirectoryFinished, nil
}

// readSummary reads and decodes the directory's summary.json; a missing
// file is an error that wraps fs.ErrNotExist.
func readSummary(dir string) (records.Summary, error) {
	var summary records.Summary
	path := filepath.Join(dir, "summary.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return summary, errorcodes.Errorf("run_output_records_unreadable", "read %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		return summary, errorcodes.Errorf("run_output_record_decode_failed", "decode %s: %w", path, err)
	}
	return summary, nil
}

// openChecked opens a job's commands.jsonl without following links and
// requires a regular file, mode 0640 or stricter, owned by this process's
// UID or, in the shared jobs tree, standing in its group (group, -1 outside
// that tree), as a member's job files stand (FILES section 4.3): the
// directory path's checks before a job is rendered from its folder. A job
// the caller's daemon holds comes over the socket, and the client opens no
// file for it.
func openChecked(path string, group int) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errorcodes.Errorf("run_output_records_unreadable", "open %s: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, errorcodes.Errorf("run_output_records_unreadable", "stat %s: %w", path, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || !fi.Mode().IsRegular() {
		f.Close()
		return nil, errorcodes.Errorf("run_output_records_unreadable", "%s is not a regular file", path)
	}
	if int(st.Uid) != os.Geteuid() {
		switch {
		case group < 0:
			f.Close()
			return nil, errorcodes.Errorf("run_output_records_unreadable", "%s is owned by uid %d, not %d", path, st.Uid, os.Geteuid())
		case int(st.Gid) != group:
			f.Close()
			return nil, errorcodes.Errorf("run_output_records_unreadable", "%s is another operator's, outside the tree's group\n  file gid %d\n  tree gid %d", path, st.Gid, group)
		}
	}
	if fi.Mode().Perm()&0o137 != 0 {
		f.Close()
		return nil, errorcodes.Errorf("run_output_records_unreadable", "%s has mode %04o; the canonical file is 0640 or stricter", path, fi.Mode().Perm())
	}
	return f, nil
}

// notKeptAtAll is the folder follow's word for a job that keeps no
// commands.jsonl: no record to render, the display its end alone.
const notKeptAtAll = "the job's records are not kept (output.files.commands-jsonl is false)"

// folderPoll is how often a folder follow reads an unfinished job's
// commands.jsonl, summary, and scoreboard.
const folderPoll = 250 * time.Millisecond

// FollowJobDirectory renders a job no daemon of the caller's holds from its
// folder (EXAMPLES chapter 61, issue 4), as the daemon's follow renders it:
// each record of commands.jsonl as it is written, opened with openChecked's
// checks, and, once the summary names a final status, the display's end
// from that summary; a finished job renders at once. A job whose summary
// names no commands.jsonl keeps none: one warning says so, and the display
// is its end alone. While the summary is not final the job's scoreboard
// says whether it runs: one stale by watch's rule (watch.stale-after) or
// gone is job_orphaned. The summary is returned for the result line and the
// exit.
func FollowJobDirectory(ctx context.Context, common CommonOptions, folder JobFolder, jobID string, o FollowRenderOptions, stdout, stderr io.Writer) (records.Summary, error) {
	cfg, operator := common.Config, common.Operator
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return records.Summary{}, errorcodes.Ensure(err, "base_directory_unavailable")
	}
	boards, err := scoreboard.Directories(cfg.String("scoreboards"), operator.Home, base)
	if err != nil {
		return records.Summary{}, err
	}
	renderer, err := jobexec.NewRunRenderer(cfg, common.Quiet, common.Debug, ArtifactsLabel(folder.Path), o.Format, o.Echo, o.DynamicBorder, o.NoBorder, stdout, stderr)
	if err != nil {
		return records.Summary{}, err
	}
	var (
		reader   *bufio.Reader
		partial  []byte
		rendered bool
		notKept  bool
	)
	tick := time.NewTicker(folderPoll)
	defer tick.Stop()
	for {
		// The summary first: records written before a final summary are in
		// the file by the read that follows.
		summary, err := readSummary(folder.Path)
		absent := errors.Is(err, fs.ErrNotExist)
		if err != nil && !absent {
			return summary, err
		}
		if !absent && !notKept && summary.Paths["commands_jsonl"] == "" {
			// Whether the summary names the file, never the path it names.
			notKept = true
			jobexec.WriteWarning(stderr, cfg, notKeptAtAll)
		}
		if reader == nil && !absent && !notKept {
			// The store makes commands.jsonl before the summary's first write.
			f, err := openChecked(filepath.Join(folder.Path, "commands.jsonl"), folder.Group)
			if err != nil {
				return summary, err
			}
			defer f.Close()
			reader = bufio.NewReaderSize(f, 64*1024)
		}
		if reader != nil {
			n := 0
			if partial, n, err = renderLines(reader, partial, renderer); err != nil {
				return summary, err
			}
			rendered = rendered || n > 0
		}
		if !absent && records.TerminalStatus(summary.FinalStatus) {
			if len(partial) > 0 {
				// A last record without its LF.
				if err := renderer.Line(append(partial, '\n')); err != nil {
					return summary, err
				}
			}
			return summary, renderer.Finish(&summary)
		}
		if err := orphaned(boards, jobID, cfg.Duration("watch.stale-after")); err != nil {
			if rendered {
				// The format closed, as for a follow whose stream was lost.
				_ = renderer.Finish(nil)
			}
			return summary, err
		}
		select {
		case <-ctx.Done():
			return summary, ctx.Err()
		case <-tick.C:
		}
	}
}

// renderLines renders each whole line the reader holds now and keeps a line
// still being written, without its LF, for the next read; n is how many it
// rendered.
func renderLines(r *bufio.Reader, partial []byte, renderer *jobexec.RunRenderer) ([]byte, int, error) {
	n := 0
	for {
		chunk, err := r.ReadBytes('\n')
		partial = append(partial, chunk...)
		if errors.Is(err, io.EOF) {
			return partial, n, nil
		}
		if err != nil {
			return partial, n, errorcodes.Errorf("run_output_records_unreadable", "read commands.jsonl: %w", err)
		}
		if err := renderer.Line(partial); err != nil {
			return nil, n, err
		}
		partial, n = nil, n+1
	}
}

// orphaned is job_orphaned for an unfinished job whose scoreboard is gone
// or stale by watch's rule, or names the job's end and is older than
// staleAfter, the summary never made final (EXAMPLES chapter 61, issue 4);
// nil while it shows the job running or just ended.
func orphaned(boards []string, jobID string, staleAfter time.Duration) error {
	s, ok := scoreboard.Find(boards, jobID)
	if !ok {
		return errorcodes.Errorf("job_orphaned", "job %s has no final summary and no scoreboard", jobID)
	}
	now := time.Now()
	age := now.Sub(s.LastUpdatedAt)
	stacked := fmt.Sprintf("\n  last update %s ago\n  stale after %s", age.Round(time.Millisecond), staleAfter)
	switch {
	case scoreboard.Stale(s, staleAfter, now):
		return errorcodes.Errorf("job_orphaned", "job %s has no final summary and a stale scoreboard%s", jobID, stacked)
	case records.TerminalStatus(s.Status) && staleAfter > 0 && age > staleAfter:
		return errorcodes.Errorf("job_orphaned", "job %s has no final summary and its scoreboard ended %s%s", jobID, s.Status, stacked)
	}
	return nil
}
