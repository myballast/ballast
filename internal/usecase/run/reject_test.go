package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// A run waits in the queue, and what was true when it was accepted may not be
// true when its turn comes: the window of its target can shut, its approval can
// be withdrawn. The worker says so while the run is still queued — the one status
// the state machine lets "rejected" be reached from — instead of starting it and
// failing it afterwards.

// aWaitingRun puts a run in the queue of a worker whose clock is at the epoch.
func aWaitingRun(f executionFixture, mutate func(*domain.Environment)) domain.Run {
	environment := verifiedTarget()
	if mutate != nil {
		mutate(&environment)
	}
	f.service.envs = fakeEnvironments{environment: environment}

	waiting := domain.Run{
		ID:            uuid.New(),
		ProjectID:     uuid.New(),
		EnvironmentID: environment.ID,
		ScenarioID:    uuid.New(),
		Profile:       domain.ProfileSmoke,
		ProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 60},
		Status:        domain.StatusQueued,
	}
	f.runs.waiting = []domain.Run{waiting}
	return waiting
}

func TestARunWhoseWindowShutWhileItWaitedIsRejected(t *testing.T) {
	f := newExecutionFixture()
	f.runs.next = nil

	// The worker's clock is at the epoch, 00:00 UTC on a Thursday. The window
	// is the working day.
	waiting := aWaitingRun(f, func(e *domain.Environment) {
		e.Window = &domain.AllowedWindow{
			StartMinute: 9 * 60, EndMinute: 17 * 60, Zone: "UTC",
			Days: domain.DaysOf(time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
		}
	})

	if _, err := f.service.ProcessNext(context.Background(), f.orgID, nil); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}

	if len(f.runs.rejections) != 1 || f.runs.rejections[0].id != waiting.ID {
		t.Fatalf("rejections: %+v", f.runs.rejections)
	}
	// The reason cites the window: which hours, which days, in which zone.
	reason := f.runs.rejections[0].reason
	for _, want := range []string{"09:00", "17:00", "mon–fri", "UTC"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason %q does not say %q", reason, want)
		}
	}

	// Nothing started, and the refusal is in the journal with nobody's name on
	// it: the person who queued the run asked for the opposite.
	if len(f.executor.executed()) != 0 {
		t.Error("a rejected run was executed")
	}
	entries := f.audit.entries
	if len(entries) != 1 || entries[0].Action != domain.ActionRunRefused || entries[0].ActorID != nil {
		t.Errorf("journal: %+v", entries)
	}
}

func TestARunWhoseTargetLostItsApprovalIsRejected(t *testing.T) {
	f := newExecutionFixture()
	f.runs.next = nil

	waiting := aWaitingRun(f, func(e *domain.Environment) {
		e.VerificationStatus = domain.VerificationPending
	})

	if _, err := f.service.ProcessNext(context.Background(), f.orgID, nil); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}

	if len(f.runs.rejections) != 1 || f.runs.rejections[0].id != waiting.ID {
		t.Fatalf("rejections: %+v", f.runs.rejections)
	}
	if !strings.Contains(f.runs.rejections[0].reason, "not verified") {
		t.Errorf("the reason: %q", f.runs.rejections[0].reason)
	}
}

// A run that is fine is left alone, and goes on to be claimed.
func TestARunInsideItsWindowIsNotRejected(t *testing.T) {
	f := newExecutionFixture()

	aWaitingRun(f, func(e *domain.Environment) {
		e.Window = &domain.AllowedWindow{StartMinute: 22 * 60, EndMinute: 4 * 60, Zone: "UTC"}
	})
	f.runs.waiting[0].ProfileParams = domain.ProfileParams{TargetVUs: 1, PlateauS: 60}

	// 00:00 UTC is inside 22:00–04:00.
	f.process(t)

	if len(f.runs.rejections) != 0 {
		t.Errorf("a run inside its window was rejected: %+v", f.runs.rejections)
	}
}

// A run that outlasts what is left of the window is refused too, as it is when
// it is accepted: starting it would load the target after the window has shut.
func TestARunThatWouldOutlastItsWindowIsRejected(t *testing.T) {
	f := newExecutionFixture()
	f.runs.next = nil

	waiting := aWaitingRun(f, func(e *domain.Environment) {
		// Open until 00:01: a minute of window against a run of an hour.
		e.Window = &domain.AllowedWindow{StartMinute: 22 * 60, EndMinute: 1, Zone: "UTC"}
	})
	f.runs.waiting[0].ProfileParams = domain.ProfileParams{TargetVUs: 1, PlateauS: 3600}

	if _, err := f.service.ProcessNext(context.Background(), f.orgID, nil); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}

	if len(f.runs.rejections) != 1 || f.runs.rejections[0].id != waiting.ID {
		t.Fatalf("rejections: %+v", f.runs.rejections)
	}
	if !strings.Contains(f.runs.rejections[0].reason, "closes in") {
		t.Errorf("the reason: %q", f.runs.rejections[0].reason)
	}
}

// Claimed or cancelled between the read and the write: nothing was rejected, so
// nothing is journaled.
func TestARunSomebodyClaimedFirstIsNotJournaledAsRejected(t *testing.T) {
	f := newExecutionFixture()
	f.runs.next = nil
	f.runs.alreadyClaimed = true

	aWaitingRun(f, func(e *domain.Environment) { e.VerificationStatus = domain.VerificationPending })

	if _, err := f.service.ProcessNext(context.Background(), f.orgID, nil); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if len(f.audit.entries) != 0 {
		t.Errorf("a refusal that did not happen was journaled: %+v", f.audit.entries)
	}
}
