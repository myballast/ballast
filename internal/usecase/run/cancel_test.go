package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// adminOf answers like the real authorizer for somebody who administers some of
// an organization's projects and not others.
type adminOf struct {
	projects map[uuid.UUID]bool
	asked    []domain.Permission
}

func (a *adminOf) Can(
	_ context.Context, _ domain.Identity, perm domain.Permission, projectID uuid.UUID,
) (bool, error) {
	a.asked = append(a.asked, perm)
	return a.projects[projectID], nil
}

// cancelFixture is a run service holding one run, with everything else faked.
func cancelFixture(status domain.RunStatus, outcome domain.CancelOutcome) (
	*Service, *fakeRuns, domain.Run, domain.Identity,
) {
	existing := domain.Run{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Status:    status,
	}

	runs := &fakeRuns{
		byID:      map[uuid.UUID]domain.Run{existing.ID: existing},
		cancelled: outcome,
	}

	service := NewService(
		runs,
		fakeScenarios{},
		fakeEnvironments{},
		&fakeAllowlist{},
		noSecrets{},
		nil, nil, nil, nil, &fakeJournal{}, nil, nil,
		allowAll{},
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	)

	return service, runs, existing, domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
}

// A queued run is over when the request returns: nothing had started, and it
// held no target.
func TestCancellingAQueuedRunEndsIt(t *testing.T) {
	service, runs, existing, who := cancelFixture(domain.StatusQueued, domain.RunCancelled)

	outcome, err := service.Cancel(context.Background(), who, existing.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if outcome != domain.RunCancelled {
		t.Fatalf("the outcome is %q, wanted cancelled", outcome)
	}
	if runs.cancelCalls != 1 {
		t.Fatalf("the repository was asked %d time(s)", runs.cancelCalls)
	}
}

// A run being executed is only asked. Reporting it as cancelled here would
// claim the load had stopped while the generator is still sending it, and would
// free the target for a run that then measures this one's traffic too.
func TestCancellingARunningRunOnlyAsksIt(t *testing.T) {
	service, _, existing, who := cancelFixture(domain.StatusRunning, domain.CancellationRequested)

	outcome, err := service.Cancel(context.Background(), who, existing.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if outcome != domain.CancellationRequested {
		t.Fatalf("the outcome is %q, wanted cancellation_requested", outcome)
	}
}

// Nothing to stop. Refused by the state machine before the database is touched,
// so that the answer comes from the rule rather than from a WHERE clause
// happening to match no row.
func TestCancellingAFinishedRunIsRefused(t *testing.T) {
	for _, status := range []domain.RunStatus{
		domain.StatusPassed, domain.StatusFailed, domain.StatusErrored,
		domain.StatusTimeout, domain.StatusCancelled,
	} {
		t.Run(string(status), func(t *testing.T) {
			service, runs, existing, who := cancelFixture(status, domain.RunCancelled)

			_, err := service.Cancel(context.Background(), who, existing.ID)
			if !errors.Is(err, domain.ErrNotCancellable) {
				t.Fatalf("got %v, want ErrNotCancellable", err)
			}
			if runs.cancelCalls != 0 {
				t.Fatalf("a finished run was sent to the database to be cancelled")
			}
		})
	}
}

// A run of a project the caller cannot see answers as a run that is not there.
// A refusal would confirm that the identifier stands for something, which is
// what somebody guessing identifiers is after.
func TestCancellingARunTheCallerMayNotSeeReadsAsAbsent(t *testing.T) {
	service, runs, existing, who := cancelFixture(domain.StatusRunning, domain.CancellationRequested)
	service.authz = refuseAll{}

	_, err := service.Cancel(context.Background(), who, existing.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if runs.cancelCalls != 0 {
		t.Fatal("a run was cancelled by somebody with no right to")
	}
}

// The emergency stop is an administrator's, and an administrator of the whole
// organization. Somebody who administers some of it is refused rather than
// handed a stop that silently leaves the rest of the load running.
func TestTheEmergencyStopIsRefusedToAPartialAdministrator(t *testing.T) {
	theirs, somebodyElses := uuid.New(), uuid.New()

	runs := &fakeRuns{stoppedQueued: 3, stoppedRunning: 2}
	journal := &fakeJournal{}
	service := NewService(
		runs, fakeScenarios{}, fakeEnvironments{}, &fakeAllowlist{}, noSecrets{},
		nil, nil,
		&fakeProjects{ids: []uuid.UUID{theirs, somebodyElses}},
		nil, journal, nil, nil,
		&adminOf{projects: map[uuid.UUID]bool{theirs: true}},
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	)

	_, _, err := service.StopEverything(context.Background(),
		domain.Identity{UserID: uuid.New(), OrgID: uuid.New()})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if runs.everythingStopped != 0 {
		t.Fatal("an organization was stopped by somebody who administers part of it")
	}
}

// And it asks about every project, with both permissions: the one only an
// administrator's mask holds, and the one the action actually is.
func TestTheEmergencyStopStopsEverythingForAnAdministratorOfAllOfIt(t *testing.T) {
	first, second := uuid.New(), uuid.New()

	runs := &fakeRuns{stoppedQueued: 3, stoppedRunning: 2}
	authz := &adminOf{projects: map[uuid.UUID]bool{first: true, second: true}}
	journal := &fakeJournal{}
	service := NewService(
		runs, fakeScenarios{}, fakeEnvironments{}, &fakeAllowlist{}, noSecrets{},
		nil, nil,
		&fakeProjects{ids: []uuid.UUID{first, second}},
		nil, journal, nil, nil, authz,
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	)

	cancelled, asked, err := service.StopEverything(context.Background(),
		domain.Identity{UserID: uuid.New(), OrgID: uuid.New()})
	if err != nil {
		t.Fatalf("StopEverything: %v", err)
	}
	if cancelled != 3 || asked != 2 {
		t.Fatalf("the stop reported %d cancelled and %d asked", cancelled, asked)
	}

	// Two projects, two permissions each.
	if len(authz.asked) != 4 {
		t.Fatalf("the authorizer was asked %d questions, wanted 4: %v", len(authz.asked), authz.asked)
	}
	for _, needed := range []domain.Permission{domain.PermManageProject, domain.PermCancelRun} {
		var seen int
		for _, perm := range authz.asked {
			if perm == needed {
				seen++
			}
		}
		if seen != 2 {
			t.Errorf("%v was asked about %d project(s), wanted 2", needed, seen)
		}
	}
}

// The platform's switch, at the API's door. Nothing about the request is read,
// and nothing is written.
func TestASuspendedInstallationRefusesANewRun(t *testing.T) {
	f := newFixture(t, nil)
	f.service.accepting = false

	if _, err := f.service.Enqueue(context.Background(), f.command()); !errors.Is(err, domain.ErrRunsSuspended) {
		t.Fatalf("got %v, want ErrRunsSuspended", err)
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was written while the installation was not accepting any")
	}
}

// And at the worker's. Without this, a queue filled before the switch was
// thrown would drain itself afterwards, which is the opposite of what somebody
// throwing it wants.
func TestASuspendedWorkerLeavesTheQueueAlone(t *testing.T) {
	f := newCollectFixture(nil)
	f.service.accepting = false
	f.runs.next = running(uuid.New())

	worked, err := f.service.ProcessNext(context.Background(), f.orgID, nil)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if worked {
		t.Fatal("a queued run was started while the installation was not accepting any")
	}
}

// The worker side of a cancellation: the run is asked to stop, and the
// execution ends without waiting for the generator to finish on its own.
func TestARunAskedToStopHasItsExecutionEnded(t *testing.T) {
	executor := &waitingExecutor{release: make(chan struct{})}
	f := newCollectFixture(executor)
	f.service.watchEvery = time.Millisecond

	claimed := running(uuid.New())
	f.runs.next = claimed

	// What the watcher will read: the same run, asked to stop.
	asked := time.Unix(0, 0).UTC()
	stopping := *claimed
	stopping.CancelRequestedAt = &asked
	f.runs.byID = map[uuid.UUID]domain.Run{claimed.ID: stopping}

	// Nothing releases the executor: if the watcher does not end the
	// execution, this test hangs, which is the failure.
	worked, err := f.service.ProcessNext(context.Background(), f.orgID, nil)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !worked {
		t.Fatal("the queued run was not picked up")
	}

	if !executor.wasCancelled() {
		t.Fatal("the generator was left running after the run was asked to stop")
	}
	if f.runs.finished != domain.StatusCancelled {
		t.Fatalf("the run ended as %q, wanted cancelled", f.runs.finished)
	}
	// And it says why, rather than "context canceled".
	if f.runs.finishedMessage != cancelledMessage {
		t.Fatalf("the run says %q", f.runs.finishedMessage)
	}
}
