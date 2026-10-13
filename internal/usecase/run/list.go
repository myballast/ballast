package run

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// How many runs one list may hold. The default is a screenful; the ceiling
// keeps a single request from reading a project's whole history at once.
const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

// Cursor is where a page of runs ends: the next page holds the runs strictly
// older than it. A run's creation time alone would not do, since two runs can
// be created in the same instant; the id breaks the tie, as it does in the
// order the runs are listed in.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Page is one page of a project's runs. Next is nil on the last page.
type Page struct {
	Runs []domain.Run
	Next *Cursor
}

// InProject lists a project's runs, newest first, a page at a time.
//
// The route's middleware has already checked view_project on the path's
// project; it is checked again here, so that the service stays correct
// whichever way it is reached.
func (s *Service) InProject(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, before *Cursor, limit int,
) (Page, error) {
	allowed, err := s.authz.Can(ctx, who, domain.PermViewProject, projectID)
	if err != nil {
		return Page{}, err
	}
	if !allowed {
		s.log.InfoContext(ctx, "run list refused",
			"org_id", who.OrgID, "user_id", who.UserID, "project_id", projectID)
		return Page{}, domain.ErrForbidden
	}

	limit = boundLimit(limit)

	// One row more than the page, to learn whether another page follows
	// without counting the project's whole history.
	runs, err := s.runs.InProject(ctx, who.OrgID, projectID, before, limit+1)
	if err != nil {
		return Page{}, err
	}
	if len(runs) <= limit {
		return Page{Runs: runs}, nil
	}

	runs = runs[:limit]
	last := runs[len(runs)-1]
	return Page{Runs: runs, Next: &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}}, nil
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
