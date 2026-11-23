//go:build integration

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
	"github.com/youssefmahersi/ballast/internal/usecase/webhook"
)

// A tool that is neither GitHub nor GitLab starts a run by signing a request, on the real
// stack: the real router, the real services and a real database. The webhook is made
// through the API by a project administrator, as the screen does, and used the way a
// tool uses it: three headers and a body, and no session.
//
// What these tests hold in place, end to end:
//
//   - a signed request starts a run in its project, and the run and the journal both say
//     the webhook did it, and no person or token;
//   - a wrong signature, a stale timestamp, a replay, an organization or project that has
//     no webhook are refused with the same bytes, and create no run;
//   - every guardrail a person meets stops a webhook too, and an administrator's rotation
//     or removal takes effect on the next request;
//   - the secret is in the response that makes it and in no other, and neither a token
//     nor a webhook can manage one.
//
// No container is started: nothing here is ever executed.

type runsLauncher struct{ runs *run.Service }

func (l runsLauncher) Enqueue(ctx context.Context, launch webhook.Launch) (domain.Run, error) {
	return l.runs.Enqueue(ctx, run.EnqueueCommand{
		Actor: launch.Actor, ScenarioID: launch.ScenarioID, EnvironmentID: launch.EnvironmentID,
		Profile: launch.Profile, Params: launch.Params, CI: launch.CI,
	})
}

type madeWebhook struct {
	secret string
	path   string
}

func (w ciWorld) newWebhook(t *testing.T) madeWebhook {
	t.Helper()

	res, raw := w.call(t, w.asAdmin(), http.MethodPost, "/projects/"+w.a.projectID.String()+"/webhook", "")
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("make a webhook: %d: %s", res.StatusCode, raw)
	}
	var made struct {
		Webhook struct {
			Path string `json:"path"`
		} `json:"webhook"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(raw, &made); err != nil {
		t.Fatalf("read the webhook: %v: %s", err, raw)
	}
	return madeWebhook{secret: made.Secret, path: made.Webhook.Path}
}

// post is what a tool sends: the three headers, and the body as bytes.
func (w ciWorld) post(t *testing.T, path, secret, delivery string, at time.Time, body string) (*http.Response, []byte) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, w.api.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	stamp := at.Unix()
	req.Header.Set("X-Ballast-Timestamp", fmt.Sprint(stamp))
	req.Header.Set("X-Ballast-Delivery", delivery)
	req.Header.Set("X-Ballast-Signature", domain.SignWebhook(secret, stamp, delivery, []byte(body)))

	res, err := w.api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var raw bytes.Buffer
	_, _ = raw.ReadFrom(res.Body)
	return res, raw.Bytes()
}

func delivery() string { return "delivery-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20] }

func (w ciWorld) runs(ctx context.Context, t *testing.T) int {
	return w.count(ctx, t, "SELECT count(*) FROM run WHERE org_id = $1", w.org)
}

// --- A tool launches a run ---------------------------------------------------------------

func TestAToolQueuesARunWithAWebhookAndEveryRecordSaysSo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	hook := w.newWebhook(t)

	res, raw := w.post(t, hook.path, hook.secret, delivery(), time.Now(), w.runBody(w.a))
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("a signed request: %d: %s", res.StatusCode, raw)
	}
	var queued struct{ ID string }
	if err := json.Unmarshal(raw, &queued); err != nil || queued.ID == "" {
		t.Fatalf("the response is not a queued run: %v: %s", err, raw)
	}

	var person, token, webhookID *uuid.UUID
	var status string
	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT triggered_by, triggered_by_token, triggered_by_webhook, status FROM run WHERE id = $1`,
			queued.ID).Scan(&person, &token, &webhookID, &status)
	})
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	if webhookID == nil || person != nil || token != nil || status != "queued" {
		t.Errorf("the run says person %v, token %v, webhook %v, status %s; want the webhook alone, queued", person, token, webhookID, status)
	}

	var actor, actorToken, actorWebhook *uuid.UUID
	err = w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT actor_id, actor_token_id, actor_webhook_id FROM audit_log
			WHERE org_id = $1 AND action = 'run.queued'`, w.org).Scan(&actor, &actorToken, &actorWebhook)
	})
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	if actorWebhook == nil || *actorWebhook != *webhookID || actor != nil || actorToken != nil {
		t.Errorf("the entry names person %v, token %v, webhook %v", actor, actorToken, actorWebhook)
	}

	// An administrator reads the same thing from the API, said as the webhook's.
	res, raw = w.call(t, w.asAdmin(), http.MethodGet, "/projects/"+w.a.projectID.String()+"/runs", "")
	if res.StatusCode != http.StatusOK || !strings.Contains(string(raw), "the project's webhook") {
		t.Errorf("the list of runs does not say the webhook launched it: %d: %s", res.StatusCode, raw)
	}
}

func TestTheCreationOfAWebhookIsInTheJournalWithoutItsSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	hook := w.newWebhook(t)

	var by *uuid.UUID
	var metadata string
	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT actor_id, metadata::text FROM audit_log
			WHERE org_id = $1 AND action = 'webhook.created'`, w.org).Scan(&by, &metadata)
	})
	if err != nil {
		t.Fatalf("read the entry: %v", err)
	}
	if by == nil || *by != w.adminID {
		t.Errorf("the entry names %v, want the administrator", by)
	}
	if strings.Contains(metadata, hook.secret) || strings.Contains(metadata, "blw_") {
		t.Errorf("the entry holds a secret: %s", metadata)
	}

	res, raw := w.call(t, w.asAdmin(), http.MethodGet, "/projects/"+w.a.projectID.String()+"/webhook", "")
	if res.StatusCode != http.StatusOK || strings.Contains(string(raw), hook.secret) || strings.Contains(string(raw), "blw_") {
		t.Errorf("reading the webhook: %d, and it shows a secret: %s", res.StatusCode, raw)
	}
	res, raw = w.call(t, w.asAdmin(), http.MethodGet, "/projects/"+w.a.projectID.String()+"/audit", "")
	if res.StatusCode != http.StatusOK || strings.Contains(string(raw), hook.secret) {
		t.Errorf("the journal as the API serves it: %d: shows a secret", res.StatusCode)
	}
}

// --- Refusals ----------------------------------------------------------------------------

func TestEveryRefusalBeforeTheSignatureHoldsIsTheSameBytesAndStartsNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	hook := w.newWebhook(t)
	body := w.runBody(w.a)
	now := time.Now()

	// A first request that is served, so that it can be sent again.
	d := delivery()
	if res, raw := w.post(t, hook.path, hook.secret, d, now, body); res.StatusCode != http.StatusAccepted {
		t.Fatalf("the first request: %d: %s", res.StatusCode, raw)
	}
	runsBefore := w.runs(ctx, t)

	otherOrg := "/webhooks/" + uuid.NewString() + "/" + w.a.projectID.String()
	projectWithNone := "/webhooks/" + w.org.String() + "/" + w.b.projectID.String()

	type attempt struct {
		name string
		res  *http.Response
		raw  []byte
	}
	var attempts []attempt
	add := func(name string, res *http.Response, raw []byte) {
		attempts = append(attempts, attempt{name, res, raw})
	}

	res, raw := w.post(t, hook.path, "blw_not-the-secret-0123456789abcdefghijklmnopq", delivery(), now, body)
	add("a wrong secret", res, raw)
	res, raw = w.post(t, hook.path, hook.secret, delivery(), now.Add(-time.Hour), body)
	add("a stale timestamp", res, raw)
	res, raw = w.post(t, hook.path, hook.secret, d, now, body)
	add("a replay", res, raw)
	res, raw = w.post(t, otherOrg, hook.secret, delivery(), now, body)
	add("an organization with no such webhook", res, raw)
	res, raw = w.post(t, projectWithNone, hook.secret, delivery(), now, body)
	add("a project with no webhook", res, raw)
	res, raw = w.post(t, "/webhooks/not-an-id/"+w.a.projectID.String(), hook.secret, delivery(), now, body)
	add("an address that is not ids", res, raw)
	res, raw = w.post(t, hook.path, hook.secret, delivery(), now, strings.Repeat("a", domain.MaxWebhookBody+1))
	add("a body that is too large", res, raw)

	req, _ := http.NewRequest(http.MethodPost, w.api.URL+hook.path, strings.NewReader(body))
	bare, err := w.api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var bareBody bytes.Buffer
	_, _ = bareBody.ReadFrom(bare.Body)
	_ = bare.Body.Close()
	add("no headers at all", bare, bareBody.Bytes())

	for _, a := range attempts {
		if a.res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401: %s", a.name, a.res.StatusCode, a.raw)
		}
		if !bytes.Equal(a.raw, attempts[0].raw) {
			t.Errorf("%s: the bytes differ from the first refusal's: %q vs %q", a.name, a.raw, attempts[0].raw)
		}
		if a.res.Header.Get("Retry-After") != "" || a.res.Header.Get("Set-Cookie") != "" {
			t.Errorf("%s: carries a header that says more", a.name)
		}
	}
	if after := w.runs(ctx, t); after != runsBefore {
		t.Errorf("%d runs after the refusals, %d before: a refused request started one", after, runsBefore)
	}
}

func TestRotatingTakesEffectAtOnceAndDeletingEndsTheWebhook(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	hook := w.newWebhook(t)
	body := w.runBody(w.a)

	res, raw := w.call(t, w.asAdmin(), http.MethodPost, "/projects/"+w.a.projectID.String()+"/webhook/rotate", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rotate: %d: %s", res.StatusCode, raw)
	}
	var rotated struct {
		Webhook struct {
			Path string `json:"path"`
		} `json:"webhook"`
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(raw, &rotated)
	if rotated.Secret == "" || rotated.Secret == hook.secret || rotated.Webhook.Path != hook.path {
		t.Fatalf("rotation gave secret %q at %q: the address must stay, the secret must change", rotated.Secret, rotated.Webhook.Path)
	}

	if res, _ := w.post(t, hook.path, hook.secret, delivery(), time.Now(), body); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("the old secret: %d, want 401", res.StatusCode)
	}
	if res, raw := w.post(t, hook.path, rotated.Secret, delivery(), time.Now(), body); res.StatusCode != http.StatusAccepted {
		t.Errorf("the new secret: %d: %s", res.StatusCode, raw)
	}

	if res, raw := w.call(t, w.asAdmin(), http.MethodDelete, "/projects/"+w.a.projectID.String()+"/webhook", ""); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d: %s", res.StatusCode, raw)
	}
	if res, _ := w.post(t, hook.path, rotated.Secret, delivery(), time.Now(), body); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("after the deletion: %d, want 401", res.StatusCode)
	}
	if n := w.count(ctx, t, "SELECT count(*) FROM audit_log WHERE org_id = $1 AND action IN ('webhook.rotated','webhook.deleted')", w.org); n != 2 {
		t.Errorf("%d entries for the rotation and the deletion, want 2", n)
	}
}

// --- The guardrails ----------------------------------------------------------------------

func TestAWebhookCannotAimAtAnUnverifiedTargetAndTheRefusalNamesIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	hook := w.newWebhook(t)

	res, raw := w.post(t, hook.path, hook.secret, delivery(), time.Now(), w.runBodyFor(w.a.scenarioID, w.aUnverified))
	if res.StatusCode != http.StatusForbidden || !strings.Contains(string(raw), "has not been verified") {
		t.Fatalf("an unverified target: %d: %s", res.StatusCode, raw)
	}
	if w.runs(ctx, t) != 0 {
		t.Error("a run was written for an unverified target")
	}
	if n := w.count(ctx, t, `SELECT count(*) FROM audit_log
		WHERE org_id = $1 AND action = 'run.refused' AND outcome = 'refused' AND actor_webhook_id IS NOT NULL
		  AND actor_id IS NULL AND actor_token_id IS NULL`, w.org); n != 1 {
		t.Errorf("%d refusal entries naming the webhook, want 1", n)
	}
}

func TestAWebhookIsOneProjectsAndCannotRunAScenarioOfAnother(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	hook := w.newWebhook(t)

	res, raw := w.post(t, hook.path, hook.secret, delivery(), time.Now(), w.runBody(w.b))
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a scenario of another project: %d: %s", res.StatusCode, raw)
	}
	if w.runs(ctx, t) != 0 {
		t.Error("a run was written in another project")
	}
}

func TestAWebhookIsHeldToTheHourlyLimitAndToldWhenItOpens(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	hook := w.newWebhook(t)

	// The webhook has launched the most it may, the oldest ten minutes ago.
	var id uuid.UUID
	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT id FROM project_webhook WHERE org_id = $1", w.org).Scan(&id)
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < domain.MaxRunsPerUserPerWindow; i++ {
		age := time.Duration(10+i) * time.Second
		if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO run
				(org_id, project_id, scenario_id, environment_id, scenario_version, profile, status,
				 triggered_by_webhook, created_at, ended_at)
				VALUES ($1, $2, $3, $4, 1, 'smoke', 'passed', $5, now() - $6::interval, now())`,
				w.org, w.a.projectID, w.a.scenarioID, w.a.environmentID, id, fmt.Sprintf("%d seconds", int(age.Seconds())+590))
			return err
		}); err != nil {
			t.Fatalf("stage run %d: %v", i, err)
		}
	}

	res, raw := w.post(t, hook.path, hook.secret, delivery(), time.Now(), w.runBody(w.a))

	if res.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(raw), "this webhook has triggered") {
		t.Fatalf("past the limit: %d: %s", res.StatusCode, raw)
	}
	if res.Header.Get("Retry-After") == "" {
		t.Error("no Retry-After for a window that has a known end")
	}
}

// --- Who may manage ------------------------------------------------------------------------

func TestNeitherATokenNorAWebhookNorAStrangerManagesAWebhook(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	hook := w.newWebhook(t)
	token := w.newToken(t, "ci-main")

	for name, who := range map[string]as{
		"a token":              asPipeline(token.secret),
		"the webhook's secret": asPipeline(hook.secret),
		"nobody":               func(*http.Request) {},
	} {
		for _, call := range []struct{ method, path string }{
			{http.MethodGet, "/projects/" + w.a.projectID.String() + "/webhook"},
			{http.MethodPost, "/projects/" + w.a.projectID.String() + "/webhook/rotate"},
			{http.MethodDelete, "/projects/" + w.a.projectID.String() + "/webhook"},
		} {
			res, raw := w.call(t, who, call.method, call.path, "")
			if res.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s: %s %s answered %d: %s", name, call.method, call.path, res.StatusCode, raw)
			}
		}
	}
	// And the webhook is still there, with its secret unchanged.
	if res, raw := w.post(t, hook.path, hook.secret, delivery(), time.Now(), w.runBody(w.a)); res.StatusCode != http.StatusAccepted {
		t.Errorf("the webhook no longer works: %d: %s", res.StatusCode, raw)
	}
}
