package run

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// ProcessNext takes one queued run, executes it, and records the outcome.
//
// It reports whether there was anything to do, so a polling loop knows when to
// wait rather than spin.
//
// Picking a run up is deliberately naive here: no SELECT ... FOR UPDATE SKIP
// LOCKED, and no lock on the environment, so two workers pointed at the same
// organization can both take the same run, and two runs can hit one target at
// once — which makes both sets of measurements meaningless. The queue ticket is
// what closes that.
func (s *Service) ProcessNext(ctx context.Context, orgID uuid.UUID, logs chan<- domain.LogLine) (bool, error) {
	claimed, ok, err := s.runs.ClaimNextQueued(ctx, orgID, s.clock.Now())
	if err != nil || !ok {
		return false, err
	}

	// The transition the claim performed, stated in the domain's terms rather
	// than assumed from the SQL.
	if !domain.StatusQueued.CanTransitionTo(domain.StatusRunning) {
		return true, fmt.Errorf("queued runs may not start")
	}

	s.log.InfoContext(ctx, "run started", "run_id", claimed.ID, "profile", claimed.Profile)

	status, message := s.execute(ctx, orgID, claimed, logs)

	if !claimed.Status.CanTransitionTo(status) {
		return true, fmt.Errorf("a run cannot go from %s to %s", claimed.Status, status)
	}

	if err := s.runs.Finish(ctx, orgID, claimed.ID, status, s.clock.Now(), message); err != nil {
		return true, err
	}

	s.log.InfoContext(ctx, "run finished", "run_id", claimed.ID, "status", status, "message", message)

	return true, nil
}

// execute runs the generator and stores what it reported. It returns the status
// the run ends in, so that a failure anywhere still leaves the run in a
// terminal state rather than stuck in running for ever.
func (s *Service) execute(
	ctx context.Context,
	orgID uuid.UUID,
	current domain.Run,
	logs chan<- domain.LogLine,
) (domain.RunStatus, string) {
	job, err := s.buildJob(ctx, orgID, current)
	if err != nil {
		return domain.StatusErrored, err.Error()
	}

	outcome, err := s.executor.Execute(ctx, job, logs)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return domain.StatusCancelled, err.Error()
	case err != nil:
		return domain.StatusErrored, err.Error()
	case outcome.TimedOut:
		// The generator was killed at its ceiling. Whatever it managed to
		// report is not a measurement of anything.
		return domain.StatusTimeout, "the run exceeded the environment's maximum duration"
	}

	results, err := s.parser.Parse(outcome.Summary)
	if err != nil {
		return domain.StatusErrored, fmt.Sprintf("the generator's report could not be read: %v", err)
	}

	if err := s.runs.SaveResults(ctx, orgID, current.ID, results); err != nil {
		return domain.StatusErrored, fmt.Sprintf("the results could not be stored: %v", err)
	}

	// A generator that failed for its own reasons rather than on a threshold.
	// k6 signals a threshold failure with its own exit code, which the verdict
	// below accounts for.
	if outcome.ExitCode != 0 && len(results.Thresholds) == 0 {
		return domain.StatusErrored, fmt.Sprintf("the generator exited with %d", outcome.ExitCode)
	}

	return results.Verdict(), ""
}

func (s *Service) buildJob(ctx context.Context, orgID uuid.UUID, current domain.Run) (Job, error) {
	scenario, err := s.scenarios.ScenarioByID(ctx, orgID, current.ScenarioID)
	if err != nil {
		return Job{}, fmt.Errorf("read the scenario: %w", err)
	}

	environment, err := s.envs.EnvironmentByID(ctx, orgID, current.EnvironmentID)
	if err != nil {
		return Job{}, fmt.Errorf("read the environment: %w", err)
	}

	// Checked again here, not only at POST. A target approved yesterday may
	// have been revoked since, and this is the last moment before traffic
	// leaves.
	if !environment.IsVerified() {
		return Job{}, domain.ErrTargetNotVerified
	}

	// Checked again here for the same reason, and this one is not belt and
	// braces: a run accepted at five in the afternoon can still be waiting in the
	// queue at three in the morning, and the window exists precisely to say that
	// three in the morning is not a time to load this target.
	if !environment.AllowsAt(s.clock.Now()) {
		return Job{}, domain.ErrOutsideAllowedWindow
	}

	thresholds, err := s.scenarios.ScenarioThresholds(ctx, orgID, current.ScenarioID)
	if err != nil {
		return Job{}, fmt.Errorf("read the thresholds: %w", err)
	}

	// From the run, not from the scenario: what was frozen at launch is what
	// gets executed, whatever the scenario says today.
	options, err := s.options.Build(current.Profile, current.ProfileParams, thresholds)
	if err != nil {
		return Job{}, fmt.Errorf("build the generator options: %w", err)
	}

	// The environment's credentials, decrypted here and handed to the generator
	// in its environment. This is the last place in Ballast where they exist as
	// anything readable.
	secrets, err := s.secrets.SecretsOf(ctx, orgID, current.EnvironmentID)
	if err != nil {
		return Job{}, fmt.Errorf("read the environment's secrets: %w", err)
	}

	return Job{
		RunID:   current.ID,
		Script:  []byte(scenario.Script),
		Options: options,
		Env:     injected(secrets, environment),
	}, nil
}

// injected builds the generator's environment: the secrets first, then what
// Ballast puts there itself.
//
// The order is the point. BASE_URL is the target whose ownership was proved and
// approved, and writing it last means a secret of that name cannot redirect the
// load — whatever a secret is called, it loses. Keys that would do this are
// refused when a secret is stored, which is the check that is supposed to catch
// it; this is what holds if that check is ever widened by accident.
func injected(secrets map[string]string, environment domain.Environment) map[string]string {
	env := make(map[string]string, len(secrets)+1)
	for key, value := range secrets {
		env[key] = value
	}

	env[domain.BaseURLVariable] = environment.BaseURL

	return env
}
