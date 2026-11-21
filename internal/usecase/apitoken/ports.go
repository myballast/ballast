package apitoken

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Found is what a hash resolves to, whatever became of the token: revoked and
// expired ones come back too, with their dates, because deciding that a token is
// dead belongs to the caller, which has the clock.
type Found struct {
	TokenID   uuid.UUID
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Repository reads tokens. Declared here, by the code that consumes it, and
// implemented in adapter/postgres.
type Repository interface {
	// ByHash resolves a hash to its token without knowing the organization,
	// which is what a request carrying one does not know. False when no token
	// has that hash.
	ByHash(ctx context.Context, hash string) (Found, bool, error)
	// Touch records that the token was used at the given moment. It may do
	// nothing when a use was recorded a moment ago: it is information for
	// whoever reads the list and never a condition of use.
	Touch(ctx context.Context, orgID, tokenID uuid.UUID, at time.Time) error
}

// Clock tells the time. Injected, so that the moment a token expires is a thing a
// test can choose.
type Clock interface {
	Now() time.Time
}

// Authorizer answers what a caller may do in a project. Declared here, by the
// code that consumes it, and implemented by usecase/authz.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
}

// Listed is a token as the list shows it: the token, and the name of the person
// who created it when that person still exists.
type Listed struct {
	domain.APIToken
	CreatedByName string
}

// Registry is what managing tokens writes and reads. Declared here, and
// implemented in adapter/postgres.
type Registry interface {
	// ByID returns a token under the organization, and false when there is none.
	ByID(ctx context.Context, orgID, tokenID uuid.UUID) (domain.APIToken, bool, error)
	// Create writes the token and its journal entry in one transaction, and
	// refuses with domain.ErrTooManyAPITokens when the project already holds the
	// most it may. The ceiling is checked in that transaction, so that two
	// creations at once cannot both pass it.
	Create(ctx context.Context, token domain.APIToken, tokenHash string, entry domain.AuditEntry) error
	// List returns the tokens of a project, revoked ones included, newest first.
	List(ctx context.Context, orgID, projectID uuid.UUID) ([]Listed, error)
	// Revoke withdraws the token and writes its journal entry in one
	// transaction. It returns domain.ErrNotFound when no live token matched,
	// which includes one that somebody else revoked a moment ago.
	Revoke(ctx context.Context, token domain.APIToken, at time.Time, entry domain.AuditEntry) error
}
