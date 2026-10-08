package osutil

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// withSetupPlaces points the places setup shared makes at dir's: the
// scratch root dir/shm, the system root dir/opt with dir/opt/shared, for
// one test.
func withSetupPlaces(t *testing.T, dir string) {
	t.Helper()
	scratch, system, shared := ScratchRoot, SystemRoots, SharedRoots
	ScratchRoot = filepath.Join(dir, "shm")
	SystemRoots = []string{filepath.Join(dir, "opt")}
	SharedRoots = []string{filepath.Join(dir, "opt", "shared")}
	t.Cleanup(func() { ScratchRoot, SystemRoots, SharedRoots = scratch, system, shared })
}

// withNoFreeInodes makes the filesystem of every path under full one with
// no free inodes, for one test.
func withNoFreeInodes(t *testing.T, full string) {
	t.Helper()
	saved := statfs
	statfs = func(path string, st *syscall.Statfs_t) error {
		if err := saved(path, st); err != nil {
			return err
		}
		if path == full || strings.HasPrefix(path, full+"/") {
			st.Files, st.Ffree = 1000, 0
		}
		return nil
	}
	t.Cleanup(func() { statfs = saved })
}

func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes every directory")
	}
}

// TestResolvePath: ~ and ~/ are the home given, ~user is refused, a
// relative path is taken from the working directory, empty stays empty.
func TestResolvePath(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ in, want, code string }{
		{"~", "/home/op", ""},
		{"~/a/../b", "/home/op/b", ""},
		{"/x//y/", "/x/y", ""},
		{"rel/x", filepath.Join(wd, "rel/x"), ""},
		{"~other/x", "", "path_other_user_home_unsupported"},
		{"", "", ""},
	} {
		got, err := ResolvePath(tc.in, "/home/op")
		if got != tc.want || errorcodes.Of(err) != tc.code {
			t.Errorf("%q: %q %v, want %q %s", tc.in, got, err, tc.want, tc.code)
		}
	}
	if _, err := ResolvePath("~/x", ""); errorcodes.Of(err) != "operator_identity_unavailable" {
		t.Errorf("no home: %v", err)
	}
}

// TestCheckSetupPlaces: a path whose making would make a place setup shared
// makes is refused, naming the highest such place and the key; a path
// inside places that exist, or outside them, is not.
func TestCheckSetupPlaces(t *testing.T) {
	dir := t.TempDir()
	withSetupPlaces(t, dir)
	err := CheckSetupPlaces(filepath.Join(ScratchRoot, "x", "y"), "tempdir")
	if errorcodes.Of(err) != "shared_directory_absent" || !strings.Contains(err.Error(), ScratchRoot+" is absent") || !strings.Contains(err.Error(), "sudo karvi setup shared") || !strings.Contains(err.Error(), "set tempdir to a path elsewhere") {
		t.Fatalf("under an absent scratch root: %v", err)
	}
	err = CheckSetupPlaces(filepath.Join(dir, "opt", "shared", "jobs"), "output.root")
	if errorcodes.Of(err) != "shared_directory_absent" || !strings.Contains(err.Error(), filepath.Join(dir, "opt")+" is absent") {
		t.Fatalf("the highest place: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "opt", "shared"), 0o770); err != nil {
		t.Fatal(err)
	}
	err = CheckSetupPlaces(filepath.Join(dir, "opt", "shared", "jobs", "260101"), "output.root")
	if errorcodes.Of(err) != "shared_directory_absent" || !strings.Contains(err.Error(), filepath.Join(dir, "opt", "shared", "jobs")+" is absent") {
		t.Fatalf("an absent tree: %v", err)
	}
	if err := os.Mkdir(ScratchRoot, 0o770); err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{filepath.Join(ScratchRoot, "u", "sockets"), filepath.Join(ScratchRoot, "scoreboards"), filepath.Join(dir, "elsewhere", "x"), filepath.Join(dir, "opt", "shared", "other")} {
		if err := CheckSetupPlaces(ok, "k"); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	// The maker refuses it too, whatever reached it.
	if err := MakeDirectories(filepath.Join(dir, "opt", "users", "u"), 0o700); errorcodes.Of(err) != "shared_directory_absent" {
		t.Fatalf("the maker: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "opt", "users")); !os.IsNotExist(err) {
		t.Fatalf("users was made: %v", err)
	}
}

// pathOfLength is a path of exactly n bytes under parent, its last
// component padded with x.
func pathOfLength(t *testing.T, parent string, n int) string {
	t.Helper()
	pad := n - len(parent) - 1
	if pad < 1 {
		t.Skipf("%s is too long for a %d-byte path", parent, n)
	}
	return filepath.Join(parent, strings.Repeat("x", pad))
}

// TestScratchBoundedForTheAskpassSocket: an askpass socket's longest name
// fits under MaxScratchDir and one byte more does not bind; the chain
// passes a longer candidate by, listed whether present or not, and the
// maker skips it; an explicit path longer is tempdir_too_long, nothing
// made. The test works under a short directory of its own, so that the
// lengths are exact and the maker never reaches the host's /tmp/karvi-<uid>.
func TestScratchBoundedForTheAskpassSocket(t *testing.T) {
	skipAsRoot(t)
	dir, err := os.MkdirTemp("", "k")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	longest := fmt.Sprintf("askpass-%d-%s.sock", 4194304, strings.Repeat("a", ControlSocketNameLength))
	if MaxScratchDir != 69 || MaxScratchDir+1+len(longest) != 107 {
		t.Fatalf("MaxScratchDir %d with the longest name %q", MaxScratchDir, longest)
	}
	fits := pathOfLength(t, dir, MaxScratchDir)
	if err := os.Mkdir(fits, 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(fits, longest))
	if err != nil {
		t.Fatalf("the longest name under %d bytes: %v", len(fits), err)
	}
	ln.Close()
	if ln, err := net.Listen("unix", filepath.Join(fits, "x"+longest)); err == nil {
		ln.Close()
		t.Fatal("a 108-byte socket path bound")
	}

	// The chain: the scratch root's folder too long is passed by, absent,
	// and listed; <basedir>/tmp of the bound is taken by both.
	withSetupPlaces(t, dir)
	ScratchRoot = pathOfLength(t, dir, MaxScratchDir-1)
	if err := os.Mkdir(ScratchRoot, 0o770); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(ScratchRoot, "u")
	base := pathOfLength(t, dir, MaxScratchDir-len("/tmp"))
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := os.Geteuid()
	tmp := filepath.Join(base, "tmp")
	reason := fmt.Sprintf("%d bytes, longer than the askpass socket allows (%d)", MaxScratchDir+1, MaxScratchDir)
	got, err := ScratchPlace("auto", base, dir, "u", uid)
	if err != nil || got.Path != tmp || len(got.Passed) != 1 || got.Passed[0] != (Passed{own, reason}) {
		t.Fatalf("place %+v %v", got, err)
	}
	if made, err := ResolveScratch("auto", base, dir, "u", uid); err != nil || made != tmp {
		t.Fatalf("the maker took %q %v", made, err)
	}
	if _, err := os.Lstat(own); !os.IsNotExist(err) {
		t.Fatalf("the passed folder was made: %v", err)
	}

	// <basedir>/tmp one byte over, present: passed by, the next candidate
	// named (the twin alone: the maker would make it on the host).
	longBase := pathOfLength(t, dir, MaxScratchDir+1-len("/tmp"))
	if err := os.MkdirAll(filepath.Join(longBase, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err = ScratchPlace("auto", longBase, dir, "u", uid)
	if err != nil || got.Path != filepath.Join("/tmp", "karvi-"+strconv.Itoa(uid)) || len(got.Passed) != 2 || got.Passed[1] != (Passed{filepath.Join(longBase, "tmp"), reason}) {
		t.Fatalf("a long basedir: %+v %v", got, err)
	}

	// An explicit path: of the bound, taken; one byte over, refused by
	// both, naming its length, and not made.
	if made, err := ResolveScratch(fits, base, dir, "u", uid); err != nil || made != fits {
		t.Fatalf("explicit at the bound: %q %v", made, err)
	}
	over := pathOfLength(t, dir, MaxScratchDir+1)
	for name, resolve := range map[string]func() error{
		"place": func() error { _, err := ScratchPlace(over, base, dir, "u", uid); return err },
		"maker": func() error { _, err := ResolveScratch(over, base, dir, "u", uid); return err },
	} {
		if err := resolve(); errorcodes.Of(err) != "tempdir_too_long" || !strings.Contains(err.Error(), over+" is 70 bytes") || !strings.Contains(err.Error(), "at most 69 bytes") {
			t.Fatalf("explicit over the bound, %s: %v", name, err)
		}
	}
	if _, err := os.Lstat(over); !os.IsNotExist(err) {
		t.Fatalf("the long path was made: %v", err)
	}
}

// TestScratchPlaceNamesWhatTheMakerTakes: in each case the twin names,
// creating nothing, the candidate the maker then takes, and the present
// candidates passed by with their reasons.
func TestScratchPlaceNamesWhatTheMakerTakes(t *testing.T) {
	skipAsRoot(t)
	dir := t.TempDir()
	withSetupPlaces(t, dir)
	base := filepath.Join(dir, "base")
	if err := os.Mkdir(base, 0o750); err != nil {
		t.Fatal(err)
	}
	uid := os.Geteuid()
	agree := func(name, raw string, want Place) {
		t.Helper()
		got, err := ScratchPlace(raw, base, dir, "u", uid)
		if err != nil || got.Path != want.Path || len(got.Passed) != len(want.Passed) {
			t.Fatalf("%s: place %+v %v, want %+v", name, got, err, want)
		}
		for i := range want.Passed {
			if got.Passed[i] != want.Passed[i] {
				t.Fatalf("%s: passed %+v, want %+v", name, got.Passed, want.Passed)
			}
		}
		if _, err := os.Lstat(got.Path); !os.IsNotExist(err) && name == "absent" {
			t.Fatalf("%s: the place was made", name)
		}
		made, err := ResolveScratch(raw, base, dir, "u", uid)
		if err != nil || made != got.Path {
			t.Fatalf("%s: the maker took %q %v, the place %q", name, made, err, got.Path)
		}
	}
	// No scratch root: <basedir>/tmp, absent, made by the maker.
	agree("absent", "auto", Place{Path: filepath.Join(base, "tmp")})

	// The scratch root present and the operator's folder absent: taken.
	if err := os.Mkdir(ScratchRoot, 0o770); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(ScratchRoot, "u")
	agree("in the scratch root", "auto", Place{Path: own})

	// The operator's folder closed: passed by, named.
	if err := os.Chmod(own, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(own, 0o700) })
	agree("closed", "auto", Place{Path: filepath.Join(base, "tmp"), Passed: []Passed{{own, reasonNotWritable}}})
	os.Chmod(own, 0o700)

	// The scratch root's filesystem out of inodes: passed by, named.
	withNoFreeInodes(t, ScratchRoot)
	agree("no inodes", "auto", Place{Path: filepath.Join(base, "tmp"), Passed: []Passed{{own, reasonNoInodes}}})

	// An explicit path replaces the chain; under an absent setup place it
	// is refused by both, before anything is made.
	agree("explicit", filepath.Join(dir, "t"), Place{Path: filepath.Join(dir, "t")})
	if err := os.RemoveAll(ScratchRoot); err != nil {
		t.Fatal(err)
	}
	guarded := filepath.Join(ScratchRoot, "x")
	if _, err := ScratchPlace(guarded, base, dir, "u", uid); errorcodes.Of(err) != "shared_directory_absent" || !strings.Contains(err.Error(), "tempdir") {
		t.Fatalf("the guard, place: %v", err)
	}
	if _, err := ResolveScratch(guarded, base, dir, "u", uid); errorcodes.Of(err) != "shared_directory_absent" {
		t.Fatalf("the guard, maker: %v", err)
	}
	if _, err := os.Stat(ScratchRoot); !os.IsNotExist(err) {
		t.Fatalf("the scratch root was made: %v", err)
	}
}

// TestSpoolPlaceNamesWhatTheMakerTakes: the first spool root out of inodes
// or closed is passed by for the second, by the twin as by the maker; none
// usable is the refusal of both.
func TestSpoolPlaceNamesWhatTheMakerTakes(t *testing.T) {
	skipAsRoot(t)
	dir := t.TempDir()
	withSetupPlaces(t, dir)
	first, second := filepath.Join(dir, "tmp"), filepath.Join(dir, "vartmp")
	for _, d := range []string{first, second} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	saved := SpoolRoots
	SpoolRoots = []string{first, second}
	t.Cleanup(func() { SpoolRoots = saved })
	uid := os.Geteuid()
	own := func(root string) string { return filepath.Join(root, "karvi-"+strconv.Itoa(uid)) }

	got, err := SpoolPlace("auto", dir, uid)
	if err != nil || got.Path != own(first) || len(got.Passed) != 0 {
		t.Fatalf("absent: %+v %v", got, err)
	}
	if err := os.Mkdir(own(first), 0o700); err != nil {
		t.Fatal(err)
	}
	withNoFreeInodes(t, first)
	got, err = SpoolPlace("auto", dir, uid)
	if err != nil || got.Path != own(second) || len(got.Passed) != 1 || got.Passed[0] != (Passed{own(first), reasonNoInodes}) {
		t.Fatalf("no inodes: %+v %v", got, err)
	}
	if made, err := ResolveSpoolDir("auto", dir, uid); err != nil || made != got.Path {
		t.Fatalf("the maker took %q %v", made, err)
	}
	if err := os.Chmod(second, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(second, 0o700) })
	if err := os.Chmod(own(second), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(own(second), 0o700) })
	got, err = SpoolPlace("auto", dir, uid)
	if errorcodes.Of(err) != "spool_directory_unavailable" || len(got.Passed) != 2 {
		t.Fatalf("none usable: %+v %v", got, err)
	}
	if _, err := ResolveSpoolDir("auto", dir, uid); errorcodes.Of(err) != "spool_directory_unavailable" {
		t.Fatalf("the maker: %v", err)
	}
}

// TestTreePlaceWritesNothing: a shared tree present and closed is refused
// by the twin with the maker's code, without a probe file; an absent one
// gives the tree under basedir; an explicit tree under an absent shared
// root is refused naming the key.
func TestTreePlaceWritesNothing(t *testing.T) {
	skipAsRoot(t)
	dir := t.TempDir()
	withSetupPlaces(t, dir)
	base := filepath.Join(dir, "base")
	if got, err := OutputRootPlace("auto", "auto", base, dir); err != nil || got != filepath.Join(base, "jobs") {
		t.Fatalf("no shared root: %q %v", got, err)
	}
	if _, err := OutputRootPlace(filepath.Join(dir, "opt", "shared", "jobs"), "auto", base, dir); errorcodes.Of(err) != "shared_directory_absent" || !strings.Contains(err.Error(), "output.root") {
		t.Fatalf("explicit, under an absent shared root: %v", err)
	}
	jobs := filepath.Join(dir, "opt", "shared", "jobs")
	if err := os.MkdirAll(jobs, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := OutputRootPlace("auto", "auto", base, dir); err != nil || got != jobs {
		t.Fatalf("the shared tree: %q %v", got, err)
	}
	if err := os.Chmod(jobs, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(jobs, 0o700) })
	_, err := OutputRootPlace("auto", "auto", base, dir)
	if errorcodes.Of(err) != "output_directory_not_writable" || !strings.Contains(err.Error(), reasonNotWritable) {
		t.Fatalf("closed, place: %v", err)
	}
	if _, err := ResolveOutputRoot("auto", "auto", base, dir); errorcodes.Of(err) != "output_directory_not_writable" {
		t.Fatalf("closed, maker: %v", err)
	}
	if got, err := CrunDirectoryPlace("rel", "auto", base, dir); err != nil || !filepath.IsAbs(got) {
		t.Fatalf("relative: %q %v", got, err)
	}
}

// TestDaemonSocket: auto under basedir, ~ the home, a relative path made
// absolute, and one under an absent setup place refused.
func TestDaemonSocket(t *testing.T) {
	dir := t.TempDir()
	withSetupPlaces(t, dir)
	if got, err := DaemonSocket("auto", "/b", "/h"); err != nil || got != "/b/socket/daemon.sock" {
		t.Fatalf("auto: %q %v", got, err)
	}
	if got, err := DaemonSocket("~/d.sock", "/b", "/h"); err != nil || got != "/h/d.sock" {
		t.Fatalf("~: %q %v", got, err)
	}
	if got, err := DaemonSocket("d.sock", "/b", "/h"); err != nil || !filepath.IsAbs(got) {
		t.Fatalf("relative: %q %v", got, err)
	}
	if _, err := DaemonSocket(filepath.Join(ScratchRoot, "d.sock"), "/b", "/h"); errorcodes.Of(err) != "shared_directory_absent" || !strings.Contains(err.Error(), "daemon.socket") {
		t.Fatalf("guard: %v", err)
	}
}

// TestOperatorPrivateRoots: every private root of the operator's that
// exists, in the chain's order, the home's last; nothing is made.
func TestOperatorPrivateRoots(t *testing.T) {
	dir := t.TempDir()
	withSetupPlaces(t, dir)
	home := filepath.Join(dir, "home")
	if got := OperatorPrivateRoots(home, "u"); len(got) != 0 {
		t.Fatalf("a fresh host: %q", got)
	}
	site, xdg := filepath.Join(dir, "opt", "users", "u"), filepath.Join(home, ".local/share/karvi")
	for _, d := range []string{site, xdg, filepath.Join(dir, "opt", "users", "other")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if got := OperatorPrivateRoots(home, "u"); len(got) != 2 || got[0] != site || got[1] != xdg {
		t.Fatalf("both: %q", got)
	}
}
