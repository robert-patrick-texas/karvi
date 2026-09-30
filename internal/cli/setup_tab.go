package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// `sudo karvi setup tab` places the bash
// completion function under the completions directory the bash-completion
// package reads, so every operator's shell completes karvi's words, and
// karvi-prune's (17.1), from the next login. The function is the one
// constant below, which the tests and the suite drive as well, so the
// installed script and the tested one are the same bytes.

// completionScript is the bash function, one for both commands (karvi and
// karvi-prune). It rejoins words the shell split at
// = (so --set=KEY stays one word), hands the line to the hidden word of the
// command being completed ($1, the command as bash names it, so the
// executable the operator is completing answers), and reads the answer:
// candidates, one per line, and the :files directive that asks bash for
// file names. A single candidate ending in = takes no space, so a --set KEY=
// waits for its value.
const completionScript = `# karvi bash completion, written by: sudo karvi setup tab
# The candidates come from the executable being completed ($1: karvi, or
# karvi-prune, each through its hidden word __complete), never from this
# file, so it needs no change when their words change.
_karvi() {
  local cur words cword
  _init_completion -n = || return
  local IFS=$'\n' out line files=
  out=$("$1" __complete "$cword" "${words[@]}" 2>/dev/null) || return
  COMPREPLY=()
  for line in $out; do
    if [ "$line" = ':files' ]; then files=1; else COMPREPLY+=("$line"); fi
  done
  if [ -n "$files" ] && [ ${#COMPREPLY[@]} -eq 0 ]; then
    compopt -o default
  fi
  if [ ${#COMPREPLY[@]} -eq 1 ] && [[ ${COMPREPLY[0]} == *= ]]; then
    compopt -o nospace
  fi
}
complete -F _karvi karvi
complete -F _karvi karvi-prune
`

// completionMarker is the first line of every script karvi wrote: a file at
// the path without it is the site's own and is left as it is.
var completionMarker = strings.SplitN(completionScript, "\n", 2)[0]

// completionDir is where bash-completion reads site files; a test points it
// elsewhere. The file's name is the command's, as the package expects.
var completionDir = "/etc/bash_completion.d"

const completionMode os.FileMode = 0o644

// setupTab is `sudo karvi setup tab`: as root, write completionScript to
// completionDir/karvi and report it as created, exists (the same bytes), or
// updated (an older karvi script, replaced: the file is karvi's). A file
// without the marker is the site's and is reported and left
// (setup_completion_mismatch); a missing completions directory means the
// bash-completion package is not installed, and nothing is created
// (setup_completion_dir_missing).
func setupTab(inv *Invocation, streams app.IO) int {
	path := filepath.Join(completionDir, "karvi")
	if !setupIsRoot() {
		return usageError(streams.Stderr, "setup_requires_root", "karvi setup tab writes %s as root: run it as sudo karvi setup tab", path)
	}
	fi, err := os.Lstat(completionDir)
	if err != nil || !fi.IsDir() {
		return usageError(streams.Stderr, "setup_completion_dir_missing", "the completions directory %s is not present: install the bash-completion package, which reads it, then run sudo karvi setup tab again", completionDir)
	}
	state := "created"
	if data, err := os.ReadFile(path); err == nil {
		switch {
		case bytes.Equal(data, []byte(completionScript)):
			state = "exists"
		case strings.HasPrefix(string(data), completionMarker+"\n"):
			state = "updated"
		default:
			return reportError(streams.Stderr, "setup_completion_mismatch", errorcodes.Errorf("setup_completion_mismatch", "%s exists and is not a script karvi wrote; nothing was changed: move the site's file aside to let karvi place its own", path))
		}
	} else if !os.IsNotExist(err) {
		return reportError(streams.Stderr, "setup_completion_write_failed", errorcodes.Errorf("setup_completion_write_failed", "%s: %v", path, err))
	}
	if state != "exists" {
		if err := writeCompletion(path); err != nil {
			return reportError(streams.Stderr, "setup_completion_write_failed", errorcodes.Errorf("setup_completion_write_failed", "write %s: %v", path, err))
		}
	}
	if !inv.Global.quiet {
		fmt.Fprintf(streams.Stdout, "%-8s %s  mode %04o\n", state, path, completionMode)
	}
	return 0
}

// writeCompletion writes the script whole at the mode, replacing a file in
// place: a reader of the old file sees the old or the new script, never a
// part.
func writeCompletion(path string) error {
	tmp := path + ".karvi-tmp"
	if err := os.WriteFile(tmp, []byte(completionScript), completionMode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, completionMode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
