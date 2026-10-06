package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

type fakeSecrets struct {
	values map[string]string
	err    error
	asked  int
}

func (f *fakeSecrets) SecretsOf(_ context.Context, _, _ uuid.UUID) (map[string]string, error) {
	f.asked++
	return f.values, f.err
}

// staticOptions stands in for the k6 options builder: what it produces is tested
// in adapter/k6, and a job needs it only to be non-empty.
type staticOptions struct{}

func (staticOptions) Build(domain.ProfileKind, domain.ProfileParams, []domain.Threshold) ([]byte, error) {
	return []byte(`{"scenarios":{}}`), nil
}

type jobFixture struct {
	service *Service
	secrets *fakeSecrets
	run     domain.Run
	orgID   uuid.UUID
}

func newJobFixture(environment domain.Environment, secrets map[string]string, at time.Time) jobFixture {
	orgID := uuid.New()
	secretReader := &fakeSecrets{values: secrets}

	return jobFixture{
		service: NewService(
			&fakeRuns{},
			fakeScenarios{scenario: domain.Scenario{Script: "export default function () {}"}},
			fakeEnvironments{environment: environment},
			secretReader,
			nil,
			staticOptions{},
			nil,
			allowAll{},
			fixedClock{at: at},
			discardLogger(),
		),
		secrets: secretReader,
		run: domain.Run{
			ID:            uuid.New(),
			EnvironmentID: environment.ID,
			ScenarioID:    uuid.New(),
			Profile:       domain.ProfileSmoke,
		},
		orgID: orgID,
	}
}

func verifiedTarget() domain.Environment {
	return domain.Environment{
		ID:                 uuid.New(),
		ProjectID:          uuid.New(),
		BaseURL:            "https://staging.example.test",
		VerificationStatus: domain.VerificationVerified,
		MaxVUs:             10,
		MaxDurationS:       60,
	}
}

func TestTheGeneratorIsHandedTheEnvironmentsSecrets(t *testing.T) {
	f := newJobFixture(verifiedTarget(), map[string]string{
		"API_TOKEN":  "a-token",
		"BASIC_AUTH": "a-password",
	}, time.Unix(0, 0).UTC())

	job, err := f.service.buildJob(context.Background(), f.orgID, f.run)
	if err != nil {
		t.Fatalf("build the job: %v", err)
	}

	if job.Env["API_TOKEN"] != "a-token" || job.Env["BASIC_AUTH"] != "a-password" {
		t.Fatalf("the secrets did not reach the job: %v", job.Env)
	}
	if job.Env[domain.BaseURLVariable] != "https://staging.example.test" {
		t.Fatalf("the base URL is %q", job.Env[domain.BaseURLVariable])
	}
	if f.secrets.asked != 1 {
		t.Fatalf("the secrets were read %d times", f.secrets.asked)
	}
}

// Defence in depth, and the reason the order in injected() is not an accident: a
// secret named BASE_URL would point the load at a host nobody verified. Storing
// one is refused, and if that refusal is ever loosened, this still holds.
func TestASecretCannotRedirectTheLoad(t *testing.T) {
	f := newJobFixture(verifiedTarget(), map[string]string{
		domain.BaseURLVariable: "https://not-ours.example.test",
	}, time.Unix(0, 0).UTC())

	job, err := f.service.buildJob(context.Background(), f.orgID, f.run)
	if err != nil {
		t.Fatalf("build the job: %v", err)
	}

	if job.Env[domain.BaseURLVariable] != "https://staging.example.test" {
		t.Fatalf("a secret redirected the load to %q", job.Env[domain.BaseURLVariable])
	}
}

// Every variable Ballast injects itself must be one a secret may not be called,
// or the precedence above is the only thing standing between a secret and
// Ballast's own configuration.
func TestEveryVariableBallastInjectsIsRefusedAsASecretKey(t *testing.T) {
	f := newJobFixture(verifiedTarget(), nil, time.Unix(0, 0).UTC())

	job, err := f.service.buildJob(context.Background(), f.orgID, f.run)
	if err != nil {
		t.Fatalf("build the job: %v", err)
	}

	reserved := map[string]bool{}
	for _, key := range domain.ReservedSecretKeys() {
		reserved[key] = true
	}

	for key := range job.Env {
		if !reserved[key] {
			t.Fatalf("Ballast injects %s and a secret may be called that", key)
		}
	}
}

func TestAnUnverifiedTargetIsRefusedAtTheLastMoment(t *testing.T) {
	target := verifiedTarget()
	target.VerificationStatus = domain.VerificationProofOK

	f := newJobFixture(target, nil, time.Unix(0, 0).UTC())

	if _, err := f.service.buildJob(context.Background(), f.orgID, f.run); !errors.Is(err, domain.ErrTargetNotVerified) {
		t.Fatalf("an unapproved target built a job: %v", err)
	}
}

// A run accepted inside the window and started outside it. The queue is not
// instantaneous, and the window is about when traffic actually leaves.
func TestARunThatWaitedUntilTheWindowClosedDoesNotStart(t *testing.T) {
	target := verifiedTarget()
	target.Window = &domain.AllowedWindow{StartMinute: 22 * 60, EndMinute: 23 * 60, Zone: "UTC"}

	threeInTheMorning, err := time.Parse(time.RFC3339, "2026-10-07T03:00:00Z")
	if err != nil {
		t.Fatalf("the test's own timestamp is wrong: %v", err)
	}

	f := newJobFixture(target, map[string]string{"API_TOKEN": "a-token"}, threeInTheMorning)

	if _, err := f.service.buildJob(context.Background(), f.orgID, f.run); !errors.Is(err, domain.ErrOutsideAllowedWindow) {
		t.Fatalf("a run outside the window built a job: %v", err)
	}
	if f.secrets.asked != 0 {
		t.Fatal("the secrets were decrypted for a run that is not allowed to start")
	}
}

func TestARunInsideTheWindowStarts(t *testing.T) {
	target := verifiedTarget()
	target.Window = &domain.AllowedWindow{StartMinute: 22 * 60, EndMinute: 23 * 60, Zone: "UTC"}

	halfPastTen, err := time.Parse(time.RFC3339, "2026-10-06T22:30:00Z")
	if err != nil {
		t.Fatalf("the test's own timestamp is wrong: %v", err)
	}

	f := newJobFixture(target, nil, halfPastTen)

	if _, err := f.service.buildJob(context.Background(), f.orgID, f.run); err != nil {
		t.Fatalf("a run inside the window was refused: %v", err)
	}
}

func TestSecretsThatCannotBeReadStopTheRun(t *testing.T) {
	f := newJobFixture(verifiedTarget(), nil, time.Unix(0, 0).UTC())
	f.secrets.err = errors.New("the key is wrong")

	if _, err := f.service.buildJob(context.Background(), f.orgID, f.run); err == nil {
		t.Fatal("a job was built although the secrets could not be decrypted")
	}
}
