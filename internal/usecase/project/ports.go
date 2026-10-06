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
	SetRole(ctx context.Context, orgID, projectID, userID uuid.UUID, role domain.Role) error
	Remove(ctx context.Context, orgID, projectID, userID uuid.UUID) error
}

// Member is a membership plus enough of the person to show them in a list.
type Member struct {
	domain.Membership
	Email string
	Name  string
}

// Authorizer answers what the caller may do. Every question it is asked names a
// project: a permission check without a scope is a bug, not a refusal.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
	// CanInAnyProject covers the one action that has no project yet: creating
	// one. It still asks the question per project, so the rule remains "this
	// person already administers something here".
	CanInAnyProject(ctx context.Context, who domain.Identity, perm domain.Permission) (bool, error)
}

// Clock exists so that what a project records about when it was created can be
// controlled in a test.
type Clock interface {
	Now() time.Time
}
