package audit

import (
	"context"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Journal reads what was recorded.
//
// A reading only: nothing in this package writes an entry, and the port says
// so. An interface that could also record would invite a caller to record from
// here, far from the action it describes.
type Journal interface {
	// InProject returns one project's entries, newest first, including those
	// that belong to the organization rather than to any project.
	InProject(ctx context.Context, orgID, projectID uuid.UUID, limit int) ([]domain.AuditEntry, error)
}

// Authorizer answers what the caller may do. Every question names a project:
// a permission says what somebody may do, never what to.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
}
