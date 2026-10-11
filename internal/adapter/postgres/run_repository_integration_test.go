//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// seedProject creates the rows a run needs to exist at all, through the
// application role and its policies.
func seedProject(ctx context.Context, t *testing.T, db *postgres.DB, org uuid.UUID) (domain.Scenario, domain.Environment) {
	t.Helper()

	var (
		user     = uuid.New()
		project  = uuid.New()
		scenario = uuid.New()
		env      = uuid.New()
	)

	err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		for _, statement := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO organization (id, name) VALUES ($1, 'repository test')`, []any{org}},
			{`INSERT INTO app_user (id, org_id, subject, email) VALUES ($1, $2, $3, $4)`,
				[]any{user, org, "subject-" + user.String(), user.String() + "@example.test"}},
			{`INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, 'p', $3)`,
				[]any{project, org, project.String()}},
			{`INSERT INTO environment
				(id, org_id, project_id, name, base_url, verification_token,
				 verification_status, verified_at, verified_by, max_vus, max_duration_s)
			  VALUES ($1, $2, $3, 'staging', 'https://staging.example.test', 'token',
				 'verified', now(), $4, 10, 60)`,
				[]any{env, org, project, user}},
			{`INSERT INTO scenario (id, org_id, project_id, name, script)
			  VALUES ($1, $2, $3, 'checkout', 'export default function () {}')`,
				[]any{scenario, org, project}},
			// A run names the version it executed; the schema requires it to
			// exist.
			{`INSERT INTO scenario_version
				(org_id, scenario_id, version, script, default_profile)
			  VALUES ($1, $2, 1, 'export default function () {}', 'smoke')`,
				[]any{org, scenario}},
		} {
			if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed the project: %v", err)
	}

	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		_ = db.InTenantTx(cleanup, org, func(tx pgx.Tx) error {
			for _, table := range []string{
				"environment_lock", "run_threshold", "run_metric", "run",
				"scenario_version", "scenario",
				"environment", "project", "app_user", "organization",
			} {
				if _, err := tx.Exec(cleanup,
					"DELETE FROM "+pgx.Identifier{table}.Sanitize()+" WHERE org_id = $1", org); err != nil {
					return err
				}
			}
			return nil
		})
	})

	return domain.Scenario{ID: scenario, ProjectID: project, Version: 1},
		domain.Environment{ID: env, ProjectID: project, MaxVUs: 10, MaxDurationS: 60}
}

func openDB(ctx context.Context, t *testing.T) *postgres.DB {
	t.Helper()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open %q: %v", dsn(), err)
	}
	t.Cleanup(db.Close)

	return db
}

// The whole life of a run, through the repository and therefore through
// InTenantTx at every step.
func TestRunRepositoryRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)

	runs := postgres.NewRunRepository(db)

	params := domain.ProfileParams{TargetVUs: 3, RampUpS: 5, PlateauS: 10, RampDownS: 5}
	created, err := runs.Create(ctx, domain.NewRun(org, scenario, environment, domain.ProfileLoad, params, nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Status != domain.StatusQueued {
		t.Fatalf("a new run is %s, want queued", created.Status)
	}

	// Frozen parameters survive the round trip unchanged.
	read, err := runs.ByID(ctx, org, created.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if read.ProfileParams != params {
		t.Errorf("stored %+v, read back %+v", params, read.ProfileParams)
	}
	if read.ScenarioVersion != scenario.Version {
		t.Errorf("scenario version %d, want %d", read.ScenarioVersion, scenario.Version)
	}

	claimed, ok, err := runs.ClaimNextQueued(ctx, org, time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("ClaimNextQueued: ok=%t err=%v", ok, err)
	}
	if claimed.ID != created.ID {
		t.Fatalf("claimed %s, want %s", claimed.ID, created.ID)
	}
	if claimed.Status != domain.StatusRunning {
		t.Errorf("a claimed run is %s, want running", claimed.Status)
	}

	// Nothing left to claim.
	if _, ok, err := runs.ClaimNextQueued(ctx, org, time.Now().UTC()); err != nil || ok {
		t.Errorf("a second claim returned ok=%t err=%v", ok, err)
	}

	results := domain.RunResults{
		DurationMs: 1234,
		Metrics: []domain.RunMetric{
			{Metric: "http_req_duration", Stat: "avg", Value: 12.5},
			{Metric: "http_req_duration", Stat: "p(95)", Value: 42},
			{Metric: "http_req_duration", Tag: "name:GET /health", Stat: "p(95)", Value: 7},
		},
		Thresholds: []domain.ThresholdResult{
			{Metric: "http_req_failed", Expression: "rate<0.01", Passed: true},
			{Metric: "http_req_duration", Expression: "p(95)<10", Passed: false},
		},
	}
	if err := runs.SaveResults(ctx, org, created.ID, results); err != nil {
		t.Fatalf("SaveResults: %v", err)
	}

	// Six statistics of one trend metric no longer collide: that is what the
	// stat column was added for.
	back, err := runs.Results(ctx, org, created.ID)
	if err != nil {
		t.Fatalf("Results: %v", err)
	}
	if len(back.Metrics) != len(results.Metrics) {
		t.Errorf("stored %d metrics, read back %d", len(results.Metrics), len(back.Metrics))
	}
	if len(back.Thresholds) != len(results.Thresholds) {
		t.Errorf("stored %d verdicts, read back %d", len(results.Thresholds), len(back.Thresholds))
	}
	if back.Verdict() != domain.StatusFailed {
		t.Errorf("verdict is %s, want failed: one threshold did not hold", back.Verdict())
	}

	if err := runs.Finish(ctx, org, created.ID, domain.StatusFailed, time.Now().UTC(), ""); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// Finishing twice must not happen: the statement only matches a run that is
	// still running.
	if err := runs.Finish(ctx, org, created.ID, domain.StatusPassed, time.Now().UTC(), ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a finished run was finished again: %v", err)
	}

	final, err := runs.ByID(ctx, org, created.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if final.Status != domain.StatusFailed {
		t.Errorf("final status is %s, want failed", final.Status)
	}
	if final.EndedAt == nil || final.StartedAt == nil {
		t.Error("a finished run carries no timestamps")
	}
}

// A run belonging to another organization must read as absent, not as
// forbidden: telling the two apart says the run exists.
func TestRunRepositoryHidesOtherOrganizations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	runs := postgres.NewRunRepository(db)

	orgA, orgB := uuid.New(), uuid.New()
	scenarioA, environmentA := seedProject(ctx, t, db, orgA)
	seedProject(ctx, t, db, orgB)

	created, err := runs.Create(ctx, domain.NewRun(
		orgA, scenarioA, environmentA, domain.ProfileSmoke,
		domain.ProfileParams{TargetVUs: 1, PlateauS: 5}, nil,
	))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := runs.ByID(ctx, orgB, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("organization B read organization A's run: %v", err)
	}

	if _, ok, err := runs.ClaimNextQueued(ctx, orgB, time.Now().UTC()); err != nil || ok {
		t.Errorf("organization B claimed organization A's run: ok=%t err=%v", ok, err)
	}
}
