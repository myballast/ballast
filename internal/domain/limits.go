package domain

import "time"

// The limits nobody can raise without changing this file.
//
// They are the last line in front of abuse, and that is why they are constants
// rather than configuration: a ceiling somebody can lift from the outside is a
// ceiling an attacker lifts, and a quota per plan is a different feature with a
// different shape. What an environment declares is checked as well, and the
// smaller of the two always wins — these are a floor under the carelessness of
// whoever configured an environment, not a replacement for it.
const (
	// MaxVUsAnywhere is the most virtual users any run may use, whatever its
	// environment allows.
	//
	// Two hundred because of what runs them: a generator container holds half
	// a gigabyte, and k6 spends one to five megabytes per virtual user. Past
	// that the container swaps and slows, and what the run then measures is
	// its own generator rather than the target — with nothing in the numbers
	// to say so. A ceiling above what the machine can do is a ceiling that
	// lets somebody produce false measurements.
	MaxVUsAnywhere = 200

	// MaxDurationAnywhere is the longest any run may last, whatever its
	// environment allows.
	//
	// An hour: long enough for a soak to mean something, short enough that a
	// mistake costs an hour of traffic rather than a weekend of it. The
	// generator's own time limit has to be at least this, or a run allowed
	// here would be killed there — which the worker refuses to start with.
	MaxDurationAnywhere = time.Hour

	// RateWindow is the stretch the two limits below are counted over.
	RateWindow = time.Hour

	// MaxRunsPerUserPerWindow is how many runs one person may trigger in that
	// window. Thirty is already heavy use by a person; a pipeline pushes a
	// handful per commit.
	MaxRunsPerUserPerWindow = 30

	// MaxRunsPerOrgPerWindow is the same for a whole organization: enough for
	// ten busy people, far short of what a loop could ask for.
	MaxRunsPerOrgPerWindow = 100

	// MaxQueuedRunsPerOrg is how many runs may wait at once.
	//
	// One run executes at a time per target, so twenty waiting is already
	// hours of backlog: past that the queue is not a queue, it is a mistake
	// nobody has noticed yet.
	MaxQueuedRunsPerOrg = 20
)

// MaxDurationAnywhereS is the same ceiling in whole seconds, which is how a
// run's parameters are written.
const MaxDurationAnywhereS = int(MaxDurationAnywhere / time.Second)
