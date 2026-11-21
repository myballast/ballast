package apitoken

import (
	"context"
	"time"

	"github.com/google/uuid"
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
