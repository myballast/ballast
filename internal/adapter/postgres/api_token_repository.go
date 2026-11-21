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
	"github.com/youssefmahersi/ballast/internal/usecase/apitoken"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
)

// Compile-time proof that this repository satisfies the two ports that declared
// what they need from it.
var (
	_ apitoken.Repository = (*APITokenRepository)(nil)
	_ authz.Tokens        = (*APITokenRepository)(nil)
)

// APITokenRepository reads API tokens.
//
// Two halves of one table. ByHash is the cross-tenant half: it runs outside
// InTenantTx, because a request carrying a token knows neither the organization
// nor the project. ByID and Touch are the ordinary half, inside InTenantTx, and
// answer only for the organization they are asked under.
type APITokenRepository struct {
	db *DB
}

// NewAPITokenRepository returns a repository over db.
func NewAPITokenRepository(db *DB) *APITokenRepository {
	return &APITokenRepository{db: db}
}

// lookupByHash is the one statement here that runs outside InTenantTx, for the
// reason LookupSubject and the invitation lookup do. It calls api_token_lookup,
// which migration 021 grants for this and answers with what authentication
// needs, by exact match on the hash.
const lookupByHash = `
	SELECT token_id, org_id, project_id, expires_at, revoked_at
	FROM api_token_lookup($1)`

// ByHash resolves a hash to its token, whatever became of it.
func (r *APITokenRepository) ByHash(ctx context.Context, hash string) (apitoken.Found, bool, error) {
	var found apitoken.Found

	err := r.db.pool.QueryRow(ctx, lookupByHash, hash).Scan(
		&found.TokenID, &found.OrgID, &found.ProjectID, &found.ExpiresAt, &found.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return apitoken.Found{}, false, nil
	}
	if err != nil {
		return apitoken.Found{}, false, fmt.Errorf("look the API token up: %w", err)
	}

	return found, true, nil
}

// ByID reads a token under its organization.
func (r *APITokenRepository) ByID(
	ctx context.Context, orgID, tokenID uuid.UUID,
) (domain.APIToken, bool, error) {
	var (
		token domain.APIToken
		found bool
	)

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).ApiTokenByID(ctx, sqlc.ApiTokenByIDParams{OrgID: orgID, ID: tokenID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the API token: %w", err)
		}

		token = domain.APIToken{
			ID:         row.ID,
			OrgID:      row.OrgID,
			ProjectID:  row.ProjectID,
			Name:       row.Name,
			Prefix:     row.Prefix,
			CreatedBy:  row.CreatedBy,
			ExpiresAt:  row.ExpiresAt,
			LastUsedAt: row.LastUsedAt,
			RevokedAt:  row.RevokedAt,
			CreatedAt:  row.CreatedAt,
		}
		found = true
		return nil
	})

	return token, found, err
}

// Touch records a use, at most once a minute (the statement says so). A token of
// another organization matches no row, and that is not an error: nobody learns
// from a failure that the id exists somewhere else.
func (r *APITokenRepository) Touch(ctx context.Context, orgID, tokenID uuid.UUID, at time.Time) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		err := sqlc.New(tx).TouchApiToken(ctx, sqlc.TouchApiTokenParams{
			OrgID: orgID, ID: tokenID, LastUsedAt: &at,
		})
		if err != nil {
			return fmt.Errorf("record the use of the API token: %w", err)
		}
		return nil
	})
}
