//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/openapi"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/scenario"
)

// Repeating a run executes what the run froze, and not what the scenario has
// become. A scenario is rewritten after a run — a new script, other defaults —
// and the run is repeated: the target is hit by the first script, and the new
// run says version 1, the first profile and the first load.
//
// This is also the proof that the worker executes the version a run names. The
// scenario's own script is what it used to read, and a run queued as version 1
// would have run version 2.
func TestARerunExecutesWhatTheRunFroze(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var (
		mu   sync.Mutex
		seen []string
	)
	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	paths := func() map[string]bool {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]bool{}
		for _, p := range seen {
			out[p] = true
		}
		return out
	}

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, scenarioID, environment := seed(ctx, t, db, target, nil)
	s := build(t, db, org)

	status, first := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenarioID, environment))
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", status)
	}
	s.processOne(ctx, t)

	original := s.get(t, first.ID)
	if original.Status != string(domain.StatusPassed) {
		t.Fatalf("the run ended as %s: %s", original.Status, original.ErrorMessage)
	}
	if !paths()["/healthz"] {
		t.Fatalf("the first run never reached /healthz: %v", seen)
	}

	// Rewrite the scenario: another script, other defaults.
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
		postgres.NewScenarioWriteRepository(db), postgres.NewScenarioWriteRepository(db),
		k6.NewScriptInspector(), openapi.NewReader(), k6.NewGenerator(),
		authz.NewService(postgres.NewMembershipRepository(db), log),
		infra.SystemClock{}, log,
	)
	script := "import http from 'k6/http';\nexport default function () { http.get(`${__ENV.BASE_URL}/elsewhere`); }\n"
	profile := domain.ProfileSoak
	params := domain.ProfileParams{TargetVUs: 3, RampUpS: 1, PlateauS: 600, RampDownS: 1}
	edited, _, err := scenarios.Update(ctx, who, projectID, scenarioID, scenario.Update{
		Script: &script, DefaultProfile: &profile, DefaultProfileParams: &params,
	})
	if err != nil {
		t.Fatalf("update the scenario: %v", err)
	}
	if edited.Version == original.ScenarioVersion {
		t.Fatalf("the scenario stayed at version %d; the test would prove nothing", edited.Version)
	}

	mu.Lock()
	seen = nil
	mu.Unlock()

	// Repeat the first run.
	res := s.do(t, http.MethodPost, "/runs/"+first.ID+"/rerun", nil)
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /runs/{id}/rerun returned %d, want 202", res.StatusCode)
	}
	var queued dto.EnqueueRunResponse
	if err := json.NewDecoder(res.Body).Decode(&queued); err != nil {
		t.Fatalf("decode the response: %v", err)
	}
	if queued.ID == first.ID {
		t.Fatal("repeating a run returned the same run")
	}

	// What is written on the new run is what the first one froze.
	repeated := s.get(t, queued.ID)
	if repeated.ScenarioVersion != original.ScenarioVersion {
		t.Errorf("the new run froze version %d, want %d: the scenario is at %d now",
			repeated.ScenarioVersion, original.ScenarioVersion, edited.Version)
	}
	if repeated.Profile != original.Profile || !reflect.DeepEqual(repeated.Params, original.Params) {
		t.Errorf("the new run is %s %+v, want %s %+v", repeated.Profile, repeated.Params, original.Profile, original.Params)
	}
	if repeated.EnvironmentID != original.EnvironmentID || repeated.ScenarioID != original.ScenarioID {
		t.Error("the new run targets something else than the first one")
	}
	if repeated.Number != original.Number+1 {
		t.Errorf("the new run is #%d, want #%d", repeated.Number, original.Number+1)
	}

	// And what is executed is that version's script: the target is hit by the
	// first script and never by the one that replaced it.
	s.processOne(ctx, t)
	done := s.get(t, queued.ID)
	if done.Status != string(domain.StatusPassed) {
		t.Fatalf("the repeated run ended as %s: %s", done.Status, done.ErrorMessage)
	}
	got := paths()
	if !got["/healthz"] {
		t.Errorf("the repeated run never ran the first script: the target saw %v", got)
	}
	if got["/elsewhere"] {
		t.Errorf("the repeated run ran the scenario's current script: the target saw %v", got)
	}

	// A run that is not there is not repeated.
	missing := s.do(t, http.MethodPost, "/runs/"+uuid.NewString()+"/rerun", nil)
	defer func() { _ = missing.Body.Close() }()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("repeating a run that does not exist returned %d, want 404", missing.StatusCode)
	}
}
