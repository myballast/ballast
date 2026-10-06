package authz

import (
	"context"

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
