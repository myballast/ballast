package run

import (
	"context"
	"fmt"
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

// Cursor is a place in the list: a page ends, or begins, at it. A run's
// creation time alone would not do, since two runs can be created in the same
// instant; the id breaks the tie, as it does in the order the runs are listed
// in.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Filter narrows a project's runs. Every field is optional: the zero value
// matches all of them.
type Filter struct {
	Status        domain.RunStatus
	ScenarioID    *uuid.UUID
	EnvironmentID *uuid.UUID
}

// Window says which side of a cursor to read. At most one is set: neither is
// the newest runs, Before is the page of older runs, After the page of newer.
type Window struct {
	Before *Cursor
	After  *Cursor
}

// ListQuery is what a list of runs is asked for.
type ListQuery struct {
	Filter Filter
	Window Window
	Limit  int
}

// Listed is a run as a list shows it: the run, and the few things a row says
// about it that are not on the run itself.
type Listed struct {
	domain.Run
	// TriggeredByName is who started it. Empty when nobody did, or when the
	// person has since been removed.
	TriggeredByName string
	// P95Ms and ErrorRate are the headline numbers of a finished run, as k6
	// reported them. Nil while the run has none: queued, running, or ended
	// before it measured anything.
	P95Ms     *float64
	ErrorRate *float64
}

// Page is one page of a project's runs. Next is where the older runs begin and
// Prev where the newer ones do; each is nil when there is nothing that way.
type Page struct {
	Runs []Listed
	Next *Cursor
	Prev *Cursor
}

// InProject lists a project's runs, newest first, a page at a time, in either
// direction from a cursor.
//
// The route's middleware has already checked view_project on the path's
// project; it is checked again here, so that the service stays correct
// whichever way it is reached.
func (s *Service) InProject(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, query ListQuery,
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

	if query.Filter.Status != "" && !query.Filter.Status.Known() {
		return Page{}, fmt.Errorf("%w: %q is not a status", domain.ErrInvalidRunFilter, query.Filter.Status)
	}
	if query.Window.Before != nil && query.Window.After != nil {
		return Page{}, fmt.Errorf("%w: a page cannot be both before and after", domain.ErrInvalidRunFilter)
	}

	limit := boundLimit(query.Limit)

	// One row more than the page, to learn whether another page follows
	// without counting the project's whole history.
	runs, err := s.runs.InProject(ctx, who.OrgID, projectID, query.Filter, query.Window, limit+1)
	if err != nil {
		return Page{}, err
	}

	return paged(runs, limit, query.Window), nil
}

// paged cuts the extra row off and says where the neighbouring pages begin.
//
// The extra row sits at the far end of the direction that was read: the oldest
// when reading toward older runs, the newest when reading toward newer ones.
// Either way it is only there to say "there is more", and is not shown.
func paged(runs []Listed, limit int, window Window) Page {
	more := len(runs) > limit

	if window.After != nil {
		if more {
			runs = runs[1:]
		}
		page := Page{Runs: runs}
		if len(runs) > 0 {
			// The cursor itself is older than every run read after it, so there
			// is an older page to go back to.
			page.Next = cursorOf(runs[len(runs)-1])
			if more {
				page.Prev = cursorOf(runs[0])
			}
		}
		return page
	}

	if more {
		runs = runs[:limit]
	}
	page := Page{Runs: runs}
	if len(runs) > 0 {
		if more {
			page.Next = cursorOf(runs[len(runs)-1])
		}
		// Having been given a cursor to read before, there are newer runs: the
		// ones the cursor came from.
		if window.Before != nil {
			page.Prev = cursorOf(runs[0])
		}
	}
	return page
}

func cursorOf(l Listed) *Cursor {
	return &Cursor{CreatedAt: l.CreatedAt, ID: l.ID}
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
