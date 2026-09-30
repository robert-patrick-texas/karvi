package osutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// The job ID: `YYMMDD-HHMMSS-xx`, the
// client's clock at the plan draft in the effective timezone, to the
// second, and a two-character sequence over the digits and lowercase
// letters, 00 first and bumped only when the name is taken. Nothing is
// random: uniqueness comes from the job directory, created exclusively
// here before the plan is drafted. A cmd activity and a run take the same
// form and the same reservation. The other six identifier kinds keep
// NewID's format.

// JobIDLayout is the stamp, a time.Format layout.
const JobIDLayout = "060102-150405"

// JobIDLength is the length of a job ID: the stamp, a dash, the sequence.
const JobIDLength = len(JobIDLayout) + 1 + 2

// jobSequenceAlphabet orders the 1296 sequences of one second.
const jobSequenceAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// JobSequences is how many jobs one second holds.
const JobSequences = len(jobSequenceAlphabet) * len(jobSequenceAlphabet)

// JobIDStamp is now in loc, the ID without its sequence.
func JobIDStamp(now time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return now.In(loc).Format(JobIDLayout)
}

// jobSequence is the two characters of sequence n, 0 <= n < JobSequences.
func jobSequence(n int) string {
	return string([]byte{jobSequenceAlphabet[n/len(jobSequenceAlphabet)], jobSequenceAlphabet[n%len(jobSequenceAlphabet)]})
}

// ReserveSequence takes the first free sequence of now's stamp. try is
// called with each candidate ID in order, 00 first: it returns nil when it
// took the name, os.ErrExist when the name is taken (the next is tried),
// or another error, which ends the search as it is. The one loop serves
// every reservation that gives an activity the job form: the job directory
// (ReserveJobID) and a login's scoreboard file (scoreboard.Writer.Reserve).
// where names the place for the message when
// every sequence of the second is taken, which no site will see.
func ReserveSequence(now time.Time, loc *time.Location, where string, try func(id string) error) (string, error) {
	stamp := JobIDStamp(now, loc)
	for n := 0; n < JobSequences; n++ {
		id := stamp + "-" + jobSequence(n)
		err := try(id)
		if err == nil {
			return id, nil
		}
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return "", err
	}
	return "", errorcodes.Errorf("activity_id_generation_failed", "every job ID of %s under %s is taken", stamp, where)
}

// ReserveJobID takes the first free job ID of now under root: the day
// folder is created if missing (under a setgid root with the root's mode,
// DayFolderMode), and the job directory is created exclusively with mode,
// sequence 00 first and the next on "exists". It returns the ID and the
// directory. Every sequence
// of the second taken is activity_id_generation_failed, which no site will
// see; a directory that cannot be created is output_directory_not_writable.
func ReserveJobID(root string, now time.Time, loc *time.Location, mode os.FileMode) (id, dir string, err error) {
	day := filepath.Join(root, DayFolder(now, loc))
	dayMode := DayFolderMode(root, mode)
	if err := EnsureOutputDirectory(day, dayMode, "output_directory_not_writable"); err != nil {
		return "", "", err
	}
	id, err = ReserveSequence(now, loc, day, func(id string) error {
		candidate := filepath.Join(day, id)
		err := os.Mkdir(candidate, mode)
		if errors.Is(err, os.ErrNotExist) {
			// Another activity's release removed the day folder between
			// its creation above and this mkdir (ReleaseJobID leaves no
			// empty day folder behind); make it again and try the same
			// name once more.
			if err = EnsureOutputDirectory(day, dayMode, "output_directory_not_writable"); err != nil {
				return err
			}
			err = os.Mkdir(candidate, mode)
		}
		if err == nil {
			// The mode explicitly, so the umask cannot narrow it; an
			// inherited setgid bit stays.
			if err := chmodKeepingSetgid(candidate, mode); err != nil {
				return errorcodes.Errorf("output_directory_not_writable", "chmod %s: %w", candidate, err)
			}
			dir = candidate
			return nil
		}
		if errors.Is(err, os.ErrExist) {
			return err
		}
		return errorcodes.Errorf("output_directory_not_writable", "cannot create %s: %w", candidate, err)
	})
	if err != nil {
		return "", "", err
	}
	return id, dir, nil
}

// UnreservedJobID is the ID of a job that writes no files (`--nof`, every
// output.files key off): sequence 00 with no directory behind it, an
// accepted gap in the sequence.
func UnreservedJobID(now time.Time, loc *time.Location) string {
	return JobIDStamp(now, loc) + "-" + jobSequence(0)
}

// ReleaseJobID removes a reservation that never became a job: a
// refused plan or a credential error before submission. It removes only an
// empty directory, so a directory that gained files is never touched, and
// an absent one is nothing to do; the day folder goes too when the
// reservation was the only thing in it, so a refused activity leaves no
// trace in the tree (rmdir refuses a folder holding another job).
func ReleaseJobID(dir string) {
	if dir == "" {
		return
	}
	if err := os.Remove(dir); err != nil {
		return
	}
	_ = os.Remove(filepath.Dir(dir))
}

// JobIDTime is the instant a job ID's stamp names in loc, to the second.
// A string that is not a job ID is job_request_malformed, the code the
// daemon answers. The stamp is read in loc because it was written in the
// effective timezone: the day folder is the stamp's date.
func JobIDTime(id string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	if len(id) != JobIDLength || id[len(JobIDLayout)] != '-' {
		return time.Time{}, errorcodes.Errorf("job_request_malformed", "%q is not a job ID (YYMMDD-HHMMSS-xx)", id)
	}
	t, err := time.ParseInLocation(JobIDLayout, id[:len(JobIDLayout)], loc)
	if err != nil {
		return time.Time{}, errorcodes.Errorf("job_request_malformed", "job ID %q: %w", id, err)
	}
	return t, nil
}

// ClaimJobDirectory prepares a job's directory for its store. The
// directory the client reserved exists and is empty; it is
// accepted when it is a directory owned by this process's UID with nothing
// in it, and refused as output_directory_in_use otherwise, so two jobs can
// never write into one directory (earlier an existing directory was
// accepted as it was). A directory that does not exist is created with
// mode, as before: a job whose ID was made without a reservation on this
// host. The writability check then applies as to any folder.
func ClaimJobDirectory(dir string, mode os.FileMode) error {
	fi, err := os.Lstat(dir)
	switch {
	case err == nil:
		if !fi.IsDir() {
			return errorcodes.Errorf("output_directory_not_writable", "%s exists and is not a folder", dir)
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
			return errorcodes.Errorf("output_directory_in_use", "%s is owned by uid %d, not %d", dir, st.Uid, os.Geteuid())
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return errorcodes.Errorf("output_directory_not_writable", "read %s: %w", dir, err)
		}
		if len(entries) > 0 {
			return errorcodes.Errorf("output_directory_in_use", "%s already holds %d entries (%s); another job's?", dir, len(entries), entries[0].Name())
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return errorcodes.Errorf("output_directory_not_writable", "lstat %s: %w", dir, err)
	}
	return EnsureOutputDirectory(dir, mode, "output_directory_not_writable")
}
