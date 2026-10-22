package scenario

import (
	"context"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// PreviewCollection reads a collection, and an environment file if one is given,
// and says what an import would make of each request. Nothing is stored.
//
// Like the preview of a specification, it asks for the right to create a
// scenario before it reads anything.
func (s *Service) PreviewCollection(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, collection, environment []byte,
) (domain.CollectionPreview, error) {
	if err := s.require(ctx, who, domain.PermManageScenarios, projectID); err != nil {
		return domain.CollectionPreview{}, err
	}

	imported, err := s.collections.Read(collection, environment)
	if err != nil {
		return domain.CollectionPreview{}, err
	}
	return imported.Preview()
}

// CollectionImport is a request to build a scenario from some requests of a
// collection.
type CollectionImport struct {
	ProjectID uuid.UUID
	Name      string
	// Collection and Environment are the files again: the preview keeps nothing
	// on the server. The environment may be empty.
	Collection  []byte
	Environment []byte
	// Requests are the ones to keep, by position, in the order they should run.
	Requests []int
}

// CollectionResult is what an import leaves the person to do and to know: the
// secrets to create, which are keys and never values, and everything that could
// not be carried over.
type CollectionResult struct {
	Secrets  []domain.DetectedSecret
	Findings []domain.ImportFinding
}

// ImportCollection builds a graph scenario from the chosen requests and stores
// it.
//
// The analysis refuses to produce a graph that still holds a secret of the file,
// so nothing that reaches the generator, the script inspector or the database
// carries one.
func (s *Service) ImportCollection(
	ctx context.Context, who domain.Identity, request CollectionImport,
) (domain.Scenario, domain.ScriptReport, CollectionResult, error) {
	none := domain.ScriptReport{}

	if err := s.require(ctx, who, domain.PermManageScenarios, request.ProjectID); err != nil {
		return domain.Scenario{}, none, CollectionResult{}, err
	}

	imported, err := s.collections.Read(request.Collection, request.Environment)
	if err != nil {
		return domain.Scenario{}, none, CollectionResult{}, err
	}

	analysis, err := domain.AnalyseCollection(imported, request.Requests)
	if err != nil {
		return domain.Scenario{}, none, CollectionResult{}, err
	}

	graph, err := analysis.Graph()
	if err != nil {
		return domain.Scenario{}, none, CollectionResult{}, err
	}

	script, err := s.generator.Generate(graph)
	if err != nil {
		return domain.Scenario{}, none, CollectionResult{}, err
	}

	result := CollectionResult{Secrets: analysis.Secrets, Findings: analysis.Findings}

	created, report, err := s.Create(ctx, who, NewScenario{
		ProjectID: request.ProjectID,
		Name:      request.Name,
		Script:    script,
		Graph:     &graph,
	})
	if err != nil {
		return domain.Scenario{}, report, result, err
	}

	// Counts only: what the file held is never written to a log.
	s.log.InfoContext(ctx, "scenario imported from a collection",
		"org_id", who.OrgID, "project_id", request.ProjectID, "scenario_id", created.ID,
		"requests", len(analysis.Requests), "secrets", len(analysis.Secrets), "findings", len(analysis.Findings))

	return created, report, result, nil
}
