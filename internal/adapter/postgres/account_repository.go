package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
)

// AccountRepository finds and creates the people who sign in.
type AccountRepository struct {
	db *DB
}

// NewAccountRepository returns a repository over db.
func NewAccountRepository(db *DB) *AccountRepository {
	return &AccountRepository{db: db}
}

// Lookup resolves a subject without an organization, through auth_lookup.
func (r *AccountRepository) Lookup(ctx context.Context, subject string) ([]domain.Identity, error) {
	matches, err := r.db.LookupSubject(ctx, subject)
	if err != nil {
		return nil, err
	}

	identities := make([]domain.Identity, 0, len(matches))
	for _, m := range matches {
		identities = append(identities, domain.Identity{UserID: m.UserID, OrgID: m.OrgID})
	}
	return identities, nil
}

// Create stores a new organization, its first user, project and membership in
// one transaction scoped to that organization: either all of it exists
// afterwards, or none of it does.
func (r *AccountRepository) Create(ctx context.Context, a auth.NewAccount) error {
	return r.db.InTenantTx(ctx, a.OrgID, func(tx pgx.Tx) error {
		q := sqlc.New(tx)

		if err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
			ID: a.OrgID, Name: a.OrgName, CreatedAt: a.CreatedAt,
		}); err != nil {
			return fmt.Errorf("create the organization: %w", err)
		}

		if err := q.CreateUser(ctx, sqlc.CreateUserParams{
			ID: a.UserID, OrgID: a.OrgID, Subject: a.Subject, Email: a.Email, Name: a.Name, CreatedAt: a.CreatedAt,
		}); err != nil {
			return fmt.Errorf("create the user: %w", err)
		}

		if err := q.CreateProject(ctx, sqlc.CreateProjectParams{
			ID: a.ProjectID, OrgID: a.OrgID, Name: a.ProjectName, Slug: a.ProjectSlug, CreatedAt: a.CreatedAt,
		}); err != nil {
			return fmt.Errorf("create the first project: %w", err)
		}

		if err := q.CreateMembership(ctx, sqlc.CreateMembershipParams{
			OrgID: a.OrgID, UserID: a.UserID, ProjectID: a.ProjectID, Role: string(a.Role), CreatedAt: a.CreatedAt,
		}); err != nil {
			return fmt.Errorf("create the membership: %w", err)
		}

		return nil
	})
}

// Me reads the signed-in user and their organization.
func (r *AccountRepository) Me(ctx context.Context, who domain.Identity) (auth.Me, error) {
	var me auth.Me

	err := r.db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).GetMe(ctx, sqlc.GetMeParams{OrgID: who.OrgID, ID: who.UserID})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read the signed-in user: %w", err)
		}

		me = auth.Me{UserID: row.UserID, Name: row.Name, Email: row.Email, OrgID: row.OrgID, OrgName: row.OrgName}
		return nil
	})

	return me, err
}
