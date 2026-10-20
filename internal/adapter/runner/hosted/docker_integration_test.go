//go:build integration

package hosted_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/runner/hosted"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// The container reaches the test server through the host gateway, which Docker
// Desktop and the Linux runners both provide.
const hostAlias = "host.docker.internal"

// ourRuns is every run identifier this suite used. TestMain checks those and
// no others.
//
// The check reads the whole daemon, and `go test ./...` runs packages at the
// same time: internal/e2e drives this very runner and executes real runs, two
// containers to a run. Counting every ballast container made this suite fail
// for somebody else's run that happened to be in flight — a failure that never
// reproduces on its own and that names the wrong package.
var ourRuns sync.Map

// ourRun mints a run identifier and records it as this suite's.
//
// Every identifier in this package comes from here, which is what keeps the
// check complete: Run is handed one in its job, leftover and NewRunNetwork take
// one as an argument, so there is no other way for this package to label a
// container.
func ourRun() uuid.UUID {
	id := uuid.New()
	ourRuns.Store(id.String(), struct{}{})
	return id
}

// TestMain is where the third acceptance criterion is enforced: whatever these
// tests do, no container of theirs may outlive them.
func TestMain(m *testing.M) {
	code := m.Run()

	found, err := listBallastContainers(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not check for leftover containers: %v\n", err)
		os.Exit(code)
	}

	var leftover []types.Container
	for _, c := range found {
		if _, mine := ourRuns.Load(c.Labels[hosted.RunIDLabel]); mine {
			leftover = append(leftover, c)
		}
	}

	if len(leftover) > 0 {
		fmt.Fprintf(os.Stderr, "%d container(s) outlived the suite:\n", len(leftover))
		for _, c := range leftover {
			fmt.Fprintf(os.Stderr, "  %s run_id=%s status=%s\n",
				c.ID[:12], c.Labels[hosted.RunIDLabel], c.Status)
		}
		code = 1
	}

	os.Exit(code)
}

func listBallastContainers(ctx context.Context) ([]types.Container, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer func() { _ = cli.Close() }()

	return cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", hosted.RunIDLabel)),
	})
}

func newRunner(t *testing.T, ttl time.Duration) *hosted.Runner {
	t.Helper()

	runner, err := hosted.New(hosted.Config{
		TTL:        ttl,
		CPUs:       1,
		MemoryMB:   256,
		ExtraHosts: []string{hostAlias + ":host-gateway"},
		// These tests serve their target from the test process itself, on the
		// machine running them, so the proxy has to be allowed to reach a
		// private address. Empty in production, and the tests written for the
		// run network do not set it: those prove the strict path, with a target
		// container whose address is inside the subnet the worker created.
		AllowedCIDRs: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
	})
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	return runner
}

// reachesNothing is for the jobs below whose script makes no request at all —
// the time limit, the cancellation, the log stream. The field is required
// because a run allowed to reach nothing is a wiring mistake rather than a run,
// and naming a host these scripts never ask for says so.
var reachesNothing = []string{"unreachable.example.test:80"}

// allowing is the host:port of a target, which is what the proxy's allowlist
// holds. A run that may reach nothing is refused before it starts, so every job
// below names the one target it is about.
func allowing(t *testing.T, target string) []string {
	t.Helper()

	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("the test's own target is not a URL: %v", err)
	}
	return []string{parsed.Host}
}

// A server bound to every interface, because 127.0.0.1 is not reachable from
// inside a container.
func newTargetServer(t *testing.T, handler http.Handler) string {
	t.Helper()

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := httptest.NewUnstartedServer(handler)
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	return fmt.Sprintf("http://%s:%d", hostAlias, listener.Addr().(*net.TCPAddr).Port)
}

func smokeOptions(t *testing.T, duration string) []byte {
	t.Helper()

	raw, err := json.Marshal(map[string]any{
		"scenarios": map[string]any{
			"smoke": map[string]any{
				"executor":     "constant-vus",
				"vus":          1,
				"duration":     duration,
				"gracefulStop": "0s",
			},
		},
	})
	if err != nil {
		t.Fatalf("build the options: %v", err)
	}
	return raw
}

func drain(logs <-chan domain.LogLine) (*sync.WaitGroup, *[]domain.LogLine) {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		gathered []domain.LogLine
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for line := range logs {
			mu.Lock()
			gathered = append(gathered, line)
			mu.Unlock()
		}
	}()

	return &wg, &gathered
}

// The first acceptance criterion: a smoke run against a local server produces a
// summary.
func TestSmokeRunProducesASummary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var hits int
	var mu sync.Mutex
	target := newTargetServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	script := []byte(`
import http from 'k6/http';
import { check, sleep } from 'k6';

export function setup() {
	return { token: 'a-token-setup-returned' };
}

export default function () {
	const res = http.get(` + "`${__ENV.BASE_URL}/health`" + `);
	check(res, { 'status is 200': (r) => r.status === 200 });
	sleep(0.2);
}
`)

	logs := make(chan domain.LogLine, 256)
	wg, gathered := drain(logs)

	result, err := newRunner(t, 2*time.Minute).Run(ctx, hosted.Job{
		RunID:        ourRun(),
		Script:       script,
		Options:      smokeOptions(t, "3s"),
		Env:          map[string]string{"BASE_URL": target},
		AllowedHosts: allowing(t, target),
	}, logs)
	close(logs)
	wg.Wait()

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("k6 exited with %d", result.ExitCode)
	}
	if result.TimedOut {
		t.Error("the run reported a timeout it did not have")
	}
	if len(result.Summary) == 0 {
		t.Fatal("no summary was produced")
	}

	mu.Lock()
	served := hits
	mu.Unlock()
	if served == 0 {
		t.Error("the container never reached the test server")
	}

	var summary struct {
		Metrics   map[string]any `json:"metrics"`
		SetupData struct {
			Token string `json:"token"`
		} `json:"setup_data"`
	}
	if err := json.Unmarshal(result.Summary, &summary); err != nil {
		t.Fatalf("the summary is not JSON: %v", err)
	}
	if len(summary.Metrics) == 0 {
		t.Error("the summary carries no metric")
	}
	// The raw summary still holds it. Dropping it is the parser's job, and this
	// records that the runner hands over the unfiltered file.
	if summary.SetupData.Token != "a-token-setup-returned" {
		t.Error("the raw summary should still carry setup_data at this stage")
	}

	if len(*gathered) == 0 {
		t.Error("no log line was streamed")
	}
}

// The user's own options must be inert: k6 reads them from the entry module
// Ballast generates, and nowhere else.
func TestUserOptionsAreIgnored(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	target := newTargetServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Five hundred virtual users for an hour, and a threshold that can never
	// pass. None of it may happen.
	script := []byte(`
import http from 'k6/http';
export const options = {
	vus: 500,
	duration: '1h',
	thresholds: { iterations: ['count>99999999'] },
};
export default function () {
	http.get(` + "`${__ENV.BASE_URL}/`" + `);
}
`)

	started := time.Now()
	result, err := newRunner(t, 2*time.Minute).Run(ctx, hosted.Job{
		RunID:        ourRun(),
		Script:       script,
		Options:      smokeOptions(t, "3s"),
		Env:          map[string]string{"BASE_URL": target},
		AllowedHosts: allowing(t, target),
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if elapsed := time.Since(started); elapsed > time.Minute {
		t.Errorf("the run took %s; the script's own duration took effect", elapsed)
	}

	var summary struct {
		Metrics map[string]struct {
			Values map[string]float64 `json:"values"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(result.Summary, &summary); err != nil {
		t.Fatalf("the summary is not JSON: %v", err)
	}

	if peak := summary.Metrics["vus_max"].Values["value"]; peak > 1 {
		t.Errorf("the run reached %v virtual users; Ballast asked for 1", peak)
	}
}

// The second acceptance criterion.
func TestTTLKillsTheContainer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	script := []byte(`
import { sleep } from 'k6';
export default function () { sleep(60); }
`)

	started := time.Now()
	result, err := newRunner(t, 5*time.Second).Run(ctx, hosted.Job{
		RunID:        ourRun(),
		Script:       script,
		Options:      smokeOptions(t, "120s"),
		AllowedHosts: reachesNothing,
	}, nil)
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.TimedOut {
		t.Errorf("the run was not reported as timed out, exit code %d", result.ExitCode)
	}
	if elapsed > time.Minute {
		t.Errorf("the container lived %s despite a 5 second limit", elapsed)
	}
}

// Cancelling the caller's context must still take the container down. The
// cleanup deliberately does not run on that context, or it would be cancelled
// alongside the thing it is meant to clean up.
func TestContainerIsRemovedWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	script := []byte(`
import { sleep } from 'k6';
export default function () { sleep(60); }
`)

	runID := ourRun()

	done := make(chan error, 1)
	go func() {
		_, err := newRunner(t, 2*time.Minute).Run(ctx, hosted.Job{
			RunID:        runID,
			Script:       script,
			Options:      smokeOptions(t, "120s"),
			AllowedHosts: reachesNothing,
		}, nil)
		done <- err
	}()

	time.Sleep(10 * time.Second)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Run reported success for a cancelled context")
		}
	case <-time.After(time.Minute):
		t.Fatal("Run did not return after its context was cancelled")
	}

	// The daemon needs a moment to finish the removal.
	deadline := time.Now().Add(30 * time.Second)
	for {
		containers, err := listBallastContainers(context.Background())
		if err != nil {
			t.Fatalf("list containers: %v", err)
		}

		found := false
		for _, c := range containers {
			if c.Labels[hosted.RunIDLabel] == runID.String() {
				found = true
			}
		}
		if !found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the container for run %s survived its cancellation", runID)
		}
		time.Sleep(time.Second)
	}
}

func TestLogsAreStreamedWithTheirStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	script := []byte(`
export default function () {
	console.log('a line on stdout');
	console.error('a line on stderr');
}
`)

	logs := make(chan domain.LogLine, 256)
	wg, gathered := drain(logs)

	if _, err := newRunner(t, time.Minute).Run(ctx, hosted.Job{
		RunID:        ourRun(),
		Script:       script,
		Options:      smokeOptions(t, "1s"),
		AllowedHosts: reachesNothing,
	}, logs); err != nil {
		t.Fatalf("Run: %v", err)
	}
	close(logs)
	wg.Wait()

	var sawMessage bool
	seen := map[string]bool{}
	for _, line := range *gathered {
		seen[line.Stream] = true

		if strings.Contains(line.Message, "a line on stdout") {
			sawMessage = true
		}
		if line.At.IsZero() {
			t.Error("a log line carries no timestamp")
		}
		if strings.Contains(line.Message, "\n") {
			t.Errorf("a log line was not split: %q", line.Message)
		}
	}

	if !sawMessage {
		t.Errorf("the script's own output never arrived; got %d lines", len(*gathered))
	}
	for _, stream := range []string{domain.StreamStdout, domain.StreamStderr} {
		if !seen[stream] {
			t.Errorf("nothing arrived on %s", stream)
		}
	}
}
