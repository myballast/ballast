package handler_test

import (
	"context"
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
	environment domain.Environment
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
	handler   *handler.Environment
	secrets   *stubSecrets
	projectID uuid.UUID
	id        uuid.UUID
}

func newEnvironmentFixture(t *testing.T) environmentFixture {
	t.Helper()

	projectID, id := uuid.New(), uuid.New()
	environments := &stubEnvironments{environment: domain.Environment{
		ID:                 id,
		ProjectID:          projectID,
		Name:               "staging",
		BaseURL:            "https://staging.example.test",
		VerificationStatus: domain.VerificationPending,
		MaxVUs:             10,
		MaxDurationS:       60,
	}}
	secrets := &stubSecrets{values: map[string]string{}}

	return environmentFixture{
		handler: handler.NewEnvironment(environment.NewService(
			environments, secrets, stubTokens{}, permissive{},
			fixedClock{at: time.Unix(0, 0).UTC()},
			discard(),
		), discard()),
		secrets:   secrets,
		projectID: projectID,
		id:        id,
	}
}

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
