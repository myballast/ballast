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
	result, err := r.Run(ctx, jobFrom(job), logs)
	if err != nil {
		return run.Outcome{}, err
	}

	return outcomeFrom(result), nil
}

// jobFrom is the translation itself, pulled out of Execute so that it can be
// checked without a Docker daemon.
//
// That is not a stylistic preference. A field added to run.Job and forgotten
// here produces no compiler error and no obvious failure: the generator simply
// runs without it. It happened once already — AllowedHosts was added to the
// service's job and not copied here, and the only thing that noticed was a test
// that started a real container and watched the proxy refuse everything, which
// is a slow and indirect way to learn about a missing assignment.
func jobFrom(job run.Job) Job {
	return Job{
		RunID:        job.RunID,
		Script:       job.Script,
		Options:      job.Options,
		Env:          job.Env,
		AllowedHosts: job.AllowedHosts,
		// Network is deliberately not translated: the service knows nothing
		// about networks, and a job that names none makes its own and removes
		// it. Only this package's own tests supply one.
	}
}

// outcomeFrom is the same translation on the way back.
func outcomeFrom(result Result) run.Outcome {
	return run.Outcome{
		Summary:  result.Summary,
		ExitCode: result.ExitCode,
		TimedOut: result.TimedOut,
	}
}
