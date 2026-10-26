package domain

import (
	"errors"
	"strings"
	"testing"
)

// a graph that is valid, which the refusals below each break in one way.
func validGraph() ScenarioGraph {
	return ScenarioGraph{
		Start: "call",
		Nodes: []Node{
			{ID: "call", Kind: NodeHTTPRequest, Method: "GET", Path: "/items"},
			{ID: "done", Kind: NodeEnd},
		},
		Edges: []Edge{{From: "call", To: "done"}},
	}
}

func TestAValidGraphIsAccepted(t *testing.T) {
	if err := validGraph().Validate(); err != nil {
		t.Fatalf("a valid graph was refused: %v", err)
	}
}

// Each case is a graph somebody could draw, and a reason it cannot become a
// script. The message has to name the node, because a canvas with forty nodes
// gives a reader nowhere to start otherwise.
func TestWhatAGraphIsRefusedFor(t *testing.T) {
	cases := map[string]struct {
		graph ScenarioGraph
		says  string
	}{
		"no node at all": {
			graph: ScenarioGraph{Start: "call"},
			says:  "no node",
		},
		"an unknown kind": {
			graph: func() ScenarioGraph {
				g := validGraph()
				g.Nodes[0].Kind = "run_shell"
				return g
			}(),
			says: "run_shell",
		},
		"two nodes sharing an id": {
			graph: func() ScenarioGraph {
				g := validGraph()
				g.Nodes = append(g.Nodes, Node{ID: "call", Kind: NodeEnd})
				return g
			}(),
			says: "call",
		},
		"a start that is not in the graph": {
			graph: func() ScenarioGraph {
				g := validGraph()
				g.Start = "elsewhere"
				return g
			}(),
			says: "elsewhere",
		},
		"an edge to nowhere": {
			graph: func() ScenarioGraph {
				g := validGraph()
				g.Edges = append(g.Edges, Edge{From: "done", To: "ghost"})
				return g
			}(),
			says: "ghost",
		},
		"a node nothing reaches": {
			graph: func() ScenarioGraph {
				g := validGraph()
				g.Nodes = append(g.Nodes, Node{ID: "orphan", Kind: NodeEnd})
				return g
			}(),
			says: "orphan",
		},
		"a condition with one branch": {
			graph: ScenarioGraph{
				Start: "ask",
				Nodes: []Node{
					{ID: "ask", Kind: NodeCondition, Expression: "a == 1"},
					{ID: "done", Kind: NodeEnd},
				},
				Edges: []Edge{{From: "ask", To: "done", When: "true"}},
			},
			says: "ask",
		},
		"a loop with no way out": {
			graph: ScenarioGraph{
				Start: "repeat",
				Nodes: []Node{
					{ID: "repeat", Kind: NodeLoop, Iterations: 3},
					{ID: "done", Kind: NodeEnd},
				},
				Edges: []Edge{{From: "repeat", To: "done", When: "body"}},
			},
			says: "repeat",
		},
		"a loop that repeats more than the ceiling": {
			graph: ScenarioGraph{
				Start: "repeat",
				Nodes: []Node{
					{ID: "repeat", Kind: NodeLoop, Iterations: MaxLoopIterations + 1},
					{ID: "call", Kind: NodeHTTPRequest, Method: "GET", Path: "/items"},
					{ID: "done", Kind: NodeEnd},
				},
				Edges: []Edge{
					{From: "repeat", To: "call", When: "body"},
					{From: "repeat", To: "done", When: "after"},
					{From: "call", To: "repeat"},
				},
			},
			says: "repeat",
		},
		"an expression that is not a comparison": {
			graph: ScenarioGraph{
				Start: "ask",
				Nodes: []Node{
					{ID: "ask", Kind: NodeCondition, Expression: "fetch('http://attacker.test')"},
					{ID: "yes", Kind: NodeEnd},
					{ID: "no", Kind: NodeEnd},
				},
				Edges: []Edge{
					{From: "ask", To: "yes", When: "true"},
					{From: "ask", To: "no", When: "false"},
				},
			},
			says: "ask",
		},
		"a node id that could close a comment": {
			graph: func() ScenarioGraph {
				g := validGraph()
				g.Nodes[0].ID = "call */ evil"
				g.Start = "call */ evil"
				g.Edges[0].From = "call */ evil"
				return g
			}(),
			says: "call */ evil",
		},
		"a secret key that would reconfigure k6": {
			graph: ScenarioGraph{
				Start: "set",
				Nodes: []Node{
					{ID: "set", Kind: NodeSetVariable, Variable: "vus", FromEnv: "K6_VUS"},
					{ID: "done", Kind: NodeEnd},
				},
				Edges: []Edge{{From: "set", To: "done"}},
			},
			says: "K6_VUS",
		},
		"a secret key that would redirect the load": {
			graph: ScenarioGraph{
				Start: "set",
				Nodes: []Node{
					{ID: "set", Kind: NodeSetVariable, Variable: "target", FromEnv: "BASE_URL"},
					{ID: "done", Kind: NodeEnd},
				},
				Edges: []Edge{{From: "set", To: "done"}},
			},
			says: "BASE_URL",
		},
		"a value and a secret at once": {
			graph: ScenarioGraph{
				Start: "set",
				Nodes: []Node{
					{ID: "set", Kind: NodeSetVariable, Variable: "token", Value: "abc", FromEnv: "API_TOKEN"},
					{ID: "done", Kind: NodeEnd},
				},
				Edges: []Edge{{From: "set", To: "done"}},
			},
			says: "set",
		},
		"a placeholder that is never closed": {
			graph: func() ScenarioGraph {
				g := validGraph()
				g.Nodes[0].Headers = map[string]string{"X-Thing": "Bearer {{token"}
				return g
			}(),
			says: "{{",
		},
		"a placeholder naming something that is not a variable": {
			graph: func() ScenarioGraph {
				g := validGraph()
				g.Nodes[0].Path = "/users/{not a name}"
				return g
			}(),
			says: "not a name",
		},
		"a method nobody uses": {
			graph: func() ScenarioGraph {
				g := validGraph()
				g.Nodes[0].Method = "TRACE"
				return g
			}(),
			says: "TRACE",
		},
		"a path with no slash": {
			graph: func() ScenarioGraph {
				g := validGraph()
				g.Nodes[0].Path = "items"
				return g
			}(),
			says: "call",
		},
		"nowhere to end": {
			graph: ScenarioGraph{
				Start: "repeat",
				Nodes: []Node{
					{ID: "repeat", Kind: NodeLoop, Iterations: 2},
					{ID: "call", Kind: NodeHTTPRequest, Method: "GET", Path: "/items"},
				},
				Edges: []Edge{
					{From: "repeat", To: "call", When: "body"},
					{From: "repeat", To: "call", When: "after"},
					{From: "call", To: "repeat"},
				},
			},
			says: "end",
		},
	}

	for what, c := range cases {
		t.Run(what, func(t *testing.T) {
			err := c.graph.Validate()
			if err == nil {
				t.Fatal("the graph was accepted")
			}
			if !errors.Is(err, ErrInvalidGraph) {
				t.Fatalf("refused for the wrong reason: %v", err)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("the message does not mention %q: %v", c.says, err)
			}
		})
	}
}

// A graph that loops without ever passing through its loop node would be walked
// for ever. It is refused rather than left to run out of memory.
func TestAGraphThatNeverEndsIsRefused(t *testing.T) {
	graph := ScenarioGraph{
		Start: "one",
		Nodes: []Node{
			{ID: "one", Kind: NodeSetVariable, Variable: "a", Value: "1"},
			{ID: "two", Kind: NodeSetVariable, Variable: "b", Value: "2"},
			{ID: "done", Kind: NodeEnd},
		},
		Edges: []Edge{
			{From: "one", To: "two"},
			{From: "two", To: "one"},
		},
	}

	err := graph.Validate()
	if err == nil {
		t.Fatal("a graph that never reaches an end was accepted")
	}
	if !errors.Is(err, ErrInvalidGraph) {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

func TestWhatANodesLabelsAndQueryAreRefusedFor(t *testing.T) {
	cases := map[string]struct {
		change func(*Node)
		says   string
	}{
		"a group too long":        {func(n *Node) { n.Group = strings.Repeat("g", maxNodeLabelLength+1) }, "group"},
		"a label too long":        {func(n *Node) { n.Label = strings.Repeat("l", maxNodeLabelLength+1) }, "label"},
		"a nameless parameter":    {func(n *Node) { n.Query = []QueryParam{{Name: " ", Value: "1"}} }, "no name"},
		"a value never closed":    {func(n *Node) { n.Query = []QueryParam{{Name: "a", Value: "{{x"}} }, "never closed"},
		"a value too long":        {func(n *Node) { n.Query = []QueryParam{{Name: "a", Value: strings.Repeat("v", maxNodeTextLength+1)}} }, "longer"},
		"too many parameters":     {func(n *Node) { n.Query = make([]QueryParam, maxQueryParams+1) }, "more than"},
		"a bad variable in value": {func(n *Node) { n.Query = []QueryParam{{Name: "a", Value: "{{no good}}"}} }, "not a usable"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			graph := validGraph()
			c.change(&graph.Nodes[0])

			err := graph.Validate()
			if !errors.Is(err, ErrInvalidGraph) || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("error = %v, wanted ErrInvalidGraph mentioning %q", err, c.says)
			}
		})
	}
}

func TestANodeWithLabelsAndAQueryIsAccepted(t *testing.T) {
	graph := validGraph()
	graph.Nodes[0].Group = "Orders / Admin"
	graph.Nodes[0].Label = "List orders"
	graph.Nodes[0].Query = []QueryParam{{Name: "page", Value: "{{page}}"}, {Name: "q", Value: "a=b"}}

	if err := graph.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// A new graph scenario starts from this, so it has to be a graph the server
// would accept: the person editing it has not drawn anything yet.
func TestTheStarterGraphIsValid(t *testing.T) {
	graph := StarterGraph()

	if err := graph.Validate(); err != nil {
		t.Fatalf("the starter graph is refused: %v", err)
	}
	if len(graph.Nodes) < 2 || graph.Nodes[0].Kind != NodeHTTPRequest || graph.Nodes[len(graph.Nodes)-1].Kind != NodeEnd {
		t.Errorf("the starter graph is not a request followed by an end: %+v", graph.Nodes)
	}
}

// Each caller gets its own: a graph edited in place must not change the next
// scenario's starting point.
func TestTheStarterGraphIsNotShared(t *testing.T) {
	first := StarterGraph()
	first.Nodes[0].Path = "/changed"
	first.Nodes[0].Headers = map[string]string{"X": "y"}

	if second := StarterGraph(); second.Nodes[0].Path == "/changed" || second.Nodes[0].Headers != nil {
		t.Error("two calls share the same nodes")
	}
}

// A graph is stored in the clear, and so is the script written from it. A header
// that carries a credential may therefore only be a reference to a variable,
// which a secret fills at run time: a token typed into the node would sit in the
// database and in every version of the script after it.
func TestACredentialHeaderMustReadAVariable(t *testing.T) {
	with := func(name, value string) ScenarioGraph {
		graph := validGraph()
		graph.Nodes[0].Headers = map[string]string{name: value}
		return graph
	}

	refused := map[string]ScenarioGraph{
		"a bearer token typed in":    with("Authorization", "Bearer abc.def.ghi"),
		"the name in another case":   with("authorization", "Basic dXNlcjpwYXNz"),
		"a cookie typed in":          with("Cookie", "session=abc123"),
		"a proxy credential":         with("Proxy-Authorization", "Basic abc"),
		"an empty credential header": with("Authorization", ""),
		"only a single brace":        with("Authorization", "Bearer {token}"),
	}
	for name, graph := range refused {
		t.Run("refused: "+name, func(t *testing.T) {
			err := graph.Validate()
			if !errors.Is(err, ErrInvalidGraph) {
				t.Fatalf("got %v, wanted ErrInvalidGraph", err)
			}
			if strings.Contains(err.Error(), "abc") || strings.Contains(err.Error(), "dXNl") {
				t.Errorf("the message repeats the credential: %v", err)
			}
		})
	}

	accepted := map[string]ScenarioGraph{
		"a bearer variable":        with("Authorization", "Bearer {{token}}"),
		"a basic variable":         with("Authorization", "Basic {{basic_auth}}"),
		"the whole value":          with("Cookie", "{{session}}"),
		"a header that is not one": with("X-Request-Id", "abc-123"),
	}
	for name, graph := range accepted {
		t.Run("accepted: "+name, func(t *testing.T) {
			if err := graph.Validate(); err != nil {
				t.Fatalf("refused: %v", err)
			}
		})
	}
}

// An extraction reads the body, a header or the status of the response, and
// each says what it needs: a path, a header name, or nothing at all.
func TestWhatAnExtractionNeedsDependsOnWhereItReads(t *testing.T) {
	extract := func(source ExtractSource, from string) ScenarioGraph {
		graph := validGraph()
		graph.Nodes = append([]Node{{ID: "take", Kind: NodeExtractVariable, Variable: "token", Source: source, From: from}}, graph.Nodes...)
		graph.Edges = []Edge{{From: "call", To: "take"}, {From: "take", To: "done"}}
		return graph
	}

	accepted := map[string]ScenarioGraph{
		"no source is the body, as graphs stored before sources were": extract("", "access_token"),
		"a body path":                     extract(ExtractFromBody, "data.session.access_token"),
		"a body path written as JSONPath": extract(ExtractFromBody, "$.data.items.0.id"),
		"a header":                        extract(ExtractFromHeader, "X-Request-Id"),
		"the status needs no path":        extract(ExtractFromStatus, ""),
	}
	for name, graph := range accepted {
		t.Run("accepted: "+name, func(t *testing.T) {
			if err := graph.Validate(); err != nil {
				t.Fatalf("refused: %v", err)
			}
		})
	}

	refused := map[string]struct {
		graph ScenarioGraph
		says  string
	}{
		"an unknown source":        {extract("cookie", "a"), "source"},
		"a body with no path":      {extract(ExtractFromBody, "  "), "extracts from nothing"},
		"a bare dollar":            {extract(ExtractFromBody, "$."), "extracts from nothing"},
		"a header with no name":    {extract(ExtractFromHeader, ""), "extracts from nothing"},
		"a header that is not one": {extract(ExtractFromHeader, "X Id: 1"), "header name"},
		"a status with a path":     {extract(ExtractFromStatus, "code"), "status"},
	}
	for name, c := range refused {
		t.Run("refused: "+name, func(t *testing.T) {
			err := c.graph.Validate()
			if !errors.Is(err, ErrInvalidGraph) {
				t.Fatalf("got %v, wanted ErrInvalidGraph", err)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("the message %q does not say %q", err.Error(), c.says)
			}
		})
	}
}

// The path k6 reads has no dollar: a selector is data.token, not $.data.token.
func TestTheBodyPathIsWhatK6Reads(t *testing.T) {
	cases := map[string]string{
		"data.token":   "data.token",
		"$.data.token": "data.token",
		"$data.token":  "$data.token",
		" $.a.b ":      "a.b",
		"items.0.id":   "items.0.id",
	}
	for written, wanted := range cases {
		node := Node{Kind: NodeExtractVariable, From: written}
		if got := node.BodyPath(); got != wanted {
			t.Errorf("%q reads as %q, wanted %q", written, got, wanted)
		}
	}

	if (Node{Kind: NodeExtractVariable}).Where() != ExtractFromBody {
		t.Error("an extraction with no source does not read the body")
	}
}
