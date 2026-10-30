package runner

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Service reads what the runners of an organization say about themselves.
type Service struct {
	repo     Repository
	policy   Policy
	projects Directory
	authz    Authorizer
	clock    Clock
	log      *slog.Logger
}

// NewService returns the reader.
func NewService(
	repo Repository, policy Policy, projects Directory, authz Authorizer, clock Clock, log *slog.Logger,
) *Service {
	return &Service{repo: repo, policy: policy, projects: projects, authz: authz, clock: clock, log: log}
}

// View is one runner as a reader sees it: what it said, and what that means now.
type View struct {
	Runner domain.Runner
	// Status is read against the clock, never stored.
	Status domain.RunnerStatus
	// Outdated says the runner is below the minimum, and takes no run.
	Outdated bool
}

// Overview is every runner of the organization, and the minimum they are held to.
type Overview struct {
	Runners         []View
	MinimumContract int
}

// Overview reads the runners of the caller's organization.
//
// Reserved to an administrator of every project of it, for the reason the
// emergency stop and the organization's usage are: it is a state of the whole
// platform, and somebody who administers only part of it is refused rather than
// shown a picture that leaves out the rest, which would be taken for all of it.
func (s *Service) Overview(ctx context.Context, who domain.Identity) (Overview, error) {
	if err := s.requireAdministratorEverywhere(ctx, who); err != nil {
		return Overview{}, err
	}

	minimum, err := s.policy.MinimumContract(ctx, who.OrgID)
	if err != nil {
		return Overview{}, fmt.Errorf("read the minimum contract: %w", err)
	}

	runners, err := s.repo.List(ctx, who.OrgID)
	if err != nil {
		return Overview{}, fmt.Errorf("list the runners: %w", err)
	}

	now := s.clock.Now()
	views := make([]View, 0, len(runners))
	for _, runner := range runners {
		views = append(views, View{
			Runner:   runner,
			Status:   runner.StatusAt(now),
			Outdated: runner.Outdated(minimum),
		})
	}

	return Overview{Runners: views, MinimumContract: minimum}, nil
}

// requireAdministratorEverywhere refuses anybody who does not administer every
// project of the organization. The same rule as the one in the run service,
// asked the same way: project by project, because a check without a scope is a
// bug.
func (s *Service) requireAdministratorEverywhere(ctx context.Context, who domain.Identity) error {
	projects, err := s.projects.ProjectIDs(ctx, who.OrgID)
	if err != nil {
		return fmt.Errorf("list the projects: %w", err)
	}

	for _, project := range projects {
		allowed, err := s.authz.Can(ctx, who, domain.PermManageProject, project)
		if err != nil {
			return err
		}
		if !allowed {
			s.log.InfoContext(ctx, "an organization-wide reading was refused",
				"org_id", who.OrgID, "user_id", who.UserID, "project_id", project)
			return domain.ErrForbidden
		}
	}

	return nil
}
