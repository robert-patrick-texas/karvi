package jobexec

import (
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// PlanConfiguration is the configuration a daemon runs a job under: the
// plan's configuration block built by configload.FromValues, refused as
// execution_plan_invalid when it cannot be built or its digest is not the
// plan's sources.config_digest, the digest the client's load gave it. The
// daemon builds it at prepare from the draft and at commit from the final
// plan; an in-process job runs under the client's own loaded snapshot.
func PlanConfiguration(plan *executionplan.ExecutionPlan) (configload.Snapshot, error) {
	cfg, err := configload.FromValues(plan.Configuration)
	if err != nil {
		return configload.Snapshot{}, errorcodes.Errorf("execution_plan_invalid", "configuration: %v", err)
	}
	if cfg.Digest != plan.Sources.ConfigDigest {
		return configload.Snapshot{}, errorcodes.Errorf("execution_plan_invalid", "configuration: digest %s is not sources.config_digest %s", cfg.Digest, plan.Sources.ConfigDigest)
	}
	return cfg, nil
}
