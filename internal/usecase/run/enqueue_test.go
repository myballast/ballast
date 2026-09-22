package run

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Fakes, not mocks: they record what they were given and hand back what the
// test set up. None of them asserts anything, so a test that cares about an
// interaction says so itself.

type fakeRuns struct {
	created domain.Run
	err     error
}

func (f *fakeRuns) Create(_ context.Context, run domain.Run) (domain.Run, error) {
	if f.err != nil {
		return domain.Run{}, f.err
	}
	run.ID = uuid.New()
	f.created = run
	return run, nil
}

func (f *fakeRuns) ByID(context.Context, uuid.UUID, uuid.UUID) (domain.Run, error) {
	return domain.Run{}, domain.ErrNotFound
}

func (f *fakeRuns) ClaimNextQueued(context.Context, uuid.UUID, time.Time) (domain.Run, bool, error) {
	return domain.Run{}, false, nil
}

func (f *fakeRuns) Finish(context.Context, uuid.UUID, uuid.UUID, domain.RunStatus, time.Time, string) error {
	return nil
}

func (f *fakeRuns) SaveResults(context.Context, uuid.UUID, uuid.UUID, domain.RunResults) error {
	return nil
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
	orgID    uuid.UUID
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
			nil, nil, nil,
			fixedClock{at: time.Unix(0, 0).UTC()},
			discardLogger(),
		),
		runs:     runs,
		scenario: scenario,
		env:      environment,
		orgID:    uuid.New(),
	}
}

func (f fixture) command() EnqueueCommand {
	return EnqueueCommand{
		OrgID:         f.orgID,
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
