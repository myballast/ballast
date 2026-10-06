package environment

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// The interfaces below are declared here, by the code that consumes them, and
// implemented in adapter/postgres.

// Repository stores target environments.
//
// Every method runs inside a transaction scoped to orgID, which is what makes
// row-level security apply.
type Repository interface {
	// Create writes the environment with the verification token it will have to
	// serve. It arrives unverified, and nothing here can make it otherwise.
	Create(ctx context.Context, orgID uuid.UUID, newEnvironment domain.Environment, token string, at time.Time) (domain.Environment, error)
	ByID(ctx context.Context, orgID, id uuid.UUID) (domain.Environment, error)
	InProject(ctx context.Context, orgID, projectID uuid.UUID) ([]domain.Environment, error)
	// Save writes the mutable half of an environment: its name, its target, its
	// ceilings and its window. resetVerification clears the proof and the
	// approval, for when the target itself has changed.
	Save(ctx context.Context, orgID uuid.UUID, updated domain.Environment, resetVerification bool) (domain.Environment, error)
	// Delete refuses an environment that still holds runs, and checks that in
	// the transaction that would have deleted it.
	Delete(ctx context.Context, orgID, id uuid.UUID) error
}

// Secrets stores the credentials a scenario needs against one target.
//
// There is no method that returns a value, and that absence is the design. This
// is the port the API process holds, so the API has no way to read a secret
// back — not a rule somebody has to remember, but a capability it was never
// given. What the generator needs is read through a different port, declared by
// the run use case, which only the worker is wired to.
type Secrets interface {
	// Keys lists what is set, without the values.
	Keys(ctx context.Context, orgID, environmentID uuid.UUID) ([]domain.Secret, error)
	// Put stores a value under key, replacing whatever was there. The value is
	// encrypted before it reaches a disk.
	Put(ctx context.Context, orgID, environmentID uuid.UUID, key, value string, at time.Time) (domain.Secret, error)
	Remove(ctx context.Context, orgID, environmentID uuid.UUID, key string) error
}

// Tokens makes the challenge an environment has to serve to prove somebody owns
// it.
//
// Created with the environment because the column cannot be empty, and used by
// nothing yet: reading the proof and approving the target are the verification's
// business.
type Tokens interface {
	New() (string, error)
}

// Authorizer answers what the caller may do. Every question names a project.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
}

// Clock exists so that what an environment records about when it changed can be
// controlled in a test.
type Clock interface {
	Now() time.Time
}
