//go:build integration

package e2e

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
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

// A run says what was executed, and goes on saying it whatever happens to the
// scenario afterwards: a new script, a new name, new defaults, new thresholds.
// The detail of a run is read from the run and from the scenario version it
// names; editing the scenario writes a new version and touches neither.
func TestARunIsUnchangedByEditingItsScenario(t *testing.T) {
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

	org, scenarioID, environment := seed(ctx, t, db, target, [][2]string{
		{"http_req_failed", "rate<0.5"},
	})
	s := build(t, db, org)

	status, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenarioID, environment))
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", status)
	}
	s.processOne(ctx, t)

	before := s.get(t, queued.ID)
	if before.Status != string(domain.StatusPassed) {
		t.Fatalf("the run ended as %s: %s", before.Status, before.ErrorMessage)
	}

	// Edit everything a scenario has, as the person who owns it would.
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
	scenarios := scenario.NewService(
		postgres.NewScenarioWriteRepository(db),
		postgres.NewScenarioWriteRepository(db),
		k6.NewScriptInspector(),
		openapi.NewReader(),
		k6.NewGenerator(),
		authz.NewService(postgres.NewMembershipRepository(db), log),
		infra.SystemClock{},
		log,
	)

	renamed := "health, rewritten"
	script := "import http from 'k6/http';\nexport default function () { http.get(`${__ENV.BASE_URL}/elsewhere`); }\n"
	profile := domain.ProfileLoad
	params := domain.ProfileParams{TargetVUs: 50, RampUpS: 30, PlateauS: 600, RampDownS: 30}
	edited, _, err := scenarios.Update(ctx, who, projectID, scenarioID, scenario.Update{
		Name:                 &renamed,
		Script:               &script,
		DefaultProfile:       &profile,
		DefaultProfileParams: &params,
	})
	if err != nil {
		t.Fatalf("update the scenario: %v", err)
	}
	if edited.Version == before.ScenarioVersion {
		t.Fatalf("a new script left the scenario at version %d; the test would prove nothing", edited.Version)
	}
	if _, err := scenarios.AddThreshold(ctx, who, projectID, scenarioID, "http_req_duration", "p(95)<1"); err != nil {
		t.Fatalf("add a threshold: %v", err)
	}

	// The run reads exactly as it did: same version, profile, parameters,
	// metrics and verdicts. Not one field moved.
	after := s.get(t, queued.ID)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("editing the scenario changed the run:\nbefore %+v\nafter  %+v", before, after)
	}

	// And the version the run names still holds the script that ran.
	versions, err := scenarios.Versions(ctx, who, projectID, scenarioID)
	if err != nil {
		t.Fatalf("read the versions: %v", err)
	}
	var executed *domain.ScenarioVersion
	for i := range versions {
		if versions[i].Version == before.ScenarioVersion {
			executed = &versions[i]
		}
	}
	if executed == nil {
		t.Fatalf("version %d, which the run executed, is gone", before.ScenarioVersion)
	}
	if executed.Script != healthScript {
		t.Errorf("version %d's script changed:\n%s", before.ScenarioVersion, executed.Script)
	}
}
