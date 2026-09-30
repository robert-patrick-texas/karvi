package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// The output spool: a command whose settled response passes
// output.spool-threshold-bytes continues into a file under spooldir, one per
// command in flight, streamed into the record's line when the command ends
// and removed once the record is durable. This file holds what the spool's
// directory needs before any byte is spooled: the file's name, the sweep
// of abandoned files, and the admission preflight with its free-space
// rule. The reader that fills a spool and the store that drains it live
// in devsession and in store.go.

// SpoolSuffix ends every spool file's name.
const SpoolSuffix = ".spool"

// SpoolName names the spool of one command in flight:
// <activity>.<device>.<index>.<pid>.spool, 0600 in the uid's own directory.
// The name is the metadata: an activity ID holds no dot, so a sweep and a
// future recovery read the fields from the name alone, parsing
// from the right, since a device name may hold dots of its own.
func SpoolName(activity, device string, index, pid int) string {
	return fmt.Sprintf("%s.%s.%d.%d%s", activity, device, index, pid, SpoolSuffix)
}

// spoolOwner reads the pid from a spool file's name, false for any other
// name in the directory (which the sweep leaves alone).
func spoolOwner(name string) (pid int, ok bool) {
	base, found := strings.CutSuffix(name, SpoolSuffix)
	if !found {
		return 0, false
	}
	parts := strings.Split(base, ".")
	if len(parts) < 4 {
		return 0, false
	}
	if _, err := strconv.Atoi(parts[len(parts)-2]); err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// SweepSpools removes the abandoned spools in dir: a file of the spool's
// name shape, owned by the
// effective uid, whose pid is not alive, or is alive as something other
// than a karvi executable (/proc/PID/cmdline), left by a daemon or client
// that died with a command in flight. A live karvi pid is in use and its
// file is left alone; anything else in the directory is not karvi's and is
// not touched. Each removal is reported through log as
// spool_abandoned_removed with the file's name; the names removed are
// returned for a test. It runs at the daemon's start and at the start of
// every run that resolves spooldir (7.2), so a spool outlives its owner by
// at most one admission.
func SweepSpools(dir string, log func(name string)) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	uid := os.Geteuid()
	var removed []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		pid, ok := spoolOwner(e.Name())
		if !ok {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != uid {
			continue
		}
		if spoolInUse(pid) {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) != nil {
			continue
		}
		removed = append(removed, e.Name())
		if log != nil {
			log(e.Name())
		}
	}
	return removed
}

// spoolInUse says whether pid is alive as a karvi executable, the one
// condition that keeps a spool in the sweep; a test replaces it.
var spoolInUse = func(pid int) bool {
	return osutil.ProcessAlive(pid, "") && strings.HasPrefix(osutil.ProcessCommandName(pid), "karvi")
}
