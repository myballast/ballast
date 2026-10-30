package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/runner"
)

// Compile-time proof that this repository satisfies the port the runner service
// declared.
var _ runner.Repository = (*RunnerRepository)(nil)

// RunnerRepository persists what the workers say about themselves.
type RunnerRepository struct {
	db *DB
}

// NewRunnerRepository returns a repository over db.
func NewRunnerRepository(db *DB) *RunnerRepository {
	return &RunnerRepository{db: db}
}

// Report writes what a runner says about itself, in place of what it said
// before under the same name.
func (r *RunnerRepository) Report(ctx context.Context, orgID uuid.UUID, runner domain.Runner) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		err := sqlc.New(tx).ReportRunner(ctx, sqlc.ReportRunnerParams{
			OrgID:       orgID,
			Name:        runner.Name,
			Version:     runner.Version,
			Contract:    int32(runner.Contract), //nolint:gosec // bounded by Runner.Validate
			State:       string(runner.State),
			StartedAt:   runner.StartedAt,
			HeartbeatAt: runner.HeartbeatAt,
		})
		if err != nil {
			return fmt.Errorf("write the runner: %w", err)
		}
		return nil
	})
}

// List returns the runners of an organization, by name.
func (r *RunnerRepository) List(ctx context.Context, orgID uuid.UUID) ([]domain.Runner, error) {
	var found []domain.Runner

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListRunners(ctx, orgID)
		if err != nil {
			return fmt.Errorf("list the runners: %w", err)
		}

		found = make([]domain.Runner, 0, len(rows))
		for _, row := range rows {
			found = append(found, domain.Runner{
				ID:          row.ID,
				OrgID:       row.OrgID,
				Name:        row.Name,
				Version:     row.Version,
				Contract:    int(row.Contract),
				State:       domain.RunnerState(row.State),
				StartedAt:   row.StartedAt,
				HeartbeatAt: row.HeartbeatAt,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return found, nil
}
