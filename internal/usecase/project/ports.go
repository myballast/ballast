package project

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Repository stores projects. Declared here, by the code that consumes it, and
// implemented in adapter/postgres.
type Repository interface {
	// Create writes the project and the membership that makes owner its
	// administrator, together. A project nobody can administer has to be
	// repaired by hand.
	Create(ctx context.Context, newProject domain.Project, owner domain.Identity, at time.Time) (domain.Project, error)
	ByID(ctx context.Context, orgID, id uuid.UUID) (domain.Project, error)
	OfMember(ctx context.Context, who domain.Identity) ([]domain.Project, error)
	Rename(ctx context.Context, orgID, id uuid.UUID, name string) (domain.Project, error)
	// Delete refuses a project that still holds runs, and checks that in the
	// same transaction as the delete: its foreign keys cascade, so losing that
	// race would cost a run history.
	Delete(ctx context.Context, orgID, id uuid.UUID) error
}

// Memberships stores who belongs to a project.
type Memberships interface {
	InProject(ctx context.Context, orgID, projectID uuid.UUID) ([]Member, error)
	// Both take the audit entry and write it in the transaction that performs
	// the change: a role granted with no record of who granted it is how a
	// privilege arrives without anybody having given it.
	SetRole(ctx context.Context, orgID, projectID, userID uuid.UUID, role domain.Role, entry domain.AuditEntry) error
	Remove(ctx context.Context, orgID, projectID, userID uuid.UUID, entry domain.AuditEntry) error
}

// Member is a membership plus enough of the person to show them in a list.
type Member struct {
	domain.Membership
	Email string
	Name  string
	// LastActivity is the latest entry the project's journal holds with this
	// person as its author, and nil when there is none. Read from the journal
	// rather than kept on the membership: there is no second place to write, and
	// so none to forget.
	LastActivity *time.Time
}

// Authorizer answers what the caller may do. Every question it is asked names a
// project: a permission check without a scope is a bug, not a refusal.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
	// CanInAnyProject covers the one action that has no project yet: creating
	// one. It still asks the question per project, so the rule remains "this
	// person already administers something here".
	CanInAnyProject(ctx context.Context, who domain.Identity, perm domain.Permission) (bool, error)
	// MembershipOf is the caller's own membership in a project, and false when
	// they hold none.
	MembershipOf(ctx context.Context, who domain.Identity, projectID uuid.UUID) (domain.Membership, bool, error)
}

// AuditLog records what was refused. What was granted travels with the change
// itself, in its transaction; a refusal changes nothing and has no transaction
// to travel in.
type AuditLog interface {
	Record(ctx context.Context, entry domain.AuditEntry) error
}

// Clock exists so that what a project records about when it was created can be
// controlled in a test.
type Clock interface {
	Now() time.Time
}
