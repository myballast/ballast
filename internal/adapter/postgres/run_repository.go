package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// RunRepository persists runs. Every method opens a tenant transaction, which
// is the only way rows are ever reached.
type RunRepository struct {
	db *DB
}

// NewRunRepository returns a repository over db.
func NewRunRepository(db *DB) *RunRepository {
	return &RunRepository{db: db}
}

// profileParams is the stored shape of a run's load parameters. Spelled out
// rather than derived from the Go field names, so renaming a field in the
// domain does not silently change what past runs mean.
type profileParams struct {
	TargetVUs int `json:"target_vus"`
	RampUpS   int `json:"ramp_up_s"`
	PlateauS  int `json:"plateau_s"`
	RampDownS int `json:"ramp_down_s"`
}

func marshalParams(p domain.ProfileParams) ([]byte, error) {
	return json.Marshal(profileParams{
		TargetVUs: p.TargetVUs,
		RampUpS:   p.RampUpS,
		PlateauS:  p.PlateauS,
		RampDownS: p.RampDownS,
	})
}

func unmarshalParams(raw []byte) (domain.ProfileParams, error) {
	if len(raw) == 0 {
		return domain.ProfileParams{}, nil
	}

	var stored profileParams
	if err := json.Unmarshal(raw, &stored); err != nil {
		return domain.ProfileParams{}, fmt.Errorf("read the stored profile parameters: %w", err)
	}

	return domain.ProfileParams{
		TargetVUs: stored.TargetVUs,
		RampUpS:   stored.RampUpS,
		PlateauS:  stored.PlateauS,
		RampDownS: stored.RampDownS,
	}, nil
}

func toDomainRun(row sqlc.Run) (domain.Run, error) {
	params, err := unmarshalParams(row.ProfileParams)
	if err != nil {
		return domain.Run{}, err
	}

	return domain.Run{
		ID:              row.ID,
		OrgID:           row.OrgID,
		ProjectID:       row.ProjectID,
		ScenarioID:      row.ScenarioID,
		EnvironmentID:   row.EnvironmentID,
		ScenarioVersion: int(row.ScenarioVersion),
		Profile:         domain.ProfileKind(row.Profile),
		ProfileParams:   params,
		Status:          domain.RunStatus(row.Status),
		TriggeredBy:     row.TriggeredBy,
		ErrorMessage:    derefString(row.ErrorMessage),
		CreatedAt:       row.CreatedAt,
		StartedAt:       row.StartedAt,
		EndedAt:         row.EndedAt,
		HeartbeatAt:     row.HeartbeatAt,

		CancelRequestedAt: row.CancelRequestedAt,
	}, nil
}

// toDomainRuns converts a batch, failing on the first row it cannot read: a
// half-converted list of abandoned runs would mean half of them are cleaned up.
func toDomainRuns(rows []sqlc.Run) ([]domain.Run, error) {
	runs := make([]domain.Run, 0, len(rows))
	for _, row := range rows {
		converted, err := toDomainRun(row)
		if err != nil {
			return nil, err
		}
		runs = append(runs, converted)
	}
	return runs, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Create writes a queued run.
func (r *RunRepository) Create(ctx context.Context, run domain.Run) (domain.Run, error) {
	params, err := marshalParams(run.ProfileParams)
	if err != nil {
		return domain.Run{}, err
	}

	var created domain.Run
	err = r.db.InTenantTx(ctx, run.OrgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).CreateRun(ctx, sqlc.CreateRunParams{
			OrgID:         run.OrgID,
			ProjectID:     run.ProjectID,
			ScenarioID:    run.ScenarioID,
			EnvironmentID: run.EnvironmentID,
			// The column is an integer and the check constraint keeps it
			// positive; a scenario version large enough to overflow would
			// mean a script edited two billion times.
			ScenarioVersion: int32(run.ScenarioVersion), //nolint:gosec // bounded by the schema
			Profile:         string(run.Profile),
			ProfileParams:   params,
			TriggeredBy:     run.TriggeredBy,
		})
		if err != nil {
			return fmt.Errorf("insert the run: %w", err)
		}

		created, err = toDomainRun(row)
		return err
	})

	return created, err
}

// ByID reads one run.
func (r *RunRepository) ByID(ctx context.Context, orgID, id uuid.UUID) (domain.Run, error) {
	var found domain.Run

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).GetRun(ctx, sqlc.GetRunParams{OrgID: orgID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			// Indistinguishable from a run belonging to another organization,
			// which is what row-level security turned it into. That is the
			// intended answer: telling the two apart would leak the fact that
			// the run exists.
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read the run: %w", err)
		}

		found, err = toDomainRun(row)
		return err
	})

	return found, err
}

// ClaimNextQueued moves the oldest queued run to running and returns it.
func (r *RunRepository) ClaimNextQueued(
	ctx context.Context, orgID uuid.UUID, at time.Time,
) (domain.Run, bool, error) {
	var (
		claimed domain.Run
		found   bool
	)

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		// Queued, on an environment nothing is holding, and not already being
		// looked at by another worker.
		candidate, err := queries.NextClaimableRun(ctx, sqlc.NextClaimableRunParams{
			OrgID:     orgID,
			ExpiresAt: at,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("look for a claimable run: %w", err)
		}

		next, err := toDomainRun(candidate)
		if err != nil {
			return err
		}

		// In the same transaction as the claim, which is the whole point: a
		// claim that committed before the lock was taken would be a window in
		// which another worker could take the same environment.
		if _, err := queries.AcquireEnvironmentLock(ctx, sqlc.AcquireEnvironmentLockParams{
			OrgID:         orgID,
			EnvironmentID: next.EnvironmentID,
			RunID:         next.ID,
			AcquiredAt:    at,
			ExpiresAt:     next.LockExpiry(at),
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Somebody took this environment between the filter above and
				// here. The run stays queued, untouched, and this worker waits
				// for its next turn rather than hunting for another candidate:
				// the window is narrow, and a poll interval costs a second.
				return nil
			}
			return fmt.Errorf("take the environment's lock: %w", err)
		}

		started, err := queries.StartRun(ctx, sqlc.StartRunParams{
			OrgID:     orgID,
			ID:        next.ID,
			StartedAt: &at,
		})
		if err != nil {
			return fmt.Errorf("start the run: %w", err)
		}

		claimed, err = toDomainRun(started)
		found = err == nil
		return err
	})

	return claimed, found, err
}

// Finish records a run's terminal status.
func (r *RunRepository) Finish(
	ctx context.Context, orgID, id uuid.UUID,
	status domain.RunStatus, at time.Time, message string,
) error {
	var reason *string
	if message != "" {
		reason = &message
	}

	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		finished, err := queries.FinishRun(ctx, sqlc.FinishRunParams{
			OrgID:        orgID,
			ID:           id,
			Status:       string(status),
			EndedAt:      &at,
			ErrorMessage: reason,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// The statement only matches a run that is still running, so no row
			// means somebody already finished it.
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("finish the run: %w", err)
		}

		// The environment goes free in the same transaction that ends the run.
		// Apart, there would be a moment where the run is over and its target
		// still looks busy, or worse the other way round.
		//
		// Named by run, so a lock already taken over after this one expired is
		// not dropped by the worker whose run lost it.
		if _, err := queries.ReleaseEnvironmentLock(ctx, sqlc.ReleaseEnvironmentLockParams{
			OrgID:         orgID,
			EnvironmentID: finished.EnvironmentID,
			RunID:         id,
		}); err != nil {
			return fmt.Errorf("release the environment's lock: %w", err)
		}

		return nil
	})
}

// SaveResults stores everything the generator measured, in one transaction: a
// run whose metrics are half written describes nothing.
func (r *RunRepository) SaveResults(
	ctx context.Context, orgID, runID uuid.UUID, results domain.RunResults,
) error {
	names := make([]string, 0, len(results.Metrics))
	tags := make([]string, 0, len(results.Metrics))
	stats := make([]string, 0, len(results.Metrics))
	values := make([]float64, 0, len(results.Metrics))
	for _, metric := range results.Metrics {
		names = append(names, metric.Metric)
		tags = append(tags, metric.Tag)
		stats = append(stats, metric.Stat)
		values = append(values, metric.Value)
	}

	metrics := make([]string, 0, len(results.Thresholds))
	expressions := make([]string, 0, len(results.Thresholds))
	passed := make([]bool, 0, len(results.Thresholds))
	for _, threshold := range results.Thresholds {
		metrics = append(metrics, threshold.Metric)
		expressions = append(expressions, threshold.Expression)
		passed = append(passed, threshold.Passed)
	}

	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		if len(names) > 0 {
			if err := queries.InsertRunMetrics(ctx, sqlc.InsertRunMetricsParams{
				OrgID:   orgID,
				RunID:   runID,
				Column3: names,
				Column4: tags,
				Column5: stats,
				Column6: values,
			}); err != nil {
				return fmt.Errorf("insert the metrics: %w", err)
			}
		}

		if len(metrics) > 0 {
			if err := queries.InsertRunThresholds(ctx, sqlc.InsertRunThresholdsParams{
				OrgID:   orgID,
				RunID:   runID,
				Column3: metrics,
				Column4: expressions,
				Column5: passed,
			}); err != nil {
				return fmt.Errorf("insert the threshold verdicts: %w", err)
			}
		}

		return nil
	})
}

// Results reads back what a run measured.
func (r *RunRepository) Results(
	ctx context.Context, orgID, runID uuid.UUID,
) (domain.RunResults, error) {
	var results domain.RunResults

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		metrics, err := queries.ListRunMetrics(ctx, sqlc.ListRunMetricsParams{OrgID: orgID, RunID: runID})
		if err != nil {
			return fmt.Errorf("read the metrics: %w", err)
		}
		for _, metric := range metrics {
			results.Metrics = append(results.Metrics, domain.RunMetric{
				Metric: metric.Name,
				Tag:    metric.TagName,
				Stat:   metric.Stat,
				Value:  metric.Value,
			})
		}

		thresholds, err := queries.ListRunThresholds(ctx, sqlc.ListRunThresholdsParams{OrgID: orgID, RunID: runID})
		if err != nil {
			return fmt.Errorf("read the threshold verdicts: %w", err)
		}
		for _, threshold := range thresholds {
			results.Thresholds = append(results.Thresholds, domain.ThresholdResult{
				Metric:     threshold.Metric,
				Expression: threshold.Expression,
				Passed:     threshold.Passed,
			})
		}

		return nil
	})

	return results, err
}

// Heartbeat records that this run is still being watched, and reports whether
// it is still this worker's to watch.
//
// False means the run left running while the worker was executing it: a
// collector decided the worker was dead and took it. The worker's business then
// is to stop, not to carry on writing to a run somebody else has ended.
func (r *RunRepository) Heartbeat(
	ctx context.Context, orgID, id uuid.UUID, at time.Time,
) (bool, error) {
	var ours bool

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		touched, err := sqlc.New(tx).TouchRun(ctx, sqlc.TouchRunParams{
			OrgID:       orgID,
			ID:          id,
			HeartbeatAt: &at,
		})
		if err != nil {
			return fmt.Errorf("record the heartbeat: %w", err)
		}

		ours = touched > 0
		return nil
	})

	return ours, err
}

// OrphanStale moves every run last seen before cutoff out of running.
//
// It stops at orphaned on purpose. The container may still be alive and loading
// somebody's target, so the run is not over; what is settled is that nobody is
// watching it.
func (r *RunRepository) OrphanStale(
	ctx context.Context, orgID uuid.UUID, cutoff time.Time,
) ([]domain.Run, error) {
	var abandoned []domain.Run

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).OrphanStaleRuns(ctx, sqlc.OrphanStaleRunsParams{
			OrgID:       orgID,
			HeartbeatAt: &cutoff,
		})
		if err != nil {
			return fmt.Errorf("look for abandoned runs: %w", err)
		}

		abandoned, err = toDomainRuns(rows)
		return err
	})

	return abandoned, err
}

// Orphaned lists the runs waiting to be cleaned up.
//
// Read separately from OrphanStale rather than using what that returned,
// because a collector that died between the two steps left runs here that no
// later call to OrphanStale would ever return again.
func (r *RunRepository) Orphaned(ctx context.Context, orgID uuid.UUID) ([]domain.Run, error) {
	var waiting []domain.Run

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListOrphanedRuns(ctx, orgID)
		if err != nil {
			return fmt.Errorf("list the abandoned runs: %w", err)
		}

		waiting, err = toDomainRuns(rows)
		return err
	})

	return waiting, err
}

// FailOrphaned ends an abandoned run and frees its target.
//
// Called once whatever the run left behind has been destroyed: this is the
// statement that says the load has stopped, so it must not run while it might
// still be going.
//
// The release is in the same transaction for the reason Finish's is: apart,
// there is a moment where the run is over and its target still looks busy, or
// worse the other way round.
func (r *RunRepository) FailOrphaned(
	ctx context.Context, orgID, id uuid.UUID, at time.Time, message string,
) error {
	var reason *string
	if message != "" {
		reason = &message
	}

	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		failed, err := queries.FailOrphanedRun(ctx, sqlc.FailOrphanedRunParams{
			OrgID:        orgID,
			ID:           id,
			EndedAt:      &at,
			ErrorMessage: reason,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Another collector got there first. Its work and this one's are
			// the same work, so there is nothing to report.
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("end the abandoned run: %w", err)
		}

		if _, err := queries.ReleaseEnvironmentLock(ctx, sqlc.ReleaseEnvironmentLockParams{
			OrgID:         orgID,
			EnvironmentID: failed.EnvironmentID,
			RunID:         id,
		}); err != nil {
			return fmt.Errorf("release the environment's lock: %w", err)
		}

		return nil
	})
}

// LiveRunIDs is every run a container may legitimately exist for: being
// executed, and watched recently enough to believe it.
func (r *RunRepository) LiveRunIDs(
	ctx context.Context, orgID uuid.UUID, since time.Time,
) ([]uuid.UUID, error) {
	var live []uuid.UUID

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		ids, err := sqlc.New(tx).LiveRunIDs(ctx, sqlc.LiveRunIDsParams{
			OrgID:       orgID,
			HeartbeatAt: &since,
		})
		if err != nil {
			return fmt.Errorf("list the live runs: %w", err)
		}

		live = ids
		return nil
	})

	return live, err
}

// Cancel stops a run, or asks it to stop, and says which of the two happened.
//
// One transaction and two statements, because what is possible depends on a
// status that another worker may be changing at this very moment. A queued run
// is cancelled outright: it holds no target and has no container. A run being
// executed is only asked — its generator is loading somebody's environment, and
// the status that says the load has stopped belongs to whoever stops it.
//
// Neither statement matching means the run is over already, which is not a
// failure of this call but an answer to it.
func (r *RunRepository) Cancel(
	ctx context.Context, orgID, id uuid.UUID, at time.Time, message string,
) (domain.CancelOutcome, error) {
	var outcome domain.CancelOutcome

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		var reason *string
		if message != "" {
			reason = &message
		}

		_, err := queries.CancelQueuedRun(ctx, sqlc.CancelQueuedRunParams{
			OrgID:        orgID,
			ID:           id,
			EndedAt:      &at,
			ErrorMessage: reason,
		})
		if err == nil {
			outcome = domain.RunCancelled
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("cancel the queued run: %w", err)
		}

		_, err = queries.RequestRunCancellation(ctx, sqlc.RequestRunCancellationParams{
			OrgID:             orgID,
			ID:                id,
			CancelRequestedAt: &at,
		})
		if err == nil {
			outcome = domain.CancellationRequested
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("ask the run to stop: %w", err)
		}

		return domain.ErrNotCancellable
	})

	return outcome, err
}

// CancelEverything is the emergency stop: nothing else starts, and everything
// under way is asked to stop.
//
// Both in one transaction, so there is no moment in which the queue has been
// emptied and the running runs have not been told — a worker finishing a run in
// that moment would find the queue empty and go to sleep, which is the right
// behaviour for a reason that would not be true yet.
func (r *RunRepository) CancelEverything(
	ctx context.Context, orgID uuid.UUID, at time.Time, message string,
) (int, int, error) {
	var cancelled, asked int

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		var reason *string
		if message != "" {
			reason = &message
		}

		queued, err := queries.CancelEveryQueuedRun(ctx, sqlc.CancelEveryQueuedRunParams{
			OrgID:        orgID,
			EndedAt:      &at,
			ErrorMessage: reason,
		})
		if err != nil {
			return fmt.Errorf("cancel the queued runs: %w", err)
		}
		cancelled = len(queued)

		running, err := queries.RequestEveryRunningCancellation(ctx,
			sqlc.RequestEveryRunningCancellationParams{OrgID: orgID, CancelRequestedAt: &at})
		if err != nil {
			return fmt.Errorf("ask the running runs to stop: %w", err)
		}
		asked = len(running)

		return nil
	})

	return cancelled, asked, err
}

// InProject reads a project's runs, newest first, at most limit of them.
func (r *RunRepository) InProject(ctx context.Context, orgID, projectID uuid.UUID, limit int) ([]domain.Run, error) {
	var found []domain.Run

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListProjectRuns(ctx, sqlc.ListProjectRunsParams{
			OrgID:     orgID,
			ProjectID: projectID,
			Limit:     int32(limit), //nolint:gosec // bounded to run.MaxListLimit by the service
		})
		if err != nil {
			return fmt.Errorf("list the project's runs: %w", err)
		}

		found, err = toDomainRuns(rows)
		return err
	})

	return found, err
}
