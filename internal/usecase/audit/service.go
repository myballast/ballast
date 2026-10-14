package audit

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Service reads the journal of a project.
type Service struct {
	journal Journal
	authz   Authorizer
	log     *slog.Logger
}

// NewService wires the service.
func NewService(journal Journal, authz Authorizer, log *slog.Logger) *Service {
	return &Service{journal: journal, authz: authz, log: log}
}

// InProject returns a project's journal, newest first.
//
// Reserved to an administrator of that project. The journal holds who ran what
// against which host, who was refused, and who changed somebody's rights: it is
// the record people are held to, and reading it is a different thing from
// taking part in the project.
//
// The reading includes the entries that belong to the organization rather than
// to a project — an emergency stop is the one that matters — because an action
// on the organization concerns every project in it.
func (s *Service) InProject(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, limit int,
) ([]domain.AuditEntry, error) {
	// PermManageProject rather than a permission of its own: no mask but an
	// administrator's holds it, which is what "reserved to admin" means in a
	// product where a role is held within a project.
	allowed, err := s.authz.Can(ctx, who, domain.PermManageProject, projectID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		s.log.InfoContext(ctx, "journal hidden for want of permission",
			"org_id", who.OrgID, "user_id", who.UserID, "project_id", projectID)
		return nil, domain.ErrForbidden
	}

	return s.journal.InProject(ctx, who.OrgID, projectID, limit)
}
