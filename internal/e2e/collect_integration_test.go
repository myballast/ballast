//go:build integration

// The ticket's acceptance criterion: a worker killed in the middle of a run
// ends with that run errored, its target free, and no container left running.
//
// What this reconstructs, and what it does not. A worker dying is staged rather
// than performed: the run is claimed the way a worker claims it, a real
// generator is started through the real runner and then abandoned — a process
// killed outright runs none of its deferred cleanups, so its container outlives
// it — and the heartbeat stops. What is not staged is anything after that: the
// collector is the real one, the container it kills is a real container loading
// a real target, and the status and the lock are read back out of PostgreSQL.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/adapter/runner/hosted"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// containersOf counts what is on the machine for one run.
func containersOf(ctx context.Context, t *testing.T, cli dockerclient.APIClient, runID uuid.UUID) int {
	t.Helper()

	found, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", hosted.RunIDLabel+"="+runID.String())),
	})
	if err != nil {
		t.Fatalf("list the containers: %v", err)
	}
	return len(found)
}

// waitForContainers waits until a run has the expected number of containers on
// the machine, which is how the test knows the abandoned generator is really
// up rather than still being created or its image still being pulled.
func waitForContainers(
	ctx context.Context, t *testing.T, cli dockerclient.APIClient, runID uuid.UUID, want int,
) {
	t.Helper()

	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		if containersOf(ctx, t, cli, runID) >= want {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatalf("waiting for the generator: %v", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}

	t.Fatalf("the generator never came up: %d container(s) for run %s",
		containersOf(ctx, t, cli, runID), runID)
}

// stopReporting is the worker's death, as the rest of Ballast sees it: the run
// is still running and nothing has said anything about it for a while.
func stopReporting(
	ctx context.Context, t *testing.T, db *postgres.DB, org, runID uuid.UUID, at time.Time,
) {
	t.Helper()

	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE run SET heartbeat_at = $3 WHERE org_id = $1 AND id = $2`, org, runID, at)
		return err
	}); err != nil {
		t.Fatalf("stop the heartbeat: %v", err)
	}
}

func lockHolders(ctx context.Context, t *testing.T, db *postgres.DB, org uuid.UUID) int {
	t.Helper()

	var held int
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM environment_lock WHERE org_id = $1`, org).Scan(&held)
	}); err != nil {
		t.Fatalf("read the locks: %v", err)
	}
	return held
}

func TestAWorkerKilledMidRunLeavesAnErroredRunAndNothingElse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
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

	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	// A run accepted the ordinary way, long enough that its generator is still
	// working when the worker dies.
	status, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":90}}`,
		scenario, environment))
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", status)
	}

	// The worker claims it: running, and its target held. This is the last
	// thing it does before dying.
	runs := postgres.NewRunRepository(db)
	claimed, found, err := runs.ClaimNextQueued(ctx, org, time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatalf("ClaimNextQueued: %v", err)
	}
	if !found || claimed.ID.String() != queued.ID {
		t.Fatalf("the worker claimed %v (found=%v), queued was %s", claimed.ID, found, queued.ID)
	}
	if held := lockHolders(ctx, t, db, org); held != 1 {
		t.Fatalf("%d target(s) held after a claim", held)
	}

	// The generator that worker started, abandoned where it stands. The run
	// never returns here and nothing cancels it: what a killed process leaves
	// is a container nobody is going to clean up.
	abandoned := make(chan struct{})
	go func() {
		defer close(abandoned)

		_, _ = s.runner.Run(context.WithoutCancel(ctx), hosted.Job{
			RunID:        claimed.ID,
			Script:       []byte(healthScript),
			Options:      generatorOptions(t, claimed),
			Env:          map[string]string{domain.BaseURLVariable: target},
			AllowedHosts: []string{hostPortOf(t, target)},
		}, nil)
	}()

	// The generator and the proxy that is its only way out.
	waitForContainers(ctx, t, cli, claimed.ID, 2)

	// And the worker goes quiet.
	stopReporting(ctx, t, db, org, claimed.ID, time.Now().UTC().Add(-10*time.Minute))

	// Another worker's collector, on its own schedule, doing what the ticker in
	// cmd/worker does.
	ended, err := s.service.Collect(ctx, org)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ended != 1 {
		t.Fatalf("%d run(s) were collected", ended)
	}

	// The three things the ticket asks for.
	collected := s.get(t, claimed.ID.String())
	if collected.Status != string(domain.StatusErrored) {
		t.Fatalf("the abandoned run is %q, wanted errored", collected.Status)
	}
	if collected.ErrorMessage == "" {
		t.Error("the abandoned run says nothing about why it ended")
	}

	if held := lockHolders(ctx, t, db, org); held != 0 {
		t.Errorf("%d target(s) are still held", held)
	}

	if left := containersOf(ctx, t, cli, claimed.ID); left != 0 {
		t.Errorf("%d container(s) of the abandoned run are still on the machine", left)
	}

	// The abandoned call returns once its container is gone from under it. Not
	// a claim being tested — it is this test cleaning up after itself before
	// the suite checks that nothing outlived it.
	select {
	case <-abandoned:
	case <-ctx.Done():
		t.Fatalf("the abandoned generator never returned: %v", ctx.Err())
	}
}

// generatorOptions builds what the worker would have handed the generator, from
// the parameters frozen on the run.
func generatorOptions(t *testing.T, claimed domain.Run) []byte {
	t.Helper()

	options, err := k6.NewOptionsBuilder().Build(claimed.Profile, claimed.ProfileParams, nil)
	if err != nil {
		t.Fatalf("build the generator options: %v", err)
	}
	return options
}

// hostPortOf is the one host:port the run may reach, as the proxy's allowlist
// holds it.
func hostPortOf(t *testing.T, target string) string {
	t.Helper()

	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("the test's own target is not a URL: %v", err)
	}
	return parsed.Host
}
