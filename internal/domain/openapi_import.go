package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidSpec reports an OpenAPI specification Ballast refuses to read. The
// message says which limit it crossed or what it could not make sense of, and
// it never repeats an example or a header value from the file.
var ErrInvalidSpec = errors.New("invalid OpenAPI specification")

// Limits on what an import reads. Constants of domain, like the ceilings on
// virtual users: the file is chosen by whoever uploads it, so a bound that could
// be raised from the outside would not bound anything.
const (
	// MaxSpecBytes is the largest specification accepted. Real ones are a few
	// hundred kilobytes to a few megabytes.
	MaxSpecBytes = 5 * 1024 * 1024
	// MaxSpecRefs is how many $ref one import may look at in all.
	MaxSpecRefs = 10000
	// MaxSpecRefDepth is how many $ref may point at one another in a row
	// before the import gives up. A chain longer than this is not written by
	// hand.
	MaxSpecRefDepth = 10
)

// MaxImportedTextLength is the longest example the import keeps for a body or a
// header. It is the longest text a node holds, so anything longer could not be
// stored in the graph anyway.
const MaxImportedTextLength = maxNodeTextLength

// maxImportedEndpoints is how many requests one import may turn into nodes.
// One node is kept for the end of the scenario.
const maxImportedEndpoints = maxNodesPerGraph - 1

// ImportedEndpoint is one operation read from a specification, before anybody
// chose to keep it.
type ImportedEndpoint struct {
	Method string
	// Path is a template, as the specification wrote it: /users/{id}.
	Path string
	// Tag is the first tag of the operation, and empty when it has none.
	Tag     string
	Summary string
	// PathParams are the names between braces in Path, in order.
	PathParams []string
	// Headers holds the headers the specification gives a value for. Never a
	// credential: the reader leaves those out.
	Headers map[string]string
	// ContentType and Body come from the request body's example, when there is
	// one. Body is empty when the specification gives none.
	ContentType string
	Body        string
	// BodyTooLong is set when the specification has an example body that is
	// longer than a node holds. Body is then empty.
	BodyTooLong bool
}

// EndpointRef names one endpoint of a specification. It is what a caller sends
// back to say which ones to keep.
type EndpointRef struct {
	Method string
	Path   string
}

// Ref is the reference to this endpoint.
func (e ImportedEndpoint) Ref() EndpointRef {
	return EndpointRef{Method: e.Method, Path: e.Path}
}

// String writes the reference the way people read it: GET /users/{id}.
func (r EndpointRef) String() string {
	return r.Method + " " + r.Path
}

// AuthScheme is a way of authenticating that the specification declares. It
// carries no value: it is there so the person importing knows what the requests
// will need.
type AuthScheme struct {
	Name string
	// Type is apiKey, http, oauth2, openIdConnect, or basic for a 2.0 file.
	Type string
	// Detail says where or how: "header X-API-Key", "bearer", "basic".
	Detail string
}

// ImportedSpec is everything an import reads from a file.
type ImportedSpec struct {
	Title     string
	Endpoints []ImportedEndpoint
	// Schemes are the authentication schemes declared by the file.
	Schemes []AuthScheme
	// BasePath is the path prefix the file says its API is served under, when
	// it says one: /v1. The paths of the endpoints do not include it.
	BasePath string
	// Notes are things the reader left out or changed, in a sentence each.
	Notes []string
}

// EndpointByRef finds an endpoint of the specification.
func (s ImportedSpec) EndpointByRef(ref EndpointRef) (ImportedEndpoint, bool) {
	for _, endpoint := range s.Endpoints {
		if endpoint.Ref() == ref {
			return endpoint, true
		}
	}
	return ImportedEndpoint{}, false
}

// IsCredentialHeader reports whether a header carries a credential by its
// name. An example in a specification for one of these is a real token more
// often than anyone would like, and a graph is stored in the clear.
func IsCredentialHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie":
		return true
	}
	return false
}

// GraphFromEndpoints turns chosen endpoints into a scenario graph: one request
// node each, chained in the order given, then an end node.
//
// The order is the order of the list and nothing more. A specification has no
// journey in it — it lists what exists, not what a user does first — so the
// requests are independent and the person decides how they follow one another.
//
// What cannot be carried over is left out with a note rather than refused: an
// example too long for a node becomes an empty body to fill in. An endpoint
// whose path cannot be a node at all is left out with a note too, since the
// other endpoints are still worth importing.
func GraphFromEndpoints(endpoints []ImportedEndpoint) (ScenarioGraph, []string, error) {
	if len(endpoints) == 0 {
		return ScenarioGraph{}, nil, fmt.Errorf("%w: no endpoint was chosen", ErrInvalidSpec)
	}
	if len(endpoints) > maxImportedEndpoints {
		return ScenarioGraph{}, nil, fmt.Errorf("%w: %d endpoints were chosen, more than the %d one scenario can hold",
			ErrInvalidSpec, len(endpoints), maxImportedEndpoints)
	}

	var notes []string
	var nodes []Node

	for _, endpoint := range endpoints {
		node, nodeNotes := requestNodeOf(endpoint, len(nodes)+1)
		notes = append(notes, nodeNotes...)

		if err := node.validate(); err != nil {
			notes = append(notes, fmt.Sprintf("%s was left out: %v", endpoint.Ref(), err))
			continue
		}
		nodes = append(nodes, node)
	}

	if len(nodes) == 0 {
		return ScenarioGraph{}, notes, fmt.Errorf("%w: none of the chosen endpoints could become a request", ErrInvalidSpec)
	}

	graph := chainNodes(nodes)
	if err := graph.Validate(); err != nil {
		return ScenarioGraph{}, notes, err
	}

	return graph, notes, nil
}

// chainNodes joins the nodes one after the other and closes the scenario with
// an end node.
func chainNodes(nodes []Node) ScenarioGraph {
	end := Node{ID: "end", Kind: NodeEnd}
	nodes = append(nodes, end)

	edges := make([]Edge, 0, len(nodes)-1)
	for i := 0; i < len(nodes)-1; i++ {
		edges = append(edges, Edge{From: nodes[i].ID, To: nodes[i+1].ID})
	}

	return ScenarioGraph{Start: nodes[0].ID, Nodes: nodes, Edges: edges}
}

// requestNodeOf builds the node for one endpoint, dropping what a node cannot
// hold and saying so.
func requestNodeOf(endpoint ImportedEndpoint, position int) (Node, []string) {
	var notes []string

	node := Node{
		ID:     fmt.Sprintf("request_%d", position),
		Kind:   NodeHTTPRequest,
		Method: endpoint.Method,
		Path:   variableSafePath(endpoint.Path),
	}

	headers := map[string]string{}
	for name, value := range endpoint.Headers {
		if reason := headerProblem(value); reason != "" {
			notes = append(notes, fmt.Sprintf("%s: the example for the header %s %s, so it is empty", endpoint.Ref(), name, reason))
			value = ""
		}
		headers[name] = value
	}

	if endpoint.ContentType != "" {
		headers["Content-Type"] = endpoint.ContentType
	}
	if len(headers) > 0 {
		node.Headers = headers
	}

	if endpoint.BodyTooLong {
		notes = append(notes, fmt.Sprintf("%s: the example body is longer than %d characters, so the body is empty",
			endpoint.Ref(), MaxImportedTextLength))
	}

	node.Body = endpoint.Body
	if reason := bodyProblem(endpoint.Body); reason != "" {
		notes = append(notes, fmt.Sprintf("%s: the example body %s, so the body is empty", endpoint.Ref(), reason))
		node.Body = ""
	}

	return node, notes
}

// headerProblem says why a header example cannot be kept, or returns nothing.
func headerProblem(value string) string {
	if len(value) > maxNodeTextLength {
		return fmt.Sprintf("is longer than %d characters", maxNodeTextLength)
	}
	if _, err := ParseTemplate(value); err != nil {
		return "contains an unclosed {{"
	}
	// An absolute address written in a script would reach a host no
	// environment vouched for, and the script inspector refuses it.
	if looksLikeAbsoluteURL(value) {
		return "is an absolute address"
	}
	return ""
}

// bodyProblem says why a body example cannot be kept, or returns nothing.
func bodyProblem(body string) string {
	if len(body) > maxNodeTextLength {
		return fmt.Sprintf("is longer than %d characters", maxNodeTextLength)
	}
	if _, err := ParseTemplate(body); err != nil {
		return "contains an unclosed {{"
	}
	if looksLikeAbsoluteURL(body) {
		return "is an absolute address"
	}
	return ""
}

func looksLikeAbsoluteURL(text string) bool {
	lower := strings.ToLower(text)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// variableSafePath rewrites the names between braces so that each one is a
// usable variable name: {user-id} becomes {user_id}. A path without such names
// comes back unchanged, and so does one whose braces make no sense, which the
// node's own validation then refuses.
func variableSafePath(path string) string {
	var result strings.Builder
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

		result.WriteString(rest[:openAt+1])
		result.WriteString(safeVariableName(rest[openAt+1 : closeAt]))
		result.WriteString("}")
		rest = rest[closeAt+1:]
	}

	result.WriteString(rest)
	return result.String()
}

// safeVariableName replaces every character a variable name cannot hold with an
// underscore, and puts one in front when the name would start with a digit.
func safeVariableName(name string) string {
	if name == "" {
		return name
	}

	var safe strings.Builder
	for _, character := range name {
		isLetter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		isDigit := character >= '0' && character <= '9'
		if isLetter || isDigit || character == '_' {
			safe.WriteRune(character)
		} else {
			safe.WriteRune('_')
		}
	}

	result := safe.String()
	if result[0] >= '0' && result[0] <= '9' {
		result = "_" + result
	}
	return result
}
