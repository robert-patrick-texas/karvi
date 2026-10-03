package osutil

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestSetupSharedTrees covers the shared layout:
// the system root is created plain, the shared directory and the three
// trees in the group at the mode with the setgid bit, whatever the umask; a
// second run finds them and changes nothing; a tree with another mode, and
// a file in a tree's place, are reported and left as they are while the
// other directories are still reported; a shared directory the site made
// otherwise is reported alone.
func TestSetupSharedTrees(t *testing.T) {
	root := filepath.Join(t.TempDir(), "opt", "karvi", "shared")
	users := filepath.Join(filepath.Dir(root), "users")
	scratch := filepath.Join(t.TempDir(), "karvi")
	gid := os.Getgid()
	group := strconv.Itoa(gid)
	if gr, err := user.LookupGroupId(group); err == nil {
		group = gr.Name
	}
	var results []SharedSetup
	var err error
	withUmask(t, func() { results, err = SetupSharedTrees(root, scratch, gid, 0o770) })
	if err != nil {
		t.Fatal(err)
	}
	if m := mode(t, filepath.Dir(root)); m != 0o755 {
		t.Fatalf("system root mode %o, want 0755", m)
	}
	if len(results) != 8 {
		t.Fatalf("results %+v, want eight", results)
	}
	for i, sub := range append([]string{""}, SharedTrees...) {
		p := filepath.Join(root, sub)
		r := results[i]
		if r.Path != p || r.State != "created" || r.Group != group || r.Mode != os.ModeSetgid|0o770 {
			t.Errorf("%s: %+v", sub, r)
		}
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode()&os.ModeSetgid == 0 || fi.Mode().Perm() != 0o770 {
			t.Errorf("%s: mode %v, want setgid and 0770", sub, fi.Mode())
		}
	}
	// The operators' users directory beside shared: in the
	// group, group write and search, the sticky bit, nothing for others.
	if r := results[4]; r.Path != users || r.State != "created" || r.Group != group || r.Mode != os.ModeSticky|UsersDirMode {
		t.Errorf("users: %+v", r)
	}
	if fi, err := os.Lstat(users); err != nil || fi.Mode()&os.ModeSticky == 0 || fi.Mode()&os.ModeSetgid != 0 || fi.Mode().Perm() != 0o770 {
		t.Errorf("users: mode %v, want sticky and 0770", fi.Mode())
	}
	results, err = SetupSharedTrees(root, scratch, gid, 0o770)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.State != "exists" {
			t.Errorf("second run: %+v, want exists", r)
		}
	}
	// A directory the site made otherwise is repaired and says what it had;
	// one that is not a real directory is
	// reported and left, the others still reported.
	if err := os.Chmod(filepath.Join(root, "crun"), os.ModeSetgid|0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(users, 0o777); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(root, "transcripts"))
	if err := os.WriteFile(filepath.Join(root, "transcripts"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	results, err = SetupSharedTrees(root, scratch, gid, 0o770)
	if errorcodes.Of(err) != "setup_directory_mismatch" {
		t.Fatalf("mismatch: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "transcripts exists and is not a real directory") || !strings.Contains(msg, "nothing was changed there") || strings.Contains(msg, "crun") {
		t.Errorf("message: %s", msg)
	}
	if len(results) != 7 || results[2].State != "repaired" || filepath.Base(results[2].Path) != "crun" || results[2].Was != "group "+group+" mode 2775" {
		t.Errorf("the shared directory, jobs, the repaired crun, users, and the scratch root's three are reported: %+v", results)
	}
	if results[3].State != "repaired" || results[3].Path != users || results[3].Was != "group "+group+" mode 0777" {
		t.Errorf("users repaired: %+v", results[3])
	}
	if fi, _ := os.Lstat(filepath.Join(root, "crun")); fi.Mode().Perm() != 0o770 || fi.Mode()&os.ModeSetgid == 0 {
		t.Errorf("the repaired tree: %v", fi.Mode())
	}
	if fi, _ := os.Lstat(users); fi.Mode().Perm() != 0o770 || fi.Mode()&os.ModeSticky == 0 {
		t.Errorf("the repaired users: %v", fi.Mode())
	}
	// The shared directory itself made otherwise is repaired too, and the
	// trees under it are still looked at.
	os.Remove(filepath.Join(root, "transcripts"))
	if err := os.Chmod(root, os.ModeSetgid|0o775); err != nil {
		t.Fatal(err)
	}
	results, err = SetupSharedTrees(root, scratch, gid, 0o770)
	if err != nil || len(results) != 8 || results[0].State != "repaired" || results[3].State != "created" {
		t.Errorf("the shared directory repaired and the tree remade: %v %+v", err, results)
	}
	if modeString(os.ModeSetgid|0o770) != "2770" || modeString(0o750) != "0750" || modeString(os.ModeSticky|0o770) != "1770" {
		t.Errorf("modeString: %s %s %s", modeString(os.ModeSetgid|0o770), modeString(0o750), modeString(os.ModeSticky|0o770))
	}
}

// TestSetupScratchRoot: setup shared makes the scratch root and its
// scoreboards with the setgid and the sticky bit and its capacity with the
// setgid bit alone, in the group at the mode whatever the umask, owned by
// the caller (root, under sudo); a scratch root an operator's run of an
// earlier release left at 0700 is repaired.
func TestSetupScratchRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "opt", "karvi", "shared")
	scratch := filepath.Join(dir, "shm", "karvi")
	if err := os.Mkdir(filepath.Dir(scratch), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	var results []SharedSetup
	var err error
	withUmask(t, func() { results, err = SetupSharedTrees(root, scratch, os.Getgid(), 0o770) })
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		path, state, mode string
	}{
		{scratch, "repaired", "3770"},
		{filepath.Join(scratch, "scoreboards"), "created", "3770"},
		{filepath.Join(scratch, "capacity"), "created", "2770"},
	}
	for i, w := range want {
		r := results[5+i]
		if r.Path != w.path || r.State != w.state || modeString(r.Mode) != w.mode {
			t.Errorf("%s: %+v, want %s %s", w.path, r, w.state, w.mode)
		}
		fi, err := os.Lstat(w.path)
		if err != nil {
			t.Fatal(err)
		}
		if modeString(fi.Mode()) != w.mode {
			t.Errorf("%s: mode %s on disk, want %s", w.path, modeString(fi.Mode()), w.mode)
		}
	}
	if results[5].Was != "group "+groupName(os.Getgid())+" mode 0700" {
		t.Errorf("the repaired scratch root: %+v", results[5])
	}
}

// TestTmpfilesRule: the rule names every scratch place with setup shared's
// modes, owned by root in the group, and the packaged example is the rule
// for the default group (security.shared-group, netops), so the two
// cannot drift.
func TestTmpfilesRule(t *testing.T) {
	rule := TmpfilesRule("/dev/shm/karvi", "netops", 0o770)
	for _, line := range []string{
		TmpfilesMarker + "\n",
		"d /dev/shm/karvi 3770 root netops - -\n",
		"d /dev/shm/karvi/scoreboards 3770 root netops - -\n",
		"d /dev/shm/karvi/capacity 2770 root netops - -\n",
	} {
		if !strings.Contains(rule, line) {
			t.Errorf("the rule lacks %q:\n%s", line, rule)
		}
	}
	if !strings.HasPrefix(rule, TmpfilesMarker+"\n") {
		t.Errorf("the rule does not open with the marker:\n%s", rule)
	}
	if !strings.Contains(TmpfilesRule("/dev/shm/karvi", "ops", 0o775), "d /dev/shm/karvi/capacity 2775 root ops - -\n") {
		t.Error("--mode 2775 is not carried into the rule")
	}
	packaged, err := os.ReadFile(filepath.Join("..", "..", "packaging", "tmpfiles.d", "karvi.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(packaged) != rule {
		t.Errorf("packaging/tmpfiles.d/karvi.conf is not the rule for netops:\n%s\nwant:\n%s", packaged, rule)
	}
}
