package k6_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// goldenScript compares a generated script against testdata/<name>.js, or
// rewrites it under -update.
//
// The script is what a user reads to understand what their scenario does, so a
// change to it is a change they will see. A reference file makes that change
// show up in a diff rather than in a run.
func goldenScript(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".js")

	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run go test -tags= -run %s ./... -update to create it)", path, err, t.Name())
	}

	if got != string(want) {
		t.Errorf("the generated script differs from %s\n--- got ---\n%s\n--- want ---\n%s",
			path, got, want)
	}
}

// One reference file per kind of node, so that a change to how any single one
// is written shows up on its own line.
func TestEveryKindOfNodeHasAReference(t *testing.T) {
	cases := map[string]domain.ScenarioGraph{
		"node_http_request":     oneNode(domain.Node{ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items"}),
		"node_http_with_body":   oneNode(domain.Node{ID: "call", Kind: domain.NodeHTTPRequest, Method: "POST", Path: "/items", Body: `{"name":"thing"}`, Headers: map[string]string{"Content-Type": "application/json"}}),
		"node_http_critical":    oneNode(domain.Node{ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items", Critical: true}),
		"node_http_with_path":   oneNode(domain.Node{ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/users/{id}/orders/{order}"}),
		"node_set_variable":     oneNode(domain.Node{ID: "set", Kind: domain.NodeSetVariable, Variable: "token", Value: "abc"}),
		"node_extract_variable": oneNode(domain.Node{ID: "read", Kind: domain.NodeExtractVariable, Variable: "token", From: "access_token"}),
		"node_think_time":       oneNode(domain.Node{ID: "wait", Kind: domain.NodeThinkTime, Seconds: 1.5}),
		"node_secret_variable":  oneNode(domain.Node{ID: "token", Kind: domain.NodeSetVariable, Variable: "token", FromEnv: "API_TOKEN"}),
		"node_http_with_header_variable": oneNode(domain.Node{
			ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/orders",
			Headers: map[string]string{"Authorization": "Bearer {{token}}"},
		}),
		"node_http_with_query_and_label": oneNode(domain.Node{
			ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/orders/{id}",
			Group: "Orders", Label: "Read one order",
			Query: []domain.QueryParam{{Name: "expand", Value: "items"}, {Name: "as", Value: "{{who}}"}},
		}),
		"node_assertion": oneNode(domain.Node{ID: "assert", Kind: domain.NodeAssertion, Expression: "status == 200"}),
		"node_assertion_critical": oneNode(domain.Node{
			ID: "assert", Kind: domain.NodeAssertion, Expression: "status == 200", Critical: true,
		}),
	}

	for name, graph := range cases {
		t.Run(name, func(t *testing.T) {
			script, err := k6.GenerateScript(graph)
			if err != nil {
				t.Fatalf("GenerateScript: %v", err)
			}

			goldenScript(t, name, script)
			mustParse(t, script)
		})
	}
}

// Three whole scenarios, because a node on its own says nothing about nesting,
// branching or where a loop closes.
func TestCompleteGraphsHaveAReference(t *testing.T) {
	cases := map[string]domain.ScenarioGraph{
		"graph_login_and_read": completeGraph(),
		"graph_branching":      branchingGraph(),
		"graph_loop":           loopGraph(),
	}

	for name, graph := range cases {
		t.Run(name, func(t *testing.T) {
			if err := graph.Validate(); err != nil {
				t.Fatalf("the test's own graph is invalid: %v", err)
			}

			script, err := k6.GenerateScript(graph)
			if err != nil {
				t.Fatalf("GenerateScript: %v", err)
			}

			goldenScript(t, name, script)
			mustParse(t, script)
		})
	}
}

// oneNode wraps a single node between a start and an end, so a reference file
// shows that node and nothing else.
func oneNode(node domain.Node) domain.ScenarioGraph {
	return domain.ScenarioGraph{
		Start: node.ID,
		Nodes: []domain.Node{node, {ID: "done", Kind: domain.NodeEnd}},
		Edges: []domain.Edge{{From: node.ID, To: "done"}},
	}
}

// completeGraph signs in, reads the token, calls an endpoint with it and
// checks what came back. It is the shape most scenarios have.
func completeGraph() domain.ScenarioGraph {
	return domain.ScenarioGraph{
		Start: "login",
		Nodes: []domain.Node{
			{
				ID: "login", Kind: domain.NodeHTTPRequest,
				Method:   "POST",
				Path:     "/login",
				Body:     `{"user":"tester"}`,
				Headers:  map[string]string{"Content-Type": "application/json"},
				Critical: true,
			},
			{ID: "token", Kind: domain.NodeExtractVariable, Variable: "token", From: "access_token"},
			{ID: "wait", Kind: domain.NodeThinkTime, Seconds: 0.5},
			{
				ID: "orders", Kind: domain.NodeHTTPRequest,
				Method:  "GET",
				Path:    "/users/{user}/orders",
				Headers: map[string]string{"Authorization": "Bearer placeholder"},
			},
			{ID: "ok", Kind: domain.NodeAssertion, Expression: "status == 200"},
			{ID: "done", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{
			{From: "login", To: "token"},
			{From: "token", To: "wait"},
			{From: "wait", To: "orders"},
			{From: "orders", To: "ok"},
			{From: "ok", To: "done"},
		},
	}
}

// branchingGraph takes one path or the other and ends in both.
func branchingGraph() domain.ScenarioGraph {
	return domain.ScenarioGraph{
		Start: "call",
		Nodes: []domain.Node{
			{ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items"},
			{ID: "read", Kind: domain.NodeExtractVariable, Variable: "count", From: "count"},
			{ID: "any", Kind: domain.NodeCondition, Expression: "count > 0"},
			{ID: "first", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items/{count}"},
			{ID: "none", Kind: domain.NodeSetVariable, Variable: "empty", Value: "yes"},
			{ID: "done", Kind: domain.NodeEnd},
			{ID: "stop", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{
			{From: "call", To: "read"},
			{From: "read", To: "any"},
			{From: "any", To: "first", When: "true"},
			{From: "any", To: "none", When: "false"},
			{From: "first", To: "done"},
			{From: "none", To: "stop"},
		},
	}
}

// loopGraph repeats a call a fixed number of times, then stops.
func loopGraph() domain.ScenarioGraph {
	return domain.ScenarioGraph{
		Start: "repeat",
		Nodes: []domain.Node{
			{ID: "repeat", Kind: domain.NodeLoop, Iterations: 5},
			{ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items"},
			{ID: "wait", Kind: domain.NodeThinkTime, Seconds: 0.2},
			{ID: "after", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/summary"},
			{ID: "done", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{
			{From: "repeat", To: "call", When: "body"},
			{From: "repeat", To: "after", When: "after"},
			{From: "call", To: "wait"},
			{From: "wait", To: "repeat"},
			{From: "after", To: "done"},
		},
	}
}
