package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// fakePresence writes down what the worker said, in order.
type fakePresence struct {
	mu    sync.Mutex
	said  []domain.RunnerState
	fails int
}

func (f *fakePresence) say(state domain.RunnerState) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fails > 0 {
		f.fails--
		return errors.New("the database is unreachable")
	}
	f.said = append(f.said, state)
	return nil
}

func (f *fakePresence) Serving(context.Context) error  { return f.say(domain.RunnerServing) }
func (f *fakePresence) Draining(context.Context) error { return f.say(domain.RunnerDraining) }
func (f *fakePresence) Stopped(context.Context) error  { return f.say(domain.RunnerStopped) }

func (f *fakePresence) history() []domain.RunnerState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.RunnerState(nil), f.said...)
}

func beatInBackground(ctx, drain context.Context, presence Presence, every time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		beat(ctx, drain, presence, every, quiet())
	}()
	return done
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the beat did not return")
	}
}

// Said at once and not after the first interval: a worker that has just started
// is online from that moment, not ten seconds later.
func TestTheWorkerSaysItIsServingAtOnceAndKeepsSayingIt(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	drain, startDraining := context.WithCancel(context.Background())
	defer startDraining()
	presence := &fakePresence{}

	done := beatInBackground(ctx, drain, presence, 10*time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	stop()
	waitDone(t, done)

	said := presence.history()
	if len(said) < 4 {
		t.Fatalf("only %d reports in 80ms at 10ms apart: %v", len(said), said)
	}
	if said[0] != domain.RunnerServing {
		t.Fatalf("the first report is %q, want serving", said[0])
	}
}

// What a deployment looks like from outside: the first signal flips the state,
// and the beats go on, because a draining worker is a live one.
func TestTheFirstSignalIsReportedAsDrainingAndTheBeatsContinue(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	drain, startDraining := context.WithCancel(context.Background())
	presence := &fakePresence{}

	done := beatInBackground(ctx, drain, presence, 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	startDraining()
	time.Sleep(60 * time.Millisecond)
	stop()
	waitDone(t, done)

	said := presence.history()

	firstDraining := -1
	for i, s := range said {
		if s == domain.RunnerDraining {
			firstDraining = i
			break
		}
	}
	if firstDraining < 0 {
		t.Fatalf("draining was never reported: %v", said)
	}
	for _, s := range said[firstDraining : len(said)-1] {
		if s != domain.RunnerDraining {
			t.Fatalf("a worker that is draining went back to %q: %v", s, said)
		}
	}
	if len(said)-firstDraining < 3 {
		t.Fatalf("the beats stopped when draining began: %v", said)
	}
}

// The last word, written when the worker is leaving.
func TestAStoppingWorkerSaysItHasStopped(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	drain, startDraining := context.WithCancel(context.Background())
	defer startDraining()
	presence := &fakePresence{}

	done := beatInBackground(ctx, drain, presence, 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	stop()
	waitDone(t, done)

	said := presence.history()
	if got := said[len(said)-1]; got != domain.RunnerStopped {
		t.Fatalf("the last report is %q, want stopped: %v", got, said)
	}
}

// A database that blinks must not take the worker down, or make it stop beating:
// the next beat is the retry.
func TestAReportThatFailsIsRetriedAtTheNextBeat(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	drain, startDraining := context.WithCancel(context.Background())
	defer startDraining()
	presence := &fakePresence{fails: 3}

	done := beatInBackground(ctx, drain, presence, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	stop()
	waitDone(t, done)

	if len(presence.history()) < 3 {
		t.Fatalf("the beat gave up after a failed report: %v", presence.history())
	}
}
