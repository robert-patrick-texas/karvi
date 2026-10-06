package osutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// ResolvePath is the one rule of every place key and every file key: `~`
// and `~/…` are the operator's home, home, the password database's and
// never $HOME; `~user` is refused (path_other_user_home_unsupported); a
// relative path is taken from the working directory, the invoking
// client's, and made absolute. The result is clean. An empty value is
// unset and stays empty: the caller decides what an unset key means.
func ResolvePath(raw, home string) (string, error) {
	if raw == "" {
		return "", nil
	}
	p := raw
	switch {
	case raw == "~" || strings.HasPrefix(raw, "~/"):
		if home == "" {
			return "", errorcodes.Errorf("operator_identity_unavailable", "the operator's home is unknown, so %s cannot be resolved", raw)
		}
		p = filepath.Join(home, strings.TrimPrefix(raw[1:], "/"))
	case strings.HasPrefix(raw, "~"):
		return "", errorcodes.Errorf("path_other_user_home_unsupported", "~otheruser paths are not supported: %s", raw)
	}
	return filepath.Abs(p)
}

// Passed is a candidate of a chain that is present on the host and was
// passed by, with the reason: the activity's own words where it warns,
// else the judgement's (`config show --explain` prints it on a `passed:`
// line).
type Passed struct {
	Path, Reason string
}

// Place is where a chain resolves, found without creating anything: Path
// the candidate the activity would take, Passed the candidates before it
// that are present and were passed by. An absent candidate passed by is not
// listed: absence is a host's ordinary state, and the activity passes it in
// silence too.
type Place struct {
	Path   string
	Passed []Passed
}

// setupPlaces are the directories `sudo karvi setup shared` makes and no
// operator's run does: the scratch root, each system root with its `users`
// and `shared`, and the trees under each shared root.
func setupPlaces() []string {
	places := []string{ScratchRoot}
	shared := append([]string(nil), SharedRoots...)
	for _, root := range SystemRoots {
		places = append(places, root, filepath.Join(root, "users"))
		shared = append(shared, filepath.Join(root, "shared"))
	}
	for _, s := range shared {
		places = append(places, s)
		for _, tree := range SharedTrees {
			places = append(places, filepath.Join(s, tree))
		}
	}
	return places
}

// CheckSetupPlaces is the one guard of the places setup shared makes: it
// finds the components of path that do not exist and refuses, with
// shared_directory_absent, a path whose making would make one of them,
// naming the highest such place, `sudo karvi setup shared`, and key, the
// setting to point elsewhere ("" where the caller has none to name). It
// reads the file system and changes nothing; makeDirectories calls it
// before it makes anything, and each chain's chooser calls it on an
// explicit path, so the refusal comes before any device is contacted and
// `config show --explain` prints it.
func CheckSetupPlaces(path, key string) error {
	places := setupPlaces()
	refused := ""
	for q := filepath.Clean(path); ; q = filepath.Dir(q) {
		if _, err := os.Lstat(q); err == nil || !os.IsNotExist(err) {
			break
		}
		for _, place := range places {
			if q == filepath.Clean(place) {
				refused = q
			}
		}
		if filepath.Dir(q) == q {
			break
		}
	}
	if refused == "" {
		return nil
	}
	elsewhere := "point the setting that names " + path + " elsewhere"
	if key != "" {
		elsewhere = "set " + key + " to a path elsewhere"
	}
	return errorcodes.Errorf("shared_directory_absent", "%s is absent, and only `sudo karvi setup shared` makes it, never an operator's run: the site runs that, or %s", refused, elsewhere)
}

// statfs reads a filesystem's counts for the inode judgement; a test
// replaces it.
var statfs = syscall.Statfs

// noFreeInodes says whether path's filesystem counts inodes and has none
// free, where a directory or a probe file cannot be made whatever the
// permissions say (a full /tmp moved the spool to /var/tmp).
func noFreeInodes(path string) bool {
	var st syscall.Statfs_t
	if statfs(path, &st) != nil {
		return false
	}
	return st.Files > 0 && st.Ffree == 0
}

// The reasons a present candidate is passed by, in the judgement's words.
const (
	reasonNotReal     = "not a real directory"
	reasonNotWritable = "not writable by the operator"
	reasonNoInodes    = "no free inodes"
)

// judgeDirectory is the judgement of a candidate the operator writes in,
// as the activity's maker would find it, without writing: present says
// whether something is at path, and reason is empty when the activity can
// take the candidate. A present candidate must be a real directory, not a
// link, that the operator can write and search (access(2)), on a
// filesystem with free inodes. An absent one is taken where the maker can
// make it: the nearest existing ancestor a directory the operator can
// write and search, on a filesystem with free inodes, and no place setup
// shared makes among the missing components.
func judgeDirectory(path string) (present bool, reason string) {
	fi, err := os.Lstat(path)
	if err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return true, reasonNotReal
		}
		if syscall.Access(path, 0o3) != nil { // W_OK|X_OK
			return true, reasonNotWritable
		}
		if noFreeInodes(path) {
			return true, reasonNoInodes
		}
		return true, ""
	}
	if !os.IsNotExist(err) {
		return true, reasonNotWritable
	}
	return false, makeableReason(path)
}

// makeableReason says why an absent path cannot be made by the operator,
// empty when it can (judgeDirectory).
func makeableReason(path string) string {
	if CheckSetupPlaces(path, "") != nil {
		return "a place setup shared makes"
	}
	a := filepath.Dir(path)
	for {
		fi, err := os.Stat(a)
		if err == nil {
			switch {
			case !fi.IsDir():
				return reasonNotReal
			case syscall.Access(a, 0o3) != nil:
				return reasonNotWritable
			case noFreeInodes(a):
				return reasonNoInodes
			}
			return ""
		}
		if !os.IsNotExist(err) || filepath.Dir(a) == a {
			return reasonNotWritable
		}
		a = filepath.Dir(a)
	}
}

// chain is a list of candidates the operator writes in, the first usable
// taken (the scratch and the spool), with the code and message of the
// refusal when none is.
type chain struct {
	candidates    []string
	code, message string
}

// place is the chain's twin: what the maker takes, by judgeDirectory,
// creating nothing.
func (c chain) place() (Place, error) {
	var pl Place
	for _, p := range c.candidates {
		present, reason := judgeDirectory(p)
		if reason == "" {
			pl.Path = p
			return pl, nil
		}
		if present {
			pl.Passed = append(pl.Passed, Passed{Path: p, Reason: reason})
		}
	}
	return pl, c.refusal()
}

// make is the chain's maker: each candidate the judgement accepts is made
// 0700 when missing, must be a real directory and not a link, and must
// take one temporary file, the activity's real test; the first that does
// is the answer.
func (c chain) make() (string, error) {
	for _, p := range c.candidates {
		if _, reason := judgeDirectory(p); reason != "" {
			continue
		}
		if err := makeDirectories(p, PrivateMode); err != nil {
			continue
		}
		fi, err := os.Lstat(p)
		if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if probeWritable(p) != nil {
			continue
		}
		return p, nil
	}
	return "", c.refusal()
}

func (c chain) refusal() error {
	return errorcodes.Errorf(c.code, "%s: tried %s", c.message, strings.Join(c.candidates, ", "))
}

// explicitChain is the chain of an explicit value: the one path
// (explicitPath).
func explicitChain(raw, home, key, code, message string) (chain, error) {
	p, err := explicitPath(raw, home, key)
	if err != nil {
		return chain{}, err
	}
	return chain{candidates: []string{p}, code: code, message: message}, nil
}

// explicitPath is an explicit place key's path: resolved by ResolvePath,
// and refused when making it would make a place setup shared makes.
func explicitPath(raw, home, key string) (string, error) {
	p, err := ResolvePath(raw, home)
	if err != nil {
		return "", err
	}
	if err := CheckSetupPlaces(p, key); err != nil {
		return "", err
	}
	return p, nil
}

// ScratchFolder is the chain of a folder of the scratch root that every
// operator shares, the scoreboards and the capacity ledger: Path the
// candidate tried first, made in an existing parent (MakeSharedDirectory),
// "" when the chain has none; Fallback the operator's private folder
// under basedir, "" for an explicit path, which is used or refused.
type ScratchFolder struct {
	Path, Fallback string
}

// ScratchFolderChoice is that chain for key, whose folder in the scratch
// root and under <basedir>/state is name: "auto" is <ScratchRoot>/name
// where the scratch root exists, else <basedir>/state/name; an explicit
// path (explicitPath) replaces both. It reads the file system and changes
// nothing: an operator's run never makes the scratch root, so without it
// the private folder is taken without a word.
func ScratchFolderChoice(raw, home, key, name, base string) (ScratchFolder, error) {
	if raw != "" && raw != "auto" {
		p, err := explicitPath(raw, home, key)
		return ScratchFolder{Path: p}, err
	}
	fallback, err := filepath.Abs(filepath.Join(base, "state", name))
	if err != nil {
		return ScratchFolder{}, err
	}
	if scratchRootPresent() {
		return ScratchFolder{Path: filepath.Join(ScratchRoot, name), Fallback: fallback}, nil
	}
	return ScratchFolder{Fallback: fallback}, nil
}

// Place is the chain's twin: the folder the activity would take, judged
// by judgeDirectory and by judge, the folder's own rule ("" when the
// folder passes it; nil for none), creating nothing. The shared folder
// present and passed by is listed with its reason, and the private folder
// taken; an explicit path passed by is refused with code.
func (f ScratchFolder) Place(judge func(string) string, code string) (Place, error) {
	var pl Place
	if f.Path != "" {
		present, reason := judgeDirectory(f.Path)
		if reason == "" && present && judge != nil {
			reason = judge(f.Path)
		}
		if reason == "" {
			pl.Path = f.Path
			return pl, nil
		}
		if f.Fallback == "" {
			return pl, errorcodes.Errorf(code, "%s: %s", f.Path, reason)
		}
		if present {
			pl.Passed = append(pl.Passed, Passed{Path: f.Path, Reason: reason})
		}
	}
	pl.Path = f.Fallback
	return pl, nil
}

// UsableDirectory is the activity's test of a folder it writes in, once
// made: a real directory, not a link, that the operator can write and
// search (access(2)). The error is the bare cause, for the caller's
// message that names the folder.
func UsableDirectory(p string) error {
	fi, err := os.Lstat(p)
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			return pe.Err
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return syscall.ENOTDIR
	}
	return syscall.Access(p, 0o3) // W_OK|X_OK
}
