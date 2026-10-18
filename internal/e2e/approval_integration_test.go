//go:build integration

package e2e

import (
	"context"
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
	"github.com/youssefmahersi/ballast/internal/infra/crypto"
	"github.com/youssefmahersi/ballast/internal/usecase/audit"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/environment"
	"github.com/youssefmahersi/ballast/internal/usecase/project"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// servedFiles stands in for the target's web server, and for the verifier that
// reads it. The real verifier refuses every address that is not public, and
// must go on refusing them: a test server on this machine is exactly what it
// exists to keep out. What is under test here is who may take which step, not
// how the file is fetched — that has its own tests in adapter/verifier.
type servedFiles struct{ files map[string]string }

func (s *servedFiles) ReadProof(_ context.Context, proofURL, expected string) error {
	content, ok := s.files[proofURL]
	if !ok {
		return fmt.Errorf("%s answered 404", proofURL)
	}
	if strings.TrimSpace(content) != expected {
		return fmt.Errorf("%s does not hold the token", proofURL)
	}
	return nil
}

type approvalWorld struct {
	api        *httptest.Server
	users      map[domain.Role]uuid.UUID
	db         *postgres.DB
	org        uuid.UUID
	project    uuid.UUID
	scenario   uuid.UUID
	sessions   map[domain.Role]*http.Cookie
	targetHost *servedFiles
}

// newApprovalWorld builds one project with three members: a maintainer, an
// admin and a runner. It holds a scenario and no environment at all.
func newApprovalWorld(ctx context.Context, t *testing.T) approvalWorld {
	t.Helper()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, projectID, scenarioID := uuid.New(), uuid.New(), uuid.New()
	users := map[domain.Role]uuid.UUID{
		domain.RoleMaintainer: uuid.New(),
		domain.RoleAdmin:      uuid.New(),
		domain.RoleRunner:     uuid.New(),
	}

	err = db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		statements := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO organization (id, name) VALUES ($1, 'approval test')`, []any{org}},
			{`INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, 'shop', 'shop')`, []any{projectID, org}},
			{`INSERT INTO scenario (id, org_id, project_id, name, script)
			  VALUES ($1, $2, $3, 'health', 'export default function () {}')`, []any{scenarioID, org, projectID}},
			{`INSERT INTO scenario_version (org_id, scenario_id, version, script, default_profile)
			  VALUES ($1, $2, 1, 'export default function () {}', 'smoke')`, []any{org, scenarioID}},
		}
		for role, user := range users {
			statements = append(statements,
				struct {
					sql  string
					args []any
				}{`INSERT INTO app_user (id, org_id, subject, email) VALUES ($1, $2, $3, $4)`,
					[]any{user, org, "subject-" + user.String(), user.String() + "@example.test"}},
				struct {
					sql  string
					args []any
				}{`INSERT INTO membership (org_id, user_id, project_id, role) VALUES ($1, $2, $3, $4)`,
					[]any{org, user, projectID, string(role)}},
			)
		}
		for _, s := range statements {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed the organization: %v", err)
	}

	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		_ = db.InTenantTx(cleanup, org, func(tx pgx.Tx) error {
			for _, table := range []string{
				"run_threshold", "run_metric", "run", "threshold", "scenario_version", "scenario",
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
	memberships := postgres.NewMembershipRepository(db)
	permissions := authz.NewService(memberships, log)
	target := &servedFiles{files: map[string]string{}}

	runs := run.NewService(
		postgres.NewRunRepository(db),
		postgres.NewScenarioRepository(db),
		postgres.NewEnvironmentRepository(db),
		postgres.NewEnvironmentRepository(db),
		neverReadsSecrets{t: t},
		// A run is queued here, never executed.
		neverRuns{t: t},
		nil,
		postgres.NewProjectRepository(db),
		postgres.NewLogRepository(db),
		postgres.NewAuditRepository(db),
		k6.NewOptionsBuilder(),
		k6.NewSummaryParser(),
		permissions,
		infra.SystemClock{},
		run.Accepting(true),
		log,
	)

	projects := project.NewService(
		postgres.NewProjectRepository(db), memberships, permissions, postgres.NewAuditRepository(db), infra.SystemClock{}, log)

	// No secret is set in this test, so no cipher is needed to store one.
	environments := environment.NewService(
		postgres.NewEnvironmentWriteRepository(db),
		nil,
		crypto.NewRandomTokens(),
		target,
		postgres.NewAuditRepository(db),
		permissions,
		infra.SystemClock{},
		log,
	)

	manager, err := session.NewManager(
		[]byte(strings.Repeat("a", session.MinSecretLength)), time.Hour, infra.SystemClock{})
	if err != nil {
		t.Fatalf("session manager: %v", err)
	}

	sessions := map[domain.Role]*http.Cookie{}
	for role, user := range users {
		issued := httptest.NewRecorder()
		if err := manager.Issue(issued, domain.Identity{UserID: user, OrgID: org}); err != nil {
			t.Fatalf("issue a session: %v", err)
		}
		sessions[role] = issued.Result().Cookies()[0]
	}

	web, _ := url.Parse("http://web.test")
	api := httptest.NewServer(httpadapter.NewRouter(
		runs, projects, nil, environments, nil,
		audit.NewService(postgres.NewAuditRepository(db), permissions, log),
		auth.NewService(nil, nil, infra.SystemClock{}, log),
		permissions, manager, web, log,
	))
	t.Cleanup(api.Close)

	return approvalWorld{
		api: api, users: users, db: db, org: org, project: projectID, scenario: scenarioID,
		sessions: sessions, targetHost: target,
	}
}

// member is who holds a role in this world.
func (w approvalWorld) member(role domain.Role) uuid.UUID {
	return w.users[role]
}

// as sends a request with the session of the member holding role, and decodes
// a JSON answer into into when it is given.
func (w approvalWorld) as(t *testing.T, role domain.Role, method, path, body string, into any) int {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, w.api.URL+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.AddCookie(w.sessions[role])
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := w.api.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()

	if into != nil && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(into); err != nil {
			t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
	return res.StatusCode
}

// The acceptance criterion of the environments screen, through the API it
// calls: a maintainer declares a target and proves it, an admin approves it,
// and a runner can then send load at it — and at no step before.
func TestAMaintainerDeclaresAnAdminApprovesARunnerRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newApprovalWorld(ctx, t)
	environments := fmt.Sprintf("/projects/%s/environments", w.project)
	declaration := `{"name":"staging","base_url":"https://shop.example.test","max_vus":5,"max_duration_s":60}`

	// Declaring is the maintainer's; a runner cannot.
	if status := w.as(t, domain.RoleRunner, "POST", environments, declaration, nil); status != http.StatusForbidden {
		t.Fatalf("a runner declaring a target got %d, want 403", status)
	}

	var declared struct {
		ID     string `json:"id"`
		Status string `json:"verification_status"`
	}
	if status := w.as(t, domain.RoleMaintainer, "POST", environments, declaration, &declared); status != http.StatusCreated {
		t.Fatalf("the maintainer declaring a target got %d, want 201", status)
	}
	if declared.Status != "pending" {
		t.Fatalf("a declared target is %q, want pending", declared.Status)
	}
	environment := environments + "/" + declared.ID

	launch := func() int {
		t.Helper()
		return w.as(t, domain.RoleRunner, "POST", "/runs", fmt.Sprintf(
			`{"scenario_id":%q,"environment_id":%q,"profile":"smoke"}`, w.scenario, declared.ID), nil)
	}
	if status := launch(); status != http.StatusForbidden {
		t.Fatalf("a run against a target nobody verified got %d, want 403", status)
	}

	// The challenge is for whoever manages environments.
	if status := w.as(t, domain.RoleRunner, "GET", environment+"/verification", "", nil); status != http.StatusForbidden {
		t.Fatalf("a runner reading the challenge got %d, want 403", status)
	}
	var challenge struct {
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	if status := w.as(t, domain.RoleMaintainer, "GET", environment+"/verification", "", &challenge); status != http.StatusOK {
		t.Fatalf("the maintainer reading the challenge got %d, want 200", status)
	}

	// Verifying before the file is served proves nothing.
	if status := w.as(t, domain.RoleMaintainer, "POST", environment+"/verify", "", nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("verifying a target that serves nothing got %d, want 422", status)
	}

	w.targetHost.files[challenge.URL] = challenge.Token + "\n"

	var proven struct {
		Status string `json:"verification_status"`
	}
	if status := w.as(t, domain.RoleMaintainer, "POST", environment+"/verify", "", &proven); status != http.StatusOK {
		t.Fatalf("verifying a target that serves its token got %d, want 200", status)
	}
	if proven.Status != "proof_ok" {
		t.Fatalf("a proven target is %q, want proof_ok", proven.Status)
	}
	if status := launch(); status != http.StatusForbidden {
		t.Fatalf("a run against a proven but unapproved target got %d, want 403", status)
	}

	// Approving is the admin's alone: the maintainer who proved the target
	// cannot also let load reach it.
	for _, role := range []domain.Role{domain.RoleMaintainer, domain.RoleRunner} {
		if status := w.as(t, role, "POST", environment+"/approve", "", nil); status != http.StatusForbidden {
			t.Fatalf("a %s approving the target got %d, want 403", role, status)
		}
	}

	var approved struct {
		Status string `json:"verification_status"`
	}
	if status := w.as(t, domain.RoleAdmin, "POST", environment+"/approve", "", &approved); status != http.StatusOK {
		t.Fatalf("the admin approving the target got %d, want 200", status)
	}
	if approved.Status != "verified" {
		t.Fatalf("an approved target is %q, want verified", approved.Status)
	}

	var hosts int
	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_allowed_host
			WHERE project_id = $1 AND host = 'shop.example.test'`, w.project).Scan(&hosts)
	}); err != nil {
		t.Fatalf("read the allowlist: %v", err)
	}
	if hosts != 1 {
		t.Errorf("the approved host is %d time(s) in the project's allowlist, want once", hosts)
	}

	if status := launch(); status != http.StatusAccepted {
		t.Fatalf("a runner launching against the approved target got %d, want 202", status)
	}
}
