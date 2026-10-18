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
	// Me reads the signed-in user and their organization.
	Me(ctx context.Context, who domain.Identity) (Me, error)
}

// Me is who is signed in, as the front end shows them.
type Me struct {
	UserID uuid.UUID
	// Subject is how the identity provider knows them. It never leaves the
	// service: it is what finds the other organizations they belong to.
	Subject string
	Name    string
	Email   string
	OrgID   uuid.UUID
	OrgName string
}

// Organization is somewhere a person belongs, as the organization switcher lists
// it.
type Organization struct {
	ID   uuid.UUID
	Name string
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
