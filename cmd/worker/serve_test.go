package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/runner"
)

// Written before the loop it asks for. A worker that serves every organization
// has two promises to keep that a worker for one never had to: an organization
// must not wait for another's run while the machine has room, and no
// organization may take the room that is left for the others.

// fixedScope is the organizations with work, changeable while the loop runs.
type fixedScope struct {
	mu    sync.Mutex
	orgs  []uuid.UUID
	fail  int // the next reads that fail
	reads int
}

func (s *fixedScope) Active(context.Context) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reads++
	if s.fail > 0 {
		s.fail--
		return nil, errors.New("the database is gone")
	}
	return append([]uuid.UUID(nil), s.orgs...), nil
}

func (s *fixedScope) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

// orgProcessor answers for every organization and writes down what it was asked,
// in order, and how many runs were in flight at the busiest moment.
type orgProcessor struct {
	mu        sync.Mutex
	asked     []uuid.UUID
	calls     map[uuid.UUID]int
	running   map[uuid.UUID]int
	peak      map[uuid.UUID]int
	current   int
	peakTotal int

	// run is one call: n is how many times this organization has been asked.
	run func(ctx context.Context, org uuid.UUID, n int) (bool, error)
}

func newOrgProcessor(run func(ctx context.Context, org uuid.UUID, n int) (bool, error)) *orgProcessor {
	return &orgProcessor{
		calls:   map[uuid.UUID]int{},
		running: map[uuid.UUID]int{},
		peak:    map[uuid.UUID]int{},
		run:     run,
	}
}

func (p *orgProcessor) ProcessNext(ctx context.Context, org uuid.UUID, _ chan<- domain.LogLine) (bool, error) {
	p.mu.Lock()
	p.asked = append(p.asked, org)
	p.calls[org]++
	n := p.calls[org]
	p.running[org]++
	p.current++
	if p.running[org] > p.peak[org] {
		p.peak[org] = p.running[org]
	}
	if p.current > p.peakTotal {
		p.peakTotal = p.current
	}
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.running[org]--
		p.current--
		p.mu.Unlock()
	}()

	return p.run(ctx, org, n)
}

func (p *orgProcessor) order() []uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uuid.UUID(nil), p.asked...)
}

func (p *orgProcessor) peakOf(org uuid.UUID) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peak[org]
}

func (p *orgProcessor) peakAll() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peakTotal
}

func (p *orgProcessor) askedAbout(org uuid.UUID) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[org]
}

type loop struct {
	drain      context.Context
	startDrain context.CancelFunc
	hard       context.Context
	stopNow    context.CancelFunc
	done       <-chan error
}

// newLoop makes the two contexts a worker stops by, before anything runs on
// them: a test that must stop the worker from inside a call needs the function
// that does it before the call can happen.
func newLoop(t *testing.T) loop {
	t.Helper()

	drain, startDrain := context.WithCancel(context.Background())
	hard, stopNow := context.WithCancel(context.Background())
	t.Cleanup(startDrain)
	t.Cleanup(stopNow)

	return loop{drain: drain, startDrain: startDrain, hard: hard, stopNow: stopNow}
}

func (l loop) start(t *testing.T, processor Processor, scope Scope, total, perOrg int) loop {
	t.Helper()

	slots, err := runner.NewSlots(total, perOrg)
	if err != nil {
		t.Fatalf("slots: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- serve(l.drain, l.hard, processor, scope, slots, 5*time.Millisecond, quiet())
	}()

	l.done = done
	return l
}

func serveInBackground(t *testing.T, processor Processor, scope Scope, total, perOrg int) loop {
	t.Helper()
	return newLoop(t).start(t, processor, scope, total, perOrg)
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func twoOrgs() (uuid.UUID, uuid.UUID) { return uuid.UUID{15: 1}, uuid.UUID{15: 2} }

// The point of the whole change. One organization's hour-long soak is under way;
// another's run must start now, not in an hour.
//
// With a single slot this fails: the second organization is looked at only when
// the first one's run is over.
func TestAnotherOrganizationDoesNotWaitForARunThatIsUnderWay(t *testing.T) {
	a, b := twoOrgs()
	started := map[uuid.UUID]chan struct{}{a: make(chan struct{}), b: make(chan struct{})}
	release := make(chan struct{})

	processor := newOrgProcessor(func(_ context.Context, org uuid.UUID, n int) (bool, error) {
		if n == 1 {
			close(started[org])
			<-release
			return true, nil
		}
		return false, nil
	})
	l := serveInBackground(t, processor, &fixedScope{orgs: []uuid.UUID{a, b}}, 3, 2)

	waitClosed(t, started[a], "the first organization's run to start")
	waitClosed(t, started[b], "the second organization's run to start while the first is under way")

	close(release)
	l.startDrain()
	if err := waitFor(t, l.done, "the loop"); err != nil {
		t.Fatalf("got %v", err)
	}
}

// The machine's capacity is the number of slots, whatever is waiting.
func TestNoMoreRunsAreInFlightThanTheMachineHasSlotsFor(t *testing.T) {
	a, b := twoOrgs()
	var finished atomic.Int32

	processor := newOrgProcessor(func(_ context.Context, _ uuid.UUID, _ int) (bool, error) {
		time.Sleep(15 * time.Millisecond)
		return finished.Add(1) <= 6, nil
	})
	l := serveInBackground(t, processor, &fixedScope{orgs: []uuid.UUID{a, b}}, 1, 1)

	time.Sleep(300 * time.Millisecond)
	l.startDrain()
	if err := waitFor(t, l.done, "the loop"); err != nil {
		t.Fatalf("got %v", err)
	}

	if got := processor.peakAll(); got != 1 {
		t.Fatalf("%d runs were in flight on a machine with one slot", got)
	}
}

// What keeps one organization from taking the machine: its share holds even when
// the machine has slots to spare and the organization has runs waiting.
func TestAnOrganizationNeverRunsMoreThanItsShare(t *testing.T) {
	a, b := twoOrgs()
	var finished atomic.Int32

	processor := newOrgProcessor(func(_ context.Context, _ uuid.UUID, _ int) (bool, error) {
		time.Sleep(15 * time.Millisecond)
		return finished.Add(1) <= 12, nil
	})
	l := serveInBackground(t, processor, &fixedScope{orgs: []uuid.UUID{a, b}}, 4, 1)

	time.Sleep(400 * time.Millisecond)
	l.startDrain()
	if err := waitFor(t, l.done, "the loop"); err != nil {
		t.Fatalf("got %v", err)
	}

	for _, org := range []uuid.UUID{a, b} {
		if got := processor.peakOf(org); got != 1 {
			t.Fatalf("organization %s had %d runs in flight with a share of one", org, got)
		}
	}
}

// With room for one run at a time, the organizations are served in turn. An
// organization with ten runs waiting must not keep another's first run behind all
// ten.
func TestOrganizationsAreServedInTurnWhenThereIsRoomForOneRun(t *testing.T) {
	a, b := twoOrgs()
	l := newLoop(t)

	processor := newOrgProcessor(func(_ context.Context, _ uuid.UUID, _ int) (bool, error) {
		return true, nil
	})
	// Stop after eight claims: there is always another run waiting.
	counting := &stopAfter{inner: processor, after: 8, stop: l.startDrain}
	l = l.start(t, counting, &fixedScope{orgs: []uuid.UUID{a, b}}, 1, 1)

	if err := waitFor(t, l.done, "the loop"); err != nil {
		t.Fatalf("got %v", err)
	}

	order := processor.order()
	if len(order) < 6 {
		t.Fatalf("only %d claims were made: %v", len(order), order)
	}
	for i := 2; i < len(order); i++ {
		if order[i] == order[i-1] && order[i] == order[i-2] {
			t.Fatalf("one organization was served three times running while the other waited: %v", order)
		}
	}
	if order[0] == order[1] {
		t.Fatalf("the first two claims went to the same organization: %v", order)
	}
}

// stopAfter starts the drain once it has been asked n times, from inside the call
// like a signal landing in the middle of a run.
type stopAfter struct {
	inner Processor
	after int32
	n     atomic.Int32
	stop  context.CancelFunc
}

func (s *stopAfter) ProcessNext(ctx context.Context, org uuid.UUID, logs chan<- domain.LogLine) (bool, error) {
	if s.n.Add(1) == s.after && s.stop != nil {
		s.stop()
	}
	return s.inner.ProcessNext(ctx, org, logs)
}

// An organization the list does not name has nothing queued, and asking it would
// be a query for nothing, every second, for every organization there is.
func TestAnOrganizationWithoutWorkIsNeverAsked(t *testing.T) {
	a, b := twoOrgs()
	processor := newOrgProcessor(func(context.Context, uuid.UUID, int) (bool, error) { return false, nil })
	l := serveInBackground(t, processor, &fixedScope{orgs: []uuid.UUID{b}}, 3, 2)

	time.Sleep(60 * time.Millisecond)
	l.startDrain()
	if err := waitFor(t, l.done, "the loop"); err != nil {
		t.Fatalf("got %v", err)
	}

	if got := processor.askedAbout(a); got != 0 {
		t.Fatalf("an organization with no work was asked %d times", got)
	}
	if got := processor.askedAbout(b); got == 0 {
		t.Fatal("the organization with work was never asked")
	}
}

// An organization whose worker is too old for it, or whose target is held, takes
// nothing; the organization after it must not be kept waiting by that.
func TestAnOrganizationThatTakesNothingDoesNotHoldUpTheNext(t *testing.T) {
	a, b := twoOrgs()
	served := make(chan struct{})
	var once sync.Once

	processor := newOrgProcessor(func(_ context.Context, org uuid.UUID, _ int) (bool, error) {
		if org == a {
			return false, nil
		}
		once.Do(func() { close(served) })
		return false, nil
	})
	l := serveInBackground(t, processor, &fixedScope{orgs: []uuid.UUID{a, b}}, 1, 1)

	waitClosed(t, served, "the second organization to be asked past one that takes nothing")
	l.startDrain()
	_ = waitFor(t, l.done, "the loop")
}

// A list that cannot be read is a tick with nothing to do. The loop does not
// stop, does not claim from a list it never got, and picks up when the database
// answers again.
func TestAnUnreadableListIsRetriedAndTheLoopGoesOn(t *testing.T) {
	a, _ := twoOrgs()
	scope := &fixedScope{orgs: []uuid.UUID{a}, fail: 3}
	asked := make(chan struct{})
	var once sync.Once

	processor := newOrgProcessor(func(context.Context, uuid.UUID, int) (bool, error) {
		once.Do(func() { close(asked) })
		return false, nil
	})
	l := serveInBackground(t, processor, scope, 3, 2)

	waitClosed(t, asked, "the loop to recover once the list could be read")
	if scope.readCount() < 4 {
		t.Fatalf("the list was read %d times; it fails three times before it answers", scope.readCount())
	}

	l.startDrain()
	if err := waitFor(t, l.done, "the loop"); err != nil {
		t.Fatalf("a list that failed took the loop down: %v", err)
	}
}

// The first signal is a deployment. With several runs in flight, none of them is
// told to stop, nothing new is taken, and the worker leaves only when the last of
// them has finished.
func TestDrainingLeavesEveryRunInFlightAloneAndWaitsForAllOfThem(t *testing.T) {
	a, b := twoOrgs()
	started := map[uuid.UUID]chan struct{}{a: make(chan struct{}), b: make(chan struct{})}
	release := make(chan struct{})
	var cancelled, finished atomic.Int32

	processor := newOrgProcessor(func(ctx context.Context, org uuid.UUID, n int) (bool, error) {
		if n > 1 {
			return false, nil
		}
		close(started[org])
		<-release
		if ctx.Err() != nil {
			cancelled.Add(1)
		}
		finished.Add(1)
		return true, nil
	})
	l := serveInBackground(t, processor, &fixedScope{orgs: []uuid.UUID{a, b}}, 3, 2)

	waitClosed(t, started[a], "a's run")
	waitClosed(t, started[b], "b's run")

	l.startDrain()
	select {
	case err := <-l.done:
		t.Fatalf("the worker left (%v) with two runs still measuring", err)
	case <-time.After(80 * time.Millisecond):
	}

	asked := processor.askedAbout(a) + processor.askedAbout(b)
	close(release)

	if err := waitFor(t, l.done, "the drain"); err != nil {
		t.Fatalf("got %v", err)
	}
	if finished.Load() != 2 {
		t.Fatalf("the worker left with %d of 2 runs finished", finished.Load())
	}
	if cancelled.Load() != 0 {
		t.Fatalf("%d run(s) were cancelled by the first signal", cancelled.Load())
	}
	if got := processor.askedAbout(a) + processor.askedAbout(b); got != asked {
		t.Fatalf("%d new claim(s) were made after the first signal", got-asked)
	}
}

// The second signal, or the end of the time a run is allowed, takes every run
// down: that is what destroys their containers.
func TestAHardStopCancelsEveryRunInFlight(t *testing.T) {
	a, b := twoOrgs()
	var cancelled atomic.Int32
	started := make(chan struct{}, 2)

	processor := newOrgProcessor(func(ctx context.Context, _ uuid.UUID, n int) (bool, error) {
		if n > 1 {
			return false, nil
		}
		started <- struct{}{}
		<-ctx.Done()
		cancelled.Add(1)
		return true, ctx.Err()
	})
	l := serveInBackground(t, processor, &fixedScope{orgs: []uuid.UUID{a, b}}, 3, 2)

	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("a run did not start")
		}
	}

	l.startDrain()
	l.stopNow()

	if err := waitFor(t, l.done, "a hard stop"); err != nil {
		t.Fatalf("got %v", err)
	}
	if cancelled.Load() != 2 {
		t.Fatalf("%d of 2 runs were cancelled by the hard stop", cancelled.Load())
	}
}
