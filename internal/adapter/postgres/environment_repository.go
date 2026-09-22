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

		found = domain.Environment{
			ID:                 row.ID,
			ProjectID:          row.ProjectID,
			Name:               row.Name,
			BaseURL:            row.BaseUrl,
			VerificationStatus: domain.VerificationStatus(row.VerificationStatus),
			MaxVUs:             int(row.MaxVus),
			MaxDurationS:       int(row.MaxDurationS),
		}
		return nil
	})

	return found, err
}
