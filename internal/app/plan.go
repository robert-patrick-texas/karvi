package app

import (
	"context"
	"os"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/planner"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
)

// localJob is a plan packaged for the local endpoint: the direct modes run
// it in this process through jobexec.Run.
type localJob struct {
	Plan       executionplan.ExecutionPlan
	Header     executionplan.PublicJobHeader
	Package    credentialpackage.CredentialPackage
	Projection credentialpackage.SafePackageProjection
	planner    *planner.CredentialPlanner
}

// Destroy wipes the package's and the planner's secrets.
func (j *localJob) Destroy() {
	j.Package.Destroy()
	if j.planner != nil {
		j.planner.Destroy()
	}
}

// LocalAudience is the audience of a package built for the local
// endpoint: direct command mode and run --no-daemon.
const LocalAudience = executionplan.EndpointLocal

// planAndPackage is the client-side sequence for the local endpoint
// (without a daemon): draft the plan, resolve
// client-authority credentials, prepare daemon-authority targets in this
// process, which is the endpoint, resolve theirs, bind, finalize, and build
// and validate the package. Any failure returns before a job exists.
func planAndPackage(ctx context.Context, cfg configload.Snapshot, operator credentials.Operator, set planner.TargetSet, opts planner.DraftOptions, activityID string, streams IO) (*localJob, error) {
	now := time.Now()
	plan, err := planner.Draft(ctx, cfg, operator, set, opts, now)
	if err != nil {
		return nil, err
	}
	warn := func(s string) { jobexec.WriteWarning(streams.Stderr, cfg, s) }
	cp, err := planner.NewCredentialPlanner(cfg, operator, set.Devices, now, planner.CredentialOptions{Warn: warn})
	if err != nil {
		return nil, err
	}
	if err := cp.Resolve(ctx, plan); err != nil {
		cp.Destroy()
		return nil, err
	}
	if hasDaemonAuthority(plan) {
		evidence, err := resolver.PrepareDaemon(ctx, cfg, plan.Targets)
		if err != nil {
			cp.Destroy()
			return nil, err
		}
		prepID, err := osutil.NewID(time.Now())
		if err != nil {
			cp.Destroy()
			return nil, errorcodes.Ensure(err, "activity_id_generation_failed")
		}
		digest, err := executionplan.SumJSON(evidence)
		if err != nil {
			cp.Destroy()
			return nil, err
		}
		plan, err = executionplan.IncorporatePreparation(plan, executionplan.PreparationEvidence{ExecutionEndpoint: executionplan.EndpointLocal, PreparationID: prepID, PreparationDigest: digest, PreparedAt: time.Now(), Addresses: evidence})
		if err != nil {
			cp.Destroy()
			return nil, err
		}
		if err := cp.Resolve(ctx, plan); err != nil {
			cp.Destroy()
			return nil, err
		}
	}
	bound, err := cp.Bind(plan)
	if err != nil {
		cp.Destroy()
		return nil, err
	}
	final, err := executionplan.Finalize(bound, time.Now())
	if err != nil {
		cp.Destroy()
		return nil, err
	}
	header, err := planner.Header(final, activityID, executionplan.ModeLive)
	if err != nil {
		cp.Destroy()
		return nil, err
	}
	hostname, _ := os.Hostname()
	pkg, err := cp.Package(final, header, LocalAudience, hostname, time.Now())
	if err != nil {
		cp.Destroy()
		return nil, err
	}
	ref, projection, err := planner.PackageReference(pkg)
	if err != nil {
		pkg.Destroy()
		cp.Destroy()
		return nil, err
	}
	commit, err := planner.CommitHeader(final, activityID, executionplan.ModeLive, ref)
	if err != nil {
		pkg.Destroy()
		cp.Destroy()
		return nil, err
	}
	return &localJob{Plan: final, Header: commit, Package: pkg, Projection: projection, planner: cp}, nil
}

func hasDaemonAuthority(plan executionplan.ExecutionPlan) bool {
	for _, t := range plan.Targets {
		if t.AddressPlan.Authority == executionplan.AddressByDaemon {
			return true
		}
	}
	return false
}
