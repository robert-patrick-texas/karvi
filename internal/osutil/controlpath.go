package osutil

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"

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

// ControlPathRootPlace is ControlPathRoot's twin: where it would resolve,
// made or not, and the scratch root's folder when present and passed by;
// nothing is created. The scratch root's folder is taken when it is absent
// and can be made, or is the operator's private one; the fallback, or an
// explicit path, present and not the operator's private one is refused
// with the activity's code.
func ControlPathRootPlace(raw, base, home, username string, uid int) (Place, error) {
	if raw != "" && raw != "auto" {
		p, err := explicitPath(raw, home, "ssh.control-path-root")
		if err != nil {
			return Place{}, err
		}
		return Place{Path: p}, privatePlaceProblem(p, uid)
	}
	var pl Place
	if p, ok := scratchSockets(username, uid); ok {
		if p.Reason == "" {
			pl.Path = p.Path
			return pl, nil
		}
		pl.Passed = append(pl.Passed, p)
	}
	p, err := filepath.Abs(filepath.Join(base, "socket", "ssh"))
	if err != nil {
		return pl, err
	}
	pl.Path = p
	return pl, privatePlaceProblem(p, uid)
}

// privatePlaceProblem is the refusal safePrivateDirectory would give p,
// judged without writing: a present p by checkPrivateDirectory, an absent
// one by whether it can be made.
func privatePlaceProblem(p string, uid int) error {
	if _, err := os.Lstat(p); os.IsNotExist(err) {
		if reason := makeableReason(p); reason != "" {
			return errorcodes.Errorf("private_directory_not_writable", "%s cannot be made: %s", p, reason)
		}
		return nil
	}
	return checkPrivateDirectory(p, uid)
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
		if e.Type()&os.ModeSocket == 0 || !controlSocketName(e.Name()) || !ownedBy(e, uid) {
			continue
		}
		path := filepath.Join(root, e.Name())
		if !socketAbandoned(path) {
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
