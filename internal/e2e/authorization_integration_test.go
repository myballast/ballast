//go:build integration

package e2e

import (
	"context"
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
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/project"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// A permission mask says what somebody may do. It never says what to, and these
// tests are about that second half: the same organization, two projects, and a
// person who belongs to one of them.
//
// No container is started. Nothing here is ever executed — what is under test is
// what gets refused before anything runs.

// neverRuns stands in for the generator. Reaching it would mean a request got
// past a check it should not have.
type neverRuns struct{ t *testing.T }

func (n neverRuns) Execute(context.Context, run.Job, chan<- domain.LogLine) (run.Outcome, error) {
	n.t.Error("the generator was reached; a request got past a permission check")
	return run.Outcome{}, fmt.Errorf("the generator must not be reached in this test")
}

// neverReadsSecrets stands in for the secret reader, for the same reason: these
// tests end before anything is executed.
type neverReadsSecrets struct{ t *testing.T }

func (n neverReadsSecrets) SecretsOf(context.Context, uuid.UUID, uuid.UUID) (map[string]string, error) {
	n.t.Error("the secrets were decrypted; a request got past a permission check")
	return nil, fmt.Errorf("the secrets must not be read in this test")
}

// tenant is one project of an organization, with everything a run needs.
type tenant struct {
	projectID     uuid.UUID
	scenarioID    uuid.UUID
	environmentID uuid.UUID
}

type world struct {
	api *httptest.Server
	org uuid.UUID
	// session is what the person who belongs to A would carry.
	session *http.Cookie
	userID  uuid.UUID
	a, b    tenant
}

// newWorld builds one organization with two projects. The signed-in person is a
// maintainer of A and holds nothing at all in B.
func newWorld(ctx context.Context, t *testing.T) world {
	t.Helper()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org := uuid.New()
	user := uuid.New()

	var a, b tenant
	err = db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO organization (id, name) VALUES ($1, 'authorization test')`, org); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO app_user (id, org_id, subject, email) VALUES ($1, $2, $3, $4)`,
			user, org, "subject-"+user.String(), user.String()+"@example.test"); err != nil {
			return err
		}

		for _, p := range []struct {
			into *tenant
			slug string
		}{{&a, "project-a"}, {&b, "project-b"}} {
			*p.into = tenant{
				projectID:     uuid.New(),
				scenarioID:    uuid.New(),
				environmentID: uuid.New(),
			}

			if _, err := tx.Exec(ctx,
				`INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, $3, $3)`,
				p.into.projectID, org, p.slug); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO environment
				(id, org_id, project_id, name, base_url, verification_token,
				 verification_status, verified_at, verified_by, max_vus, max_duration_s)
				VALUES ($1, $2, $3, 'local', $4, 'token', 'verified', now(), $5, 5, 60)`,
				p.into.environmentID, org, p.into.projectID,
				"https://"+p.slug+".example.test", user); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO scenario (id, org_id, project_id, name, script)
				 VALUES ($1, $2, $3, 'health', 'export default function () {}')`,
				p.into.scenarioID, org, p.into.projectID); err != nil {
				return err
			}
			// A run names the version it executed, and that version has to
			// exist: the schema no longer lets a run point at a script nobody
			// kept.
			if _, err := tx.Exec(ctx, `INSERT INTO scenario_version
				(org_id, scenario_id, version, script, default_profile)
				VALUES ($1, $2, 1, 'export default function () {}', 'smoke')`,
				org, p.into.scenarioID); err != nil {
				return err
			}
		}

		// Maintainer of A, and nothing in B. Maintainer on purpose: it is the
		// role that can configure a project, so a leak would be a real one.
		_, err := tx.Exec(ctx,
			`INSERT INTO membership (org_id, user_id, project_id, role)
			 VALUES ($1, $2, $3, 'maintainer')`, org, user, a.projectID)
		return err
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

	runs := run.NewService(
		postgres.NewRunRepository(db),
		postgres.NewScenarioRepository(db),
		postgres.NewEnvironmentRepository(db),
		postgres.NewEnvironmentRepository(db),
		neverReadsSecrets{t: t},
		neverRuns{t: t},
		// Nothing here executes a run, so nothing here has a container to
		// collect either.
		nil,
		postgres.NewProjectRepository(db),
		k6.NewOptionsBuilder(),
		k6.NewSummaryParser(),
		permissions,
		infra.SystemClock{},
		run.Accepting(true),
		log,
	)

	projects := project.NewService(
		postgres.NewProjectRepository(db),
		memberships,
		permissions,
		infra.SystemClock{},
		log,
	)

	sessions, err := session.NewManager(
		[]byte(strings.Repeat("a", session.MinSecretLength)), time.Hour, infra.SystemClock{})
	if err != nil {
		t.Fatalf("session manager: %v", err)
	}

	issued := httptest.NewRecorder()
	if err := sessions.Issue(issued, domain.Identity{UserID: user, OrgID: org}); err != nil {
		t.Fatalf("issue a session: %v", err)
	}

	web, _ := url.Parse("http://web.test")
	api := httptest.NewServer(httpadapter.NewRouter(
		runs, projects, nil, nil, auth.NewService(nil, nil, infra.SystemClock{}, log), permissions, sessions, web, log,
	))
	t.Cleanup(api.Close)

	return world{
		api:     api,
		org:     org,
		session: issued.Result().Cookies()[0],
		userID:  user,
		a:       a,
		b:       b,
	}
}

func (w world) request(t *testing.T, method, path, body string) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, w.api.URL+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.AddCookie(w.session)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := w.api.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })

	return res
}

func (w world) enqueueBody(scenario, environment uuid.UUID) string {
	return fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":1}}`,
		scenario, environment)
}

// The first acceptance criterion. A maintainer of A has, in B, exactly the
// rights of a stranger.
func TestAMaintainerOfOneProjectCanDoNothingInAnother(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newWorld(ctx, t)
	somebody := uuid.New()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"read the project", http.MethodGet, "/projects/" + w.b.projectID.String(), ""},
		{"rename the project", http.MethodPatch, "/projects/" + w.b.projectID.String(), `{"name":"Taken over"}`},
		{"delete the project", http.MethodDelete, "/projects/" + w.b.projectID.String(), ""},
		{"list its members", http.MethodGet, "/projects/" + w.b.projectID.String() + "/members", ""},
		{
			"grant themselves a role",
			http.MethodPut,
			"/projects/" + w.b.projectID.String() + "/members/" + w.userID.String(),
			`{"role":"admin"}`,
		},
		{
			"remove somebody",
			http.MethodDelete,
			"/projects/" + w.b.projectID.String() + "/members/" + somebody.String(),
			"",
		},
		{
			"queue a run on it",
			http.MethodPost,
			"/runs",
			w.enqueueBody(w.b.scenarioID, w.b.environmentID),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := w.request(t, c.method, c.path, c.body)

			if res.StatusCode != http.StatusForbidden {
				body, _ := io.ReadAll(res.Body)
				t.Errorf("got %d, want 403: %s", res.StatusCode, body)
			}
		})
	}

	// And nothing changed: the project kept its name, and no run was written.
	w.expectUnchanged(ctx, t)
}

// The same person, in their own project, is not refused. Without this the test
// above would pass just as well against an API that refuses everything.
func TestTheSameMaintainerIsAllowedInTheirOwnProject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newWorld(ctx, t)

	res := w.request(t, http.MethodGet, "/projects/"+w.a.projectID.String(), "")
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Errorf("reading their own project gave %d: %s", res.StatusCode, body)
	}

	res = w.request(t, http.MethodPost, "/runs", w.enqueueBody(w.a.scenarioID, w.a.environmentID))
	if res.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(res.Body)
		t.Errorf("queueing a run on their own project gave %d: %s", res.StatusCode, body)
	}

	// A maintainer is not an administrator: managing members is still refused,
	// in their own project.
	res = w.request(t, http.MethodGet, "/projects/"+w.a.projectID.String()+"/members", "")
	if res.StatusCode != http.StatusOK {
		t.Errorf("listing members of their own project gave %d", res.StatusCode)
	}
	res = w.request(t, http.MethodPut,
		"/projects/"+w.a.projectID.String()+"/members/"+w.userID.String(), `{"role":"admin"}`)
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a maintainer promoting themselves gave %d, want 403", res.StatusCode)
	}
}

// The second acceptance criterion. Both identifiers are the caller's own
// organization, and one is from a project they administer — row-level security
// has nothing to say about it.
func TestARunCannotMixTwoProjects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newWorld(ctx, t)

	// Each case has one answer, not a choice of two, because the order of the
	// checks is fixed: the permission on the scenario's project is asked first,
	// and only then whether the two identifiers belong together. That order is
	// deliberate — asking the other way round would tell somebody with no
	// business in either project that the two sit in different ones.
	cases := map[string]struct {
		scenario, environment uuid.UUID
		want                  int
	}{
		// They may launch in A, so the permission passes and the mismatch is
		// what refuses.
		"their scenario, another project's target": {
			w.a.scenarioID, w.b.environmentID, http.StatusBadRequest,
		},
		// They hold nothing in B, so it never gets as far as comparing.
		"another project's scenario, their target": {
			w.b.scenarioID, w.a.environmentID, http.StatusForbidden,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			res := w.request(t, http.MethodPost, "/runs", w.enqueueBody(c.scenario, c.environment))

			if res.StatusCode == http.StatusAccepted {
				t.Fatal("a run was queued across two projects")
			}
			if res.StatusCode != c.want {
				body, _ := io.ReadAll(res.Body)
				t.Errorf("got %d, want %d: %s", res.StatusCode, c.want, body)
			}
		})
	}

	w.expectUnchanged(ctx, t)
}

// A run of a project the caller does not belong to reads as a run that is not
// there. Answering 403 would confirm the identifier means something.
func TestARunOfAnotherProjectReadsAsAbsent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newWorld(ctx, t)

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	var hidden uuid.UUID
	if err := db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO run
			(org_id, project_id, scenario_id, environment_id, scenario_version, profile)
			VALUES ($1, $2, $3, $4, 1, 'smoke') RETURNING id`,
			w.org, w.b.projectID, w.b.scenarioID, w.b.environmentID).Scan(&hidden)
	}); err != nil {
		t.Fatalf("seed a run in the other project: %v", err)
	}

	res := w.request(t, http.MethodGet, "/runs/"+hidden.String(), "")
	if res.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(res.Body)
		t.Errorf("got %d, want 404: %s", res.StatusCode, body)
	}

	// The same answer as an identifier that stands for nothing at all.
	res = w.request(t, http.MethodGet, "/runs/"+uuid.New().String(), "")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown run gave %d, want 404", res.StatusCode)
	}
}

// expectUnchanged checks that a refused request wrote nothing.
func (w world) expectUnchanged(ctx context.Context, t *testing.T) {
	t.Helper()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	if err := db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx,
			"SELECT name FROM project WHERE id = $1", w.b.projectID).Scan(&name); err != nil {
			return err
		}
		if name != "project-b" {
			t.Errorf("the other project is now named %q", name)
		}

		var members int
		if err := tx.QueryRow(ctx,
			"SELECT count(*) FROM membership WHERE project_id = $1", w.b.projectID).Scan(&members); err != nil {
			return err
		}
		if members != 0 {
			t.Errorf("the other project gained %d member(s)", members)
		}

		var runs int
		if err := tx.QueryRow(ctx,
			"SELECT count(*) FROM run WHERE project_id = $1", w.b.projectID).Scan(&runs); err != nil {
			return err
		}
		if runs != 0 {
			t.Errorf("%d run(s) were queued on the other project", runs)
		}
		return nil
	}); err != nil {
		t.Fatalf("read back the other project: %v", err)
	}
}
