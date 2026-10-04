package osutil

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// withUmask runs fn under a restrictive umask so the tests prove the modes are
// applied explicitly.
func withUmask(t *testing.T, fn func()) {
	t.Helper()
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	fn()
}

func TestFileMode(t *testing.T) {
	for value, want := range map[string]os.FileMode{"0640": 0o640, "0644": 0o644, "0660": 0o660, "": 0o660, "bogus": 0o660} {
		if got := FileMode(value); got != want {
			t.Fatalf("%q: %o", value, got)
		}
	}
}

// TestMakeDirectoriesKeepsSetgid: a
// folder created inside a setgid folder keeps the inherited bit (the kernel
// sets it on the new folder; the explicit chmod used to clear it), and a
// folder created elsewhere gains none.
func TestMakeDirectoriesKeepsSetgid(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o770); err != nil {
		t.Fatal(err)
	}
	// os.FileMode carries the bit as ModeSetgid, not as an octal 2000.
	if err := os.Chmod(shared, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(shared, "day", "job")
	withUmask(t, func() {
		if err := MakeDirectories(inside, 0o750); err != nil {
			t.Fatal(err)
		}
	})
	for _, path := range []string{filepath.Join(shared, "day"), inside} {
		fi, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode()&os.ModeSetgid == 0 || fi.Mode().Perm() != 0o750 {
			t.Fatalf("%s: %v, want setgid and 0750", path, fi.Mode())
		}
	}
	plain := filepath.Join(root, "plain", "job")
	if err := MakeDirectories(plain, 0o750); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(plain); fi.Mode()&os.ModeSetgid != 0 {
		t.Fatalf("a folder outside a setgid folder gained the bit: %v", fi.Mode())
	}
}

func TestDirectoryMode(t *testing.T) {
	for value, want := range map[string]os.FileMode{"0700": 0o700, "0750": 0o750, "0755": 0o755, "0770": 0o770, "": 0o750, "bogus": 0o750} {
		if got := DirectoryMode(value); got != want {
			t.Errorf("DirectoryMode(%q)=%o, want %o", value, got, want)
		}
	}
}

// TestEnsureStateTreeModes covers the modes of the state root's subtrees.
func TestEnsureStateTreeModes(t *testing.T) {
	base := t.TempDir()
	withUmask(t, func() {
		if err := EnsureStateTree(base, os.Getuid(), 0o770); err != nil {
			t.Fatal(err)
		}
	})
	for name, want := range map[string]os.FileMode{"logs": 0o750, "socket": 0o700, "state": 0o700, "jobs": 0o770, "transcripts": 0o770} {
		if got := mode(t, filepath.Join(base, name)); got != want {
			t.Errorf("%s mode %o, want %o", name, got, want)
		}
	}
	// An existing jobs subtree keeps its permissions; an existing state
	// subtree must stay private.
	if err := os.Chmod(filepath.Join(base, "jobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureStateTree(base, os.Getuid(), 0o750); err != nil {
		t.Fatal(err)
	}
	if got := mode(t, filepath.Join(base, "jobs")); got != 0o755 {
		t.Fatalf("existing jobs subtree was changed to %o", got)
	}
	if err := os.Chmod(filepath.Join(base, "state"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := EnsureStateTree(base, os.Getuid(), 0o750); errorcodes.Of(err) != "private_directory_mode_exposed" {
		t.Fatalf("group-readable state subtree accepted: %v", err)
	}
}

// TestResolveBaseDirAcceptsSharedRoot: an explicit basedir is accepted
// whatever its owner, group, and mode.
func TestResolveBaseDirAcceptsSharedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(root, 0o775); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveBaseDir(root, "", "op"); err != nil {
		t.Fatalf("group-writable root refused: %v", err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveBaseDir(file, "", "op"); errorcodes.Of(err) != "private_directory_not_real" {
		t.Fatalf("a file as root: %v", err)
	}
	home := t.TempDir()
	var xdg string
	var err error
	withUmask(t, func() { xdg, err = ResolveBaseDir("auto", home, "op") })
	if err != nil || mode(t, xdg) != 0o750 {
		t.Fatalf("auto-created XDG root: %v mode %o", err, mode(t, xdg))
	}
}

// TestResolveBaseDirUnderUsers: the operator's
// root under a system root's users directory, named by username: created
// at RootMode when the site provisioned users; an existing one taken as it
// is; an absent users directory passed by to the XDG root; a users
// directory that is not a real directory, is writable by everyone, or
// cannot be written by the operator hard-fails.
// BaseDirPath names what ResolveBaseDir picks and makes nothing: the XDG
// root, then an operator's folder a users directory would receive, and the
// refusal of a users directory that is not right.
func TestBaseDirPathCreatesNothing(t *testing.T) {
	saved := SystemRoots
	t.Cleanup(func() { SystemRoots = saved })
	opt := filepath.Join(t.TempDir(), "opt", "karvi")
	SystemRoots = []string{opt}
	home := t.TempDir()
	got, err := BaseDirPath("auto", home, "alice")
	if err != nil || got != filepath.Join(home, ".local/share/karvi") {
		t.Fatalf("no users directory: %q %v", got, err)
	}
	if _, err := os.Lstat(got); !os.IsNotExist(err) {
		t.Fatalf("the XDG root was made: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(opt, "users"), 0o770); err != nil {
		t.Fatal(err)
	}
	got, err = BaseDirPath("auto", home, "alice")
	if err != nil || got != filepath.Join(opt, "users", "alice") {
		t.Fatalf("under users: %q %v", got, err)
	}
	if _, err := os.Lstat(got); !os.IsNotExist(err) {
		t.Fatalf("the operator's folder was made: %v", err)
	}
	if made, err := ResolveBaseDir("auto", home, "alice"); err != nil || made != got {
		t.Fatalf("ResolveBaseDir took %q (%v), BaseDirPath named %q", made, err, got)
	}
	if err := os.Chmod(filepath.Join(opt, "users"), 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := BaseDirPath("auto", home, "bob"); errorcodes.Of(err) != "private_directory_not_writable" {
		t.Fatalf("a users directory writable by everyone: %v", err)
	}
	if got, err := BaseDirPath("~/root", home, "alice"); err != nil || got != filepath.Join(home, "root") {
		t.Fatalf("explicit: %q %v", got, err)
	}
}

func TestResolveBaseDirUnderUsers(t *testing.T) {
	saved := SystemRoots
	t.Cleanup(func() { SystemRoots = saved })
	opt, lib := filepath.Join(t.TempDir(), "opt", "karvi"), filepath.Join(t.TempDir(), "var", "lib", "karvi")
	SystemRoots = []string{opt, lib}
	home := t.TempDir()
	// No users directory anywhere: the XDG root, as before.
	got, err := ResolveBaseDir("auto", home, "alice")
	if err != nil || got != filepath.Join(home, ".local/share/karvi") {
		t.Fatalf("no users directory: %q %v", got, err)
	}
	// The second root's users directory alone: the root is made there.
	if err := os.MkdirAll(filepath.Join(lib, "users"), 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(lib, "users"), os.ModeSticky|0o770); err != nil {
		t.Fatal(err)
	}
	withUmask(t, func() { got, err = ResolveBaseDir("auto", home, "alice") })
	if err != nil || got != filepath.Join(lib, "users", "alice") || mode(t, got) != 0o750 {
		t.Fatalf("created under users: %q %v mode %o", got, err, mode(t, got))
	}
	// An existing root is taken as it is, whatever its mode.
	if err := os.Chmod(got, 0o700); err != nil {
		t.Fatal(err)
	}
	if again, err := ResolveBaseDir("auto", home, "alice"); err != nil || again != got {
		t.Fatalf("existing root: %q %v", again, err)
	}
	// The first root's users directory wins once it exists.
	if err := os.MkdirAll(filepath.Join(opt, "users"), 0o770); err != nil {
		t.Fatal(err)
	}
	if got, err = ResolveBaseDir("auto", home, "alice"); err != nil || got != filepath.Join(opt, "users", "alice") {
		t.Fatalf("the first root: %q %v", got, err)
	}
	// A users directory writable by everyone hard-fails, naming the mode.
	if err := os.Chmod(filepath.Join(opt, "users"), 0o777); err != nil {
		t.Fatal(err)
	}
	os.Remove(got)
	if _, err := ResolveBaseDir("auto", home, "bob"); errorcodes.Of(err) != "private_directory_not_writable" || !strings.Contains(err.Error(), "writable by everyone (mode 0777)") {
		t.Fatalf("world-writable users: %v", err)
	}
	// One the operator cannot create in hard-fails too, and does not fall
	// through to the second root.
	if err := os.Chmod(filepath.Join(opt, "users"), 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveBaseDir("auto", home, "bob"); errorcodes.Of(err) != "private_directory_not_writable" || !strings.Contains(err.Error(), "cannot create a private root in it") {
		t.Fatalf("unwritable users: %v", err)
	}
	os.Chmod(filepath.Join(opt, "users"), 0o770)
	// A file where users should be is not a real directory.
	os.RemoveAll(filepath.Join(opt, "users"))
	if err := os.WriteFile(filepath.Join(opt, "users"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveBaseDir("auto", home, "bob"); errorcodes.Of(err) != "private_directory_not_real" {
		t.Fatalf("a file as users: %v", err)
	}
	// A username that cannot name a directory is refused before any look.
	for _, name := range []string{"", ".", "..", "a/b"} {
		if _, err := ResolveBaseDir("auto", home, name); errorcodes.Of(err) != "operator_identity_unavailable" {
			t.Errorf("username %q: %v", name, err)
		}
	}
}

// TestEnsureOutputDirectory covers the job and transcript tree folders.
func TestEnsureOutputDirectory(t *testing.T) {
	root := t.TempDir()
	job := filepath.Join(root, "2026-09-14", "job1")
	withUmask(t, func() {
		if err := EnsureOutputDirectory(job, 0o750, "output_directory_not_writable"); err != nil {
			t.Fatal(err)
		}
	})
	if mode(t, filepath.Join(root, "2026-09-14")) != 0o750 || mode(t, job) != 0o750 {
		t.Fatalf("created folders: %o %o", mode(t, filepath.Join(root, "2026-09-14")), mode(t, job))
	}
	// An existing day folder with any permissions is accepted when writable and
	// never changed.
	if err := os.Chmod(filepath.Join(root, "2026-09-14"), 0o2770); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOutputDirectory(filepath.Join(root, "2026-09-14", "job2"), 0o700, "output_directory_not_writable"); err != nil {
		t.Fatal(err)
	}
	if got := mode(t, filepath.Join(root, "2026-09-14")); got != 0o770 {
		t.Fatalf("existing day folder changed to %o", got)
	}
	if got := mode(t, filepath.Join(root, "2026-09-14", "job2")); got != 0o700 {
		t.Fatalf("job2 mode %o", got)
	}
	// A path component that is a file is refused with the caller's code.
	if err := os.WriteFile(filepath.Join(root, "2026-09-15"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOutputDirectory(filepath.Join(root, "2026-09-15", "job3"), 0o750, "output_directory_not_writable"); errorcodes.Of(err) != "output_directory_not_writable" {
		t.Fatalf("file in the path: %v", err)
	}
	if os.Geteuid() == 0 {
		return
	}
	locked := filepath.Join(root, "2026-09-16")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOutputDirectory(locked, 0o750, "transcript_directory_not_writable"); errorcodes.Of(err) != "transcript_directory_not_writable" {
		t.Fatalf("unwritable existing folder: %v", err)
	}
	if err := EnsureOutputDirectory(filepath.Join(locked, "job4"), 0o750, "output_directory_not_writable"); errorcodes.Of(err) != "output_directory_not_writable" {
		t.Fatalf("cannot create below an unwritable folder: %v", err)
	}
}

// TestCreateExclusive covers exclusive file creation in the trees.
func TestCreateExclusive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.jsonl")
	withUmask(t, func() {
		f, err := CreateExclusive(path, os.O_APPEND)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	})
	if got := mode(t, path); got != 0o640 {
		t.Fatalf("file mode %o, want 0640", got)
	}
	if _, err := CreateExclusive(path, 0); !os.IsExist(err) {
		t.Fatalf("second create must fail exclusively: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateExclusive(link, 0); err == nil {
		t.Fatal("a link at the path must not be followed")
	}
	if _, err := os.Lstat(filepath.Join(dir, "elsewhere")); !os.IsNotExist(err) {
		t.Fatal("the link target was created")
	}
}

// TestJobDirectoryUsesEffectiveTimezone: the day is the
// acceptance date in the effective timezone, here on either side of midnight.
func TestJobDirectoryUsesEffectiveTimezone(t *testing.T) {
	accepted := time.Date(2026, 9, 14, 3, 30, 0, 0, time.UTC)
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	if got := JobDirectory("/jobs", "id1", accepted, time.UTC); got != "/jobs/260914/id1" {
		t.Fatalf("UTC: %s", got)
	}
	if got := JobDirectory("/jobs", "id1", accepted, ny); got != "/jobs/260913/id1" {
		t.Fatalf("New York: %s", got)
	}
}

// TestResolveCrunDirectory: auto is
// <basedir>/crun, ~ is the home, a relative path is from the working
// directory; the three roots share one rule.
func TestResolveCrunDirectory(t *testing.T) {
	got, err := ResolveCrunDirectory("auto", "none", "/var/lib/karvi", "/home/x")
	if err != nil || got != "/var/lib/karvi/crun" {
		t.Fatalf("auto: %q %v", got, err)
	}
	if got, _ = ResolveCrunDirectory("~/configs", "none", "/var/lib/karvi", "/home/x"); got != "/home/x/configs" {
		t.Fatalf("home: %q", got)
	}
	if got, _ = ResolveCrunDirectory("configs", "none", "/var/lib/karvi", "/home/x"); !filepath.IsAbs(got) || filepath.Base(got) != "configs" {
		t.Fatalf("relative: %q", got)
	}
	if got, _ = ResolveOutputRoot("auto", "none", "/var/lib/karvi", "/home/x"); got != "/var/lib/karvi/jobs" {
		t.Fatalf("the job root: %q", got)
	}
}

// TestResolveSharedTrees: with sharedroot
// auto the first shared root holding the tree is the default of the three
// settings; a root without the tree is passed by and the tree falls back
// under basedir; a tree the operator cannot write to, or a file in its
// place, is refused with the tree's code and a message naming the way out;
// "none" consults nothing; a path consults that root; an explicit setting
// never asks.
func TestResolveSharedTrees(t *testing.T) {
	first, second, base := t.TempDir(), t.TempDir(), t.TempDir()
	saved := SharedRoots
	SharedRoots = []string{first, second}
	t.Cleanup(func() { SharedRoots = saved })
	for _, sub := range []string{"jobs", "transcripts"} {
		if err := os.Mkdir(filepath.Join(second, sub), 0o770); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(first, "jobs"), 0o770); err != nil {
		t.Fatal(err)
	}
	if got, err := ResolveOutputRoot("auto", "auto", base, "/home/x"); err != nil || got != filepath.Join(first, "jobs") {
		t.Fatalf("jobs: %q %v, want the first root's", got, err)
	}
	if got, err := ResolveTranscriptRoot("auto", "auto", base, "/home/x"); err != nil || got != filepath.Join(second, "transcripts") {
		t.Fatalf("transcripts: %q %v, want the second root's (the first has none)", got, err)
	}
	if got, err := ResolveCrunDirectory("auto", "auto", base, "/home/x"); err != nil || got != filepath.Join(base, "crun") {
		t.Fatalf("crun: %q %v, want basedir's (no root has one)", got, err)
	}
	if got, err := ResolveOutputRoot("auto", "none", base, "/home/x"); err != nil || got != filepath.Join(base, "jobs") {
		t.Fatalf("none: %q %v", got, err)
	}
	if got, err := ResolveOutputRoot("auto", second, base, "/home/x"); err != nil || got != filepath.Join(second, "jobs") {
		t.Fatalf("a path as sharedroot: %q %v", got, err)
	}
	if got, err := ResolveOutputRoot("/explicit", "auto", base, "/home/x"); err != nil || got != "/explicit" {
		t.Fatalf("explicit output.root: %q %v", got, err)
	}
	if err := os.Chmod(filepath.Join(first, "jobs"), 0o550); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveOutputRoot("auto", "auto", base, "/home/x")
	if errorcodes.Of(err) != "output_directory_not_writable" || !strings.Contains(err.Error(), "join the group") || !strings.Contains(err.Error(), "output.root") {
		t.Fatalf("an unwritable shared tree: %v", err)
	}
	os.Chmod(filepath.Join(first, "jobs"), 0o770)
	if err := os.WriteFile(filepath.Join(first, "crun"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveCrunDirectory("auto", "auto", base, "/home/x"); errorcodes.Of(err) != "crun_directory_not_writable" || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("a file in the tree's place: %v", err)
	}
}

// TestDayFolderMode: under a setgid
// root the day folder takes the root's permission bits and the job folder
// keeps output.directory-mode; under a plain root both take the mode.
func TestDayFolderMode(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(shared, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	if got := DayFolderMode(shared, 0o750); got != 0o770 {
		t.Fatalf("setgid root: %o, want 0770", got)
	}
	plain := t.TempDir()
	if got := DayFolderMode(plain, 0o750); got != 0o750 {
		t.Fatalf("plain root: %o, want 0750", got)
	}
	var dir string
	var err error
	withUmask(t, func() { _, dir, err = ReserveJobID(shared, jobTestNow, time.UTC, 0o750) })
	if err != nil {
		t.Fatal(err)
	}
	day := filepath.Dir(dir)
	if m := mode(t, day); m != 0o770 {
		t.Fatalf("day folder %s: %o, want the root's 0770", day, m)
	}
	if fi, _ := os.Lstat(day); fi.Mode()&os.ModeSetgid == 0 {
		t.Fatalf("day folder lost the setgid bit")
	}
	if m := mode(t, dir); m != 0o750 {
		t.Fatalf("job folder %s: %o, want 0750", dir, m)
	}
	withUmask(t, func() { _, dir, err = ReserveJobID(plain, jobTestNow, time.UTC, 0o750) })
	if err != nil || mode(t, filepath.Dir(dir)) != 0o750 {
		t.Fatalf("plain day folder: %v %o", err, mode(t, filepath.Dir(dir)))
	}
}

// TestEnsureCollectionDirectory: a
// missing directory is created at the mode; an existing one is left as it
// is; a file in its place and an unwritable one are refused with the code
// and the shape a shared directory needs; the sticky bit on the operator's
// own directory passes (the refusal is for another owner's).
func TestEnsureCollectionDirectory(t *testing.T) {
	root := t.TempDir()
	made := filepath.Join(root, "crun")
	withUmask(t, func() {
		if err := EnsureCollectionDirectory(made, 0o770); err != nil {
			t.Fatal(err)
		}
	})
	if mode(t, made) != 0o770 {
		t.Fatalf("created at %o", mode(t, made))
	}
	if err := os.Chmod(made, os.ModeSetgid|0o750); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCollectionDirectory(made, 0o770); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(made); fi.Mode().Perm() != 0o750 || fi.Mode()&os.ModeSetgid == 0 {
		t.Fatalf("an existing directory changed: %v", fi.Mode())
	}
	if err := os.Chmod(made, os.ModeSticky|0o770); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCollectionDirectory(made, 0o770); err != nil {
		t.Fatalf("the operator's own sticky directory: %v", err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := EnsureCollectionDirectory(file, 0o770)
	if errorcodes.Of(err) != "crun_directory_not_writable" || !strings.Contains(err.Error(), "2770 or 2775") {
		t.Fatalf("a file in the way: %v", err)
	}
	if os.Geteuid() == 0 {
		return
	}
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCollectionDirectory(locked, 0o770); errorcodes.Of(err) != "crun_directory_not_writable" || !strings.Contains(err.Error(), "2770 or 2775") {
		t.Fatalf("unwritable: %v", err)
	}
}

// TestIsDayFolder: the one matcher accepts exactly the YYMMDD layout as a
// valid date and nothing else, the earlier YYYY-MM-DD layout included.
func TestIsDayFolder(t *testing.T) {
	for _, name := range []string{"260926", "000101", "991231"} {
		if !IsDayFolder(name) {
			t.Fatalf("%q must be a day folder", name)
		}
	}
	for _, name := range []string{"2026-09-26", "26092", "2609261", "261340", "260932", "26092a", "", "260926-153859-00", "2026", "..", "jobs"} {
		if IsDayFolder(name) {
			t.Fatalf("%q must not be a day folder", name)
		}
	}
	d, ok := DayFolderDate("260817")
	if !ok || !d.Equal(time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("DayFolderDate(260817) = %v %v", d, ok)
	}
	if DayFolder(time.Date(2026, 8, 17, 23, 0, 0, 0, time.UTC), time.UTC) != "260817" {
		t.Fatal("DayFolder and DayFolderDate disagree on the layout")
	}
}

// TestSiteUserRoots: the private roots a site
// provisioned under the system roots, in the roots' order, directories
// only; a system root without a users directory contributes nothing.
func TestSiteUserRoots(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	saved := SystemRoots
	SystemRoots = []string{a, b, filepath.Join(a, "absent")}
	t.Cleanup(func() { SystemRoots = saved })
	for _, p := range []string{filepath.Join(a, "users", "1000"), filepath.Join(a, "users", "1001"), filepath.Join(b, "users", "1002")} {
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a, "users", "README"), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	got := SiteUserRoots()
	want := []string{filepath.Join(a, "users", "1000"), filepath.Join(a, "users", "1001"), filepath.Join(b, "users", "1002")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("SiteUserRoots = %v, want %v", got, want)
	}
}

// TestResolveSpoolDir: an explicit path replaces the
// chain and is made 0700; a path that cannot be a directory, or a link, is
// spool_directory_unavailable; the scratch resolver shares the probe loop
// and still takes its explicit path.
func TestResolveSpoolDir(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "spool")
	got, err := ResolveSpoolDir(want, root, os.Geteuid())
	if err != nil || got != want {
		t.Fatalf("explicit path: %q %v", got, err)
	}
	if fi, err := os.Stat(want); err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("made 0700: %v %v", fi, err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveSpoolDir(filepath.Join(file, "spool"), root, os.Geteuid()); errorcodes.Of(err) != "spool_directory_unavailable" {
		t.Fatalf("under a file: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(want, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveSpoolDir(link, root, os.Geteuid()); errorcodes.Of(err) != "spool_directory_unavailable" {
		t.Fatalf("a link: %v", err)
	}
	if got, err := ResolveSpoolDir("~/spool2", root, os.Geteuid()); err != nil || got != filepath.Join(root, "spool2") {
		t.Fatalf("~ expanded: %q %v", got, err)
	}
	if got, err := ResolveScratch(filepath.Join(root, "scratch"), root, root, "u", os.Geteuid()); err != nil || got != filepath.Join(root, "scratch") {
		t.Fatalf("the scratch's explicit path: %q %v", got, err)
	}
}

// TestResolveBaseDirIgnoresLegacyHome: a present ~/karvi is not a base;
// the XDG root is created beside it.
func TestResolveBaseDirIgnoresLegacyHome(t *testing.T) {
	saved := SystemRoots
	t.Cleanup(func() { SystemRoots = saved })
	SystemRoots = []string{filepath.Join(t.TempDir(), "opt", "karvi"), filepath.Join(t.TempDir(), "var", "lib", "karvi")}
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "karvi"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveBaseDir("auto", home, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".local/share/karvi"); got != want {
		t.Fatalf("base %s, want %s", got, want)
	}
}
