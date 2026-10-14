package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

type fakeEnvironments struct {
	byID    map[uuid.UUID]domain.Environment
	tokens  map[uuid.UUID]string
	created *domain.Environment
	saved   *domain.Environment
	resets  int
	deleted []uuid.UUID
	refuse  error

	// What the verification did, so a test can say that an approval happened
	// once, by whom, and for which host.
	proofs      int
	failures    int
	approved    int
	approvedBy  uuid.UUID
	allowedHost string
	// approvalEntry is the audit entry the approval carried with it.
	approvalEntry domain.AuditEntry
}

func (f *fakeEnvironments) Create(
	_ context.Context, _ uuid.UUID, newEnvironment domain.Environment, token string, at time.Time,
) (domain.Environment, error) {
	newEnvironment.CreatedAt = at
	f.created = &newEnvironment
	f.byID[newEnvironment.ID] = newEnvironment
	f.tokens[newEnvironment.ID] = token
	return newEnvironment, nil
}

func (f *fakeEnvironments) ByID(_ context.Context, _, id uuid.UUID) (domain.Environment, error) {
	if found, ok := f.byID[id]; ok {
		return found, nil
	}
	return domain.Environment{}, domain.ErrNotFound
}

func (f *fakeEnvironments) InProject(_ context.Context, _, projectID uuid.UUID) ([]domain.Environment, error) {
	var found []domain.Environment
	for _, e := range f.byID {
		if e.ProjectID == projectID {
			found = append(found, e)
		}
	}
	return found, nil
}

func (f *fakeEnvironments) Save(
	_ context.Context, _ uuid.UUID, updated domain.Environment, resetVerification bool,
) (domain.Environment, error) {
	if resetVerification {
		f.resets++
		updated.VerificationStatus = domain.VerificationPending
	}
	f.saved = &updated
	f.byID[updated.ID] = updated
	return updated, nil
}

// RecordProofOK and RecordProofFailed are two methods rather than one taking a
// status, so that no caller can write "verified" through the verification path.
// Approving is the only way into that state, and it is a different method with
// a different permission in front of it.
func (f *fakeEnvironments) RecordProofOK(
	_ context.Context, _, id uuid.UUID,
) (domain.Environment, error) {
	f.proofs++
	return f.move(id, domain.VerificationProofOK)
}

func (f *fakeEnvironments) RecordProofFailed(
	_ context.Context, _, id uuid.UUID,
) (domain.Environment, error) {
	f.failures++
	return f.move(id, domain.VerificationFailed)
}

func (f *fakeEnvironments) Approve(
	_ context.Context, _, id uuid.UUID, by domain.Identity, _ time.Time, host string,
	entry domain.AuditEntry,
) (domain.Environment, error) {
	f.approved++
	f.approvedBy = by.UserID
	f.allowedHost = host
	// Kept here rather than in the journal fake: the entry travels into the
	// transaction that approves, so this is where a test sees it.
	f.approvalEntry = entry
	return f.move(id, domain.VerificationVerified)
}

func (f *fakeEnvironments) move(
	id uuid.UUID, to domain.VerificationStatus,
) (domain.Environment, error) {
	found, ok := f.byID[id]
	if !ok {
		return domain.Environment{}, domain.ErrNotFound
	}
	found.VerificationStatus = to
	f.byID[id] = found
	return found, nil
}

func (f *fakeEnvironments) Delete(_ context.Context, _, id uuid.UUID) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.deleted = append(f.deleted, id)
	delete(f.byID, id)
	return nil
}

// fakeSecrets keeps the values it is given, so a test can assert that the right
// one was stored — which is the only place in this package a secret's value
// exists at all.
type fakeSecrets struct {
	values  map[string]string
	order   []string
	removed []string
}

func (f *fakeSecrets) Keys(_ context.Context, _, environmentID uuid.UUID) ([]domain.Secret, error) {
	out := make([]domain.Secret, 0, len(f.order))
	for _, key := range f.order {
		out = append(out, domain.Secret{
			ID:            uuid.New(),
			EnvironmentID: environmentID,
			Key:           key,
		})
	}
	return out, nil
}

func (f *fakeSecrets) Put(
	_ context.Context, _, environmentID uuid.UUID, key, value string, at time.Time,
) (domain.Secret, error) {
	if _, seen := f.values[key]; !seen {
		f.order = append(f.order, key)
	}
	f.values[key] = value

	return domain.Secret{
		ID:            uuid.New(),
		EnvironmentID: environmentID,
		Key:           key,
		CreatedAt:     at,
		UpdatedAt:     at,
	}, nil
}

func (f *fakeSecrets) Remove(_ context.Context, _, _ uuid.UUID, key string) error {
	f.removed = append(f.removed, key)
	delete(f.values, key)
	return nil
}

// fakeJournal stands in for the audit log: it keeps what it was handed, which
// is all a test needs to ask what the journal will hold.
type fakeJournal struct {
	entries []domain.AuditEntry
	err     error
}

func (f *fakeJournal) Record(_ context.Context, entry domain.AuditEntry) error {
	f.entries = append(f.entries, entry)
	return f.err
}

type fixedTokens struct{ token string }

func (t fixedTokens) New() (string, error) { return t.token, nil }

type allowAll struct{}

func (allowAll) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return true, nil
}

type refuseAll struct{}

func (refuseAll) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return false, nil
}

// grantOnly answers yes to one permission and no to every other, which is how a
// route that checks the wrong one is caught.
type grantOnly struct{ perm domain.Permission }

func (g grantOnly) Can(
	_ context.Context, _ domain.Identity, perm domain.Permission, _ uuid.UUID,
) (bool, error) {
	return perm == g.perm, nil
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type fixture struct {
	service      *Service
	environments *fakeEnvironments
	secrets      *fakeSecrets
	proof        *fakeProof
	journal      *fakeJournal
	logged       *bytes.Buffer
	who          domain.Identity
	projectID    uuid.UUID
}

func newFixture(authz Authorizer) fixture {
	environments := &fakeEnvironments{
		byID:   map[uuid.UUID]domain.Environment{},
		tokens: map[uuid.UUID]string{},
	}
	secrets := &fakeSecrets{values: map[string]string{}}
	proof := &fakeProof{}
	journal := &fakeJournal{}
	logged := &bytes.Buffer{}

	return fixture{
		service: NewService(
			environments, secrets,
			fixedTokens{token: "challenge-token"},
			proof,
			journal,
			authz,
			fixedClock{at: time.Unix(0, 0).UTC()},
			// Everything the service logs, at every level, so a test can read it.
			slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug})),
		),
		environments: environments,
		secrets:      secrets,
		journal:      journal,
		proof:        proof,
		logged:       logged,
		who:          domain.Identity{UserID: uuid.New(), OrgID: uuid.New()},
		projectID:    uuid.New(),
	}
}

func (f fixture) request() NewEnvironment {
	return NewEnvironment{
		ProjectID:    f.projectID,
		Name:         "staging",
		BaseURL:      "https://staging.example.test",
		MaxVUs:       50,
		MaxDurationS: 600,
	}
}

func (f fixture) create(t *testing.T) domain.Environment {
	t.Helper()

	created, err := f.service.Create(context.Background(), f.who, f.request())
	if err != nil {
		t.Fatalf("create the environment: %v", err)
	}
	return created
}

// verified puts an environment in the one state that lets load through, so that
// what happens to it when the target changes can be observed.
func (f fixture) verified(t *testing.T) domain.Environment {
	t.Helper()

	created := f.create(t)
	created.VerificationStatus = domain.VerificationVerified
	f.environments.byID[created.ID] = created
	return created
}

func TestADeclaredEnvironmentIsNotVerified(t *testing.T) {
	f := newFixture(allowAll{})

	created := f.create(t)

	if created.VerificationStatus != domain.VerificationPending {
		t.Fatalf("a new environment is %q; it must be pending", created.VerificationStatus)
	}
	if created.IsVerified() {
		t.Fatal("a new environment reports itself verified")
	}
	if f.environments.tokens[created.ID] == "" {
		t.Fatal("no verification token was written")
	}
}

func TestTheWindowIsStoredAsGiven(t *testing.T) {
	f := newFixture(allowAll{})

	request := f.request()
	request.Window = &domain.AllowedWindow{StartMinute: 22 * 60, EndMinute: 6 * 60, Zone: "Europe/Paris"}

	created, err := f.service.Create(context.Background(), f.who, request)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if created.Window == nil {
		t.Fatal("the window was dropped")
	}
	if *created.Window != *request.Window {
		t.Fatalf("stored %+v, asked for %+v", *created.Window, *request.Window)
	}
}

func TestAWindowBallastWillNotStoreIsRefused(t *testing.T) {
	f := newFixture(allowAll{})

	request := f.request()
	request.Window = &domain.AllowedWindow{StartMinute: 0, EndMinute: 60, Zone: "Nowhere/At_All"}

	if _, err := f.service.Create(context.Background(), f.who, request); !errors.Is(err, domain.ErrInvalidEnvironment) {
		t.Fatalf("a window in an unknown zone was accepted: %v", err)
	}
	if f.environments.created != nil {
		t.Fatal("the environment was stored anyway")
	}
}

func TestAnInvalidTargetIsRefusedBeforeAnythingIsStored(t *testing.T) {
	f := newFixture(allowAll{})

	request := f.request()
	request.BaseURL = "ftp://staging.example.test"

	if _, err := f.service.Create(context.Background(), f.who, request); !errors.Is(err, domain.ErrInvalidEnvironment) {
		t.Fatalf("an ftp target was accepted: %v", err)
	}
	if f.environments.created != nil {
		t.Fatal("the environment was stored anyway")
	}
}

// The reason this matters: an environment verified against a host somebody owns,
// then edited to point somewhere else, would carry approved load straight at a
// host nobody proved anything about.
func TestRetargetingAnEnvironmentResetsItsVerification(t *testing.T) {
	f := newFixture(allowAll{})

	verified := f.verified(t)
	elsewhere := "https://not-ours.example.test"

	updated, err := f.service.Update(context.Background(), f.who, f.projectID, verified.ID, Update{
		BaseURL: &elsewhere,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	if updated.IsVerified() {
		t.Fatal("the environment is still verified after being pointed at another host")
	}
	if f.environments.resets != 1 {
		t.Fatalf("the repository was asked to reset the verification %d times", f.environments.resets)
	}
}

func TestChangingThePortResetsTheVerificationToo(t *testing.T) {
	f := newFixture(allowAll{})

	verified := f.verified(t)
	otherPort := "https://staging.example.test:8443"

	updated, err := f.service.Update(context.Background(), f.who, f.projectID, verified.ID, Update{
		BaseURL: &otherPort,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	if updated.IsVerified() {
		t.Fatal("a proof served on one port was accepted for another")
	}
}

// Editing anything below the host leaves the proof standing: ownership is of the
// origin, and re-proving it for a path would be asking for nothing.
func TestEditingThePathOrTheCeilingsKeepsTheVerification(t *testing.T) {
	f := newFixture(allowAll{})

	verified := f.verified(t)
	deeper := "https://staging.example.test/api/v2"
	vus := 80

	updated, err := f.service.Update(context.Background(), f.who, f.projectID, verified.ID, Update{
		BaseURL: &deeper,
		MaxVUs:  &vus,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	if !updated.IsVerified() {
		t.Fatal("changing the path and the ceiling dropped the verification")
	}
	if updated.MaxVUs != vus {
		t.Fatalf("max_vus is %d, asked for %d", updated.MaxVUs, vus)
	}
	if f.environments.resets != 0 {
		t.Fatal("the verification was reset for a change below the host")
	}
}

func TestRemovingAWindowAndLeavingItAloneAreDifferentInstructions(t *testing.T) {
	f := newFixture(allowAll{})

	request := f.request()
	request.Window = &domain.AllowedWindow{StartMinute: 60, EndMinute: 120, Zone: "UTC"}
	created, err := f.service.Create(context.Background(), f.who, request)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	name := "staging-renamed"
	kept, err := f.service.Update(context.Background(), f.who, f.projectID, created.ID, Update{Name: &name})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if kept.Window == nil {
		t.Fatal("renaming the environment removed its window")
	}

	cleared, err := f.service.Update(context.Background(), f.who, f.projectID, created.ID, Update{
		WindowSet: true, Window: nil,
	})
	if err != nil {
		t.Fatalf("clear the window: %v", err)
	}
	if cleared.Window != nil {
		t.Fatal("the window was not removed")
	}
}

func TestAnEnvironmentOfAnotherProjectIsNotThere(t *testing.T) {
	f := newFixture(allowAll{})

	created := f.create(t)
	someoneElsesProject := uuid.New()

	if _, err := f.service.ByID(context.Background(), f.who, someoneElsesProject, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("an environment of another project answered %v, wanted not found", err)
	}
}

func TestEveryOperationIsGuarded(t *testing.T) {
	f := newFixture(refuseAll{})

	// Put the row there behind the service's back: what is tested is the
	// refusal, not whether a creation happened first.
	id := uuid.New()
	f.environments.byID[id] = domain.Environment{ID: id, ProjectID: f.projectID}

	ctx := context.Background()
	operations := map[string]func() error{
		"list": func() error {
			_, err := f.service.InProject(ctx, f.who, f.projectID)
			return err
		},
		"create": func() error {
			_, err := f.service.Create(ctx, f.who, f.request())
			return err
		},
		"read": func() error {
			_, err := f.service.ByID(ctx, f.who, f.projectID, id)
			return err
		},
		"update": func() error {
			_, err := f.service.Update(ctx, f.who, f.projectID, id, Update{})
			return err
		},
		"delete": func() error {
			return f.service.Delete(ctx, f.who, f.projectID, id)
		},
		"list the secret keys": func() error {
			_, err := f.service.SecretKeys(ctx, f.who, f.projectID, id)
			return err
		},
		"set a secret": func() error {
			_, err := f.service.SetSecret(ctx, f.who, f.projectID, id, "API_TOKEN", "t")
			return err
		},
		"remove a secret": func() error {
			return f.service.RemoveSecret(ctx, f.who, f.projectID, id, "API_TOKEN")
		},
	}

	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			if err := operation(); !errors.Is(err, domain.ErrForbidden) {
				t.Fatalf("%s answered %v, wanted forbidden", name, err)
			}
		})
	}
}

// Reading a project and declaring a target are not the same right. A viewer who
// could edit an environment could aim the whole project's load somewhere else.
func TestWritingTakesMoreThanReading(t *testing.T) {
	f := newFixture(grantOnly{perm: domain.PermViewProject})

	id := uuid.New()
	f.environments.byID[id] = domain.Environment{ID: id, ProjectID: f.projectID}

	ctx := context.Background()

	if _, err := f.service.ByID(ctx, f.who, f.projectID, id); err != nil {
		t.Fatalf("a viewer could not read an environment: %v", err)
	}
	if _, err := f.service.Create(ctx, f.who, f.request()); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("a viewer declared an environment: %v", err)
	}
	if _, err := f.service.Update(ctx, f.who, f.projectID, id, Update{}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("a viewer edited an environment: %v", err)
	}
}

// A key names how the target expects to be authenticated. That is not a thing to
// show everybody who may read the project.
func TestTheSecretKeysAreNotForEveryReader(t *testing.T) {
	f := newFixture(grantOnly{perm: domain.PermViewProject})

	id := uuid.New()
	f.environments.byID[id] = domain.Environment{ID: id, ProjectID: f.projectID}

	if _, err := f.service.SecretKeys(context.Background(), f.who, f.projectID, id); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("a viewer listed the secret keys: %v", err)
	}
}

func TestADeletionTheRepositoryRefusesIsReported(t *testing.T) {
	f := newFixture(allowAll{})

	created := f.create(t)
	f.environments.refuse = domain.ErrEnvironmentNotEmpty

	err := f.service.Delete(context.Background(), f.who, f.projectID, created.ID)
	if !errors.Is(err, domain.ErrEnvironmentNotEmpty) {
		t.Fatalf("deleting an environment that holds runs answered %v", err)
	}
}

const token = "ghp_a_token_that_would_be_worth_stealing"

func TestASecretIsStoredUnderItsKey(t *testing.T) {
	f := newFixture(allowAll{})

	created := f.create(t)

	stored, err := f.service.SetSecret(context.Background(), f.who, f.projectID, created.ID, "API_TOKEN", token)
	if err != nil {
		t.Fatalf("set the secret: %v", err)
	}

	if stored.Key != "API_TOKEN" {
		t.Fatalf("stored under %q", stored.Key)
	}
	if f.secrets.values["API_TOKEN"] != token {
		t.Fatal("the repository was handed something other than the value given")
	}
}

// The ticket's acceptance criterion, read literally: no secret value in an API
// response. The service is where that is decided, because the type it returns has
// no field for a value — this test is what keeps somebody from adding one.
func TestWhatComesBackFromSettingASecretDoesNotCarryItsValue(t *testing.T) {
	f := newFixture(allowAll{})

	created := f.create(t)

	stored, err := f.service.SetSecret(context.Background(), f.who, f.projectID, created.ID, "API_TOKEN", token)
	if err != nil {
		t.Fatalf("set the secret: %v", err)
	}

	if found := appearsIn(stored, token); found != "" {
		t.Fatalf("the value came back in %s", found)
	}

	keys, err := f.service.SecretKeys(context.Background(), f.who, f.projectID, created.ID)
	if err != nil {
		t.Fatalf("list the keys: %v", err)
	}
	if len(keys) != 1 || keys[0].Key != "API_TOKEN" {
		t.Fatalf("the key list is %+v", keys)
	}
	for _, secret := range keys {
		if found := appearsIn(secret, token); found != "" {
			t.Fatalf("the value came back in %s", found)
		}
	}
}

// The other half of the criterion: no secret value in the logs. The service logs
// several lines on this path, and this reads all of them.
func TestNothingThePathLogsCarriesTheValue(t *testing.T) {
	f := newFixture(allowAll{})

	created := f.create(t)
	ctx := context.Background()

	if _, err := f.service.SetSecret(ctx, f.who, f.projectID, created.ID, "API_TOKEN", token); err != nil {
		t.Fatalf("set the secret: %v", err)
	}
	if _, err := f.service.SecretKeys(ctx, f.who, f.projectID, created.ID); err != nil {
		t.Fatalf("list the keys: %v", err)
	}
	if err := f.service.RemoveSecret(ctx, f.who, f.projectID, created.ID, "API_TOKEN"); err != nil {
		t.Fatalf("remove the secret: %v", err)
	}

	logged := f.logged.String()
	if logged == "" {
		t.Fatal("nothing was logged at all, so this test proves nothing")
	}
	if strings.Contains(logged, token) {
		t.Fatalf("the value is in the logs:\n%s", logged)
	}
	// Not only the whole value: a fragment of it is a leak as well.
	if strings.Contains(logged, "ghp_") {
		t.Fatalf("part of the value is in the logs:\n%s", logged)
	}
	// The key is expected there, and is what makes the line useful.
	if !strings.Contains(logged, "API_TOKEN") {
		t.Fatalf("the key was not logged, so the line says nothing:\n%s", logged)
	}
}

// A value refused for its shape must not reach a log either — that is exactly
// the moment somebody is tempted to print what was wrong with it.
func TestARefusedSecretIsNotLoggedEither(t *testing.T) {
	f := newFixture(allowAll{})

	created := f.create(t)
	ctx := context.Background()

	_, err := f.service.SetSecret(ctx, f.who, f.projectID, created.ID, "API_TOKEN", token+"\n")
	if !errors.Is(err, domain.ErrInvalidSecret) {
		t.Fatalf("a value with a newline was accepted: %v", err)
	}
	if strings.Contains(err.Error(), "ghp_") {
		t.Fatalf("the refusal repeats the value: %v", err)
	}
	if strings.Contains(f.logged.String(), "ghp_") {
		t.Fatalf("the refused value is in the logs:\n%s", f.logged.String())
	}
}

func TestAKeyThatWouldReconfigureTheGeneratorIsRefused(t *testing.T) {
	f := newFixture(allowAll{})

	created := f.create(t)

	for _, key := range []string{"K6_VUS", "BASE_URL"} {
		if _, err := f.service.SetSecret(context.Background(), f.who, f.projectID, created.ID, key, "x"); !errors.Is(err, domain.ErrInvalidSecret) {
			t.Fatalf("%s was accepted as a secret key: %v", key, err)
		}
		if _, stored := f.secrets.values[key]; stored {
			t.Fatalf("%s was stored anyway", key)
		}
	}
}

func TestSettingASecretTwiceReplacesIt(t *testing.T) {
	f := newFixture(allowAll{})

	created := f.create(t)
	ctx := context.Background()

	if _, err := f.service.SetSecret(ctx, f.who, f.projectID, created.ID, "API_TOKEN", "first"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := f.service.SetSecret(ctx, f.who, f.projectID, created.ID, "API_TOKEN", "second"); err != nil {
		t.Fatalf("set again: %v", err)
	}

	if f.secrets.values["API_TOKEN"] != "second" {
		t.Fatalf("the value is %q", f.secrets.values["API_TOKEN"])
	}
	if len(f.secrets.order) != 1 {
		t.Fatalf("the key is stored %d times", len(f.secrets.order))
	}
}

// appearsIn renders a value the way a response body would and reports where the
// secret turned up, if it did. It reads every exported field, so a field added
// later is covered without anybody remembering to extend this.
func appearsIn(value any, secret string) string {
	rendered, err := json.Marshal(value)
	if err != nil {
		return "a value that could not even be rendered: " + err.Error()
	}
	if bytes.Contains(rendered, []byte(secret)) {
		return string(rendered)
	}

	// Fields with no JSON tag, and anything json skips, still reach a log line
	// through %+v.
	spelled := fmt.Sprintf("%+v", value)
	if strings.Contains(spelled, secret) {
		return spelled
	}

	return ""
}
