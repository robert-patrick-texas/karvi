package cli

import (
	"bytes"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// TestSetupShared covers setup shared at the command: without root
// the command is refused before anything is looked at; as root (stood in
// for) the shared directory and the three trees are created under the
// first shared root in the group given, each reported on one line, and a
// second run reports them as existing; a group the host does not know is refused; the group falls back
// to SUDO_GID; --help shows the text.
func TestSetupShared(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "opt", "karvi", "shared")
	scratch := filepath.Join(dir, "shm", "karvi")
	tmpfiles := filepath.Join(dir, "etc", "tmpfiles.d", "karvi.conf")
	for _, d := range []string{filepath.Dir(scratch), filepath.Dir(tmpfiles)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	savedRoots, savedRoot, savedScratch, savedTmpfiles := osutil.SharedRoots, setupIsRoot, osutil.ScratchRoot, osutil.TmpfilesPath
	osutil.SharedRoots, osutil.ScratchRoot, osutil.TmpfilesPath = []string{root, "/var/lib/karvi/shared"}, scratch, tmpfiles
	t.Cleanup(func() {
		osutil.SharedRoots, setupIsRoot, osutil.ScratchRoot, osutil.TmpfilesPath = savedRoots, savedRoot, savedScratch, savedTmpfiles
	})
	gid := strconv.Itoa(os.Getgid())
	gr, err := user.LookupGroupId(gid)
	if err != nil {
		t.Skipf("the test's own group has no name: %v", err)
	}
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Main(args, strings.NewReader(""), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	setupIsRoot = func() bool { return false }
	if code, _, errText := run("setup", "shared", "--group", gr.Name); code != exitcode.ExitPermissionError || !strings.Contains(errText, "setup_requires_root") || !strings.Contains(errText, "sudo karvi setup shared") {
		t.Fatalf("not root: %d %q", code, errText)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("the refusal created something: %v", err)
	}
	setupIsRoot = func() bool { return true }
	code, out, errText := run("setup", "shared", "--group", gr.Name)
	if code != 0 || errText != "" {
		t.Fatalf("as root: %d %q", code, errText)
	}
	want := "created  " + root + "  group " + gr.Name + "  mode 2770\n" +
		"created  " + filepath.Join(root, "jobs") + "  group " + gr.Name + "  mode 2770\n" +
		"created  " + filepath.Join(root, "crun") + "  group " + gr.Name + "  mode 2770\n" +
		"created  " + filepath.Join(root, "transcripts") + "  group " + gr.Name + "  mode 2770\n" +
		"created  " + filepath.Join(filepath.Dir(root), "users") + "  group " + gr.Name + "  mode 1770\n" +
		"created  " + scratch + "  group " + gr.Name + "  mode 3770\n" +
		"created  " + filepath.Join(scratch, "scoreboards") + "  group " + gr.Name + "  mode 3770\n" +
		"created  " + filepath.Join(scratch, "capacity") + "  group " + gr.Name + "  mode 2770\n" +
		"created  " + filepath.Join(scratch, "capacity", "devices") + "  group " + gr.Name + "  mode 2770\n" +
		"created  " + tmpfiles + "  mode 0644\n"
	if out != want {
		t.Fatalf("report:\n%s\nwant:\n%s", out, want)
	}
	if rule, err := os.ReadFile(tmpfiles); err != nil || string(rule) != osutil.TmpfilesRule(scratch, gr.Name, 0o770) {
		t.Fatalf("the tmpfiles rule: %v\n%s", err, rule)
	}
	code, out, _ = run("setup", "shared", "--group", gr.Name)
	if code != 0 || strings.Count(out, "exists   ") != 10 {
		t.Fatalf("second run: %d\n%s", code, out)
	}
	// The group from sudo's environment.
	t.Setenv("SUDO_GID", gid)
	if code, out, _ = run("setup", "shared"); code != 0 || strings.Count(out, "group "+gr.Name) != 9 {
		t.Fatalf("SUDO_GID: %d\n%s", code, out)
	}
	t.Setenv("SUDO_GID", "")
	if code, _, errText = run("setup", "shared"); code != exitcode.ExitUsageError || !strings.Contains(errText, "setup_group_required") {
		t.Fatalf("no group: %d %q", code, errText)
	}
	if code, _, errText = run("setup", "shared", "--group", "no-such-group-karvi"); code != exitcode.ExitUsageError || !strings.Contains(errText, "setup_group_required") {
		t.Fatalf("unknown group: %d %q", code, errText)
	}
	// Another mode over the existing trees repairs them and says what they
	// had, the scratch root's four among them, and updates the rule;
	// users keeps its own mode.
	if code, out, errText = run("setup", "shared", "--mode", "2775", "--group", gr.Name); code != 0 || strings.Count(out, "repaired ") != 8 || !strings.Contains(out, "mode 2775  (was group "+gr.Name+" mode 2770)") || !strings.Contains(out, "mode 3775  (was group "+gr.Name+" mode 3770)") || !strings.Contains(out, "exists   "+filepath.Join(filepath.Dir(root), "users")) || !strings.Contains(out, "updated  "+tmpfiles) {
		t.Fatalf("another mode over existing trees: %d %q\n%s", code, errText, out)
	}
	// A rule the site wrote is reported and left, after the directories.
	site := "d /dev/shm/karvi 3770 root ops - -\n"
	if err := os.WriteFile(tmpfiles, []byte(site), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errText = run("setup", "shared", "--group", gr.Name); code != exitcode.ExitPermissionError || !strings.Contains(errText, "setup_tmpfiles_mismatch") || strings.Count(out, "\n") != 9 {
		t.Fatalf("the site's rule: %d %q\n%s", code, errText, out)
	}
	if rule, _ := os.ReadFile(tmpfiles); string(rule) != site {
		t.Fatalf("the site's rule was changed:\n%s", rule)
	}
	// A host without the tmpfiles directory is told, after the directories.
	if err := os.RemoveAll(filepath.Dir(tmpfiles)); err != nil {
		t.Fatal(err)
	}
	if code, _, errText = run("setup", "shared", "--group", gr.Name); code == 0 || !strings.Contains(errText, "setup_tmpfiles_dir_missing") {
		t.Fatalf("no tmpfiles directory: %d %q", code, errText)
	}
	if code, out, _ = run("setup", "shared", "--help"); code != 0 || !strings.Contains(out, "sudo karvi setup shared [--group NAME] [--mode 2770|2775]") {
		t.Fatalf("help: %d\n%s", code, out)
	}
	if code, out, _ = run("--help"); code != 0 || !strings.Contains(out, "setup shared") {
		t.Fatalf("the top help lists setup shared:\n%s", out)
	}
}
