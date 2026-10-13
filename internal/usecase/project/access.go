package project

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Access is what the caller holds in a project: their role, and the
// permissions they actually have, which a custom mask on the membership can
// make different from the role's.
type Access struct {
	Role        domain.Role
	Permissions domain.PermissionSet
	// Custom is true when a mask stored on the membership replaces the role's.
	Custom bool
}

// Access reads the caller's own access to a project, from the database, at
// the moment of asking. It is what the front end hides buttons by; the API
// still decides every request on its own.
//
// It asks no permission: it only ever describes the caller to themselves. A
// caller who is not a member is told the project is not found, as everywhere.
func (s *Service) Access(ctx context.Context, who domain.Identity, projectID uuid.UUID) (Access, error) {
	membership, found, err := s.authz.MembershipOf(ctx, who, projectID)
	if err != nil {
		return Access{}, fmt.Errorf("read the membership: %w", err)
	}
	if !found {
		return Access{}, domain.ErrNotFound
	}

	// A role this build does not know grants nothing: Permissions says so
	// with its second value, and the empty set is the answer.
	permissions, _ := membership.Permissions()

	return Access{
		Role:        membership.Role,
		Permissions: permissions,
		Custom:      membership.Override != nil,
	}, nil
}
