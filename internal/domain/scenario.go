package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Scenario is a versioned k6 script belonging to a project.
//
// The script never hardcodes what it targets or how hard: it reads its base URL
// from the environment it is pointed at, and its load from the profile chosen
// at launch. That is what lets the same script run as a one minute smoke test
// and as a three hour soak.
type Scenario struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	Script    string
	// Version is frozen onto every run made from this scenario, and every
	// version ever written is kept, so what a past run executed can be read
	// back rather than inferred.
	Version        int
	DefaultProfile ProfileKind
	// DefaultProfileParams is the load this scenario suggests. Whatever is used
	// is still clamped to the environment's ceilings before a run is written.
	DefaultProfileParams ProfileParams
	// Mode says whether Script was written by hand or generated from Graph.
	// Empty means ModeCode, which is what every scenario written before graphs
	// existed is.
	Mode ScenarioMode
	// Graph is set when Mode is ModeGraph, and nil otherwise.
	Graph     *ScenarioGraph
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ScenarioVersion is one version of a scenario's script, kept for good.
//
// Editing a scenario adds one of these. Nothing overwrites one: a run names the
// version it executed, and that has to keep meaning what it meant.
type ScenarioVersion struct {
	ScenarioID           uuid.UUID
	Version              int
	Script               string
	DefaultProfile       ProfileKind
	DefaultProfileParams ProfileParams
	// Mode and Graph are frozen like everything else on a version: the script
	// a run executed was generated from this graph by the generator of that
	// day, and neither is rewritten afterwards.
	Mode  ScenarioMode
	Graph *ScenarioGraph
	// CreatedBy is nil once the person who wrote it has been removed.
	CreatedBy *uuid.UUID
	CreatedAt time.Time
}

const maxScenarioNameLength = 120

// ValidateScenario refuses a name Ballast will not store. The script is checked
// separately, by something that can parse JavaScript.
func ValidateScenario(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("%w: the name is empty", ErrInvalidScenario)
	case len(name) > maxScenarioNameLength:
		return fmt.Errorf("%w: the name is longer than %d characters",
			ErrInvalidScenario, maxScenarioNameLength)
	}
	return nil
}
