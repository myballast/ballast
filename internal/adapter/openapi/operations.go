package openapi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// maxSummaryLength keeps a summary short enough for a line of a list.
const maxSummaryLength = 200

// methodsInOrder are the methods a scenario can make, in the order they are
// listed for one path. The same order everywhere keeps two readings of one file
// identical.
var methodsInOrder = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"}

// methodsLeftOut are methods a specification may describe that a scenario
// cannot make.
var methodsLeftOut = []string{"options", "trace"}

// specReader holds what one reading of one file needs.
type specReader struct {
	document   map[string]any
	isSwagger2 bool
	references *resolver
	// credentialHeaders are the lower-cased names of the headers the file says
	// carry an API key. An example for one of them is a key.
	credentialHeaders map[string]bool
	notes             []string
}

// read builds the whole result, schemes first: they say which headers are
// credentials, and the operations need to know.
func (r *specReader) read(paths map[string]any) (domain.ImportedSpec, error) {
	schemes, err := r.readSchemes()
	if err != nil {
		return domain.ImportedSpec{}, err
	}

	basePath := r.basePath()
	if basePath != "" {
		r.notes = append(r.notes, fmt.Sprintf(
			"The specification serves its API under %s. Ballast keeps the paths as written, "+
				"so the environment's base URL should end with %s.", basePath, basePath))
	}

	endpoints, err := r.readPaths(paths)
	if err != nil {
		return domain.ImportedSpec{}, err
	}

	return domain.ImportedSpec{
		Title:     shortened(asString(asMap(r.document["info"])["title"]), maxTitleLength),
		Endpoints: endpoints,
		Schemes:   schemes,
		BasePath:  basePath,
		Notes:     r.notes,
	}, nil
}

// readPaths walks every path item and collects the operations a scenario can
// make.
func (r *specReader) readPaths(paths map[string]any) ([]domain.ImportedEndpoint, error) {
	var endpoints []domain.ImportedEndpoint
	leftOut := 0

	for _, path := range sortedKeys(paths) {
		// Keys such as x-internal are extensions, not paths.
		if !strings.HasPrefix(path, "/") {
			continue
		}

		item, err := r.references.resolveMap(paths[path])
		if err != nil {
			return nil, err
		}

		for _, method := range methodsInOrder {
			operation := asMap(item[strings.ToLower(method)])
			if operation == nil {
				continue
			}

			endpoint, err := r.endpointOf(method, path, item, operation)
			if err != nil {
				return nil, err
			}
			endpoints = append(endpoints, endpoint)
		}

		for _, method := range methodsLeftOut {
			if asMap(item[method]) != nil {
				leftOut++
			}
		}
	}

	if leftOut > 0 {
		r.notes = append(r.notes, fmt.Sprintf(
			"%d operations (OPTIONS or TRACE) were left out: a scenario makes GET, POST, PUT, PATCH, DELETE and HEAD requests.",
			leftOut))
	}

	sortEndpoints(endpoints)
	return endpoints, nil
}

// endpointOf reads one operation.
func (r *specReader) endpointOf(
	method, path string, item, operation map[string]any,
) (domain.ImportedEndpoint, error) {
	parameters, err := r.parametersOf(item, operation)
	if err != nil {
		return domain.ImportedEndpoint{}, err
	}

	endpoint := domain.ImportedEndpoint{
		Method:     method,
		Path:       path,
		Tag:        firstTag(operation),
		Summary:    summaryOf(operation),
		PathParams: pathParamNames(path),
		Headers:    r.headersOf(parameters),
	}

	body, err := r.bodyOf(operation, parameters)
	if err != nil {
		return domain.ImportedEndpoint{}, err
	}
	endpoint.ContentType = body.contentType
	endpoint.Body = body.text
	endpoint.BodyTooLong = body.tooLong

	return endpoint, nil
}

// parametersOf merges the parameters of the path with those of the operation.
// The operation wins when both name the same one, as the specification says.
func (r *specReader) parametersOf(item, operation map[string]any) ([]map[string]any, error) {
	byKey := map[string]map[string]any{}
	var order []string

	for _, source := range []map[string]any{item, operation} {
		for _, entry := range asList(source["parameters"]) {
			parameter, err := r.references.resolveMap(entry)
			if err != nil {
				return nil, err
			}
			if parameter == nil {
				continue
			}

			key := asString(parameter["in"]) + ":" + strings.ToLower(asString(parameter["name"]))
			if _, seen := byKey[key]; !seen {
				order = append(order, key)
			}
			byKey[key] = parameter
		}
	}

	merged := make([]map[string]any, 0, len(order))
	for _, key := range order {
		merged = append(merged, byKey[key])
	}
	return merged, nil
}

// headersOf collects the headers of an operation with a value to offer.
//
// A required header with no example is kept, empty, so that it shows up in the
// request as something to fill in. An optional one with no example is not worth
// a line. A credential is never kept, whatever the file says it is worth.
func (r *specReader) headersOf(parameters []map[string]any) map[string]string {
	headers := map[string]string{}

	for _, parameter := range parameters {
		if asString(parameter["in"]) != "header" {
			continue
		}

		name := asString(parameter["name"])
		if name == "" || domain.IsCredentialHeader(name) || r.credentialHeaders[strings.ToLower(name)] {
			continue
		}

		value, found := r.exampleText(parameter)
		if !found && parameter["required"] != true {
			continue
		}
		headers[name] = value
	}

	if len(headers) == 0 {
		return nil
	}
	return headers
}

// exampleText finds a value for a parameter: its example, then the first of its
// examples, then its default, then the same on its schema.
func (r *specReader) exampleText(parameter map[string]any) (string, bool) {
	candidates := []any{parameter["example"], parameter["x-example"], parameter["default"]}

	examples := asMap(parameter["examples"])
	if len(examples) > 0 {
		first := sortedKeys(examples)[0]
		if example, err := r.references.resolveMap(examples[first]); err == nil {
			candidates = append(candidates, example["value"])
		}
	}

	if schema, err := r.references.resolveMap(parameter["schema"]); err == nil && schema != nil {
		candidates = append(candidates, schema["example"], schema["default"])
		if values := asList(schema["enum"]); len(values) > 0 {
			candidates = append(candidates, values[0])
		}
	}

	for _, candidate := range candidates {
		if text, isScalar := scalarText(candidate); isScalar {
			return text, true
		}
	}
	return "", false
}

func firstTag(operation map[string]any) string {
	tags := asList(operation["tags"])
	if len(tags) == 0 {
		return ""
	}
	return asString(tags[0])
}

func summaryOf(operation map[string]any) string {
	summary := asString(operation["summary"])
	if summary == "" {
		summary = asString(operation["operationId"])
	}
	return shortened(summary, maxSummaryLength)
}

// pathParamNames lists the names between braces, as written.
func pathParamNames(path string) []string {
	var names []string
	rest := path

	for {
		openAt := strings.Index(rest, "{")
		if openAt < 0 {
			break
		}
		closeAt := strings.Index(rest[openAt:], "}")
		if closeAt < 0 {
			break
		}
		closeAt += openAt

		names = append(names, rest[openAt+1:closeAt])
		rest = rest[closeAt+1:]
	}

	return names
}

// basePath is the path prefix the file says it is served under, or nothing.
func (r *specReader) basePath() string {
	if r.isSwagger2 {
		return cleanBasePath(asString(r.document["basePath"]))
	}

	servers := asList(r.document["servers"])
	if len(servers) == 0 {
		return ""
	}
	return basePathOfServer(asString(asMap(servers[0])["url"]))
}

// basePathOfServer keeps the path of a server address: https://host/v1 gives
// /v1. An address with variables in it gives nothing, since the path is not
// known until somebody chooses them.
func basePathOfServer(address string) string {
	if strings.Contains(address, "{") {
		return ""
	}

	if schemeEnd := strings.Index(address, "://"); schemeEnd >= 0 {
		afterScheme := address[schemeEnd+3:]
		pathStart := strings.Index(afterScheme, "/")
		if pathStart < 0 {
			return ""
		}
		address = afterScheme[pathStart:]
	}

	return cleanBasePath(address)
}

func cleanBasePath(path string) string {
	path = strings.TrimRight(path, "/")
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	return path
}

// sortEndpoints puts the tagged endpoints first, by tag, then by path and by
// method. Always the same order for the same file.
func sortEndpoints(endpoints []domain.ImportedEndpoint) {
	sort.SliceStable(endpoints, func(i, j int) bool {
		a, b := endpoints[i], endpoints[j]

		if (a.Tag == "") != (b.Tag == "") {
			return a.Tag != ""
		}
		if a.Tag != b.Tag {
			return a.Tag < b.Tag
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return methodPosition(a.Method) < methodPosition(b.Method)
	})
}

func methodPosition(method string) int {
	for position, known := range methodsInOrder {
		if known == method {
			return position
		}
	}
	return len(methodsInOrder)
}
