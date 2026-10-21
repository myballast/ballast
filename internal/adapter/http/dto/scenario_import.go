package dto

// OpenAPIPreviewRequest carries the text of a specification.
type OpenAPIPreviewRequest struct {
	// Spec is the whole file, as text: JSON or YAML.
	Spec string `json:"spec"`
}

// OpenAPIEndpoint is one operation of the specification, as the selection
// screen lists it.
type OpenAPIEndpoint struct {
	Method string `json:"method"`
	// Path is the template as the specification wrote it: /users/{id}.
	Path string `json:"path"`
	// Tag is the first tag of the operation, empty when it has none.
	Tag     string `json:"tag"`
	Summary string `json:"summary"`
	// PathParams are the names between braces. A request that has some will
	// need a value for each before it runs.
	PathParams []string `json:"path_params"`
	// HasBody is true when the specification gives the request a body, whether
	// or not it gives an example.
	HasBody bool `json:"has_body"`
	// BodyTooLong is true when an example exists but is too long to keep.
	BodyTooLong bool `json:"body_too_long"`
}

// OpenAPIAuthScheme is a way of authenticating that the specification declares.
// It carries no secret: it says what the requests will need.
type OpenAPIAuthScheme struct {
	Name string `json:"name"`
	// Type is apiKey, http, oauth2, openIdConnect or basic.
	Type   string `json:"type"`
	Detail string `json:"detail"`
}

// OpenAPIPreviewResponse lists what could be imported from a specification.
type OpenAPIPreviewResponse struct {
	Title string `json:"title"`
	// BasePath is the prefix the specification says its API is served under,
	// empty when it says none. The paths do not include it.
	BasePath  string              `json:"base_path"`
	Endpoints []OpenAPIEndpoint   `json:"endpoints"`
	Schemes   []OpenAPIAuthScheme `json:"schemes"`
	Notes     []string            `json:"notes"`
}

// OpenAPIEndpointRef names one endpoint of the specification.
type OpenAPIEndpointRef struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// ImportOpenAPIRequest builds a scenario from some endpoints of a specification.
type ImportOpenAPIRequest struct {
	Name string `json:"name"`
	// Spec is the whole file again; nothing is kept between the preview and
	// the import.
	Spec string `json:"spec"`
	// Endpoints are the ones to keep, in the order the requests should run.
	Endpoints []OpenAPIEndpointRef `json:"endpoints"`
}

// ImportOpenAPIResponse is the scenario that was created, and what could not be
// carried over from the specification.
type ImportOpenAPIResponse struct {
	Scenario Scenario `json:"scenario"`
	Notes    []string `json:"notes"`
}
