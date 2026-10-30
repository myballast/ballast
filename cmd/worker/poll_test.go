package main

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Written before the change it asks for. A deployment sends the worker SIGTERM,
// and a worker that took that as "stop everything" would kill a run that is in
// the middle of measuring something: the container is destroyed, the numbers
// are gone, and the target was loaded for nothing.
//
// So there are two ways to stop, and they are different. Draining is the first
// signal: take nothing new, finish what is running, then leave. A hard stop is
// the second signal, or the end of the time a run is allowed: now the run is
// cancelled, which is what destroys its container.

type fakeProcessor struct {
	mu    sync.Mutex
	calls int

	// started is closed by the first call, so a test knows a run is under way.
	started chan struct{}
	once    sync.Once

	// run is what one call does, given the context it was handed.
	run func(ctx context.Context, calls int) (bool, error)
}

func newFakeProcessor(run func(ctx context.Context, calls int) (bool, error)) *fakeProcessor {
	return &fakeProcessor{started: make(chan struct{}), run: run}
}

func (f *fakeProcessor) ProcessNext(ctx context.Context, _ uuid.UUID, _ chan<- domain.LogLine) (bool, error) {
	f.mu.Lock()
	f.calls++
	calls := f.calls
	f.mu.Unlock()

	f.once.Do(func() { close(f.started) })

	return f.run(ctx, calls)
}

func (f *fakeProcessor) called() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// pollInBackground starts the loop and reports what it returned.
func pollInBackground(drain, hard context.Context, processor Processor) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- poll(drain, hard, processor, uuid.New(), 5*time.Millisecond, quiet())
	}()
	return done
}

func waitFor(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: the loop did not return", what)
		return nil
	}
}

// The reason this file exists. The first signal arrives while a run is being
// executed: the run must not be told to stop.
func TestADrainingWorkerLeavesTheRunItIsExecutingAlone(t *testing.T) {
	drain, startDraining := context.WithCancel(context.Background())
	defer startDraining()
	hard, stopNow := context.WithCancel(context.Background())
	defer stopNow()

	release := make(chan struct{})
	var seen error
	processor := newFakeProcessor(func(ctx context.Context, _ int) (bool, error) {
		<-release
		// Read after the signal, once the run would be finishing.
		seen = ctx.Err()
		return true, nil
	})

	done := pollInBackground(drain, hard, processor)

	<-processor.started
	startDraining()
	time.Sleep(50 * time.Millisecond)
	close(release)

	if err := waitFor(t, done, "draining"); err != nil {
		t.Fatalf("a drained worker leaves cleanly, got %v", err)
	}
	if seen != nil {
		t.Fatalf("the run was cancelled by the first signal (%v): a deployment would kill a measurement", seen)
	}
}

// Draining means taking nothing new, even when there is plenty to take.
func TestADrainingWorkerClaimsNothingMore(t *testing.T) {
	drain, startDraining := context.WithCancel(context.Background())
	defer startDraining()
	hard, stopNow := context.WithCancel(context.Background())
	defer stopNow()

	processor := newFakeProcessor(func(_ context.Context, calls int) (bool, error) {
		if calls == 1 {
			// The signal lands while the first run executes.
			startDraining()
		}
		// There is always another run waiting.
		return true, nil
	})

	done := pollInBackground(drain, hard, processor)

	if err := waitFor(t, done, "draining with a full queue"); err != nil {
		t.Fatalf("a drained worker leaves cleanly, got %v", err)
	}
	if got := processor.called(); got != 1 {
		t.Fatalf("the worker claimed %d runs; after the signal it must claim none", got)
	}
}

// A worker with nothing to do is the common case, and it must not need a run to
// finish before it can leave.
func TestAnIdleWorkerLeavesAtOnceWhenDrained(t *testing.T) {
	drain, startDraining := context.WithCancel(context.Background())
	defer startDraining()
	hard, stopNow := context.WithCancel(context.Background())
	defer stopNow()

	processor := newFakeProcessor(func(context.Context, int) (bool, error) { return false, nil })

	done := pollInBackground(drain, hard, processor)
	<-processor.started
	startDraining()

	if err := waitFor(t, done, "an idle worker"); err != nil {
		t.Fatalf("a drained worker leaves cleanly, got %v", err)
	}
}

// And a worker that was told to stop before it began must not start anything.
func TestAWorkerAlreadyDrainedClaimsNothing(t *testing.T) {
	drain, startDraining := context.WithCancel(context.Background())
	startDraining()
	hard, stopNow := context.WithCancel(context.Background())
	defer stopNow()

	processor := newFakeProcessor(func(context.Context, int) (bool, error) { return true, nil })

	if err := waitFor(t, pollInBackground(drain, hard, processor), "a drained worker"); err != nil {
		t.Fatalf("got %v", err)
	}
	if got := processor.called(); got != 0 {
		t.Fatalf("the worker claimed %d runs after being told to stop", got)
	}
}

// The way out, for a run that will not end: the second signal, or the end of the
// time a run is allowed. This one does cancel the run, because that is what
// takes its container down.
func TestAHardStopCancelsTheRunInFlight(t *testing.T) {
	drain, startDraining := context.WithCancel(context.Background())
	defer startDraining()
	hard, stopNow := context.WithCancel(context.Background())
	defer stopNow()

	processor := newFakeProcessor(func(ctx context.Context, _ int) (bool, error) {
		<-ctx.Done()
		return true, ctx.Err()
	})

	done := pollInBackground(drain, hard, processor)

	<-processor.started
	startDraining()
	stopNow()

	if err := waitFor(t, done, "a hard stop"); err != nil {
		t.Fatalf("a worker that was stopped leaves cleanly, got %v", err)
	}
}
