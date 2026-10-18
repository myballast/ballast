package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// onlyViewing grants the right to read a project and nothing else: a viewer.
type onlyViewing struct{}

func (onlyViewing) Can(_ context.Context, _ domain.Identity, perm domain.Permission, _ uuid.UUID) (bool, error) {
	return perm == domain.PermViewProject, nil
}

// A run that was launched as version 3 of a scenario that has since moved on:
// its own profile and load, which are not the scenario's.
func earlierRun(f fixture) domain.Run {
	return domain.Run{
		ID:              uuid.New(),
		OrgID:           f.actor.OrgID,
		ProjectID:       f.scenario.ProjectID,
		ScenarioID:      f.scenario.ID,
		EnvironmentID:   f.env.ID,
		ScenarioVersion: 3,
		Profile:         domain.ProfileLoad,
		ProfileParams:   domain.ProfileParams{TargetVUs: 8, RampUpS: 5, PlateauS: 30, RampDownS: 5},
		Status:          domain.StatusFailed,
	}
}

// the scenario as it is now: further on, and with other defaults
func movedOn(s *domain.Scenario, _ *domain.Environment) {
	s.Version = 9
	s.DefaultProfile = domain.ProfileSoak
}

func rerunService(f fixture, authz Authorizer, source domain.Run, environment domain.Environment) *Service {
	f.runs.byID = map[uuid.UUID]domain.Run{source.ID: source}
	return NewService(
		f.runs,
		fakeScenarios{scenario: f.scenario},
		fakeEnvironments{environment: environment},
		&fakeAllowlist{hosts: []string{"staging.example.test"}},
		noSecrets{},
		nil, nil, nil, nil, f.audit, nil, nil,
		authz,
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	)
}

// The criterion: the new run carries what the run it repeats froze, not what
// the scenario is today.
func TestARerunCarriesWhatTheRunFroze(t *testing.T) {
	f := newFixture(t, movedOn)
	source := earlierRun(f)

	created, err := rerunService(f, allowAll{}, source, f.env).Rerun(context.Background(), f.actor, source.ID)
	if err != nil {
		t.Fatalf("Rerun: %v", err)
	}

	if created.ScenarioVersion != 3 {
		t.Errorf("the new run froze version %d, want the earlier run's 3 and not the scenario's %d", created.ScenarioVersion, f.scenario.Version)
	}
	if created.Profile != domain.ProfileLoad {
		t.Errorf("the new run's profile is %q, want load and not the scenario's default", created.Profile)
	}
	if created.ProfileParams != source.ProfileParams {
		t.Errorf("the new run's load is %+v, want %+v", created.ProfileParams, source.ProfileParams)
	}
	if created.ScenarioID != source.ScenarioID || created.EnvironmentID != source.EnvironmentID {
		t.Errorf("the new run is of scenario %s and environment %s, want the same as the earlier run's",
			created.ScenarioID, created.EnvironmentID)
	}
	if created.Status != domain.StatusQueued {
		t.Errorf("the new run is %q, want queued", created.Status)
	}
	if created.TriggeredBy == nil || *created.TriggeredBy != f.actor.UserID {
		t.Errorf("the new run was triggered by %v, want the person who repeated it", created.TriggeredBy)
	}
}

// The ceilings an environment was declared with are not something repeating a
// run can get round: lowered since, they lower the load again.
func TestARerunIsClampedToTheEnvironmentAsItIsNow(t *testing.T) {
	f := newFixture(t, nil)
	source := earlierRun(f)
	lowered := f.env
	lowered.MaxVUs = 4

	created, err := rerunService(f, allowAll{}, source, lowered).Rerun(context.Background(), f.actor, source.ID)
	if err != nil {
		t.Fatalf("Rerun: %v", err)
	}
	if created.ProfileParams.TargetVUs != 4 {
		t.Errorf("the load is %d VUs, want it lowered to the environment's 4", created.ProfileParams.TargetVUs)
	}
}

func TestARerunIsRefusedAgainstATargetThatIsNoLongerVerified(t *testing.T) {
	f := newFixture(t, nil)
	source := earlierRun(f)
	revoked := f.env
	revoked.VerificationStatus = domain.VerificationPending

	_, err := rerunService(f, allowAll{}, source, revoked).Rerun(context.Background(), f.actor, source.ID)

	if !errors.Is(err, domain.ErrTargetNotVerified) {
		t.Fatalf("got %v, want ErrTargetNotVerified", err)
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was queued against a target nobody has verified")
	}
}

// A viewer may read the run and may not repeat it, and the attempt is on the
// record.
func TestAViewerCannotRerun(t *testing.T) {
	f := newFixture(t, nil)
	source := earlierRun(f)

	_, err := rerunService(f, onlyViewing{}, source, f.env).Rerun(context.Background(), f.actor, source.ID)

	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was queued for somebody who may not run tests")
	}
	entries := f.audit.recorded()
	if len(entries) != 1 || entries[0].Outcome != domain.Refused || entries[0].Action != domain.ActionRunRefused {
		t.Errorf("the journal holds %+v, want one refusal", entries)
	}
}

// Somebody who cannot see the run is told it is not there, as for reading it:
// a refusal would confirm the identifier stands for something.
func TestRerunningARunTheCallerCannotSeeReadsAsAbsent(t *testing.T) {
	f := newFixture(t, nil)
	source := earlierRun(f)

	_, err := rerunService(f, refuseAll{}, source, f.env).Rerun(context.Background(), f.actor, source.ID)

	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if errors.Is(err, domain.ErrForbidden) {
		t.Error("the refusal leaked: a 403 would confirm the run exists")
	}
}

func TestRerunningARunThatDoesNotExistIsNotFound(t *testing.T) {
	f := newFixture(t, nil)

	_, err := rerunService(f, allowAll{}, earlierRun(f), f.env).Rerun(context.Background(), f.actor, uuid.New())

	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

// What is asked is checked against the run's project: the permission is the
// one for running tests, on the project the earlier run belongs to.
func TestARerunAsksAboutTheRunsProject(t *testing.T) {
	f := newFixture(t, nil)
	source := earlierRun(f)
	asked := &askedFor{}

	_, _ = rerunService(f, asked, source, f.env).Rerun(context.Background(), f.actor, source.ID)

	if asked.projectID != source.ProjectID {
		t.Errorf("asked about project %s, want the run's %s", asked.projectID, source.ProjectID)
	}
}

// The platform's switch closes this door as it closes POST /runs: repeating a
// run is not a way to start one during an incident.
func TestARerunIsRefusedWhileTheInstallationIsNotAccepting(t *testing.T) {
	f := newFixture(t, nil)
	source := earlierRun(f)
	f.runs.byID = map[uuid.UUID]domain.Run{source.ID: source}
	closed := NewService(
		f.runs, fakeScenarios{scenario: f.scenario}, fakeEnvironments{environment: f.env},
		&fakeAllowlist{hosts: []string{"staging.example.test"}}, noSecrets{},
		nil, nil, nil, nil, f.audit, nil, nil,
		allowAll{}, fixedClock{at: time.Unix(0, 0).UTC()}, Accepting(false), discardLogger(),
	)

	_, err := closed.Rerun(context.Background(), f.actor, source.ID)

	if !errors.Is(err, domain.ErrRunsSuspended) {
		t.Fatalf("got %v, want ErrRunsSuspended", err)
	}
}

// The journal says which run was repeated, so that a pilot reading it can tell
// a repeat from a run somebody wrote out.
func TestARerunSaysWhichRunItRepeats(t *testing.T) {
	f := newFixture(t, nil)
	source := earlierRun(f)

	if _, err := rerunService(f, allowAll{}, source, f.env).Rerun(context.Background(), f.actor, source.ID); err != nil {
		t.Fatalf("Rerun: %v", err)
	}

	entries := f.audit.recorded()
	if len(entries) != 1 || entries[0].Action != domain.ActionRunQueued {
		t.Fatalf("the journal holds %+v, want one run.queued", entries)
	}
	if got := entries[0].Details["rerun_of"]; got != source.ID.String() {
		t.Errorf("rerun_of is %q, want %s", got, source.ID)
	}
}
