package dto

// CollectionPreviewRequest carries the text of a Postman collection and,
// optionally, of an environment file.
type CollectionPreviewRequest struct {
	// Collection is the whole file as text: a Postman collection, v2.0 or v2.1.
	Collection string `json:"collection"`
	// Environment is an exported Postman environment. It may be left out.
	Environment string `json:"environment,omitempty"`
}

// CollectionRequest is one request of the collection, as the choice screen lists
// it.
type CollectionRequest struct {
	// Position is how the request is named when it is chosen. Names repeat,
	// positions do not.
	Position int `json:"position"`
	// Folders is the path of folders the request sits in, outermost first.
	Folders []string `json:"folders"`
	Name    string   `json:"name"`
	Method  string   `json:"method"`
	// Host is the host a HAR request was sent to, empty for other formats.
	Host string `json:"host"`
	// Kind is what a browser used a HAR request for — api, document, script,
	// style, image, font, media or other — empty for other formats.
	Kind string `json:"kind"`
	// Path is the template the request becomes, empty when it was left out.
	Path    string `json:"path"`
	HasBody bool   `json:"has_body"`
	// LeftOut is true when the request cannot become one of a scenario; its
	// findings say why.
	LeftOut  bool     `json:"left_out"`
	Findings []string `json:"findings"`
}

// DetectedSecret is a secret the import found. It carries no value: the value
// stays in the file, and the person types it again where it belongs.
type DetectedSecret struct {
	// Key is the environment key the scenario reads it from.
	Key string `json:"key"`
	// Variable is the name it has in the scenario.
	Variable string `json:"variable"`
}

// ImportFinding is one thing the import could not carry over, or changed.
type ImportFinding struct {
	// Request is the name of the request, empty when it is about the collection.
	Request string `json:"request"`
	Message string `json:"message"`
}

// CollectionPreviewResponse lists what could be imported from a collection.
type CollectionPreviewResponse struct {
	Name     string              `json:"name"`
	Requests []CollectionRequest `json:"requests"`
	// Secrets are the keys to create if every request were imported.
	Secrets  []DetectedSecret `json:"secrets"`
	Findings []ImportFinding  `json:"findings"`
	// VariableNodes is how many nodes the variables would take at the start of a
	// scenario, which leaves that much less room for requests.
	VariableNodes int `json:"variable_nodes"`
}

// ImportCollectionRequest builds a scenario from some requests of a collection.
type ImportCollectionRequest struct {
	Name        string `json:"name"`
	Collection  string `json:"collection"`
	Environment string `json:"environment,omitempty"`
	// Requests are the positions to keep, in the order they should run.
	Requests []int `json:"requests"`
}

// ImportCollectionResponse is the scenario that was created, the secrets to
// create for it, and what could not be carried over.
type ImportCollectionResponse struct {
	Scenario Scenario         `json:"scenario"`
	Secrets  []DetectedSecret `json:"secrets"`
	Findings []ImportFinding  `json:"findings"`
}

// CatalogCollectionImportRequest adds requests of a collection to the catalog.
type CatalogCollectionImportRequest struct {
	// Source names the collection. Importing a source that exists replaces what
	// it had.
	Source      string `json:"source"`
	Collection  string `json:"collection"`
	Environment string `json:"environment,omitempty"`
	Requests    []int  `json:"requests"`
}

// CatalogCollectionImportResponse says how many endpoints the source now has,
// and what could not be carried over.
type CatalogCollectionImportResponse struct {
	Count    int             `json:"count"`
	Findings []ImportFinding `json:"findings"`
}

// HARPreviewRequest carries the text of a HAR file.
type HARPreviewRequest struct {
	// HAR is the whole file as text. At most 25 MB.
	HAR string `json:"har"`
}

// ImportHARRequest builds a scenario from some requests of a HAR file.
type ImportHARRequest struct {
	Name string `json:"name"`
	HAR  string `json:"har"`
	// Requests are the positions to keep, in the order they should run.
	Requests []int `json:"requests"`
}

// CatalogHARImportRequest adds requests of a HAR file to the catalog.
type CatalogHARImportRequest struct {
	// Source names the file. Importing a source that exists replaces what it had.
	Source   string `json:"source"`
	HAR      string `json:"har"`
	Requests []int  `json:"requests"`
}

// CurlPreviewRequest carries pasted cURL commands.
type CurlPreviewRequest struct {
	// Commands is the pasted text: one or several curl commands. At most 256 KB.
	Commands string `json:"commands"`
}

// ImportCurlRequest builds a scenario from the commands.
type ImportCurlRequest struct {
	Name     string `json:"name"`
	Commands string `json:"commands"`
	// Requests are the positions to keep, in the order they should run.
	Requests []int `json:"requests"`
}

// CatalogCurlImportRequest adds requests of the commands to the catalog.
type CatalogCurlImportRequest struct {
	Source   string `json:"source"`
	Commands string `json:"commands"`
	Requests []int  `json:"requests"`
}
