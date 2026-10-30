package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/usecase/runner"
)

// Compile-time proof that this repository satisfies the port the gate declared.
var _ runner.Policy = (*RunnerPolicyRepository)(nil)

// RunnerPolicyRepository reads the minimum an organization holds its workers to.
type RunnerPolicyRepository struct {
	db *DB
}

// NewRunnerPolicyRepository returns a repository over db.
func NewRunnerPolicyRepository(db *DB) *RunnerPolicyRepository {
	return &RunnerPolicyRepository{db: db}
}

// MinimumContract is the oldest contract the organization hands a run to.
func (r *RunnerPolicyRepository) MinimumContract(ctx context.Context, orgID uuid.UUID) (int, error) {
	var minimum int

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		value, err := sqlc.New(tx).GetMinimumRunnerContract(ctx, orgID)
		if err != nil {
			return fmt.Errorf("read the minimum contract: %w", err)
		}
		minimum = int(value)
		return nil
	})
	if err != nil {
		return 0, err
	}

	return minimum, nil
}
