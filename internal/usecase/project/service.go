package project

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Service orchestrates projects and their members.
type Service struct {
	projects    Repository
	memberships Memberships
	authz       Authorizer
	journal     AuditLog
	clock       Clock
	log         *slog.Logger
}

// NewService wires the service.
func NewService(
	projects Repository,
	memberships Memberships,
	authz Authorizer,
	journal AuditLog,
	clock Clock,
	log *slog.Logger,
) *Service {
	return &Service{
		projects:    projects,
		memberships: memberships,
		authz:       authz,
		journal:     journal,
		clock:       clock,
		log:         log,
	}
}

// List returns the projects the caller belongs to.
//
// No permission is checked, because there is nothing to refuse: a project the
// caller is not a member of is not theirs to see, and the query says so by
// joining on their memberships. Row-level security keeps the organization
// boundary.
func (s *Service) List(ctx context.Context, who domain.Identity) ([]domain.Project, error) {
	return s.projects.OfMember(ctx, who)
}

// Create makes a project and its creator its administrator.
//
// The caller must already administer a project of this organization. The
// permission model holds no organization-wide role — every mask is granted
// within a project — so this is the nearest thing that stays scoped, and it
// keeps somebody invited as a viewer from spinning up projects of their own.
func (s *Service) Create(
	ctx context.Context, who domain.Identity, name, slug string,
) (domain.Project, error) {
	if err := domain.ValidateProject(name, slug); err != nil {
		return domain.Project{}, err
	}

	allowed, err := s.authz.CanInAnyProject(ctx, who, domain.PermManageProject)
	if err != nil {
		return domain.Project{}, err
	}
	if !allowed {
		return domain.Project{}, domain.ErrForbidden
	}

	created, err := s.projects.Create(ctx, domain.Project{
		ID:    uuid.New(),
		OrgID: who.OrgID,
		Name:  name,
		Slug:  slug,
	}, who, s.clock.Now())
	if err != nil {
		return domain.Project{}, err
	}

	s.log.InfoContext(ctx, "project created",
		"org_id", who.OrgID, "project_id", created.ID, "slug", created.Slug, "by", who.UserID)

	return created, nil
}

// ByID returns one project.
func (s *Service) ByID(
	ctx context.Context, who domain.Identity, id uuid.UUID,
) (domain.Project, error) {
	if err := s.require(ctx, who, domain.PermViewProject, id); err != nil {
		return domain.Project{}, err
	}

	return s.projects.ByID(ctx, who.OrgID, id)
}

// Rename changes a project's display name. The slug stays: it appears in URLs
// and in object storage keys, where moving it would orphan what is filed under
// it.
func (s *Service) Rename(
	ctx context.Context, who domain.Identity, id uuid.UUID, name string,
) (domain.Project, error) {
	if err := s.require(ctx, who, domain.PermManageProject, id); err != nil {
		return domain.Project{}, err
	}

	existing, err := s.projects.ByID(ctx, who.OrgID, id)
	if err != nil {
		return domain.Project{}, err
	}
	if err := domain.ValidateProject(name, existing.Slug); err != nil {
		return domain.Project{}, err
	}

	return s.projects.Rename(ctx, who.OrgID, id, name)
}

// Delete removes a project that holds no run. The repository refuses one that
// does, in the transaction that would have deleted it.
func (s *Service) Delete(ctx context.Context, who domain.Identity, id uuid.UUID) error {
	if err := s.require(ctx, who, domain.PermManageProject, id); err != nil {
		return err
	}

	if err := s.projects.Delete(ctx, who.OrgID, id); err != nil {
		return err
	}

	s.log.InfoContext(ctx, "project deleted", "org_id", who.OrgID, "project_id", id, "by", who.UserID)
	return nil
}

// Members lists who belongs to a project.
func (s *Service) Members(
	ctx context.Context, who domain.Identity, projectID uuid.UUID,
) ([]Member, error) {
	if err := s.require(ctx, who, domain.PermViewProject, projectID); err != nil {
		return nil, err
	}

	return s.memberships.InProject(ctx, who.OrgID, projectID)
}

// SetMemberRole grants or changes somebody's role.
//
// It refuses to take away the project's last administrator, including when the
// caller is demoting themselves. A project nobody can administer cannot be
// repaired through this API at all.
func (s *Service) SetMemberRole(
	ctx context.Context, who domain.Identity, projectID, userID uuid.UUID, role domain.Role,
) error {
	if err := s.require(ctx, who, domain.PermManageMembers, projectID); err != nil {
		return err
	}
	if !role.IsValid() {
		return fmt.Errorf("%w: %q is not a role", domain.ErrInvalidProject, role)
	}
	if err := s.refuseAboveOwnRank(ctx, who, projectID, userID, role); err != nil {
		return err
	}

	if role != domain.RoleAdmin {
		if err := s.refuseLosingLastAdmin(ctx, who, projectID, userID); err != nil {
			return err
		}
	}

	entry := domain.Allow(
		who.OrgID, &who.UserID, domain.ActionRoleChanged, domain.TargetMembership, s.clock.Now(),
	).InProject(projectID).On(userID).With("role", string(role))

	if err := s.memberships.SetRole(ctx, who.OrgID, projectID, userID, role, entry); err != nil {
		return err
	}

	s.log.InfoContext(ctx, "member role set",
		"org_id", who.OrgID, "project_id", projectID, "user_id", userID, "role", role, "by", who.UserID)

	return nil
}

// RemoveMember takes somebody out of a project, unless they are its last
// administrator.
func (s *Service) RemoveMember(
	ctx context.Context, who domain.Identity, projectID, userID uuid.UUID,
) error {
	if err := s.require(ctx, who, domain.PermManageMembers, projectID); err != nil {
		return err
	}
	if err := s.refuseLosingLastAdmin(ctx, who, projectID, userID); err != nil {
		return err
	}

	entry := domain.Allow(
		who.OrgID, &who.UserID, domain.ActionMemberRemoved, domain.TargetMembership, s.clock.Now(),
	).InProject(projectID).On(userID)

	if err := s.memberships.Remove(ctx, who.OrgID, projectID, userID, entry); err != nil {
		return err
	}

	s.log.InfoContext(ctx, "member removed",
		"org_id", who.OrgID, "project_id", projectID, "user_id", userID, "by", who.UserID)

	return nil
}

// refuseAboveOwnRank stops a caller from granting more than they hold, and from
// changing the role of somebody who holds more than they do.
//
// Only administrators manage members today, and nobody outranks an
// administrator, so the rule cannot fire through a role alone. It is written
// anyway, because "who may manage members" and "up to which role" are two
// questions, and the day a lower role is given the first permission the second
// must already be answered. A mask stored by hand on a membership can do it
// now.
func (s *Service) refuseAboveOwnRank(
	ctx context.Context, who domain.Identity, projectID, userID uuid.UUID, role domain.Role,
) error {
	own, found, err := s.authz.MembershipOf(ctx, who, projectID)
	if err != nil {
		return fmt.Errorf("read the caller's membership: %w", err)
	}
	if !found {
		return domain.ErrForbidden
	}

	allowed := domain.CanGrant(own.Role, role)
	if allowed {
		members, err := s.memberships.InProject(ctx, who.OrgID, projectID)
		if err != nil {
			return err
		}
		for _, member := range members {
			if member.UserID == userID && member.Role.Rank() > own.Role.Rank() {
				allowed = false
			}
		}
	}
	if allowed {
		return nil
	}

	s.refused(ctx, domain.Refuse(
		who.OrgID, &who.UserID, domain.ActionRoleChanged, domain.TargetMembership, "role_above_own", s.clock.Now(),
	).InProject(projectID).On(userID))
	return domain.ErrRoleAboveOwn
}

// refused writes the journal entry of a refusal. A failure to write it is
// logged and does not change the answer: the caller was refused either way.
func (s *Service) refused(ctx context.Context, entry domain.AuditEntry) {
	if err := s.journal.Record(ctx, entry); err != nil {
		s.log.ErrorContext(ctx, "record a refusal", "action", entry.Action, "error", err)
	}
}

// refuseLosingLastAdmin stops a change that would leave the project with no
// administrator.
//
// The read and the write that follows are not one transaction: two
// administrators stepping down at the same instant could both be told to go
// ahead. The window is narrow and the consequence is a project to repair rather
// than data lost, so it is left open and written down rather than paid for with
// a lock on every role change.
func (s *Service) refuseLosingLastAdmin(
	ctx context.Context, who domain.Identity, projectID, userID uuid.UUID,
) error {
	members, err := s.memberships.InProject(ctx, who.OrgID, projectID)
	if err != nil {
		return err
	}

	admins, isAdmin := 0, false
	for _, member := range members {
		mask, known := member.Permissions()
		if !known || !mask.Has(domain.PermManageMembers) {
			continue
		}

		admins++
		if member.UserID == userID {
			isAdmin = true
		}
	}

	if isAdmin && admins == 1 {
		return domain.ErrLastAdministrator
	}
	return nil
}

// require refuses the caller unless they hold perm in projectID.
//
// A refusal and a project that does not exist both come back as the same
// forbidden: distinguishing them would tell somebody with no business there
// that the project exists.
func (s *Service) require(
	ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID,
) error {
	allowed, err := s.authz.Can(ctx, who, perm, projectID)
	if err != nil {
		return err
	}
	if !allowed {
		s.log.InfoContext(ctx, "request refused",
			"org_id", who.OrgID, "user_id", who.UserID, "project_id", projectID, "permission", int64(perm))
		return domain.ErrForbidden
	}
	return nil
}
