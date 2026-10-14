//go:build integration

// What a run cost, from the socket to the month's total.
//
// The ticket's two acceptance criteria are read here the way somebody would
// read them: a real run against a real target, and then the question "how much
// did this organization move this month?" asked through the API.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// volumes is what one run recorded.
type volumes struct {
	egress, ingress int64
	sent, received  int64
	measured        bool
}

func volumesOf(ctx context.Context, t *testing.T, db *postgres.DB, org, runID interface{ String() string }) volumes {
	t.Helper()

	var v volumes
	if err := db.InTenantTx(ctx, mustUUID(t, org.String()), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT egress_bytes, ingress_bytes,
			data_sent_bytes, data_received_bytes, traffic_measured
			FROM run WHERE org_id = $1 AND id = $2`,
			org.String(), runID.String()).
			Scan(&v.egress, &v.ingress, &v.sent, &v.received, &v.measured)
	}); err != nil {
		t.Fatalf("read the run's volumes: %v", err)
	}
	return v
}

// The first acceptance criterion: a finished run carries its volumes. Both
// measurements of them, taken in two places that cannot see each other.
func TestAFinishedRunCarriesWhatItMoved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Something with a body, so that what came back is worth counting.
		_, _ = w.Write([]byte(`{"status":"ok","items":[1,2,3,4,5,6,7,8,9,10]}`))
	}))

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, scenario, environment := seed(ctx, t, db, target, nil)
	s := build(t, db, org)

	status, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":2,"plateau_s":3}}`,
		scenario, environment))
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", status)
	}

	s.processOne(ctx, t)

	moved := volumesOf(ctx, t, db, s.org, mustUUID(t, queued.ID))

	switch {
	case moved.sent == 0 || moved.received == 0:
		t.Fatalf("the generator reported %+v", moved)
	case moved.egress == 0 || moved.ingress == 0:
		t.Fatalf("nothing was counted on the wire: %+v", moved)
	case !moved.measured:
		t.Fatal("a run both measurements were taken of is marked unmeasured")
	}

	// What comes back is copied through, so the wire sees it all and the
	// headers around it: never less than the generator counted.
	if moved.ingress < moved.received {
		t.Errorf("the wire counted %d bytes in and the generator claimed %d",
			moved.ingress, moved.received)
	}

	// Going out, the two are not comparable, and this target is why: it speaks
	// plain http, so the proxy re-issues each request rather than copying a
	// tunnel. What the generator counted as sent went to the proxy — a hop
	// inside this machine that leaves nothing — and what the proxy then wrote
	// to the target is smaller, having lost the absolute URL and the
	// proxy-only headers.
	//
	// That is not a measurement error: the smaller figure is the one that left,
	// and it is the one a bill is made of. Against an https target, where the
	// tunnel is copied byte for byte, the wire figure is the larger of the two
	// because it carries the TLS records as well.
	if moved.egress == moved.sent {
		t.Errorf("the two measurements of what was sent are identical (%d), "+
			"which means one of them is not being taken", moved.egress)
	}

	t.Logf("sent: %d on the wire, %d reported · received: %d on the wire, %d reported",
		moved.egress, moved.sent, moved.ingress, moved.received)
}

// The second acceptance criterion: one request gives the month's volume for an
// organization — and for one of its projects.
func TestAMonthsTrafficIsOneRequestAway(t *testing.T) {
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
	s := build(t, db, org)

	// Two finished runs, written the way the worker writes them, so that this
	// test is about the sum rather than about Docker.
	_, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenario, environment))

	runs := postgres.NewRunRepository(db)
	runID := mustUUID(t, queued.ID)
	if err := runs.SaveResults(ctx, org, runID,
		domain.RunResults{Traffic: domain.Traffic{SentBytes: 1000, ReceivedBytes: 20000, Measured: true}},
		domain.Traffic{SentBytes: 1500, ReceivedBytes: 21000, Measured: true},
	); err != nil {
		t.Fatalf("SaveResults: %v", err)
	}

	ended := time.Now().UTC()
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE run SET status = 'passed', ended_at = $3 WHERE org_id = $1 AND id = $2`,
			org, runID, ended)
		return err
	}); err != nil {
		t.Fatalf("finish the run: %v", err)
	}

	month := ended.Format("2006-01")

	var organisation struct {
		EgressBytes    int64 `json:"egress_bytes"`
		DataSentBytes  int64 `json:"data_sent_bytes"`
		Runs           int   `json:"runs"`
		UnmeasuredRuns int   `json:"unmeasured_runs"`
	}
	res := s.do(t, http.MethodGet, "/usage?month="+month, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /usage returned %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(&organisation); err != nil {
		t.Fatalf("decode the usage: %v", err)
	}
	_ = res.Body.Close()

	switch {
	case organisation.Runs != 1:
		t.Errorf("the month counted %d runs", organisation.Runs)
	case organisation.EgressBytes != 1500 || organisation.DataSentBytes != 1000:
		t.Errorf("the month counted %+v", organisation)
	case organisation.UnmeasuredRuns != 0:
		t.Errorf("%d runs were counted as unmeasured", organisation.UnmeasuredRuns)
	}

	// The same question about one project.
	var project struct {
		EgressBytes int64 `json:"egress_bytes"`
		Runs        int   `json:"runs"`
	}
	run := s.get(t, queued.ID)
	res = s.do(t, http.MethodGet,
		fmt.Sprintf("/projects/%s/usage?month=%s", run.ProjectID, month), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /projects/{id}/usage returned %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(&project); err != nil {
		t.Fatalf("decode the project's usage: %v", err)
	}
	_ = res.Body.Close()

	if project.Runs != 1 || project.EgressBytes != 1500 {
		t.Errorf("the project's month counted %+v", project)
	}

	// A month nothing ended in is zero rather than an error: nothing happened
	// is an answer.
	res = s.do(t, http.MethodGet, "/usage?month=2020-01", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /usage for an empty month returned %d", res.StatusCode)
	}
	_ = res.Body.Close()

	// And something that is not a month is refused rather than guessed at.
	res = s.do(t, http.MethodGet, "/usage?month=octobre", nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /usage?month=octobre returned %d, want 400", res.StatusCode)
	}
	_ = res.Body.Close()
}
