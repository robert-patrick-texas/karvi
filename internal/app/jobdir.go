package app

import (
	"encoding/json"
	"errors"
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
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

// The job directory of a job the daemon does not hold: derived from the
// job ID and the configuration, never searched, and
// read only for a finished job.

// JobIDTime parses the instant a job ID's stamp names, read in loc, the
// effective timezone the stamp was written in; the day folder is that
// instant's date. A string that is not a
// job ID is job_request_malformed, the code the daemon answers.
func JobIDTime(jobID string, loc *time.Location) (time.Time, error) {
	if !executionplan.ValidJobID(jobID) {
		return time.Time{}, errorcodes.Errorf("job_request_malformed", "%q is not a job ID (YYMMDD-HHMMSS-xx)", jobID)
	}
	return osutil.JobIDTime(jobID, loc)
}

// jobDirectoryFor is the path the runner wrote for jobID under cfg: the
// output root, the ID's instant in the effective timezone, the ID.
func jobDirectoryFor(cfg configload.Snapshot, base, home, jobID string) (string, error) {
	location, err := display.Location(cfg.String("timezone"))
	if err != nil {
		return "", err
	}
	accepted, err := JobIDTime(jobID, location)
	if err != nil {
		return "", err
	}
	root, err := osutil.ResolveOutputRoot(cfg.String("output.root"), cfg.String("sharedroot"), base, home)
	if err != nil {
		return "", errorcodes.Ensure(err, "output_root_unavailable")
	}
	return osutil.JobDirectory(root, jobID, accepted, location), nil
}

// JobDirectoryFor derives the job directory under the invocation's
// configuration: the ID's instant in the effective timezone
// gives the day folder the runner most likely wrote.
func JobDirectoryFor(common CommonOptions, jobID string) (string, error) {
	cfg, operator, err := prepareConfig(common, false)
	if err != nil {
		return "", err
	}
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return "", errorcodes.Ensure(err, "base_directory_unavailable")
	}
	return jobDirectoryFor(cfg, base, operator.Home, jobID)
}

// LocateJobDirectory finds the directory of a job the daemon does not hold
// (refined at implementation): the output root is derived
// from the configuration, and the job's directory is the day folder holding
// a subdirectory named by the job ID. The client reserves
// that directory under the day folder of the ID's own stamp,
// so the derived path is the one written; the glob across day folders
// remains for a site whose timezone changed since, where the stamp reads
// as another day. It returns the path and whether it exists; a malformed
// ID is job_request_malformed. The derived day folder is returned when
// nothing exists, so a caller can name where it looked.
func LocateJobDirectory(common CommonOptions, jobID string) (dir string, found bool, err error) {
	cfg, operator, err := prepareConfig(common, false)
	if err != nil {
		return "", false, err
	}
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return "", false, errorcodes.Ensure(err, "base_directory_unavailable")
	}
	derived, err := jobDirectoryFor(cfg, base, operator.Home, jobID)
	if err != nil {
		return "", false, err
	}
	if _, statErr := os.Lstat(derived); statErr == nil {
		return derived, true, nil
	}
	root, err := osutil.ResolveOutputRoot(cfg.String("output.root"), cfg.String("sharedroot"), base, operator.Home)
	if err != nil {
		return derived, false, errorcodes.Ensure(err, "output_root_unavailable")
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", jobID))
	if err != nil || len(matches) == 0 {
		return derived, false, nil
	}
	return matches[0], true, nil
}

// JobDirectoryState is what the directory path found.
type JobDirectoryState int

const (
	// JobDirectoryAbsent: no directory; the job is unknown here.
	JobDirectoryAbsent JobDirectoryState = iota
	// JobDirectoryFinished: the directory holds summary.json.
	JobDirectoryFinished
	// JobDirectoryOrphaned: the directory exists without a summary; the job
	// is unfinished and, when no daemon holds it, its daemon is gone.
	JobDirectoryOrphaned
)

// InspectJobDirectory reports the directory's state without reading it.
func InspectJobDirectory(dir string) (JobDirectoryState, error) {
	if _, err := os.Lstat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return JobDirectoryAbsent, nil
		}
		return JobDirectoryAbsent, errorcodes.Errorf("run_output_records_unreadable", "%s: %w", dir, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "summary.json")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return JobDirectoryOrphaned, nil
		}
		return JobDirectoryAbsent, errorcodes.Errorf("run_output_records_unreadable", "%s: %w", filepath.Join(dir, "summary.json"), err)
	}
	return JobDirectoryFinished, nil
}

// openChecked opens a job's commands.jsonl without following links and
// requires a regular file owned by this process's UID, mode 0640 or
// stricter: the directory path's checks before a
// finished job is rendered from its folder. A followed job's records come
// over the socket and the client opens no file for them.
func openChecked(path string) (*os.File, error) {
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
		f.Close()
		return nil, errorcodes.Errorf("run_output_records_unreadable", "%s is owned by uid %d, not %d", path, st.Uid, os.Geteuid())
	}
	if fi.Mode().Perm()&0o137 != 0 {
		f.Close()
		return nil, errorcodes.Errorf("run_output_records_unreadable", "%s has mode %04o; the canonical file is 0640 or stricter", path, fi.Mode().Perm())
	}
	return f, nil
}

// RenderJobDirectory renders a finished job from its directory (decision
// 3.2): commands.jsonl is opened with the checks the follow applies to the
// announced file (a regular file, the operator's UID, mode 0640 or
// stricter, no link following) and rendered through the post-hoc renderer;
// the summary is returned for the result line and the exit.
func RenderJobDirectory(common CommonOptions, dir string, o FollowRenderOptions, out io.Writer) (records.Summary, error) {
	var summary records.Summary
	raw, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		return summary, errorcodes.Errorf("run_output_records_unreadable", "read %s: %w", filepath.Join(dir, "summary.json"), err)
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		return summary, errorcodes.Errorf("run_output_record_decode_failed", "decode %s: %w", filepath.Join(dir, "summary.json"), err)
	}
	f, err := openChecked(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		return summary, err
	}
	f.Close()
	// The display ends with the footer from the summary the directory
	// holds, or the summary line in jsonl (28.4).
	if err := RenderRunOutput(common, dir, o.Format, o.Echo, o.DynamicBorder, o.NoBorder, &summary, out); err != nil {
		return summary, err
	}
	return summary, nil
}
