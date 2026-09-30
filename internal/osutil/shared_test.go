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
	gid := os.Getgid()
	group := strconv.Itoa(gid)
	if gr, err := user.LookupGroupId(group); err == nil {
		group = gr.Name
	}
	var results []SharedSetup
	var err error
	withUmask(t, func() { results, err = SetupSharedTrees(root, gid, 0o770) })
	if err != nil {
		t.Fatal(err)
	}
	if m := mode(t, filepath.Dir(root)); m != 0o755 {
		t.Fatalf("system root mode %o, want 0755", m)
	}
	if len(results) != 5 {
		t.Fatalf("results %+v, want five", results)
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
	results, err = SetupSharedTrees(root, gid, 0o770)
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
	results, err = SetupSharedTrees(root, gid, 0o770)
	if errorcodes.Of(err) != "setup_directory_mismatch" {
		t.Fatalf("mismatch: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "transcripts exists and is not a real directory") || !strings.Contains(msg, "nothing was changed there") || strings.Contains(msg, "crun") {
		t.Errorf("message: %s", msg)
	}
	if len(results) != 4 || results[2].State != "repaired" || filepath.Base(results[2].Path) != "crun" || results[2].Was != "group "+group+" mode 2775" {
		t.Errorf("the shared directory, jobs, the repaired crun, and users are reported: %+v", results)
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
	results, err = SetupSharedTrees(root, gid, 0o770)
	if err != nil || len(results) != 5 || results[0].State != "repaired" || results[3].State != "created" {
		t.Errorf("the shared directory repaired and the tree remade: %v %+v", err, results)
	}
	if modeString(os.ModeSetgid|0o770) != "2770" || modeString(0o750) != "0750" || modeString(os.ModeSticky|0o770) != "1770" {
		t.Errorf("modeString: %s %s %s", modeString(os.ModeSetgid|0o770), modeString(0o750), modeString(os.ModeSticky|0o770))
	}
}
