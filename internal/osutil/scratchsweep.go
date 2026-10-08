package osutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

// AskpassSocketName is the name of an askpass broker's socket of id (16
// lowercase hex): askpass-<pid>-<id>.sock, the maker's pid first, so that a
// sweep judges it as it judges a scratch file. The broker is one-use: a
// sweep that connected to tell a live socket from an abandoned one would be
// the connection it serves, and the helper would find no socket.
func AskpassSocketName(id string) string {
	return fmt.Sprintf("askpass-%d-%s.sock", os.Getpid(), id)
}

// MaxScratchDir is the longest scratch directory an askpass socket's path
// fits under: 107 bytes less the separator and AskpassSocketName's longest
// name, its pid of 7 digits (Linux's PID_MAX_LIMIT, 4194304). A bound by the
// running pid would let a directory pass until the pids reach 7 digits.
const MaxScratchDir = 107 - 1 - len("askpass-") - 7 - 1 - ControlSocketNameLength - len(".sock")

// askpassSocketOwner reports the pid an askpass socket's name carries, and
// whether the name is one AskpassSocketName makes.
func askpassSocketOwner(name string) (int, bool) {
	middle, ok := strings.CutPrefix(name, "askpass-")
	if !ok {
		return 0, false
	}
	middle, ok = strings.CutSuffix(middle, ".sock")
	if !ok {
		return 0, false
	}
	pidText, id, ok := strings.Cut(middle, "-")
	if !ok || !controlSocketName(id) {
		return 0, false
	}
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
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

// SweepScratch removes what a karvi killed outright left in the scratch
// dir, owned by the effective uid: a scratch file of a name karvi makes
// (ScratchFilePattern) or an askpass socket (AskpassSocketName) whose pid
// is not alive as a karvi executable. It connects to no socket. Anything
// else in the directory is not karvi's and is not touched. Each removal is reported through log
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
		var pid int
		var ok bool
		switch {
		case e.Type().IsRegular():
			pid, ok = scratchFileOwner(name)
		case e.Type()&os.ModeSocket != 0:
			pid, ok = askpassSocketOwner(name)
		}
		if !ok || !ownedBy(e, uid) || scratchOwnerAlive(pid) {
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
