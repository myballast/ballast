//go:build integration

// Cancelling, against a real database, because every rule here is a WHERE
// clause: what a queued run's cancellation ends, what a running run's
// cancellation deliberately does not end, and what the emergency stop reaches.
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

func (w queueWorld) cancelRequestedAt(ctx context.Context, t *testing.T, id uuid.UUID) *time.Time {
	t.Helper()

	found, err := w.runs.ByID(ctx, w.org, id)
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	return found.CancelRequestedAt
}

// A queued run is over as soon as it is cancelled: nothing had started, and it
// held no target.
func TestCancellingAQueuedRunEndsIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	queued := world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	outcome, err := world.runs.Cancel(ctx, world.org, queued.ID, at, "somebody changed their mind")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if outcome != domain.RunCancelled {
		t.Fatalf("the outcome is %q, wanted cancelled", outcome)
	}

	ended, err := world.runs.ByID(ctx, world.org, queued.ID)
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	if ended.Status != domain.StatusCancelled {
		t.Fatalf("the run is %q", ended.Status)
	}
	if ended.EndedAt == nil || ended.ErrorMessage == "" || ended.CancelRequestedAt == nil {
		t.Fatalf("the cancelled run records %v / %q / %v",
			ended.EndedAt, ended.ErrorMessage, ended.CancelRequestedAt)
	}

	if held := world.locksHeld(ctx, t); len(held) != 0 {
		t.Fatalf("a queued run held a target: %v", held)
	}
}

// The rule the whole design rests on. A run being executed is asked to stop and
// nothing more: its status still says running and its target is still held,
// because its generator is still loading that target. Ending it here would free
// the target for a run that then measures this one's traffic as well as its own.
func TestCancellingARunningRunOnlyRecordsTheRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	claimed := world.claimOne(ctx, t, at)

	outcome, err := world.runs.Cancel(ctx, world.org, claimed.ID, at, "stop it")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if outcome != domain.CancellationRequested {
		t.Fatalf("the outcome is %q, wanted cancellation_requested", outcome)
	}

	asked, err := world.runs.ByID(ctx, world.org, claimed.ID)
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	if asked.Status != domain.StatusRunning {
		t.Fatalf("the run is %q, wanted running until its worker has stopped it", asked.Status)
	}
	if asked.CancelRequestedAt == nil {
		t.Fatal("the request was not recorded")
	}
	if asked.EndedAt != nil {
		t.Fatal("a run still generating load was given an end")
	}

	if held := world.locksHeld(ctx, t); held[world.env.ID] != claimed.ID {
		t.Fatalf("the target was freed while its generator was still running: %v", held)
	}
}

// Asking twice is one decision. The second asker must not move the moment it
// was taken, which is what a client watching cancel_requested_at reads.
func TestAskingARunToStopTwiceKeepsTheFirstInstant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	claimed := world.claimOne(ctx, t, at)

	if _, err := world.runs.Cancel(ctx, world.org, claimed.ID, at, "stop it"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	first := world.cancelRequestedAt(ctx, t, claimed.ID)

	if _, err := world.runs.Cancel(ctx, world.org, claimed.ID, at.Add(time.Minute), "again"); err != nil {
		t.Fatalf("Cancel again: %v", err)
	}

	if second := world.cancelRequestedAt(ctx, t, claimed.ID); !second.Equal(*first) {
		t.Fatalf("the request moved from %s to %s", first, second)
	}
}

// Neither statement matches a run that is over, and that is the answer rather
// than a failure: there is nothing left to stop.
func TestCancellingAFinishedRunIsRefusedByTheStatements(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	claimed := world.claimOne(ctx, t, at)
	if err := world.runs.Finish(ctx, world.org, claimed.ID, domain.StatusPassed, at, ""); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	_, err := world.runs.Cancel(ctx, world.org, claimed.ID, at, "too late")
	if !errors.Is(err, domain.ErrNotCancellable) {
		t.Fatalf("got %v, want ErrNotCancellable", err)
	}

	if status := world.statusOf(ctx, t, claimed.ID); status != domain.StatusPassed {
		t.Fatalf("a finished run became %q", status)
	}
}

// The emergency stop: the queue is emptied, and everything under way is asked
// to stop. Two counts, because they are two different facts.
func TestTheEmergencyStopEmptiesTheQueueAndAsksTheRest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	second := world.anotherEnvironment(ctx, t)

	// One run under way, and two waiting.
	world.queue(ctx, t, world.env)
	at := time.Now().UTC().Truncate(time.Microsecond)
	claimed := world.claimOne(ctx, t, at)
	waiting := []domain.Run{
		world.queue(ctx, t, world.env),
		world.queue(ctx, t, second),
	}

	cancelled, asked, err := world.runs.CancelEverything(ctx, world.org, at, "incident")
	if err != nil {
		t.Fatalf("CancelEverything: %v", err)
	}
	if cancelled != 2 || asked != 1 {
		t.Fatalf("the stop reported %d cancelled and %d asked, wanted 2 and 1", cancelled, asked)
	}

	for _, run := range waiting {
		if status := world.statusOf(ctx, t, run.ID); status != domain.StatusCancelled {
			t.Errorf("a queued run is %q", status)
		}
	}

	// The running one is asked, not ended: its generator is still going, and
	// its target is still held until the worker has stopped it.
	if status := world.statusOf(ctx, t, claimed.ID); status != domain.StatusRunning {
		t.Errorf("the running run is %q, wanted running", status)
	}
	if world.cancelRequestedAt(ctx, t, claimed.ID) == nil {
		t.Error("the running run was not asked to stop")
	}
	if held := world.locksHeld(ctx, t); held[world.env.ID] != claimed.ID {
		t.Errorf("the running run's target was freed: %v", held)
	}
}

// An emergency stop is an organization's own emergency. Row-level security is
// what keeps it there, and this is the test that would notice if a statement
// were ever written without its org_id.
func TestAnEmergencyStopReachesNoOtherOrganization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	theirs := newQueueWorld(ctx, t)
	untouched := theirs.queue(ctx, t, theirs.env)

	ours := newQueueWorld(ctx, t)
	ours.queue(ctx, t, ours.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	cancelled, asked, err := ours.runs.CancelEverything(ctx, ours.org, at, "incident")
	if err != nil {
		t.Fatalf("CancelEverything: %v", err)
	}
	if cancelled != 1 || asked != 0 {
		t.Fatalf("the stop reached %d queued and %d running runs of its own organization", cancelled, asked)
	}

	if status := theirs.statusOf(ctx, t, untouched.ID); status != domain.StatusQueued {
		t.Fatalf("another organization's run is %q", status)
	}
}
