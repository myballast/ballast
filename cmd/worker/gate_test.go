package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type fakeGate struct {
	admitted bool
	minimum  int
	err      error
}

func (g fakeGate) Admit(context.Context, uuid.UUID) (bool, int, error) {
	return g.admitted, g.minimum, g.err
}

func TestAnAdmittedWorkerTakesTheNextRun(t *testing.T) {
	next := newFakeProcessor(func(context.Context, int) (bool, error) { return true, nil })
	guard := &contractGuard{next: next, gate: fakeGate{admitted: true, minimum: 1}, log: quiet()}

	worked, err := guard.ProcessNext(context.Background(), uuid.New(), nil)
	if err != nil || !worked {
		t.Fatalf("worked = %v, err = %v; an admitted worker takes what is there", worked, err)
	}
	if next.called() != 1 {
		t.Fatalf("the run service was asked %d times, want 1", next.called())
	}
}

// The point of the minimum: the claim is never made, so the run is not touched
// and stays queued for a worker that is current.
func TestARefusedWorkerClaimsNothing(t *testing.T) {
	next := newFakeProcessor(func(context.Context, int) (bool, error) { return true, nil })
	guard := &contractGuard{next: next, gate: fakeGate{admitted: false, minimum: 3}, log: quiet()}

	for range 5 {
		worked, err := guard.ProcessNext(context.Background(), uuid.New(), nil)
		if err != nil {
			t.Fatalf("a refusal is not a failure: %v", err)
		}
		if worked {
			t.Fatal("a refused worker reported having worked")
		}
	}
	if next.called() != 0 {
		t.Fatalf("a refused worker asked the run service %d times; it must never claim", next.called())
	}
}

// An unreadable minimum is not "admitted".
func TestAnUnreadableMinimumClaimsNothingAndSaysSo(t *testing.T) {
	next := newFakeProcessor(func(context.Context, int) (bool, error) { return true, nil })
	guard := &contractGuard{next: next, gate: fakeGate{err: errors.New("the database is gone")}, log: quiet()}

	if _, err := guard.ProcessNext(context.Background(), uuid.New(), nil); err == nil {
		t.Fatal("a failed read was swallowed")
	}
	if next.called() != 0 {
		t.Fatal("the worker claimed a run without knowing the minimum")
	}
}

// countingHandler counts what a logger was asked to write, by level.
type countingHandler struct {
	mu    sync.Mutex
	warns int
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		h.mu.Lock()
		h.warns++
		h.mu.Unlock()
	}
	return nil
}
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

// perOrgGate answers a different minimum for each organization, which is what
// the minimum is: a setting of the organization, not of the worker.
type perOrgGate struct{ refused map[uuid.UUID]int }

func (g perOrgGate) Admit(_ context.Context, org uuid.UUID) (bool, int, error) {
	if minimum, ok := g.refused[org]; ok {
		return false, minimum, nil
	}
	return true, 1, nil
}

// With several runs in flight the guard is called from several goroutines at
// once, and with several organizations each has a minimum of its own. What it
// remembers having said must therefore belong to the organization, and be safe to
// touch from two goroutines — run under -race, this is what finds the shared
// field.
func TestTheGuardSaysWhyOncePerOrganizationAndIsSafeInParallel(t *testing.T) {
	a, b := uuid.UUID{15: 1}, uuid.UUID{15: 2}
	counting := &countingHandler{}
	next := newFakeProcessor(func(context.Context, int) (bool, error) { return true, nil })
	guard := &contractGuard{
		next:     next,
		gate:     perOrgGate{refused: map[uuid.UUID]int{a: 3, b: 5}},
		contract: 1,
		log:      slog.New(counting),
	}

	var wg sync.WaitGroup
	for _, org := range []uuid.UUID{a, b} {
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if worked, err := guard.ProcessNext(context.Background(), org, nil); err != nil || worked {
					t.Errorf("worked = %v, err = %v; a refused worker takes nothing", worked, err)
				}
			}()
		}
	}
	wg.Wait()

	counting.mu.Lock()
	defer counting.mu.Unlock()
	if counting.warns != 2 {
		t.Fatalf("the guard warned %d times for 40 refusals over two organizations; once each is enough", counting.warns)
	}
	if next.called() != 0 {
		t.Fatal("a refused worker reached the run service")
	}
}
