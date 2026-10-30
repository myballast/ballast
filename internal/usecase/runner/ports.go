// Package runner is what a worker says about itself, and what is read back.
package runner

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Repository stores the presence of runners. Declared here, by the code that
// consumes it, and implemented in adapter/postgres.
type Repository interface {
	// Report writes what a runner says about itself, replacing what it said
	// before under the same name. Announcing and beating are the same call: a
	// runner that restarts takes its row back, and a row that was removed comes
	// back with the next beat.
	Report(ctx context.Context, orgID uuid.UUID, runner domain.Runner) error
	// List returns the runners of an organization, by name.
	List(ctx context.Context, orgID uuid.UUID) ([]domain.Runner, error)
}

// Clock is the time, injected so that nothing here reads the system's.
type Clock interface {
	Now() time.Time
}

// Directory lists the projects of an organization.
type Directory interface {
	ProjectIDs(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error)
}

// Authorizer answers what the caller may do. Every question it is asked names a
// project: a permission mask says what somebody may do, never what to, so a
// check without a scope is a bug rather than a refusal.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
}
