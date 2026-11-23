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
	// CI is what the pipeline that launches the run says about itself, as it said
	// it. Optional. It is checked and tidied here (domain.ParseRunCI) before
	// anything else is read, and a mistake in it is a mistake in the request.
	CI *domain.RunCI
}

// admit is what stands before every way of asking for a run, and reads nothing
// of what is asked: the platform's switch, and the caller's limits.
func (s *Service) admit(ctx context.Context, actor domain.Identity) error {
	// The platform's switch, before anything is read. During an incident the
	// answer does not depend on the request, so nothing about the request is
	// looked at — and nothing is written.
	if !s.accepting {
		s.log.WarnContext(ctx, "run refused: this installation is not accepting new runs",
			"org_id", actor.OrgID, "user_id", actor.UserID, "token_id", actor.TokenID, "webhook_id", actor.WebhookID)
		// No project: nothing about the request was read, because during an
		// incident the answer does not depend on it. The entry belongs to the
		// organization, and every project's journal shows it.
		s.record(ctx, refused(actor,
			"this installation is not accepting new runs", s.clock.Now()))
		return domain.ErrRunsSuspended
	}

	// Before the scenario is even read. Somebody past their limit is past it
	// whatever they were about to ask for, and the limits are the last thing
	// standing between an installation and a loop.
	return s.withinLimits(ctx, actor)
}

// Enqueue accepts a run and leaves it waiting for a worker.
func (s *Service) Enqueue(ctx context.Context, cmd EnqueueCommand) (domain.Run, error) {
	if err := s.admit(ctx, cmd.Actor); err != nil {
		return domain.Run{}, err
	}

	params, ok := cmd.Profile.Defaults()
	if !ok {
		return domain.Run{}, fmt.Errorf("%w: unknown profile %q", domain.ErrInvalidProfileParams, cmd.Profile)
	}
	if cmd.Params != nil {
		params = *cmd.Params
	}

	// A mistake in what a pipeline says about itself is a mistake in the request,
	// and is told as one: nothing is written and nothing is journaled, for a
	// journal full of typing mistakes is one nobody reads.
	var ci *domain.RunCI
	if cmd.CI != nil {
		parsed, err := domain.ParseRunCI(*cmd.CI)
		if err != nil {
			return domain.Run{}, err
		}
		ci = &parsed
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
			"org_id", cmd.Actor.OrgID, "user_id", cmd.Actor.UserID, "token_id", cmd.Actor.TokenID,
			"webhook_id", cmd.Actor.WebhookID,
			"project_id", scenario.ProjectID, "scenario_id", scenario.ID)
		s.record(ctx, refused(cmd.Actor,
			"the caller may not run tests in this project", s.clock.Now()).
			InProject(scenario.ProjectID))
		return domain.Run{}, domain.ErrForbidden
	}

	return s.queue(ctx, cmd.Actor, scenario, cmd.EnvironmentID, cmd.Profile, params, nil, ci)
}

// queue is what every way of asking for a run has in common once the scenario
// is known and the caller may run tests in its project: the environment is
// read, has to belong to the same project, has to be verified, the load is
// clamped to its ceilings, the hours it accepts load are checked, and the run is
// written.
//
// scenario carries the version the run is to freeze: the one it is, for a run
// asked for today, and the one an earlier run froze, for a run that repeats it.
// rerunOf is that earlier run, or nil. ci is what the pipeline said about itself,
// already checked, or nil.
func (s *Service) queue(
	ctx context.Context,
	actor domain.Identity,
	scenario domain.Scenario,
	environmentID uuid.UUID,
	profile domain.ProfileKind,
	params domain.ProfileParams,
	rerunOf *uuid.UUID,
	ci *domain.RunCI,
) (domain.Run, error) {
	if err := s.checkScenario(ctx, actor, scenario); err != nil {
		return domain.Run{}, err
	}

	environment, err := s.envs.EnvironmentByID(ctx, actor.OrgID, environmentID)
	if err != nil {
		return domain.Run{}, err
	}

	// Row-level security guarantees both rows belong to the caller's
	// organization. It says nothing about them belonging to the same project,
	// and a run that mixes two is a run against a target the scenario's project
	// was never allowed to reach.
	if err := domain.RequireSameProject(scenario.ProjectID, environment.ProjectID); err != nil {
		s.record(ctx, refused(actor,
			"the scenario and the environment belong to different projects", s.clock.Now()).
			InProject(scenario.ProjectID))
		return domain.Run{}, err
	}

	// The control that keeps Ballast from being aimed at somebody else's
	// servers. It is checked here, before anything is written, on every path
	// that leads to a run.
	if !environment.IsVerified() {
		s.log.WarnContext(ctx, "run refused against an unverified target",
			"org_id", actor.OrgID,
			"environment_id", environment.ID,
			"verification_status", environment.VerificationStatus,
		)
		// The refusal the whole product rests on, and the entry a pilot is
		// measured by: somebody tried to send load at a host nobody proved
		// they own.
		s.record(ctx, refused(actor,
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
			"org_id", actor.OrgID,
			"environment_id", environment.ID,
			"at", now,
			"duration_s", params.TotalDuration(),
			"reason", err.Error(),
		)
		s.record(ctx, refused(actor,
			"the target accepts no load at this hour", now).
			InProject(scenario.ProjectID).
			With("environment_id", environment.ID.String()).
			With("duration_s", strconv.Itoa(params.TotalDuration())))
		return domain.Run{}, err
	}

	// Who launched it is a person, an API token or a webhook, and the run says
	// which: a pipeline's run has no person, and must not read as the run of a
	// member who has since left.
	toQueue := domain.NewRun(
		actor.OrgID, scenario, environment, profile, params, nil,
	).By(actor.Actor())
	if ci != nil {
		toQueue = toQueue.WithCI(*ci)
	}

	created, err := s.runs.Create(ctx, toQueue)
	if err != nil {
		return domain.Run{}, err
	}

	queued := domain.Allow(
		actor.OrgID, nil, domain.ActionRunQueued, domain.TargetRun, now,
	).By(actor.Actor()).InProject(scenario.ProjectID).On(created.ID).
		With("environment_id", environment.ID.String()).
		With("profile", string(profile)).
		With("target_vus", strconv.Itoa(params.TargetVUs)).
		With("duration_s", strconv.Itoa(params.TotalDuration()))
	if rerunOf != nil {
		// Which run this one repeats: an identifier, chosen by this code.
		queued = queued.With("rerun_of", rerunOf.String())
	}
	s.record(ctx, queued)

	s.log.InfoContext(ctx, "run queued",
		"run_id", created.ID,
		"scenario_id", scenario.ID,
		"environment_id", environment.ID,
		"profile", profile,
		"target_vus", params.TargetVUs,
		"duration_s", params.TotalDuration(),
	)

	return created, nil
}

// Rerun queues a run again: the same scenario at the same version, the same
// profile and parameters, against the same environment — all read from the run
// being repeated, and none from what the scenario or its defaults are today.
//
// It is a new run in every other respect, and goes through everything a new run
// goes through. The platform's switch, the limits, the permission to run tests,
// the verification of the target, the hours it accepts load and the ceilings it
// was declared with all apply: an environment that has been lowered or revoked
// since is a reason to refuse or to lower the load, and repeating a run is no
// way round that.
//
// Somebody who may not see the run is told it is not there, as for reading it.
// Somebody who may see it and not run tests in its project is refused, and the
// attempt is recorded.
func (s *Service) Rerun(ctx context.Context, actor domain.Identity, runID uuid.UUID) (domain.Run, error) {
	if err := s.admit(ctx, actor); err != nil {
		return domain.Run{}, err
	}

	source, err := s.runs.ByID(ctx, actor.OrgID, runID)
	if err != nil {
		return domain.Run{}, err
	}

	visible, err := s.authz.Can(ctx, actor, domain.PermViewProject, source.ProjectID)
	if err != nil {
		return domain.Run{}, err
	}
	if !visible {
		s.log.InfoContext(ctx, "run hidden for want of permission",
			"org_id", actor.OrgID, "user_id", actor.UserID, "project_id", source.ProjectID, "run_id", runID)
		return domain.Run{}, domain.ErrNotFound
	}

	allowed, err := s.authz.Can(ctx, actor, domain.PermRunTest, source.ProjectID)
	if err != nil {
		return domain.Run{}, err
	}
	if !allowed {
		s.log.InfoContext(ctx, "run refused for want of permission",
			"org_id", actor.OrgID, "user_id", actor.UserID,
			"project_id", source.ProjectID, "run_id", runID)
		s.record(ctx, refused(actor,
			"the caller may not run tests in this project", s.clock.Now()).
			InProject(source.ProjectID))
		return domain.Run{}, domain.ErrForbidden
	}

	// What the run being repeated said about the pipeline that launched it is
	// carried over, as a copy: the page of the repeat says where what it repeats
	// came from.
	var ci *domain.RunCI
	if source.CI != nil {
		repeated := *source.CI
		ci = &repeated
	}

	scenario := domain.Scenario{ID: source.ScenarioID, ProjectID: source.ProjectID, Version: source.ScenarioVersion}
	return s.queue(ctx, actor, scenario, source.EnvironmentID, source.Profile, source.ProfileParams, &source.ID, ci)
}

// Detail is a run, what it measured, and who started it: what the page of a run
// is made of. The same rules as ByID apply, since it is ByID.
func (s *Service) Detail(
	ctx context.Context, who domain.Identity, id uuid.UUID,
) (Listed, domain.RunResults, error) {
	found, results, err := s.ByID(ctx, who, id)
	if err != nil {
		return Listed{}, domain.RunResults{}, err
	}

	detail := Listed{Run: found}
	switch {
	case found.TriggeredBy != nil:
		if detail.TriggeredByName, err = s.runs.UserName(ctx, who.OrgID, *found.TriggeredBy); err != nil {
			return Listed{}, domain.RunResults{}, err
		}
	case found.TriggeredByToken != nil:
		// Said as a token's, with its name: a person of the same name must not be
		// mistaken for a pipeline, and the other way round.
		name, err := s.runs.TokenName(ctx, who.OrgID, *found.TriggeredByToken)
		if err != nil {
			return Listed{}, domain.RunResults{}, err
		}
		detail.TriggeredByName = tokenLabel(name)
	case found.TriggeredByWebhook != nil:
		// Said as the webhook's. It has no name: a project has one.
		detail.TriggeredByName = "the project's webhook"
	}
	return detail, results, nil
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

// checkScenario looks over what is about to be run, once more.
//
// A graph is sound when it is stored, and a script parses. Rules get added since,
// though, and the run is the moment a fault stops being a thing to fix and starts
// costing a measurement: a graph stored under the older rules that would be refused
// today is refused here, with every reason, and a script k6 could not load is found
// before a run exists rather than as an error in the middle of one.
//
// It is not an entry in the audit journal: nobody was stopped from doing harm, a
// scenario is broken.
func (s *Service) checkScenario(ctx context.Context, actor domain.Identity, scenario domain.Scenario) error {
	// A run repeated from an earlier one names its scenario and the version it
	// froze, and carries nothing else. What is looked over is that version — what
	// will be executed, not what the scenario has become.
	if scenario.Script == "" && scenario.Version > 0 {
		version, err := s.scenarios.ScenarioVersionOf(ctx, actor.OrgID, scenario.ID, scenario.Version)
		if err != nil {
			return err
		}
		scenario.Script, scenario.Mode, scenario.Graph = version.Script, version.Mode, version.Graph
	}

	if scenario.Mode == domain.ModeGraph && scenario.Graph != nil {
		if findings := scenario.Graph.Check(); len(findings) > 0 {
			s.log.InfoContext(ctx, "run refused: the graph is no longer valid",
				"org_id", actor.OrgID, "scenario_id", scenario.ID, "findings", len(findings))
			return &domain.GraphRefused{Findings: findings}
		}
	}

	if s.scripts != nil {
		if report := s.scripts.Inspect(scenario.Script); report.Err() != nil {
			s.log.InfoContext(ctx, "run refused: the script does not parse",
				"org_id", actor.OrgID, "scenario_id", scenario.ID)
			return report.Err()
		}
	}
	return nil
}

// tokenLabel is how a run launched by an API token says so. A token that has no
// name any more is still a token's run.
func tokenLabel(name string) string {
	if name == "" {
		return "an API token"
	}
	return "API token " + name
}
