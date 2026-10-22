package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Repository stores catalog endpoints. Declared here, by the code that consumes
// it, and implemented in adapter/postgres.
type Repository interface {
	// ReplaceSource writes the endpoints of one source in place of whatever that
	// source had, in one transaction: a reader sees the old set or the new one,
	// never half of each. Other sources are untouched. It refuses, and writes
	// nothing, when the project would end up with more than
	// domain.MaxCatalogEndpoints.
	ReplaceSource(
		ctx context.Context, orgID, projectID uuid.UUID, source string, endpoints []domain.CatalogEndpoint,
	) error
	// List returns a project's endpoints, tagged ones first, then by path and
	// method.
	List(ctx context.Context, orgID, projectID uuid.UUID, filter domain.CatalogFilter) ([]domain.CatalogEndpoint, error)
	// Delete removes one endpoint, and reports ErrNotFound for one this project
	// does not have.
	Delete(ctx context.Context, orgID, projectID, id uuid.UUID) error
	// DeleteSource removes every endpoint of a source and says how many it
	// removed.
	DeleteSource(ctx context.Context, orgID, projectID uuid.UUID, source string) (int, error)
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

// Authorizer answers what the caller may do. Every question names a project.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
}

// Clock exists so that the time an endpoint was imported can be controlled in a
// test.
type Clock interface {
	Now() time.Time
}
