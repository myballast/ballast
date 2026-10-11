package domain

import (
	"time"

	"github.com/google/uuid"
)

// Run is one execution of a scenario against an environment.
//
// Everything describing what was executed is frozen here when the run is
// created: the scenario version, the profile, and the parameters after they
// were clamped to the environment's ceilings. Editing a scenario afterwards
// must never change what a past run says it did.
type Run struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	ProjectID       uuid.UUID
	ScenarioID      uuid.UUID
	EnvironmentID   uuid.UUID
	ScenarioVersion int
	Profile         ProfileKind
	ProfileParams   ProfileParams
	Status          RunStatus
	TriggeredBy     *uuid.UUID
	ErrorMessage    string
	CreatedAt       time.Time
	StartedAt       *time.Time
	EndedAt         *time.Time
	HeartbeatAt     *time.Time
	// CancelRequestedAt is when somebody asked this run to stop. It is a
	// request and not an ending: for a run being executed, the status that says
	// the load has stopped is written by the worker that stopped it.
	CancelRequestedAt *time.Time
}

// CancellationRequested reports whether somebody has asked this run to stop.
func (r Run) CancellationRequested() bool {
	return r.CancelRequestedAt != nil
}

// NewRun builds a run ready to be queued.
//
// params are expected to have been clamped already: this records a decision,
// it does not make one.
func NewRun(
	orgID uuid.UUID,
	scenario Scenario,
	environment Environment,
	profile ProfileKind,
	params ProfileParams,
	triggeredBy *uuid.UUID,
) Run {
	return Run{
		OrgID:           orgID,
		ProjectID:       scenario.ProjectID,
		ScenarioID:      scenario.ID,
		EnvironmentID:   environment.ID,
		ScenarioVersion: scenario.Version,
		Profile:         profile,
		ProfileParams:   params,
		Status:          StatusQueued,
		TriggeredBy:     triggeredBy,
	}
}

// Verdict is the run status these results call for.
//
// Whether a single threshold held is k6's judgement, read from the summary and
// never recomputed. What is decided here is only how those verdicts add up: one
// failure is enough to fail the run, and a run with no threshold at all has
// nothing to fail.
func (r RunResults) Verdict() RunStatus {
	for _, threshold := range r.Thresholds {
		if !threshold.Passed {
			return StatusFailed
		}
	}
	return StatusPassed
}

// lockGrace is what a run is allowed on top of its own duration before its hold
// on an environment stops counting.
//
// It covers what happens either side of the load itself: pulling a generator
// image on a machine that has never run a test, starting a container, and
// reading the summary back afterwards. Too short and a worker loses its target
// mid-measurement to another worker; too long and a crashed worker blocks that
// target for no reason. Five minutes is the first of those two mistakes avoided
// at the cost of the second being briefly possible.
const lockGrace = 5 * time.Minute

// LockExpiry is when this run's hold on its environment stops counting.
//
// Built from the run's own duration, which was clamped to the environment's
// ceiling before the run was written, so this is an upper bound on how long a
// living worker could legitimately hold it.
//
// It matters only when a worker dies: one that is alive releases the lock when
// the run ends.
func (r Run) LockExpiry(at time.Time) time.Time {
	return at.Add(time.Duration(r.ProfileParams.TotalDuration())*time.Second + lockGrace)
}

// HeartbeatInterval is how often a running run says it is still being watched.
//
// The worker writes heartbeat_at this often for as long as it is executing. It
// is not an extension of the environment's lock: a lock covers the run's whole
// clamped duration from the start, and a heartbeat answers a different question
// — is the process that started this still alive?
const HeartbeatInterval = 10 * time.Second

// heartbeatTimeout is how long a run may go unwatched before it is considered
// abandoned: six missed beats.
//
// The width is the point. A worker is alive but silent for a while more often
// than it is dead — a slow write, a database failover, a long garbage
// collection — and a run taken away from a living worker is the worse of the
// two mistakes: its container is destroyed mid-measurement. The other mistake
// costs a target held a minute longer than necessary.
const heartbeatTimeout = 6 * HeartbeatInterval

// StaleBefore is the instant before which a heartbeat means nothing: a run last
// seen earlier than this is abandoned.
//
// The collector asks the database for runs older than this, so the rule lives
// here rather than in a WHERE clause.
func StaleBefore(at time.Time) time.Time {
	return at.Add(-heartbeatTimeout)
}

// CancelPollInterval is how often a worker asks whether the run it is
// executing has been cancelled.
//
// A read by primary key, not a write: at two seconds a write per running run
// would be a lot of them, and reading costs an index lookup. Two seconds
// because a cancellation is promised to take effect in under five, and
// destroying a container takes a moment of that.
const CancelPollInterval = 2 * time.Second

// CancelOutcome is what asking for a run to stop achieved.
type CancelOutcome string

const (
	// RunCancelled means it is over: the run had not started, so nothing had to
	// be stopped before saying so.
	RunCancelled CancelOutcome = "cancelled"
	// CancellationRequested means a generator is running and has been asked to
	// stop. The run reaches cancelled when the worker executing it has
	// destroyed the container and freed the target.
	CancellationRequested CancelOutcome = "cancellation_requested"
)
