package hosted

import (
	"context"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// Compile-time proof that this adapter satisfies the port the run service
// declared. The interface belongs to the consumer; this file is where the
// implementer says it meets it.
var _ run.Executor = (*Runner)(nil)

// Execute adapts the run service's vocabulary to this runner's.
//
// The translation is trivial and that is the point: the service never learns
// what a container is, so a runner deployed on a client's own infrastructure
// will be another implementation of this method rather than a change to the
// service.
func (r *Runner) Execute(
	ctx context.Context, job run.Job, logs chan<- domain.LogLine,
) (run.Outcome, error) {
	result, err := r.Run(ctx, Job{
		RunID:        job.RunID,
		Script:       job.Script,
		Options:      job.Options,
		Env:          job.Env,
		AllowedHosts: job.AllowedHosts,
	}, logs)
	if err != nil {
		return run.Outcome{}, err
	}

	return run.Outcome{
		Summary:  result.Summary,
		ExitCode: result.ExitCode,
		TimedOut: result.TimedOut,
	}, nil
}
