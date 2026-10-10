//go:build integration

// Written before the implementation. These are the ticket's three acceptance
// criteria, plus the one that matters most: that the run network has no way out
// at all, proved without going through k6.
package hosted_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/runner/hosted"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// reachScript tries one request and reports what happened, so a refusal can be
// told apart from a success by reading the generator's own output rather than by
// inferring it from an exit code.
//
// The body is printed too: when the proxy refuses, the reason is in it, and a
// test whose failure says only "not 200" is one somebody has to reproduce by
// hand before learning anything.
func reachScript(url string) []byte {
	return []byte(`
import http from 'k6/http';

export default function () {
	const res = http.get('` + url + `', { timeout: '5s' });
	console.log('BALLAST_STATUS=' + res.status +
		' BALLAST_ERROR=' + res.error +
		' BALLAST_BODY=' + String(res.body).replace(/\s+/g, ' ').slice(0, 200));
}
`)
}

func logsOf(lines []domain.LogLine) string {
	var all strings.Builder
	for _, line := range lines {
		all.WriteString(line.Message)
		all.WriteString("\n")
	}
	return all.String()
}

// The structural guarantee, and the only test here that does not involve k6: a
// container on a run's network has no route out. Everything else in this ticket
// is a second line of defence behind this one. If this fails, a script that
// ignores the proxy reaches the internet and the rest is decoration.
func TestARunNetworkHasNoWayOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner := newRunner(t, 30*time.Second)

	name, cleanup, err := runner.NewRunNetwork(ctx, uuid.New())
	if err != nil {
		t.Fatalf("create the run network: %v", err)
	}
	t.Cleanup(cleanup)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	// Docker's own view first: a network nothing can route out of.
	inspected, err := cli.NetworkInspect(ctx, name, network.InspectOptions{})
	if err != nil {
		t.Fatalf("inspect the network: %v", err)
	}
	if !inspected.Internal {
		t.Fatal("the run network is not internal; containers on it have a default route")
	}

	// And then the only thing that really settles it: try.
	output := runInNetwork(ctx, t, cli, name,
		`ip route; wget -q -O- -T 3 http://1.1.1.1/ 2>&1 || echo BALLAST_NO_EGRESS`)

	if !strings.Contains(output, "BALLAST_NO_EGRESS") {
		t.Fatalf("a container on the run network reached the internet:\n%s", output)
	}
	if strings.Contains(output, "default via") {
		t.Fatalf("the run network has a default route:\n%s", output)
	}
}

// The third acceptance criterion: the allowed host works. The target runs as a
// container on the run's own network, so its address is inside the subnet the
// worker created and the strict address guard allows it without any exception
// being configured anywhere.
func TestAScriptReachesTheHostItIsAllowed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	runner := newRunner(t, time.Minute)
	world := newNetworkWorld(ctx, t, runner)

	logs := make(chan domain.LogLine, 256)
	wg, gathered := drain(logs)

	result, err := runner.Run(ctx, hosted.Job{
		RunID:        uuid.New(),
		Script:       reachScript(world.targetURL),
		Options:      smokeOptions(t, "2s"),
		AllowedHosts: []string{world.targetHostPort},
		Network:      world.network,
	}, logs)
	close(logs)
	wg.Wait()

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.TimedOut {
		t.Fatal("the run timed out")
	}

	output := logsOf(*gathered)
	if !strings.Contains(output, "BALLAST_STATUS=200") {
		t.Fatalf("the allowed host was not reached:\n%s", output)
	}
}

// The first acceptance criterion: a host nobody allowed. The proxy refuses it,
// and the network refuses to carry it even if the proxy were bypassed.
func TestAScriptCannotReachAHostItIsNotAllowed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	runner := newRunner(t, time.Minute)
	world := newNetworkWorld(ctx, t, runner)

	logs := make(chan domain.LogLine, 256)
	wg, gathered := drain(logs)

	// The allowlist holds the target; the script asks for somewhere else.
	_, err := runner.Run(ctx, hosted.Job{
		RunID:        uuid.New(),
		Script:       reachScript("http://example.com/"),
		Options:      smokeOptions(t, "2s"),
		AllowedHosts: []string{world.targetHostPort},
		Network:      world.network,
	}, logs)
	close(logs)
	wg.Wait()

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	output := logsOf(*gathered)
	if strings.Contains(output, "BALLAST_STATUS=200") {
		t.Fatalf("a host outside the allowlist was reached:\n%s", output)
	}

	// 403 with a reason, rather than an opaque connection failure: the script
	// is told what happened, which is what somebody debugging a scenario needs.
	// This assertion started as "status 0" — a transport error — and the proxy
	// turned out to answer better than that.
	if !strings.Contains(output, "BALLAST_STATUS=403") {
		t.Fatalf("the refusal did not reach the script as a 403:\n%s", output)
	}
	if !strings.Contains(output, "is not a host this run may reach") {
		t.Fatalf("the refusal carried no reason:\n%s", output)
	}
}

// The second acceptance criterion: an address rather than a name, so the
// allowlist's host matching has nothing to say and the address guard is what
// refuses.
func TestAScriptCannotReachAPrivateAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	runner := newRunner(t, time.Minute)
	world := newNetworkWorld(ctx, t, runner)

	for _, target := range []string{
		"http://10.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1/",
	} {
		t.Run(target, func(t *testing.T) {
			logs := make(chan domain.LogLine, 256)
			wg, gathered := drain(logs)

			_, err := runner.Run(ctx, hosted.Job{
				RunID:        uuid.New(),
				Script:       reachScript(target),
				Options:      smokeOptions(t, "2s"),
				AllowedHosts: []string{world.targetHostPort},
				Network:      world.network,
			}, logs)
			close(logs)
			wg.Wait()

			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if output := logsOf(*gathered); strings.Contains(output, "BALLAST_STATUS=200") {
				t.Fatalf("%s was reached:\n%s", target, output)
			}
		})
	}
}

// Whatever a run does, the network and the proxy that carried it go with it. A
// leaked network is a leaked route, and they accumulate silently.
func TestARunLeavesNoNetworkAndNoProxyBehind(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	runner := newRunner(t, time.Minute)
	world := newNetworkWorld(ctx, t, runner)

	runID := uuid.New()
	logs := make(chan domain.LogLine, 256)
	wg, _ := drain(logs)

	_, err := runner.Run(ctx, hosted.Job{
		RunID:        runID,
		Script:       reachScript(world.targetURL),
		Options:      smokeOptions(t, "2s"),
		AllowedHosts: []string{world.targetHostPort},
		Network:      world.network,
	}, logs)
	close(logs)
	wg.Wait()

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	left, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", hosted.RunIDLabel+"="+runID.String())),
	})
	if err != nil {
		t.Fatalf("list the containers: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("%d container(s) of this run are still there", len(left))
	}
}

// networkWorld is a run network with a target container on it, which is what
// lets the allowed path be exercised without permitting a private address
// anywhere: the target's address is inside the subnet the worker just created.
type networkWorld struct {
	network        string
	targetURL      string
	targetHostPort string
}

func newNetworkWorld(ctx context.Context, t *testing.T, runner *hosted.Runner) networkWorld {
	t.Helper()

	name, cleanup, err := runner.NewRunNetwork(ctx, uuid.New())
	if err != nil {
		t.Fatalf("create the run network: %v", err)
	}
	t.Cleanup(cleanup)

	// One label, not a dotted name. Docker's embedded resolver answers a dotted
	// alias by trying to forward it upstream, and a network with no route out
	// has no upstream — so target.ballast.test comes back SERVFAIL while
	// "target" resolves locally. Found by running it.
	const alias = "target"
	startTargetContainer(ctx, t, name, alias)

	return networkWorld{
		network:        name,
		targetURL:      "http://" + alias + "/",
		targetHostPort: alias + ":80",
	}
}

// targetImage is what the test target is served from, pinned like every other
// image here.
//
// Not the generator image, and not alpine: neither carries an HTTP server. The
// k6 image has no httpd, and modern alpine moved busybox's into a package of its
// own — both checked, after two rounds of this test failing with "no such host",
// which is what Docker answers for a container that exited before it could
// register its alias.
const targetImage = "nginx:1.27-alpine"

// startTargetContainer serves on port 80.
func startTargetContainer(ctx context.Context, t *testing.T, networkName, alias string) {
	t.Helper()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	ensureTargetImage(ctx, t, cli)

	created, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:  targetImage,
			Labels: map[string]string{hosted.RunIDLabel: "network-test-target"},
		},
		&container.HostConfig{NetworkMode: container.NetworkMode(networkName)},
		&network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: {Aliases: []string{alias}},
		}},
		nil, "")
	if err != nil {
		t.Fatalf("create the target container: %v", err)
	}

	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.WithoutCancel(ctx), created.ID,
			container.RemoveOptions{Force: true})
	})

	if err := cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start the target container: %v", err)
	}
}

// ensureTargetImage pulls the target image if this machine has never seen it,
// the way the runner does for the generator: Docker fetches nothing by itself.
func ensureTargetImage(ctx context.Context, t *testing.T, cli *client.Client) {
	t.Helper()

	if _, _, err := cli.ImageInspectWithRaw(ctx, targetImage); err == nil {
		return
	}

	progress, err := cli.ImagePull(ctx, targetImage, image.PullOptions{})
	if err != nil {
		t.Fatalf("pull %s: %v", targetImage, err)
	}
	defer func() { _ = progress.Close() }()

	// Drained, because the pull is only finished when the stream ends.
	if _, err := io.Copy(io.Discard, progress); err != nil {
		t.Fatalf("pull %s: %v", targetImage, err)
	}
}

// runInNetwork runs a shell in the given network and returns everything it
// printed.
func runInNetwork(ctx context.Context, t *testing.T, cli *client.Client, networkName, script string) string {
	t.Helper()

	created, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:      hosted.DefaultImage,
			Entrypoint: []string{"sh", "-c", script},
			Labels:     map[string]string{hosted.RunIDLabel: "network-test-probe"},
			// A terminal, so the log stream comes back as plain text instead of
			// Docker's multiplexed frames. This is a probe, not a run.
			Tty: true,
		},
		&container.HostConfig{NetworkMode: container.NetworkMode(networkName)},
		nil, nil, "")
	if err != nil {
		t.Fatalf("create the probe container: %v", err)
	}
	defer func() {
		_ = cli.ContainerRemove(context.WithoutCancel(ctx), created.ID,
			container.RemoveOptions{Force: true})
	}()

	if err := cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start the probe container: %v", err)
	}

	waited, errs := cli.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("wait for the probe: %v", err)
		}
	case <-waited:
	}

	logs, err := cli.ContainerLogs(ctx, created.ID,
		container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		t.Fatalf("read the probe's output: %v", err)
	}
	defer func() { _ = logs.Close() }()

	printed, err := io.ReadAll(logs)
	if err != nil {
		t.Fatalf("read the probe's output: %v", err)
	}

	return string(printed)
}
