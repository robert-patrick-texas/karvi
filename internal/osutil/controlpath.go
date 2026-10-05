package osutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// The system transport's control sockets. An exec device's ControlMaster
// listens in ssh.control-path-root under a name karvi picks, 16 hex
// characters; OpenSSH binds a temporary name, the path and a dot and 16
// characters, before it renames the socket into place, and a socket's path
// fits in 107 bytes (sun_path's 108 less its terminating NUL).

// ControlSocketNameLength is the length of a control socket's name.
const ControlSocketNameLength = 16

// MaxControlPathRoot is the longest root a control socket fits under: 107
// bytes less the separator, the name, and OpenSSH's 17-byte binding
// suffix.
const MaxControlPathRoot = 107 - 1 - ControlSocketNameLength - 17

// ControlPathRootPlace is where ControlPathRoot would resolve, made or
// not, for planning: nothing is created. The scratch root's folder is taken
// when it is absent (ControlPathRoot would make it) or the operator's
// private one.
func ControlPathRootPlace(raw, base, home, username string, uid int) (string, error) {
	if raw != "" && raw != "auto" {
		p, err := expandHome(raw, home)
		if err != nil {
			return "", err
		}
		return filepath.Abs(p)
	}
	if scratchRootPresent() {
		p := filepath.Join(ScratchRoot, username, "sockets")
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p, nil
		}
		if _, err := safePrivateDirectory(p, uid, false); err == nil {
			return p, nil
		}
	}
	return filepath.Abs(filepath.Join(base, "socket", "ssh"))
}

// CheckControlPathRoot refuses a root too long for a control socket's
// path, naming the root and its length.
func CheckControlPathRoot(root string) error {
	if len(root) > MaxControlPathRoot {
		return errorcodes.Errorf("control_path_root_too_long", "the control-path root %s is %d bytes; a control socket's path must fit OpenSSH's 108-byte limit with its 16-character name and 17-character binding suffix, so the root may be at most %d bytes; set ssh.control-path-root to a shorter directory", root, len(root), MaxControlPathRoot)
	}
	return nil
}

// NewControlSocketName is a fresh control socket's name.
func NewControlSocketName() (string, error) {
	var b [ControlSocketNameLength / 2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// controlSocketName says whether name is one karvi makes: 16 lowercase hex
// characters.
func controlSocketName(name string) bool {
	if len(name) != ControlSocketNameLength {
		return false
	}
	for _, c := range name {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// SweepControlSockets removes the dead control sockets in root: a socket
// of a name karvi makes, owned by the effective uid, that refuses a
// connection, left by a master killed outright. A socket that answers, or
// fails in any other way, is left, and anything else in the directory is
// not karvi's and is not touched. Each removal is reported through log as
// control_socket_abandoned_removed with the socket's name; the names
// removed are returned for a test. It runs at the daemon's start and at
// every admission.
func SweepControlSockets(root string, log func(name string)) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	uid := os.Geteuid()
	var removed []string
	for _, e := range entries {
		if e.Type()&os.ModeSocket == 0 || !controlSocketName(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != uid {
			continue
		}
		path := filepath.Join(root, e.Name())
		conn, err := net.DialTimeout("unix", path, time.Second)
		if err == nil {
			conn.Close()
			continue
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			continue
		}
		if os.Remove(path) != nil {
			continue
		}
		removed = append(removed, e.Name())
		if log != nil {
			log(e.Name())
		}
	}
	return removed
}
