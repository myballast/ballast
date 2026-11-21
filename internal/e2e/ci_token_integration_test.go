//go:build integration

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	httpadapter "github.com/youssefmahersi/ballast/internal/adapter/http"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/usecase/apitoken"
	"github.com/youssefmahersi/ballast/internal/usecase/audit"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/project"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// A pipeline launches a run with an API token, on the real stack: the real
// router, the real services and a real database. The token is created through the
// API by a project administrator, as it would be from the screen, and used the way
// a pipeline uses it — a bearer header and no cookie.
//
// What these tests hold in place, end to end:
//
//   - a token launches a run in its own project, and the run and the journal both
//     say a token did it;
//   - every guardrail a person meets stops a token too;
//   - a token is one project's, and a revoked or expired one stops at the next
//     request;
//   - a token reaches that one route and no other.
//
// No container is started: nothing here is ever executed.

type ciWorld struct {
	api     *httptest.Server
	db      *postgres.DB
	org     uuid.UUID
	admin   *http.Cookie
	adminID uuid.UUID
	a, b    tenant
	// aUnverified is an environment of project A whose ownership nobody proved.
	aUnverified uuid.UUID
}

func newCIWorld(ctx context.Context, t *testing.T) ciWorld {
	t.Helper()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, admin := uuid.New(), uuid.New()
	var a, b tenant
	aUnverified := uuid.New()

	err = db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO organization (id, name) VALUES ($1, 'ci token test')`, org); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO app_user (id, org_id, subject, email, name) VALUES ($1, $2, $3, $4, 'Ada Admin')`,
			admin, org, "subject-"+admin.String(), admin.String()+"@example.test"); err != nil {
			return err
		}

		for _, p := range []struct {
			into *tenant
			slug string
		}{{&a, "project-a"}, {&b, "project-b"}} {
			*p.into = tenant{projectID: uuid.New(), scenarioID: uuid.New(), environmentID: uuid.New()}

			if _, err := tx.Exec(ctx, `INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, $3, $3)`,
				p.into.projectID, org, p.slug); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO environment
				(id, org_id, project_id, name, base_url, verification_token,
				 verification_status, verified_at, verified_by, max_vus, max_duration_s)
				VALUES ($1, $2, $3, 'local', $4, 'token', 'verified', now(), $5, 5, 60)`,
				p.into.environmentID, org, p.into.projectID, "https://"+p.slug+".example.test", admin); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO scenario (id, org_id, project_id, name, script)
				VALUES ($1, $2, $3, 'health', 'export default function () {}')`,
				p.into.scenarioID, org, p.into.projectID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO scenario_version
				(org_id, scenario_id, version, script, default_profile)
				VALUES ($1, $2, 1, 'export default function () {}', 'smoke')`, org, p.into.scenarioID); err != nil {
				return err
			}
		}

		// A second environment of A that nobody proved they own.
		if _, err := tx.Exec(ctx, `INSERT INTO environment
			(id, org_id, project_id, name, base_url, verification_token,
			 verification_status, max_vus, max_duration_s)
			VALUES ($1, $2, $3, 'unproven', 'https://unproven.example.test', 'token', 'pending', 5, 60)`,
			aUnverified, org, a.projectID); err != nil {
			return err
		}

		// An administrator of A, and nothing in B.
		_, err := tx.Exec(ctx, `INSERT INTO membership (org_id, user_id, project_id, role)
			VALUES ($1, $2, $3, 'admin')`, org, admin, a.projectID)
		return err
	})
	if err != nil {
		t.Fatalf("seed the organization: %v", err)
	}

	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		_ = db.InTenantTx(cleanup, org, func(tx pgx.Tx) error {
			for _, table := range []string{
				"run_threshold", "run_metric", "run", "api_token", "threshold", "scenario_version", "scenario",
				"secret", "environment", "membership", "project_allowed_host",
				"project", "app_user", "organization",
			} {
				if _, err := tx.Exec(cleanup,
					"DELETE FROM "+pgx.Identifier{table}.Sanitize()+" WHERE org_id = $1", org); err != nil {
					return err
				}
			}
			return nil
		})
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	clock := infra.SystemClock{}

	memberships := postgres.NewMembershipRepository(db)
	apiTokens := postgres.NewAPITokenRepository(db)
	permissions := authz.NewService(memberships, log).WithTokens(apiTokens, clock)

	runs := run.NewService(
		postgres.NewRunRepository(db),
		postgres.NewScenarioRepository(db),
		postgres.NewEnvironmentRepository(db),
		postgres.NewEnvironmentRepository(db),
		neverReadsSecrets{t: t},
		neverRuns{t: t},
		nil,
		postgres.NewProjectRepository(db),
		postgres.NewLogRepository(db),
		postgres.NewAuditRepository(db),
		k6.NewOptionsBuilder(),
		k6.NewSummaryParser(),
		permissions,
		clock,
		run.Accepting(true),
		log,
	)

	projects := project.NewService(
		postgres.NewProjectRepository(db), memberships, permissions, postgres.NewAuditRepository(db), clock, log)

	sessions, err := session.NewManager(
		[]byte(strings.Repeat("a", session.MinSecretLength)), "https://ballast.test", time.Hour, clock)
	if err != nil {
		t.Fatalf("session manager: %v", err)
	}
	issued := httptest.NewRecorder()
	if err := sessions.Issue(issued, domain.Identity{UserID: admin, OrgID: org}); err != nil {
		t.Fatalf("issue a session: %v", err)
	}

	web, _ := url.Parse("http://web.test")
	api := httptest.NewServer(httpadapter.NewRouter(
		runs, projects, nil, nil, nil, nil,
		audit.NewService(postgres.NewAuditRepository(db), permissions, log),
		nil,
		apitoken.NewManager(apiTokens, permissions, clock, rand.Reader, log),
		auth.NewService(nil, nil, clock, log), permissions, sessions, db.Ping, web, log,
		httpadapter.WithTokens(apitoken.NewService(apiTokens, clock, log)),
	))
	t.Cleanup(api.Close)

	return ciWorld{
		api: api, db: db, org: org,
		admin: issued.Result().Cookies()[0], adminID: admin,
		a: a, b: b, aUnverified: aUnverified,
	}
}

// as is how a request proves who it is.
type as func(*http.Request)

func (w ciWorld) asAdmin() as { return func(r *http.Request) { r.AddCookie(w.admin) } }

// asPipeline is what a pipeline sends: a bearer header, and no cookie.
func asPipeline(secret string) as {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }
}

func (w ciWorld) call(t *testing.T, who as, method, path, body string) (*http.Response, []byte) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, w.api.URL+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	who(req)

	res, err := w.api.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, _ := io.ReadAll(res.Body)
	return res, raw
}

type createdToken struct {
	id     uuid.UUID
	secret string
}

// newToken creates a token for project A the way the screen does, through the API,
// as its administrator. The secret is the one thing the response carries once.
func (w ciWorld) newToken(t *testing.T, name string) createdToken {
	t.Helper()

	res, raw := w.call(t, w.asAdmin(), http.MethodPost,
		"/projects/"+w.a.projectID.String()+"/api-tokens", fmt.Sprintf(`{"name":%q}`, name))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create a token: %d: %s", res.StatusCode, raw)
	}

	var created struct {
		Token struct {
			ID string `json:"id"`
		} `json:"token"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("read the created token: %v: %s", err, raw)
	}
	return createdToken{id: uuid.MustParse(created.Token.ID), secret: created.Secret}
}

func (w ciWorld) runBody(tn tenant) string {
	return w.runBodyFor(tn.scenarioID, tn.environmentID)
}

func (w ciWorld) runBodyFor(scenario, environment uuid.UUID) string {
	return fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":1}}`,
		scenario, environment)
}

func (w ciWorld) count(ctx context.Context, t *testing.T, query string, args ...any) int {
	t.Helper()

	var n int
	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, args...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// --- A pipeline launches a run -------------------------------------------------------

func TestAPipelineQueuesARunWithATokenAndEveryRecordSaysSo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	res, raw := w.call(t, asPipeline(token.secret), http.MethodPost, "/runs", w.runBody(w.a))
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("a token queueing a run in its own project: %d: %s", res.StatusCode, raw)
	}
	var queued struct{ ID string }
	if err := json.Unmarshal(raw, &queued); err != nil || queued.ID == "" {
		t.Fatalf("the response is not a queued run: %v: %s", err, raw)
	}

	// The run names the token and no person.
	var triggeredBy, triggeredByToken *uuid.UUID
	var status string
	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT triggered_by, triggered_by_token, status FROM run WHERE id = $1`,
			queued.ID).Scan(&triggeredBy, &triggeredByToken, &status)
	})
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	if triggeredByToken == nil || *triggeredByToken != token.id || triggeredBy != nil || status != "queued" {
		t.Errorf("the run says person %v, token %v, status %s; want the token alone, queued", triggeredBy, triggeredByToken, status)
	}

	// So does the journal, in the database and as an administrator reads it.
	var actor, actorToken *uuid.UUID
	err = w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT actor_id, actor_token_id FROM audit_log
			WHERE org_id = $1 AND action = 'run.queued'`, w.org).Scan(&actor, &actorToken)
	})
	if err != nil {
		t.Fatalf("read the entry: %v", err)
	}
	if actorToken == nil || *actorToken != token.id || actor != nil {
		t.Errorf("the entry names person %v and token %v, want the token alone", actor, actorToken)
	}

	res, raw = w.call(t, w.asAdmin(), http.MethodGet, "/projects/"+w.a.projectID.String()+"/audit", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read the journal: %d: %s", res.StatusCode, raw)
	}
	var journal struct {
		Entries []struct {
			Action       string `json:"action"`
			ActorID      string `json:"actor_id"`
			ActorTokenID string `json:"actor_token_id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	var sawRun, sawCreation bool
	for _, e := range journal.Entries {
		switch e.Action {
		case "run.queued":
			sawRun = e.ActorTokenID == token.id.String() && e.ActorID == ""
		case "api_token.created":
			// Made by the administrator, who is a person.
			sawCreation = e.ActorID == w.adminID.String() && e.ActorTokenID == ""
		}
	}
	if !sawRun || !sawCreation {
		t.Errorf("the journal as read: run by the token %v, creation by the administrator %v: %s", sawRun, sawCreation, raw)
	}

	// And the page of the run says which pipeline, as a token's.
	res, raw = w.call(t, w.asAdmin(), http.MethodGet, "/runs/"+queued.ID, "")
	if res.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"triggered_by":"API token ci-main"`) {
		t.Errorf("the run as an administrator reads it: %d: %s", res.StatusCode, raw)
	}
}

// --- The guardrails stop a token as they stop a person -----------------------------------

func TestATokenCannotAimAtAnUnverifiedTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	res, raw := w.call(t, asPipeline(token.secret), http.MethodPost, "/runs", w.runBodyFor(w.a.scenarioID, w.aUnverified))

	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("an unverified target: %d, want 403: %s", res.StatusCode, raw)
	}
	if n := w.count(ctx, t, `SELECT count(*) FROM run WHERE org_id = $1`, w.org); n != 0 {
		t.Errorf("%d runs were written for a target nobody proved they own", n)
	}

	// The refusal is the entry a pilot is measured by, and it names the token.
	var outcome string
	var actor, actorToken *uuid.UUID
	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT outcome, actor_id, actor_token_id FROM audit_log
			WHERE org_id = $1 AND action = 'run.refused'`, w.org).Scan(&outcome, &actor, &actorToken)
	})
	if err != nil {
		t.Fatalf("read the entry: %v", err)
	}
	if outcome != "refused" || actorToken == nil || *actorToken != token.id || actor != nil {
		t.Errorf("the refusal is %s by person %v and token %v, want a refusal by the token alone", outcome, actor, actorToken)
	}
}

// A token holds the permission in its own project and nowhere else. The scenario
// and the environment are another project's, and the refusal is the same as a
// stranger's.
func TestATokenOfOneProjectCannotRunInAnother(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	res, raw := w.call(t, asPipeline(token.secret), http.MethodPost, "/runs", w.runBody(w.b))

	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("a token in another project: %d, want 403: %s", res.StatusCode, raw)
	}
	if n := w.count(ctx, t, `SELECT count(*) FROM run WHERE org_id = $1`, w.org); n != 0 {
		t.Errorf("%d runs were written in a project the token does not belong to", n)
	}

	// The same token in its own project is not refused: without this, the test
	// above would pass against an API that refuses every token.
	if res, raw := w.call(t, asPipeline(token.secret), http.MethodPost, "/runs", w.runBody(w.a)); res.StatusCode != http.StatusAccepted {
		t.Errorf("the same token in its own project: %d: %s", res.StatusCode, raw)
	}
}

// A token's hourly limit counts the runs that token launched, and nobody else's:
// a person is not held to it, and not spared by it.
func TestATokenIsHeldToItsOwnHourlyLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	// As many runs as the limit allows, already launched by this token and
	// already over: they count against the window and do not queue.
	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		for range domain.MaxRunsPerUserPerWindow {
			if _, err := tx.Exec(ctx, `INSERT INTO run
				(org_id, project_id, scenario_id, environment_id, scenario_version, profile,
				 triggered_by_token, status)
				VALUES ($1, $2, $3, $4, 1, 'smoke', $5, 'passed')`,
				w.org, w.a.projectID, w.a.scenarioID, w.a.environmentID, token.id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stage the window: %v", err)
	}

	res, raw := w.call(t, asPipeline(token.secret), http.MethodPost, "/runs", w.runBody(w.a))

	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("past the token's limit: %d, want 429: %s", res.StatusCode, raw)
	}
	if res.Header.Get("Retry-After") == "" {
		t.Error("a limit that can say when it opens did not")
	}
	var limit string
	err = w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT metadata->>'limit' FROM audit_log
			WHERE org_id = $1 AND action = 'run.refused' AND actor_token_id = $2`, w.org, token.id).Scan(&limit)
	})
	if err != nil || limit != "triggered_by_token" {
		t.Errorf("the entry blames %q (%v), want the token's own limit", limit, err)
	}

	// The administrator launched nothing, so what a pipeline did this hour is not
	// held against them.
	if res, raw := w.call(t, w.asAdmin(), http.MethodPost, "/runs", w.runBody(w.a)); res.StatusCode != http.StatusAccepted {
		t.Errorf("a person while a token is at its limit: %d: %s", res.StatusCode, raw)
	}
}

// --- A token stops when it should --------------------------------------------------------

func TestARevokedTokenStopsAtTheNextRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	if res, raw := w.call(t, asPipeline(token.secret), http.MethodPost, "/runs", w.runBody(w.a)); res.StatusCode != http.StatusAccepted {
		t.Fatalf("a live token: %d: %s", res.StatusCode, raw)
	}

	res, raw := w.call(t, w.asAdmin(), http.MethodDelete,
		"/projects/"+w.a.projectID.String()+"/api-tokens/"+token.id.String(), "")
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d: %s", res.StatusCode, raw)
	}

	res, _ = w.call(t, asPipeline(token.secret), http.MethodPost, "/runs", w.runBody(w.a))
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("a revoked token: %d, want 401", res.StatusCode)
	}
	if n := w.count(ctx, t, `SELECT count(*) FROM run WHERE org_id = $1`, w.org); n != 1 {
		t.Errorf("%d runs, want the one queued before the revocation", n)
	}
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE api_token SET expires_at = now() - interval '1 second' WHERE id = $1`, token.id)
		return err
	}); err != nil {
		t.Fatalf("let it expire: %v", err)
	}

	if res, _ := w.call(t, asPipeline(token.secret), http.MethodPost, "/runs", w.runBody(w.a)); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("an expired token: %d, want 401", res.StatusCode)
	}
}

// A token the installation never issued is refused the same way as no credential
// at all, and a cookie next to it does not rescue it.
func TestAnUnknownTokenIsRefusedEvenNextToAValidSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	forged := "blt_" + strings.Repeat("A", 43)

	for name, who := range map[string]as{
		"a forged token alone":         asPipeline(forged),
		"a forged token with a cookie": func(r *http.Request) { w.asAdmin()(r); asPipeline(forged)(r) },
	} {
		res, _ := w.call(t, who, http.MethodPost, "/runs", w.runBody(w.a))
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", name, res.StatusCode)
		}
	}
	if n := w.count(ctx, t, `SELECT count(*) FROM run WHERE org_id = $1`, w.org); n != 0 {
		t.Errorf("%d runs were written", n)
	}
}

// --- A token reaches one route ------------------------------------------------------------

// Everything else a person of this project could do, a token cannot: it is a
// request with no session on any route that did not opt in.
func TestATokenReachesNothingButTheRouteThatOpenedToIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")
	project, someID := w.a.projectID.String(), uuid.NewString()

	// Reading a run and its output, and cancelling one it launched, are not in
	// this list any more: #23 opened those three routes, and
	// run_read_token_integration_test.go holds what a token meets there. What is
	// left is closed, and stays so.
	for name, c := range map[string]struct{ method, path, body string }{
		"read the project":          {http.MethodGet, "/projects/" + project, ""},
		"list the members":          {http.MethodGet, "/projects/" + project + "/members", ""},
		"read the journal":          {http.MethodGet, "/projects/" + project + "/audit", ""},
		"list the tokens":           {http.MethodGet, "/projects/" + project + "/api-tokens", ""},
		"create a successor":        {http.MethodPost, "/projects/" + project + "/api-tokens", `{"name":"successor"}`},
		"revoke itself":             {http.MethodDelete, "/projects/" + project + "/api-tokens/" + token.id.String(), ""},
		"repeat a run":              {http.MethodPost, "/runs/" + someID + "/rerun", ""},
		"stop everything":           {http.MethodPost, "/runs/stop-all", ""},
		"read the organization use": {http.MethodGet, "/usage", ""},
	} {
		res, raw := w.call(t, asPipeline(token.secret), c.method, c.path, c.body)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401: %s", name, res.StatusCode, raw)
		}
	}

	// The token is still alive and still works where it should.
	if res, raw := w.call(t, asPipeline(token.secret), http.MethodPost, "/runs", w.runBody(w.a)); res.StatusCode != http.StatusAccepted {
		t.Errorf("the token after being refused elsewhere: %d: %s", res.StatusCode, raw)
	}
}
