package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// The ticket's acceptance criterion, read here for everything a run service
// does: each action leaves an entry naming its author, its target, what it came
// to, and when.

// only returns the single entry the journal holds, and fails if there is not
// exactly one: a test that read the first of several would pass while the
// service recorded something else entirely.
func only(t *testing.T, journal *fakeJournal) domain.AuditEntry {
	t.Helper()

	entries := journal.recorded()
	if len(entries) != 1 {
		t.Fatalf("the journal holds %d entries, wanted 1: %+v", len(entries), entries)
	}
	return entries[0]
}

func TestAQueuedRunIsRecorded(t *testing.T) {
	f := newFixture(t, nil)

	queued, err := f.service.Enqueue(context.Background(), f.command())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	entry := only(t, f.audit)
	switch {
	case entry.Action != domain.ActionRunQueued || entry.Outcome != domain.Allowed:
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	case entry.ActorID == nil || *entry.ActorID != f.actor.UserID:
		t.Errorf("the author is %v", entry.ActorID)
	case entry.TargetType != domain.TargetRun || entry.TargetID == nil || *entry.TargetID != queued.ID:
		t.Errorf("the target is %s %v", entry.TargetType, entry.TargetID)
	case entry.ProjectID == nil || *entry.ProjectID != f.scenario.ProjectID:
		t.Errorf("the project is %v", entry.ProjectID)
	case entry.At.IsZero():
		t.Error("the entry has no date")
	case entry.Details["profile"] != string(domain.ProfileSmoke):
		t.Errorf("the details are %v", entry.Details)
	}
}

// Every refusal a guard decided, with the reason it decided. These are the
// entries a pilot is measured by: nothing else records them, because nothing
// else happened.
func TestEveryRefusedRunIsRecordedWithItsReason(t *testing.T) {
	cases := map[string]struct {
		arrange func(*fixture)
		command func(fixture) EnqueueCommand
		says    string
		// project says whether the refusal knows which project it was about.
		project bool
	}{
		"the platform is not accepting runs": {
			arrange: func(f *fixture) { f.service.accepting = false },
			says:    "not accepting new runs",
		},
		"the caller may not run here": {
			arrange: func(f *fixture) { f.service.authz = refuseAll{} },
			says:    "may not run tests in this project",
			project: true,
		},
		"the target is not verified": {
			arrange: func(f *fixture) {
				f.service.envs = fakeEnvironments{environment: unverified(f.env)}
			},
			says:    "has not been verified",
			project: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			tc.arrange(&f)

			if _, err := f.service.Enqueue(context.Background(), f.command()); err == nil {
				t.Fatal("the run was accepted")
			}

			entry := only(t, f.audit)
			switch {
			case entry.Action != domain.ActionRunRefused || entry.Outcome != domain.Refused:
				t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
			case entry.ActorID == nil || *entry.ActorID != f.actor.UserID:
				t.Errorf("the author is %v", entry.ActorID)
			case !strings.Contains(entry.Details["reason"], tc.says):
				t.Errorf("the reason is %q, wanted something about %q", entry.Details["reason"], tc.says)
			case tc.project && entry.ProjectID == nil:
				t.Error("the refusal does not say which project it was about")
			case !tc.project && entry.ProjectID != nil:
				t.Errorf("a refusal that read nothing names project %v", entry.ProjectID)
			}
		})
	}
}

// A scenario and an environment from two different projects: the refusal is
// filed under the scenario's project, which is the one the caller named.
func TestARunAcrossTwoProjectsIsRecordedAgainstTheScenariosProject(t *testing.T) {
	f := newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.ProjectID = uuid.New()
	})

	if _, err := f.service.Enqueue(context.Background(), f.command()); err == nil {
		t.Fatal("the run was accepted")
	}

	entry := only(t, f.audit)
	if entry.ProjectID == nil || *entry.ProjectID != f.scenario.ProjectID {
		t.Fatalf("the refusal is filed under %v, the scenario's project is %s",
			entry.ProjectID, f.scenario.ProjectID)
	}
}

// Mistyping is not a refusal. A journal that fills up with malformed requests
// is a journal nobody reads, and what belongs in it is somebody being stopped
// from doing something.
func TestAMalformedRequestIsNotRecorded(t *testing.T) {
	f := newFixture(t, nil)

	cmd := f.command()
	cmd.Params = &domain.ProfileParams{TargetVUs: 0, PlateauS: 10}

	if _, err := f.service.Enqueue(context.Background(), cmd); err == nil {
		t.Fatal("parameters that run nothing were accepted")
	}

	if entries := f.audit.recorded(); len(entries) != 0 {
		t.Fatalf("a malformed request was recorded: %+v", entries)
	}
}

func TestACancelledRunIsRecordedWithWhatItCameTo(t *testing.T) {
	for _, outcome := range []domain.CancelOutcome{domain.RunCancelled, domain.CancellationRequested} {
		t.Run(string(outcome), func(t *testing.T) {
			service, _, existing, who := cancelFixture(domain.StatusQueued, outcome)
			audit := &fakeJournal{}
			service.journal = audit

			if _, err := service.Cancel(context.Background(), who, existing.ID); err != nil {
				t.Fatalf("Cancel: %v", err)
			}

			entry := only(t, audit)
			switch {
			case entry.Action != domain.ActionRunCancelled || entry.Outcome != domain.Allowed:
				t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
			case entry.TargetID == nil || *entry.TargetID != existing.ID:
				t.Errorf("the target is %v", entry.TargetID)
			// A queued run is over; a running one has been asked to stop. The
			// journal has to tell them apart, because they are not the same
			// fact about the load.
			case entry.Details["result"] != string(outcome):
				t.Errorf("the entry says the run came to %q", entry.Details["result"])
			}
		})
	}
}

// An emergency stop belongs to the organization, not to a project, and says how
// much it stopped.
func TestAnEmergencyStopIsRecordedAgainstTheOrganization(t *testing.T) {
	runs := &fakeRuns{stoppedQueued: 3, stoppedRunning: 2}
	audit := &fakeJournal{}
	first, second := uuid.New(), uuid.New()

	service := NewService(
		runs, fakeScenarios{}, fakeEnvironments{}, &fakeAllowlist{}, noSecrets{},
		nil, nil,
		&fakeProjects{ids: []uuid.UUID{first, second}},
		nil, audit, nil, nil,
		&adminOf{projects: map[uuid.UUID]bool{first: true, second: true}},
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	)

	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
	if _, _, err := service.StopEverything(context.Background(), who); err != nil {
		t.Fatalf("StopEverything: %v", err)
	}

	entry := only(t, audit)
	switch {
	case entry.Action != domain.ActionRunsStopped || entry.Outcome != domain.Allowed:
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	case entry.ProjectID != nil:
		t.Errorf("an organization-wide action was filed under project %v", entry.ProjectID)
	case entry.TargetType != domain.TargetOrg:
		t.Errorf("the target type is %q", entry.TargetType)
	case entry.Details["cancelled"] != "3" || entry.Details["asked_to_stop"] != "2":
		t.Errorf("the details are %v", entry.Details)
	}
}

// Somebody who administers part of an organization trying to stop all of it is
// precisely what a journal is for.
func TestAnEmergencyStopRefusedIsRecorded(t *testing.T) {
	audit := &fakeJournal{}
	theirs, somebodyElses := uuid.New(), uuid.New()

	service := NewService(
		&fakeRuns{}, fakeScenarios{}, fakeEnvironments{}, &fakeAllowlist{}, noSecrets{},
		nil, nil,
		&fakeProjects{ids: []uuid.UUID{theirs, somebodyElses}},
		nil, audit, nil, nil,
		&adminOf{projects: map[uuid.UUID]bool{theirs: true}},
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	)

	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
	if _, _, err := service.StopEverything(context.Background(), who); err == nil {
		t.Fatal("the stop went through")
	}

	entry := only(t, audit)
	if entry.Action != domain.ActionRunsStopped || entry.Outcome != domain.Refused {
		t.Fatalf("the entry says %s / %s", entry.Action, entry.Outcome)
	}
	if entry.ActorID == nil || *entry.ActorID != who.UserID {
		t.Fatalf("the author is %v", entry.ActorID)
	}
}

// The worker stopping a run on the doorstep — its target unverified since, or
// its window closed — has no author. Nobody asked for it: the person who
// queued the run asked for the opposite.
func TestARunStoppedOnTheDoorstepIsRecordedWithNoAuthor(t *testing.T) {
	f := newExecutionFixture()
	f.service.envs = fakeEnvironments{environment: unverified(verifiedTarget())}

	f.process(t)

	entry := only(t, f.audit)
	switch {
	case entry.Action != domain.ActionRunRefused || entry.Outcome != domain.Refused:
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	case entry.ActorID != nil:
		t.Errorf("the entry names %v as its author", entry.ActorID)
	case entry.TargetID == nil || *entry.TargetID != f.claimed.ID:
		t.Errorf("the target is %v", entry.TargetID)
	case !strings.Contains(entry.Details["reason"], "not verified"):
		t.Errorf("the reason is %q", entry.Details["reason"])
	}
}

// A generator that could not start is an incident, not a refusal: the run says
// so in its own error message, and the journal is not a second error log.
func TestAFailedExecutionIsNotRecordedAsARefusal(t *testing.T) {
	f := newExecutionFixture()
	f.executor.err = errors.New("the generator image is not on this machine")

	f.process(t)

	if entries := f.audit.recorded(); len(entries) != 0 {
		t.Fatalf("a failed execution was recorded as a refusal: %+v", entries)
	}
}

// unverified is the same target, with its proof taken away.
func unverified(environment domain.Environment) domain.Environment {
	environment.VerificationStatus = domain.VerificationPending
	return environment
}
