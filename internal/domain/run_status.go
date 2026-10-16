package domain

// RunStatus is where a run stands in its lifecycle.
//
// The state machine below is the one drawn in docs/uml/uml.md. It lives here
// rather than in a service because it is a business rule: a run that has
// finished must not start again, and that has to hold whatever the database,
// the worker or the HTTP layer happen to believe on a given day.
type RunStatus string

// The ten statuses a run can hold. The strings are the values stored in
// run.status and constrained by migration 001; they are part of the schema, not
// an implementation detail.
const (
	StatusQueued    RunStatus = "queued"
	StatusRunning   RunStatus = "running"
	StatusPassed    RunStatus = "passed"
	StatusFailed    RunStatus = "failed"
	StatusErrored   RunStatus = "errored"
	StatusTimeout   RunStatus = "timeout"
	StatusCancelled RunStatus = "cancelled"
	StatusRejected  RunStatus = "rejected"
	StatusSkipped   RunStatus = "skipped"
	StatusOrphaned  RunStatus = "orphaned"
)

// Known reports whether s is one of the ten statuses. A filter is read from a
// request, and a status nobody defined is a mistake to refuse rather than a
// query that matches nothing.
func (s RunStatus) Known() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusPassed, StatusFailed, StatusErrored,
		StatusTimeout, StatusCancelled, StatusRejected, StatusSkipped, StatusOrphaned:
		return true
	}
	return false
}

// transitions maps a status to every status a run may move to from it.
//
// orphaned is the one that is easy to get wrong: it is not terminal. It means
// the worker died mid-run and the container may well still be alive, so the
// reaper has to pick the run up and turn it into errored once it has cleaned
// up. Treating it as an ending would leave load generators running with nobody
// watching them.
var transitions = map[RunStatus][]RunStatus{
	StatusQueued: {
		StatusRunning,
		StatusCancelled,
		StatusRejected,
		StatusSkipped,
	},
	StatusRunning: {
		StatusPassed,
		StatusFailed,
		StatusErrored,
		StatusTimeout,
		StatusCancelled,
		StatusOrphaned,
	},
	StatusOrphaned: {
		StatusErrored,
	},
}

// CanTransitionTo reports whether a run in status s may move to next.
//
// A status never transitions to itself: re-entering a state is a different
// event from staying in it, and a run that is already running must not be
// started a second time.
func (s RunStatus) CanTransitionTo(next RunStatus) bool {
	for _, allowed := range transitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// IsTerminal reports whether a run in status s has finished for good.
//
// Derived from the transitions table rather than from a second list, so the two
// cannot drift apart. It tests the length rather than the presence of the key,
// so an entry left with an empty list still reads as terminal.
//
// A status this package does not know reads as terminal too. That is the safe
// way round: an unrecognised value leaves a run stuck and visible rather than
// letting something move it along.
func (s RunStatus) IsTerminal() bool {
	return len(transitions[s]) == 0
}
