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
	_ apitoken.Registry   = (*APITokenRepository)(nil)
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

// Create writes the token and its journal entry in one transaction: if the entry
// cannot be written, the token does not exist.
//
// The ceiling is checked in that same transaction, behind a lock on the project's
// row. A count taken outside it would let two creations at once both find room;
// with the lock, the second waits for the first and then counts it.
func (r *APITokenRepository) Create(
	ctx context.Context, token domain.APIToken, tokenHash string, entry domain.AuditEntry,
) error {
	return r.db.InTenantTx(ctx, token.OrgID, func(tx pgx.Tx) error {
		q := sqlc.New(tx)

		// A project of another organization is not there to be locked, which is
		// the same answer as a project that does not exist.
		_, err := q.LockProjectForApiTokens(ctx, sqlc.LockProjectForApiTokensParams{
			OrgID: token.OrgID, ID: token.ProjectID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock the project: %w", err)
		}

		live, err := q.CountLiveApiTokens(ctx, sqlc.CountLiveApiTokensParams{
			OrgID: token.OrgID, ProjectID: token.ProjectID,
		})
		if err != nil {
			return fmt.Errorf("count the live tokens: %w", err)
		}
		if live >= domain.MaxAPITokensPerProject {
			return domain.ErrTooManyAPITokens
		}

		if err := q.InsertApiToken(ctx, sqlc.InsertApiTokenParams{
			ID:        token.ID,
			OrgID:     token.OrgID,
			ProjectID: token.ProjectID,
			Name:      token.Name,
			Prefix:    token.Prefix,
			TokenHash: tokenHash,
			CreatedBy: token.CreatedBy,
			ExpiresAt: token.ExpiresAt,
			CreatedAt: token.CreatedAt,
		}); err != nil {
			return fmt.Errorf("write the API token: %w", err)
		}

		return insertAudit(ctx, tx, entry)
	})
}

// List returns the tokens of a project, revoked ones included, newest first. A
// project of another organization has none here: row-level security hides them,
// and nobody learns from an error that it exists.
func (r *APITokenRepository) List(
	ctx context.Context, orgID, projectID uuid.UUID,
) ([]apitoken.Listed, error) {
	listed := []apitoken.Listed{}

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListApiTokens(ctx, sqlc.ListApiTokensParams{OrgID: orgID, ProjectID: projectID})
		if err != nil {
			return fmt.Errorf("list the API tokens: %w", err)
		}

		for _, row := range rows {
			listed = append(listed, apitoken.Listed{
				APIToken: domain.APIToken{
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
				},
				CreatedByName: row.CreatedByName,
			})
		}
		return nil
	})

	return listed, err
}

// Revoke writes the date of the revocation and its journal entry in one
// transaction. A token that was already revoked, or that is not in this
// organization and project, matches no row: that is a not found, and no entry is
// written for it — the journal says a token was revoked once because it was.
func (r *APITokenRepository) Revoke(
	ctx context.Context, token domain.APIToken, at time.Time, entry domain.AuditEntry,
) error {
	return r.db.InTenantTx(ctx, token.OrgID, func(tx pgx.Tx) error {
		affected, err := sqlc.New(tx).RevokeApiToken(ctx, sqlc.RevokeApiTokenParams{
			OrgID: token.OrgID, ProjectID: token.ProjectID, ID: token.ID, RevokedAt: &at,
		})
		if err != nil {
			return fmt.Errorf("revoke the API token: %w", err)
		}
		if affected == 0 {
			return domain.ErrNotFound
		}

		return insertAudit(ctx, tx, entry)
	})
}
