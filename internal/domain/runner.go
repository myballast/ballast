package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RunnerContract is the version of what a worker and the database promise each
// other: a column the worker writes, a status it knows how to read. It goes up
// when an older worker could no longer be trusted with a run, and only then.
//
// A number and not the build, because a minimum has to be compared and a commit
// hash cannot be. The build is Runner.Version, for a person to read.
//
// A constant of the domain and not configuration, like the ceilings on a run:
// a worker that could be told it is current is a worker that is not checked.
const RunnerContract = 1

// MaxRunnerContract bounds the number, so that it fits the integer column it is
// stored in with room to spare.
const MaxRunnerContract = 1_000_000

const (
	// RunnerHeartbeatEvery is how often a worker says it is alive. The same
	// pace as the heartbeat of a run, and for the same reason: a write every
	// ten seconds is affordable, one every two would not be.
	RunnerHeartbeatEvery = 10 * time.Second

	// RunnerOfflineAfter is how long a worker may be silent before it is read
	// as offline: three beats, so that one late write is not an outage.
	RunnerOfflineAfter = 3 * RunnerHeartbeatEvery
)

// RunnerState is what a worker says about itself.
type RunnerState string

const (
	// RunnerServing means the worker takes runs.
	RunnerServing RunnerState = "serving"
	// RunnerDraining means the worker was told to stop, takes nothing new, and
	// is letting the run it is executing finish.
	RunnerDraining RunnerState = "draining"
	// RunnerStopped means the worker left, and said so.
	RunnerStopped RunnerState = "stopped"
)

// RunnerStatus is what a reader concludes, at an instant, from what a worker
// last said. It is never stored: a worker that was killed without a word is
// offline because nothing was said, and nothing had to write that down.
type RunnerStatus string

const (
	// RunnerOnline means the worker spoke recently and takes runs.
	RunnerOnline RunnerStatus = "online"
	// RunnerStatusDraining means it spoke recently and is finishing its last run.
	RunnerStatusDraining RunnerStatus = "draining"
	// RunnerOffline means it said it stopped, or has been silent too long.
	RunnerOffline RunnerStatus = "offline"
)

// ErrInvalidRunner is returned for a runner the table would refuse.
var ErrInvalidRunner = errors.New("invalid runner")

// Runner is one machine that executes runs, as it last described itself.
type Runner struct {
	ID    uuid.UUID
	OrgID uuid.UUID
	// Name identifies the runner inside its organization: a runner restarted
	// under the same name is the same runner.
	Name string
	// Version is the build, for a person to read.
	Version string
	// Contract is compared against the minimum; see RunnerContract.
	Contract int
	State    RunnerState
	// StartedAt is when the process started. It is sent with every beat and does
	// not change for as long as the process lives.
	StartedAt   time.Time
	HeartbeatAt time.Time
}

// Validate says the checks the table makes, so that no other writer can store a
// row the database would refuse.
func (r Runner) Validate() error {
	switch {
	case len(r.Name) < 1 || len(r.Name) > 80:
		return fmt.Errorf("%w: the name has to be between 1 and 80 characters", ErrInvalidRunner)
	case len(r.Version) < 1 || len(r.Version) > 64:
		return fmt.Errorf("%w: the version has to be between 1 and 64 characters", ErrInvalidRunner)
	case r.Contract < 1 || r.Contract > MaxRunnerContract:
		return fmt.Errorf("%w: the contract has to be between 1 and %d", ErrInvalidRunner, MaxRunnerContract)
	}

	switch r.State {
	case RunnerServing, RunnerDraining, RunnerStopped:
		return nil
	default:
		return fmt.Errorf("%w: %q is not a state", ErrInvalidRunner, r.State)
	}
}

// StatusAt reads what the runner last said against the clock.
//
// A beat that appears to come from the future is a fresh one: the worker's clock
// and the reader's can differ by a few seconds, and that is no reason to call a
// worker that just spoke offline.
func (r Runner) StatusAt(now time.Time) RunnerStatus {
	if r.State == RunnerStopped {
		return RunnerOffline
	}
	if now.Sub(r.HeartbeatAt) > RunnerOfflineAfter {
		return RunnerOffline
	}
	if r.State == RunnerDraining {
		return RunnerStatusDraining
	}
	return RunnerOnline
}

// Outdated says whether the runner is older than the minimum contract: a runner
// that must not be given a run.
func (r Runner) Outdated(minimum int) bool {
	return r.Contract < minimum
}
