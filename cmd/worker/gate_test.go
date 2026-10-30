package main

import (
	"context"
	"errors"
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
