package run

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Fakes, not mocks: they record what they were given and hand back what the
// test set up. None of them asserts anything, so a test that cares about an
// interaction says so itself.

type fakeRuns struct {
	// listed is what InProject hands back; listedFor and listedLimit record
	// what it was asked.
	listed      []domain.Run
	listedFor   uuid.UUID
	listedLimit int

	created domain.Run
	// byID is what ByID hands back. Empty means every read is a miss.
	byID map[uuid.UUID]domain.Run
	err  error

	// What the heartbeat and the collector see. Guarded, because the heartbeat
	// writes from a goroutine of its own while the test reads.
	mu sync.Mutex
	// beats counts the heartbeats recorded.
	beats int
	// lost makes Heartbeat answer that the run is no longer this worker's,
	// which is what a collector having taken it looks like from here.
	lost bool
	// beatErr is what Heartbeat fails with, a database briefly out of reach.
	beatErr error
	// stale is what OrphanStale returns, and cutoff what it was asked.
	stale  []domain.Run
	cutoff time.Time
	// orphans is what Orphaned returns.
	orphans []domain.Run
	// live is what LiveRunIDs returns, and since what it was asked.
	live  []uuid.UUID
	since time.Time
	// journal records the calls whose order is the rule, shared with the fake
	// that destroys containers.
	journal *journal
	// next is what ClaimNextQueued hands over. Nil means an empty queue.
	next *domain.Run
	// saved is what SaveResults was given and savedFor which run for, so a
	// test can check that what the generator reported is what was stored.
	saved    domain.RunResults
	savedFor uuid.UUID
	saveErr  error
	// cancelled is what Cancel answers, and cancelErr what it fails with.
	cancelled domain.CancelOutcome
	cancelErr error
	// cancelCalls counts the cancellations asked for.
	cancelCalls int
	// stoppedQueued and stoppedRunning are what CancelEverything reports.
	stoppedQueued, stoppedRunning int
	// everythingStopped counts the emergency stops performed.
	everythingStopped int
	// finished is the status Finish was given, finishedMessage the reason it
	// was given with, and finishErr what it fails with — domain.ErrNotFound
	// when somebody else ended the run first.
	finished        domain.RunStatus
	finishedMessage string
	finishErr       error
}

func (f *fakeRuns) Create(_ context.Context, run domain.Run) (domain.Run, error) {
	if f.err != nil {
		return domain.Run{}, f.err
	}
	run.ID = uuid.New()
	f.created = run
	return run, nil
}

func (f *fakeRuns) ByID(_ context.Context, _, id uuid.UUID) (domain.Run, error) {
	if found, ok := f.byID[id]; ok {
		return found, nil
	}
	return domain.Run{}, domain.ErrNotFound
}

func (f *fakeRuns) ClaimNextQueued(context.Context, uuid.UUID, time.Time) (domain.Run, bool, error) {
	if f.next == nil {
		return domain.Run{}, false, nil
	}
	return *f.next, true, nil
}

func (f *fakeRuns) Finish(
	_ context.Context, _, _ uuid.UUID, status domain.RunStatus, _ time.Time, message string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.finished, f.finishedMessage = status, message
	f.journal.record("finished")
	return f.finishErr
}

func (f *fakeRuns) SaveResults(
	_ context.Context, _, runID uuid.UUID, results domain.RunResults,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.saved, f.savedFor = results, runID
	return f.saveErr
}

func (f *fakeRuns) Results(context.Context, uuid.UUID, uuid.UUID) (domain.RunResults, error) {
	return domain.RunResults{}, nil
}

type fakeScenarios struct {
	scenario domain.Scenario
	err      error
}

func (f fakeScenarios) ScenarioByID(context.Context, uuid.UUID, uuid.UUID) (domain.Scenario, error) {
	return f.scenario, f.err
}

func (f fakeScenarios) ScenarioThresholds(context.Context, uuid.UUID, uuid.UUID) ([]domain.Threshold, error) {
	return nil, nil
}

type fakeEnvironments struct {
	environment domain.Environment
	err         error
}

func (f fakeEnvironments) EnvironmentByID(context.Context, uuid.UUID, uuid.UUID) (domain.Environment, error) {
	return f.environment, f.err
}

// noSecrets is an environment with none set, which is every environment in the
// tests that are about accepting a run rather than executing one.
type noSecrets struct{}

func (noSecrets) SecretsOf(context.Context, uuid.UUID, uuid.UUID) (map[string]string, error) {
	return nil, nil
}

// allowAll and refuseAll stand in for the authorizer. What it decides is tested
// in usecase/authz; what matters here is that the service asks, and with which
// project.
type allowAll struct{}

func (allowAll) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return true, nil
}

type refuseAll struct{}

func (refuseAll) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return false, nil
}

// askedFor records what the service asked about, so a test can check the scope
// rather than only the answer.
type askedFor struct {
	perm      domain.Permission
	projectID uuid.UUID
}

func (a *askedFor) Can(
	_ context.Context, _ domain.Identity, perm domain.Permission, projectID uuid.UUID,
) (bool, error) {
	a.perm, a.projectID = perm, projectID
	return true, nil
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fixture struct {
	service  *Service
	runs     *fakeRuns
	scenario domain.Scenario
	env      domain.Environment
	actor    domain.Identity
}

func newFixture(t *testing.T, mutate func(*domain.Scenario, *domain.Environment)) fixture {
	t.Helper()

	projectID := uuid.New()
	scenario := domain.Scenario{
		ID:             uuid.New(),
		ProjectID:      projectID,
		Name:           "checkout",
		Script:         "export default function () {}",
		Version:        3,
		DefaultProfile: domain.ProfileSmoke,
	}
	environment := domain.Environment{
		ID:                 uuid.New(),
		ProjectID:          projectID,
		Name:               "staging",
		BaseURL:            "https://staging.example.test",
		VerificationStatus: domain.VerificationVerified,
		MaxVUs:             10,
		MaxDurationS:       60,
	}

	if mutate != nil {
		mutate(&scenario, &environment)
	}

	runs := &fakeRuns{}

	return fixture{
		service: NewService(
			runs,
			fakeScenarios{scenario: scenario},
			fakeEnvironments{environment: environment},
			&fakeAllowlist{hosts: []string{"staging.example.test"}},
			noSecrets{},
			nil, nil, nil, nil, nil, nil,
			allowAll{},
			fixedClock{at: time.Unix(0, 0).UTC()},
			Accepting(true),
			discardLogger(),
		),
		runs:     runs,
		scenario: scenario,
		env:      environment,
		actor:    domain.Identity{UserID: uuid.New(), OrgID: uuid.New()},
	}
}

func (f fixture) command() EnqueueCommand {
	return EnqueueCommand{
		Actor:         f.actor,
		ScenarioID:    f.scenario.ID,
		EnvironmentID: f.env.ID,
		Profile:       domain.ProfileSmoke,
	}
}

func TestEnqueueFreezesWhatWillBeExecuted(t *testing.T) {
	f := newFixture(t, nil)

	queued, err := f.service.Enqueue(context.Background(), f.command())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	switch {
	case queued.Status != domain.StatusQueued:
		t.Errorf("status is %s, want queued", queued.Status)
	case queued.ScenarioVersion != f.scenario.Version:
		t.Errorf("scenario version is %d, want %d", queued.ScenarioVersion, f.scenario.Version)
	case queued.ProjectID != f.scenario.ProjectID:
		t.Error("the run was not attached to the scenario's project")
	case queued.Profile != domain.ProfileSmoke:
		t.Errorf("profile is %q, want smoke", queued.Profile)
	}
}

// The ceiling is applied before the run is written. What is stored has to be
// what will actually be executed, or the history lies.
func TestEnqueueClampsBeforeStoring(t *testing.T) {
	f := newFixture(t, nil)

	cmd := f.command()
	cmd.Params = &domain.ProfileParams{TargetVUs: 500, RampUpS: 30, PlateauS: 3600, RampDownS: 30}

	queued, err := f.service.Enqueue(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if queued.ProfileParams.TargetVUs != f.env.MaxVUs {
		t.Errorf("stored %d virtual users, the environment allows %d",
			queued.ProfileParams.TargetVUs, f.env.MaxVUs)
	}
	if total := queued.ProfileParams.TotalDuration(); total > f.env.MaxDurationS {
		t.Errorf("stored a run of %ds, the environment allows %ds", total, f.env.MaxDurationS)
	}
	if f.runs.created.ProfileParams != queued.ProfileParams {
		t.Error("what was returned and what was stored differ")
	}
}

// The control the whole product rests on.
func TestEnqueueRefusesAnUnverifiedTarget(t *testing.T) {
	for _, status := range []domain.VerificationStatus{
		domain.VerificationPending,
		domain.VerificationProofOK,
		domain.VerificationFailed,
	} {
		t.Run(string(status), func(t *testing.T) {
			f := newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
				env.VerificationStatus = status
			})

			_, err := f.service.Enqueue(context.Background(), f.command())
			if !errors.Is(err, domain.ErrTargetNotVerified) {
				t.Fatalf("got %v, want ErrTargetNotVerified", err)
			}
			if f.runs.created.ScenarioID != uuid.Nil {
				t.Error("a run was written for an unverified target")
			}
		})
	}
}

// Row-level security keeps one organization's rows away from another's. It says
// nothing about a scenario and an environment from two different projects of
// the same organization, so this is checked in code.
func TestEnqueueRefusesAScenarioAndTargetFromDifferentProjects(t *testing.T) {
	f := newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.ProjectID = uuid.New()
	})

	_, err := f.service.Enqueue(context.Background(), f.command())
	if !errors.Is(err, domain.ErrCrossProject) {
		t.Fatalf("got %v, want ErrCrossProject", err)
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was written across two projects")
	}
}

func TestEnqueueRefusesParametersThatRunNothing(t *testing.T) {
	f := newFixture(t, nil)

	cmd := f.command()
	cmd.Params = &domain.ProfileParams{TargetVUs: 0, PlateauS: 10}

	if _, err := f.service.Enqueue(context.Background(), cmd); !errors.Is(err, domain.ErrInvalidProfileParams) {
		t.Fatalf("got %v, want ErrInvalidProfileParams", err)
	}
}

func TestEnqueueRefusesAnUnknownProfile(t *testing.T) {
	f := newFixture(t, nil)

	cmd := f.command()
	cmd.Profile = domain.ProfileKind("chaos")

	if _, err := f.service.Enqueue(context.Background(), cmd); !errors.Is(err, domain.ErrInvalidProfileParams) {
		t.Fatalf("got %v, want ErrInvalidProfileParams", err)
	}
}

// serviceWith builds a service whose only interesting collaborator is the
// authorizer.
func serviceWith(t *testing.T, authz Authorizer, scenario domain.Scenario, environment domain.Environment) (*Service, *fakeRuns) {
	t.Helper()

	runs := &fakeRuns{}
	return NewService(
		runs,
		fakeScenarios{scenario: scenario},
		fakeEnvironments{environment: environment},
		&fakeAllowlist{hosts: []string{"staging.example.test"}},
		noSecrets{},
		nil, nil, nil, nil, nil, nil,
		authz,
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	), runs
}

// The permission is asked about the scenario's project, which is the project the
// load would be generated for. Asking about anything else would be a check that
// looks right and protects nothing.
func TestEnqueueAsksAboutTheScenariosProject(t *testing.T) {
	f := newFixture(t, nil)
	asked := &askedFor{}

	service, _ := serviceWith(t, asked, f.scenario, f.env)
	if _, err := service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if asked.perm != domain.PermRunTest {
		t.Errorf("asked for bit %d, want PermRunTest", int64(asked.perm))
	}
	if asked.projectID != f.scenario.ProjectID {
		t.Errorf("asked about project %s, want the scenario's %s", asked.projectID, f.scenario.ProjectID)
	}
}

func TestEnqueueRefusesSomebodyWhoMayNotRun(t *testing.T) {
	f := newFixture(t, nil)

	service, runs := serviceWith(t, refuseAll{}, f.scenario, f.env)
	if _, err := service.Enqueue(context.Background(), f.command()); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was written for somebody who was refused")
	}
}

// The permission comes first, before the two identifiers are compared. Both
// faults are present here, and the one reported says which check ran: the other
// order would tell somebody with no business in either project that the two sit
// in different ones.
func TestEnqueueChecksThePermissionBeforeComparingProjects(t *testing.T) {
	f := newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.ProjectID = uuid.New()
	})

	service, _ := serviceWith(t, refuseAll{}, f.scenario, f.env)

	_, err := service.Enqueue(context.Background(), f.command())
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden: the comparison ran before the permission", err)
	}
	if errors.Is(err, domain.ErrCrossProject) {
		t.Error("the mismatch was reported to somebody with no permission in either project")
	}
}

// With the permission granted, the mismatch is what refuses — and it is the
// mismatch that is reported, so a caller can tell the two apart.
func TestEnqueueRefusesAMismatchOncePermissionIsGranted(t *testing.T) {
	f := newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.ProjectID = uuid.New()
	})

	service, runs := serviceWith(t, allowAll{}, f.scenario, f.env)

	_, err := service.Enqueue(context.Background(), f.command())
	if !errors.Is(err, domain.ErrCrossProject) {
		t.Fatalf("got %v, want ErrCrossProject", err)
	}
	if runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was written across two projects")
	}
}

// A run of a project the caller does not belong to reads as absent. Reporting a
// refusal would confirm the identifier stands for something.
func TestByIDHidesARunTheCallerMayNotSee(t *testing.T) {
	f := newFixture(t, nil)

	runs := &fakeRuns{byID: map[uuid.UUID]domain.Run{}}
	id := uuid.New()
	runs.byID[id] = domain.Run{ID: id, OrgID: f.actor.OrgID, ProjectID: uuid.New()}

	service := NewService(
		runs,
		fakeScenarios{scenario: f.scenario},
		fakeEnvironments{environment: f.env},
		&fakeAllowlist{hosts: []string{"staging.example.test"}},
		noSecrets{},
		nil, nil, nil, nil, nil, nil,
		refuseAll{},
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	)

	_, _, err := service.ByID(context.Background(), f.actor, id)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if errors.Is(err, domain.ErrForbidden) {
		t.Error("the refusal leaked: a 403 would confirm the run exists")
	}
}

// The fixture's clock reads midnight UTC, so a window over the small hours is
// open and one over the evening is shut.
func TestEnqueueRefusesARunOutsideTheTargetsWindow(t *testing.T) {
	f := newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.Window = &domain.AllowedWindow{StartMinute: 20 * 60, EndMinute: 23 * 60, Zone: "UTC"}
	})

	_, err := f.service.Enqueue(context.Background(), f.command())
	if !errors.Is(err, domain.ErrOutsideAllowedWindow) {
		t.Fatalf("got %v, want ErrOutsideAllowedWindow", err)
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Fatal("the run was queued anyway")
	}
}

func TestEnqueueAcceptsARunInsideTheTargetsWindow(t *testing.T) {
	f := newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.Window = &domain.AllowedWindow{StartMinute: 0, EndMinute: 6 * 60, Zone: "UTC"}
	})

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("a run inside the window was refused: %v", err)
	}
}

// An environment with no window takes load at any hour, which is every
// environment that existed before the column did.
func TestEnqueueAcceptsARunWhenNoWindowWasDeclared(t *testing.T) {
	f := newFixture(t, nil)

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("a run against an environment with no window was refused: %v", err)
	}
}

// The fixture's clock reads midnight UTC. A window closing at one o'clock takes
// a short run and refuses a long one, which is the rule this covers: the hour
// alone is not the question.
func TestEnqueueRefusesARunThatWouldOutlastTheWindow(t *testing.T) {
	f := newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.Window = &domain.AllowedWindow{StartMinute: 0, EndMinute: 60, Zone: "UTC"}
		// Room for the run below to be refused by the window rather than clamped
		// by the ceiling.
		env.MaxDurationS = 7200
	})

	cmd := f.command()
	cmd.Params = &domain.ProfileParams{TargetVUs: 1, PlateauS: 90 * 60}

	_, err := f.service.Enqueue(context.Background(), cmd)
	if !errors.Is(err, domain.ErrOutsideAllowedWindow) {
		t.Fatalf("got %v, want ErrOutsideAllowedWindow", err)
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Fatal("the run was queued anyway")
	}
}

// The clamp runs first, so a run asking for longer than the environment allows is
// measured against the window at its clamped length, not at the length asked for.
func TestTheWindowIsAskedAboutTheClampedLength(t *testing.T) {
	f := newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.Window = &domain.AllowedWindow{StartMinute: 0, EndMinute: 60, Zone: "UTC"}
		env.MaxDurationS = 300
	})

	cmd := f.command()
	// Two hours asked for, five minutes allowed: it fits once clamped.
	cmd.Params = &domain.ProfileParams{TargetVUs: 1, PlateauS: 2 * 3600}

	queued, err := f.service.Enqueue(context.Background(), cmd)
	if err != nil {
		t.Fatalf("a run clamped to five minutes was refused: %v", err)
	}
	if queued.ProfileParams.TotalDuration() != 300 {
		t.Fatalf("the stored run lasts %ds", queued.ProfileParams.TotalDuration())
	}
}

// The rest of the run repository: what a heartbeat writes, and what a collector
// reads. On fakeRuns rather than a fake of its own, because the service holds
// one repository and these are part of it.

func (f *fakeRuns) Heartbeat(_ context.Context, _, _ uuid.UUID, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.beats++
	if f.beatErr != nil {
		return false, f.beatErr
	}
	return !f.lost, nil
}

func (f *fakeRuns) OrphanStale(_ context.Context, _ uuid.UUID, cutoff time.Time) ([]domain.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cutoff = cutoff
	return f.stale, f.err
}

func (f *fakeRuns) Orphaned(context.Context, uuid.UUID) ([]domain.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.orphans, f.err
}

func (f *fakeRuns) FailOrphaned(
	_ context.Context, _, id uuid.UUID, _ time.Time, _ string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.journal.record("errored " + id.String())
	return f.err
}

func (f *fakeRuns) LiveRunIDs(_ context.Context, _ uuid.UUID, since time.Time) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.since = since
	return f.live, f.err
}

// beatsRecorded is how many heartbeats were written, read from the test's
// goroutine while the service's own may still be writing.
func (f *fakeRuns) beatsRecorded() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.beats
}

func (f *fakeRuns) Cancel(
	_ context.Context, _, _ uuid.UUID, _ time.Time, _ string,
) (domain.CancelOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cancelCalls++
	return f.cancelled, f.cancelErr
}

func (f *fakeRuns) InProject(_ context.Context, _ uuid.UUID, projectID uuid.UUID, limit int) ([]domain.Run, error) {
	f.listedFor, f.listedLimit = projectID, limit
	if f.err != nil {
		return nil, f.err
	}
	return f.listed, nil
}

func (f *fakeRuns) CancelEverything(
	_ context.Context, _ uuid.UUID, _ time.Time, _ string,
) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.everythingStopped++
	return f.stoppedQueued, f.stoppedRunning, f.err
}
