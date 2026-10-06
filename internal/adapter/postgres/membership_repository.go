package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/project"
)

// Compile-time proof that this repository satisfies both ports that read
// memberships. They are two interfaces because they answer two questions: what
// may this person do here, and who is here.
var (
	_ authz.Memberships   = (*MembershipRepository)(nil)
	_ project.Memberships = (*MembershipRepository)(nil)
)

// MembershipRepository persists who belongs to which project, with what role.
type MembershipRepository struct {
	db *DB
}

// NewMembershipRepository returns a repository over db.
func NewMembershipRepository(db *DB) *MembershipRepository {
	return &MembershipRepository{db: db}
}

// OfMember reads one membership, and reports false when there is none.
func (r *MembershipRepository) OfMember(
	ctx context.Context, who domain.Identity, projectID uuid.UUID,
) (domain.Membership, bool, error) {
	var (
		membership domain.Membership
		found      bool
	)

	err := r.db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).GetMembership(ctx, sqlc.GetMembershipParams{
			OrgID:     who.OrgID,
			UserID:    who.UserID,
			ProjectID: projectID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the membership: %w", err)
		}

		membership, found = toDomainMembership(row), true
		return nil
	})

	return membership, found, err
}

// AllOfMember reads every membership a person holds.
func (r *MembershipRepository) AllOfMember(
	ctx context.Context, who domain.Identity,
) ([]domain.Membership, error) {
	var memberships []domain.Membership

	err := r.db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListMembershipsOfMember(ctx, sqlc.ListMembershipsOfMemberParams{
			OrgID:  who.OrgID,
			UserID: who.UserID,
		})
		if err != nil {
			return fmt.Errorf("list the memberships: %w", err)
		}

		memberships = make([]domain.Membership, 0, len(rows))
		for _, row := range rows {
			memberships = append(memberships, toDomainMembership(row))
		}
		return nil
	})

	return memberships, err
}

// InProject lists everyone in a project, with the address they signed in with.
func (r *MembershipRepository) InProject(
	ctx context.Context, orgID, projectID uuid.UUID,
) ([]project.Member, error) {
	var members []project.Member

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListProjectMembers(ctx, sqlc.ListProjectMembersParams{
			OrgID:     orgID,
			ProjectID: projectID,
		})
		if err != nil {
			return fmt.Errorf("list the members: %w", err)
		}

		members = make([]project.Member, 0, len(rows))
		for _, row := range rows {
			members = append(members, project.Member{
				Membership: domain.Membership{
					OrgID:     row.OrgID,
					UserID:    row.UserID,
					ProjectID: row.ProjectID,
					Role:      domain.Role(row.Role),
					Override:  row.Permissions,
					CreatedAt: row.CreatedAt,
				},
				Email: row.Email,
				Name:  row.DisplayName,
			})
		}
		return nil
	})

	return members, err
}

// SetRole grants or changes somebody's role in a project.
func (r *MembershipRepository) SetRole(
	ctx context.Context, orgID, projectID, userID uuid.UUID, role domain.Role,
) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		// A membership names a person who must exist in this organization. The
		// foreign key says the same thing, but its error names a constraint
		// rather than the mistake.
		if _, err := queries.GetUser(ctx, sqlc.GetUserParams{OrgID: orgID, ID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("read the user: %w", err)
		}

		if err := queries.UpsertMembership(ctx, sqlc.UpsertMembershipParams{
			OrgID:     orgID,
			UserID:    userID,
			ProjectID: projectID,
			Role:      string(role),
		}); err != nil {
			return fmt.Errorf("set the role: %w", err)
		}
		return nil
	})
}

// Remove takes somebody out of a project.
func (r *MembershipRepository) Remove(
	ctx context.Context, orgID, projectID, userID uuid.UUID,
) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		affected, err := sqlc.New(tx).DeleteMembership(ctx, sqlc.DeleteMembershipParams{
			OrgID:     orgID,
			UserID:    userID,
			ProjectID: projectID,
		})
		if err != nil {
			return fmt.Errorf("remove the member: %w", err)
		}
		if affected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}
