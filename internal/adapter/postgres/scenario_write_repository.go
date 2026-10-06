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
	"github.com/youssefmahersi/ballast/internal/usecase/scenario"
)

// Compile-time proof that this repository satisfies the ports the scenario
// service declared.
var (
	_ scenario.Repository = (*ScenarioWriteRepository)(nil)
	_ scenario.Thresholds = (*ScenarioWriteRepository)(nil)
)

// ScenarioWriteRepository persists scenarios, every version of their scripts, and
// the thresholds they impose.
//
// It is separate from ScenarioRepository, which the run service uses to read a
// scenario while executing one: that one answers a single question and has no
// business being able to write.
type ScenarioWriteRepository struct {
	db *DB
}

// NewScenarioWriteRepository returns a repository over db.
func NewScenarioWriteRepository(db *DB) *ScenarioWriteRepository {
	return &ScenarioWriteRepository{db: db}
}

func toDomainScenario(row sqlc.Scenario) (domain.Scenario, error) {
	params, err := unmarshalParams(row.DefaultProfileParams)
	if err != nil {
		return domain.Scenario{}, err
	}

	return domain.Scenario{
		ID:                   row.ID,
		ProjectID:            row.ProjectID,
		Name:                 row.Name,
		Script:               row.Script,
		Version:              int(row.Version),
		DefaultProfile:       domain.ProfileKind(row.DefaultProfile),
		DefaultProfileParams: params,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}, nil
}

func toDomainVersion(row sqlc.ScenarioVersion) (domain.ScenarioVersion, error) {
	params, err := unmarshalParams(row.DefaultProfileParams)
	if err != nil {
		return domain.ScenarioVersion{}, err
	}

	return domain.ScenarioVersion{
		ScenarioID:           row.ScenarioID,
		Version:              int(row.Version),
		Script:               row.Script,
		DefaultProfile:       domain.ProfileKind(row.DefaultProfile),
		DefaultProfileParams: params,
		CreatedBy:            row.CreatedBy,
		CreatedAt:            row.CreatedAt,
	}, nil
}

// Create writes the scenario and its first version together. A scenario whose
// first version is missing would be a run nobody could account for.
func (r *ScenarioWriteRepository) Create(
	ctx context.Context, newScenario domain.Scenario, by domain.Identity, at time.Time,
) (domain.Scenario, error) {
	params, err := marshalParams(newScenario.DefaultProfileParams)
	if err != nil {
		return domain.Scenario{}, err
	}

	var created domain.Scenario
	err = r.db.InTenantTx(ctx, by.OrgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		row, err := queries.InsertScenario(ctx, sqlc.InsertScenarioParams{
			ID:                   newScenario.ID,
			OrgID:                by.OrgID,
			ProjectID:            newScenario.ProjectID,
			Name:                 newScenario.Name,
			Script:               newScenario.Script,
			Version:              int32(newScenario.Version), //nolint:gosec // bounded by the schema
			DefaultProfile:       string(newScenario.DefaultProfile),
			DefaultProfileParams: params,
			CreatedAt:            at,
		})
		if err != nil {
			return fmt.Errorf("insert the scenario: %w", translate(err))
		}

		if _, err := queries.InsertScenarioVersion(ctx, sqlc.InsertScenarioVersionParams{
			OrgID:                by.OrgID,
			ScenarioID:           row.ID,
			Version:              row.Version,
			Script:               row.Script,
			DefaultProfile:       row.DefaultProfile,
			DefaultProfileParams: params,
			CreatedBy:            &by.UserID,
			CreatedAt:            at,
		}); err != nil {
			return fmt.Errorf("insert the first version: %w", err)
		}

		created, err = toDomainScenario(row)
		return err
	})

	return created, err
}

// ByID reads one scenario.
func (r *ScenarioWriteRepository) ByID(
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

		found, err = toDomainScenario(row)
		return err
	})

	return found, err
}

// InProject lists a project's scenarios.
func (r *ScenarioWriteRepository) InProject(
	ctx context.Context, orgID, projectID uuid.UUID,
) ([]domain.Scenario, error) {
	var scenarios []domain.Scenario

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListScenariosInProject(ctx, sqlc.ListScenariosInProjectParams{
			OrgID:     orgID,
			ProjectID: projectID,
		})
		if err != nil {
			return fmt.Errorf("list the scenarios: %w", err)
		}

		scenarios = make([]domain.Scenario, 0, len(rows))
		for _, row := range rows {
			found, err := toDomainScenario(row)
			if err != nil {
				return err
			}
			scenarios = append(scenarios, found)
		}
		return nil
	})

	return scenarios, err
}

// Rename changes what a scenario is called, and writes no version.
func (r *ScenarioWriteRepository) Rename(
	ctx context.Context, orgID, id uuid.UUID, name string, at time.Time,
) (domain.Scenario, error) {
	var renamed domain.Scenario

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).RenameScenario(ctx, sqlc.RenameScenarioParams{
			OrgID:     orgID,
			ID:        id,
			Name:      name,
			UpdatedAt: at,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("rename the scenario: %w", translate(err))
		}

		renamed, err = toDomainScenario(row)
		return err
	})

	return renamed, err
}

// NewVersion writes the next version and points the scenario at it, in one
// transaction. A head that disagreed with the versions would be a run claiming a
// script nobody kept.
func (r *ScenarioWriteRepository) NewVersion(
	ctx context.Context, orgID, id uuid.UUID, version domain.ScenarioVersion,
	by domain.Identity, at time.Time,
) (domain.Scenario, error) {
	params, err := marshalParams(version.DefaultProfileParams)
	if err != nil {
		return domain.Scenario{}, err
	}

	var updated domain.Scenario
	err = r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		if _, err := queries.InsertScenarioVersion(ctx, sqlc.InsertScenarioVersionParams{
			OrgID:                orgID,
			ScenarioID:           id,
			Version:              int32(version.Version), //nolint:gosec // bounded by the schema
			Script:               version.Script,
			DefaultProfile:       string(version.DefaultProfile),
			DefaultProfileParams: params,
			CreatedBy:            &by.UserID,
			CreatedAt:            at,
		}); err != nil {
			return fmt.Errorf("insert the version: %w", translate(err))
		}

		row, err := queries.PointScenarioAtVersion(ctx, sqlc.PointScenarioAtVersionParams{
			OrgID:                orgID,
			ID:                   id,
			Script:               version.Script,
			Version:              int32(version.Version), //nolint:gosec // bounded by the schema
			DefaultProfile:       string(version.DefaultProfile),
			DefaultProfileParams: params,
			UpdatedAt:            at,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("point the scenario at the version: %w", err)
		}

		updated, err = toDomainScenario(row)
		return err
	})

	return updated, err
}

// Versions lists every version ever written, newest first.
func (r *ScenarioWriteRepository) Versions(
	ctx context.Context, orgID, scenarioID uuid.UUID,
) ([]domain.ScenarioVersion, error) {
	var versions []domain.ScenarioVersion

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListScenarioVersions(ctx, sqlc.ListScenarioVersionsParams{
			OrgID:      orgID,
			ScenarioID: scenarioID,
		})
		if err != nil {
			return fmt.Errorf("list the versions: %w", err)
		}

		versions = make([]domain.ScenarioVersion, 0, len(rows))
		for _, row := range rows {
			version, err := toDomainVersion(row)
			if err != nil {
				return err
			}
			versions = append(versions, version)
		}
		return nil
	})

	return versions, err
}

// Delete removes a scenario that holds no run.
//
// The count and the delete share one transaction: checked outside, two callers
// could each see an unused scenario and the second would delete one that had
// meanwhile been run — and the rows cascade, so that takes a history with it.
func (r *ScenarioWriteRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		runs, err := queries.CountScenarioRuns(ctx, sqlc.CountScenarioRunsParams{
			OrgID:      orgID,
			ScenarioID: id,
		})
		if err != nil {
			return fmt.Errorf("count the scenario's runs: %w", err)
		}
		if runs > 0 {
			return fmt.Errorf("%w: %d run(s)", domain.ErrScenarioNotEmpty, runs)
		}

		affected, err := queries.DeleteScenario(ctx, sqlc.DeleteScenarioParams{OrgID: orgID, ID: id})
		if err != nil {
			return fmt.Errorf("delete the scenario: %w", err)
		}
		if affected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// OfScenario lists the thresholds a scenario imposes.
func (r *ScenarioWriteRepository) OfScenario(
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

		thresholds = make([]domain.Threshold, 0, len(rows))
		for _, row := range rows {
			thresholds = append(thresholds, domain.Threshold{
				ID:         row.ID,
				Metric:     row.Metric,
				Expression: row.Expression,
			})
		}
		return nil
	})

	return thresholds, err
}

// Add imposes a rule on one metric.
func (r *ScenarioWriteRepository) Add(
	ctx context.Context, orgID, scenarioID uuid.UUID, threshold domain.Threshold,
) (domain.Threshold, error) {
	var added domain.Threshold

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).InsertThreshold(ctx, sqlc.InsertThresholdParams{
			ID:         threshold.ID,
			OrgID:      orgID,
			ScenarioID: scenarioID,
			Metric:     threshold.Metric,
			Expression: threshold.Expression,
		})
		if err != nil {
			return fmt.Errorf("insert the threshold: %w", translate(err))
		}

		added = domain.Threshold{ID: row.ID, Metric: row.Metric, Expression: row.Expression}
		return nil
	})

	return added, err
}

// Remove drops a rule. The verdicts of runs already recorded are untouched: they
// live in run_threshold, frozen when the run finished.
func (r *ScenarioWriteRepository) Remove(
	ctx context.Context, orgID, scenarioID, thresholdID uuid.UUID,
) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		affected, err := sqlc.New(tx).DeleteThreshold(ctx, sqlc.DeleteThresholdParams{
			OrgID:      orgID,
			ScenarioID: scenarioID,
			ID:         thresholdID,
		})
		if err != nil {
			return fmt.Errorf("delete the threshold: %w", err)
		}
		if affected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}
