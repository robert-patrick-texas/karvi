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
	root := filepath.Join(t.TempDir(), "opt", "karvi", "shared")
	savedRoots, savedRoot := osutil.SharedRoots, setupIsRoot
	osutil.SharedRoots = []string{root, "/var/lib/karvi/shared"}
	t.Cleanup(func() { osutil.SharedRoots, setupIsRoot = savedRoots, savedRoot })
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
		"created  " + filepath.Join(filepath.Dir(root), "users") + "  group " + gr.Name + "  mode 1770\n"
	if out != want {
		t.Fatalf("report:\n%s\nwant:\n%s", out, want)
	}
	code, out, _ = run("setup", "shared", "--group", gr.Name)
	if code != 0 || strings.Count(out, "exists   ") != 5 {
		t.Fatalf("second run: %d\n%s", code, out)
	}
	// The group from sudo's environment.
	t.Setenv("SUDO_GID", gid)
	if code, out, _ = run("setup", "shared"); code != 0 || strings.Count(out, "group "+gr.Name) != 5 {
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
	// had; users keeps its own mode.
	if code, out, errText = run("setup", "shared", "--mode", "2775", "--group", gr.Name); code != 0 || strings.Count(out, "repaired ") != 4 || !strings.Contains(out, "mode 2775  (was group "+gr.Name+" mode 2770)") || !strings.Contains(out, "exists   "+filepath.Join(filepath.Dir(root), "users")) {
		t.Fatalf("another mode over existing trees: %d %q\n%s", code, errText, out)
	}
	if code, out, _ = run("setup", "shared", "--help"); code != 0 || !strings.Contains(out, "sudo karvi setup shared [--group NAME] [--mode 2770|2775]") {
		t.Fatalf("help: %d\n%s", code, out)
	}
	if code, out, _ = run("--help"); code != 0 || !strings.Contains(out, "setup shared") {
		t.Fatalf("the top help lists setup shared:\n%s", out)
	}
}
