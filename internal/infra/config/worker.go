package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Worker is what the worker reads on top of the shared configuration.
type Worker struct {
	Config

	// OrgID restricts the worker to one organization. Unset (the zero value),
	// the worker serves every organization: it finds them through the two
	// functions of migration 020, which return identifiers and nothing else.
	// A machine that is one customer's, or a developer's, names it.
	OrgID uuid.UUID

	// MaxConcurrentRuns is how many runs this machine executes at once, and
	// MaxRunsPerOrg how many of those one organization may hold. The first is a
	// property of the machine: each generator is held to one CPU and 512 MB, and
	// one more than the machine has would measure its own saturation instead of
	// the target. The second is what keeps one customer from taking all of them.
	MaxConcurrentRuns int
	MaxRunsPerOrg     int

	// DrainTimeout is how long a stopping worker waits for the run it is
	// executing before cancelling it. A first signal takes no new run and lets
	// this one finish; this is the limit on "finish".
	DrainTimeout time.Duration

	// WorkDir is where each run's files are made, then mounted into its
	// container. Docker resolves it on the host, so a worker that runs in a
	// container has to be given a folder that exists at this same path on both
	// sides. Empty is the operating system's temporary folder, which is right when
	// the worker is no container.
	WorkDir string

	// ProxyImage is the egress proxy a run is given. Empty is the development
	// tag; a machine that runs real runs names the build that matches the worker.
	ProxyImage string

	// RunnerName is how this worker is known to the database. A worker that
	// restarts under the same name is the same runner, and takes its row back.
	RunnerName string
}

// LoadWorker reads the worker's configuration.
func LoadWorker() (Worker, error) {
	common, err := Load()
	if err != nil {
		return Worker{}, err
	}

	// Empty means every organization. A value that is not a uuid is refused
	// rather than read as empty: a typo must not quietly widen what a machine
	// serves from one customer to all of them.
	var orgID uuid.UUID
	if raw := env("BALLAST_WORKER_ORG_ID", ""); raw != "" {
		orgID, err = uuid.Parse(raw)
		if err != nil {
			return Worker{}, fmt.Errorf("BALLAST_WORKER_ORG_ID is not a uuid: %w", err)
		}
	}

	// Three runs and two per organization fit the machine this was sized for
	// (4 cores, 8 GB): three generators at one CPU and 512 MB each, and a slot
	// always left for somebody else.
	maxConcurrent, err := positiveInt("BALLAST_MAX_CONCURRENT_RUNS", 3)
	if err != nil {
		return Worker{}, err
	}
	perOrg, err := positiveInt("BALLAST_MAX_RUNS_PER_ORG", 2)
	if err != nil {
		return Worker{}, err
	}
	if perOrg > maxConcurrent {
		return Worker{}, fmt.Errorf(
			"BALLAST_MAX_RUNS_PER_ORG is %d, above the %d runs the machine executes at once (BALLAST_MAX_CONCURRENT_RUNS); an organization would wait for slots that do not exist",
			perOrg, maxConcurrent)
	}

	// A minute past the time limit by default: the generator is killed at
	// RunTTL, so a drain that long has seen every run to its end.
	drain, err := duration("BALLAST_DRAIN_TIMEOUT", common.RunTTL+time.Minute)
	if err != nil {
		return Worker{}, err
	}
	// Under the time limit, a deployment could cancel a run that was still
	// entitled to its full length, and that run would end as a cancellation with
	// nothing to say why. Said at startup, like the time limit itself.
	if drain < common.RunTTL {
		return Worker{}, fmt.Errorf(
			"BALLAST_DRAIN_TIMEOUT is %s, under the %s a run may last; stopping the worker would kill a run that was still measuring",
			drain, common.RunTTL)
	}

	workDir := env("BALLAST_WORK_DIR", "")
	// Docker would read a relative path against its own directory, not this one's.
	if workDir != "" && !filepath.IsAbs(workDir) {
		return Worker{}, fmt.Errorf("BALLAST_WORK_DIR is %q, which is not an absolute path; the daemon would resolve it against its own directory", workDir)
	}

	name := env("BALLAST_RUNNER_NAME", "")
	if name == "" {
		// One worker per machine, and the machine already has a name.
		name, err = os.Hostname()
		if err != nil || name == "" {
			return Worker{}, fmt.Errorf("BALLAST_RUNNER_NAME is not set and the host name is unknown: %w", err)
		}
	}

	return Worker{
		Config: common,
		OrgID:  orgID,

		MaxConcurrentRuns: maxConcurrent,
		MaxRunsPerOrg:     perOrg,
		DrainTimeout:      drain,
		WorkDir:           workDir,
		ProxyImage:        env("BALLAST_PROXY_IMAGE", ""),
		RunnerName:        name,
	}, nil
}

// PoolConnections is the size of the database pool: three per run that may be in
// flight (the one doing the work, the heartbeat, the cancellation watch), plus
// the presence, the collector and the poll. A pool smaller than that makes a run
// wait for a connection while its own heartbeat is overdue.
func (w Worker) PoolConnections() int {
	return 3*w.MaxConcurrentRuns + 3
}
