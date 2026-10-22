package catalog

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// CollectionImport is a request to add some requests of a collection to a
// project's catalog.
type CollectionImport struct {
	ProjectID uuid.UUID
	// Source names the collection. Importing a source that exists replaces what
	// it had.
	Source string
	// Collection and Environment are the files again, read under the same limits
	// as the preview. The environment may be empty.
	Collection  []byte
	Environment []byte
	// Requests are the ones to keep, by position.
	Requests []int
}

// CollectionResult says how many endpoints the source now has, and what could not
// be carried over.
type CollectionResult struct {
	Count    int
	Findings []domain.ImportFinding
}

// ImportCollection reads a collection and replaces the source's endpoints with
// the chosen requests. Like Import, a choice of nothing is refused.
//
// What a catalog keeps is the method, the path, the folder as a tag and the
// headers and body that are not secrets. The analysis that decides what is a
// secret is the one the scenario import uses, and its final check is made here
// too: nothing is stored from an analysis that still holds a secret.
func (s *Service) ImportCollection(
	ctx context.Context, who domain.Identity, request CollectionImport,
) (CollectionResult, error) {
	if err := s.require(ctx, who, domain.PermManageScenarios, request.ProjectID); err != nil {
		return CollectionResult{}, err
	}
	if err := domain.ValidateCatalogSource(request.Source); err != nil {
		return CollectionResult{}, err
	}
	if len(request.Requests) == 0 {
		return CollectionResult{}, fmt.Errorf("%w: no request was chosen", domain.ErrInvalidCollection)
	}

	collection, err := s.collections.Read(request.Collection, request.Environment)
	if err != nil {
		return CollectionResult{}, err
	}

	analysis, err := domain.AnalyseCollection(collection, request.Requests)
	if err != nil {
		return CollectionResult{}, err
	}
	if err := analysis.Check(); err != nil {
		return CollectionResult{}, err
	}

	endpoints := analysis.Endpoints()
	if len(endpoints) == 0 {
		return CollectionResult{}, fmt.Errorf("%w: none of the chosen requests could become an endpoint", domain.ErrInvalidCollection)
	}

	count, err := s.replaceSource(ctx, who, request.ProjectID, request.Source, endpoints)
	if err != nil {
		return CollectionResult{}, err
	}

	findings := append(append([]domain.ImportFinding{}, analysis.Findings...), analysis.Duplicates...)
	return CollectionResult{Count: count, Findings: findings}, nil
}
