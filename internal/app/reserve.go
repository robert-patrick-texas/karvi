package app

import (
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/planner"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
)

// outputRootFor resolves output.root under cfg for operator: the value the
// draft writes into plan.Output.Root and the executor writes under, so the
// reservation and the job name the same directory.
func outputRootFor(cfg configload.Snapshot, operator credentials.Operator) (string, error) {
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return "", errorcodes.Ensure(err, "base_directory_unavailable")
	}
	root, err := osutil.ResolveOutputRoot(cfg.String("output.root"), cfg.String("sharedroot"), base, operator.Home)
	if err != nil {
		return "", errorcodes.Ensure(err, "output_root_unavailable")
	}
	return root, nil
}

// reserveActivityID takes a job's ID before its plan is drafted: its
// claim and, for a job that writes files, its job directory under
// output.root and the day folder of now in the effective timezone (claim).
// A cmd, a run, a crun, each run of a stream, and an exercise reserve
// alike; a job that writes no files (`--nof`, or every output.files key
// off) has its claim alone. release removes the job directory when the
// activity ends before submission; it removes only an empty directory, so
// it is safe to call after a job that wrote files. The claim stays.
func reserveActivityID(cfg configload.Snapshot, operator credentials.Operator, now time.Time) (id string, release func(), err error) {
	root := ""
	if planner.OutputFilesOn(cfg) {
		if root, err = outputRootFor(cfg, operator); err != nil {
			return "", nil, err
		}
	}
	id, dir, err := claim(cfg, operator, root, now)
	if err != nil {
		return "", nil, err
	}
	return id, func() { osutil.ReleaseJobID(dir) }, nil
}

// ClaimActivityID takes the ID of an activity that has no job directory,
// a dry run or a login, by its claim alone.
func ClaimActivityID(cfg configload.Snapshot, operator credentials.Operator, now time.Time) (string, error) {
	id, _, err := claim(cfg, operator, "", now)
	return id, err
}

// claim is the one place an activity's ID is taken
// (osutil.ClaimActivityID): the claim in the scoreboards folder, the
// folder resolved whatever watch.enabled says and passed by in silence,
// since the activity's scoreboard writer says so; and, when root is not
// "", the job directory under it.
func claim(cfg configload.Snapshot, operator credentials.Operator, root string, now time.Time) (id, dir string, err error) {
	location, err := display.Location(cfg.String("timezone"))
	if err != nil {
		return "", "", err
	}
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return "", "", errorcodes.Ensure(err, "base_directory_unavailable")
	}
	claims, mode, err := scoreboard.Folder(cfg.String("scoreboards"), operator.Home, base, nil)
	if err != nil {
		return "", "", err
	}
	return osutil.ClaimActivityID(claims, mode, root, osutil.DirectoryMode(cfg.String("output.directory-mode")), now, location)
}
