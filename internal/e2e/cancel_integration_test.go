//go:build integration

// Cancelling, from the HTTP request to the dead container.
//
// The ticket's acceptance criterion is a duration, so it is measured here
// rather than reasoned about: a real k6 container loading a real target, a real
// DELETE through the real router, and a clock reading how long the run took to
// reach cancelled with its target free and nothing left on the machine.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// cancel sends DELETE /runs/{id} and returns the status code with the body.
func (s stack) cancel(t *testing.T, id string) (int, dto.CancelRunResponse) {
	t.Helper()

	res := s.do(t, http.MethodDelete, "/runs/"+id, nil)
	defer func() { _ = res.Body.Close() }()

	var decoded dto.CancelRunResponse
	_ = json.NewDecoder(res.Body).Decode(&decoded)

	return res.StatusCode, decoded
}

// executing runs the worker's one step in the background, the way cmd/worker
// does, and returns a channel closed when it is done.
func (s stack) executing(ctx context.Context, t *testing.T) <-chan struct{} {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)

		logs := make(chan domain.LogLine, 512)
		var drained sync.WaitGroup
		drained.Add(1)
		go func() {
			defer drained.Done()

			// Counted rather than kept: the worker drops the generator's
			// output too, and storing it is the live log view's ticket. The
			// channel still has to be drained or the execution blocks on it.
			var lines int
			for range logs {
				lines++
			}
			_ = lines
		}()

		_, err := s.service.ProcessNext(ctx, s.org, logs)
		close(logs)
		drained.Wait()

		if err != nil {
			t.Errorf("ProcessNext: %v", err)
		}
	}()

	return done
}

// waitForStatus waits for a run to reach one of the given statuses, and reports
// how long that took.
func (s stack) waitForStatus(
	ctx context.Context, t *testing.T, id string, within time.Duration, wanted ...string,
) (time.Duration, dto.RunResponse) {
	t.Helper()

	from := time.Now()
	deadline := from.Add(within)

	for {
		current := s.get(t, id)
		for _, want := range wanted {
			if current.Status == want {
				return time.Since(from), current
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("the run is %q after %s, wanted one of %v",
				current.Status, time.Since(from).Round(time.Millisecond), wanted)
		}

		select {
		case <-ctx.Done():
			t.Fatalf("waiting for the run: %v", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// The acceptance criterion, measured: under five seconds from the request to a
// cancelled run with nothing of it left.
func TestCancellingARunUnderWayStopsItWithinFiveSeconds(t *testing.T) {
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

	// Long enough that the run is certainly still going when it is cancelled,
	// and far short of the environment's ceiling.
	status, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":60}}`,
		scenario, environment))
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", status)
	}

	executing := s.executing(ctx, t)

	// Under way: the status says so, and a container exists to prove it. The
	// wait is generous because a cold machine pulls the generator image first;
	// the five seconds are measured from the request, not from here.
	runID, err := uuid.Parse(queued.ID)
	if err != nil {
		t.Fatalf("the queued run's id: %v", err)
	}
	if _, _ = s.waitForStatus(ctx, t, queued.ID, 4*time.Minute, string(domain.StatusRunning)); true {
		waitForContainers(ctx, t, cli, runID, 2)
	}

	// The request, and the clock.
	code, cancelled := s.cancel(t, queued.ID)
	if code != http.StatusAccepted {
		t.Fatalf("DELETE /runs/%s returned %d, wanted 202 for a run being executed", queued.ID, code)
	}
	if cancelled.Outcome != string(domain.CancellationRequested) {
		t.Fatalf("the outcome is %q", cancelled.Outcome)
	}

	took, ended := s.waitForStatus(ctx, t, queued.ID, 5*time.Second, string(domain.StatusCancelled))
	t.Logf("the run reached cancelled %s after the request", took.Round(time.Millisecond))

	if ended.ErrorMessage == "" {
		t.Error("the cancelled run says nothing about why it ended")
	}
	if ended.CancelRequestedAt == nil {
		t.Error("the cancelled run does not say when it was asked to stop")
	}

	// The two things the criterion asks for besides the status.
	if left := containersOf(ctx, t, cli, runID); left != 0 {
		t.Errorf("%d container(s) of the cancelled run are still on the machine", left)
	}
	if held := lockHolders(ctx, t, db, org); held != 0 {
		t.Errorf("%d target(s) are still held", held)
	}

	select {
	case <-executing:
	case <-ctx.Done():
		t.Fatalf("the worker never returned: %v", ctx.Err())
	}
}

// A queued run is cancelled on the spot, and the worker then has nothing to do
// — the run it would have taken is over before it ever started.
func TestCancellingAQueuedRunIsImmediateAndLeavesNothingToRun(t *testing.T) {
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

	_, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenario, environment))

	code, cancelled := s.cancel(t, queued.ID)
	if code != http.StatusOK {
		t.Fatalf("DELETE /runs/%s returned %d, wanted 200 for a queued run", queued.ID, code)
	}
	if cancelled.Outcome != string(domain.RunCancelled) || cancelled.Status != string(domain.StatusCancelled) {
		t.Fatalf("the response says %+v", cancelled)
	}

	if current := s.get(t, queued.ID); current.Status != string(domain.StatusCancelled) {
		t.Fatalf("the run is %q", current.Status)
	}

	worked, err := s.service.ProcessNext(ctx, org, nil)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if worked {
		t.Fatal("the worker started a run that had been cancelled")
	}

	// And cancelling it again is refused rather than written twice.
	if code, _ := s.cancel(t, queued.ID); code != http.StatusConflict {
		t.Fatalf("cancelling a finished run returned %d, wanted 409", code)
	}
}

// The platform's switch, at the API's door.
func TestASuspendedInstallationRefusesNewRuns(t *testing.T) {
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
	s := buildWith(t, db, org, false)

	code, _ := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenario, environment))
	if code != http.StatusServiceUnavailable {
		t.Fatalf("POST /runs returned %d, wanted 503", code)
	}

	// Nothing was written, which is the half that matters: a refusal that
	// queued the run anyway would be a refusal in name only.
	var queued int
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM run WHERE org_id = $1`, org).Scan(&queued)
	}); err != nil {
		t.Fatalf("count the runs: %v", err)
	}
	if queued != 0 {
		t.Fatalf("%d run(s) were written while the installation was refusing them", queued)
	}
}

// The emergency stop, through the API, by somebody who administers the whole
// organization.
func TestTheEmergencyStopEmptiesTheQueue(t *testing.T) {
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

	body := fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenario, environment)
	first := mustEnqueue(t, s, body)
	second := mustEnqueue(t, s, body)

	res := s.do(t, http.MethodPost, "/runs/stop-all", nil)
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("POST /runs/stop-all returned %d: %s", res.StatusCode, raw)
	}

	var stopped dto.StopAllResponse
	if err := json.NewDecoder(res.Body).Decode(&stopped); err != nil {
		t.Fatalf("decode the response: %v", err)
	}
	if stopped.Cancelled != 2 || stopped.AskedToStop != 0 {
		t.Fatalf("the stop reported %+v, wanted two cancelled and none asked", stopped)
	}

	for _, id := range []string{first, second} {
		if current := s.get(t, id); current.Status != string(domain.StatusCancelled) {
			t.Errorf("the run %s is %q", id, current.Status)
		}
	}
}

func mustEnqueue(t *testing.T, s stack, body string) string {
	t.Helper()

	code, queued := s.enqueue(t, body)
	if code != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", code)
	}
	return queued.ID
}
