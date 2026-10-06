package scenario

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Repository stores scenarios and every version of their scripts. Declared here,
// by the code that consumes it, and implemented in adapter/postgres.
type Repository interface {
	// Create writes the scenario and its first version together.
	Create(ctx context.Context, newScenario domain.Scenario, by domain.Identity, at time.Time) (domain.Scenario, error)
	ByID(ctx context.Context, orgID, id uuid.UUID) (domain.Scenario, error)
	InProject(ctx context.Context, orgID, projectID uuid.UUID) ([]domain.Scenario, error)
	// Rename changes what a scenario is called. No version is written: a name
	// is not something a run executed.
	Rename(ctx context.Context, orgID, id uuid.UUID, name string, at time.Time) (domain.Scenario, error)
	// NewVersion writes the next version and points the scenario at it, in one
	// transaction. Nothing overwrites a version already written.
	NewVersion(ctx context.Context, orgID, id uuid.UUID, version domain.ScenarioVersion, by domain.Identity, at time.Time) (domain.Scenario, error)
	// Versions lists every version ever written, newest first.
	Versions(ctx context.Context, orgID, scenarioID uuid.UUID) ([]domain.ScenarioVersion, error)
	// Delete refuses a scenario that still holds runs, and checks that in the
	// transaction that would have deleted it.
	Delete(ctx context.Context, orgID, id uuid.UUID) error
}

// Thresholds stores the rules a scenario imposes on its metrics.
type Thresholds interface {
	OfScenario(ctx context.Context, orgID, scenarioID uuid.UUID) ([]domain.Threshold, error)
	Add(ctx context.Context, orgID, scenarioID uuid.UUID, threshold domain.Threshold) (domain.Threshold, error)
	Remove(ctx context.Context, orgID, scenarioID, thresholdID uuid.UUID) error
}

// ScriptInspector reads a script without running it.
//
// A k6 script is code somebody uploaded. What this reports decides whether it is
// stored at all, so it is asked before anything is written and again whenever a
// scenario is read, since a warning is only useful before a run.
type ScriptInspector interface {
	Inspect(script string) domain.ScriptReport
}

// Authorizer answers what the caller may do. Every question names a project.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
}

// Clock exists so that what a scenario records about when it changed can be
// controlled in a test.
type Clock interface {
	Now() time.Time
}
