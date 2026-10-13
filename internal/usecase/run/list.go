package run

import (
	"context"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// How many runs one list may hold. The default is a screenful; the ceiling
// keeps a single request from reading a project's whole history at once.
const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

// InProject lists a project's runs, newest first.
//
// The route's middleware has already checked view_project on the path's
// project; it is checked again here, so that the service stays correct
// whichever way it is reached.
func (s *Service) InProject(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, limit int,
) ([]domain.Run, error) {
	allowed, err := s.authz.Can(ctx, who, domain.PermViewProject, projectID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		s.log.InfoContext(ctx, "run list refused",
			"org_id", who.OrgID, "user_id", who.UserID, "project_id", projectID)
		return nil, domain.ErrForbidden
	}

	return s.runs.InProject(ctx, who.OrgID, projectID, boundLimit(limit))
}

func boundLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultListLimit
	case limit > MaxListLimit:
		return MaxListLimit
	}
	return limit
}
