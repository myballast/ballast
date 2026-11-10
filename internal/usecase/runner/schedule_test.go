package runner

import (
	"testing"

	"github.com/google/uuid"
)

// ids returns n identifiers in ascending order, so a test can name them by
// position and the order is the one Rotation sorts into.
func ids(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.UUID{15: byte(i + 1)}
	}
	return out
}

func equal(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The order in which organizations are looked at is the whole fairness story:
// the one served last is looked at last, so a queue of ten runs for one
// organization cannot keep another's first run waiting behind all of them.
func TestTheOrganizationServedLastIsLookedAtLast(t *testing.T) {
	o := ids(3)
	var r Rotation

	if got := r.Order(o); !equal(got, o) {
		t.Fatalf("a fresh rotation starts at the first organization: got %v", got)
	}

	r.Served(o[0])
	if got, want := r.Order(o), []uuid.UUID{o[1], o[2], o[0]}; !equal(got, want) {
		t.Fatalf("after serving the first: got %v, want %v", got, want)
	}

	r.Served(o[2])
	if got, want := r.Order(o), []uuid.UUID{o[0], o[1], o[2]}; !equal(got, want) {
		t.Fatalf("after serving the last it starts over: got %v, want %v", got, want)
	}
}

// The list comes from the database in whatever order the database likes. The
// rotation must not depend on it, or "after the last one served" would mean a
// different organization every time.
func TestTheRotationDoesNotDependOnTheOrderItIsGiven(t *testing.T) {
	o := ids(3)
	var r Rotation
	r.Served(o[0])

	shuffled := []uuid.UUID{o[2], o[0], o[1]}
	if got, want := r.Order(shuffled), []uuid.UUID{o[1], o[2], o[0]}; !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The organization served last may have no work now, and so not be in the list.
// The circle goes on from where it was instead of snapping back to the start,
// which would favor the first organization every time one dropped out.
func TestTheRotationContinuesWhenTheOrganizationServedLastIsGone(t *testing.T) {
	o := ids(4)
	var r Rotation
	r.Served(o[1])

	without := []uuid.UUID{o[0], o[2], o[3]}
	if got, want := r.Order(without), []uuid.UUID{o[2], o[3], o[0]}; !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTheRotationOfNothingIsNothing(t *testing.T) {
	var r Rotation
	r.Served(uuid.New())

	if got := r.Order(nil); len(got) != 0 {
		t.Fatalf("got %v from an empty list", got)
	}
}

// Ordering is a question, not an action: it must not reorder the caller's list.
func TestOrderingDoesNotChangeTheListItIsGiven(t *testing.T) {
	o := ids(3)
	given := []uuid.UUID{o[2], o[0], o[1]}
	var r Rotation

	r.Order(given)

	if !equal(given, []uuid.UUID{o[2], o[0], o[1]}) {
		t.Fatalf("the caller's list was reordered: %v", given)
	}
}

func TestSlotsAreRefusedWhenTheyContradictEachOther(t *testing.T) {
	cases := []struct {
		name         string
		total, perOr int
	}{
		{"no slot at all", 0, 0},
		{"a negative total", -1, 1},
		{"no slot for an organization", 3, 0},
		{"an organization may take more than the machine has", 2, 3},
	}
	for _, tc := range cases {
		if _, err := NewSlots(tc.total, tc.perOr); err == nil {
			t.Errorf("%s: %d and %d were accepted", tc.name, tc.total, tc.perOr)
		}
	}
}

// The machine's capacity: three generators fit, a fourth would measure the
// saturation of its own machine instead of the target.
func TestNoMoreRunsStartThanTheMachineHasSlotsFor(t *testing.T) {
	slots, err := NewSlots(3, 3)
	if err != nil {
		t.Fatalf("new slots: %v", err)
	}
	o := ids(4)

	for i := range 3 {
		if !slots.TryAcquire(o[i]) {
			t.Fatalf("slot %d was refused with the machine not yet full", i+1)
		}
	}
	if slots.TryAcquire(o[3]) {
		t.Fatal("a fourth run was admitted on a machine with three slots")
	}
	if got := slots.Running(); got != 3 {
		t.Fatalf("running = %d, want 3", got)
	}
}

// What keeps one organization from taking the whole machine: with three slots
// and two per organization, the third is always left for somebody else.
func TestOneOrganizationCannotTakeMoreThanItsShare(t *testing.T) {
	slots, _ := NewSlots(3, 2)
	greedy, other := ids(2)[0], ids(2)[1]

	for i := range 2 {
		if !slots.TryAcquire(greedy) {
			t.Fatalf("an organization was refused slot %d of its share of two", i+1)
		}
	}
	if slots.TryAcquire(greedy) {
		t.Fatal("an organization took a third slot beyond its share of two")
	}
	if !slots.TryAcquire(other) {
		t.Fatal("the slot left over was not available to another organization")
	}
}

func TestAFinishedRunGivesItsSlotBack(t *testing.T) {
	slots, _ := NewSlots(1, 1)
	org := uuid.New()

	if !slots.TryAcquire(org) {
		t.Fatal("the only slot was refused")
	}
	if slots.TryAcquire(org) {
		t.Fatal("a second run was admitted in the only slot")
	}

	slots.Release(org)

	if !slots.TryAcquire(org) {
		t.Fatal("the slot was not given back")
	}
}

// A release that has nothing to release must not hand out a slot that does not
// exist: a counter that went negative would admit one run too many for ever.
func TestReleasingMoreThanWasAcquiredDoesNotCreateCapacity(t *testing.T) {
	slots, _ := NewSlots(1, 1)
	org := uuid.New()

	slots.Release(org)
	slots.Release(org)

	if !slots.TryAcquire(org) {
		t.Fatal("the slot was refused")
	}
	if slots.TryAcquire(org) {
		t.Fatal("a stray release created a second slot")
	}
}
