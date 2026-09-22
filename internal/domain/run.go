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
