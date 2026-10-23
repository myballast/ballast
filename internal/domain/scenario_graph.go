package domain

import (
	"errors"
	"fmt"
	"sort"
)

// ErrInvalidGraph reports a scenario graph Ballast refuses to store.
var ErrInvalidGraph = errors.New("invalid scenario graph")

// ScenarioMode says how a scenario version was written.
//
// A scenario in ModeGraph has a script Ballast generated, and that script is
// read only: editing it would make the graph and the code disagree, with
// nothing to say which one a run used.
type ScenarioMode string

// The two ways a scenario can be written.
const (
	ModeCode  ScenarioMode = "code"
	ModeGraph ScenarioMode = "graph"
)

// NodeKind is what a node does. The set is closed: anything else is refused
// when the graph is stored, so the generator never meets a kind it cannot emit.
type NodeKind string

// Every kind of node a graph may hold. Adding one here means teaching the
// generator to write it, which is why the set is closed.
const (
	NodeHTTPRequest     NodeKind = "http_request"
	NodeExtractVariable NodeKind = "extract_variable"
	NodeSetVariable     NodeKind = "set_variable"
	NodeCondition       NodeKind = "condition"
	NodeLoop            NodeKind = "loop"
	NodeThinkTime       NodeKind = "think_time"
	NodeAssertion       NodeKind = "assertion"
	NodeEnd             NodeKind = "end"
)

// MaxLoopIterations is the most times one loop may repeat.
//
// A constant rather than a setting, like the ceilings on virtual users and
// duration: a bound somebody can raise from the outside is a bound an attacker
// raises. A loop of a thousand is already far more than a scenario needs.
const MaxLoopIterations = 1000

// Limits on what a node may carry, so that one scenario cannot produce a script
// too large to store or to read.
const (
	maxNodesPerGraph  = 200
	maxNodeIDLength   = 64
	maxNodeTextLength = 2000
	// maxNodeLabelLength bounds the group and the label of a node, which are
	// shown on the canvas and written as one comment line.
	maxNodeLabelLength = 200
	// maxQueryParams is how many query parameters one request may carry.
	maxQueryParams = 50
)

// Node is one step of a scenario.
//
// One struct for every kind rather than one type each: the fields a kind does
// not use stay empty, and Validate says which ones a kind requires. It keeps
// the JSONB stored on a version readable by eye.
type Node struct {
	ID   string   `json:"id"`
	Kind NodeKind `json:"kind"`

	// Method and Path describe an http_request. Path is a template —
	// /users/{id} — and it is what the request's name tag is built from.
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	// Body is sent as is, after escaping.
	Body string `json:"body,omitempty"`
	// Headers are sent with the request. Sorted before emission, so that two
	// graphs that differ only in map order produce the same script.
	Headers map[string]string `json:"headers,omitempty"`
	// Query is the query string of the request, in the order it is written. It
	// is kept apart from Path so that the request's name tag stays the path
	// template: with a query in it, every page number would be its own metric.
	Query []QueryParam `json:"query,omitempty"`

	// Variable is the name an extract_variable writes, or a set_variable sets.
	Variable string `json:"variable,omitempty"`
	// From is where an extract_variable reads: a JSON path in the response.
	From string `json:"from,omitempty"`
	// Value is what a set_variable stores.
	Value string `json:"value,omitempty"`
	// FromEnv is the environment variable a set_variable reads instead, which
	// is how a scenario uses a secret: the generated script reads __ENV, so the
	// value is never written into a script that anybody can see.
	FromEnv string `json:"from_env,omitempty"`

	// Expression is the test a condition or an assertion makes. It is parsed
	// by the grammar in condition.go, never copied into the script.
	Expression string `json:"expression,omitempty"`

	// Iterations is how many times a loop repeats, bounded by
	// MaxLoopIterations.
	Iterations int `json:"iterations,omitempty"`

	// Seconds is how long a think_time waits.
	Seconds float64 `json:"seconds,omitempty"`

	// Group and Label say where a node came from and what its author called it:
	// the folder of an imported collection, the name of a request in it. They
	// are for people, shown on the canvas and written as a comment in the
	// script; nothing is decided from them.
	Group string `json:"group,omitempty"`
	Label string `json:"label,omitempty"`

	// Critical marks a step the scenario cannot continue without. The
	// generator emits fail() after it.
	Critical bool `json:"critical,omitempty"`
}

// QueryParam is one name and value of a query string. The value may hold
// {{variables}}, like a header value; the name is plain text.
type QueryParam struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Edge joins two nodes. A condition has two outgoing edges, told apart by
// When; every other node has one, with When empty.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	// When labels the edges leaving a condition ("true", "false") or a loop ("body", "after").
	When string `json:"when,omitempty"`
}

// ScenarioGraph is what the canvas, the imports and the text editor all produce.
type ScenarioGraph struct {
	Start string `json:"start"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Validate refuses a graph the generator could not turn into a script, or
// should not.
//
// Everything here is checked before a version is stored, so the generator can
// assume a graph it is given is sound.
func (g ScenarioGraph) Validate() error {
	if len(g.Nodes) == 0 {
		return fmt.Errorf("%w: the graph has no node", ErrInvalidGraph)
	}
	if len(g.Nodes) > maxNodesPerGraph {
		return fmt.Errorf("%w: the graph has %d nodes, more than the %d allowed",
			ErrInvalidGraph, len(g.Nodes), maxNodesPerGraph)
	}

	byID := make(map[string]Node, len(g.Nodes))
	for _, node := range g.Nodes {
		if err := node.validate(); err != nil {
			return err
		}
		if _, seen := byID[node.ID]; seen {
			return fmt.Errorf("%w: two nodes share the id %q", ErrInvalidGraph, node.ID)
		}
		byID[node.ID] = node
	}

	if _, ok := byID[g.Start]; !ok {
		return fmt.Errorf("%w: the start node %q is not in the graph", ErrInvalidGraph, g.Start)
	}

	if err := g.validateEdges(byID); err != nil {
		return err
	}

	if err := g.validateReachability(byID); err != nil {
		return err
	}

	// Last, because it is the strongest check: a graph that cannot be walked
	// into a sequence of steps is a graph no generator could write down. Doing
	// it here means the generator never has to decide whether a graph is sound.
	_, err := g.Steps()
	return err
}

// validateEdges checks that every edge joins two known nodes, and that a
// condition is the only kind with a branch.
func (g ScenarioGraph) validateEdges(byID map[string]Node) error {
	outgoing := make(map[string][]Edge, len(g.Nodes))

	for _, edge := range g.Edges {
		if _, ok := byID[edge.From]; !ok {
			return fmt.Errorf("%w: an edge leaves the unknown node %q", ErrInvalidGraph, edge.From)
		}
		if _, ok := byID[edge.To]; !ok {
			return fmt.Errorf("%w: an edge reaches the unknown node %q", ErrInvalidGraph, edge.To)
		}
		outgoing[edge.From] = append(outgoing[edge.From], edge)
	}

	for id, node := range byID {
		edges := outgoing[id]

		if node.Kind == NodeEnd {
			if len(edges) > 0 {
				return fmt.Errorf("%w: the end node %q has an outgoing edge", ErrInvalidGraph, id)
			}
			continue
		}

		if node.Kind == NodeCondition {
			if err := validateLabelledEdges(id, edges, "true", "false"); err != nil {
				return err
			}
			continue
		}

		if node.Kind == NodeLoop {
			if err := validateLabelledEdges(id, edges, "body", "after"); err != nil {
				return err
			}
			continue
		}

		if len(edges) != 1 {
			return fmt.Errorf("%w: the node %q has %d outgoing edges, wanted 1",
				ErrInvalidGraph, id, len(edges))
		}
		if edges[0].When != "" {
			return fmt.Errorf("%w: the node %q is not a condition but its edge is labelled %q",
				ErrInvalidGraph, id, edges[0].When)
		}
	}

	return nil
}

// validateLabelledEdges checks that a node with two ways out has exactly those
// two, each labelled once.
//
// A condition with only one branch is a condition whose other outcome nobody
// decided; a loop with no way out is a loop the scenario never leaves.
func validateLabelledEdges(id string, edges []Edge, first, second string) error {
	found := map[string]int{}
	for _, edge := range edges {
		if edge.When != first && edge.When != second {
			return fmt.Errorf("%w: an edge leaving %q is labelled %q, wanted %s or %s",
				ErrInvalidGraph, id, edge.When, first, second)
		}
		found[edge.When]++
	}

	if found[first] != 1 || found[second] != 1 {
		return fmt.Errorf("%w: %q needs one %s edge and one %s edge, it has %d edges in all",
			ErrInvalidGraph, id, first, second, len(edges))
	}

	return nil
}

// validateReachability checks that every node can be reached from the start,
// and that at least one end can be reached.
//
// An unreachable node is a step somebody drew and wired to nothing: it would
// vanish from the generated script without a word, and the scenario would not
// do what the canvas shows.
func (g ScenarioGraph) validateReachability(byID map[string]Node) error {
	next := make(map[string][]string, len(g.Nodes))
	for _, edge := range g.Edges {
		next[edge.From] = append(next[edge.From], edge.To)
	}

	seen := map[string]bool{g.Start: true}
	queue := []string{g.Start}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, to := range next[current] {
			if !seen[to] {
				seen[to] = true
				queue = append(queue, to)
			}
		}
	}

	var unreachable []string
	reachesAnEnd := false

	for id, node := range byID {
		if !seen[id] {
			unreachable = append(unreachable, id)
			continue
		}
		if node.Kind == NodeEnd {
			reachesAnEnd = true
		}
	}

	if len(unreachable) > 0 {
		sort.Strings(unreachable)
		return fmt.Errorf("%w: these nodes cannot be reached from the start: %v",
			ErrInvalidGraph, unreachable)
	}
	if !reachesAnEnd {
		return fmt.Errorf("%w: no end node can be reached from the start", ErrInvalidGraph)
	}

	return nil
}
