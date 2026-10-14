package run

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// UsageInMonth is what an organization moved in one month.
//
// Reserved to an administrator of every project of the organization, for the
// reason the emergency stop is: a figure about the whole organization is only
// the whole organization's to read. Somebody who administers part of it is
// refused rather than given a total that silently leaves out the projects they
// are not in — a partial number read as a total is worse than no number.
func (s *Service) UsageInMonth(
	ctx context.Context, who domain.Identity, from, to time.Time,
) (domain.Usage, error) {
	if err := s.requireAdministratorEverywhere(ctx, who); err != nil {
		return domain.Usage{}, err
	}

	return s.runs.TrafficInPeriod(ctx, who.OrgID, from, to)
}

// ProjectUsageInMonth is what one project moved in one month.
//
// Reserved to an administrator of that project: what a project costs is a
// management figure, read by whoever answers for the project rather than by
// everybody who runs tests in it.
func (s *Service) ProjectUsageInMonth(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, from, to time.Time,
) (domain.Usage, error) {
	allowed, err := s.authz.Can(ctx, who, domain.PermManageProject, projectID)
	if err != nil {
		return domain.Usage{}, err
	}
	if !allowed {
		s.log.InfoContext(ctx, "usage hidden for want of permission",
			"org_id", who.OrgID, "user_id", who.UserID, "project_id", projectID)
		return domain.Usage{}, domain.ErrForbidden
	}

	return s.runs.ProjectTrafficInPeriod(ctx, who.OrgID, projectID, from, to)
}
