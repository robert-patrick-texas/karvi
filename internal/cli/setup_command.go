package cli

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// The setup command word: the site's one-time, privileged preparations.
// shared creates the shared root, its
// three trees, and the operators' `users` directory, and repairs the group
// or mode of one made otherwise; every other karvi command runs
// unprivileged and never creates a system root.

// setupIsRoot reports whether the process may create the shared root; a
// test stands in for root.
var setupIsRoot = func() bool { return os.Geteuid() == 0 }

// setupShared is `sudo karvi setup shared [--group NAME] [--mode 2770|2775]`:
// as root, create the shared directory (/opt/karvi/shared) and its jobs,
// crun, and transcripts
// in the group with the mode and the setgid bit, and /opt/karvi/users in
// the group at 1770; report each as created, exists, or repaired (a wrong
// group or mode set right, what it had in the line); refuse only a path
// that is not a real directory. The group is --group, else the primary group of the operator
// who ran sudo (SUDO_GID), so `sudo karvi setup shared` from an operator's
// own shell needs no option.
func setupShared(inv *Invocation, streams app.IO) int {
	root := osutil.SharedRoots[0]
	if !setupIsRoot() {
		return usageError(streams.Stderr, "setup_requires_root", "karvi setup shared creates %s and its %s, and %s beside it, as root: run it as sudo karvi setup shared [--group NAME]", root, strings.Join(osutil.SharedTrees, ", "), filepath.Join(filepath.Dir(root), "users"))
	}
	group := inv.String(optGroupName)
	if group == "" {
		if gid := os.Getenv("SUDO_GID"); gid != "" {
			if gr, err := user.LookupGroupId(gid); err == nil {
				group = gr.Name
			}
		}
	}
	if group == "" {
		return usageError(streams.Stderr, "setup_group_required", "the group that shares the trees is not known: give --group NAME (the operators' group), or run through sudo from an operator's shell, whose primary group is taken")
	}
	gr, err := user.LookupGroup(group)
	if err != nil {
		return usageError(streams.Stderr, "setup_group_required", "group %q is not known to this host: %v", group, err)
	}
	gid, err := strconv.Atoi(gr.Gid)
	if err != nil {
		return usageError(streams.Stderr, "setup_group_required", "group %q has gid %q, not a number", group, gr.Gid)
	}
	modeWord := inv.String(optSharedMode)
	if modeWord == "" {
		modeWord = "2770"
	}
	mode := osutil.SharedTreeModes[modeWord]
	results, err := osutil.SetupSharedTrees(root, gid, mode)
	if !inv.Global.quiet {
		for _, r := range results {
			was := ""
			if r.Was != "" {
				was = "  (was " + r.Was + ")"
			}
			fmt.Fprintf(streams.Stdout, "%-8s %s  group %s  mode %s%s\n", r.State, r.Path, r.Group, osutil.ModeString(r.Mode), was)
		}
	}
	if err != nil {
		return reportError(streams.Stderr, "setup_directory_mismatch", err)
	}
	return 0
}
