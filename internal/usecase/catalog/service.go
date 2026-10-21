package catalog

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Service keeps a project's catalog of endpoints.
type Service struct {
	endpoints Repository
	specs     SpecReader
	authz     Authorizer
	clock     Clock
	log       *slog.Logger
}

// NewService wires the service.
func NewService(endpoints Repository, specs SpecReader, authz Authorizer, clock Clock, log *slog.Logger) *Service {
	return &Service{endpoints: endpoints, specs: specs, authz: authz, clock: clock, log: log}
}

// Import is a request to add some endpoints of a specification to a project's
// catalog.
type Import struct {
	ProjectID uuid.UUID
	// Source names the file. Importing a source that already exists replaces
	// what it had.
	Source string
	// Spec is the whole file, read under the same limits as the preview.
	Spec []byte
	// Endpoints are the ones to keep.
	Endpoints []domain.EndpointRef
}

// Import reads the file and replaces the source's endpoints with the chosen
// ones. It returns how many endpoints the source now has.
//
// A choice of nothing is refused rather than read as "empty the source": that
// would be a deletion nobody asked for, and removing a source has a call of its
// own.
func (s *Service) Import(ctx context.Context, who domain.Identity, request Import) (int, error) {
	if err := s.require(ctx, who, domain.PermManageScenarios, request.ProjectID); err != nil {
		return 0, err
	}
	if err := domain.ValidateCatalogSource(request.Source); err != nil {
		return 0, err
	}
	if len(request.Endpoints) == 0 {
		return 0, fmt.Errorf("%w: no endpoint was chosen", domain.ErrInvalidSpec)
	}

	spec, err := s.specs.Read(request.Spec)
	if err != nil {
		return 0, err
	}
	chosen, err := spec.Choose(request.Endpoints)
	if err != nil {
		return 0, err
	}

	entries, err := s.entriesOf(who, request, chosen)
	if err != nil {
		return 0, err
	}

	if err := s.endpoints.ReplaceSource(ctx, who.OrgID, request.ProjectID, request.Source, entries); err != nil {
		return 0, err
	}

	s.log.InfoContext(ctx, "endpoints added to the catalog",
		"org_id", who.OrgID, "project_id", request.ProjectID, "endpoints", len(entries))
	return len(entries), nil
}

// entriesOf turns the chosen endpoints into catalog entries, refusing the first
// one the catalog would not keep.
func (s *Service) entriesOf(
	who domain.Identity, request Import, chosen []domain.ImportedEndpoint,
) ([]domain.CatalogEndpoint, error) {
	now := s.clock.Now()
	entries := make([]domain.CatalogEndpoint, 0, len(chosen))

	for _, endpoint := range chosen {
		if err := endpoint.ValidateForCatalog(); err != nil {
			return nil, err
		}

		importedBy := who.UserID
		entries = append(entries, domain.CatalogEndpoint{
			ImportedEndpoint: endpoint,
			ID:               uuid.New(),
			ProjectID:        request.ProjectID,
			Source:           request.Source,
			ImportedBy:       &importedBy,
			CreatedAt:        now,
		})
	}
	return entries, nil
}

// List returns a project's endpoints.
func (s *Service) List(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, filter domain.CatalogFilter,
) ([]domain.CatalogEndpoint, error) {
	if err := s.require(ctx, who, domain.PermViewProject, projectID); err != nil {
		return nil, err
	}
	return s.endpoints.List(ctx, who.OrgID, projectID, filter)
}

// Delete removes one endpoint from the catalog.
func (s *Service) Delete(ctx context.Context, who domain.Identity, projectID, id uuid.UUID) error {
	if err := s.require(ctx, who, domain.PermManageScenarios, projectID); err != nil {
		return err
	}
	return s.endpoints.Delete(ctx, who.OrgID, projectID, id)
}

// DeleteSource removes every endpoint a source brought, and says how many.
func (s *Service) DeleteSource(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, source string,
) (int, error) {
	if err := s.require(ctx, who, domain.PermManageScenarios, projectID); err != nil {
		return 0, err
	}
	if err := domain.ValidateCatalogSource(source); err != nil {
		return 0, err
	}
	return s.endpoints.DeleteSource(ctx, who.OrgID, projectID, source)
}

func (s *Service) require(
	ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID,
) error {
	allowed, err := s.authz.Can(ctx, who, perm, projectID)
	if err != nil {
		return err
	}
	if !allowed {
		s.log.InfoContext(ctx, "request refused",
			"org_id", who.OrgID, "user_id", who.UserID,
			"project_id", projectID, "permission", int64(perm))
		return domain.ErrForbidden
	}
	return nil
}
