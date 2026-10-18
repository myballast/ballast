package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/invitation"
)

// Compile-time proof that this repository satisfies the port the invitation
// service declared.
var _ invitation.Repository = (*InvitationRepository)(nil)

// InvitationRepository persists invitations.
type InvitationRepository struct {
	db *DB
}

// NewInvitationRepository returns a repository over db.
func NewInvitationRepository(db *DB) *InvitationRepository {
	return &InvitationRepository{db: db}
}

// Create writes the invitation and its entry in one transaction.
func (r *InvitationRepository) Create(
	ctx context.Context, in domain.Invitation, tokenHash string, entry domain.AuditEntry,
) error {
	return r.db.InTenantTx(ctx, in.OrgID, func(tx pgx.Tx) error {
		q := sqlc.New(tx)

		if err := q.RevokeExpiredInvitations(ctx, sqlc.RevokeExpiredInvitationsParams{
			OrgID: in.OrgID, ProjectID: in.ProjectID, Email: in.Email, RevokedAt: &in.CreatedAt,
		}); err != nil {
			return fmt.Errorf("close the expired invitations: %w", err)
		}

		if err := q.InsertInvitation(ctx, sqlc.InsertInvitationParams{
			ID:        in.ID,
			OrgID:     in.OrgID,
			ProjectID: in.ProjectID,
			Email:     in.Email,
			Role:      string(in.Role),
			TokenHash: tokenHash,
			InvitedBy: in.InvitedBy,
			ExpiresAt: in.ExpiresAt,
			CreatedAt: in.CreatedAt,
		}); err != nil {
			// The unique index on pending invitations: the address was already
			// invited and has not answered.
			if translate(err) == domain.ErrAlreadyExists {
				return domain.ErrAlreadyExists
			}
			return fmt.Errorf("write the invitation: %w", err)
		}

		return insertAudit(ctx, tx, entry)
	})
}

// Pending lists the invitations of a project that are neither used nor revoked.
func (r *InvitationRepository) Pending(
	ctx context.Context, orgID, projectID uuid.UUID,
) ([]domain.Invitation, error) {
	var found []domain.Invitation

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListPendingInvitations(ctx, sqlc.ListPendingInvitationsParams{
			OrgID: orgID, ProjectID: projectID,
		})
		if err != nil {
			return fmt.Errorf("list the invitations: %w", err)
		}

		found = make([]domain.Invitation, 0, len(rows))
		for _, row := range rows {
			found = append(found, domain.Invitation{
				ID:         row.ID,
				OrgID:      row.OrgID,
				ProjectID:  row.ProjectID,
				Email:      row.Email,
				Role:       domain.Role(row.Role),
				InvitedBy:  row.InvitedBy,
				ExpiresAt:  row.ExpiresAt,
				AcceptedAt: row.AcceptedAt,
				RevokedAt:  row.RevokedAt,
				CreatedAt:  row.CreatedAt,
			})
		}
		return nil
	})

	return found, err
}

// Revoke withdraws a pending invitation.
func (r *InvitationRepository) Revoke(
	ctx context.Context, orgID, projectID, id uuid.UUID, at time.Time, entry domain.AuditEntry,
) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		affected, err := sqlc.New(tx).RevokeInvitation(ctx, sqlc.RevokeInvitationParams{
			OrgID: orgID, ProjectID: projectID, ID: id, RevokedAt: &at,
		})
		if err != nil {
			return fmt.Errorf("revoke the invitation: %w", err)
		}
		if affected == 0 {
			return domain.ErrNotFound
		}

		return insertAudit(ctx, tx, entry)
	})
}

// lookupInvitation is the other statement that runs outside InTenantTx, for the
// reason LookupSubject does: the person following a link does not know the
// organization that wrote it. It calls invitation_lookup, which migration 013
// grants for this and answers with what a page needs to describe the
// invitation, by exact match on the hash.
const lookupInvitation = `
	SELECT invitation_id, org_id, project_id, email, role, expires_at,
	       accepted_at, revoked_at, project_name, org_name
	FROM invitation_lookup($1)`

// ByTokenHash resolves a link to its invitation.
func (r *InvitationRepository) ByTokenHash(
	ctx context.Context, tokenHash string,
) (invitation.Described, bool, error) {
	var (
		described invitation.Described
		role      string
	)

	err := r.db.pool.QueryRow(ctx, lookupInvitation, tokenHash).Scan(
		&described.ID, &described.OrgID, &described.ProjectID, &described.Email, &role,
		&described.ExpiresAt, &described.AcceptedAt, &described.RevokedAt,
		&described.ProjectName, &described.OrgName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return invitation.Described{}, false, nil
	}
	if err != nil {
		return invitation.Described{}, false, fmt.Errorf("look the invitation up: %w", err)
	}

	described.Role = domain.Role(role)
	return described, true, nil
}

// UserByEmail finds a person of the organization by address.
func (r *InvitationRepository) UserByEmail(
	ctx context.Context, orgID uuid.UUID, email string,
) (uuid.UUID, bool, error) {
	var (
		id    uuid.UUID
		found bool
	)

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		// Stored as the provider sent it, so the comparison is on lower case.
		row, err := sqlc.New(tx).FindUserByEmail(ctx, sqlc.FindUserByEmailParams{OrgID: orgID, Email: email})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the user: %w", err)
		}

		id, found = row, true
		return nil
	})

	return id, found, err
}

// Accept makes the invitee a member of the inviting organization's project.
func (r *InvitationRepository) Accept(ctx context.Context, a invitation.Acceptance) (domain.Identity, error) {
	var joined domain.Identity

	err := r.db.InTenantTx(ctx, a.Invitation.OrgID, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		orgID := a.Invitation.OrgID

		// First, so that whoever loses a race for the link stops before creating
		// anything.
		used, err := q.UseInvitation(ctx, sqlc.UseInvitationParams{
			OrgID: orgID, ID: a.Invitation.ID, AcceptedAt: &a.At,
		})
		if err != nil {
			return fmt.Errorf("use the invitation: %w", err)
		}
		if used == 0 {
			return domain.ErrInvitationUnusable
		}

		userID, err := q.FindUserBySubject(ctx, sqlc.FindUserBySubjectParams{OrgID: orgID, Subject: a.Invitee.Subject})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			userID = a.NewUserID
			if err := q.CreateUser(ctx, sqlc.CreateUserParams{
				ID: userID, OrgID: orgID, Subject: a.Invitee.Subject,
				Email: a.Invitee.Email, Name: a.Invitee.Name, CreatedAt: a.At,
			}); err != nil {
				// Another person of the organization already holds this address.
				if translate(err) == domain.ErrAlreadyExists {
					return domain.ErrAlreadyExists
				}
				return fmt.Errorf("create the user: %w", err)
			}
		case err != nil:
			return fmt.Errorf("read the user: %w", err)
		}

		if err := q.AddMembershipIfAbsent(ctx, sqlc.AddMembershipIfAbsentParams{
			OrgID: orgID, UserID: userID, ProjectID: a.Invitation.ProjectID, Role: string(a.Invitation.Role),
		}); err != nil {
			return fmt.Errorf("add the membership: %w", err)
		}

		// The author is the person who joined, which only this transaction knows.
		entry := a.Entry
		entry.ActorID = &userID
		if err := insertAudit(ctx, tx, entry.On(userID)); err != nil {
			return err
		}

		joined = domain.Identity{UserID: userID, OrgID: orgID}
		return nil
	})

	return joined, err
}
