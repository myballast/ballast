package handler_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/handler"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/environment"
)

// The secret's value exists in this file exactly twice: in the request body sent
// in, and in the assertions that it never comes back.
const secretValue = "ghp_a_token_that_would_be_worth_stealing"

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type stubEnvironments struct {
	environment  domain.Environment
	approvedHost string
}

func (s *stubEnvironments) Create(
	_ context.Context, _ uuid.UUID, newEnvironment domain.Environment, _ string, at time.Time,
) (domain.Environment, error) {
	newEnvironment.CreatedAt = at
	s.environment = newEnvironment
	return newEnvironment, nil
}

func (s *stubEnvironments) ByID(context.Context, uuid.UUID, uuid.UUID) (domain.Environment, error) {
	return s.environment, nil
}

func (s *stubEnvironments) InProject(context.Context, uuid.UUID, uuid.UUID) ([]domain.Environment, error) {
	return []domain.Environment{s.environment}, nil
}

func (s *stubEnvironments) Save(
	_ context.Context, _ uuid.UUID, updated domain.Environment, _ bool,
) (domain.Environment, error) {
	s.environment = updated
	return updated, nil
}

func (s *stubEnvironments) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (s *stubEnvironments) RecordProofOK(
	_ context.Context, _, _ uuid.UUID,
) (domain.Environment, error) {
	s.environment.VerificationStatus = domain.VerificationProofOK
	return s.environment, nil
}

func (s *stubEnvironments) RecordProofFailed(
	_ context.Context, _, _ uuid.UUID,
) (domain.Environment, error) {
	s.environment.VerificationStatus = domain.VerificationFailed
	return s.environment, nil
}

func (s *stubEnvironments) Approve(
	_ context.Context, _, _ uuid.UUID, by domain.Identity, at time.Time, host string,
	_ domain.AuditEntry,
) (domain.Environment, error) {
	s.environment.VerificationStatus = domain.VerificationVerified
	s.environment.VerifiedAt, s.environment.VerifiedBy = &at, &by.UserID
	s.approvedHost = host
	return s.environment, nil
}

// stubProof answers whatever the test set up. What a real one refuses is tested
// in adapter/verifier.
type stubProof struct{ err error }

func (s stubProof) ReadProof(context.Context, string, string) error { return s.err }

// stubSecrets accepts whatever it is handed. What it will not do is hand a value
// back, because the port it satisfies has no method that could.
type stubSecrets struct{ values map[string]string }

func (s *stubSecrets) Keys(_ context.Context, _, environmentID uuid.UUID) ([]domain.Secret, error) {
	out := make([]domain.Secret, 0, len(s.values))
	for key := range s.values {
		out = append(out, domain.Secret{
			ID:            uuid.New(),
			EnvironmentID: environmentID,
			Key:           key,
			CreatedAt:     time.Unix(0, 0).UTC(),
			UpdatedAt:     time.Unix(0, 0).UTC(),
		})
	}
	return out, nil
}

func (s *stubSecrets) Put(
	_ context.Context, _, environmentID uuid.UUID, key, value string, at time.Time,
) (domain.Secret, error) {
	s.values[key] = value
	return domain.Secret{
		ID:            uuid.New(),
		EnvironmentID: environmentID,
		Key:           key,
		CreatedAt:     at,
		UpdatedAt:     at,
	}, nil
}

func (s *stubSecrets) Remove(_ context.Context, _, _ uuid.UUID, key string) error {
	delete(s.values, key)
	return nil
}

type stubTokens struct{}

func (stubTokens) New() (string, error) { return "challenge-token", nil }

type permissive struct{}

func (permissive) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return true, nil
}

type environmentFixture struct {
	handler      *handler.Environment
	secrets      *stubSecrets
	environments *stubEnvironments
	projectID    uuid.UUID
	id           uuid.UUID
}

func newEnvironmentFixture(t *testing.T) environmentFixture {
	t.Helper()

	return newEnvironmentFixtureWithProof(t, nil)
}

func newEnvironmentFixtureWithProof(t *testing.T, proofErr error) environmentFixture {
	t.Helper()

	projectID, id := uuid.New(), uuid.New()
	environments := &stubEnvironments{environment: domain.Environment{
		ID:                 id,
		ProjectID:          projectID,
		Name:               "staging",
		BaseURL:            "https://staging.example.test",
		VerificationStatus: domain.VerificationPending,
		VerificationToken:  "a-challenge-token",
		MaxVUs:             10,
		MaxDurationS:       60,
	}}
	secrets := &stubSecrets{values: map[string]string{}}

	return environmentFixture{
		handler: handler.NewEnvironment(environment.NewService(
			environments, secrets, stubTokens{}, stubProof{err: proofErr},
			stubJournal{}, permissive{},
			fixedClock{at: time.Unix(0, 0).UTC()},
			discard(),
		), discard()),
		secrets:      secrets,
		environments: environments,
		projectID:    projectID,
		id:           id,
	}
}

// stubJournal stands in for the audit log. What the journal records is tested
// in the usecase; here it only has to exist.
type stubJournal struct{}

func (stubJournal) Record(context.Context, domain.AuditEntry) error { return nil }

// request builds a signed-in request with the path values the router would have
// filled in, since the handler reads them rather than parsing the path itself.
func (f environmentFixture) request(method, body string, values map[string]string) *http.Request {
	req := httptest.NewRequest(method, "http://api.test/", strings.NewReader(body))
	for name, value := range values {
		req.SetPathValue(name, value)
	}

	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
	return req.WithContext(session.WithIdentity(req.Context(), who))
}

func (f environmentFixture) secretPath() map[string]string {
	return map[string]string{
		"projectId":     f.projectID.String(),
		"environmentId": f.id.String(),
		"key":           "API_TOKEN",
	}
}

// The ticket's acceptance criterion at the layer that answers the browser: the
// response to setting a secret must not carry its value. The service returns a
// type with no field for one; this is what checks that the JSON agrees.
func TestSettingASecretDoesNotEchoItsValue(t *testing.T) {
	f := newEnvironmentFixture(t)

	rec := httptest.NewRecorder()
	f.handler.SetSecret(rec, f.request(http.MethodPut, `{"value":"`+secretValue+`"}`, f.secretPath()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secretValue) {
		t.Fatalf("the response carries the value: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "ghp_") {
		t.Fatalf("the response carries part of the value: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "API_TOKEN") {
		t.Fatalf("the response does not even name the key: %s", rec.Body.String())
	}
	// It did reach the store, so this test is about the response rather than
	// about nothing having happened.
	if f.secrets.values["API_TOKEN"] != secretValue {
		t.Fatal("the value never reached the repository")
	}
}

func TestListingSecretsReturnsKeysAndNoValues(t *testing.T) {
	f := newEnvironmentFixture(t)

	set := httptest.NewRecorder()
	f.handler.SetSecret(set, f.request(http.MethodPut, `{"value":"`+secretValue+`"}`, f.secretPath()))
	if set.Code != http.StatusOK {
		t.Fatalf("setting the secret answered %d: %s", set.Code, set.Body.String())
	}

	rec := httptest.NewRecorder()
	f.handler.Secrets(rec, f.request(http.MethodGet, "", f.secretPath()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secretValue) || strings.Contains(rec.Body.String(), "ghp_") {
		t.Fatalf("the list carries a value: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "API_TOKEN") {
		t.Fatalf("the list does not name the key: %s", rec.Body.String())
	}
}

// A refused key answers 400 with something to act on, and the message names the
// key rather than the value — this is the moment a well-meant error string leaks
// a credential.
func TestARefusedKeyAnswersWithoutTheValue(t *testing.T) {
	f := newEnvironmentFixture(t)

	path := f.secretPath()
	path["key"] = "K6_VUS"

	rec := httptest.NewRecorder()
	f.handler.SetSecret(rec, f.request(http.MethodPut, `{"value":"`+secretValue+`"}`, path))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "ghp_") {
		t.Fatalf("the refusal carries the value: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "K6_VUS") {
		t.Fatalf("the refusal does not say which key: %s", rec.Body.String())
	}
	if _, stored := f.secrets.values["K6_VUS"]; stored {
		t.Fatal("the secret was stored anyway")
	}
}

func TestAWindowTravelsAsATimeOfDay(t *testing.T) {
	f := newEnvironmentFixture(t)

	body := `{"name":"staging","base_url":"https://staging.example.test",
		"max_vus":10,"max_duration_s":60,
		"allowed_window":{"start":"22:00","end":"06:00","timezone":"Europe/Paris"}}`

	rec := httptest.NewRecorder()
	f.handler.Create(rec, f.request(http.MethodPost, body,
		map[string]string{"projectId": f.projectID.String()}))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"start":"22:00"`, `"end":"06:00"`, `"timezone":"Europe/Paris"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("the response does not carry %s: %s", want, rec.Body.String())
		}
	}
}

func TestATimeOfDayBallastCannotReadIsRefused(t *testing.T) {
	f := newEnvironmentFixture(t)

	for _, clock := range []string{"9h", "25:00", "22:70", "", "22:00:00"} {
		body := `{"name":"staging","base_url":"https://staging.example.test",
			"max_vus":10,"max_duration_s":60,
			"allowed_window":{"start":"` + clock + `","end":"06:00","timezone":"UTC"}}`

		rec := httptest.NewRecorder()
		f.handler.Create(rec, f.request(http.MethodPost, body,
			map[string]string{"projectId": f.projectID.String()}))

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%q answered %d, want 400: %s", clock, rec.Code, rec.Body.String())
		}
	}
}

// A new environment is pending whatever the request says, because the one control
// that keeps Ballast from being aimed at somebody else's servers is downstream of
// this.
func TestADeclaredEnvironmentIsNeverCreatedVerified(t *testing.T) {
	f := newEnvironmentFixture(t)

	body := `{"name":"staging","base_url":"https://staging.example.test",
		"max_vus":10,"max_duration_s":60,"verification_status":"verified"}`

	rec := httptest.NewRecorder()
	f.handler.Create(rec, f.request(http.MethodPost, body,
		map[string]string{"projectId": f.projectID.String()}))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"verification_status":"pending"`) {
		t.Fatalf("a request asked for verified and got: %s", rec.Body.String())
	}
}

func (f environmentFixture) environmentPath() map[string]string {
	return map[string]string{
		"projectId":     f.projectID.String(),
		"environmentId": f.id.String(),
	}
}

func TestTheChallengeSaysWhatToServeAndWhere(t *testing.T) {
	f := newEnvironmentFixture(t)

	rec := httptest.NewRecorder()
	f.handler.Challenge(rec, f.request(http.MethodGet, "", f.environmentPath()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{
		`"token":"a-challenge-token"`,
		`"url":"https://staging.example.test/.well-known/ballast-a-challenge-token"`,
		`"verification_status":"pending"`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the challenge does not carry %s: %s", want, rec.Body.String())
		}
	}
}

// A served challenge leaves the target proven and not approved. The response has
// to say so plainly, or somebody reads a 200 as permission to run.
func TestAServedChallengeAnswersProvenAndNotVerified(t *testing.T) {
	f := newEnvironmentFixture(t)

	rec := httptest.NewRecorder()
	f.handler.Verify(rec, f.request(http.MethodPost, "", f.environmentPath()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"verification_status":"proof_ok"`) {
		t.Fatalf("the response is %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"verified"`) {
		t.Fatalf("a served challenge answered verified: %s", rec.Body.String())
	}
}

// The reason travels to the caller: this is the whole difference between an
// endpoint somebody can use and one that says no.
func TestAFailedVerificationAnswers422WithTheReason(t *testing.T) {
	f := newEnvironmentFixtureWithProof(t,
		errors.New("the target does not resolve to a publicly routable address"))

	rec := httptest.NewRecorder()
	f.handler.Verify(rec, f.request(http.MethodPost, "", f.environmentPath()))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "publicly routable") {
		t.Fatalf("the refusal lost its reason: %s", rec.Body.String())
	}
	// The challenge token has no business in an error body either.
	if strings.Contains(rec.Body.String(), "a-challenge-token") {
		t.Fatalf("the refusal carries the token: %s", rec.Body.String())
	}
}

func TestApprovingWithoutAProofAnswers409(t *testing.T) {
	f := newEnvironmentFixture(t)

	rec := httptest.NewRecorder()
	f.handler.Approve(rec, f.request(http.MethodPost, "", f.environmentPath()))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if f.environments.approvedHost != "" {
		t.Fatalf("the host %q was allowed anyway", f.environments.approvedHost)
	}
}

func TestApprovingAProvenTargetAnswersVerifiedAndAllowsItsHost(t *testing.T) {
	f := newEnvironmentFixture(t)
	f.environments.environment.VerificationStatus = domain.VerificationProofOK

	rec := httptest.NewRecorder()
	f.handler.Approve(rec, f.request(http.MethodPost, "", f.environmentPath()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"verification_status":"verified"`) {
		t.Fatalf("the response is %s", rec.Body.String())
	}
	if f.environments.approvedHost != "staging.example.test" {
		t.Fatalf("the project allows %q", f.environments.approvedHost)
	}
}

// The environment's own response must not carry the challenge token: it is read
// through the verification endpoint, which asks for a different permission.
func TestTheEnvironmentResponseDoesNotCarryTheToken(t *testing.T) {
	f := newEnvironmentFixture(t)

	rec := httptest.NewRecorder()
	f.handler.ByID(rec, f.request(http.MethodGet, "", f.environmentPath()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "a-challenge-token") {
		t.Fatalf("the environment response carries the token: %s", rec.Body.String())
	}
}

// "Leave the window alone" and "there is no window any more" are different
// instructions, spelled as an absent field and as null. Go decodes a JSON null
// into a nil pointer, which would read as absent: the window could never be
// removed. This is the test that keeps the two apart.
func TestAnUpdateTellsAnAbsentWindowFromANullOne(t *testing.T) {
	path := func(f environmentFixture) map[string]string {
		return map[string]string{"projectId": f.projectID.String(), "environmentId": f.id.String()}
	}
	windowed := func(t *testing.T) environmentFixture {
		f := newEnvironmentFixture(t)
		f.environments.environment.Window = &domain.AllowedWindow{StartMinute: 22 * 60, EndMinute: 6 * 60, Zone: "UTC"}
		return f
	}

	t.Run("absent keeps it", func(t *testing.T) {
		f := windowed(t)
		rec := httptest.NewRecorder()
		f.handler.Update(rec, f.request(http.MethodPatch, `{"name":"renamed"}`, path(f)))

		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if f.environments.environment.Window == nil {
			t.Fatal("an update that did not mention the window removed it")
		}
	})

	t.Run("null removes it", func(t *testing.T) {
		f := windowed(t)
		rec := httptest.NewRecorder()
		f.handler.Update(rec, f.request(http.MethodPatch, `{"allowed_window":null}`, path(f)))

		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if f.environments.environment.Window != nil {
			t.Fatalf("allowed_window: null left the window in place: %+v", *f.environments.environment.Window)
		}
		if strings.Contains(rec.Body.String(), "allowed_window") {
			t.Fatalf("the response still carries a window: %s", rec.Body.String())
		}
	})

	t.Run("a window replaces it", func(t *testing.T) {
		f := windowed(t)
		rec := httptest.NewRecorder()
		f.handler.Update(rec, f.request(http.MethodPatch,
			`{"allowed_window":{"start":"01:00","end":"05:00","timezone":"Europe/Paris"}}`, path(f)))

		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if got := f.environments.environment.Window; got == nil || got.StartMinute != 60 || got.Zone != "Europe/Paris" {
			t.Fatalf("the window is %+v, want 01:00 to 05:00 in Europe/Paris", got)
		}
	})
}
