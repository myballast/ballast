package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidCatalog reports an endpoint, or a source name, that Ballast will not
// keep in a project's catalog.
var ErrInvalidCatalog = errors.New("invalid catalog entry")

// Limits on what a catalog holds, so that one import cannot fill a table.
const (
	// MaxCatalogEndpoints is how many endpoints one project may keep in all.
	MaxCatalogEndpoints = 5000
	// maxCatalogHeaders is how many headers one endpoint may carry.
	maxCatalogHeaders = 50
	// maxCatalogHeaderName is the longest header name kept.
	maxCatalogHeaderName = 200
	maxCatalogPath       = 2000
	maxCatalogTag        = 120
	maxCatalogSummary    = 200
)

// CatalogEndpoint is an endpoint a project knows about, read from a
// specification somebody imported.
//
// It holds what a graph node would hold, and nothing a graph node may not: the
// credential headers are refused here, so that the guarantee does not depend on
// the reader that produced the endpoint.
type CatalogEndpoint struct {
	ImportedEndpoint
	ID        uuid.UUID
	ProjectID uuid.UUID
	// Source says which file the endpoint came from. Importing the same source
	// again replaces what it had.
	Source string
	// ImportedBy is nil once the person who imported it has been removed.
	ImportedBy *uuid.UUID
	CreatedAt  time.Time
}

// CatalogFilter narrows a list of catalog endpoints. Empty fields match
// everything.
type CatalogFilter struct {
	// Query is looked for, whatever the case, in the method, the path, the
	// summary and the tag.
	Query string
	Tag   string
	// Source is matched exactly.
	Source string
}

// ValidateCatalogSource refuses a source name Ballast will not store.
func ValidateCatalogSource(source string) error {
	switch {
	case strings.TrimSpace(source) == "":
		return fmt.Errorf("%w: the source has no name", ErrInvalidCatalog)
	case len(source) > maxScenarioNameLength:
		return fmt.Errorf("%w: the source name is longer than %d characters", ErrInvalidCatalog, maxScenarioNameLength)
	}
	return nil
}

// ValidateForCatalog refuses an endpoint the catalog would not keep.
//
// The headers are checked for credentials again here even though the reader
// leaves them out: a catalog is read by everybody who can see the project, and
// the rule must hold for any endpoint that reaches it.
func (e ImportedEndpoint) ValidateForCatalog() error {
	if !httpMethods[e.Method] {
		return fmt.Errorf("%w: %q is not a method a scenario can make", ErrInvalidCatalog, e.Method)
	}
	if !strings.HasPrefix(e.Path, "/") || len(e.Path) > maxCatalogPath {
		return fmt.Errorf("%w: the path of %s %q is not usable", ErrInvalidCatalog, e.Method, shorten(e.Path, 60))
	}
	if len(e.Tag) > maxCatalogTag || len(e.Summary) > maxCatalogSummary {
		return fmt.Errorf("%w: the tag or the summary of %s is too long", ErrInvalidCatalog, e.Ref())
	}
	if len(e.Body) > MaxImportedTextLength {
		return fmt.Errorf("%w: the body of %s is longer than %d characters", ErrInvalidCatalog, e.Ref(), MaxImportedTextLength)
	}
	return e.validateCatalogHeaders()
}

func (e ImportedEndpoint) validateCatalogHeaders() error {
	if len(e.Headers) > maxCatalogHeaders {
		return fmt.Errorf("%w: %s has more than %d headers", ErrInvalidCatalog, e.Ref(), maxCatalogHeaders)
	}

	for name, value := range e.Headers {
		switch {
		case strings.TrimSpace(name) == "" || len(name) > maxCatalogHeaderName:
			return fmt.Errorf("%w: %s has a header whose name is not usable", ErrInvalidCatalog, e.Ref())
		case IsCredentialHeader(name):
			return fmt.Errorf("%w: %s carries the header %s, which is a credential and is never kept",
				ErrInvalidCatalog, e.Ref(), name)
		case len(value) > MaxImportedTextLength:
			return fmt.Errorf("%w: the header %s of %s is longer than %d characters",
				ErrInvalidCatalog, name, e.Ref(), MaxImportedTextLength)
		}
	}
	return nil
}

func shorten(text string, limit int) string {
	characters := []rune(text)
	if len(characters) <= limit {
		return text
	}
	return string(characters[:limit])
}

// Choose finds each reference in the specification, once, in the order given. A
// reference to an endpoint the file does not have is an error: it means the file
// changed between the preview and the import, or the request was made by hand.
func (s ImportedSpec) Choose(references []EndpointRef) ([]ImportedEndpoint, error) {
	chosen := make([]ImportedEndpoint, 0, len(references))
	alreadyChosen := map[EndpointRef]bool{}

	for _, reference := range references {
		if alreadyChosen[reference] {
			continue
		}
		alreadyChosen[reference] = true

		endpoint, found := s.EndpointByRef(reference)
		if !found {
			return nil, fmt.Errorf("%w: %s is not in the specification", ErrInvalidSpec, reference)
		}
		chosen = append(chosen, endpoint)
	}

	return chosen, nil
}
