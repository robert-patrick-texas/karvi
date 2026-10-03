package cli

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// The setup command word: the site's one-time, privileged preparations.
// shared creates the shared root, its
// three trees, the operators' `users` directory, and the scratch root with
// its tmpfiles rule, and repairs the group or mode of one made otherwise;
// every other karvi command runs unprivileged and never creates a system
// root or the scratch root.

// setupIsRoot reports whether the process may create the shared root; a
// test stands in for root.
var setupIsRoot = func() bool { return os.Geteuid() == 0 }

// setupShared is `sudo karvi setup shared [--group NAME] [--mode 2770|2775]`:
// as root, create the shared directory (/opt/karvi/shared) and its jobs,
// crun, and transcripts
// in the group with the mode and the setgid bit, /opt/karvi/users in
// the group at 1770, and the scratch root /dev/shm/karvi with its
// scoreboards (3770) and capacity (2770); report each as created, exists,
// or repaired (a wrong group or mode set right, what it had in the line);
// refuse only a path that is not a real directory; then write the
// scratch root's tmpfiles rule (setupTmpfiles). The group is --group, else the primary group of the operator
// who ran sudo (SUDO_GID), so `sudo karvi setup shared` from an operator's
// own shell needs no option.
func setupShared(inv *Invocation, streams app.IO) int {
	root := osutil.SharedRoots[0]
	if !setupIsRoot() {
		return usageError(streams.Stderr, "setup_requires_root", "karvi setup shared creates %s and its %s, %s beside it, and the scratch root %s with its tmpfiles rule %s, as root: run it as sudo karvi setup shared [--group NAME]", root, strings.Join(osutil.SharedTrees, ", "), filepath.Join(filepath.Dir(root), "users"), osutil.ScratchRoot, osutil.TmpfilesPath)
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
	results, err := osutil.SetupSharedTrees(root, osutil.ScratchRoot, gid, mode)
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
	return setupTmpfiles(inv, streams, group, mode)
}

// setupTmpfiles writes the scratch root's tmpfiles rule
// (osutil.TmpfilesRule) to osutil.TmpfilesPath, so the next boot makes
// the scratch root again as setup shared made it, and reports it as
// created, exists (the same bytes), or updated (an older rule karvi
// wrote, or another group or mode). A file without karvi's marker is the
// site's and is reported and left (setup_tmpfiles_mismatch); a host
// without the tmpfiles directory has no systemd-tmpfiles, and the site
// remakes the scratch root at boot its own way
// (setup_tmpfiles_dir_missing). The directories are made either way.
func setupTmpfiles(inv *Invocation, streams app.IO, group string, mode os.FileMode) int {
	path := osutil.TmpfilesPath
	rule := osutil.TmpfilesRule(osutil.ScratchRoot, group, mode)
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || !fi.IsDir() {
		return reportError(streams.Stderr, "setup_tmpfiles_dir_missing", errorcodes.Errorf("setup_tmpfiles_dir_missing", "the directories are made, but %s is not present, so no systemd-tmpfiles rule was written: %s is a tmpfs emptied at every boot, and the site makes the scratch root again at boot its own way, or runs sudo karvi setup shared after each", filepath.Dir(path), osutil.ScratchRoot))
	}
	state := "created"
	if data, err := os.ReadFile(path); err == nil {
		switch {
		case string(data) == rule:
			state = "exists"
		case strings.HasPrefix(string(data), osutil.TmpfilesMarker+"\n"):
			state = "updated"
		default:
			return reportError(streams.Stderr, "setup_tmpfiles_mismatch", errorcodes.Errorf("setup_tmpfiles_mismatch", "the directories are made, but %s exists and is not a rule karvi wrote; nothing was changed there: move the site's file aside to let karvi place its own, or keep the site's if it makes %s in the operators' group", path, osutil.ScratchRoot))
		}
	} else if !os.IsNotExist(err) {
		return reportError(streams.Stderr, "setup_tmpfiles_write_failed", errorcodes.Errorf("setup_tmpfiles_write_failed", "%s: %v", path, err))
	}
	if state != "exists" {
		if err := writeSiteFile(path, rule, tmpfilesMode); err != nil {
			return reportError(streams.Stderr, "setup_tmpfiles_write_failed", errorcodes.Errorf("setup_tmpfiles_write_failed", "write %s: %v", path, err))
		}
	}
	if !inv.Global.quiet {
		fmt.Fprintf(streams.Stdout, "%-8s %s  mode %04o\n", state, path, tmpfilesMode)
	}
	return 0
}

const tmpfilesMode os.FileMode = 0o644
