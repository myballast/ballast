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

	// OrgID is the one organization the worker takes runs from.
	//
	// Row-level security keeps the application role from listing
	// organizations, so the worker cannot find work across tenants yet. That
	// is the PostgreSQL queue's problem to solve; until then the worker is
	// pointed at one organization, and runs queued in any other wait.
	OrgID uuid.UUID

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

	raw := env("BALLAST_WORKER_ORG_ID", "")
	if raw == "" {
		return Worker{}, fmt.Errorf("BALLAST_WORKER_ORG_ID is not set; the worker serves one organization until the queue can reach them all")
	}
	orgID, err := uuid.Parse(raw)
	if err != nil {
		return Worker{}, fmt.Errorf("BALLAST_WORKER_ORG_ID is not a uuid: %w", err)
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
		Config:       common,
		OrgID:        orgID,
		DrainTimeout: drain,
		WorkDir:      workDir,
		ProxyImage:   env("BALLAST_PROXY_IMAGE", ""),
		RunnerName:   name,
	}, nil
}
