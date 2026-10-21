package dto

import "time"

// CatalogEndpoint is one endpoint of a project's catalog.
type CatalogEndpoint struct {
	ID string `json:"id"`
	// Source names the file the endpoint was imported from.
	Source string `json:"source"`
	Method string `json:"method"`
	// Path is a template, as the specification wrote it: /users/{id}.
	Path    string `json:"path"`
	Tag     string `json:"tag"`
	Summary string `json:"summary"`
	// PathParams are the names between braces, as written.
	PathParams []string `json:"path_params"`
	// Headers holds the headers the specification gave a value for. Never a
	// credential.
	Headers     map[string]string `json:"headers"`
	ContentType string            `json:"content_type"`
	// Body is the example body, empty when the specification gave none or it
	// was too long to keep.
	Body        string    `json:"body"`
	BodyTooLong bool      `json:"body_too_long"`
	CreatedAt   time.Time `json:"created_at"`
}

// CatalogListResponse is returned by the list of a project's endpoints.
type CatalogListResponse struct {
	Endpoints []CatalogEndpoint `json:"endpoints"`
}

// CatalogImportRequest adds endpoints of a specification to the catalog.
type CatalogImportRequest struct {
	// Source names the file. Importing a source that already exists replaces
	// what it had.
	Source string `json:"source"`
	// Spec is the whole file as text, the same one the preview was given.
	Spec string `json:"spec"`
	// Endpoints are the ones to keep.
	Endpoints []OpenAPIEndpointRef `json:"endpoints"`
}

// CatalogImportResponse says how many endpoints the source now has.
type CatalogImportResponse struct {
	Count int `json:"count"`
}

// CatalogRemovedResponse says how many endpoints a deletion removed.
type CatalogRemovedResponse struct {
	Removed int `json:"removed"`
}
