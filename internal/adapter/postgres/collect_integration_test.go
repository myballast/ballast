//go:build integration

// The database half of the garbage collector: what a heartbeat writes, what a
// stale one lets a collector take, and what ending an abandoned run frees.
//
// Against a real database, because every one of these rules is a WHERE clause.
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// lastSeen rewrites a run's heartbeat, which is how a worker's death is staged:
// the run is running and nothing has said anything about it since.
func (w queueWorld) lastSeen(ctx context.Context, t *testing.T, run uuid.UUID, at time.Time) {
	t.Helper()

	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE run SET heartbeat_at = $3 WHERE org_id = $1 AND id = $2`, w.org, run, at)
		return err
	}); err != nil {
		t.Fatalf("set the heartbeat: %v", err)
	}
}

func (w queueWorld) heartbeatOf(ctx context.Context, t *testing.T, id uuid.UUID) time.Time {
	t.Helper()

	found, err := w.runs.ByID(ctx, w.org, id)
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	if found.HeartbeatAt == nil {
		t.Fatalf("the run %s has no heartbeat", id)
	}
	return *found.HeartbeatAt
}

// claimOne takes the one queued run a test has prepared.
func (w queueWorld) claimOne(ctx context.Context, t *testing.T, at time.Time) domain.Run {
	t.Helper()

	claimed, found, err := w.runs.ClaimNextQueued(ctx, w.org, at)
	if err != nil {
		t.Fatalf("ClaimNextQueued: %v", err)
	}
	if !found {
		t.Fatal("nothing was claimed")
	}
	return claimed
}

// A heartbeat writes the time, and says the run is still this worker's.
func TestAHeartbeatIsRecordedAndSaysTheRunIsStillOurs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)

	// Truncated because PostgreSQL keeps microseconds in a timestamptz and Go
	// keeps nanoseconds: a value compared after a round trip has to be one the
	// database can hold.
	at := time.Now().UTC().Truncate(time.Microsecond)
	claimed := world.claimOne(ctx, t, at)

	later := at.Add(12 * time.Second)
	ours, err := world.runs.Heartbeat(ctx, world.org, claimed.ID, later)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if !ours {
		t.Fatal("a worker was told the run it is executing is not its own")
	}

	if seen := world.heartbeatOf(ctx, t, claimed.ID); !seen.Equal(later) {
		t.Fatalf("the run was last seen at %s, wanted %s", seen, later)
	}
}

// The signal a worker needs when a collector has taken its run: the run is no
// longer running, so the heartbeat touches nothing and says so.
func TestAHeartbeatOnARunThatLeftRunningSaysItIsLost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	claimed := world.claimOne(ctx, t, at)

	// A collector decided this worker was dead.
	if _, err := world.runs.OrphanStale(ctx, world.org, at.Add(time.Second)); err != nil {
		t.Fatalf("OrphanStale: %v", err)
	}

	ours, err := world.runs.Heartbeat(ctx, world.org, claimed.ID, at.Add(time.Minute))
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if ours {
		t.Fatal("a worker was told it still owns a run that was taken from it")
	}
}

// The threshold, from both sides: a run seen recently is left alone, and the
// same run once it has gone quiet is not.
func TestOnlyARunUnseenPastTheCutoffIsOrphaned(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	claimed := world.claimOne(ctx, t, at)

	// Seen one second ago, with a cutoff a minute back: still watched.
	world.lastSeen(ctx, t, claimed.ID, at.Add(-time.Second))
	taken, err := world.runs.OrphanStale(ctx, world.org, at.Add(-time.Minute))
	if err != nil {
		t.Fatalf("OrphanStale: %v", err)
	}
	if len(taken) != 0 {
		t.Fatalf("a run seen a second ago was collected: %v", taken)
	}
	if status := world.statusOf(ctx, t, claimed.ID); status != domain.StatusRunning {
		t.Fatalf("the run is %q", status)
	}

	// Not seen for ten minutes, same cutoff.
	world.lastSeen(ctx, t, claimed.ID, at.Add(-10*time.Minute))
	taken, err = world.runs.OrphanStale(ctx, world.org, at.Add(-time.Minute))
	if err != nil {
		t.Fatalf("OrphanStale: %v", err)
	}
	if len(taken) != 1 || taken[0].ID != claimed.ID {
		t.Fatalf("an abandoned run was not collected: %v", taken)
	}

	// orphaned, and not errored: at this point the container may still be
	// loading the target, so the run is not over.
	if status := world.statusOf(ctx, t, claimed.ID); status != domain.StatusOrphaned {
		t.Fatalf("the abandoned run is %q, wanted orphaned", status)
	}
	if held := world.locksHeld(ctx, t); held[world.env.ID] != claimed.ID {
		t.Fatalf("the target was freed before the run was cleaned up: %v", held)
	}
}

// The end of an abandoned run: errored, with a reason, and its target free in
// the same transaction.
func TestEndingAnAbandonedRunFreesItsTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	claimed := world.claimOne(ctx, t, at)
	world.lastSeen(ctx, t, claimed.ID, at.Add(-time.Hour))

	if _, err := world.runs.OrphanStale(ctx, world.org, at.Add(-time.Minute)); err != nil {
		t.Fatalf("OrphanStale: %v", err)
	}

	waiting, err := world.runs.Orphaned(ctx, world.org)
	if err != nil {
		t.Fatalf("Orphaned: %v", err)
	}
	if len(waiting) != 1 || waiting[0].ID != claimed.ID {
		t.Fatalf("the abandoned run is not waiting to be collected: %v", waiting)
	}

	if err := world.runs.FailOrphaned(
		ctx, world.org, claimed.ID, at, "its worker stopped reporting",
	); err != nil {
		t.Fatalf("FailOrphaned: %v", err)
	}

	ended, err := world.runs.ByID(ctx, world.org, claimed.ID)
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	if ended.Status != domain.StatusErrored {
		t.Fatalf("the collected run is %q, wanted errored", ended.Status)
	}
	if ended.ErrorMessage == "" {
		t.Fatal("the collected run says nothing about why it ended")
	}
	if ended.EndedAt == nil {
		t.Fatal("the collected run has no end")
	}

	if held := world.locksHeld(ctx, t); len(held) != 0 {
		t.Fatalf("the target is still held: %v", held)
	}

	// And a second collector finds nothing left to end, rather than rewriting
	// the first one's ending.
	err = world.runs.FailOrphaned(ctx, world.org, claimed.ID, at, "collected twice")
	if err == nil {
		t.Fatal("an already collected run was collected again")
	}
}

// What the sweep of a starting worker is allowed to spare: a run being
// executed, and watched recently enough to believe it.
func TestLiveRunsAreTheOnesStillBeingWatched(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	claimed := world.claimOne(ctx, t, at)

	live, err := world.runs.LiveRunIDs(ctx, world.org, at.Add(-time.Minute))
	if err != nil {
		t.Fatalf("LiveRunIDs: %v", err)
	}
	if len(live) != 1 || live[0] != claimed.ID {
		t.Fatalf("a run started a moment ago is not live: %v", live)
	}

	// The same run, gone quiet. Its container has nobody watching it either, so
	// the sweep is right to take it.
	world.lastSeen(ctx, t, claimed.ID, at.Add(-time.Hour))
	live, err = world.runs.LiveRunIDs(ctx, world.org, at.Add(-time.Minute))
	if err != nil {
		t.Fatalf("LiveRunIDs: %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("a run unseen for an hour is live: %v", live)
	}
}

// The collector is as tenant-scoped as everything else: it runs for one
// organization, and another organization's abandoned run is not its business —
// nor is it visible to it at all.
func TestACollectorSeesNoOtherOrganizationsAbandonedRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	theirs := newQueueWorld(ctx, t)
	theirs.queue(ctx, t, theirs.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	abandoned := theirs.claimOne(ctx, t, at)
	theirs.lastSeen(ctx, t, abandoned.ID, at.Add(-time.Hour))

	// A second organization, with its own everything.
	ours := newQueueWorld(ctx, t)

	taken, err := ours.runs.OrphanStale(ctx, ours.org, at.Add(-time.Minute))
	if err != nil {
		t.Fatalf("OrphanStale: %v", err)
	}
	if len(taken) != 0 {
		t.Fatalf("a collector took another organization's run: %v", taken)
	}

	waiting, err := ours.runs.Orphaned(ctx, ours.org)
	if err != nil {
		t.Fatalf("Orphaned: %v", err)
	}
	if len(waiting) != 0 {
		t.Fatalf("a collector was handed another organization's run: %v", waiting)
	}

	// Which is not a matter of its own organization being empty: the run is
	// still there, untouched, for its own collector to find.
	if status := theirs.statusOf(ctx, t, abandoned.ID); status != domain.StatusRunning {
		t.Fatalf("the other organization's run is %q", status)
	}
}
