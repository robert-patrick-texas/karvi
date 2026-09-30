package app

import (
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/planner"
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

// reserveActivityID takes the activity's ID before its plan is drafted:
// the job directory is created
// exclusively under output.root and the day folder of now in the effective
// timezone, and the ID is the directory's name. A cmd and a run reserve
// alike. release removes the reservation when the activity ends
// before submission; it removes only an empty directory, so it is safe to
// call after a job that wrote files. An activity that writes no files
// (`--nof`, or every output.files key off) reserves nothing and
// takes the unreserved ID of the second.
func reserveActivityID(cfg configload.Snapshot, operator credentials.Operator, now time.Time) (id string, release func(), err error) {
	location, err := display.Location(cfg.String("timezone"))
	if err != nil {
		return "", nil, err
	}
	if !planner.OutputFilesOn(cfg) {
		return osutil.UnreservedJobID(now, location), func() {}, nil
	}
	root, err := outputRootFor(cfg, operator)
	if err != nil {
		return "", nil, err
	}
	id, dir, err := osutil.ReserveJobID(root, now, location, osutil.DirectoryMode(cfg.String("output.directory-mode")))
	if err != nil {
		return "", nil, err
	}
	return id, func() { osutil.ReleaseJobID(dir) }, nil
}
