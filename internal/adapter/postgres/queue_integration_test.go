//go:build integration

// Two runs against one target at once produce two sets of measurements, both
// wrong: each is measuring the other's load as well as its own, and nothing in
// the numbers says so. These tests are about that not happening.
//
// The serialisation point is inside PostgreSQL, so they open real concurrent
// transactions rather than standing in for them.
package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// queueWorld is one project with everything a run needs, and a way to add a
// second target to it.
type queueWorld struct {
	db       *postgres.DB
	runs     *postgres.RunRepository
	org      uuid.UUID
	user     uuid.UUID
	scenario domain.Scenario
	env      domain.Environment
}

func newQueueWorld(ctx context.Context, t *testing.T) queueWorld {
	t.Helper()

	db := openDB(ctx, t)
	org := uuid.New()
	scenario, env := seedProject(ctx, t, db, org)

	var user uuid.UUID
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM app_user WHERE org_id = $1`, org).Scan(&user)
	}); err != nil {
		t.Fatalf("find the seeded user: %v", err)
	}

	return queueWorld{
		db:       db,
		runs:     postgres.NewRunRepository(db),
		org:      org,
		user:     user,
		scenario: scenario,
		env:      env,
	}
}

// anotherEnvironment adds a second verified target to the same project, which is
// what the parallel case needs.
func (w queueWorld) anotherEnvironment(ctx context.Context, t *testing.T) domain.Environment {
	t.Helper()

	id := uuid.New()
	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO environment
			(id, org_id, project_id, name, base_url, verification_token,
			 verification_status, verified_at, verified_by, max_vus, max_duration_s)
		  VALUES ($1, $2, $3, $4, 'https://second.example.test', 'token',
			 'verified', now(), $5, 10, 60)`,
			id, w.org, w.scenario.ProjectID, "second-"+id.String()[:8], w.user)
		return err
	}); err != nil {
		t.Fatalf("seed a second environment: %v", err)
	}

	return domain.Environment{ID: id, ProjectID: w.scenario.ProjectID, MaxVUs: 10, MaxDurationS: 60}
}

// provenEnvironment adds a target that has served its challenge and awaits an
// approval, and returns its identifier.
func (w queueWorld) provenEnvironment(ctx context.Context, t *testing.T) uuid.UUID {
	t.Helper()

	id := uuid.New()
	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO environment
			(id, org_id, project_id, name, base_url, verification_token,
			 verification_status, max_vus, max_duration_s)
		  VALUES ($1, $2, $3, $4, 'https://staging.example.test', 'token',
			 'proof_ok', 10, 60)`,
			id, w.org, w.scenario.ProjectID, "proven-"+id.String()[:8])
		return err
	}); err != nil {
		t.Fatalf("seed a proven environment: %v", err)
	}

	return id
}

// queue puts a run in the queue, against the given target.
func (w queueWorld) queue(ctx context.Context, t *testing.T, env domain.Environment) domain.Run {
	t.Helper()

	created, err := w.runs.Create(ctx, domain.NewRun(
		w.org, w.scenario, env, domain.ProfileSmoke,
		domain.ProfileParams{TargetVUs: 1, PlateauS: 5}, &w.user,
	))
	if err != nil {
		t.Fatalf("queue a run: %v", err)
	}
	return created
}

// claimAtOnce has workers claim concurrently and reports what each one got.
func (w queueWorld) claimAtOnce(ctx context.Context, t *testing.T, workers int) []domain.Run {
	t.Helper()

	var (
		mu      sync.Mutex
		claimed []domain.Run
		wg      sync.WaitGroup
		start   = make(chan struct{})
		at      = time.Now().UTC()
	)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// All of them held at the gate, so the attempts really do overlap.
			<-start

			run, found, err := w.runs.ClaimNextQueued(ctx, w.org, at)
			if err != nil {
				t.Errorf("ClaimNextQueued: %v", err)
				return
			}
			if !found {
				return
			}

			mu.Lock()
			claimed = append(claimed, run)
			mu.Unlock()
		}()
	}

	close(start)
	wg.Wait()

	return claimed
}

func (w queueWorld) statusOf(ctx context.Context, t *testing.T, id uuid.UUID) domain.RunStatus {
	t.Helper()

	found, err := w.runs.ByID(ctx, w.org, id)
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	return found.Status
}

func (w queueWorld) locksHeld(ctx context.Context, t *testing.T) map[uuid.UUID]uuid.UUID {
	t.Helper()

	held := map[uuid.UUID]uuid.UUID{}
	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT environment_id, run_id FROM environment_lock WHERE org_id = $1`, w.org)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var env, run uuid.UUID
			if err := rows.Scan(&env, &run); err != nil {
				return err
			}
			held[env] = run
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read the locks: %v", err)
	}

	return held
}

// Eight workers, eight queued runs, one target: exactly one run starts, and the
// other seven are left exactly as they were.
//
// A sanity check rather than the proof. It cannot insist that the eight overlap,
// and once one of them commits the filter in the select hides every remaining
// run from the others — so it passes with the lock's condition removed, which is
// how that was found out. The proof is further down, where two transactions are
// held open across a barrier.
func TestWorkersClaimingAtOnceGetOneRunBetweenThem(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)

	queued := make([]domain.Run, 0, 8)
	for range 8 {
		queued = append(queued, world.queue(ctx, t, world.env))
	}

	claimed := world.claimAtOnce(ctx, t, 8)

	if len(claimed) != 1 {
		t.Fatalf("%d workers started a run on one target at once", len(claimed))
	}

	running := 0
	for _, run := range queued {
		switch status := world.statusOf(ctx, t, run.ID); status {
		case domain.StatusRunning:
			running++
		case domain.StatusQueued:
		default:
			t.Errorf("a run that was not claimed is %q; it should have been left alone", status)
		}
	}
	if running != 1 {
		t.Fatalf("%d runs are running", running)
	}

	// And the lock says who has it.
	held := world.locksHeld(ctx, t)
	if len(held) != 1 || held[world.env.ID] != claimed[0].ID {
		t.Fatalf("the locks are %v, the claim was %s", held, claimed[0].ID)
	}
}

// The second acceptance criterion: a busy target does not hold up a free one.
// Two workers, two targets, both start.
func TestTwoTargetsAreClaimedInParallel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	second := world.anotherEnvironment(ctx, t)

	first := world.queue(ctx, t, world.env)
	other := world.queue(ctx, t, second)

	claimed := world.claimAtOnce(ctx, t, 2)

	if len(claimed) != 2 {
		t.Fatalf("%d of 2 runs on separate targets were claimed", len(claimed))
	}

	started := map[uuid.UUID]bool{}
	for _, run := range claimed {
		started[run.ID] = true
	}
	if !started[first.ID] || !started[other.ID] {
		t.Fatalf("the claims were %v, wanted both %s and %s", started, first.ID, other.ID)
	}

	if held := world.locksHeld(ctx, t); len(held) != 2 {
		t.Fatalf("%d targets are held, wanted 2", len(held))
	}
}

// A run whose target is busy stays queued. Not rejected, not errored: it is
// waiting, and that is a state it already has.
func TestARunWhoseTargetIsHeldStaysQueued(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	waiting := world.queue(ctx, t, world.env)

	// Somebody else holds the target, and will for an hour.
	holder := world.queue(ctx, t, world.env)
	world.hold(ctx, t, world.env.ID, holder.ID, time.Now().UTC().Add(time.Hour))

	run, found, err := world.runs.ClaimNextQueued(ctx, world.org, time.Now().UTC())
	if err != nil {
		t.Fatalf("ClaimNextQueued: %v", err)
	}
	if found {
		t.Fatalf("a run was claimed on a held target: %s", run.ID)
	}

	if status := world.statusOf(ctx, t, waiting.ID); status != domain.StatusQueued {
		t.Fatalf("the waiting run is %q", status)
	}
}

// A lock that has run out is taken over rather than respected. This is what
// keeps a worker that died from blocking its target for good — until the
// heartbeat ticket makes the wait shorter.
func TestALockThatHasRunOutIsTakenOver(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)

	abandoned := world.queue(ctx, t, world.env)
	world.hold(ctx, t, world.env.ID, abandoned.ID, time.Now().UTC().Add(-time.Minute))

	next := world.queue(ctx, t, world.env)

	claimed, found, err := world.runs.ClaimNextQueued(ctx, world.org, time.Now().UTC())
	if err != nil {
		t.Fatalf("ClaimNextQueued: %v", err)
	}
	if !found {
		t.Fatal("an expired lock held the target")
	}
	// The oldest queued run is the abandoned one, which is still queued: it was
	// never started, only its lock was written.
	if claimed.ID != abandoned.ID && claimed.ID != next.ID {
		t.Fatalf("claimed %s, which is neither queued run", claimed.ID)
	}

	if held := world.locksHeld(ctx, t); held[world.env.ID] != claimed.ID {
		t.Fatalf("the lock names %s, the claim was %s", held[world.env.ID], claimed.ID)
	}
}

// Finishing a run frees its target, in the same transaction that ends it.
func TestFinishingARunFreesItsTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)
	world.queue(ctx, t, world.env)

	first, found, err := world.runs.ClaimNextQueued(ctx, world.org, time.Now().UTC())
	if err != nil || !found {
		t.Fatalf("the first claim: %v", err)
	}

	// While it holds the target, nothing else starts.
	if _, found, err := world.runs.ClaimNextQueued(ctx, world.org, time.Now().UTC()); err != nil || found {
		t.Fatalf("a second run started while the first held the target (found=%v, err=%v)", found, err)
	}

	if err := world.runs.Finish(ctx, world.org, first.ID, domain.StatusPassed, time.Now().UTC(), ""); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if held := world.locksHeld(ctx, t); len(held) != 0 {
		t.Fatalf("the target is still held after the run ended: %v", held)
	}

	second, found, err := world.runs.ClaimNextQueued(ctx, world.org, time.Now().UTC())
	if err != nil || !found {
		t.Fatalf("the queued run did not start once the target was free (found=%v, err=%v)", found, err)
	}
	if second.ID == first.ID {
		t.Fatal("the finished run was claimed again")
	}
}

// Two records of "this target is busy" now exist: the lock row and the run's
// status. They are written in one transaction, and this is what holds them to
// each other — the same arrangement as a scenario's head and its versions.
func TestTheLockAndTheRunStatusNeverDisagree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)

	claimed, found, err := world.runs.ClaimNextQueued(ctx, world.org, time.Now().UTC())
	if err != nil || !found {
		t.Fatalf("claim: %v", err)
	}

	if status := world.statusOf(ctx, t, claimed.ID); status != domain.StatusRunning {
		t.Fatalf("the claimed run is %q, and its target is held", status)
	}
	if held := world.locksHeld(ctx, t); held[world.env.ID] != claimed.ID {
		t.Fatalf("the run is running and the lock says %v", held)
	}

	if err := world.runs.Finish(ctx, world.org, claimed.ID, domain.StatusPassed, time.Now().UTC(), ""); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if status := world.statusOf(ctx, t, claimed.ID); status == domain.StatusRunning {
		t.Fatal("the run is still running after it finished")
	}
	if held := world.locksHeld(ctx, t); len(held) != 0 {
		t.Fatalf("the run has finished and the lock is still %v", held)
	}
}

// A run whose lock expired and was taken over must not drop the new holder's
// lock when it eventually finishes. Its own hold is gone; the target is somebody
// else's now.
func TestAFinishedRunDoesNotDropALockItLost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)

	lost := world.queue(ctx, t, world.env)
	// It is running, and its hold ran out.
	if err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE run SET status = 'running', started_at = now() WHERE org_id = $1 AND id = $2`,
			world.org, lost.ID)
		return err
	}); err != nil {
		t.Fatalf("make the run running: %v", err)
	}

	taken := world.queue(ctx, t, world.env)
	world.hold(ctx, t, world.env.ID, taken.ID, time.Now().UTC().Add(time.Hour))

	if err := world.runs.Finish(ctx, world.org, lost.ID, domain.StatusTimeout, time.Now().UTC(), "too slow"); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if held := world.locksHeld(ctx, t); held[world.env.ID] != taken.ID {
		t.Fatalf("the lock is now %v; the run that lost it dropped somebody else's", held)
	}
}

// The hold lasts the run's own length plus a margin, not a fixed figure: a soak
// test and a smoke test do not deserve the same lock after a crash.
func TestTheHoldLastsAsLongAsTheRunCould(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)

	long, err := world.runs.Create(ctx, domain.NewRun(
		world.org, world.scenario, world.env, domain.ProfileSmoke,
		// Clamped to the environment's 60 seconds on the way in.
		domain.ProfileParams{TargetVUs: 1, PlateauS: 3600}, &world.user,
	))
	if err != nil {
		t.Fatalf("queue a long run: %v", err)
	}

	// Truncated, because PostgreSQL keeps microseconds in a timestamptz and Go
	// keeps nanoseconds: a value compared after a round trip has to be one the
	// database can hold. Untruncated, this passed on a machine whose clock
	// happened to give round microseconds and failed on one that did not.
	at := time.Now().UTC().Truncate(time.Microsecond)
	if _, found, err := world.runs.ClaimNextQueued(ctx, world.org, at); err != nil || !found {
		t.Fatalf("claim: %v", err)
	}

	var expires time.Time
	if err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT expires_at FROM environment_lock WHERE org_id = $1 AND environment_id = $2`,
			world.org, world.env.ID).Scan(&expires)
	}); err != nil {
		t.Fatalf("read the lock: %v", err)
	}

	wanted := long.LockExpiry(at)
	if !expires.Equal(wanted) {
		t.Fatalf("the hold runs out at %v, wanted %v (the run's own duration plus a margin)", expires, wanted)
	}
	if !expires.After(at.Add(time.Duration(long.ProfileParams.TotalDuration()) * time.Second)) {
		t.Fatal("the hold runs out before the run could have finished")
	}
}

// hold writes a lock directly, which is how a test stands in for another worker
// without running one.
func (w queueWorld) hold(ctx context.Context, t *testing.T, env, run uuid.UUID, until time.Time) {
	t.Helper()

	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO environment_lock (org_id, environment_id, run_id, acquired_at, expires_at)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (org_id, environment_id) DO UPDATE
			 SET run_id = excluded.run_id, acquired_at = excluded.acquired_at,
			     expires_at = excluded.expires_at`,
			w.org, env, run, until.Add(-time.Hour), until)
		return err
	}); err != nil {
		t.Fatalf("hold the environment: %v", err)
	}
}

// The mechanism itself, proved rather than hoped for.
//
// The test above releases eight workers at once and expects one claim, but it
// cannot insist that they overlap: as soon as one commits, the filter in the
// select hides every remaining run and the others find nothing. It passes with
// the lock's condition removed, which was discovered by removing it — so it
// stands as a sanity check and not as the proof.
//
// This is the proof. Two transactions are held open across a barrier, so both
// have decided the target is free before either tries to take it, which is the
// one ordering the filter cannot rescue. It drives the same generated
// statements the repository drives, through InTenantTx like everything else;
// what it adds is a pause in the middle, which the repository has no business
// offering.
func TestTwoTransactionsThatBothThinkTheTargetIsFreeOnlyOneTakesIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)
	world.queue(ctx, t, world.env)

	var (
		bothLooked sync.WaitGroup
		took       sync.Mutex
		holders    []uuid.UUID
		done       sync.WaitGroup
	)
	bothLooked.Add(2)

	at := time.Now().UTC()

	// Each transaction takes the lock for the run its own select returned, which
	// is what the repository does. An earlier version of this test handed each
	// one the other's run, and the two deadlocked on each other's row — the
	// foreign key from the lock to the run takes a share lock on it, so crossing
	// them builds a cycle that production never has.
	attempt := func() {
		defer done.Done()

		err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
			queries := sqlc.New(tx)

			mine, err := queries.NextClaimableRun(ctx, sqlc.NextClaimableRunParams{
				OrgID: world.org, ExpiresAt: at,
			})
			if err != nil {
				return err
			}

			// Neither goes on until the other has looked, and both have seen a
			// free target. This is the window the filter cannot close and the
			// lock has to.
			bothLooked.Done()
			bothLooked.Wait()

			_, err = queries.AcquireEnvironmentLock(ctx, sqlc.AcquireEnvironmentLockParams{
				OrgID:         world.org,
				EnvironmentID: mine.EnvironmentID,
				RunID:         mine.ID,
				AcquiredAt:    at,
				ExpiresAt:     at.Add(time.Hour),
			})
			if errors.Is(err, pgx.ErrNoRows) {
				// Somebody else has it. This transaction takes nothing.
				return nil
			}
			if err != nil {
				return err
			}

			took.Lock()
			holders = append(holders, mine.ID)
			took.Unlock()
			return nil
		})
		if err != nil {
			t.Errorf("an attempt failed: %v", err)
		}
	}

	done.Add(2)
	go attempt()
	go attempt()
	done.Wait()

	if len(holders) != 1 {
		t.Fatalf("%d of two transactions took the same target: %v", len(holders), holders)
	}

	if held := world.locksHeld(ctx, t); held[world.env.ID] != holders[0] {
		t.Fatalf("the lock names %s and the winner was %s", held[world.env.ID], holders[0])
	}
}
