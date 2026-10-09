package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// EnvironmentRepository reads target environments.
type EnvironmentRepository struct {
	db *DB
}

// NewEnvironmentRepository returns a repository over db.
func NewEnvironmentRepository(db *DB) *EnvironmentRepository {
	return &EnvironmentRepository{db: db}
}

// EnvironmentByID reads one environment, including whether its ownership was
// ever proved and approved.
func (r *EnvironmentRepository) EnvironmentByID(
	ctx context.Context, orgID, id uuid.UUID,
) (domain.Environment, error) {
	var found domain.Environment

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).GetEnvironment(ctx, sqlc.GetEnvironmentParams{OrgID: orgID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read the environment: %w", err)
		}

		// The same mapping the write side uses. Two of them would drift, and the
		// first thing to go missing would be the allowed window — read here, on
		// the path that decides whether a run may start at all.
		found = toDomainEnvironment(row)
		return nil
	})

	return found, err
}
