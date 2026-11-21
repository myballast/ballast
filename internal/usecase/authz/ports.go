package authz

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Memberships reads what a person is in a project. Declared here, by the code
// that consumes it, and implemented in adapter/postgres.
type Memberships interface {
	// OfMember returns one membership, and false when there is none. A person
	// with no membership in a project is told nothing about whether the project
	// exists.
	OfMember(ctx context.Context, who domain.Identity, projectID uuid.UUID) (domain.Membership, bool, error)
	// AllOfMember returns every membership the person holds in their
	// organization.
	AllOfMember(ctx context.Context, who domain.Identity) ([]domain.Membership, error)
}

// Tokens reads the row of an API token. It is read on every check, which is what
// makes revoking a token take effect on the next request.
type Tokens interface {
	// ByID returns the token under the organization, and false when there is
	// none — including a token that belongs to another organization, which from
	// here is the same thing.
	ByID(ctx context.Context, orgID, tokenID uuid.UUID) (domain.APIToken, bool, error)
}

// Clock tells the time, so that the instant a token expires can be chosen in a
// test.
type Clock interface {
	Now() time.Time
}
