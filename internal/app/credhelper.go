package app

import (
	"context"

	"github.com/robert-patrick-texas/karvi/internal/credentialbackend"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credhelper"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
)

// credentialInput is the resolver's input at an invocation's first
// credential step, before any backend or fallback is asked: the terminal
// and the process environment, with the credential helper's values in front
// of the environment while creds.helper-enabled is true (EXAMPLES chapter
// 56, issues 8 and 9). The helper runs here, once per invocation, or its
// values come from common.Helper, which holds them for a stream's later jobs
// until --restart; a job whose snapshot has the helper off (--no-credh)
// neither runs nor reads it. A helper that fails refuses the invocation, or
// the stream's job, here, before any device is reached.
func credentialInput(ctx context.Context, common CommonOptions, streams IO) (credentialbackend.InputProvider, error) {
	cfg := common.Config
	if !cfg.Bool("creds.helper-enabled") {
		return credentialbackend.TTYInput{}, nil
	}
	holder := common.Helper
	if holder == nil {
		holder = &credhelper.Holder{}
	}
	warn := func(s string) { jobexec.WriteWarning(streams.Stderr, cfg, s) }
	values, err := holder.Values(ctx, credhelper.FromConfig(cfg, common.Operator), warn, jobexec.DebugLogger(common.Debug, cfg, streams.Stderr))
	if err != nil {
		return nil, err
	}
	return credhelper.Input(credentialbackend.TTYInput{}, values), nil
}
