package config

import (
	"fmt"

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

	return Worker{Config: common, OrgID: orgID}, nil
}
