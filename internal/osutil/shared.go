package osutil

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// The site's shared trees and the operators' roots' parent: `sudo karvi
// setup shared`
// creates the shared directory (/opt/karvi/shared) and the three trees
// under it, every one in the group with group write and search and the
// setgid bit, so that every member of the group writes into them and what
// is made below stays in the group, and the
// `users` directory beside it (/opt/karvi/users) in the group with the
// sticky bit, so that every member creates its own private root there and
// none removes another's. Everything else karvi does with the trees is
// unprivileged and never changes a group or a mode.

// SharedRootMode is the mode of the shared directory's parent, the system
// root (/opt/karvi), a plain directory anyone may search: the shared
// directory and the trees below it carry the group and the bit.
const SharedRootMode os.FileMode = 0o755

// SharedTreeModes are the modes setup shared gives a tree, before the
// setgid bit: the two shapes the collection run's message names.
var SharedTreeModes = map[string]os.FileMode{"2770": 0o770, "2775": 0o775}

// SharedSetup is one directory's outcome: created, present with the group
// and mode asked for, or repaired to them (Was says what it had).
type SharedSetup struct {
	Path  string
	State string // "created", "exists", or "repaired"
	Group string
	Mode  os.FileMode // the permission bits with the setgid or sticky bit
	Was   string      // for "repaired": the group and mode found
}

// sharedPlace is one directory setup shared makes: its path and the mode
// it takes, bits included (setgid for the shared trees, sticky for users).
type sharedPlace struct {
	path string
	mode os.FileMode
}

// SetupSharedTrees creates the parent of shared when it is missing
// (SharedRootMode, the caller's owner), then shared itself and each of
// SharedTrees under it with mode plus the setgid bit in group gid, and the
// system root's `users` directory (the operators' private roots' parent,
// UsersDirMode plus the sticky bit) in the same group, applying
// group and mode explicitly so the umask cannot narrow them. A directory
// that exists is kept when it is a real directory in gid at its mode, and
// repaired to them when it is not (setup shared may fix a broken group or
// mode, since it made the layout); one
// that is not a real directory is refused (setup_directory_mismatch) and
// nothing is changed. Every directory is reported, the shared one first,
// users last.
func SetupSharedTrees(shared string, gid int, mode os.FileMode) ([]SharedSetup, error) {
	group := strconv.Itoa(gid)
	if gr, err := user.LookupGroupId(group); err == nil {
		group = gr.Name
	}
	root := filepath.Dir(shared)
	if err := makeDirectories(root, SharedRootMode); err != nil {
		return nil, errorcodes.Errorf("setup_directory_create_failed", "create %s: %v", root, err)
	}
	places := []sharedPlace{{shared, os.ModeSetgid | mode}}
	for _, sub := range SharedTrees {
		places = append(places, sharedPlace{filepath.Join(shared, sub), os.ModeSetgid | mode})
	}
	places = append(places, sharedPlace{filepath.Join(root, "users"), os.ModeSticky | UsersDirMode})
	var results []SharedSetup
	var mismatch []string
	for _, pl := range places {
		r, why, err := setupSharedDirectory(pl.path, gid, group, pl.mode)
		if err != nil {
			return results, err
		}
		if why != "" {
			mismatch = append(mismatch, why)
			continue
		}
		results = append(results, r)
	}
	if len(mismatch) > 0 {
		return results, errorcodes.Errorf("setup_directory_mismatch", "%s; nothing was changed there: the site removes or renames it and runs setup shared again", strings.Join(mismatch, "; "))
	}
	return results, nil
}

// setupSharedDirectory is one directory of setup shared: created in gid at
// mode (bits included) when missing; kept and reported as existing when it
// is a real directory in gid at that mode; repaired to gid and mode when it
// is a real directory with another group or mode; otherwise why says what
// the site made instead, and nothing is changed.
func setupSharedDirectory(p string, gid int, group string, mode os.FileMode) (r SharedSetup, why string, err error) {
	fi, err := os.Lstat(p)
	switch {
	case err == nil:
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return r, fmt.Sprintf("%s exists and is not a real directory", p), nil
		}
		st, _ := fi.Sys().(*syscall.Stat_t)
		sameGroup := st == nil || int(st.Gid) == gid
		sameMode := fi.Mode().Perm() == mode.Perm() && fi.Mode()&(os.ModeSetgid|os.ModeSticky) == mode&(os.ModeSetgid|os.ModeSticky)
		if sameGroup && sameMode {
			return SharedSetup{Path: p, State: "exists", Group: group, Mode: mode}, "", nil
		}
		was := fmt.Sprintf("group %s mode %s", groupName(int(st.Gid)), modeString(fi.Mode()))
		if err := applyGroupAndMode(p, gid, mode); err != nil {
			return r, "", err
		}
		return SharedSetup{Path: p, State: "repaired", Group: group, Mode: mode, Was: was}, "", nil
	case !os.IsNotExist(err):
		return r, "", errorcodes.Errorf("setup_directory_create_failed", "%s: %v", p, err)
	}
	if err := os.Mkdir(p, mode.Perm()); err != nil {
		return r, "", errorcodes.Errorf("setup_directory_create_failed", "create %s: %v", p, err)
	}
	if err := applyGroupAndMode(p, gid, mode); err != nil {
		return r, "", err
	}
	return SharedSetup{Path: p, State: "created", Group: group, Mode: mode}, "", nil
}

// applyGroupAndMode sets a directory's group and its mode with its bits,
// explicitly, so neither the umask nor what the site had stands.
func applyGroupAndMode(p string, gid int, mode os.FileMode) error {
	if err := os.Chown(p, -1, gid); err != nil {
		return errorcodes.Errorf("setup_directory_create_failed", "set the group of %s: %v", p, err)
	}
	if err := os.Chmod(p, mode); err != nil {
		return errorcodes.Errorf("setup_directory_create_failed", "set the mode of %s: %v", p, err)
	}
	return nil
}

// groupName is the name of gid, or its number when the group is unknown.
func groupName(gid int) string {
	if gr, err := user.LookupGroupId(strconv.Itoa(gid)); err == nil {
		return gr.Name
	}
	return strconv.Itoa(gid)
}

// ModeString is modeString for the setup report.
func ModeString(m os.FileMode) string { return modeString(m) }

// modeString renders a folder mode as the four octal digits an operator
// types (2770, 1770), the setgid or sticky bit first.
func modeString(m os.FileMode) string {
	bits := m.Perm()
	if m&os.ModeSetgid != 0 {
		bits |= 0o2000
	}
	if m&os.ModeSticky != 0 {
		bits |= 0o1000
	}
	return fmt.Sprintf("%04o", uint32(bits))
}

// ErrSharedDirectoryAbsent is MakeSharedDirectory's answer for a directory
// whose parent is missing: the caller takes its private fallback without a
// warning, since nothing the site made is wrong.
var ErrSharedDirectoryAbsent = errorcodes.Errorf("shared_directory_absent", "the shared directory and its parent are absent")

// MakeSharedDirectory prepares a group-shared directory an operator's
// process writes in (watch.directory, sessions.shared-capacity-root). One
// that exists is left as it is, for the caller to judge. A missing one is
// made only when its parent exists: under a parent carrying the setgid
// bit (the scratch root setup shared makes) with the parent's permission
// bits set explicitly, so the umask cannot close it to the group;
// elsewhere at mode under the umask. A missing parent is never made, so an
// operator never creates the scratch root and closes it to every other
// (ErrSharedDirectoryAbsent).
func MakeSharedDirectory(path string, mode os.FileMode) error {
	if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(path)
	fi, err := os.Stat(parent)
	if os.IsNotExist(err) {
		return ErrSharedDirectoryAbsent
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSetgid == 0 {
		if err := os.Mkdir(path, mode); err != nil && !os.IsExist(err) {
			return err
		}
		return nil
	}
	if err := os.Mkdir(path, fi.Mode().Perm()); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return os.Chmod(path, os.ModeSetgid|fi.Mode().Perm())
}
