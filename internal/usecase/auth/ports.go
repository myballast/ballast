package auth

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// IdentityProvider is somewhere a person proves who they are: GitHub, Google.
//
// The redirect dance is OAuth2 with PKCE. state and verifier are generated and
// kept by the caller, between the two halves of it.
type IdentityProvider interface {
	// AuthCodeURL is where the browser is sent to sign in.
	AuthCodeURL(state, verifier string) string
	// Exchange trades the code the provider sent back for the identity it
	// vouches for.
	Exchange(ctx context.Context, code, verifier string) (domain.ExternalIdentity, error)
}

// Accounts finds and creates the people who sign in.
type Accounts interface {
	// Lookup resolves a subject without knowing its organization, which is the
	// situation at sign-in. Every match comes back, oldest first.
	Lookup(ctx context.Context, subject string) ([]domain.Identity, error)
	// Create stores a new organization with its first user, project and
	// membership, all at once.
	Create(ctx context.Context, account NewAccount) error
}

// NewAccount is everything a first sign-in creates.
type NewAccount struct {
	OrgID   uuid.UUID
	OrgName string

	UserID  uuid.UUID
	Subject string
	Email   string
	Name    string

	ProjectID   uuid.UUID
	ProjectName string
	ProjectSlug string

	// Role is the user's role in that first project.
	Role domain.Role

	CreatedAt time.Time
}

// Clock exists so that what an account records about when it was created can
// be controlled in a test.
type Clock interface {
	Now() time.Time
}
