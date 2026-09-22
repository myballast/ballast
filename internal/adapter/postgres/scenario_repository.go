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

// ScenarioRepository reads scenarios and the thresholds they impose.
type ScenarioRepository struct {
	db *DB
}

// NewScenarioRepository returns a repository over db.
func NewScenarioRepository(db *DB) *ScenarioRepository {
	return &ScenarioRepository{db: db}
}

// ScenarioByID reads one scenario.
func (r *ScenarioRepository) ScenarioByID(
	ctx context.Context, orgID, id uuid.UUID,
) (domain.Scenario, error) {
	var found domain.Scenario

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).GetScenario(ctx, sqlc.GetScenarioParams{OrgID: orgID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read the scenario: %w", err)
		}

		found = domain.Scenario{
			ID:             row.ID,
			ProjectID:      row.ProjectID,
			Name:           row.Name,
			Script:         row.Script,
			Version:        int(row.Version),
			DefaultProfile: domain.ProfileKind(row.DefaultProfile),
		}
		return nil
	})

	return found, err
}

// ScenarioThresholds reads the rules a scenario imposes on its metrics.
func (r *ScenarioRepository) ScenarioThresholds(
	ctx context.Context, orgID, scenarioID uuid.UUID,
) ([]domain.Threshold, error) {
	var thresholds []domain.Threshold

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListScenarioThresholds(ctx, sqlc.ListScenarioThresholdsParams{
			OrgID:      orgID,
			ScenarioID: scenarioID,
		})
		if err != nil {
			return fmt.Errorf("read the thresholds: %w", err)
		}

		for _, row := range rows {
			thresholds = append(thresholds, domain.Threshold{
				Metric:     row.Metric,
				Expression: row.Expression,
			})
		}
		return nil
	})

	return thresholds, err
}
