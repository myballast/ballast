//go:build integration

package e2e

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/openapi"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/scenario"
)

// importer is the scenario service wired the way cmd/api wires it, together with
// the person and the project a scenario can be imported into.
type importer struct {
	service   *scenario.Service
	who       domain.Identity
	projectID uuid.UUID
}

func newImporter(ctx context.Context, t *testing.T, db *postgres.DB, org uuid.UUID) importer {
	t.Helper()

	var who domain.Identity
	var projectID uuid.UUID
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		who.OrgID = org
		return tx.QueryRow(ctx,
			"SELECT user_id, project_id FROM membership WHERE org_id = $1 AND role = 'admin' LIMIT 1", org,
		).Scan(&who.UserID, &projectID)
	}); err != nil {
		t.Fatalf("find the admin: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return importer{
		service: scenario.NewService(
			postgres.NewScenarioWriteRepository(db),
			postgres.NewScenarioWriteRepository(db),
			k6.NewScriptInspector(),
			openapi.NewReader(),
			k6.NewGenerator(),
			authz.NewService(postgres.NewMembershipRepository(db), log),
			infra.SystemClock{},
			log,
		),
		who:       who,
		projectID: projectID,
	}
}

const petsSpecification = `{
	"openapi": "3.0.3",
	"info": {"title": "Pets", "version": "1"},
	"paths": {
		"/ping": {"get": {"tags": ["system"]}},
		"/items/{id}": {"get": {"tags": ["items"]}},
		"/items": {
			"post": {
				"tags": ["items"],
				"requestBody": {"content": {"application/json": {"example": {"name": "thing", "tags": ["a", "b"]}}}}
			}
		}
	}
}`

// A scenario imported from a specification is a real scenario: the script that
// was generated from the chosen endpoints runs in a k6 container, and the target
// receives exactly the requests the specification described.
func TestAnImportedSpecificationRunsAgainstATarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	type received struct{ method, path, contentType, body string }
	var mu sync.Mutex
	var requests []received

	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, received{r.Method, r.URL.Path, r.Header.Get("Content-Type"), string(body)})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, _, environment := seed(ctx, t, db, target, nil)
	imports := newImporter(ctx, t, db, org)

	imported, _, notes, err := imports.service.ImportOpenAPI(ctx, imports.who, scenario.OpenAPIImport{
		ProjectID: imports.projectID,
		Name:      "pets",
		Spec:      []byte(petsSpecification),
		Endpoints: []domain.EndpointRef{
			{Method: "GET", Path: "/ping"},
			{Method: "POST", Path: "/items"},
		},
	})
	if err != nil {
		t.Fatalf("ImportOpenAPI: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("notes = %v, wanted none", notes)
	}
	if imported.Mode != domain.ModeGraph {
		t.Fatalf("the scenario is in mode %q, wanted graph", imported.Mode)
	}

	s := build(t, db, org)
	status, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		imported.ID, environment))
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", status)
	}
	s.processOne(ctx, t)

	finished := s.get(t, queued.ID)
	if finished.Status != string(domain.StatusPassed) {
		t.Fatalf("the run ended as %s: %s", finished.Status, finished.ErrorMessage)
	}

	mu.Lock()
	defer mu.Unlock()

	var pings, posts int
	for _, request := range requests {
		switch {
		case request.method == "GET" && request.path == "/ping":
			pings++
		case request.method == "POST" && request.path == "/items":
			posts++
			if request.body != `{"name":"thing","tags":["a","b"]}` {
				t.Errorf("the target received the body %q", request.body)
			}
			if request.contentType != "application/json" {
				t.Errorf("the target received the content type %q", request.contentType)
			}
		default:
			t.Errorf("the target received an unexpected %s %s", request.method, request.path)
		}
	}
	if pings == 0 || posts == 0 {
		t.Errorf("the target received %d pings and %d posts, wanted both", pings, posts)
	}
}

// The ticket asks for a real specification of a hundred endpoints to import in
// under two seconds. This measures the whole of it: reading the file, building
// the graph, generating and inspecting the script, and storing the scenario.
func TestAHundredEndpointSpecificationImportsInUnderTwoSeconds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, _, _ := seed(ctx, t, db, "http://example.test", nil)
	imports := newImporter(ctx, t, db, org)

	var paths strings.Builder
	var chosen []domain.EndpointRef
	for i := 0; i < 50; i++ {
		if i > 0 {
			paths.WriteString(",")
		}
		path := fmt.Sprintf("/resource%d/{id}", i)
		fmt.Fprintf(&paths, `%q: {
			"get": {"tags": ["group%d"], "summary": "read %d"},
			"put": {"tags": ["group%d"], "requestBody": {"content": {"application/json": {"example": {"name": "n%d", "count": %d}}}}}
		}`, path, i%7, i, i%7, i, i)
		chosen = append(chosen,
			domain.EndpointRef{Method: "GET", Path: path},
			domain.EndpointRef{Method: "PUT", Path: path})
	}
	specification := fmt.Sprintf(`{"openapi":"3.0.3","info":{"title":"Big"},"paths":{%s}}`, paths.String())

	started := time.Now()
	imported, _, _, err := imports.service.ImportOpenAPI(ctx, imports.who, scenario.OpenAPIImport{
		ProjectID: imports.projectID,
		Name:      "big",
		Spec:      []byte(specification),
		Endpoints: chosen,
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("ImportOpenAPI: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("importing a hundred endpoints took %v, the ticket allows 2s", elapsed)
	}

	// Read back from the database, not from what Create returned.
	stored, report, err := imports.service.ByID(ctx, imports.who, imports.projectID, imported.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if stored.Mode != domain.ModeGraph || stored.Graph == nil || len(stored.Graph.Nodes) != 101 {
		t.Fatalf("stored mode = %q, graph = %v, wanted a graph of 100 requests and an end", stored.Mode, stored.Graph)
	}
	if len(report.Errors()) != 0 {
		t.Errorf("the stored script has errors: %+v", report.Errors())
	}
	if !strings.Contains(stored.Script, `name: "GET /resource0/{id}"`) {
		t.Errorf("the name tag is not the path template; the script is:\n%.600s", stored.Script)
	}
}

// A script generated from a graph is read only, and importing is the only way
// to make one for now. Editing it must keep being refused after a round trip
// through the database.
func TestAnImportedScenarioCannotHaveItsScriptEdited(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, _, _ := seed(ctx, t, db, "http://example.test", nil)
	imports := newImporter(ctx, t, db, org)

	imported, _, _, err := imports.service.ImportOpenAPI(ctx, imports.who, scenario.OpenAPIImport{
		ProjectID: imports.projectID,
		Name:      "pets",
		Spec:      []byte(petsSpecification),
		Endpoints: []domain.EndpointRef{{Method: "GET", Path: "/ping"}},
	})
	if err != nil {
		t.Fatalf("ImportOpenAPI: %v", err)
	}

	script := "export default function () {}"
	_, _, err = imports.service.Update(ctx, imports.who, imports.projectID, imported.ID, scenario.Update{Script: &script})
	if err == nil || !strings.Contains(err.Error(), domain.ErrScriptIsGenerated.Error()) {
		t.Fatalf("Update error = %v, wanted ErrScriptIsGenerated", err)
	}
}
