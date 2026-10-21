package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/catalog"
)

// Compile-time proof that this repository satisfies the port the catalog
// service declared.
var _ catalog.Repository = (*CatalogRepository)(nil)

// CatalogRepository persists the endpoints of a project's catalog.
type CatalogRepository struct {
	db *DB
}

// NewCatalogRepository returns a repository over db.
func NewCatalogRepository(db *DB) *CatalogRepository {
	return &CatalogRepository{db: db}
}

// ReplaceSource deletes what the source had and writes the new endpoints, in one
// transaction. The count is taken before the transaction ends, so a project that
// would pass its limit keeps the set it had.
func (r *CatalogRepository) ReplaceSource(
	ctx context.Context, orgID, projectID uuid.UUID, source string, endpoints []domain.CatalogEndpoint,
) error {
	rows, err := catalogRows(orgID, endpoints)
	if err != nil {
		return err
	}

	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		q := sqlc.New(tx)

		if _, err := q.DeleteCatalogSource(ctx, sqlc.DeleteCatalogSourceParams{
			OrgID: orgID, ProjectID: projectID, Source: source,
		}); err != nil {
			return fmt.Errorf("remove the endpoints of the source: %w", err)
		}

		if err := writeCatalogRows(ctx, q, rows); err != nil {
			return err
		}

		total, err := q.CountCatalogEndpoints(ctx, sqlc.CountCatalogEndpointsParams{
			OrgID: orgID, ProjectID: projectID,
		})
		if err != nil {
			return fmt.Errorf("count the endpoints: %w", err)
		}
		if total > domain.MaxCatalogEndpoints {
			// Returning an error rolls the transaction back, which restores
			// what the source had.
			return fmt.Errorf("%w: the catalog would hold %d endpoints, more than the %d one project keeps",
				domain.ErrInvalidCatalog, total, domain.MaxCatalogEndpoints)
		}
		return nil
	})
}

// List returns a project's endpoints.
func (r *CatalogRepository) List(
	ctx context.Context, orgID, projectID uuid.UUID, filter domain.CatalogFilter,
) ([]domain.CatalogEndpoint, error) {
	var found []domain.CatalogEndpoint

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListCatalogEndpoints(ctx, sqlc.ListCatalogEndpointsParams{
			OrgID: orgID, ProjectID: projectID,
			Tag: filter.Tag, Source: filter.Source, Query: filter.Query,
		})
		if err != nil {
			return fmt.Errorf("list the endpoints: %w", err)
		}

		found = make([]domain.CatalogEndpoint, 0, len(rows))
		for _, row := range rows {
			endpoint, err := catalogEndpointOf(row)
			if err != nil {
				return err
			}
			found = append(found, endpoint)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// Delete removes one endpoint of a project.
func (r *CatalogRepository) Delete(ctx context.Context, orgID, projectID, id uuid.UUID) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		removed, err := sqlc.New(tx).DeleteCatalogEndpoint(ctx, sqlc.DeleteCatalogEndpointParams{
			OrgID: orgID, ProjectID: projectID, ID: id,
		})
		if err != nil {
			return fmt.Errorf("remove the endpoint: %w", err)
		}
		if removed == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// DeleteSource removes every endpoint of a source.
func (r *CatalogRepository) DeleteSource(
	ctx context.Context, orgID, projectID uuid.UUID, source string,
) (int, error) {
	var removed int64

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		count, err := sqlc.New(tx).DeleteCatalogSource(ctx, sqlc.DeleteCatalogSourceParams{
			OrgID: orgID, ProjectID: projectID, Source: source,
		})
		if err != nil {
			return fmt.Errorf("remove the endpoints of the source: %w", err)
		}
		removed = count
		return nil
	})
	return int(removed), err
}

// writeCatalogRows sends every row in one batch and reads each answer, so that a
// row the database refuses is reported rather than lost.
func writeCatalogRows(ctx context.Context, q *sqlc.Queries, rows []sqlc.CreateCatalogEndpointParams) error {
	if len(rows) == 0 {
		return nil
	}

	var failure error
	batch := q.CreateCatalogEndpoint(ctx, rows)
	batch.Exec(func(position int, err error) {
		if err != nil && failure == nil {
			failure = fmt.Errorf("write the endpoint %s %s: %w", rows[position].Method, rows[position].Path, err)
		}
	})
	if err := batch.Close(); err != nil && failure == nil {
		failure = fmt.Errorf("write the endpoints: %w", err)
	}
	return failure
}

// catalogRows turns domain endpoints into the rows to copy in. The org_id comes
// from the caller and not from the endpoints: it is the one the transaction is
// scoped to, and a row with another would be refused by row-level security.
func catalogRows(orgID uuid.UUID, endpoints []domain.CatalogEndpoint) ([]sqlc.CreateCatalogEndpointParams, error) {
	rows := make([]sqlc.CreateCatalogEndpointParams, 0, len(endpoints))

	for _, endpoint := range endpoints {
		headers := endpoint.Headers
		if headers == nil {
			headers = map[string]string{}
		}
		encoded, err := json.Marshal(headers)
		if err != nil {
			return nil, fmt.Errorf("encode the headers: %w", err)
		}

		params := endpoint.PathParams
		if params == nil {
			params = []string{}
		}

		rows = append(rows, sqlc.CreateCatalogEndpointParams{
			ID:          endpoint.ID,
			OrgID:       orgID,
			ProjectID:   endpoint.ProjectID,
			Source:      endpoint.Source,
			Method:      endpoint.Method,
			Path:        endpoint.Path,
			Tag:         endpoint.Tag,
			Summary:     endpoint.Summary,
			PathParams:  params,
			Headers:     encoded,
			ContentType: endpoint.ContentType,
			Body:        endpoint.Body,
			BodyTooLong: endpoint.BodyTooLong,
			ImportedBy:  endpoint.ImportedBy,
			CreatedAt:   endpoint.CreatedAt,
		})
	}
	return rows, nil
}

func catalogEndpointOf(row sqlc.CatalogEndpoint) (domain.CatalogEndpoint, error) {
	headers := map[string]string{}
	if err := json.Unmarshal(row.Headers, &headers); err != nil {
		return domain.CatalogEndpoint{}, fmt.Errorf("read the headers of an endpoint: %w", err)
	}
	if len(headers) == 0 {
		headers = nil
	}

	return domain.CatalogEndpoint{
		ImportedEndpoint: domain.ImportedEndpoint{
			Method:      row.Method,
			Path:        row.Path,
			Tag:         row.Tag,
			Summary:     row.Summary,
			PathParams:  row.PathParams,
			Headers:     headers,
			ContentType: row.ContentType,
			Body:        row.Body,
			BodyTooLong: row.BodyTooLong,
		},
		ID:         row.ID,
		ProjectID:  row.ProjectID,
		Source:     row.Source,
		ImportedBy: row.ImportedBy,
		CreatedAt:  row.CreatedAt,
	}, nil
}
