package run

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// EnqueueCommand is a request to run a scenario against an environment.
type EnqueueCommand struct {
	OrgID         uuid.UUID
	ScenarioID    uuid.UUID
	EnvironmentID uuid.UUID
	Profile       domain.ProfileKind
	// Params is optional. Left empty, the profile's own template is used.
	Params  *domain.ProfileParams
	ActorID *uuid.UUID
}

// Enqueue accepts a run and leaves it waiting for a worker.
func (s *Service) Enqueue(ctx context.Context, cmd EnqueueCommand) (domain.Run, error) {
	params, ok := cmd.Profile.Defaults()
	if !ok {
		return domain.Run{}, fmt.Errorf("%w: unknown profile %q", domain.ErrInvalidProfileParams, cmd.Profile)
	}
	if cmd.Params != nil {
		params = *cmd.Params
	}

	scenario, err := s.scenarios.ScenarioByID(ctx, cmd.OrgID, cmd.ScenarioID)
	if err != nil {
		return domain.Run{}, err
	}

	environment, err := s.envs.EnvironmentByID(ctx, cmd.OrgID, cmd.EnvironmentID)
	if err != nil {
		return domain.Run{}, err
	}

	// Row-level security guarantees both rows belong to the caller's
	// organization. It says nothing about them belonging to the same project,
	// and a run that mixes two is a run against a target the scenario's project
	// was never allowed to reach.
	if scenario.ProjectID != environment.ProjectID {
		return domain.Run{}, domain.ErrCrossProject
	}

	// The control that keeps Ballast from being aimed at somebody else's
	// servers. It is checked here, before anything is written, on every path
	// that leads to a run.
	if !environment.IsVerified() {
		s.log.WarnContext(ctx, "run refused against an unverified target",
			"org_id", cmd.OrgID,
			"environment_id", environment.ID,
			"verification_status", environment.VerificationStatus,
		)
		return domain.Run{}, domain.ErrTargetNotVerified
	}

	if err := params.Validate(); err != nil {
		return domain.Run{}, err
	}

	// Clamped before the run is written, never after. What is stored is what
	// will actually be executed.
	params = params.ClampTo(environment)

	created, err := s.runs.Create(ctx, domain.NewRun(
		cmd.OrgID, scenario, environment, cmd.Profile, params, cmd.ActorID,
	))
	if err != nil {
		return domain.Run{}, err
	}

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
func (s *Service) ByID(ctx context.Context, orgID, id uuid.UUID) (domain.Run, domain.RunResults, error) {
	found, err := s.runs.ByID(ctx, orgID, id)
	if err != nil {
		return domain.Run{}, domain.RunResults{}, err
	}

	results, err := s.runs.Results(ctx, orgID, id)
	if err != nil {
		return domain.Run{}, domain.RunResults{}, err
	}

	return found, results, nil
}
