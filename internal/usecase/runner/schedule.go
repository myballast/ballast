package runner

import (
	"bytes"
	"errors"
	"slices"
	"sync"

	"github.com/google/uuid"
)

// Rotation decides in which order organizations are looked at: the one served
// last is looked at last. A queue of ten runs for one organization cannot then
// keep another's first run waiting behind all of them.
//
// It remembers one identifier, and it is not safe for concurrent use: one loop
// owns it.
type Rotation struct {
	last    uuid.UUID
	started bool
}

// Served records the organization a run was just taken for.
func (r *Rotation) Served(id uuid.UUID) {
	r.last = id
	r.started = true
}

// Order returns the organizations starting after the one served last. The list
// is sorted first, because the database answers in whatever order it likes and
// "after the last one" would otherwise mean a different organization each time;
// the caller's slice is left as it was. When the one served last is no longer in
// the list, the circle goes on from where it was.
func (r *Rotation) Order(orgs []uuid.UUID) []uuid.UUID {
	sorted := slices.Clone(orgs)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })

	if !r.started {
		return sorted
	}

	// The first one that sorts after the last served.
	start := len(sorted)
	for i, id := range sorted {
		if bytes.Compare(id[:], r.last[:]) > 0 {
			start = i
			break
		}
	}

	return append(sorted[start:], sorted[:start]...)
}

// Slots is the capacity of a machine: how many runs it executes at once, and how
// many of those one organization may hold.
//
// Three generators fit on the machine this was sized for, and a fourth would
// measure the saturation of its own machine instead of the target. The share of
// an organization is what keeps one customer from taking all of them.
type Slots struct {
	mu     sync.Mutex
	total  int
	perOrg int
	taken  int
	byOrg  map[uuid.UUID]int
}

// NewSlots refuses a capacity that contradicts itself.
func NewSlots(total, perOrg int) (*Slots, error) {
	switch {
	case total < 1:
		return nil, errors.New("a worker needs at least one slot")
	case perOrg < 1:
		return nil, errors.New("an organization needs at least one slot")
	case perOrg > total:
		return nil, errors.New("an organization cannot hold more slots than the machine has")
	}
	return &Slots{total: total, perOrg: perOrg, byOrg: make(map[uuid.UUID]int)}, nil
}

// TryAcquire takes a slot for org, and says whether there was one to take.
func (s *Slots) TryAcquire(org uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.taken >= s.total || s.byOrg[org] >= s.perOrg {
		return false
	}
	s.taken++
	s.byOrg[org]++
	return true
}

// Release gives a slot back. Releasing one that was not held does nothing: a
// counter that went negative would admit one run too many for ever.
func (s *Slots) Release(org uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.byOrg[org] == 0 {
		return
	}
	s.byOrg[org]--
	if s.byOrg[org] == 0 {
		delete(s.byOrg, org)
	}
	s.taken--
}

// Running is how many slots are held.
func (s *Slots) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taken
}
