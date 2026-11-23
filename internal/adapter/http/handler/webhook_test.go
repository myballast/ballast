package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/http/handler"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
	"github.com/youssefmahersi/ballast/internal/usecase/webhook"
)

// The webhook has two faces. The one an administrator uses is behind a session and
// manages a secret; the one a tool uses has no session at all, and what a caller can see
// of it is what these tests hold in place:
//
//   - every way of being refused before the signature holds looks the same from outside:
//     the same status and the same bytes, whatever failed, and nothing in them that was
//     sent;
//   - what happens after, the same run service answers, in the words and statuses of
//     POST /runs;
//   - the secret is in one response and in no other, and that response is not cached;
//   - an installation that does not serve webhooks answers as one.
//
// Written before the code, which is why they do not compile yet.

var (
	hookNow    = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	hookSecret = "blw_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
)

type hookClock struct{}

func (hookClock) Now() time.Time { return hookNow }

type hookStore struct {
	hook    *domain.Webhook
	secret  string
	seen    map[string]bool
	deleted bool
}

func (s *hookStore) SecretOf(_ context.Context, _, _ uuid.UUID) (domain.Webhook, string, bool, error) {
	if s.hook == nil {
		return domain.Webhook{}, "", false, nil
	}
	return *s.hook, s.secret, true, nil
}

func (s *hookStore) Remember(_ context.Context, _, _ uuid.UUID, id string, _, _ time.Time) (bool, error) {
	if s.seen[id] {
		return false, nil
	}
	s.seen[id] = true
	return true, nil
}

func (s *hookStore) ByProject(context.Context, uuid.UUID, uuid.UUID) (domain.Webhook, bool, error) {
	if s.hook == nil {
		return domain.Webhook{}, false, nil
	}
	return *s.hook, true, nil
}

func (s *hookStore) Create(_ context.Context, hook domain.Webhook, secret string, _ domain.AuditEntry) error {
	if s.hook != nil {
		return domain.ErrWebhookExists
	}
	s.hook, s.secret = &hook, secret
	return nil
}

func (s *hookStore) Rotate(_ context.Context, hook domain.Webhook, secret string, _ time.Time, _ domain.AuditEntry) error {
	s.hook, s.secret = &hook, secret
	return nil
}

func (s *hookStore) Delete(context.Context, domain.Webhook, domain.AuditEntry) error {
	s.hook, s.deleted = nil, true
	return nil
}

type hookLauncher struct {
	err  error
	runs int
}

func (l *hookLauncher) Enqueue(context.Context, webhook.Launch) (domain.Run, error) {
	l.runs++
	return domain.Run{ID: uuid.New(), Status: domain.StatusQueued}, l.err
}

type allowManage struct{}

func (allowManage) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return true, nil
}

type hookRig struct {
	h        *handler.Webhook
	store    *hookStore
	launcher *hookLauncher
	org, prj uuid.UUID
}

func newHookRig(t *testing.T, serving bool) *hookRig {
	t.Helper()

	r := &hookRig{org: uuid.New(), prj: uuid.New(), launcher: &hookLauncher{}}
	hook := domain.Webhook{ID: uuid.New(), OrgID: r.org, ProjectID: r.prj, CreatedAt: hookNow.Add(-time.Hour)}
	r.store = &hookStore{hook: &hook, secret: hookSecret, seen: map[string]bool{}}

	if !serving {
		r.h = handler.NewWebhook(nil, nil, discard())
		return r
	}
	stream := make([]byte, 512)
	for i := range stream {
		stream[i] = byte(i)
	}
	manager := webhook.NewManager(r.store, allowManage{}, hookClock{}, bytes.NewReader(stream), discard())
	receiver := webhook.NewReceiver(r.store, r.store, r.launcher, hookClock{}, discard())
	r.h = handler.NewWebhook(manager, receiver, discard())
	return r
}

func (r *hookRig) body() string {
	return fmt.Sprintf(`{"scenario_id":%q,"environment_id":%q,"profile":"smoke"}`, uuid.New(), uuid.New())
}

// signed builds a request as a tool would, with no session.
func (r *hookRig) signed(body, delivery, secret string, at time.Time) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.SetPathValue("orgId", r.org.String())
	request.SetPathValue("projectId", r.prj.String())
	stamp := at.Unix()
	request.Header.Set("X-Ballast-Timestamp", fmt.Sprint(stamp))
	request.Header.Set("X-Ballast-Delivery", delivery)
	request.Header.Set("X-Ballast-Signature", domain.SignWebhook(secret, stamp, delivery, []byte(body)))
	return request
}

// --- Receiving ------------------------------------------------------------------------------

func TestASignedRequestAnswers202WithTheRun(t *testing.T) {
	r := newHookRig(t, true)

	recorder := httptest.NewRecorder()
	r.h.Receive(recorder, r.signed(r.body(), "delivery-0123456789", hookSecret, hookNow))

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
	var got dto.EnqueueRunResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil || got.Status != "queued" || got.ID == "" {
		t.Errorf("body = %s (%v)", recorder.Body, err)
	}
}

func TestEveryRefusalBeforeTheSignatureHoldsIsTheSameAnswerToTheByte(t *testing.T) {
	r := newHookRig(t, true)
	body := r.body()

	build := map[string]func() *http.Request{
		"a wrong secret": func() *http.Request { return r.signed(body, "delivery-aaaaaaaaaaaa", "blw_nope", hookNow) },
		"a stale timestamp": func() *http.Request {
			return r.signed(body, "delivery-bbbbbbbbbbbb", hookSecret, hookNow.Add(-time.Hour))
		},
		"no headers": func() *http.Request {
			q := r.signed(body, "delivery-cccccccccccc", hookSecret, hookNow)
			q.Header = http.Header{}
			return q
		},
		"a project with no webhook": func() *http.Request {
			q := r.signed(body, "delivery-dddddddddddd", hookSecret, hookNow)
			q.SetPathValue("projectId", uuid.NewString())
			r.store.hook = nil
			return q
		},
		"an address that is not ids": func() *http.Request {
			q := r.signed(body, "delivery-eeeeeeeeeeee", hookSecret, hookNow)
			q.SetPathValue("orgId", "not-a-uuid")
			return q
		},
		"a body that is too large": func() *http.Request {
			return r.signed(strings.Repeat("a", domain.MaxWebhookBody+10), "delivery-ffffffffffff", hookSecret, hookNow)
		},
	}

	var first []byte
	for name, make := range build {
		request := make()
		recorder := httptest.NewRecorder()
		r.h.Receive(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401: %s", name, recorder.Code, recorder.Body)
		}
		if first == nil {
			first = recorder.Body.Bytes()
		} else if !bytes.Equal(first, recorder.Body.Bytes()) {
			t.Errorf("%s: the body differs from another refusal's: %q vs %q", name, recorder.Body, first)
		}
		if recorder.Header().Get("Retry-After") != "" {
			t.Errorf("%s: a refusal carries a Retry-After", name)
		}
	}
	if r.launcher.runs != 0 {
		t.Error("a run was asked for by a request that was not authenticated")
	}
}

func TestAReplayAnswersTheSameAsAnythingElseUnauthorized(t *testing.T) {
	r := newHookRig(t, true)
	body := r.body()

	first := httptest.NewRecorder()
	r.h.Receive(first, r.signed(body, "delivery-0123456789", hookSecret, hookNow))
	replay := httptest.NewRecorder()
	r.h.Receive(replay, r.signed(body, "delivery-0123456789", hookSecret, hookNow))

	wrong := httptest.NewRecorder()
	r.h.Receive(wrong, r.signed(body, "delivery-9999999999", "blw_nope", hookNow))

	if first.Code != http.StatusAccepted || replay.Code != http.StatusUnauthorized {
		t.Fatalf("first %d, replay %d", first.Code, replay.Code)
	}
	if !bytes.Equal(replay.Body.Bytes(), wrong.Body.Bytes()) {
		t.Errorf("a replay (%q) is told apart from a wrong signature (%q)", replay.Body, wrong.Body)
	}
}

func TestTheRefusalCarriesNothingThatWasSent(t *testing.T) {
	r := newHookRig(t, true)
	request := r.signed(r.body(), "delivery-sentbythecaller", "blw_nope", hookNow)

	recorder := httptest.NewRecorder()
	r.h.Receive(recorder, request)

	for _, sent := range []string{"delivery-sentbythecaller", request.Header.Get("X-Ballast-Signature"), "blw_nope", r.org.String(), r.prj.String()} {
		if strings.Contains(recorder.Body.String(), sent) {
			t.Errorf("the refusal repeats %q", sent)
		}
	}
}

func TestABodyThatIsWrongAfterAGoodSignatureIs400WithTheReason(t *testing.T) {
	r := newHookRig(t, true)

	recorder := httptest.NewRecorder()
	r.h.Receive(recorder, r.signed(`{"scenario_id":"x"}`, "delivery-0123456789", hookSecret, hookNow))

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "scenario_id is not a uuid") {
		t.Errorf("status = %d: %s", recorder.Code, recorder.Body)
	}
}

func TestWhatTheRunServiceRefusesIsSaidAsPostRunsSaysIt(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		header string
	}{
		"a target that is not verified": {domain.ErrTargetNotVerified, http.StatusForbidden, ""},
		"no permission":                 {domain.ErrForbidden, http.StatusForbidden, ""},
		"the switch":                    {domain.ErrRunsSuspended, http.StatusServiceUnavailable, ""},
		"a window":                      {domain.ErrOutsideAllowedWindow, http.StatusForbidden, ""},
		"a limit with a wait":           {run.TooManyRuns{Reason: "this webhook has triggered 30 runs in the last 1h0m0s, which is the most allowed", RetryAfter: 90 * time.Second}, http.StatusTooManyRequests, "90"},
		"a limit with none":             {run.TooManyRuns{Reason: "this organization already has 20 runs waiting, which is the most it may queue"}, http.StatusTooManyRequests, ""},
		"an error nobody expected":      {fmt.Errorf("pq: relation secret_table does not exist"), http.StatusInternalServerError, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newHookRig(t, true)
			r.launcher.err = c.err

			recorder := httptest.NewRecorder()
			r.h.Receive(recorder, r.signed(r.body(), "delivery-0123456789", hookSecret, hookNow))

			if recorder.Code != c.status {
				t.Errorf("status = %d, want %d: %s", recorder.Code, c.status, recorder.Body)
			}
			if got := recorder.Header().Get("Retry-After"); got != c.header {
				t.Errorf("Retry-After = %q, want %q", got, c.header)
			}
			if strings.Contains(recorder.Body.String(), "secret_table") {
				t.Errorf("an error from below leaked: %s", recorder.Body)
			}
		})
	}
}

func TestAnInstallationThatDoesNotServeWebhooksSaysSo(t *testing.T) {
	r := newHookRig(t, false)

	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"receive": r.h.Receive, "get": r.h.Get, "create": r.h.Create, "rotate": r.h.Rotate, "delete": r.h.Delete,
	} {
		recorder := httptest.NewRecorder()
		call(recorder, r.signed(r.body(), "delivery-0123456789", hookSecret, hookNow))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, recorder.Code)
		}
	}
}

// --- Managing ---------------------------------------------------------------------------------

func manageRequest(method string, project uuid.UUID) *http.Request {
	request := httptest.NewRequest(method, "/", nil)
	request.SetPathValue("projectId", project.String())
	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
	return request.WithContext(session.WithIdentity(request.Context(), who))
}

func TestMakingAWebhookAnswers201WithTheSecretOnceAndNoCache(t *testing.T) {
	r := newHookRig(t, true)
	r.store.hook = nil
	project := uuid.New()

	recorder := httptest.NewRecorder()
	r.h.Create(recorder, manageRequest(http.MethodPost, project))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
	var created dto.CreatedWebhook
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Secret, domain.WebhookSecretPrefix) {
		t.Errorf("secret %q", created.Secret)
	}
	if created.Webhook.ProjectID != project.String() || !strings.HasPrefix(created.Webhook.Path, "/webhooks/") || !strings.HasSuffix(created.Webhook.Path, "/"+project.String()) {
		t.Errorf("webhook %+v: it has to say where to send requests", created.Webhook)
	}
}

func TestReadingTheWebhookShowsNoSecretAndAMissingOneIs404(t *testing.T) {
	r := newHookRig(t, true)

	recorder := httptest.NewRecorder()
	r.h.Get(recorder, manageRequest(http.MethodGet, r.prj))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
	for _, leak := range []string{hookSecret, "secret", "blw_"} {
		if strings.Contains(recorder.Body.String(), leak) {
			t.Errorf("reading the webhook shows %q", leak)
		}
	}

	r.store.hook = nil
	recorder = httptest.NewRecorder()
	r.h.Get(recorder, manageRequest(http.MethodGet, r.prj))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", recorder.Code)
	}
}

func TestMakingASecondWebhookIs409AndRotatingOrDeletingNothingIs404(t *testing.T) {
	r := newHookRig(t, true)

	recorder := httptest.NewRecorder()
	r.h.Create(recorder, manageRequest(http.MethodPost, r.prj))
	if recorder.Code != http.StatusConflict {
		t.Errorf("a second webhook: status = %d, want 409", recorder.Code)
	}

	r.store.hook = nil
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){"rotate": r.h.Rotate, "delete": r.h.Delete} {
		recorder = httptest.NewRecorder()
		call(recorder, manageRequest(http.MethodPost, r.prj))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s with none: status = %d, want 404", name, recorder.Code)
		}
	}
}

func TestRotatingAnswers200WithANewSecretAndDeletingAnswers204(t *testing.T) {
	r := newHookRig(t, true)

	recorder := httptest.NewRecorder()
	r.h.Rotate(recorder, manageRequest(http.MethodPost, r.prj))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("rotate: status = %d, cache %q", recorder.Code, recorder.Header().Get("Cache-Control"))
	}
	var rotated dto.CreatedWebhook
	_ = json.Unmarshal(recorder.Body.Bytes(), &rotated)
	if !strings.HasPrefix(rotated.Secret, domain.WebhookSecretPrefix) || rotated.Secret == hookSecret {
		t.Errorf("the secret is %q", rotated.Secret)
	}

	recorder = httptest.NewRecorder()
	r.h.Delete(recorder, manageRequest(http.MethodDelete, r.prj))
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Errorf("delete: status = %d, body %q", recorder.Code, recorder.Body)
	}
}

func TestAnAdministratorWhoMayNotManageIs403(t *testing.T) {
	r := newHookRig(t, true)
	manager := webhook.NewManager(r.store, refuseManage{}, hookClock{}, bytes.NewReader(make([]byte, 64)), discard())
	h := handler.NewWebhook(manager, nil, discard())

	recorder := httptest.NewRecorder()
	h.Create(recorder, manageRequest(http.MethodPost, uuid.New()))
	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", recorder.Code)
	}
}

type refuseManage struct{}

func (refuseManage) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return false, nil
}
