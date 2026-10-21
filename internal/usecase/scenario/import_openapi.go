package scenario

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// PreviewOpenAPI reads a specification and lists what could be imported from
// it. Nothing is stored.
//
// It asks for the same permission as the import itself: the preview is the first
// half of it, and there is no reason to let somebody who cannot create a
// scenario spend the server's time reading a file.
func (s *Service) PreviewOpenAPI(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, spec []byte,
) (domain.ImportedSpec, error) {
	if err := s.require(ctx, who, domain.PermManageScenarios, projectID); err != nil {
		return domain.ImportedSpec{}, err
	}

	return s.specs.Read(spec)
}

// OpenAPIImport is a request to build a scenario from some endpoints of a
// specification.
type OpenAPIImport struct {
	ProjectID uuid.UUID
	Name      string
	// Spec is the whole file again. The preview keeps nothing on the server, so
	// the file is read a second time, under the same limits.
	Spec []byte
	// Endpoints are the ones to keep, in the order the requests should run.
	Endpoints []domain.EndpointRef
}

// ImportOpenAPI builds a graph scenario from the chosen endpoints and stores it.
//
// The notes say what could not be carried over from the specification, for the
// endpoints that were chosen.
func (s *Service) ImportOpenAPI(
	ctx context.Context, who domain.Identity, request OpenAPIImport,
) (domain.Scenario, domain.ScriptReport, []string, error) {
	if err := s.require(ctx, who, domain.PermManageScenarios, request.ProjectID); err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, nil, err
	}

	spec, err := s.specs.Read(request.Spec)
	if err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, nil, err
	}

	chosen, err := chooseEndpoints(spec, request.Endpoints)
	if err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, nil, err
	}

	graph, notes, err := domain.GraphFromEndpoints(chosen)
	if err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, notes, err
	}

	script, err := s.generator.Generate(graph)
	if err != nil {
		return domain.Scenario{}, domain.ScriptReport{}, notes, err
	}

	created, report, err := s.Create(ctx, who, NewScenario{
		ProjectID: request.ProjectID,
		Name:      request.Name,
		Script:    script,
		Graph:     &graph,
	})
	if err != nil {
		return domain.Scenario{}, report, notes, err
	}

	s.log.InfoContext(ctx, "scenario imported from an OpenAPI specification",
		"org_id", who.OrgID, "project_id", request.ProjectID,
		"scenario_id", created.ID, "endpoints", len(chosen))

	return created, report, notes, nil
}

// chooseEndpoints finds each reference in the specification, once, in the order
// given. A reference to an endpoint the file does not have is an error: it means
// the file changed between the preview and the import, or the request was made
// by hand.
func chooseEndpoints(spec domain.ImportedSpec, references []domain.EndpointRef) ([]domain.ImportedEndpoint, error) {
	chosen := make([]domain.ImportedEndpoint, 0, len(references))
	alreadyChosen := map[domain.EndpointRef]bool{}

	for _, reference := range references {
		if alreadyChosen[reference] {
			continue
		}
		alreadyChosen[reference] = true

		endpoint, found := spec.EndpointByRef(reference)
		if !found {
			return nil, fmt.Errorf("%w: %s is not in the specification", domain.ErrInvalidSpec, reference)
		}
		chosen = append(chosen, endpoint)
	}

	return chosen, nil
}
