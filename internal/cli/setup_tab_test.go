package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/completion"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// TestSetupTab covers setup tab at the command: without
// root the command is refused before anything is looked at; as root (stood
// in for) the script is written at 0644 and reported created, a second run
// reports exists, an older karvi script is replaced and reported updated, a
// site's own file is reported and left, and a missing completions directory
// is refused with the package named. The script's first line is the marker.
func TestSetupTab(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bash_completion.d")
	savedDir, savedRoot := completionDir, setupIsRoot
	completionDir = dir
	t.Cleanup(func() { completionDir, setupIsRoot = savedDir, savedRoot })
	path := filepath.Join(dir, "karvi")
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Main(args, strings.NewReader(""), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	setupIsRoot = func() bool { return false }
	if code, _, errText := run("setup", "tab"); code != exitcode.ExitPermissionError || !strings.Contains(errText, "setup_requires_root") || !strings.Contains(errText, "sudo karvi setup tab") {
		t.Fatalf("not root: %d %q", code, errText)
	}
	setupIsRoot = func() bool { return true }
	if code, _, errText := run("setup", "tab"); code != exitcode.ExitDependencyError || !strings.Contains(errText, "setup_completion_dir_missing") || !strings.Contains(errText, "bash-completion") {
		t.Fatalf("no directory: %d %q", code, errText)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errText := run("setup", "tab")
	if code != 0 || errText != "" || out != "created  "+path+"  mode 0644\n" {
		t.Fatalf("as root: %d %q %q", code, out, errText)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != completionScript {
		t.Fatalf("the file is not the script: %v", err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != completionMode {
		t.Errorf("mode %o", fi.Mode().Perm())
	}
	// The shape: the marker first, the hidden word called on the command
	// bash names ($1), and the one function registered for both commands.
	if !strings.HasPrefix(completionScript, "# karvi bash completion") || !strings.Contains(completionScript, `"$1" `+completion.Word) || !strings.HasSuffix(completionScript, "complete -F _karvi karvi\ncomplete -F _karvi karvi-prune\n") {
		t.Error("the script's shape")
	}
	if code, out, _ = run("setup", "tab"); code != 0 || out != "exists   "+path+"  mode 0644\n" {
		t.Fatalf("second run: %d %q", code, out)
	}
	if code, out, _ = run("--quiet", "setup", "tab"); code != 0 || out != "" {
		t.Fatalf("quiet: %d %q", code, out)
	}
	// An older karvi script is replaced.
	if err := os.WriteFile(path, []byte(completionMarker+"\n_karvi() { :; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, _ = run("setup", "tab"); code != 0 || out != "updated  "+path+"  mode 0644\n" {
		t.Fatalf("older script: %d %q", code, out)
	}
	if data, _ := os.ReadFile(path); string(data) != completionScript {
		t.Error("the older script was not replaced")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != completionMode {
		t.Errorf("mode after the update %o", fi.Mode().Perm())
	}
	// A site's own file is left as it is.
	if err := os.WriteFile(path, []byte("# the site's own\ncomplete -W 'run login' karvi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errText = run("setup", "tab"); code != exitcode.ExitPermissionError || !strings.Contains(errText, "setup_completion_mismatch") {
		t.Fatalf("site's file: %d %q", code, errText)
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "# the site's own") {
		t.Error("the site's file was changed")
	}
	if code, out, _ = run("setup", "tab", "--help"); code != 0 || !strings.Contains(out, "sudo karvi setup tab") {
		t.Fatalf("help: %d\n%s", code, out)
	}
}
