package invitation

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Repository stores invitations. Declared here, by the code that consumes it,
// and implemented in adapter/postgres.
type Repository interface {
	// Create writes the invitation and its journal entry in one transaction: a
	// link that exists with nobody's name on it is a way in nobody gave. An
	// earlier invitation for the same address that expired unused is revoked in
	// the same transaction, so that expiry never blocks inviting again.
	Create(ctx context.Context, invitation domain.Invitation, tokenHash string, entry domain.AuditEntry) error
	// Pending lists the invitations of a project that were neither used nor
	// revoked, newest first. Expired ones are included, and say so by their date.
	Pending(ctx context.Context, orgID, projectID uuid.UUID) ([]domain.Invitation, error)
	// Revoke withdraws a pending invitation, with its journal entry. An
	// invitation that is not there, or not pending, is domain.ErrNotFound.
	Revoke(ctx context.Context, orgID, projectID, id uuid.UUID, at time.Time, entry domain.AuditEntry) error
	// ByTokenHash resolves a link without knowing its organization, which is the
	// situation of the person following it.
	ByTokenHash(ctx context.Context, tokenHash string) (Described, bool, error)
	// UserByEmail finds a person of the organization by address.
	UserByEmail(ctx context.Context, orgID uuid.UUID, email string) (uuid.UUID, bool, error)
	// Accept makes the invitee a member, marks the invitation used and writes
	// the entry, in one transaction of the inviting organization. It finds the
	// invitee's user there or creates it, fills the entry's author and target
	// with that user, and returns who they are in that organization. A second
	// use of the same invitation is domain.ErrInvitationUnusable.
	Accept(ctx context.Context, accept Acceptance) (domain.Identity, error)
}

// Described is what a link points to, with what the person following it needs
// to read before agreeing.
type Described struct {
	domain.Invitation
	OrgName     string
	ProjectName string
}

// Acceptance is everything Accept needs to write.
type Acceptance struct {
	Invitation domain.Invitation
	// Invitee is the person following the link, as their own organization
	// knows them.
	Invitee Profile
	// NewUserID is the identifier a user created for them will have.
	NewUserID uuid.UUID
	At        time.Time
	Entry     domain.AuditEntry
}

// Profile is a signed-in person as the invitation needs them.
type Profile struct {
	Subject string
	Email   string
	Name    string
}

// People reads who the caller is.
type People interface {
	Profile(ctx context.Context, who domain.Identity) (Profile, error)
}

// Memberships adds an existing user of the organization to a project directly.
type Memberships interface {
	SetRole(ctx context.Context, orgID, projectID, userID uuid.UUID, role domain.Role, entry domain.AuditEntry) error
}

// Authorizer answers what the caller may do. Every question names a project.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
	MembershipOf(ctx context.Context, who domain.Identity, projectID uuid.UUID) (domain.Membership, bool, error)
}

// AuditLog records what was refused.
type AuditLog interface {
	Record(ctx context.Context, entry domain.AuditEntry) error
}

// Clock exists so that expiry can be controlled in a test.
type Clock interface {
	Now() time.Time
}

// Tokens makes the secret a link carries. An interface so that a test can know
// the value; the real one reads crypto/rand.
type Tokens interface {
	New() (string, error)
}
