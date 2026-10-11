//go:build integration

// Two workers, two runs on one target, over one database: the whole chain from
// claiming to releasing, with the executions timed.
//
// What this is and is not. It demonstrates the behaviour end to end — a second
// run waits, then starts once the target is free — and it catches a lock that is
// never released, since the second run would then never run at all. It does not
// prove the exclusion: two sequential claims cannot be made to overlap from
// outside the transaction, so with the lock's condition removed this test still
// passes. The proof is in adapter/postgres, where two transactions are held open
// across a barrier; that one fails the moment the condition goes.
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// span is one execution, as the worker that ran it saw it.
type span struct {
	runID uuid.UUID
	from  time.Time
	to    time.Time
}

// slowExecutor stands in for the generator. It does nothing but take time and
// write down when it was busy, which is the whole of what this test needs to
// see.
type slowExecutor struct {
	takes time.Duration

	mu    sync.Mutex
	spans []span
}

func (e *slowExecutor) Execute(
	ctx context.Context, job run.Job, _ chan<- domain.LogLine,
) (run.Outcome, error) {
	from := time.Now()

	select {
	case <-time.After(e.takes):
	case <-ctx.Done():
		return run.Outcome{}, ctx.Err()
	}

	e.mu.Lock()
	e.spans = append(e.spans, span{runID: job.RunID, from: from, to: time.Now()})
	e.mu.Unlock()

	// A summary the parser accepts, so the run reaches a terminal state the
	// ordinary way and releases its target.
	summary, err := json.Marshal(map[string]any{
		"metrics": map[string]any{},
		"state":   map[string]any{"testRunDurationMs": 1},
	})
	if err != nil {
		return run.Outcome{}, err
	}

	return run.Outcome{Summary: summary}, nil
}

func (e *slowExecutor) recorded() []span {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]span, len(e.spans))
	copy(out, e.spans)
	return out
}

// worker is a run service of its own, as a second process would have.
func worker(t *testing.T, db *postgres.DB, executor run.Executor) *run.Service {
	t.Helper()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	return run.NewService(
		postgres.NewRunRepository(db),
		postgres.NewScenarioRepository(db),
		postgres.NewEnvironmentRepository(db),
		postgres.NewEnvironmentRepository(db),
		postgres.NewSecretRepository(db, cipher(t)),
		executor,
		k6.NewOptionsBuilder(),
		k6.NewSummaryParser(),
		authz.NewService(postgres.NewMembershipRepository(db), log),
		infra.SystemClock{},
		log,
	)
}

func TestTwoWorkersNeverRunTwoRunsAtOnceOnOneTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	// The host the seed puts in the project's allowlist. A target the project
	// may not reach stops a run before it executes — the check from the network
	// allowlist ticket — which is how the first version of this test managed to
	// execute nothing at all.
	org, scenario, environment := seed(ctx, t, db, "http://"+hostAlias, nil)

	// Two runs waiting on the same target.
	runs := postgres.NewRunRepository(db)
	for range 2 {
		if _, err := runs.Create(ctx, domain.NewRun(
			org,
			domain.Scenario{ID: scenario, ProjectID: projectOf(ctx, t, db, org), Version: 1},
			domain.Environment{ID: environment, ProjectID: projectOf(ctx, t, db, org), MaxVUs: 5, MaxDurationS: 120},
			domain.ProfileSmoke,
			domain.ProfileParams{TargetVUs: 1, PlateauS: 1},
			nil,
		)); err != nil {
			t.Fatalf("queue a run: %v", err)
		}
	}

	// One executor between them, so the spans it records are comparable.
	executor := &slowExecutor{takes: 400 * time.Millisecond}

	// Built before the race starts: two services over one database, which is
	// what two worker processes are.
	workers := []*run.Service{worker(t, db, executor), worker(t, db, executor)}

	// Released together, so the two attempts are at least as simultaneous as
	// anything outside a transaction can make them.
	start := make(chan struct{})

	var wg sync.WaitGroup
	for _, each := range workers {
		wg.Add(1)
		go func(service *run.Service) {
			defer wg.Done()
			<-start

			// Two turns each: whichever worker loses the first race takes the
			// second run once the target is free.
			for range 2 {
				if _, err := service.ProcessNext(ctx, org, nil); err != nil {
					t.Errorf("ProcessNext: %v", err)
					return
				}
			}
		}(each)
	}
	close(start)
	wg.Wait()

	recorded := executor.recorded()
	if len(recorded) != 2 {
		t.Fatalf("%d of the two runs were executed", len(recorded))
	}

	// The whole point. Two generators against one target would each be
	// measuring the other's load, and nothing in the numbers would say so.
	first, second := recorded[0], recorded[1]
	if second.from.Before(first.to) && first.from.Before(second.to) {
		t.Fatalf("two runs executed at once on one target:\n  %s %s → %s\n  %s %s → %s",
			first.runID, first.from, first.to,
			second.runID, second.from, second.to)
	}

	// And the target is free afterwards.
	var held int
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM environment_lock WHERE org_id = $1`, org).Scan(&held)
	}); err != nil {
		t.Fatalf("count the locks: %v", err)
	}
	if held != 0 {
		t.Fatalf("%d lock(s) outlived the runs", held)
	}
}

// projectOf reads the seeded project, which the seed does not hand back.
func projectOf(ctx context.Context, t *testing.T, db *postgres.DB, org uuid.UUID) uuid.UUID {
	t.Helper()

	var project uuid.UUID
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM project WHERE org_id = $1`, org).Scan(&project)
	}); err != nil {
		t.Fatalf("find the seeded project: %v", err)
	}
	return project
}
