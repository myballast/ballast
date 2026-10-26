package handler

import (
	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// graphDTO is the graph as the API shows it. A pure function, tested by
// reflection: a field added to a node and forgotten here would be dropped on
// every save without an error anywhere.
func graphDTO(graph *domain.ScenarioGraph) *dto.ScenarioGraph {
	if graph == nil {
		return nil
	}

	out := dto.ScenarioGraph{
		Start: graph.Start,
		Nodes: make([]dto.GraphNode, 0, len(graph.Nodes)),
		Edges: make([]dto.GraphEdge, 0, len(graph.Edges)),
	}
	for _, node := range graph.Nodes {
		query := make([]dto.GraphQueryParam, 0, len(node.Query))
		for _, param := range node.Query {
			query = append(query, dto.GraphQueryParam{Name: param.Name, Value: param.Value})
		}
		if len(query) == 0 {
			query = nil
		}

		out.Nodes = append(out.Nodes, dto.GraphNode{
			ID: node.ID, Kind: string(node.Kind),
			Method: node.Method, Path: node.Path, Body: node.Body,
			Headers: node.Headers, Query: query,
			Variable: node.Variable, Source: string(node.Source), From: node.From, Value: node.Value, FromEnv: node.FromEnv,
			Expression: node.Expression, Iterations: node.Iterations, Over: node.Over, Item: node.Item,
			Seconds: node.Seconds, SecondsMax: node.SecondsMax,
			Group: node.Group, Label: node.Label, Critical: node.Critical,
		})
	}
	for _, edge := range graph.Edges {
		out.Edges = append(out.Edges, dto.GraphEdge{From: edge.From, To: edge.To, When: edge.When})
	}
	return &out
}

// graphOf is the graph a request sent. The inverse of graphDTO.
func graphOf(graph *dto.ScenarioGraph) *domain.ScenarioGraph {
	if graph == nil {
		return nil
	}

	out := domain.ScenarioGraph{
		Start: graph.Start,
		Nodes: make([]domain.Node, 0, len(graph.Nodes)),
		Edges: make([]domain.Edge, 0, len(graph.Edges)),
	}
	for _, node := range graph.Nodes {
		var query []domain.QueryParam
		for _, param := range node.Query {
			query = append(query, domain.QueryParam{Name: param.Name, Value: param.Value})
		}

		out.Nodes = append(out.Nodes, domain.Node{
			ID: node.ID, Kind: domain.NodeKind(node.Kind),
			Method: node.Method, Path: node.Path, Body: node.Body,
			Headers: node.Headers, Query: query,
			Variable: node.Variable, Source: domain.ExtractSource(node.Source), From: node.From, Value: node.Value, FromEnv: node.FromEnv,
			Expression: node.Expression, Iterations: node.Iterations, Over: node.Over, Item: node.Item,
			Seconds: node.Seconds, SecondsMax: node.SecondsMax,
			Group: node.Group, Label: node.Label, Critical: node.Critical,
		})
	}
	for _, edge := range graph.Edges {
		out.Edges = append(out.Edges, domain.Edge{From: edge.From, To: edge.To, When: edge.When})
	}
	return &out
}
