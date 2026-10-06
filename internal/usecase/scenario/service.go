package scenario

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Service orchestrates scenarios, their versions and their thresholds.
type Service struct {
	scenarios  Repository
	thresholds Thresholds
	inspector  ScriptInspector
	authz      Authorizer
	clock      Clock
	log        *slog.Logger
}

// NewService wires the service.
func NewService(
	scenarios Repository,
	thresholds Thresholds,
	inspector ScriptInspector,
	authz Authorizer,
	clock Clock,
	log *slog.Logger,
) *Service {
	return &Service{
		scenarios:  scenarios,
		thresholds: thresholds,
		inspector:  inspector,
		authz:      authz,
		clock:      clock,
		log:        log,
	}
}

// InProject lists a project's scenarios.
func (s *Service) InProject(
	ctx context.Context, who domain.Identity, projectID uuid.UUID,
) ([]domain.Scenario, error) {
	if err := s.require(ctx, who, domain.PermViewProject, projectID); err != nil {
		return nil, err
	}

	return s.scenarios.InProject(ctx, who.OrgID, projectID)
}

// NewScenario is a scenario to create.
type NewScenario struct {
	ProjectID            uuid.UUID
	Name                 string
	Script               string
	DefaultProfile       domain.ProfileKind
	DefaultProfileParams *domain.ProfileParams
}

// Create stores a scenario and its first version.
//
// The script is inspected first, and a script that addresses a host of its own,
// or never reads the one it is given, is refused rather than stored and refused
// later. The checks are about what the scenario would reach, so the right moment
// is before it exists.
func (s *Service) Create(
	ctx context.Context, who domain.Identity, request NewScenario,
) (domain.Scenario, domain.ScriptReport, error) {
	if err := s.require(ctx, who, domain.PermManageScenarios, request.ProjectID); err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, err
	}
	if err := domain.ValidateScenario(request.Name); err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, err
	}

	profile, params, err := resolveDefaults(request.DefaultProfile, request.DefaultProfileParams)
	if err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, err
	}

	report := s.inspector.Inspect(request.Script)
	if err := report.Err(); err != nil {
		s.log.InfoContext(ctx, "scenario refused",
			"org_id", who.OrgID, "project_id", request.ProjectID,
			"findings", len(report.Errors()))
		return domain.Scenario{}, report, err
	}

	created, err := s.scenarios.Create(ctx, domain.Scenario{
		ID:                   uuid.New(),
		ProjectID:            request.ProjectID,
		Name:                 request.Name,
		Script:               request.Script,
		Version:              1,
		DefaultProfile:       profile,
		DefaultProfileParams: params,
	}, who, s.clock.Now())
	if err != nil {
		return domain.Scenario{}, report, err
	}

	s.log.InfoContext(ctx, "scenario created",
		"org_id", who.OrgID, "project_id", request.ProjectID,
		"scenario_id", created.ID, "warnings", len(report.Warnings()))

	return created, report, nil
}

// ByID returns a scenario and what its script is worth saying about.
//
// The report comes back with it because a warning is only useful before a run:
// this is where somebody about to launch one sees that their requests carry no
// name tags.
func (s *Service) ByID(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID,
) (domain.Scenario, domain.ScriptReport, error) {
	found, err := s.read(ctx, who, domain.PermViewProject, projectID, id)
	if err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, err
	}

	return found, s.inspector.Inspect(found.Script), nil
}

// Update is what a caller wants changed. Anything left nil stays as it is.
type Update struct {
	Name                 *string
	Script               *string
	DefaultProfile       *domain.ProfileKind
	DefaultProfileParams *domain.ProfileParams
}

// Update changes a scenario.
//
// A new version is written when the script changes, and only then. A rename is
// not something a run executed, and the defaults are a suggestion: a run freezes
// its own profile and parameters when it is queued, so changing them here cannot
// alter what any past run did.
func (s *Service) Update(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID, update Update,
) (domain.Scenario, domain.ScriptReport, error) {
	current, err := s.read(ctx, who, domain.PermManageScenarios, projectID, id)
	if err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, err
	}

	if update.Name != nil {
		if err := domain.ValidateScenario(*update.Name); err != nil {
			return domain.Scenario{}, domain.ScriptReport{}, err
		}

		current, err = s.scenarios.Rename(ctx, who.OrgID, id, *update.Name, s.clock.Now())
		if err != nil {
			return domain.Scenario{}, domain.ScriptReport{}, err
		}
	}

	script := current.Script
	if update.Script != nil {
		script = *update.Script
	}

	profile := current.DefaultProfile
	if update.DefaultProfile != nil {
		profile = *update.DefaultProfile
	}
	params := current.DefaultProfileParams
	if update.DefaultProfileParams != nil {
		params = *update.DefaultProfileParams
	}

	resolvedProfile, resolvedParams, err := resolveDefaults(profile, &params)
	if err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, err
	}

	report := s.inspector.Inspect(script)
	if err := report.Err(); err != nil {
		return domain.Scenario{}, report, err
	}

	unchanged := script == current.Script &&
		resolvedProfile == current.DefaultProfile &&
		resolvedParams == current.DefaultProfileParams
	if unchanged {
		return current, report, nil
	}

	// The next version. Nothing overwrites the one before it.
	updated, err := s.scenarios.NewVersion(ctx, who.OrgID, id, domain.ScenarioVersion{
		ScenarioID:           id,
		Version:              current.Version + 1,
		Script:               script,
		DefaultProfile:       resolvedProfile,
		DefaultProfileParams: resolvedParams,
	}, who, s.clock.Now())
	if err != nil {
		return domain.Scenario{}, report, err
	}

	s.log.InfoContext(ctx, "scenario version written",
		"org_id", who.OrgID, "scenario_id", id,
		"version", updated.Version, "warnings", len(report.Warnings()))

	return updated, report, nil
}

// Versions lists every version ever written, newest first.
func (s *Service) Versions(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID,
) ([]domain.ScenarioVersion, error) {
	if _, err := s.read(ctx, who, domain.PermViewProject, projectID, id); err != nil {
		return nil, err
	}

	return s.scenarios.Versions(ctx, who.OrgID, id)
}

// Delete removes a scenario that holds no run.
func (s *Service) Delete(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID,
) error {
	if _, err := s.read(ctx, who, domain.PermManageScenarios, projectID, id); err != nil {
		return err
	}

	if err := s.scenarios.Delete(ctx, who.OrgID, id); err != nil {
		return err
	}

	s.log.InfoContext(ctx, "scenario deleted", "org_id", who.OrgID, "scenario_id", id, "by", who.UserID)
	return nil
}

// Thresholds lists the rules a scenario imposes.
func (s *Service) Thresholds(
	ctx context.Context, who domain.Identity, projectID, scenarioID uuid.UUID,
) ([]domain.Threshold, error) {
	if _, err := s.read(ctx, who, domain.PermViewProject, projectID, scenarioID); err != nil {
		return nil, err
	}

	return s.thresholds.OfScenario(ctx, who.OrgID, scenarioID)
}

// AddThreshold imposes a rule on one metric.
func (s *Service) AddThreshold(
	ctx context.Context, who domain.Identity, projectID, scenarioID uuid.UUID, metric, expression string,
) (domain.Threshold, error) {
	if _, err := s.read(ctx, who, domain.PermManageScenarios, projectID, scenarioID); err != nil {
		return domain.Threshold{}, err
	}
	if err := domain.ValidateThreshold(metric, expression); err != nil {
		return domain.Threshold{}, err
	}

	return s.thresholds.Add(ctx, who.OrgID, scenarioID, domain.Threshold{
		ID:         uuid.New(),
		Metric:     metric,
		Expression: expression,
	})
}

// RemoveThreshold drops a rule.
//
// Runs already recorded keep their verdicts: those live in run_threshold, frozen
// when the run finished, and nothing here touches them.
func (s *Service) RemoveThreshold(
	ctx context.Context, who domain.Identity, projectID, scenarioID, thresholdID uuid.UUID,
) error {
	if _, err := s.read(ctx, who, domain.PermManageScenarios, projectID, scenarioID); err != nil {
		return err
	}

	return s.thresholds.Remove(ctx, who.OrgID, scenarioID, thresholdID)
}

// read checks the permission, reads the scenario, and checks that it really
// belongs to the project the path named.
//
// The last part is what row-level security cannot do: both rows are the caller's
// organization, and a scenario of another project reached through a project the
// caller administers would otherwise pass every check.
func (s *Service) read(
	ctx context.Context, who domain.Identity, perm domain.Permission, projectID, id uuid.UUID,
) (domain.Scenario, error) {
	if err := s.require(ctx, who, perm, projectID); err != nil {
		return domain.Scenario{}, err
	}

	found, err := s.scenarios.ByID(ctx, who.OrgID, id)
	if err != nil {
		return domain.Scenario{}, err
	}

	if err := domain.RequireSameProject(projectID, found.ProjectID); err != nil {
		// Named as absent rather than refused: the caller asked about a project
		// they may see, and this scenario is not in it.
		return domain.Scenario{}, domain.ErrNotFound
	}

	return found, nil
}

func (s *Service) require(
	ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID,
) error {
	allowed, err := s.authz.Can(ctx, who, perm, projectID)
	if err != nil {
		return err
	}
	if !allowed {
		s.log.InfoContext(ctx, "request refused",
			"org_id", who.OrgID, "user_id", who.UserID,
			"project_id", projectID, "permission", int64(perm))
		return domain.ErrForbidden
	}
	return nil
}

// resolveDefaults fills in a profile's own template when no parameters were
// given, and refuses a profile Ballast does not know.
func resolveDefaults(
	profile domain.ProfileKind, params *domain.ProfileParams,
) (domain.ProfileKind, domain.ProfileParams, error) {
	if profile == "" {
		profile = domain.ProfileSmoke
	}

	template, known := profile.Defaults()
	if !known {
		return "", domain.ProfileParams{}, domain.ErrInvalidProfileParams
	}
	if params == nil {
		return profile, template, nil
	}

	if err := params.Validate(); err != nil {
		return "", domain.ProfileParams{}, err
	}
	return profile, *params, nil
}
