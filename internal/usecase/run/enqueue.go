package run

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// EnqueueCommand is a request to run a scenario against an environment.
type EnqueueCommand struct {
	// Actor carries both the person and their organization. The two never
	// travel apart: one scopes the queries, the other scopes the permission,
	// and a command that could hold only the first would let the check be
	// forgotten.
	Actor         domain.Identity
	ScenarioID    uuid.UUID
	EnvironmentID uuid.UUID
	Profile       domain.ProfileKind
	// Params is optional. Left empty, the profile's own template is used.
	Params *domain.ProfileParams
}

// Enqueue accepts a run and leaves it waiting for a worker.
func (s *Service) Enqueue(ctx context.Context, cmd EnqueueCommand) (domain.Run, error) {
	// The platform's switch, before anything is read. During an incident the
	// answer does not depend on the request, so nothing about the request is
	// looked at — and nothing is written.
	if !s.accepting {
		s.log.WarnContext(ctx, "run refused: this installation is not accepting new runs",
			"org_id", cmd.Actor.OrgID, "user_id", cmd.Actor.UserID)
		// No project: nothing about the request was read, because during an
		// incident the answer does not depend on it. The entry belongs to the
		// organization, and every project's journal shows it.
		s.record(ctx, refused(cmd.Actor,
			"this installation is not accepting new runs", s.clock.Now()))
		return domain.Run{}, domain.ErrRunsSuspended
	}

	// Before the scenario is even read. Somebody past their limit is past it
	// whatever they were about to ask for, and the limits are the last thing
	// standing between an installation and a loop.
	if err := s.withinLimits(ctx, cmd.Actor); err != nil {
		return domain.Run{}, err
	}

	params, ok := cmd.Profile.Defaults()
	if !ok {
		return domain.Run{}, fmt.Errorf("%w: unknown profile %q", domain.ErrInvalidProfileParams, cmd.Profile)
	}
	if cmd.Params != nil {
		params = *cmd.Params
	}

	scenario, err := s.scenarios.ScenarioByID(ctx, cmd.Actor.OrgID, cmd.ScenarioID)
	if err != nil {
		return domain.Run{}, err
	}

	// Before the environment is even read. Asking the other way round would
	// tell somebody with no business in either project that the two identifiers
	// sit in different ones.
	allowed, err := s.authz.Can(ctx, cmd.Actor, domain.PermRunTest, scenario.ProjectID)
	if err != nil {
		return domain.Run{}, err
	}
	if !allowed {
		s.log.InfoContext(ctx, "run refused for want of permission",
			"org_id", cmd.Actor.OrgID, "user_id", cmd.Actor.UserID,
			"project_id", scenario.ProjectID, "scenario_id", scenario.ID)
		s.record(ctx, refused(cmd.Actor,
			"the caller may not run tests in this project", s.clock.Now()).
			InProject(scenario.ProjectID))
		return domain.Run{}, domain.ErrForbidden
	}

	environment, err := s.envs.EnvironmentByID(ctx, cmd.Actor.OrgID, cmd.EnvironmentID)
	if err != nil {
		return domain.Run{}, err
	}

	// Row-level security guarantees both rows belong to the caller's
	// organization. It says nothing about them belonging to the same project,
	// and a run that mixes two is a run against a target the scenario's project
	// was never allowed to reach.
	if err := domain.RequireSameProject(scenario.ProjectID, environment.ProjectID); err != nil {
		s.record(ctx, refused(cmd.Actor,
			"the scenario and the environment belong to different projects", s.clock.Now()).
			InProject(scenario.ProjectID))
		return domain.Run{}, err
	}

	// The control that keeps Ballast from being aimed at somebody else's
	// servers. It is checked here, before anything is written, on every path
	// that leads to a run.
	if !environment.IsVerified() {
		s.log.WarnContext(ctx, "run refused against an unverified target",
			"org_id", cmd.Actor.OrgID,
			"environment_id", environment.ID,
			"verification_status", environment.VerificationStatus,
		)
		// The refusal the whole product rests on, and the entry a pilot is
		// measured by: somebody tried to send load at a host nobody proved
		// they own.
		s.record(ctx, refused(cmd.Actor,
			"the target environment has not been verified", s.clock.Now()).
			InProject(scenario.ProjectID).
			With("environment_id", environment.ID.String()))
		return domain.Run{}, domain.ErrTargetNotVerified
	}

	if err := params.Validate(); err != nil {
		return domain.Run{}, err
	}

	// Clamped before the run is written, never after. What is stored is what
	// will actually be executed.
	params = params.ClampTo(environment)

	// The hours the target tolerates being loaded, asked after the clamp because
	// the clamp is what decides how long the run really is. Checked here so that
	// a run that cannot fit is refused rather than queued, and checked again by
	// the worker, because the queue is not instantaneous.
	now := s.clock.Now()
	runFor := time.Duration(params.TotalDuration()) * time.Second
	if err := environment.RequireWindowAllows(now, runFor); err != nil {
		s.log.InfoContext(ctx, "run refused by the target's window",
			"org_id", cmd.Actor.OrgID,
			"environment_id", environment.ID,
			"at", now,
			"duration_s", params.TotalDuration(),
			"reason", err.Error(),
		)
		s.record(ctx, refused(cmd.Actor,
			"the target accepts no load at this hour", now).
			InProject(scenario.ProjectID).
			With("environment_id", environment.ID.String()).
			With("duration_s", strconv.Itoa(params.TotalDuration())))
		return domain.Run{}, err
	}

	created, err := s.runs.Create(ctx, domain.NewRun(
		cmd.Actor.OrgID, scenario, environment, cmd.Profile, params, &cmd.Actor.UserID,
	))
	if err != nil {
		return domain.Run{}, err
	}

	s.record(ctx, domain.Allow(
		cmd.Actor.OrgID, &cmd.Actor.UserID, domain.ActionRunQueued, domain.TargetRun, now,
	).InProject(scenario.ProjectID).On(created.ID).
		With("environment_id", environment.ID.String()).
		With("profile", string(cmd.Profile)).
		With("target_vus", strconv.Itoa(params.TargetVUs)).
		With("duration_s", strconv.Itoa(params.TotalDuration())))

	s.log.InfoContext(ctx, "run queued",
		"run_id", created.ID,
		"scenario_id", scenario.ID,
		"environment_id", environment.ID,
		"profile", cmd.Profile,
		"target_vus", params.TargetVUs,
		"duration_s", params.TotalDuration(),
	)

	return created, nil
}

// ByID returns a run and what it measured.
//
// A run of a project the caller does not belong to answers as a run that is not
// there. Returning a refusal instead would confirm that the identifier stands
// for something, which is the one thing somebody guessing identifiers is after.
func (s *Service) ByID(
	ctx context.Context, who domain.Identity, id uuid.UUID,
) (domain.Run, domain.RunResults, error) {
	found, err := s.runs.ByID(ctx, who.OrgID, id)
	if err != nil {
		return domain.Run{}, domain.RunResults{}, err
	}

	allowed, err := s.authz.Can(ctx, who, domain.PermViewProject, found.ProjectID)
	if err != nil {
		return domain.Run{}, domain.RunResults{}, err
	}
	if !allowed {
		s.log.InfoContext(ctx, "run hidden for want of permission",
			"org_id", who.OrgID, "user_id", who.UserID, "project_id", found.ProjectID, "run_id", id)
		return domain.Run{}, domain.RunResults{}, domain.ErrNotFound
	}

	results, err := s.runs.Results(ctx, who.OrgID, id)
	if err != nil {
		return domain.Run{}, domain.RunResults{}, err
	}

	return found, results, nil
}
