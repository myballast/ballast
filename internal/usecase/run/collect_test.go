package run

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// journal is what makes an order testable: two fakes write to one list, and the
// test reads the sequence rather than each call in isolation.
type journal struct {
	mu     sync.Mutex
	events []string
}

func (j *journal) record(event string) {
	if j == nil {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	j.events = append(j.events, event)
}

func (j *journal) recorded() []string {
	j.mu.Lock()
	defer j.mu.Unlock()

	out := make([]string, len(j.events))
	copy(out, j.events)
	return out
}

// fakeContainers stands in for whatever destroys a generator's leftovers: it
// writes down what it was asked to destroy. Nothing here knows how, which is
// the point of the port.
type fakeContainers struct {
	journal *journal
	err     error
	// destroyed is what DestroyExcept reports having removed.
	destroyed int

	mu   sync.Mutex
	kept [][]uuid.UUID
}

func (f *fakeContainers) Destroy(_ context.Context, runID uuid.UUID) error {
	if f.err != nil {
		return f.err
	}

	f.journal.record("destroyed " + runID.String())
	return nil
}

func (f *fakeContainers) DestroyExcept(_ context.Context, keep []uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.kept = append(f.kept, keep)
	return f.destroyed, f.err
}

// fakeProjects stands in for the project directory the emergency stop reads.
type fakeProjects struct {
	ids []uuid.UUID
	err error
}

func (f *fakeProjects) ProjectIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return f.ids, f.err
}

// fakeLogs stands in for the store that keeps a run's output.
type fakeLogs struct {
	// journal is shared with the other fakes, so a test can check that the
	// last line was stored before the run was recorded as finished.
	journal *journal

	// delay is how long a write takes. A real one takes milliseconds, and that
	// is what makes the order between storing a line and finishing a run
	// observable rather than a coin toss.
	delay time.Duration

	mu                  sync.Mutex
	orgID, runID        uuid.UUID
	appended            []domain.LogLine
	since               []domain.LogLine
	after               int64
	limit               int
	appendErr, sinceErr error
}

func (f *fakeLogs) Append(
	_ context.Context, orgID, runID uuid.UUID, lines []domain.LogLine,
) error {
	time.Sleep(f.delay)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.orgID, f.runID = orgID, runID

	// Copied, because the caller reuses the slice it was given.
	f.appended = append(f.appended, lines...)
	f.journal.record("stored")
	return f.appendErr
}

func (f *fakeLogs) Since(
	_ context.Context, _, _ uuid.UUID, after int64, limit int,
) ([]domain.LogLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.after, f.limit = after, limit
	return f.since, f.sinceErr
}

// stored is what was appended, read from the test's goroutine while the
// service's own may still be appending.
func (f *fakeLogs) stored() []domain.LogLine {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]domain.LogLine, len(f.appended))
	copy(out, f.appended)
	return out
}

// emptyResults stands in for the summary parser. What it extracts is tested in
// adapter/k6; here a run only needs to reach a terminal state.
type emptyResults struct{}

func (emptyResults) Parse([]byte) (domain.RunResults, error) {
	return domain.RunResults{}, nil
}

// waitingExecutor blocks until released, or until its context is cancelled —
// which is what a worker losing its run has to do to the container.
type waitingExecutor struct {
	release chan struct{}

	mu        sync.Mutex
	cancelled bool
}

func (e *waitingExecutor) Execute(
	ctx context.Context, _ Job, _ chan<- domain.LogLine,
) (Outcome, error) {
	select {
	case <-e.release:
		return Outcome{Summary: []byte("{}")}, nil

	case <-ctx.Done():
		e.mu.Lock()
		e.cancelled = true
		e.mu.Unlock()

		return Outcome{}, ctx.Err()
	}
}

func (e *waitingExecutor) wasCancelled() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.cancelled
}

// collectFixture is a worker's run service with every collaborator faked.
type collectFixture struct {
	service    *Service
	runs       *fakeRuns
	containers *fakeContainers
	logs       *fakeLogs
	journal    *journal
	orgID      uuid.UUID
	now        time.Time
}

func newCollectFixture(executor Executor) collectFixture {
	shared := &journal{}
	runs := &fakeRuns{journal: shared}
	containers := &fakeContainers{journal: shared}
	logs := &fakeLogs{}
	now := time.Date(2026, 3, 14, 15, 9, 26, 0, time.UTC)

	service := NewService(
		runs,
		fakeScenarios{scenario: domain.Scenario{Script: "export default function () {}"}},
		fakeEnvironments{environment: verifiedTarget()},
		&fakeAllowlist{hosts: []string{"staging.example.test"}},
		&fakeSecrets{},
		executor,
		containers,
		&fakeProjects{},
		logs,
		staticOptions{},
		emptyResults{},
		allowAll{},
		fixedClock{at: now},
		Accepting(true),
		discardLogger(),
	)
	// Beats a test can wait for. The interval is a field precisely so that
	// watching three of them go by does not take half a minute.
	service.beatEvery = time.Millisecond

	return collectFixture{
		service:    service,
		runs:       runs,
		containers: containers,
		logs:       logs,
		journal:    shared,
		orgID:      uuid.New(),
		now:        now,
	}
}

// running is a run as a worker holds it: claimed, started, being executed.
func running(environmentID uuid.UUID) *domain.Run {
	return &domain.Run{
		ID:            uuid.New(),
		EnvironmentID: environmentID,
		ScenarioID:    uuid.New(),
		Profile:       domain.ProfileSmoke,
		Status:        domain.StatusRunning,
	}
}

// The first thing the ticket asks for: a run being executed says so, over and
// over, for as long as it is being executed.
func TestARunSaysItIsStillWatchedWhileItExecutes(t *testing.T) {
	executor := &waitingExecutor{release: make(chan struct{})}
	f := newCollectFixture(executor)

	environment := verifiedTarget()
	f.runs.next = running(environment.ID)

	// The execution lasts until three beats have been written, which is the
	// claim being tested: not that one is written, that they keep coming.
	go func() {
		for f.runs.beatsRecorded() < 3 {
			time.Sleep(time.Millisecond)
		}
		close(executor.release)
	}()

	worked, err := f.service.ProcessNext(context.Background(), f.orgID, nil)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !worked {
		t.Fatal("the queued run was not picked up")
	}

	if beats := f.runs.beatsRecorded(); beats < 3 {
		t.Fatalf("the run was only reported alive %d time(s)", beats)
	}
}

// The other half of the heartbeat, and the reason it reports a row count: a
// collector that took this run means somebody has already decided this worker
// is dead. Carrying on would leave the run recorded as over while its generator
// kept loading the target.
func TestAWorkerThatLostItsRunStopsExecutingIt(t *testing.T) {
	executor := &waitingExecutor{release: make(chan struct{})}
	f := newCollectFixture(executor)

	environment := verifiedTarget()
	f.runs.next = running(environment.ID)
	// The run is no longer this worker's, which is what a heartbeat touching no
	// row says.
	f.runs.lost = true
	// And the collector that took it has already ended it.
	f.runs.finishErr = domain.ErrNotFound

	// Nothing releases the executor: if the heartbeat does not cancel it, this
	// test hangs, which is the failure.
	worked, err := f.service.ProcessNext(context.Background(), f.orgID, nil)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !worked {
		t.Fatal("the queued run was not picked up")
	}

	if !executor.wasCancelled() {
		t.Fatal("the execution was left running after the run was taken away")
	}
}

// The order the whole collector is about. errored says the load has stopped, so
// it must not be written while a container might still be generating it —
// writing it first would also free the target for a run that then measures this
// one's traffic as well as its own.
func TestAnAbandonedRunIsEndedOnlyOnceItsLeftoversAreGone(t *testing.T) {
	f := newCollectFixture(nil)

	abandoned := uuid.New()
	f.runs.stale = []domain.Run{{ID: abandoned, Status: domain.StatusOrphaned}}
	f.runs.orphans = []domain.Run{{ID: abandoned, Status: domain.StatusOrphaned}}

	ended, err := f.service.Collect(context.Background(), f.orgID)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ended != 1 {
		t.Fatalf("%d run(s) were collected", ended)
	}

	want := []string{"destroyed " + abandoned.String(), "errored " + abandoned.String()}
	got := f.journal.recorded()
	if strings.Join(got, " then ") != strings.Join(want, " then ") {
		t.Fatalf("the collector did:\n  %v\nwanted:\n  %v", got, want)
	}
}

// A run left orphaned by a collector that died itself. Nothing would ever
// return it from the first step again — it is no longer running — so the second
// step reads the table rather than reusing what the first handed back.
func TestARunAlreadyOrphanedIsStillCollected(t *testing.T) {
	f := newCollectFixture(nil)

	abandoned := uuid.New()
	// Nothing went stale on this pass: this one was orphaned earlier.
	f.runs.stale = nil
	f.runs.orphans = []domain.Run{{ID: abandoned, Status: domain.StatusOrphaned}}

	ended, err := f.service.Collect(context.Background(), f.orgID)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ended != 1 {
		t.Fatalf("%d run(s) were collected", ended)
	}
}

// A container that will not die leaves the run orphaned, which is the honest
// answer: it is not over, and the next pass will try again. Ending it would say
// the load had stopped on no evidence.
func TestARunWhoseLeftoversSurviveIsNotEnded(t *testing.T) {
	f := newCollectFixture(nil)
	f.containers.err = errors.New("the daemon is not answering")

	f.runs.orphans = []domain.Run{{ID: uuid.New(), Status: domain.StatusOrphaned}}

	ended, err := f.service.Collect(context.Background(), f.orgID)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ended != 0 {
		t.Fatalf("%d run(s) were reported collected", ended)
	}
	if events := f.journal.recorded(); len(events) != 0 {
		t.Fatalf("the collector did %v", events)
	}
}

// A run in a terminal state has nothing to do in that list, and the state
// machine is what says so rather than this function's own opinion.
func TestAFinishedRunIsNotEnded(t *testing.T) {
	f := newCollectFixture(nil)

	f.runs.orphans = []domain.Run{{ID: uuid.New(), Status: domain.StatusPassed}}

	ended, err := f.service.Collect(context.Background(), f.orgID)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ended != 0 {
		t.Fatalf("%d run(s) were reported collected", ended)
	}
	if events := f.journal.recorded(); len(events) != 0 {
		t.Fatalf("the collector did %v", events)
	}
}

// Six missed beats, not one. A worker alive but silent for a moment — a slow
// write, a database failover — must not lose its run and have its container
// destroyed mid-measurement.
func TestARunIsAbandonedOnlyAfterSixMissedBeats(t *testing.T) {
	f := newCollectFixture(nil)

	if _, err := f.service.Collect(context.Background(), f.orgID); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	want := f.now.Add(-6 * domain.HeartbeatInterval)
	if !f.runs.cutoff.Equal(want) {
		t.Fatalf("runs unseen since %s were collected, wanted %s", f.runs.cutoff, want)
	}
}

// The sweep a worker runs when it starts: everything on the machine goes,
// except what belongs to a run somebody is watching.
func TestTheSweepSparesWhatIsStillBeingWatched(t *testing.T) {
	f := newCollectFixture(nil)

	watched := []uuid.UUID{uuid.New(), uuid.New()}
	f.runs.live = watched
	f.containers.destroyed = 3

	destroyed, err := f.service.Sweep(context.Background(), f.orgID)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if destroyed != 3 {
		t.Fatalf("the sweep reported %d destroyed, the daemon removed 3", destroyed)
	}

	if len(f.containers.kept) != 1 {
		t.Fatalf("the daemon was swept %d time(s)", len(f.containers.kept))
	}
	if len(f.containers.kept[0]) != len(watched) ||
		f.containers.kept[0][0] != watched[0] || f.containers.kept[0][1] != watched[1] {
		t.Fatalf("the sweep spared %v, wanted %v", f.containers.kept[0], watched)
	}

	// A run whose heartbeat has gone stale is not spared: nobody is watching
	// its container either, and the collector is about to end the run anyway.
	want := f.now.Add(-6 * domain.HeartbeatInterval)
	if !f.runs.since.Equal(want) {
		t.Fatalf("the sweep called a run live if seen since %s, wanted %s", f.runs.since, want)
	}
}
