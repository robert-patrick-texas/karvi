package osutil

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The scratch's files karvi names by its own pid, so that a sweep can tell
// one whose maker died from one in use: the system transport's generated
// configuration and a recorded login's timing log.
var scratchFileKinds = []struct{ prefix, suffix string }{
	{"karvi-ssh-", ".conf"},
	{"karvi-script-", ".timing"},
}

// ScratchFilePattern is the os.CreateTemp pattern of a scratch file of
// prefix and suffix, one of scratchFileKinds: the maker's pid, then
// CreateTemp's random part (karvi-ssh-<pid>-*.conf).
func ScratchFilePattern(prefix, suffix string) string {
	return fmt.Sprintf("%s%d-*%s", prefix, os.Getpid(), suffix)
}

// scratchFileOwner reports the pid a scratch file's name carries, and
// whether the name is one karvi makes: a known prefix and suffix around
// <pid>-<digits>.
func scratchFileOwner(name string) (int, bool) {
	for _, k := range scratchFileKinds {
		middle, ok := strings.CutPrefix(name, k.prefix)
		if !ok {
			continue
		}
		middle, ok = strings.CutSuffix(middle, k.suffix)
		if !ok {
			continue
		}
		pidText, random, ok := strings.Cut(middle, "-")
		if !ok || random == "" || strings.Trim(random, "0123456789") != "" {
			return 0, false
		}
		pid, err := strconv.Atoi(pidText)
		if err != nil || pid <= 0 {
			return 0, false
		}
		return pid, true
	}
	return 0, false
}

// askpassSocketName says whether name is an askpass broker's socket:
// askpass-<16 lowercase hex>.sock.
func askpassSocketName(name string) bool {
	middle, ok := strings.CutPrefix(name, "askpass-")
	if !ok {
		return false
	}
	middle, ok = strings.CutSuffix(middle, ".sock")
	return ok && controlSocketName(middle)
}

// KarviAlive says whether pid is alive as a karvi executable
// (/proc/PID/cmdline): the one condition that keeps a file its name says
// pid made. A pid that died, or was reused by something else, does not.
func KarviAlive(pid int) bool {
	return ProcessAlive(pid, "") && strings.HasPrefix(ProcessCommandName(pid), "karvi")
}

// scratchOwnerAlive is KarviAlive; a test replaces it, since a test binary
// is not a karvi executable.
var scratchOwnerAlive = KarviAlive

// socketAbandoned says whether the Unix socket at path refuses a
// connection: nothing listens on it. A socket that answers, or fails in any
// other way, is in use or not judged.
func socketAbandoned(path string) bool {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		conn.Close()
		return false
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

// SweepScratch removes what a karvi killed outright left in the scratch
// dir, owned by the effective uid: a scratch file of a name karvi makes
// (ScratchFilePattern) whose pid is not alive as a karvi executable, and an
// askpass socket that refuses a connection. Anything else in the directory
// is not karvi's and is not touched. Each removal is reported through log
// as scratch_abandoned_removed with the name; the names removed are
// returned for a test. It runs at the daemon's start, at every admission,
// and at a login's start.
func SweepScratch(dir string, log func(name string)) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	uid := os.Geteuid()
	var removed []string
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		switch {
		case e.Type().IsRegular():
			pid, ok := scratchFileOwner(name)
			if !ok || !ownedBy(e, uid) || scratchOwnerAlive(pid) {
				continue
			}
		case e.Type()&os.ModeSocket != 0:
			if !askpassSocketName(name) || !ownedBy(e, uid) || !socketAbandoned(path) {
				continue
			}
		default:
			continue
		}
		if os.Remove(path) != nil {
			continue
		}
		removed = append(removed, name)
		if log != nil {
			log(name)
		}
	}
	return removed
}

// ownedBy says whether the directory entry belongs to uid.
func ownedBy(e os.DirEntry, uid int) bool {
	fi, err := e.Info()
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}
