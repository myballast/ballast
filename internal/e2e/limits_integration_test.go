//go:build integration

// The last line in front of a loop, read the way a client meets it: a status
// code, a header, and a trace left behind.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// post sends one run request and returns the response, headers included.
func (s stack) post(t *testing.T, body string) *http.Response {
	t.Helper()

	return s.do(t, http.MethodPost, "/runs", strings.NewReader(body))
}

// finishEverything empties the queue without running anything: these tests are
// about the limits, not about Docker.
func finishEverything(ctx context.Context, t *testing.T, db *postgres.DB, org fmt.Stringer) {
	t.Helper()

	if err := db.InTenantTx(ctx, mustUUID(t, org.String()), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE run SET status = 'passed', ended_at = now() WHERE org_id = $1 AND status = 'queued'`,
			org.String())
		return err
	}); err != nil {
		t.Fatalf("empty the queue: %v", err)
	}
}

// The ticket's acceptance criterion, both halves of it: past the limit the
// answer is 429, and the attempt is in the journal.
//
// The window is filled by writing rows rather than by sending thirty requests.
// What is under test is the refusal, not the thirty runs before it — and a
// test that opens a connection per allowed run leaves a machine's worth of
// sockets behind for the rest of the suite to trip over, which is how this one
// was written the first time.
func TestPastALimitTheAnswerIs429AndItIsRecorded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, scenario, environment := seed(ctx, t, db, target, nil)
	s := build(t, db, org)

	body := fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenario, environment)

	// One run through the API, so that what follows is staged around a real
	// one and the project is known.
	first := s.post(t, body)
	_ = first.Body.Close()
	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("the first run answered %d", first.StatusCode)
	}

	// A full queue.
	fillWith(ctx, t, db, org, scenario, environment, domain.MaxQueuedRunsPerOrg-1,
		"queued", time.Now().UTC())

	refused := s.post(t, body)
	message := readError(t, refused)

	if refused.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a run past the queue's depth answered %d, want 429", refused.StatusCode)
	}
	if !strings.Contains(message, "waiting") {
		t.Errorf("the refusal says %q, which does not name the queue", message)
	}
	// Nothing is promised here, and that is deliberate: a queue empties when
	// runs finish, and a number invented to fill the header would be worse
	// than none.
	if retry := refused.Header.Get("Retry-After"); retry != "" {
		t.Errorf("a full queue promised a slot in %s", retry)
	}

	// The queue drains; the hour's count does not. Those runs still happened,
	// and the oldest of them was fifty minutes ago.
	finishEverything(ctx, t, db, s.org)
	backdateEverything(ctx, t, db, org, time.Now().UTC().Add(-50*time.Minute))
	fillWith(ctx, t, db, org, scenario, environment,
		domain.MaxRunsPerUserPerWindow-domain.MaxQueuedRunsPerOrg, "passed",
		time.Now().UTC().Add(-time.Minute))

	refused = s.post(t, body)
	message = readError(t, refused)

	if refused.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a run past the hour's allowance answered %d, want 429", refused.StatusCode)
	}
	if !strings.Contains(message, "last 1h0m0s") {
		t.Errorf("the refusal says %q, which does not name the window", message)
	}

	// Here the wait is arithmetic: the oldest run of the window leaves it an
	// hour after it was created, so about ten minutes from now.
	retry := refused.Header.Get("Retry-After")
	if retry == "" {
		t.Fatal("a rate window refused without saying when to come back")
	}
	seconds, err := strconv.Atoi(retry)
	if err != nil || seconds < 9*60 || seconds > 11*60 {
		t.Fatalf("Retry-After is %q, wanted about ten minutes", retry)
	}

	// And the journal holds both refusals, each naming its own limit.
	run := s.get(t, lastRunID(ctx, t, db, s.org))
	res := s.do(t, http.MethodGet, "/projects/"+run.ProjectID+"/audit?limit=500", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reading the journal answered %d", res.StatusCode)
	}

	var read struct {
		Entries []entry `json:"entries"`
	}
	if err := json.NewDecoder(res.Body).Decode(&read); err != nil {
		t.Fatalf("decode the journal: %v", err)
	}
	_ = res.Body.Close()

	blamed := map[string]bool{}
	for _, recorded := range read.Entries {
		if recorded.Action == string(domain.ActionRunRefused) {
			blamed[recorded.Details["limit"]] = true
		}
	}
	for _, limit := range []string{"queued", "triggered_by_user"} {
		if !blamed[limit] {
			t.Errorf("the journal records no refusal blaming %q", limit)
		}
	}
}

// fillWith writes runs the way the API would have, in the state and at the
// instant a test needs them. What the limits read is rows.
func fillWith(
	ctx context.Context, t *testing.T, db *postgres.DB,
	org, scenario, environment fmt.Stringer, how int, status string, at time.Time,
) {
	t.Helper()

	if err := db.InTenantTx(ctx, mustUUID(t, org.String()), func(tx pgx.Tx) error {
		for range how {
			if _, err := tx.Exec(ctx, `INSERT INTO run
				(org_id, project_id, scenario_id, environment_id, scenario_version,
				 profile, status, triggered_by, created_at)
			  SELECT $1, s.project_id, s.id, $2, 1, 'smoke', $3, u.id, $4
			  FROM scenario s, app_user u
			  WHERE s.org_id = $1 AND s.id = $5 AND u.org_id = $1
			  LIMIT 1`,
				org.String(), environment.String(), status, at, scenario.String()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("fill the window: %v", err)
	}
}

// backdateEverything moves every run of the organization back in time, so that
// the oldest of the window is a known distance away.
func backdateEverything(
	ctx context.Context, t *testing.T, db *postgres.DB, org fmt.Stringer, at time.Time,
) {
	t.Helper()

	if err := db.InTenantTx(ctx, mustUUID(t, org.String()), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE run SET created_at = $2 WHERE org_id = $1`,
			org.String(), at)
		return err
	}); err != nil {
		t.Fatalf("backdate the runs: %v", err)
	}
}

// What a run is actually given, when the environment allows more than this
// installation can generate. The ceilings are hard-coded for a reason: this is
// the number that reaches the generator.
func TestARunIsClampedToWhatTheInstallationCanGenerate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, scenario, environment := seed(ctx, t, db, target, nil)

	// Somebody typed a large number into the environment. That is a mistake,
	// not a permission.
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE environment SET max_vus = 10000, max_duration_s = 86400
			 WHERE org_id = $1 AND id = $2`, org, environment)
		return err
	}); err != nil {
		t.Fatalf("widen the environment: %v", err)
	}

	s := build(t, db, org)

	_, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke",
		  "params":{"target_vus":5000,"plateau_s":80000}}`, scenario, environment))

	stored := s.get(t, queued.ID)
	if stored.Params.TargetVUs != domain.MaxVUsAnywhere {
		t.Errorf("the run was written with %d virtual users, the ceiling is %d",
			stored.Params.TargetVUs, domain.MaxVUsAnywhere)
	}

	total := stored.Params.RampUpS + stored.Params.PlateauS + stored.Params.RampDownS
	if total != domain.MaxDurationAnywhereS {
		t.Errorf("the run was written to last %ds, the ceiling is %ds",
			total, domain.MaxDurationAnywhereS)
	}
}

// readError returns the message of a refused response.
func readError(t *testing.T, res *http.Response) string {
	t.Helper()
	defer func() { _ = res.Body.Close() }()

	var body struct {
		Error string `json:"error"`
	}
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &body)

	return body.Error
}

// lastRunID is any run of the organization, used to find the project whose
// journal holds the refusals.
func lastRunID(ctx context.Context, t *testing.T, db *postgres.DB, org fmt.Stringer) string {
	t.Helper()

	var id string
	if err := db.InTenantTx(ctx, mustUUID(t, org.String()), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id FROM run WHERE org_id = $1 ORDER BY created_at LIMIT 1`,
			org.String()).Scan(&id)
	}); err != nil {
		t.Fatalf("find a run: %v", err)
	}
	return id
}
