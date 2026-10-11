//go:build integration

// What a dead worker leaves on a machine, and the two ways it gets destroyed:
// by name, when a collector ends one abandoned run, and wholesale, when a
// worker starts and finds containers nobody is watching.
//
// Nothing here goes through Run: the leftovers are staged with the Docker client
// directly, because that is what they are — containers the process that made
// them is no longer around to remove.
package hosted_test

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/runner/hosted"
)

func dockerClient(t *testing.T) client.APIClient {
	t.Helper()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	return cli
}

// leftover starts a container labelled as one run's, on a network labelled the
// same way: what a worker killed mid-run leaves behind.
//
// The image is the egress proxy, for two reasons that have nothing to do with
// proxying: it is this repository's own, so `make test-int` has already built it
// and nothing is pulled, and it stays up on its own.
func leftover(ctx context.Context, t *testing.T, cli client.APIClient, runID uuid.UUID) (string, string) {
	t.Helper()

	label := map[string]string{hosted.RunIDLabel: runID.String()}

	networkName := hosted.NetworkPrefix + runID.String()
	if _, err := cli.NetworkCreate(ctx, networkName, network.CreateOptions{
		Driver:   "bridge",
		Internal: true,
		Labels:   label,
	}); err != nil {
		t.Fatalf("create the run network: %v", err)
	}

	created, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:  hosted.DefaultProxyImage,
			Env:    []string{"BALLAST_EGRESS_ALLOWED=nothing.example.test:80"},
			Labels: label,
		},
		&container.HostConfig{NetworkMode: container.NetworkMode(networkName)},
		&network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: {},
		}},
		nil, "",
	)
	if err != nil {
		t.Fatalf("create the leftover container: %v", err)
	}

	if err := cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start the leftover container: %v", err)
	}

	// Whatever the test concludes, nothing outlives it. Removing something the
	// collector already removed is not an error.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_ = cli.ContainerRemove(cleanup, created.ID, container.RemoveOptions{Force: true})
		_ = cli.NetworkRemove(cleanup, networkName)
	})

	return created.ID, networkName
}

// remains reports what is still on the machine for one run.
func remains(ctx context.Context, t *testing.T, cli client.APIClient, runID uuid.UUID) (int, int) {
	t.Helper()

	ours := filters.NewArgs(filters.Arg("label", hosted.RunIDLabel+"="+runID.String()))

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true, Filters: ours})
	if err != nil {
		t.Fatalf("list the containers: %v", err)
	}

	networks, err := cli.NetworkList(ctx, network.ListOptions{Filters: ours})
	if err != nil {
		t.Fatalf("list the networks: %v", err)
	}

	return len(containers), len(networks)
}

// One abandoned run, named: its generator and the network it lived on both go.
//
// The network matters as much as the container. Left behind, it accumulates one
// unusable internal network per crashed worker, and each one holds a slice of
// the daemon's address space.
func TestEverythingAnAbandonedRunLeftBehindIsDestroyed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cli := dockerClient(t)
	runner := newRunner(t, time.Minute)

	abandoned := uuid.New()
	leftover(ctx, t, cli, abandoned)

	if containers, networks := remains(ctx, t, cli, abandoned); containers != 1 || networks != 1 {
		t.Fatalf("the test staged %d container(s) and %d network(s)", containers, networks)
	}

	if err := runner.Destroy(ctx, abandoned); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	if containers, networks := remains(ctx, t, cli, abandoned); containers != 0 || networks != 0 {
		t.Fatalf("%d container(s) and %d network(s) outlived the run", containers, networks)
	}
}

// The sweep of a worker that has just started. It cannot ask the containers
// whether anybody is watching them, so it asks the database which runs are
// alive and destroys everything else.
func TestASweepDestroysWhatNoLiveRunOwns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cli := dockerClient(t)
	runner := newRunner(t, time.Minute)

	watched, abandoned := uuid.New(), uuid.New()
	leftover(ctx, t, cli, watched)
	leftover(ctx, t, cli, abandoned)

	// Everything else on this daemon is somebody else's: the integration
	// suites of other packages run at the same time against it, and a sweep is
	// machine-wide by nature. So the list of live runs handed over is every run
	// on the machine but the one being swept — which is what the predicate
	// under test takes anyway.
	keep := []uuid.UUID{watched}
	for _, existing := range everyRunOnTheMachine(ctx, t, cli) {
		if existing != abandoned {
			keep = append(keep, existing)
		}
	}

	destroyed, err := runner.DestroyExcept(ctx, keep)
	if err != nil {
		t.Fatalf("DestroyExcept: %v", err)
	}
	if destroyed != 1 {
		t.Fatalf("the sweep destroyed %d container(s), wanted 1", destroyed)
	}

	if containers, networks := remains(ctx, t, cli, abandoned); containers != 0 || networks != 0 {
		t.Fatalf("%d container(s) and %d network(s) of an abandoned run survived the sweep",
			containers, networks)
	}

	// The one being executed is untouched, which is the half of this that is
	// easy to get wrong: a sweep that destroys a live run's generator turns a
	// worker starting up into an outage.
	if containers, networks := remains(ctx, t, cli, watched); containers != 1 || networks != 1 {
		t.Fatalf("the sweep destroyed a live run's leftovers: %d container(s), %d network(s)",
			containers, networks)
	}
}

// Two collectors cleaning up after the same dead worker are doing the same
// work, and the slower one must not report a failure for having found nothing.
func TestDestroyingARunThatLeftNothingIsNotAFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	runner := newRunner(t, time.Minute)

	if err := runner.Destroy(ctx, uuid.New()); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
}

// everyRunOnTheMachine is every run a container on this daemon claims to belong
// to, valid identifier or not.
func everyRunOnTheMachine(ctx context.Context, t *testing.T, cli client.APIClient) []uuid.UUID {
	t.Helper()

	found, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", hosted.RunIDLabel)),
	})
	if err != nil {
		t.Fatalf("list the containers: %v", err)
	}

	var runs []uuid.UUID
	for _, existing := range found {
		if id, err := uuid.Parse(existing.Labels[hosted.RunIDLabel]); err == nil {
			runs = append(runs, id)
		}
	}
	return runs
}
