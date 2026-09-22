//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	httpadapter "github.com/youssefmahersi/ballast/internal/adapter/http"
	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/adapter/runner/hosted"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// The generator reaches the target through the host gateway, which Docker
// Desktop and the Linux runners both provide.
const hostAlias = "host.docker.internal"

const defaultDSN = "postgres://ballast_app:ballast_app@127.0.0.1:5432/ballast?sslmode=disable"

func dsn() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultDSN
}

// target is a server the generator can actually reach: bound to every
// interface, because 127.0.0.1 means nothing inside a container.
func newTarget(t *testing.T, handler http.Handler) string {
	t.Helper()

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := httptest.NewUnstartedServer(handler)
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	return fmt.Sprintf("http://%s:%d", hostAlias, listener.Addr().(*net.TCPAddr).Port)
}

const healthScript = `
import http from 'k6/http';
import { check } from 'k6';

export default function () {
	const res = http.get(` + "`${__ENV.BASE_URL}/healthz`" + `);
	check(res, { 'health is 200': (r) => r.status === 200 });
}
`

// seed lays down one organization with a project, a verified target and a
// scenario, through the application role and its policies.
func seed(ctx context.Context, t *testing.T, db *postgres.DB, baseURL string, thresholds [][2]string) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()

	var (
		org      = uuid.New()
		user     = uuid.New()
		project  = uuid.New()
		env      = uuid.New()
		scenario = uuid.New()
	)

	err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		statements := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO organization (id, name) VALUES ($1, 'end to end')`, []any{org}},
			{`INSERT INTO app_user (id, org_id, subject, email) VALUES ($1, $2, $3, $4)`,
				[]any{user, org, "subject-" + user.String(), user.String() + "@example.test"}},
			{`INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, 'p', $3)`,
				[]any{project, org, project.String()}},
			{`INSERT INTO environment
				(id, org_id, project_id, name, base_url, verification_token,
				 verification_status, verified_at, verified_by, max_vus, max_duration_s)
			  VALUES ($1, $2, $3, 'local', $4, 'token', 'verified', now(), $5, 5, 120)`,
				[]any{env, org, project, baseURL, user}},
			{`INSERT INTO scenario (id, org_id, project_id, name, script) VALUES ($1, $2, $3, 'health', $4)`,
				[]any{scenario, org, project, healthScript}},
		}
		for _, threshold := range thresholds {
			statements = append(statements, struct {
				sql  string
				args []any
			}{
				`INSERT INTO threshold (org_id, scenario_id, metric, expression) VALUES ($1, $2, $3, $4)`,
				[]any{org, scenario, threshold[0], threshold[1]},
			})
		}

		for _, statement := range statements {
			if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		_ = db.InTenantTx(cleanup, org, func(tx pgx.Tx) error {
			for _, table := range []string{
				"run_threshold", "run_metric", "run", "threshold", "scenario",
				"environment", "project", "app_user", "organization",
			} {
				if _, err := tx.Exec(cleanup,
					"DELETE FROM "+pgx.Identifier{table}.Sanitize()+" WHERE org_id = $1", org); err != nil {
					return err
				}
			}
			return nil
		})
	})

	return org, scenario, env
}

type stack struct {
	api     *httptest.Server
	service *run.Service
	org     uuid.UUID
}

// build wires every layer exactly as cmd/api and cmd/worker do.
func build(t *testing.T, db *postgres.DB, org uuid.UUID) stack {
	t.Helper()

	runner, err := hosted.New(hosted.Config{
		TTL:        2 * time.Minute,
		CPUs:       1,
		MemoryMB:   256,
		ExtraHosts: []string{hostAlias + ":host-gateway"},
	})
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	service := run.NewService(
		postgres.NewRunRepository(db),
		postgres.NewScenarioRepository(db),
		postgres.NewEnvironmentRepository(db),
		runner,
		k6.NewOptionsBuilder(),
		k6.NewSummaryParser(),
		infra.SystemClock{},
		log,
	)

	api := httptest.NewServer(httpadapter.NewRouter(service, org, log))
	t.Cleanup(api.Close)

	return stack{api: api, service: service, org: org}
}

func (s stack) enqueue(t *testing.T, body string) (int, dto.EnqueueRunResponse) {
	t.Helper()

	res, err := s.api.Client().Post(s.api.URL+"/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /runs: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	var decoded dto.EnqueueRunResponse
	_ = json.NewDecoder(res.Body).Decode(&decoded)

	return res.StatusCode, decoded
}

func (s stack) get(t *testing.T, id string) dto.RunResponse {
	t.Helper()

	res, err := s.api.Client().Get(s.api.URL + "/runs/" + id)
	if err != nil {
		t.Fatalf("GET /runs/%s: %v", id, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /runs/%s returned %d", id, res.StatusCode)
	}

	var decoded dto.RunResponse
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode the run: %v", err)
	}
	return decoded
}

// processOne runs the worker's single step, draining the generator's output the
// way the worker does.
func (s stack) processOne(ctx context.Context, t *testing.T) {
	t.Helper()

	logs := make(chan domain.LogLine, 512)

	var wg sync.WaitGroup
	wg.Add(1)
	var lines int
	go func() {
		defer wg.Done()
		for range logs {
			lines++
		}
	}()

	worked, err := s.service.ProcessNext(ctx, s.org, logs)
	close(logs)
	wg.Wait()

	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !worked {
		t.Fatal("the worker found nothing to do")
	}
	if lines == 0 {
		t.Error("the generator produced no output")
	}
}

// The milestone, in one test: a request in, a container run against a real
// server, and metrics in the database.
func TestVerticalSkeletonPasses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var hits int
	var mu sync.Mutex
	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, scenario, environment := seed(ctx, t, db, target, [][2]string{
		{"http_req_failed", "rate<0.5"},
	})
	s := build(t, db, org)

	status, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenario, environment))
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", status)
	}
	if queued.Status != string(domain.StatusQueued) {
		t.Fatalf("a new run is %s, want queued", queued.Status)
	}

	s.processOne(ctx, t)

	finished := s.get(t, queued.ID)
	if finished.Status != string(domain.StatusPassed) {
		t.Fatalf("the run ended as %s: %s", finished.Status, finished.ErrorMessage)
	}

	mu.Lock()
	served := hits
	mu.Unlock()
	if served == 0 {
		t.Error("the generator never reached the target")
	}

	if len(finished.Metrics) == 0 {
		t.Error("no metric was stored")
	}
	if len(finished.Thresholds) != 1 || !finished.Thresholds[0].Passed {
		t.Errorf("threshold verdicts are %+v", finished.Thresholds)
	}

	// The six statistics of a trend metric have to coexist, which is what
	// migration 002 was for.
	stats := map[string]bool{}
	for _, metric := range finished.Metrics {
		if metric.Metric == "http_req_duration" && metric.Tag == "" {
			stats[metric.Stat] = true
		}
	}
	for _, stat := range []string{"avg", "min", "med", "max", "p(90)", "p(95)"} {
		if !stats[stat] {
			t.Errorf("http_req_duration.%s was not stored", stat)
		}
	}

	// Frozen at launch, and unchanged by anything that happened since.
	if finished.Params.TargetVUs != 1 || finished.Params.PlateauS != 3 {
		t.Errorf("the stored parameters are %+v", finished.Params)
	}
}

// A threshold the run cannot meet ends it as failed, not as an error: the test
// ran, it simply did not pass.
func TestVerticalSkeletonFailsOnAThreshold(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, scenario, environment := seed(ctx, t, db, target, [][2]string{
		{"http_req_duration", "p(95)<0.0001"},
	})
	s := build(t, db, org)

	_, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenario, environment))

	s.processOne(ctx, t)

	finished := s.get(t, queued.ID)
	if finished.Status != string(domain.StatusFailed) {
		t.Fatalf("the run ended as %s, want failed", finished.Status)
	}
	if len(finished.Thresholds) != 1 || finished.Thresholds[0].Passed {
		t.Errorf("threshold verdicts are %+v", finished.Thresholds)
	}
}

// No load leaves for a target whose ownership was never proved and approved.
func TestVerticalSkeletonRefusesAnUnverifiedTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, scenario, environment := seed(ctx, t, db, "http://unused.example.test", nil)

	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE environment
			SET verification_status = 'pending', verified_at = NULL, verified_by = NULL
			WHERE id = $1`, environment)
		return err
	}); err != nil {
		t.Fatalf("revoke the verification: %v", err)
	}

	s := build(t, db, org)

	status, _ := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke"}`, scenario, environment))
	if status != http.StatusForbidden {
		t.Fatalf("POST /runs returned %d, want 403", status)
	}

	var runs int
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM run").Scan(&runs)
	}); err != nil {
		t.Fatalf("count the runs: %v", err)
	}
	if runs != 0 {
		t.Errorf("%d run(s) were written for an unverified target", runs)
	}
}
