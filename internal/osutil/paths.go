package osutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// Folder and file modes of the state, job, and transcript trees.
const (
	// RootMode is the mode of a state root or logs subtree karvi creates.
	RootMode os.FileMode = 0o750
	// PrivateMode is the mode of the socket and state subtrees.
	PrivateMode os.FileMode = 0o700
	// OutputFileMode is the mode of every file in the job and transcript trees.
	OutputFileMode os.FileMode = 0o640
	// DefaultDirectoryMode is the default of output.directory-mode.
	DefaultDirectoryMode os.FileMode = 0o750
)

// DirectoryMode parses an output.directory-mode or crun.directory-mode
// value ("0700", "0750", "0755", or "0770"). Configuration validation has
// already rejected other values; an unparsable value yields the default.
func DirectoryMode(value string) os.FileMode {
	return parseMode(value, DefaultDirectoryMode)
}

// DefaultCrunFileMode is the default of crun.file-mode: group write, so any
// member of the directory's group can collect and replace.
const DefaultCrunFileMode os.FileMode = 0o660

// FileMode parses a crun.file-mode value ("0640", "0644", or "0660"), the
// mode of a collection file; an unparsable value yields the default.
func FileMode(value string) os.FileMode {
	return parseMode(value, DefaultCrunFileMode)
}

func parseMode(value string, fallback os.FileMode) os.FileMode {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 8, 32)
	if err != nil || n == 0 {
		return fallback
	}
	return os.FileMode(n) & os.ModePerm
}

func expandHome(raw, home string) (string, error) {
	if raw == "~" {
		return home, nil
	}
	if strings.HasPrefix(raw, "~/") {
		return filepath.Join(home, raw[2:]), nil
	}
	if strings.HasPrefix(raw, "~") {
		return "", errorcodes.Errorf("path_other_user_home_unsupported", "~otheruser paths are not supported: %s", raw)
	}
	return raw, nil
}

// safePrivateDirectory accepts a real directory owned by uid with no group or
// other access that the operator can write to (the socket and state subtrees
// and the control-path root). With create, a missing path is made
// with PrivateMode.
func safePrivateDirectory(path string, uid int, create bool) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if create {
		if err := makeDirectories(p, PrivateMode); err != nil {
			return "", err
		}
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return "", errorcodes.Errorf("private_directory_not_real", "%s is not a real directory", p)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errorcodes.Errorf("private_directory_owner_uninspectable", "cannot inspect ownership of %s", p)
	}
	if int(st.Uid) != uid {
		return "", errorcodes.Errorf("private_directory_owner_mismatch", "%s is owned by uid %d, expected %d", p, st.Uid, uid)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return "", errorcodes.Errorf("private_directory_mode_exposed", "%s mode %04o exposes group/other; expected 0700", p, fi.Mode().Perm())
	}
	if err := probeWritable(p); err != nil {
		return "", errorcodes.Errorf("private_directory_not_writable", "%s is not writable: %w", p, err)
	}
	return p, nil
}

// writableDirectory accepts a real directory the operator can create files in,
// whatever its owner, group, and permissions. code is
// the registered code for a directory that is not writable.
func writableDirectory(path, code string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return "", errorcodes.Errorf("private_directory_not_real", "%s is not a real directory", p)
	}
	if err := probeWritable(p); err != nil {
		return "", errorcodes.Errorf(code, "%s is not writable by the operator: %w", p, err)
	}
	return p, nil
}

// probeWritable creates and removes a temporary file in dir.
func probeWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".karvi-write-")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return nil
}

// makeDirectories creates the missing components of path with mode, applying
// the mode explicitly so the process umask cannot narrow it. Existing
// components are left exactly as they are.
func makeDirectories(path string, mode os.FileMode) error {
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			// A link is followed once, like MkdirAll does, but never changed.
			if target, err := os.Stat(path); err == nil && target.IsDir() {
				return nil
			}
		}
		if fi.IsDir() {
			return nil
		}
		return &os.PathError{Op: "mkdir", Path: path, Err: syscall.ENOTDIR}
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(path)
	if parent != path {
		if err := makeDirectories(parent, mode); err != nil {
			return err
		}
	}
	if err := os.Mkdir(path, mode); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return chmodKeepingSetgid(path, mode)
}

// chmodKeepingSetgid applies mode to a folder karvi has just created: the
// explicit chmod defeats the umask, and it keeps the setgid bit the new
// folder inherited from a setgid parent, so a folder made inside a site's
// group directory stays in the group for what is made below it. Nothing
// else of the parent's is kept.
func chmodKeepingSetgid(path string, mode os.FileMode) error {
	if fi, err := os.Lstat(path); err == nil {
		mode |= fi.Mode() & os.ModeSetgid
	}
	return os.Chmod(path, mode)
}

// MakeDirectories is makeDirectories for a package outside osutil that
// creates a directory of its own: what it creates gets mode, and what exists
// is left exactly as it is.
func MakeDirectories(path string, mode os.FileMode) error {
	return makeDirectories(path, mode)
}

// UsersDirMode is the mode of a system root's `users` directory, the parent
// of the operators' private roots: group write
// and search so a member creates its own root, the sticky bit so no member
// removes or renames another's, nothing for others. `sudo karvi setup shared`
// makes it in the operators' group beside `shared`; karvi never makes it.
const UsersDirMode os.FileMode = 0o770

// ResolveBaseDir resolves basedir: an explicit basedir or a present
// automatic candidate must
// be a real directory the operator can write to and is accepted whatever
// its owner, group, and mode. The automatic candidates are the operator's
// own root under each system root's `users` directory, named by username
// (/opt/karvi/users/<username>, then /var/lib/karvi/users/<username>),
// then the XDG root (~/.local/share/karvi, the only one karvi makes; the
// legacy ~/karvi left the chain with the karvi line's fresh start). A
// `users` directory the site
// provisioned lets the operator's root be created there at RootMode, as
// the XDG root is created; a `users` directory that is not a real
// directory, is writable by everyone, or cannot be written by the operator
// hard-fails rather than falling through, since the site made it for this
// and a fall-through would split the operator's state across two roots.
// An absent `users` directory is passed by: only the site makes it.
func ResolveBaseDir(raw, home, username string) (string, error) {
	if raw != "" && raw != "auto" {
		p, err := expandHome(raw, home)
		if err != nil {
			return "", err
		}
		return writableDirectory(p, "private_directory_not_writable")
	}
	if username == "" || username == "." || username == ".." || strings.ContainsRune(username, '/') {
		return "", errorcodes.Errorf("operator_identity_unavailable", "the operator's username %q cannot name a private root", username)
	}
	for _, root := range SystemRoots {
		users := filepath.Join(root, "users")
		leaf := filepath.Join(users, username)
		if _, err := os.Lstat(leaf); err == nil {
			return writableDirectory(leaf, "private_directory_not_writable")
		} else if !os.IsNotExist(err) && !errors.Is(err, syscall.ENOTDIR) {
			// ENOTDIR: users is a file, which the check below names.
			return "", err
		}
		fi, err := os.Lstat(users)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		if err := checkUsersDirectory(users, fi); err != nil {
			return "", err
		}
		if err := makeDirectories(leaf, RootMode); err != nil {
			return "", errorcodes.Errorf("private_directory_not_writable", "create %s: %w", leaf, err)
		}
		return writableDirectory(leaf, "private_directory_not_writable")
	}
	xdg := filepath.Join(home, ".local/share/karvi")
	if err := makeDirectories(xdg, RootMode); err != nil {
		return "", err
	}
	return writableDirectory(xdg, "private_directory_not_writable")
}

// checkUsersDirectory is the hard check of a present `users` directory
// a real directory, not writable by everyone, and one
// the operator can create a directory in. Each refusal names what the site
// sets right.
func checkUsersDirectory(p string, fi os.FileInfo) error {
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return errorcodes.Errorf("private_directory_not_real", "%s exists and is not a real directory; the site makes it a directory in the operators' group at mode 1770 (sudo karvi setup shared)", p)
	}
	if fi.Mode().Perm()&0o002 != 0 {
		return errorcodes.Errorf("private_directory_not_writable", "%s is writable by everyone (mode %s); the site sets it to 1770 in the operators' group (sudo karvi setup shared)", p, modeString(fi.Mode()))
	}
	if err := syscall.Access(p, 0o3); err != nil { // W_OK|X_OK
		return errorcodes.Errorf("private_directory_not_writable", "%s exists and the operator cannot create a private root in it (mode %s, group %s); the site adds the operator to the group or sets the mode to 1770 (sudo karvi setup shared)", p, modeString(fi.Mode()), groupName(int(fi.Sys().(*syscall.Stat_t).Gid)))
	}
	return nil
}

// EnsureStateTree creates the required subtrees of a state root:
// logs with RootMode, socket and state with PrivateMode, and jobs and
// transcripts with outputMode (output.directory-mode). An
// existing socket or state subtree must be a real directory owned by uid with
// no group or other access; an existing logs, jobs, or transcripts subtree is
// accepted when the operator can write to it. The permissions and group of a
// path that already exists are never changed.
func EnsureStateTree(base string, uid int, outputMode os.FileMode) error {
	sub := func(name string, mode os.FileMode) (string, error) {
		p := filepath.Join(base, name)
		return p, makeDirectories(p, mode)
	}
	if p, err := sub("logs", RootMode); err != nil {
		return err
	} else if _, err := writableDirectory(p, "private_directory_not_writable"); err != nil {
		return err
	}
	for _, name := range []string{"socket", "state"} {
		p, err := sub(name, PrivateMode)
		if err != nil {
			return err
		}
		if _, err := safePrivateDirectory(p, uid, false); err != nil {
			return err
		}
	}
	if p, err := sub("jobs", outputMode); err != nil {
		return err
	} else if _, err := writableDirectory(p, "output_directory_not_writable"); err != nil {
		return err
	}
	if p, err := sub("transcripts", outputMode); err != nil {
		return err
	} else if _, err := writableDirectory(p, "transcript_directory_not_writable"); err != nil {
		return err
	}
	return nil
}

// ResolveOutputRoot, ResolveTranscriptRoot, and ResolveCrunDirectory are
// one rule (resolveUnderBase): "auto" is the subtree under the base
// directory; anything else has ~ expanded and is made absolute from the
// working directory.
// SharedRoots are the site's shared directories consulted, in order, when
// sharedroot is "auto": the `shared` directory under each of the two
// system roots of private state, so a site that keeps its private roots under
// /opt/karvi/users keeps its shared trees beside them, under
// /opt/karvi/shared. A test points the variable at a directory of its own.
var SharedRoots = []string{"/opt/karvi/shared", "/var/lib/karvi/shared"}

// SystemRoots are the two system roots under which a site
// provisions private roots as users/<username> and keeps its shared directory
// (SharedRoots). A test points the variable at a directory of its own.
var SystemRoots = []string{"/opt/karvi", "/var/lib/karvi"}

// SiteUserRoots lists the private roots a site provisioned under the
// system roots, <root>/users/<username> for every operator present, in the roots'
// order: what a root run of karvi-prune walks in place of its own basedir.
// A root under an operator's home is that
// operator's own and is not listed.
func SiteUserRoots() []string {
	var roots []string
	for _, root := range SystemRoots {
		entries, err := os.ReadDir(filepath.Join(root, "users"))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				roots = append(roots, filepath.Join(root, "users", e.Name()))
			}
		}
	}
	return roots
}

// SharedTrees are the trees a site shares under a shared root, the ones
// `karvi setup shared` creates (14.3): the job tree, the collection
// directory, and the transcript tree.
var SharedTrees = []string{"jobs", "crun", "transcripts"}

// ResolveSharedTree finds the shared tree sub. shared is
// the sharedroot setting: "auto" consults SharedRoots in order, "none"
// consults nothing, and a path consults that root. The first root holding
// sub is the tree when the operator can create files in it; one the
// operator cannot write to, or that is not a real directory, is refused
// with code and a message naming the way out (a site made the tree
// so the shift's work is in one place, so an operator outside its group is
// told, not diverted). A root without the tree is passed by, and ok is
// false when no root holds it. key names the setting that overrides the
// default in the message.
func ResolveSharedTree(shared, sub, key, code string) (path string, ok bool, err error) {
	var roots []string
	switch shared {
	case "", "auto":
		roots = SharedRoots
	case "none":
		return "", false, nil
	default:
		roots = []string{shared}
	}
	for _, root := range roots {
		p := filepath.Join(root, sub)
		fi, err := os.Lstat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", false, errorcodes.Errorf(code, "the shared %s tree %s: %w", sub, p, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return "", false, errorcodes.Errorf(code, "the shared %s tree %s exists and is not a real directory; set %s to a private path to pass it by", sub, p, key)
		}
		if err := probeWritable(p); err != nil {
			return "", false, errorcodes.Errorf(code, "the shared %s tree %s exists and the operator cannot create files in it (%v); join the group that owns it, or set %s to a private path", sub, p, err, key)
		}
		return p, true, nil
	}
	return "", false, nil
}

// ResolveOutputRoot resolves output.root: "auto" is the shared jobs tree
// when a shared root
// holds one, else <basedir>/jobs; an explicit path is that path.
func ResolveOutputRoot(raw, shared, base, home string) (string, error) {
	return resolveTree(raw, shared, base, home, "jobs", "output.root", "output_directory_not_writable")
}

// ResolveTranscriptRoot resolves transcript.root: "auto" is the shared
// transcripts tree when a shared root holds one, else <basedir>/transcripts.
func ResolveTranscriptRoot(raw, shared, base, home string) (string, error) {
	return resolveTree(raw, shared, base, home, "transcripts", "transcript.root", "transcript_directory_not_writable")
}

// ResolveCrunDirectory resolves crun.directory: "auto" is the shared crun
// tree when a shared root holds one, else <basedir>/crun.
func ResolveCrunDirectory(raw, shared, base, home string) (string, error) {
	return resolveTree(raw, shared, base, home, "crun", "crun.directory", "crun_directory_not_writable")
}

// resolveTree is the one rule of the three trees: "auto" asks the shared
// root first and falls back under basedir; an explicit path is expanded and
// made absolute.
func resolveTree(raw, shared, base, home, sub, key, code string) (string, error) {
	if raw == "" || raw == "auto" {
		if p, ok, err := ResolveSharedTree(shared, sub, key, code); err != nil {
			return "", err
		} else if ok {
			return p, nil
		}
		return filepath.Join(base, sub), nil
	}
	p, err := expandHome(raw, home)
	if err != nil {
		return "", err
	}
	return filepath.Abs(p)
}

// DayFolderMode is the mode of a day folder karvi creates under root:
// under a root that carries the setgid bit,
// the root's own permission bits, so every member of the root's group can
// create a job folder in a day folder another member made; elsewhere mode,
// output.directory-mode. Job folders take mode in every tree.
func DayFolderMode(root string, mode os.FileMode) os.FileMode {
	if fi, err := os.Stat(root); err == nil && fi.IsDir() && fi.Mode()&os.ModeSetgid != 0 {
		return fi.Mode().Perm()
	}
	return mode
}

// DayFolderLayout names the day folder of the job tree and the transcript
// tree: `YYMMDD` in the effective timezone (the earlier layout was
// `YYYY-MM-DD`), the first six characters of a job ID, so a job directory reads as one thing:
// jobs/260923/260923-215106-00.
const DayFolderLayout = "060102"

// DayFolder names the day folder of a job or transcript tree: the date of
// accepted in the effective timezone.
func DayFolder(accepted time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return accepted.In(loc).Format(DayFolderLayout)
}

// IsDayFolder says whether name is a day folder of the job or transcript
// tree: exactly DayFolderLayout, a valid date. It is
// the one matcher beside the one layout, for a walker that reads a tree
// karvi wrote (karvi-prune); a writer never needs it, since it derives the
// folder from the time. Any other name, the earlier `YYYY-MM-DD` layout
// included, is not a day folder.
func IsDayFolder(name string) bool {
	_, ok := DayFolderDate(name)
	return ok
}

// DayFolderDate parses a day folder's name as the day it belongs to, at
// midnight UTC, false when name is not a day folder. A walker compares this
// date against a retention cutoff where a folder holds no record of its own
// time (an empty day folder, a job folder without a summary; 19.4, 19.5).
func DayFolderDate(name string) (time.Time, bool) {
	if len(name) != len(DayFolderLayout) {
		return time.Time{}, false
	}
	t, err := time.Parse(DayFolderLayout, name)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// JobDirectory is <root>/YYMMDD/<id>.
func JobDirectory(root, id string, accepted time.Time, loc *time.Location) string {
	return filepath.Join(root, DayFolder(accepted, loc), id)
}

// EnsureOutputDirectory prepares a folder in the job or transcript tree:
// missing components are created with mode, an existing
// leaf is accepted when the operator can create files in it, and nothing that
// exists is changed. code names the not-writable failure
// (output_directory_not_writable or transcript_directory_not_writable).
func EnsureOutputDirectory(path string, mode os.FileMode, code string) error {
	if err := makeDirectories(path, mode); err != nil {
		if pe, ok := err.(*os.PathError); ok && pe.Err == syscall.ENOTDIR {
			return errorcodes.Errorf(code, "%s exists and is not a folder", pe.Path)
		}
		if os.IsPermission(err) {
			return errorcodes.Errorf(code, "cannot create %s: %w", path, err)
		}
		return err
	}
	_, err := writableDirectory(path, code)
	return err
}

// CreateExclusive creates a file in the job or transcript tree:
// exclusively, without following a symbolic link at path, with
// OutputFileMode applied explicitly so the umask cannot narrow it. flags adds
// to O_WRONLY (for example os.O_APPEND).
func CreateExclusive(path string, flags int) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW|flags, OutputFileMode)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(OutputFileMode); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return f, nil
}

// ResolveScratch resolves tempdir, the scratch directory of the askpass
// socket and the system transport's ssh configuration: "auto"
// is the chain /dev/shm/karvi/<username>, <basedir>/tmp, /tmp/karvi-<uid>,
// /var/tmp/karvi-<uid>, the first that firstWritableDirectory accepts; an
// explicit path replaces the chain. The output spool never lives here
// (ResolveSpoolDir).
func ResolveScratch(raw, base, home, username string, uid int) (string, error) {
	if raw != "" && raw != "auto" {
		p, err := expandHome(raw, home)
		if err != nil {
			return "", err
		}
		return firstWritableDirectory([]string{p}, "scratch_directory_unavailable", "no writable scratch directory")
	}
	own := fmt.Sprintf("karvi-%d", uid)
	candidates := []string{filepath.Join("/dev/shm/karvi", username), filepath.Join(base, "tmp"), filepath.Join("/tmp", own), filepath.Join("/var/tmp", own)}
	return firstWritableDirectory(candidates, "scratch_directory_unavailable", "no writable scratch directory")
}

func ControlPathRoot(raw, base, username string, uid int) (string, error) {
	if raw != "" && raw != "auto" {
		p, err := expandHome(raw, filepath.Dir(filepath.Dir(base)))
		if err != nil {
			return "", err
		}
		return safePrivateDirectory(p, uid, true)
	}
	preferred := filepath.Join("/dev/shm/karvi", username, "sockets")
	if p, err := safePrivateDirectory(preferred, uid, true); err == nil {
		return p, nil
	}
	return safePrivateDirectory(filepath.Join(base, "socket", "ssh"), uid, true)
}
func DaemonSocket(raw, base string) string {
	if raw == "" || raw == "auto" {
		return filepath.Join(base, "socket", "daemon.sock")
	}
	return raw
}

// EnsureCollectionDirectory prepares a crun's collection directory once,
// before any device is contacted: missing,
// it is created with mode (crun.directory-mode); present, it must be a
// directory the operator can create files in, left exactly as it is in
// mode, group, and setgid bit; and one with the sticky bit that the
// operator does not own is refused, since the kernel would refuse every
// rename over another operator's file one device at a time. The message
// names the shape a shared directory needs.
func EnsureCollectionDirectory(path string, mode os.FileMode) error {
	const code = "crun_directory_not_writable"
	if err := EnsureOutputDirectory(path, mode, code); err != nil {
		return errorcodes.Errorf(code, "%s; %s", strings.TrimPrefix(err.Error(), code+": "), collectionDirectoryShape)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return errorcodes.Errorf(code, "stat %s: %v; %s", path, err, collectionDirectoryShape)
	}
	if fi.Mode()&os.ModeSticky != 0 {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
			return errorcodes.Errorf(code, "%s has the sticky bit and is owned by uid %d, not %d: a collection file another operator wrote could not be replaced; %s", path, st.Uid, os.Geteuid(), collectionDirectoryShape)
		}
	}
	return nil
}

const collectionDirectoryShape = "a shared collection directory needs mode 2770 or 2775 (group write and search, setgid, no sticky bit)"

// ResolveSpoolDir resolves spooldir, the directory of the output spools:
// a spool is a file of one in-flight
// command's recorded bytes past output.spool-threshold-bytes, infrequent and
// possibly large, and must cost disk, never memory. It is therefore not the
// scratch directory: tempdir's auto chain prefers /dev/shm, a tmpfs, by
// design (small, fast, cleared at every reboot), and the two
// keys stay separate so that a site that wants both on one volume says so
// with explicit paths. "auto" tries /tmp/karvi-<uid>, then
// /var/tmp/karvi-<uid>, each made 0700 for the effective uid, a real
// directory and not a link, probed with one file as the scratch chain
// probes (firstWritableDirectory); an explicit path (~ expanded) replaces
// the list. No candidate writable is spool_directory_unavailable naming
// what was tried, reported at the job's admission before any device is
// contacted.
func ResolveSpoolDir(raw, home string, uid int) (string, error) {
	if raw != "" && raw != "auto" {
		p, err := expandHome(raw, home)
		if err != nil {
			return "", err
		}
		return firstWritableDirectory([]string{p}, "spool_directory_unavailable", "no writable spool directory")
	}
	own := fmt.Sprintf("karvi-%d", uid)
	candidates := make([]string, 0, len(SpoolRoots))
	for _, root := range SpoolRoots {
		candidates = append(candidates, filepath.Join(root, own))
	}
	return firstWritableDirectory(candidates, "spool_directory_unavailable", "no writable spool directory")
}

// SpoolRoots are the parents of spooldir's auto chain, /tmp then /var/tmp,
// each holding karvi-<uid>. A test points the
// variable at a directory of its own (osutiltest.Isolate), since the chain
// is a place of the host's, shared with the operator's real daemon.
var SpoolRoots = []string{"/tmp", "/var/tmp"}

// firstWritableDirectory is the one probe loop of the scratch chain and the
// spool chain: each candidate is created 0700 when missing, must be a real
// directory and not a link, and must take one temporary file; the first
// that does is the answer, and none is the error code with message.
func firstWritableDirectory(candidates []string, code, message string) (string, error) {
	for _, child := range candidates {
		if err := os.MkdirAll(child, 0700); err != nil {
			continue
		}
		fi, err := os.Lstat(child)
		if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if probeWritable(child) != nil {
			continue
		}
		return child, nil
	}
	return "", errorcodes.Errorf(code, "%s: tried %s", message, strings.Join(candidates, ", "))
}
