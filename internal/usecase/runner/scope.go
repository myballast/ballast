package runner

import (
	"context"

	"github.com/google/uuid"
)

// Organizations is the list of organizations a worker may be asked to serve.
// Declared here, by the code that consumes it, and implemented in
// adapter/postgres.
//
// Both answers are lists of identifiers and nothing more, and a list that could
// not be read is an error, never an empty list.
type Organizations interface {
	// WithWork lists the organizations with a run that is queued, running or
	// orphaned.
	WithWork(ctx context.Context) ([]uuid.UUID, error)
	// All lists every organization.
	All(ctx context.Context) ([]uuid.UUID, error)
}

// Scope is which organizations a worker serves, and where it says it is there.
//
// The two questions differ. Active is where there may be something to do: asked
// every poll, so it is kept small. Present is where the worker is a runner: an
// organization with nothing queued still has to see its worker online.
type Scope interface {
	Active(ctx context.Context) ([]uuid.UUID, error)
	Present(ctx context.Context) ([]uuid.UUID, error)
}

// OnlyOrganization restricts a worker to one organization. It needs no list, and
// so no privilege to read one: it is what a worker was before it could serve
// several, and it stays available for a machine that is one customer's.
func OnlyOrganization(id uuid.UUID) Scope {
	return only{id: id}
}

type only struct{ id uuid.UUID }

func (o only) Active(context.Context) ([]uuid.UUID, error)  { return []uuid.UUID{o.id}, nil }
func (o only) Present(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{o.id}, nil }

// AllOrganizations lets a worker serve every organization.
func AllOrganizations(list Organizations) Scope {
	return every{list: list}
}

type every struct{ list Organizations }

func (e every) Active(ctx context.Context) ([]uuid.UUID, error)  { return e.list.WithWork(ctx) }
func (e every) Present(ctx context.Context) ([]uuid.UUID, error) { return e.list.All(ctx) }
