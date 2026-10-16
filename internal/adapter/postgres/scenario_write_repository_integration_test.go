//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// newScenarioWorld lays down an organization with a project and the target a run
// needs, through the application role and its policies.
func newScenarioWorld(ctx context.Context, t *testing.T, db *postgres.DB) (domain.Identity, uuid.UUID, uuid.UUID) {
	t.Helper()

	who, _ := seedOrgAndUser(ctx, t, db)
	project, environment := uuid.New(), uuid.New()

	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, 'p', $3)`,
			project, who.OrgID, project.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO environment
			(id, org_id, project_id, name, base_url, verification_token,
			 verification_status, verified_at, verified_by, max_vus, max_duration_s)
			VALUES ($1, $2, $3, 'local', 'https://local.example.test', 'token',
			 'verified', now(), $4, 5, 60)`,
			environment, who.OrgID, project, who.UserID)
		return err
	}); err != nil {
		t.Fatalf("seed the project: %v", err)
	}

	return who, project, environment
}

// The first acceptance criterion, taken literally: a run recorded against
// version 1 must still be able to say what version 1 contained, after the
// scenario has moved on.
func TestEditingAScenarioLeavesRecordedRunsAlone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project, environment := newScenarioWorld(ctx, t, db)

	scenarios := postgres.NewScenarioWriteRepository(db)

	const first = "export default function () { /* version one */ }"
	created, err := scenarios.Create(ctx, domain.Scenario{
		ID:                   uuid.New(),
		ProjectID:            project,
		Name:                 "checkout",
		Script:               first,
		Version:              1,
		DefaultProfile:       domain.ProfileSmoke,
		DefaultProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
	}, who, time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A run of version 1, recorded.
	runs := postgres.NewRunRepository(db)
	recorded, err := runs.Create(ctx, domain.NewRun(
		who.OrgID,
		domain.Scenario{ID: created.ID, ProjectID: project, Version: created.Version},
		domain.Environment{ID: environment, ProjectID: project, MaxVUs: 5, MaxDurationS: 60},
		domain.ProfileSmoke,
		domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
		nil,
	))
	if err != nil {
		t.Fatalf("record a run: %v", err)
	}

	// The scenario moves on.
	const second = "export default function () { /* version two, quite different */ }"
	updated, err := scenarios.NewVersion(ctx, who.OrgID, created.ID, domain.ScenarioVersion{
		ScenarioID:           created.ID,
		Version:              2,
		Script:               second,
		DefaultProfile:       domain.ProfileLoad,
		DefaultProfileParams: domain.ProfileParams{TargetVUs: 50, RampUpS: 10, PlateauS: 60},
	}, who, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVersion: %v", err)
	}
	if updated.Version != 2 || updated.Script != second {
		t.Fatalf("the scenario did not move on: %+v", updated)
	}

	// The run still says version 1.
	read, err := runs.ByID(ctx, who.OrgID, recorded.ID)
	if err != nil {
		t.Fatalf("read the run back: %v", err)
	}
	if read.ScenarioVersion != 1 {
		t.Errorf("the recorded run now claims version %d", read.ScenarioVersion)
	}
	if read.ProfileParams != (domain.ProfileParams{TargetVUs: 1, PlateauS: 5}) {
		t.Errorf("the recorded run's parameters changed: %+v", read.ProfileParams)
	}

	// And version 1 can still be read, which is the part that was impossible
	// before: the script a past run executed is not inferred, it is kept.
	versions, err := scenarios.Versions(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("%d version(s) kept, want 2", len(versions))
	}
	if versions[0].Version != 2 {
		t.Errorf("versions are not newest first: %d came back first", versions[0].Version)
	}

	byNumber := map[int]domain.ScenarioVersion{}
	for _, version := range versions {
		byNumber[version.Version] = version
	}
	if byNumber[1].Script != first {
		t.Errorf("version 1 now holds %q", byNumber[1].Script)
	}
	if byNumber[1].DefaultProfile != domain.ProfileSmoke {
		t.Errorf("version 1's profile changed to %q", byNumber[1].DefaultProfile)
	}
	if byNumber[2].Script != second {
		t.Errorf("version 2 holds %q", byNumber[2].Script)
	}
	if byNumber[1].CreatedBy == nil || *byNumber[1].CreatedBy != who.UserID {
		t.Error("version 1 does not record who wrote it")
	}
}

// The head of the scenario and its newest version must agree. They are
// deliberately duplicated — the worker reads the head — so this is what keeps
// the duplication honest.
func TestTheScenarioHeadMatchesItsNewestVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project, _ := newScenarioWorld(ctx, t, db)

	scenarios := postgres.NewScenarioWriteRepository(db)

	created, err := scenarios.Create(ctx, domain.Scenario{
		ID: uuid.New(), ProjectID: project, Name: "checkout",
		Script: "one", Version: 1, DefaultProfile: domain.ProfileSmoke,
		DefaultProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
	}, who, time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A slice rather than a map: the versions have to be written in order, and a
	// map's iteration order is random -- which made this test write version 3
	// before version 2 about half the time, and then rightly complain that the
	// head had stayed behind.
	for _, next := range []struct {
		version int
		script  string
	}{{2, "two"}, {3, "three"}} {
		version, script := next.version, next.script

		if _, err := scenarios.NewVersion(ctx, who.OrgID, created.ID, domain.ScenarioVersion{
			ScenarioID: created.ID, Version: version, Script: script,
			DefaultProfile:       domain.ProfileLoad,
			DefaultProfileParams: domain.ProfileParams{TargetVUs: 2, PlateauS: 6},
		}, who, time.Now().UTC()); err != nil {
			t.Fatalf("NewVersion %d: %v", version, err)
		}
	}

	head, err := scenarios.ByID(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}

	versions, err := scenarios.Versions(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	newest := versions[0]

	switch {
	case head.Version != newest.Version:
		t.Errorf("the head is at version %d and the newest kept is %d", head.Version, newest.Version)
	case head.Script != newest.Script:
		t.Error("the head's script differs from its newest version")
	case head.DefaultProfile != newest.DefaultProfile:
		t.Error("the head's profile differs from its newest version")
	case head.DefaultProfileParams != newest.DefaultProfileParams:
		t.Error("the head's parameters differ from its newest version")
	}
}

// Deleting a scenario that has been run would take the history with it.
func TestDeletingAScenarioWithRunsIsRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project, environment := newScenarioWorld(ctx, t, db)

	scenarios := postgres.NewScenarioWriteRepository(db)
	created, err := scenarios.Create(ctx, domain.Scenario{
		ID: uuid.New(), ProjectID: project, Name: "checkout",
		Script: "one", Version: 1, DefaultProfile: domain.ProfileSmoke,
		DefaultProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
	}, who, time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Unused, so it goes.
	if err := scenarios.Delete(ctx, who.OrgID, created.ID); err != nil {
		t.Fatalf("deleting an unused scenario: %v", err)
	}

	// Another one, this time with a run.
	withRun, err := scenarios.Create(ctx, domain.Scenario{
		ID: uuid.New(), ProjectID: project, Name: "health",
		Script: "one", Version: 1, DefaultProfile: domain.ProfileSmoke,
		DefaultProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
	}, who, time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := postgres.NewRunRepository(db).Create(ctx, domain.NewRun(
		who.OrgID,
		domain.Scenario{ID: withRun.ID, ProjectID: project, Version: 1},
		domain.Environment{ID: environment, ProjectID: project, MaxVUs: 5, MaxDurationS: 60},
		domain.ProfileSmoke, domain.ProfileParams{TargetVUs: 1, PlateauS: 5}, nil,
	)); err != nil {
		t.Fatalf("record a run: %v", err)
	}

	if err := scenarios.Delete(ctx, who.OrgID, withRun.ID); !errors.Is(err, domain.ErrScenarioNotEmpty) {
		t.Fatalf("got %v, want ErrScenarioNotEmpty", err)
	}
	if _, err := scenarios.ByID(ctx, who.OrgID, withRun.ID); err != nil {
		t.Errorf("the scenario was deleted anyway: %v", err)
	}
}

// Thresholds come and go without touching what a finished run recorded.
func TestDroppingAThresholdLeavesRecordedVerdictsAlone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project, environment := newScenarioWorld(ctx, t, db)

	scenarios := postgres.NewScenarioWriteRepository(db)
	created, err := scenarios.Create(ctx, domain.Scenario{
		ID: uuid.New(), ProjectID: project, Name: "checkout",
		Script: "one", Version: 1, DefaultProfile: domain.ProfileSmoke,
		DefaultProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
	}, who, time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	added, err := scenarios.Add(ctx, who.OrgID, created.ID, domain.Threshold{
		ID: uuid.New(), Metric: "http_req_duration{name:POST /login}", Expression: "p(95)<300",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	runs := postgres.NewRunRepository(db)
	recorded, err := runs.Create(ctx, domain.NewRun(
		who.OrgID,
		domain.Scenario{ID: created.ID, ProjectID: project, Version: 1},
		domain.Environment{ID: environment, ProjectID: project, MaxVUs: 5, MaxDurationS: 60},
		domain.ProfileSmoke, domain.ProfileParams{TargetVUs: 1, PlateauS: 5}, nil,
	))
	if err != nil {
		t.Fatalf("record a run: %v", err)
	}
	if err := runs.SaveResults(ctx, who.OrgID, recorded.ID, domain.RunResults{
		Thresholds: []domain.ThresholdResult{
			{Metric: added.Metric, Expression: added.Expression, Passed: false},
		},
	}, domain.Traffic{}); err != nil {
		t.Fatalf("save the results: %v", err)
	}

	if err := scenarios.Remove(ctx, who.OrgID, created.ID, added.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	results, err := runs.Results(ctx, who.OrgID, recorded.ID)
	if err != nil {
		t.Fatalf("read the results back: %v", err)
	}
	if len(results.Thresholds) != 1 {
		t.Fatalf("the run kept %d verdict(s), want 1", len(results.Thresholds))
	}
	if results.Thresholds[0].Passed {
		t.Error("the recorded verdict changed")
	}

	// And the rule itself is gone.
	remaining, err := scenarios.OfScenario(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("OfScenario: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("%d rule(s) remain", len(remaining))
	}
}

// Changing a rule rewrites the rule, never the verdict a run already recorded
// against what it used to say.
func TestChangingAThresholdLeavesRecordedVerdictsAlone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project, environment := newScenarioWorld(ctx, t, db)

	scenarios := postgres.NewScenarioWriteRepository(db)
	created, err := scenarios.Create(ctx, domain.Scenario{
		ID: uuid.New(), ProjectID: project, Name: "checkout",
		Script: "one", Version: 1, DefaultProfile: domain.ProfileSmoke,
		DefaultProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
	}, who, time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	added, err := scenarios.Add(ctx, who.OrgID, created.ID, domain.Threshold{
		ID: uuid.New(), Metric: "http_req_duration", Expression: "p(95)<300",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	runs := postgres.NewRunRepository(db)
	recorded, err := runs.Create(ctx, domain.NewRun(
		who.OrgID,
		domain.Scenario{ID: created.ID, ProjectID: project, Version: 1},
		domain.Environment{ID: environment, ProjectID: project, MaxVUs: 5, MaxDurationS: 60},
		domain.ProfileSmoke, domain.ProfileParams{TargetVUs: 1, PlateauS: 5}, nil,
	))
	if err != nil {
		t.Fatalf("record a run: %v", err)
	}
	if err := runs.SaveResults(ctx, who.OrgID, recorded.ID, domain.RunResults{
		Thresholds: []domain.ThresholdResult{
			{Metric: added.Metric, Expression: added.Expression, Passed: true},
		},
	}, domain.Traffic{}); err != nil {
		t.Fatalf("save the results: %v", err)
	}

	changed := domain.Threshold{ID: added.ID, Metric: "http_req_duration{name:POST /login}", Expression: "p(99)<500"}
	updated, err := scenarios.Update(ctx, who.OrgID, created.ID, changed)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated != changed {
		t.Errorf("Update returned %+v, want %+v", updated, changed)
	}

	results, err := runs.Results(ctx, who.OrgID, recorded.ID)
	if err != nil {
		t.Fatalf("read the results back: %v", err)
	}
	if len(results.Thresholds) != 1 || results.Thresholds[0].Metric != "http_req_duration" ||
		results.Thresholds[0].Expression != "p(95)<300" || !results.Thresholds[0].Passed {
		t.Errorf("the recorded verdict changed: %+v", results.Thresholds)
	}

	// A rule of another scenario is not reached through this one.
	other, err := scenarios.Create(ctx, domain.Scenario{
		ID: uuid.New(), ProjectID: project, Name: "search",
		Script: "two", Version: 1, DefaultProfile: domain.ProfileSmoke,
		DefaultProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
	}, who, time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := scenarios.Update(ctx, who.OrgID, other.ID, changed); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("updating a rule through another scenario: got %v, want ErrNotFound", err)
	}
}

func TestThresholdsAreCountedPerScenario(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project, _ := newScenarioWorld(ctx, t, db)

	scenarios := postgres.NewScenarioWriteRepository(db)
	create := func(name string) domain.Scenario {
		t.Helper()
		created, err := scenarios.Create(ctx, domain.Scenario{
			ID: uuid.New(), ProjectID: project, Name: name,
			Script: name, Version: 1, DefaultProfile: domain.ProfileSmoke,
			DefaultProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
		}, who, time.Now().UTC())
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		return created
	}
	two, none := create("checkout"), create("search")

	for _, expression := range []string{"p(95)<300", "p(99)<800"} {
		if _, err := scenarios.Add(ctx, who.OrgID, two.ID, domain.Threshold{
			ID: uuid.New(), Metric: "http_req_duration", Expression: expression,
		}); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	counts, err := scenarios.CountInProject(ctx, who.OrgID, project)
	if err != nil {
		t.Fatalf("CountInProject: %v", err)
	}
	if counts[two.ID] != 2 {
		t.Errorf("%s imposes %d rule(s), want 2", two.Name, counts[two.ID])
	}
	if n, ok := counts[none.ID]; ok {
		t.Errorf("%s has an entry (%d) though it imposes nothing", none.Name, n)
	}
}
