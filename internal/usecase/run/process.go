package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// ProcessNext takes one queued run, executes it, and records the outcome.
//
// It reports whether there was anything to do, so a polling loop knows when to
// wait rather than spin.
//
// Claiming a run takes the environment's lock in the same transaction, so no
// two workers execute the same run and no two runs load the same target. For as
// long as the execution lasts, this worker says so every ten seconds; stop
// saying it and a collector ends the run and destroys what is left of it.
func (s *Service) ProcessNext(ctx context.Context, orgID uuid.UUID, logs chan<- domain.LogLine) (bool, error) {
	// The platform's switch, at the worker's door. The API refuses a new run at
	// its own; this is what keeps a queue that was filled before the switch was
	// thrown from draining itself afterwards. Runs already under way are left
	// alone: stopping those is the emergency stop's job, and restarting a
	// worker must not be a way of killing them.
	if !s.accepting {
		return false, nil
	}

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

	// The execution gets a context of its own so that the heartbeat can end it.
	// A worker that has lost its run — a collector decided it was dead — must
	// stop generating load, and cancelling is what takes the container down.
	executing, lost := context.WithCancel(ctx)
	defer lost()
	beating := s.beat(executing, orgID, claimed.ID, lost)

	// And the other watcher, reading rather than writing: it is what makes a
	// cancellation take effect in seconds. Two goroutines rather than one
	// because the two have different costs — a write every ten seconds is
	// affordable, a write every two would not be, and a read by primary key is.
	seen := &cancellation{}
	watching := s.watch(executing, orgID, claimed.ID, lost, seen)

	status, message := s.execute(executing, orgID, claimed, logs)

	// Before anything is written about the run: nothing must still be touching
	// its heartbeat, or deciding its fate, once it is finished.
	lost()
	<-beating
	<-watching

	// The execution ended in a cancelled context, and this is what knows why.
	// Without it the run would record "context canceled", which tells whoever
	// reads its history nothing about the decision somebody took.
	if status == domain.StatusCancelled && seen.requested() {
		message = cancelledMessage
	}

	if !claimed.Status.CanTransitionTo(status) {
		return true, fmt.Errorf("a run cannot go from %s to %s", claimed.Status, status)
	}

	// On ctx rather than on the execution's context, which was just cancelled.
	err = s.runs.Finish(ctx, orgID, claimed.ID, status, s.clock.Now(), message)
	if errors.Is(err, domain.ErrNotFound) {
		// The run left running while this worker was executing it, which is
		// what the heartbeat above reports as a loss. A collector has already
		// ended it; writing a second ending would overwrite the record of what
		// actually happened.
		s.log.WarnContext(ctx, "the run had already been ended by somebody else",
			"run_id", claimed.ID, "would_have_been", status)
		return true, nil
	}
	if err != nil {
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
		s.recordRefusalAtTheLastMoment(ctx, orgID, current, err)
		return domain.StatusErrored, err.Error()
	}

	// Everything the generator writes is stored as it is written, so that
	// somebody arriving mid-run sees what has happened so far, and censored on
	// the way — the one place that knows the values of this run's secrets is
	// the one that handed them over.
	captured, stored := s.capture(ctx, orgID, current.ID, s.redactorFor(ctx, current.ID, job), logs)

	outcome, err := s.executor.Execute(ctx, job, captured)

	// The runner never closes a channel it did not create, so this does, and
	// then waits: the last line of a run has to be stored before the run is
	// recorded as finished.
	close(captured)
	<-stored

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

	// The generator's own counters may be unusable, and the runner may have
	// failed to read the proxy's. Either way the volumes stored are zero and
	// the run is marked unmeasured, so that a month's total can say how much of
	// it nobody counted instead of quietly reading as free.
	if !results.Traffic.Measured {
		s.log.WarnContext(ctx, "the generator reported traffic counters that cannot be believed",
			"run_id", current.ID)
	}
	if !outcome.Egress.Measured {
		s.log.WarnContext(ctx, "what this run sent could not be counted on the wire",
			"run_id", current.ID)
	}

	if err := s.runs.SaveResults(ctx, orgID, current.ID, results, outcome.Egress); err != nil {
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
	// three in the morning is not a time to load this target. The length comes
	// from the run rather than the scenario, because what was frozen at launch is
	// what is about to be executed.
	runFor := time.Duration(current.ProfileParams.TotalDuration()) * time.Second
	if err := environment.RequireWindowAllows(s.clock.Now(), runFor); err != nil {
		return Job{}, err
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

	// What the generator is allowed to reach, which the hosted runner turns into
	// the only route its network has.
	allowed, err := s.allowedTarget(ctx, orgID, environment)
	if err != nil {
		return Job{}, err
	}

	// The environment's credentials, decrypted here and handed to the generator
	// in its environment. This is the last place in Ballast where they exist as
	// anything readable.
	secrets, err := s.secrets.SecretsOf(ctx, orgID, current.EnvironmentID)
	if err != nil {
		return Job{}, fmt.Errorf("read the environment's secrets: %w", err)
	}

	return Job{
		RunID:        current.ID,
		Script:       []byte(scenario.Script),
		Options:      options,
		Env:          injected(secrets, environment),
		AllowedHosts: allowed,
	}, nil
}

// allowedTarget is the one host:port this run may reach.
//
// Narrower than the project's allowlist on purpose: a run against staging has no
// business emitting traffic at production, even though both are approved for the
// project. The script inspector already refuses a scenario that writes a host of
// its own, so BASE_URL is the only address a script can ask for anyway.
//
// The project's list is still read, and the environment's host has to be in it.
// The two records of "this target was approved" must agree: a verified
// environment whose host is absent from the allowlist means one of them was
// changed behind the other's back, and a run is not the moment to decide which
// to believe.
func (s *Service) allowedTarget(
	ctx context.Context, orgID uuid.UUID, environment domain.Environment,
) ([]string, error) {
	hostPort, err := environment.HostPort()
	if err != nil {
		return nil, err
	}

	host, err := environment.Host()
	if err != nil {
		return nil, err
	}

	approved, err := s.allowlist.AllowedHosts(ctx, orgID, environment.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("read the project's allowed hosts: %w", err)
	}

	for _, allowed := range approved {
		if strings.EqualFold(allowed, host) {
			return []string{hostPort}, nil
		}
	}

	s.log.ErrorContext(ctx, "a verified target is absent from its project's allowlist",
		"org_id", orgID, "project_id", environment.ProjectID,
		"environment_id", environment.ID, "host", host)

	return nil, fmt.Errorf("%w: %s is not among the hosts this project may reach",
		domain.ErrTargetNotVerified, host)
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

// beat keeps a run's heartbeat fresh while it executes, and ends the execution
// if the run turns out not to be this worker's any more.
//
// It returns a channel closed once it has stopped, so the caller can be certain
// nothing writes to the run after the execution is over.
func (s *Service) beat(
	ctx context.Context, orgID, runID uuid.UUID, lost func(),
) <-chan struct{} {
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		ticker := time.NewTicker(s.beatEvery)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			ours, err := s.runs.Heartbeat(ctx, orgID, runID, s.clock.Now())
			switch {
			case err != nil && ctx.Err() != nil:
				// The execution ended while this write was in flight.
				return

			case err != nil:
				// A database briefly out of reach must not cost a run: the
				// staleness threshold is six beats wide exactly so that a
				// missed one is survivable, and the next tick tries again.
				s.log.WarnContext(ctx, "a heartbeat could not be recorded",
					"run_id", runID, "error", err)

			case !ours:
				// A collector decided this worker was dead and took the run
				// from it. Carrying on would mean two owners: a run recorded as
				// over, and a generator still loading the target. Cancelling
				// the execution destroys the container.
				s.log.ErrorContext(ctx, "this run was taken from this worker; stopping it",
					"run_id", runID)
				lost()
				return
			}
		}
	}()

	return stopped
}

// cancellation records that the run being executed was asked to stop, so that
// its ending can say so in words.
type cancellation struct {
	mu    sync.Mutex
	asked bool
}

func (c *cancellation) record() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.asked = true
}

func (c *cancellation) requested() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.asked
}

// watch ends the execution as soon as the run is asked to stop.
//
// It reads the run every couple of seconds and never writes: the promise is
// that a cancellation takes effect in under five seconds, and a container takes
// a moment of that to destroy. The heartbeat would notice a run taken away
// eventually; this notices a decision, and quickly.
//
// It returns a channel closed once it has stopped, so the caller can be certain
// nothing is still deciding the run's fate after the execution is over.
func (s *Service) watch(
	ctx context.Context, orgID, runID uuid.UUID, stop func(), seen *cancellation,
) <-chan struct{} {
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		ticker := time.NewTicker(s.watchEvery)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			current, err := s.runs.ByID(ctx, orgID, runID)
			switch {
			case err != nil && ctx.Err() != nil:
				// The execution ended while this read was in flight.
				return

			case err != nil:
				// A database briefly out of reach delays a cancellation by one
				// tick rather than costing a run. The next tick asks again.
				s.log.WarnContext(ctx, "a run could not be read while it executes",
					"run_id", runID, "error", err)

			case current.CancellationRequested():
				seen.record()
				s.log.InfoContext(ctx, "the run was asked to stop; stopping it",
					"run_id", runID, "asked_at", current.CancelRequestedAt)
				stop()
				return

			case current.Status != domain.StatusRunning:
				// Somebody ended it from elsewhere — a collector that decided
				// this worker was dead. The heartbeat reports the same thing;
				// this finds it sooner.
				s.log.ErrorContext(ctx, "this run left running while this worker executed it; stopping it",
					"run_id", runID, "status", current.Status)
				stop()
				return
			}
		}
	}()

	return stopped
}

// recordRefusalAtTheLastMoment writes the journal entry for a run that was
// accepted hours ago and stopped on the doorstep: its target is no longer
// verified, its host has left the project's allowlist, or the window it fitted
// in has closed since.
//
// It has no author. Nobody asked for this refusal — the person who queued the
// run asked for the opposite — and naming them would make the journal read as
// though they had done something at three in the morning.
//
// Only refusals are recorded, not every failed execution: a generator that
// could not start is an incident, and the run itself says so in its own error
// message. What belongs here is somebody being stopped from reaching a target.
func (s *Service) recordRefusalAtTheLastMoment(
	ctx context.Context, orgID uuid.UUID, current domain.Run, err error,
) {
	var reason string
	switch {
	case errors.Is(err, domain.ErrTargetNotVerified):
		// One message for two causes on purpose: the allowlist disagreeing
		// with the verification wraps the same sentinel, and at this point
		// Ballast does not decide which of the two records to believe.
		reason = "the target is not verified, or its host is not among those the project may reach"

	case errors.Is(err, domain.ErrOutsideAllowedWindow):
		reason = "the target accepts no load at this hour"

	default:
		return
	}

	s.record(ctx, domain.Refuse(
		orgID, nil, domain.ActionRunRefused, domain.TargetRun, reason, s.clock.Now(),
	).InProject(current.ProjectID).On(current.ID).
		With("environment_id", current.EnvironmentID.String()))
}
