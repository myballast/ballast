package scenario

import (
	"context"
	"fmt"
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
	// Update changes a rule in place, and reports ErrNotFound for a rule this
	// scenario does not impose.
	Update(ctx context.Context, orgID, scenarioID uuid.UUID, threshold domain.Threshold) (domain.Threshold, error)
	Remove(ctx context.Context, orgID, scenarioID, thresholdID uuid.UUID) error
	// CountInProject is how many rules each scenario of a project imposes, read
	// at once for the list. A scenario that imposes none is absent.
	CountInProject(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error)
}

// ScriptInspector reads a script without running it.
//
// A k6 script is code somebody uploaded. What this reports decides whether it is
// stored at all, so it is asked before anything is written and again whenever a
// scenario is read, since a warning is only useful before a run.
type ScriptInspector interface {
	Inspect(script string) domain.ScriptReport
}

// SpecReader reads the text of an OpenAPI specification. The text comes from an
// upload, so what implements this is the code that decides how far to trust it.
type SpecReader interface {
	Read(data []byte) (domain.ImportedSpec, error)
}

// CollectionReader reads the text of a file of requests: a Postman collection
// and its optional environment, a HAR file, or pasted cURL commands. As with a
// specification, the text comes from an upload.
//
// The formats differ in how they are read and not in what comes out, so one
// analysis serves them all. The second argument is the environment file, which
// only Postman has.
type CollectionReader interface {
	Read(collection, environment []byte) (domain.ImportedCollection, error)
}

// CollectionReaders holds one reader per format. A format with no reader is
// refused, not guessed.
type CollectionReaders struct {
	Postman CollectionReader
	HAR     CollectionReader
	Curl    CollectionReader
}

// For returns the reader of a format.
func (r CollectionReaders) For(format domain.CollectionFormat) (CollectionReader, error) {
	var reader CollectionReader
	switch format {
	case domain.FormatPostman:
		reader = r.Postman
	case domain.FormatHAR:
		reader = r.HAR
	case domain.FormatCurl:
		reader = r.Curl
	}
	if reader == nil {
		return nil, fmt.Errorf("%w: %q is not a format that can be imported", domain.ErrInvalidCollection, string(format))
	}
	return reader, nil
}

// ScriptGenerator writes the k6 script of a scenario graph.
type ScriptGenerator interface {
	Generate(graph domain.ScenarioGraph) (string, error)
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
