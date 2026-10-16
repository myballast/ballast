//go:build integration

package postgres_test

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

var listParams = domain.ProfileParams{TargetVUs: 1, PlateauS: 10}

// secondProject lays down another project in the organization seedProject made,
// with its own scenario and environment. It is torn down with the organization.
func secondProject(ctx context.Context, t *testing.T, db *postgres.DB, org uuid.UUID, user uuid.UUID) (domain.Scenario, domain.Environment) {
	t.Helper()

	var project, scenario, environment = uuid.New(), uuid.New(), uuid.New()
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		for _, statement := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, 'other', $3)`, []any{project, org, project.String()}},
			{`INSERT INTO environment
				(id, org_id, project_id, name, base_url, verification_token,
				 verification_status, verified_at, verified_by, max_vus, max_duration_s)
			  VALUES ($1, $2, $3, 'prod', 'https://prod.example.test', 'token', 'verified', now(), $4, 10, 60)`,
				[]any{environment, org, project, user}},
			{`INSERT INTO scenario (id, org_id, project_id, name, script)
			  VALUES ($1, $2, $3, 'search', 'export default function () {}')`, []any{scenario, org, project}},
			{`INSERT INTO scenario_version (org_id, scenario_id, version, script, default_profile)
			  VALUES ($1, $2, 1, 'export default function () {}', 'smoke')`, []any{org, scenario}},
		} {
			if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed a second project: %v", err)
	}

	return domain.Scenario{ID: scenario, ProjectID: project, Version: 1},
		domain.Environment{ID: environment, ProjectID: project, MaxVUs: 10, MaxDurationS: 60}
}

func userOf(ctx context.Context, t *testing.T, db *postgres.DB, org uuid.UUID) uuid.UUID {
	t.Helper()

	var user uuid.UUID
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM app_user WHERE org_id = $1 LIMIT 1`, org).Scan(&user)
	}); err != nil {
		t.Fatalf("find the seeded user: %v", err)
	}
	return user
}

// Every run takes the next number of its project, and a project counts from
// one whatever the others hold.
func TestRunsAreNumberedWithinTheirProject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	runs := postgres.NewRunRepository(db)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)
	otherScenario, otherEnvironment := secondProject(ctx, t, db, org, userOf(ctx, t, db, org))

	create := func(s domain.Scenario, e domain.Environment) int {
		t.Helper()
		created, err := runs.Create(ctx, domain.NewRun(org, s, e, domain.ProfileSmoke, listParams, nil))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		return created.Number
	}

	got := []int{create(scenario, environment), create(scenario, environment), create(otherScenario, otherEnvironment), create(scenario, environment)}
	want := []int{1, 2, 1, 3}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("numbers %v, want %v: each project counts from one, and a run takes the next", got, want)
		}
	}

	// The number is read back, not only returned by Create.
	listed, err := runs.InProject(ctx, org, scenario.ProjectID, run.Filter{}, run.Window{}, 10)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(listed) != 3 || listed[0].Number != 3 || listed[2].Number != 1 {
		t.Errorf("the list carries numbers %v, want 3, 2, 1", numbers(listed))
	}
}

// The project's row is locked until the transaction that numbers a run ends, so
// runs created at the same instant are numbered one after the other: no two
// alike, and none skipped.
func TestConcurrentRunsGetDistinctConsecutiveNumbers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db := openDB(ctx, t)
	runs := postgres.NewRunRepository(db)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)

	const n = 50
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		numbers []int
		failed  error
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := runs.Create(ctx, domain.NewRun(org, scenario, environment, domain.ProfileSmoke, listParams, nil))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = err
				return
			}
			numbers = append(numbers, created.Number)
		}()
	}
	wg.Wait()

	if failed != nil {
		t.Fatalf("Create: %v", failed)
	}
	seen := map[int]bool{}
	for _, number := range numbers {
		if seen[number] {
			t.Errorf("number %d was given twice", number)
		}
		seen[number] = true
	}
	for want := 1; want <= n; want++ {
		if !seen[want] {
			t.Errorf("number %d was never given: the numbers have a gap", want)
		}
	}
}

// A run that is deleted does not give its number back: "#12" must not come to
// mean another run.
func TestADeletedRunsNumberIsNotReused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	runs := postgres.NewRunRepository(db)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)

	first, err := runs.Create(ctx, domain.NewRun(org, scenario, environment, domain.ProfileSmoke, listParams, nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM run WHERE org_id = $1 AND id = $2`, org, first.ID)
		return err
	}); err != nil {
		t.Fatalf("delete the run: %v", err)
	}

	second, err := runs.Create(ctx, domain.NewRun(org, scenario, environment, domain.ProfileSmoke, listParams, nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if second.Number != first.Number+1 {
		t.Errorf("the next run is #%d after #%d was deleted, want #%d", second.Number, first.Number, first.Number+1)
	}
}

// Each filter narrows the list to what matches, and they combine. A scenario of
// another project matches nothing here, whatever its id.
func TestTheRunListCanBeFiltered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	runs := postgres.NewRunRepository(db)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)
	otherScenario, otherEnvironment := secondProject(ctx, t, db, org, userOf(ctx, t, db, org))

	// A second scenario and environment in the first project, to filter between.
	scenarioB, environmentB := uuid.New(), uuid.New()
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO scenario (id, org_id, project_id, name, script)
			VALUES ($1, $2, $3, 'search', 'x')`, scenarioB, org, scenario.ProjectID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO scenario_version (org_id, scenario_id, version, script, default_profile)
			VALUES ($1, $2, 1, 'x', 'smoke')`, org, scenarioB); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO environment
			(id, org_id, project_id, name, base_url, verification_token, verification_status, max_vus, max_duration_s)
			VALUES ($1, $2, $3, 'preview', 'https://preview.example.test', 'token', 'pending', 5, 60)`,
			environmentB, org, scenario.ProjectID)
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	scenarioBDomain := domain.Scenario{ID: scenarioB, ProjectID: scenario.ProjectID, Version: 1}
	environmentBDomain := domain.Environment{ID: environmentB, ProjectID: scenario.ProjectID, MaxVUs: 5, MaxDurationS: 60}

	for _, c := range []struct {
		scenario    domain.Scenario
		environment domain.Environment
		status      domain.RunStatus
	}{
		{scenario, environment, domain.StatusPassed},
		{scenario, environment, domain.StatusFailed},
		{scenarioBDomain, environment, domain.StatusFailed},
		{scenario, environmentBDomain, domain.StatusPassed},
		{otherScenario, otherEnvironment, domain.StatusFailed},
	} {
		created, err := runs.Create(ctx, domain.NewRun(org, c.scenario, c.environment, domain.ProfileSmoke, listParams, nil))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE run SET status = $3 WHERE org_id = $1 AND id = $2`, org, created.ID, string(c.status))
			return err
		}); err != nil {
			t.Fatalf("set the status: %v", err)
		}
	}

	count := func(filter run.Filter) int {
		t.Helper()
		got, err := runs.InProject(ctx, org, scenario.ProjectID, filter, run.Window{}, 50)
		if err != nil {
			t.Fatalf("InProject(%+v): %v", filter, err)
		}
		return len(got)
	}

	failed, passed := domain.StatusFailed, domain.StatusPassed
	for name, c := range map[string]struct {
		filter run.Filter
		want   int
	}{
		"nothing narrows":           {run.Filter{}, 4},
		"a status":                  {run.Filter{Status: failed}, 2},
		"another status":            {run.Filter{Status: passed}, 2},
		"a scenario":                {run.Filter{ScenarioID: &scenarioB}, 1},
		"an environment":            {run.Filter{EnvironmentID: &environmentB}, 1},
		"all three":                 {run.Filter{Status: failed, ScenarioID: &scenario.ID, EnvironmentID: &environment.ID}, 1},
		"a scenario of another":     {run.Filter{ScenarioID: &otherScenario.ID}, 0},
		"an environment of another": {run.Filter{EnvironmentID: &otherEnvironment.ID}, 0},
		"a status nobody has":       {run.Filter{Status: domain.StatusOrphaned}, 0},
	} {
		if got := count(c.filter); got != c.want {
			t.Errorf("%s: %d runs, want %d", name, got, c.want)
		}
	}
}

// A row says who started the run and the two numbers k6 reported; a run with
// nothing to report has none, which is not the same as zero.
func TestTheRunListCarriesWhoStartedARunAndItsHeadlines(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	runs := postgres.NewRunRepository(db)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)
	user := userOf(ctx, t, db, org)
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE app_user SET name = 'Ada Lovelace' WHERE org_id = $1 AND id = $2`, org, user)
		return err
	}); err != nil {
		t.Fatalf("name the user: %v", err)
	}

	finished, err := runs.Create(ctx, domain.NewRun(org, scenario, environment, domain.ProfileSmoke, listParams, &user))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waiting, err := runs.Create(ctx, domain.NewRun(org, scenario, environment, domain.ProfileSmoke, listParams, nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		// The whole run's p95 and error rate, and a tagged p95 that must not be
		// mistaken for it.
		for _, m := range []struct {
			name, tag, stat string
			value           float64
		}{
			{"http_req_duration", "", "p(95)", 812.5},
			{"http_req_duration", "name:POST /login", "p(95)", 9999},
			{"http_req_failed", "", "rate", 0.048},
		} {
			if _, err := tx.Exec(ctx, `INSERT INTO run_metric (org_id, run_id, name, tag_name, stat, value)
				VALUES ($1, $2, $3, $4, $5, $6)`, org, finished.ID, m.name, m.tag, m.stat, m.value); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("record the metrics: %v", err)
	}

	listed, err := runs.InProject(ctx, org, scenario.ProjectID, run.Filter{}, run.Window{}, 10)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != waiting.ID || listed[1].ID != finished.ID {
		t.Fatalf("got %v, want the waiting run then the finished one", ids(listed))
	}

	if listed[0].P95Ms != nil || listed[0].ErrorRate != nil || listed[0].TriggeredByName != "" {
		t.Errorf("a run with nothing to report carries %+v", listed[0])
	}
	got := listed[1]
	if got.P95Ms == nil || *got.P95Ms != 812.5 || got.ErrorRate == nil || *got.ErrorRate != 0.048 {
		t.Errorf("headlines p95=%v error=%v, want 812.5 and 0.048", got.P95Ms, got.ErrorRate)
	}
	if got.TriggeredByName != "Ada Lovelace" {
		t.Errorf("triggered by %q, want the user's name", got.TriggeredByName)
	}
}

// A run is reached from a cursor in both directions: forward through the history
// and back again, the same pages each way and every run once, including two runs
// created in the same instant.
func TestTheRunListPagesBothWays(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	runs := postgres.NewRunRepository(db)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)

	for range 7 {
		if _, err := runs.Create(ctx, domain.NewRun(org, scenario, environment, domain.ProfileSmoke, listParams, nil)); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	// Three runs share an instant, straddling a page boundary of two.
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE run SET created_at = '2000-01-01T00:00:00Z'
			WHERE org_id = $1 AND project_id = $2 AND number IN (2, 3, 4)`, org, scenario.ProjectID)
		return err
	}); err != nil {
		t.Fatalf("give three runs the same instant: %v", err)
	}

	const size = 3 // asked as size+1, as the service does
	var (
		forward [][]uuid.UUID
		before  *run.Cursor
	)
	for range 10 {
		got, err := runs.InProject(ctx, org, scenario.ProjectID, run.Filter{}, run.Window{Before: before}, size+1)
		if err != nil {
			t.Fatalf("InProject: %v", err)
		}
		more := len(got) > size
		if more {
			got = got[:size]
		}
		forward = append(forward, ids(got))
		if !more {
			break
		}
		last := got[len(got)-1]
		before = &run.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if len(forward) != 3 {
		t.Fatalf("7 runs in pages of 3 made %d pages: %v", len(forward), forward)
	}

	// Walk back from the oldest page. Each step asks for the runs newer than the
	// first run of the page we are on, and must land on the page before it.
	oldest, err := runs.InProject(ctx, org, scenario.ProjectID, run.Filter{}, run.Window{Before: cursorOf(ctx, t, runs, org, scenario.ProjectID, forward[1])}, size+1)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	first := oldest[0]
	back, err := runs.InProject(ctx, org, scenario.ProjectID, run.Filter{},
		run.Window{After: &run.Cursor{CreatedAt: first.CreatedAt, ID: first.ID}}, size+1)
	if err != nil {
		t.Fatalf("InProject (after): %v", err)
	}
	// size+1 asked, newest first, the newest being the extra one that says there
	// is more: dropping it leaves the page that came before.
	if len(back) != size+1 {
		t.Fatalf("reading back returned %d runs, want %d", len(back), size+1)
	}
	if got := ids(back[1:]); !sameIDs(got, forward[1]) {
		t.Errorf("going back from page 3 gave %v, want page 2 %v", got, forward[1])
	}
}

func cursorOf(ctx context.Context, t *testing.T, runs *postgres.RunRepository, org, project uuid.UUID, page []uuid.UUID) *run.Cursor {
	t.Helper()
	all, err := runs.InProject(ctx, org, project, run.Filter{}, run.Window{}, 50)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	for _, l := range all {
		if l.ID == page[len(page)-1] {
			return &run.Cursor{CreatedAt: l.CreatedAt, ID: l.ID}
		}
	}
	t.Fatalf("run %s is not in the list", page[len(page)-1])
	return nil
}

func sameIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func numbers(listed []run.Listed) []int {
	out := make([]int, len(listed))
	for i, l := range listed {
		out[i] = l.Number
	}
	return out
}

// The ticket's bar: with a thousand runs in the project, a page of the list
// answers in under 300 ms, filtered or not, deep in the history or at its head.
func TestAPageOfAThousandRunsAnswersInUnder300Milliseconds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db := openDB(ctx, t)
	runs := postgres.NewRunRepository(db)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)
	user := userOf(ctx, t, db, org)

	// A thousand finished runs, a minute apart, each with the two numbers the
	// list reads. Written in bulk: the point is the read.
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO run (org_id, project_id, scenario_id, environment_id, scenario_version, profile,
			                 profile_params, status, triggered_by, number, created_at, started_at, ended_at)
			SELECT $1, $2, $3, $4, 1, 'smoke', '{"target_vus":10,"plateau_s":60}'::jsonb,
			       (ARRAY['passed','failed','errored'])[1 + n % 3], $5, n,
			       now() - make_interval(mins => 1001 - n),
			       now() - make_interval(mins => 1001 - n),
			       now() - make_interval(mins => 1001 - n) + interval '8 minutes'
			FROM generate_series(1, 1000) AS n`,
			org, scenario.ProjectID, scenario.ID, environment.ID, user); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO run_metric (org_id, run_id, name, tag_name, stat, value)
			SELECT org_id, id, 'http_req_duration', '', 'p(95)', 300 + number FROM run WHERE org_id = $1
			UNION ALL
			SELECT org_id, id, 'http_req_failed', '', 'rate', (number % 50) / 100.0 FROM run WHERE org_id = $1`, org); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE project SET run_counter = 1000 WHERE org_id = $1 AND id = $2`, org, scenario.ProjectID)
		return err
	}); err != nil {
		t.Fatalf("write a thousand runs: %v", err)
	}

	// Deep in the history: the cursor of a run near the oldest.
	var deepCursor run.Cursor
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT created_at, id FROM run WHERE org_id = $1 AND number = 30`, org).
			Scan(&deepCursor.CreatedAt, &deepCursor.ID)
	}); err != nil {
		t.Fatalf("find a deep run: %v", err)
	}

	failed := domain.StatusFailed
	for name, c := range map[string]struct {
		filter run.Filter
		window run.Window
	}{
		"the newest page":     {run.Filter{}, run.Window{}},
		"deep in the history": {run.Filter{}, run.Window{Before: &deepCursor}},
		"filtered by status":  {run.Filter{Status: failed}, run.Window{}},
		"filtered by scenario and environment": {
			run.Filter{ScenarioID: &scenario.ID, EnvironmentID: &environment.ID}, run.Window{},
		},
	} {
		var slowest time.Duration
		for range 5 {
			start := time.Now()
			got, err := runs.InProject(ctx, org, scenario.ProjectID, c.filter, c.window, 26)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if len(got) == 0 {
				t.Fatalf("%s: no runs", name)
			}
			slowest = max(slowest, elapsed)
		}
		if slowest > 300*time.Millisecond {
			t.Errorf("%s: slowest of 5 reads took %v, want under 300ms", name, slowest)
		}
		t.Logf("%s: slowest of 5 reads %v", name, slowest.Round(time.Millisecond))
	}
}

// The number is the table's to give, not the repository's: a run inserted by any
// other path — a script, a later feature — is numbered all the same.
func TestARunInsertedByHandIsNumberedAllTheSame(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)

	var got []int
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		for range 2 {
			var number int
			if err := tx.QueryRow(ctx, `INSERT INTO run (org_id, project_id, scenario_id, environment_id, scenario_version, profile)
				VALUES ($1, $2, $3, $4, 1, 'smoke') RETURNING number`,
				org, scenario.ProjectID, scenario.ID, environment.ID).Scan(&number); err != nil {
				return err
			}
			got = append(got, number)
		}
		return nil
	}); err != nil {
		t.Fatalf("insert runs by hand: %v", err)
	}

	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("runs inserted by hand were numbered %v, want 1 then 2", got)
	}
}

// The figures of the project's page are the runs of its list, added up: written
// down from a project whose runs are known, worked out by hand, and checked
// against the list as well as against the arithmetic.
func TestTheStatsAreTheListedRunsAddedUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	runs := postgres.NewRunRepository(db)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)

	now := time.Date(2026, 10, 16, 15, 30, 0, 0, time.UTC)
	at := func(daysAgo int, hour, minute int) time.Time {
		return time.Date(2026, 10, 16, hour, minute, 0, 0, time.UTC).AddDate(0, 0, -daysAgo)
	}

	type known struct {
		daysAgo    int
		status     string
		startedMin int // minutes after creation the run started, -1 if it never did
		lastedS    int // seconds it ran
		p95        float64
	}
	// Today: 1 passed, 1 failed, 1 queued. Yesterday: 1 errored, 1 cancelled.
	// 31 days ago, so in the window before: 3 passed and 1 timed out. A run 100
	// days ago, in neither.
	data := []known{
		{0, "passed", 0, 60, 400}, {0, "failed", 5, 120, 600}, {0, "queued", -1, 0, 0},
		{1, "errored", 0, 30, 0}, {1, "cancelled", 0, 10, 0},
		{31, "passed", 0, 90, 300}, {31, "passed", 0, 90, 300}, {31, "passed", 0, 90, 300}, {31, "timeout", 0, 150, 0},
		{100, "passed", 0, 60, 100},
	}

	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		for i, d := range data {
			created := at(d.daysAgo, 10, i)
			id := uuid.New()
			var started, ended *time.Time
			if d.startedMin >= 0 {
				s := created.Add(time.Duration(d.startedMin) * time.Minute)
				e := s.Add(time.Duration(d.lastedS) * time.Second)
				started, ended = &s, &e
			}
			if _, err := tx.Exec(ctx, `INSERT INTO run
				(id, org_id, project_id, scenario_id, environment_id, scenario_version, profile, status, created_at, started_at, ended_at)
				VALUES ($1, $2, $3, $4, $5, 1, 'smoke', $6, $7, $8, $9)`,
				id, org, scenario.ProjectID, scenario.ID, environment.ID, d.status, created, started, ended); err != nil {
				return err
			}
			if d.p95 > 0 {
				if _, err := tx.Exec(ctx, `INSERT INTO run_metric (org_id, run_id, name, tag_name, stat, value)
					VALUES ($1, $2, 'http_req_duration', '', 'p(95)', $3)`, org, id, d.p95); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("write the known runs: %v", err)
	}

	periods := domain.PeriodsOf(now, 30)
	rows, err := runs.DailyStats(ctx, org, scenario.ProjectID, periods.Previous.From, periods.Current.To)
	if err != nil {
		t.Fatalf("DailyStats: %v", err)
	}
	stats := domain.SummarizeStats(periods, rows)

	want := func(name string, got *float64, value float64) {
		t.Helper()
		if got == nil || math.Abs(*got-value) > 1e-9 {
			t.Errorf("%s = %v, want %v", name, deref(got), value)
		}
	}
	want("total runs", stats.TotalRuns.Value, 5)
	want("failed runs", stats.FailedRuns.Value, 2)
	// One passed of the three that ended with a verdict: the queued and the
	// cancelled run are not among them.
	want("pass rate", stats.PassRate.Value, 1.0/3.0)
	want("average duration", stats.AvgDurationS.Value, 70)
	want("p95", stats.P95Ms.Value, 500)
	want("previous total", stats.TotalRuns.Previous, 4)
	want("total change", stats.TotalRuns.Change, 25)
	// 3 of 4 passed in the window before: down from 75% to a third.
	want("pass rate change", stats.PassRate.Change, (1.0/3.0-0.75)*100)
	want("p95 change", stats.P95Ms.Change, 200)

	// The same figures, rebuilt from the list the page shows next to them.
	listed, err := runs.InProject(ctx, org, scenario.ProjectID, run.Filter{}, run.Window{}, 50)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	var total, passed, failed int
	for _, l := range listed {
		if l.CreatedAt.Before(periods.Current.From) || !l.CreatedAt.Before(periods.Current.To) {
			continue
		}
		total++
		switch l.Status {
		case domain.StatusPassed:
			passed++
		case domain.StatusFailed, domain.StatusErrored, domain.StatusTimeout:
			failed++
		}
	}
	if float64(total) != *stats.TotalRuns.Value || float64(failed) != *stats.FailedRuns.Value ||
		float64(passed)/float64(passed+failed) != *stats.PassRate.Value {
		t.Errorf("the list holds %d runs, %d passed, %d failed; the stats say %v, %v, %v",
			total, passed, failed, deref(stats.TotalRuns.Value), deref(stats.FailedRuns.Value), deref(stats.PassRate.Value))
	}

	// Another organization, asking for this project's figures, adds up nothing.
	stranger := uuid.New()
	seedProject(ctx, t, db, stranger)
	leaked, err := runs.DailyStats(ctx, stranger, scenario.ProjectID, periods.Previous.From, periods.Current.To)
	if err != nil {
		t.Fatalf("DailyStats from another organization: %v", err)
	}
	if len(leaked) != 0 {
		t.Errorf("another organization added up %d days of this project's runs", len(leaked))
	}
}

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
