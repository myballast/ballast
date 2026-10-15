//go:build integration

// What the rate limits read, against a real database: the right window, the
// right organization, the right person.
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// triggeredAt backdates a run's creation and names who asked for it, which is
// how a window is staged: the rows are what the limits count.
func (w queueWorld) triggeredAt(
	ctx context.Context, t *testing.T, run domain.Run, by uuid.UUID, at time.Time,
) {
	t.Helper()

	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE run SET created_at = $3, triggered_by = $4 WHERE org_id = $1 AND id = $2`,
			w.org, run.ID, at, by)
		return err
	}); err != nil {
		t.Fatalf("backdate the run: %v", err)
	}
}

// Only the runs inside the window are counted, and the boundary belongs to the
// window: a run exactly one hour old is still inside it, because the limit is
// "since", not "after".
func TestOnlyTheRunsInsideTheWindowAreCounted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-domain.RateWindow)

	world.triggeredAt(ctx, t, world.queue(ctx, t, world.env), world.user, now.Add(-30*time.Minute))
	world.triggeredAt(ctx, t, world.queue(ctx, t, world.env), world.user, since)
	// An hour and a second ago: outside.
	world.triggeredAt(ctx, t, world.queue(ctx, t, world.env), world.user, since.Add(-time.Second))

	counted, err := world.runs.TriggeredSince(ctx, world.org, world.user, since)
	if err != nil {
		t.Fatalf("TriggeredSince: %v", err)
	}
	if counted.ByOrg != 2 || counted.ByUser != 2 {
		t.Fatalf("the window counted %+v, wanted two of each", counted)
	}
}

// One person's count is theirs; the organization's is everybody's. A limit
// that confused the two would let one person spend the whole organization's
// allowance, or stop them at somebody else's.
func TestAPersonsCountIsTheirsAndTheOrganizationsIsEverybodys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-domain.RateWindow)

	colleague := world.anotherPerson(ctx, t)
	world.triggeredAt(ctx, t, world.queue(ctx, t, world.env), world.user, now.Add(-time.Minute))
	world.triggeredAt(ctx, t, world.queue(ctx, t, world.env), colleague, now.Add(-time.Minute))
	world.triggeredAt(ctx, t, world.queue(ctx, t, world.env), colleague, now.Add(-time.Minute))

	counted, err := world.runs.TriggeredSince(ctx, world.org, world.user, since)
	if err != nil {
		t.Fatalf("TriggeredSince: %v", err)
	}
	if counted.ByOrg != 3 {
		t.Errorf("the organization triggered %d runs, wanted 3", counted.ByOrg)
	}
	if counted.ByUser != 1 {
		t.Errorf("the person triggered %d runs, wanted 1", counted.ByUser)
	}
}

// Another organization's runs are not in anybody's window. The count is a
// limit, so counting somebody else's rows would refuse a run for a reason that
// has nothing to do with the caller.
func TestAnotherOrganizationsRunsAreNotCounted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	theirs := newQueueWorld(ctx, t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-domain.RateWindow)

	for range 3 {
		theirs.triggeredAt(ctx, t, theirs.queue(ctx, t, theirs.env), theirs.user, now.Add(-time.Minute))
	}

	ours := newQueueWorld(ctx, t)
	counted, err := ours.runs.TriggeredSince(ctx, ours.org, ours.user, since)
	if err != nil {
		t.Fatalf("TriggeredSince: %v", err)
	}
	if counted.ByOrg != 0 || counted.ByUser != 0 {
		t.Fatalf("another organization's runs were counted: %+v", counted)
	}
}

// The queue is what is waiting, not what has ever waited.
func TestOnlyWaitingRunsAreCountedAsQueued(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)
	world.queue(ctx, t, world.env)

	waiting, err := world.runs.Queued(ctx, world.org)
	if err != nil {
		t.Fatalf("Queued: %v", err)
	}
	if waiting != 2 {
		t.Fatalf("%d runs are waiting, queued 2", waiting)
	}

	// One of them starts: it is no longer waiting.
	at := time.Now().UTC().Truncate(time.Microsecond)
	if _, _, err := world.runs.ClaimNextQueued(ctx, world.org, at); err != nil {
		t.Fatalf("ClaimNextQueued: %v", err)
	}

	waiting, err = world.runs.Queued(ctx, world.org)
	if err != nil {
		t.Fatalf("Queued: %v", err)
	}
	if waiting != 1 {
		t.Fatalf("%d runs are waiting after one started", waiting)
	}
}

// When a slot frees up: one window after the oldest run of it was created.
func TestTheOldestRunOfTheWindowIsFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-domain.RateWindow)

	colleague := world.anotherPerson(ctx, t)
	oldest := now.Add(-50 * time.Minute)
	world.triggeredAt(ctx, t, world.queue(ctx, t, world.env), world.user, oldest)
	world.triggeredAt(ctx, t, world.queue(ctx, t, world.env), world.user, now.Add(-10*time.Minute))
	// Somebody else's, older still: it belongs to the organization's window
	// and not to this person's.
	world.triggeredAt(ctx, t, world.queue(ctx, t, world.env), colleague, now.Add(-55*time.Minute))

	at, found, err := world.runs.OldestSince(ctx, world.org, world.user, since)
	if err != nil || !found {
		t.Fatalf("OldestSince: %v (found=%v)", err, found)
	}
	if !at.Equal(oldest) {
		t.Fatalf("this person's oldest run is %s, wanted %s", at, oldest)
	}

	// The organization's oldest is the colleague's.
	at, found, err = world.runs.OldestSince(ctx, world.org, uuid.Nil, since)
	if err != nil || !found {
		t.Fatalf("OldestSince: %v (found=%v)", err, found)
	}
	if !at.Equal(now.Add(-55 * time.Minute)) {
		t.Fatalf("the organization's oldest run is %s", at)
	}
}

// An empty window holds nothing, which is an answer rather than an error: a
// caller is told to wait only when there is something to wait for.
func TestAnEmptyWindowHoldsNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	since := time.Now().UTC().Add(-domain.RateWindow)

	if _, found, err := world.runs.OldestSince(ctx, world.org, world.user, since); err != nil || found {
		t.Fatalf("an empty window found something: %v (found=%v)", err, found)
	}
}
