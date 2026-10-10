package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
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

// Compile-time proof that this repository also answers what a run may reach.
var _ run.AllowlistReader = (*EnvironmentRepository)(nil)

// AllowedHosts reads the hosts a project's generators may reach.
//
// On the same repository as the target itself, and read-only like it: this is
// the worker's side of the verification, and it has no business writing either.
func (r *EnvironmentRepository) AllowedHosts(
	ctx context.Context, orgID, projectID uuid.UUID,
) ([]string, error) {
	var hosts []string

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		found, err := sqlc.New(tx).ListProjectAllowedHosts(ctx,
			sqlc.ListProjectAllowedHostsParams{OrgID: orgID, ProjectID: projectID})
		if err != nil {
			return fmt.Errorf("read the project's allowed hosts: %w", err)
		}

		hosts = found
		return nil
	})

	return hosts, err
}
