package domain

import (
	"fmt"
	"strings"
)

// Check refuses an analysis whose output still holds a secret of the file. The
// graph and the catalog entries are built from it, so both ask.
func (a CollectionAnalysis) Check() error {
	return a.checkNoSecretIsHeld()
}

// checkNoSecretIsHeld is the last guard. The rules that take a secret out of a
// header, a body or a query are written one place at a time, and a place nobody
// thought of would leak. This looks for every secret value the file held in
// everything about to be written, and refuses the import if one is there.
//
// The error says where, never what: repeating the value would put the secret in
// a log.
func (a CollectionAnalysis) checkNoSecretIsHeld() error {
	for _, request := range a.Requests {
		nodes := append([]Node{}, request.Setters...)
		if request.Node != nil {
			nodes = append(nodes, *request.Node)
		}

		for _, node := range nodes {
			if where := a.heldIn(node); where != "" {
				return fmt.Errorf("%w: a secret of the file would have been copied into %s of %q",
					ErrInvalidCollection, where, shorten(request.Name, 60))
			}
		}
	}

	for _, node := range a.Variables {
		if where := a.heldIn(node); where != "" {
			return fmt.Errorf("%w: a secret of the file would have been copied into %s of a variable",
				ErrInvalidCollection, where)
		}
	}
	return nil
}

type nodeField struct {
	name string
	text string
}

// heldIn names the first field of a node that holds a secret, or nothing.
func (a CollectionAnalysis) heldIn(node Node) string {
	fields := []nodeField{
		{"the path", node.Path}, {"the body", node.Body}, {"the value", node.Value},
		{"the label", node.Label}, {"the folder", node.Group},
	}
	for name, value := range node.Headers {
		fields = append(fields, nodeField{"a header", name + "\n" + value})
	}
	for _, param := range node.Query {
		fields = append(fields, nodeField{"the query", param.Name + "\n" + param.Value})
	}

	for _, field := range fields {
		for _, secret := range a.guarded {
			if strings.Contains(field.text, secret) {
				return field.name
			}
		}
	}
	return ""
}

// Graph builds the scenario: the variables first, then each request in the order
// chosen, each with the setters its path variables need, then the end.
//
// A scenario holds a limited number of nodes, and a collection with more
// requests than fit is refused with the numbers rather than cut: which requests
// to drop is not Ballast's to decide.
func (a CollectionAnalysis) Graph() (ScenarioGraph, error) {
	if err := a.Check(); err != nil {
		return ScenarioGraph{}, err
	}

	var nodes []Node
	nodes = append(nodes, a.Variables...)

	requests := 0
	for _, request := range a.Requests {
		if request.Node == nil {
			continue
		}
		nodes = append(nodes, request.Setters...)
		nodes = append(nodes, *request.Node)
		requests++
	}

	if requests == 0 {
		return ScenarioGraph{}, fmt.Errorf("%w: none of the chosen requests could become a request of a scenario", ErrInvalidCollection)
	}
	if len(nodes)+1 > maxNodesPerGraph {
		return ScenarioGraph{}, fmt.Errorf(
			"%w: the %d requests chosen need %d nodes with their variables, and a scenario holds %d",
			ErrInvalidCollection, requests, len(nodes)+1, maxNodesPerGraph)
	}

	numberNodes(nodes)

	graph := chainNodes(nodes)
	if err := graph.Validate(); err != nil {
		return ScenarioGraph{}, err
	}
	return graph, nil
}

// numberNodes gives each node an identifier: set_1, request_1, set_2, and so on.
func numberNodes(nodes []Node) {
	sets, requests := 0, 0
	for position := range nodes {
		if nodes[position].Kind == NodeSetVariable {
			sets++
			nodes[position].ID = fmt.Sprintf("set_%d", sets)
		} else {
			requests++
			nodes[position].ID = fmt.Sprintf("request_%d", requests)
		}
	}
}

// Endpoints are the converted requests as a catalog keeps them: method, path,
// the folder as the tag, the request's name as the summary.
//
// What a catalog may not hold is left out here as well as refused there: the
// credential headers, and any header named like a secret. A request whose method
// and path an earlier one already has is kept once, and Duplicates says so.
func (a CollectionAnalysis) Endpoints() []ImportedEndpoint {
	seen := map[string]bool{}
	var endpoints []ImportedEndpoint

	for _, request := range a.Requests {
		node := request.Node
		if node == nil {
			continue
		}
		key := node.Method + " " + node.Path
		if seen[key] {
			continue
		}
		seen[key] = true

		endpoints = append(endpoints, ImportedEndpoint{
			Method:      node.Method,
			Path:        node.Path,
			Tag:         node.Group,
			Summary:     node.Label,
			PathParams:  placeholderNames(node.Path),
			Headers:     catalogHeaders(node.Headers),
			ContentType: node.Headers["Content-Type"],
			Body:        node.Body,
		})
	}
	return endpoints
}

// catalogHeaders keeps the headers a catalog may hold.
func catalogHeaders(headers map[string]string) map[string]string {
	kept := map[string]string{}
	for name, value := range headers {
		if strings.EqualFold(name, "Content-Type") || IsCredentialHeader(name) || looksLikeASecretName(name) {
			continue
		}
		kept[name] = value
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// CollectionPreview is what an import shows before anybody chooses: each request
// with what became of it, and what was found across the whole collection.
type CollectionPreview struct {
	Name     string
	Requests []PreviewRequest
	// Secrets are the keys to create if every request were imported.
	Secrets []DetectedSecret
	// Findings are about the collection as a whole, not about one request.
	Findings []ImportFinding
}

// PreviewRequest is one request of a collection, as the choice screen lists it.
type PreviewRequest struct {
	Position int
	Folders  []string
	Name     string
	Method   string
	// Path is the template the request becomes, empty when it was left out.
	Path     string
	HasBody  bool
	LeftOut  bool
	Findings []string
}

// Preview analyses every request, to show what an import would make of each.
func (c ImportedCollection) Preview() (CollectionPreview, error) {
	positions := make([]int, 0, len(c.Requests))
	for _, request := range c.Requests {
		positions = append(positions, request.Position)
	}

	preview := CollectionPreview{Name: c.Name}
	if len(positions) == 0 {
		return preview, nil
	}

	analysis, err := AnalyseCollection(c, positions)
	if err != nil {
		return CollectionPreview{}, err
	}

	byPosition := make(map[int]CollectionRequest, len(c.Requests))
	for _, request := range c.Requests {
		byPosition[request.Position] = request
	}

	for _, analysed := range analysis.Requests {
		source := byPosition[analysed.Position]
		item := PreviewRequest{
			Position: analysed.Position,
			Folders:  source.Folders,
			Name:     analysed.Name,
			Method:   strings.ToUpper(source.Method),
			LeftOut:  analysed.Node == nil,
		}
		if analysed.Node != nil {
			item.Path = analysed.Node.Path
			item.HasBody = analysed.Node.Body != "" || analysed.Node.Headers["Content-Type"] != ""
		}
		for _, finding := range analysed.Findings {
			item.Findings = append(item.Findings, finding.Message)
		}
		preview.Requests = append(preview.Requests, item)
	}

	preview.Secrets = analysis.Secrets
	for _, finding := range analysis.Findings {
		if finding.Request == "" {
			preview.Findings = append(preview.Findings, finding)
		}
	}
	return preview, nil
}
