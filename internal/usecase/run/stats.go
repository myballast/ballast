package run

import (
	"context"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// The stretch a project's figures are read over. The default is what the page
// shows; the ceiling keeps one request from adding up a project's whole history.
const (
	DefaultStatsDays = 30
	MaxStatsDays     = 90
)

// ProjectStats is the five figures of a project over its last days, each set
// against the same number of days before.
//
// view_project is enough: the figures say what the list of runs says, added up,
// and whoever may read the runs may read their sum. The route's middleware has
// already checked it on the path's project; it is checked again here, so that
// the service stays correct whichever way it is reached.
func (s *Service) ProjectStats(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, days int,
) (domain.ProjectStats, error) {
	allowed, err := s.authz.Can(ctx, who, domain.PermViewProject, projectID)
	if err != nil {
		return domain.ProjectStats{}, err
	}
	if !allowed {
		s.log.InfoContext(ctx, "project stats refused",
			"org_id", who.OrgID, "user_id", who.UserID, "project_id", projectID)
		return domain.ProjectStats{}, domain.ErrForbidden
	}

	periods := domain.PeriodsOf(s.clock.Now(), boundDays(days))

	// One read covers both windows: they are side by side.
	rows, err := s.runs.DailyStats(ctx, who.OrgID, projectID, periods.Previous.From, periods.Current.To)
	if err != nil {
		return domain.ProjectStats{}, err
	}

	return domain.SummarizeStats(periods, rows), nil
}

func boundDays(days int) int {
	switch {
	case days <= 0:
		return DefaultStatsDays
	case days > MaxStatsDays:
		return MaxStatsDays
	}
	return days
}
