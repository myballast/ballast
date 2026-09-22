package run

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// The interfaces below are declared here, by the code that consumes them, and
// implemented elsewhere. adapter/postgres imports this package to learn what to
// satisfy; nothing in this package ever imports an adapter.

// Repository stores runs and their results.
//
// Every method is expected to run inside a transaction scoped to orgID, which
// is what makes row-level security apply. Taking orgID as an argument rather
// than reading it from somewhere ambient keeps that visible at every call.
type Repository interface {
	Create(ctx context.Context, run domain.Run) (domain.Run, error)
	ByID(ctx context.Context, orgID, id uuid.UUID) (domain.Run, error)
	ClaimNextQueued(ctx context.Context, orgID uuid.UUID, at time.Time) (domain.Run, bool, error)
	Finish(ctx context.Context, orgID, id uuid.UUID, status domain.RunStatus, at time.Time, message string) error
	SaveResults(ctx context.Context, orgID, runID uuid.UUID, results domain.RunResults) error
	Results(ctx context.Context, orgID, runID uuid.UUID) (domain.RunResults, error)
}

// ScenarioReader reads scenarios and the thresholds they impose.
type ScenarioReader interface {
	ScenarioByID(ctx context.Context, orgID, id uuid.UUID) (domain.Scenario, error)
	ScenarioThresholds(ctx context.Context, orgID, scenarioID uuid.UUID) ([]domain.Threshold, error)
}

// EnvironmentReader reads target environments.
type EnvironmentReader interface {
	EnvironmentByID(ctx context.Context, orgID, id uuid.UUID) (domain.Environment, error)
}

// Job is one execution handed to a generator.
type Job struct {
	RunID uuid.UUID
	// Script is the user's scenario, untouched.
	Script []byte
	// Options is the generator's own configuration, built from the profile.
	Options []byte
	// Env is injected into the generator. It carries an environment's secrets.
	Env map[string]string
}

// Outcome is what a finished generator left behind.
type Outcome struct {
	// Summary is the generator's raw report, unfiltered.
	Summary  []byte
	ExitCode int64
	TimedOut bool
}

// Executor runs a job to completion.
//
// This interface is the whole point of the arrangement: it is what will let a
// runner deployed on a client's own infrastructure be an addition rather than a
// rewrite. Nothing in this package knows what a container is.
type Executor interface {
	Execute(ctx context.Context, job Job, logs chan<- domain.LogLine) (Outcome, error)
}

// SummaryParser extracts the results Ballast keeps from a generator's report.
type SummaryParser interface {
	Parse(raw []byte) (domain.RunResults, error)
}

// OptionsBuilder turns a load profile into the generator's configuration.
type OptionsBuilder interface {
	Build(kind domain.ProfileKind, params domain.ProfileParams, thresholds []domain.Threshold) ([]byte, error)
}

// Clock exists so that what a run records about when it happened can be
// controlled in a test.
type Clock interface {
	Now() time.Time
}
