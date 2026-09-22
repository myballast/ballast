package domain

import "testing"

// Every status the package defines. Written out so the sweeps below cover all
// hundred ordered pairs rather than only the interesting ones.
var allStatuses = []RunStatus{
	StatusQueued,
	StatusRunning,
	StatusPassed,
	StatusFailed,
	StatusErrored,
	StatusTimeout,
	StatusCancelled,
	StatusRejected,
	StatusSkipped,
	StatusOrphaned,
}

type transition struct {
	from, to RunStatus
}

// Read off the state diagram in docs/uml/uml.md, by hand, and deliberately not
// derived from the transitions table in run_status.go. A test that builds its
// expectations from the table it is checking proves only that the table equals
// itself.
var allowedTransitions = map[transition]bool{
	{StatusQueued, StatusRunning}:    true,
	{StatusQueued, StatusCancelled}:  true,
	{StatusQueued, StatusRejected}:   true,
	{StatusQueued, StatusSkipped}:    true,
	{StatusRunning, StatusPassed}:    true,
	{StatusRunning, StatusFailed}:    true,
	{StatusRunning, StatusErrored}:   true,
	{StatusRunning, StatusTimeout}:   true,
	{StatusRunning, StatusCancelled}: true,
	{StatusRunning, StatusOrphaned}:  true,
	{StatusOrphaned, StatusErrored}:  true,
}

// The seven endings listed under the diagram. orphaned is absent on purpose:
// a run whose worker died is not finished, it is unattended.
var terminalStatuses = map[RunStatus]bool{
	StatusPassed:    true,
	StatusFailed:    true,
	StatusErrored:   true,
	StatusTimeout:   true,
	StatusCancelled: true,
	StatusRejected:  true,
	StatusSkipped:   true,
}

func TestCanTransitionTo(t *testing.T) {
	if len(allStatuses) != 10 {
		t.Fatalf("the sweep covers %d statuses, want 10", len(allStatuses))
	}
	if len(allowedTransitions) != 11 {
		t.Fatalf("the diagram has 11 transitions, the test lists %d", len(allowedTransitions))
	}

	pairs := 0
	for _, from := range allStatuses {
		for _, to := range allStatuses {
			pairs++

			want := allowedTransitions[transition{from, to}]
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s -> %s: got %t, want %t", from, to, got, want)
			}
		}
	}

	if pairs != 100 {
		t.Errorf("swept %d pairs, want 100", pairs)
	}
}

// A status must not transition to itself. Covered by the sweep above, and
// stated separately because it is the case a future edit is most likely to
// weaken by accident.
func TestNoStatusTransitionsToItself(t *testing.T) {
	for _, s := range allStatuses {
		if s.CanTransitionTo(s) {
			t.Errorf("%s transitions to itself", s)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	for _, s := range allStatuses {
		want := terminalStatuses[s]
		if got := s.IsTerminal(); got != want {
			t.Errorf("%s: IsTerminal() = %t, want %t", s, got, want)
		}
	}
}

// A terminal status has no way out, and a non-terminal one has at least one.
// This is what keeps IsTerminal honest now that it reads the transitions table
// instead of its own list.
func TestTerminalStatusesAreExactlyTheDeadEnds(t *testing.T) {
	for _, s := range allStatuses {
		outgoing := 0
		for _, to := range allStatuses {
			if s.CanTransitionTo(to) {
				outgoing++
			}
		}

		switch {
		case terminalStatuses[s] && outgoing != 0:
			t.Errorf("%s is terminal but leads to %d other statuses", s, outgoing)
		case !terminalStatuses[s] && outgoing == 0:
			t.Errorf("%s is not terminal but leads nowhere", s)
		}
	}
}

// Guards against a transition pointing at a status that does not exist, which
// the sweep above would silently ignore.
func TestTransitionsOnlyReferenceKnownStatuses(t *testing.T) {
	known := make(map[RunStatus]bool, len(allStatuses))
	for _, s := range allStatuses {
		known[s] = true
	}

	for from, targets := range transitions {
		if !known[from] {
			t.Errorf("transitions has an entry for the unknown status %q", from)
		}
		for _, to := range targets {
			if !known[to] {
				t.Errorf("%s leads to the unknown status %q", from, to)
			}
		}
	}
}

// These strings are stored in run.status and constrained by migration 001.
// A typo here is not a rename, it is rows the database refuses to accept.
func TestStatusValuesMatchTheSchema(t *testing.T) {
	want := map[RunStatus]string{
		StatusQueued:    "queued",
		StatusRunning:   "running",
		StatusPassed:    "passed",
		StatusFailed:    "failed",
		StatusErrored:   "errored",
		StatusTimeout:   "timeout",
		StatusCancelled: "cancelled",
		StatusRejected:  "rejected",
		StatusSkipped:   "skipped",
		StatusOrphaned:  "orphaned",
	}

	if len(want) != len(allStatuses) {
		t.Fatalf("the table pins %d values for %d statuses", len(want), len(allStatuses))
	}

	for status, literal := range want {
		if string(status) != literal {
			t.Errorf("status constant is %q, the schema expects %q", string(status), literal)
		}
	}
}
