package domain

import (
	"fmt"
	"strings"
)

// FindingCode says what kind of fault a finding is, stably: the canvas keys on it,
// the message is for people and may be reworded.
type FindingCode string

// The faults a graph can have.
const (
	CodeNoNode          FindingCode = "no_node"
	CodeTooManyNodes    FindingCode = "too_many_nodes"
	CodeDuplicateID     FindingCode = "duplicate_id"
	CodeInvalidNode     FindingCode = "invalid_node"
	CodeUnknownStart    FindingCode = "unknown_start"
	CodeUnknownEdgeNode FindingCode = "unknown_edge_node"
	CodeWrongExits      FindingCode = "wrong_exits"
	CodeUnreachable     FindingCode = "unreachable"
	CodeUnboundedCycle  FindingCode = "unbounded_cycle"
	CodeNoEnd           FindingCode = "no_end"
	CodeEmptyLoop       FindingCode = "empty_loop"
	CodeTooManySteps    FindingCode = "too_many_steps"
	// CodeScriptRefused is for a graph that is sound and whose script is not: the
	// generator failed, or what it wrote does not parse.
	CodeScriptRefused FindingCode = "script_refused"
)

// GraphFinding is one thing wrong with a graph, and where.
type GraphFinding struct {
	// NodeID is the node to show it on, or empty when it is about the graph as a
	// whole.
	NodeID  string
	Code    FindingCode
	Message string
	// Err is the same fault as an error, which is what Validate returns.
	Err error
}

func finding(node string, code FindingCode, format string, args ...any) GraphFinding {
	err := fmt.Errorf("%w: "+format, append([]any{ErrInvalidGraph}, args...)...)
	return GraphFinding{
		NodeID:  node,
		Code:    code,
		Message: strings.TrimPrefix(err.Error(), ErrInvalidGraph.Error()+": "),
		Err:     err,
	}
}

// Check reads the whole graph and says everything that is wrong with it, each fault
// on the node it belongs to, in the order of the graph.
//
// A person looking at forty nodes needs the list, not the first line of it: one
// error at a time sends them round the edit-and-save loop once per fault. The
// checks that depend on others only run when those hold: reachability means
// nothing with no start, and the walk into steps means nothing on a graph that
// has already been found wanting.
func (g ScenarioGraph) Check() []GraphFinding {
	if len(g.Nodes) == 0 {
		return []GraphFinding{finding("", CodeNoNode, "the graph has no node")}
	}
	if len(g.Nodes) > maxNodesPerGraph {
		return []GraphFinding{finding("", CodeTooManyNodes,
			"the graph has %d nodes, more than the %d allowed", len(g.Nodes), maxNodesPerGraph)}
	}

	var found []GraphFinding

	byID := make(map[string]Node, len(g.Nodes))
	for _, node := range g.Nodes {
		if err := node.validate(); err != nil {
			found = append(found, GraphFinding{
				NodeID: node.ID, Code: CodeInvalidNode, Err: err,
				Message: strings.TrimPrefix(err.Error(), ErrInvalidGraph.Error()+": "),
			})
		}
		if _, seen := byID[node.ID]; seen {
			found = append(found, finding(node.ID, CodeDuplicateID, "two nodes share the id %q", node.ID))
			continue
		}
		byID[node.ID] = node
	}

	_, startKnown := byID[g.Start]
	if !startKnown {
		found = append(found, finding("", CodeUnknownStart, "the start node %q is not in the graph", g.Start))
	}

	edgesKnown := true
	outgoing := make(map[string][]Edge, len(g.Nodes))
	for _, edge := range g.Edges {
		_, fromOK := byID[edge.From]
		_, toOK := byID[edge.To]
		if !fromOK {
			edgesKnown = false
			found = append(found, finding(edge.From, CodeUnknownEdgeNode, "an edge leaves the unknown node %q", edge.From))
		}
		if !toOK {
			edgesKnown = false
			found = append(found, finding(edge.To, CodeUnknownEdgeNode, "an edge reaches the unknown node %q", edge.To))
		}
		outgoing[edge.From] = append(outgoing[edge.From], edge)
	}

	for _, node := range g.Nodes {
		if byID[node.ID].Kind != node.Kind {
			continue // a repeated id: the first one is the node
		}
		found = append(found, exitsOf(node, outgoing[node.ID])...)
	}

	// The shape of the whole graph. Both need a start that exists and edges that
	// lead somewhere.
	if startKnown && edgesKnown {
		reached := g.reachable(outgoing)

		for _, node := range g.Nodes {
			if !reached[node.ID] {
				found = append(found, finding(node.ID, CodeUnreachable,
					"the node %q cannot be reached from the start", node.ID))
			}
		}

		reachesAnEnd := false
		for _, node := range g.Nodes {
			if node.Kind == NodeEnd && reached[node.ID] {
				reachesAnEnd = true
			}
		}
		if !reachesAnEnd {
			found = append(found, finding("", CodeNoEnd, "no end node can be reached from the start"))
		}

		found = append(found, g.cycles(byID, outgoing)...)
		found = append(found, g.emptyLoops(outgoing)...)
	}

	// Last, because it is the strongest check: a graph that cannot be walked into a
	// sequence of steps is a graph no generator could write down. Doing it here
	// means the generator never has to decide whether a graph is sound.
	if len(found) == 0 {
		if _, err := g.Steps(); err != nil {
			found = append(found, GraphFinding{
				Code: CodeTooManySteps, Err: err,
				Message: strings.TrimPrefix(err.Error(), ErrInvalidGraph.Error()+": "),
			})
		}
	}

	return found
}

// exitsOf checks the ways out of one node: an End has none, a condition and a loop
// have their two, labelled once each, and anything else has exactly one.
func exitsOf(node Node, edges []Edge) []GraphFinding {
	id := node.ID

	switch node.Kind {
	case NodeEnd:
		if len(edges) > 0 {
			return []GraphFinding{finding(id, CodeWrongExits, "the end node %q has an outgoing edge", id)}
		}
		return nil

	case NodeCondition:
		return labelled(id, edges, "true", "false")

	case NodeLoop:
		return labelled(id, edges, "body", "after")
	}

	if len(edges) != 1 {
		return []GraphFinding{finding(id, CodeWrongExits, "the node %q has %d outgoing edges, wanted 1", id, len(edges))}
	}
	if edges[0].When != "" {
		return []GraphFinding{finding(id, CodeWrongExits,
			"the node %q is not a condition but its edge is labelled %q", id, edges[0].When)}
	}
	return nil
}

// labelled checks that a node with two ways out has exactly those two, each labelled
// once. A condition with only one branch is a condition whose other outcome nobody
// decided; a loop with no way out is a loop the scenario never leaves.
func labelled(id string, edges []Edge, first, second string) []GraphFinding {
	count := map[string]int{}
	for _, edge := range edges {
		if edge.When != first && edge.When != second {
			return []GraphFinding{finding(id, CodeWrongExits,
				"an edge leaving %q is labelled %q, wanted %s or %s", id, edge.When, first, second)}
		}
		count[edge.When]++
	}

	if count[first] != 1 || count[second] != 1 {
		return []GraphFinding{finding(id, CodeWrongExits,
			"%q needs one %s edge and one %s edge, it has %d edges in all", id, first, second, len(edges))}
	}
	return nil
}

// reachable is every node a walk from the start can come to.
func (g ScenarioGraph) reachable(outgoing map[string][]Edge) map[string]bool {
	seen := map[string]bool{g.Start: true}
	queue := []string{g.Start}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range outgoing[current] {
			if !seen[edge.To] {
				seen[edge.To] = true
				queue = append(queue, edge.To)
			}
		}
	}
	return seen
}

// cycles reports every node on a cycle that nothing bounds.
//
// Only a loop may send the run back, by the edge from the end of its body to the
// loop itself: its maximum is what bounds the cycle. Those edges are set aside, and
// whatever still goes round is a scenario that would never finish — an iteration
// that repeats until the run is killed, measuring nothing in particular.
func (g ScenarioGraph) cycles(byID map[string]Node, outgoing map[string][]Edge) []GraphFinding {
	back := map[Edge]bool{}

	for _, node := range g.Nodes {
		if node.Kind != NodeLoop {
			continue
		}

		start := ""
		for _, edge := range outgoing[node.ID] {
			if edge.When == "body" {
				start = edge.To
			}
		}
		if start == "" || start == node.ID {
			continue
		}

		// The body: everything the loop's body can come to before it comes back.
		body := map[string]bool{start: true}
		queue := []string{start}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if current == node.ID {
				continue
			}
			for _, edge := range outgoing[current] {
				if edge.To == node.ID {
					back[edge] = true
				}
				if edge.To != node.ID && !body[edge.To] {
					body[edge.To] = true
					queue = append(queue, edge.To)
				}
			}
		}
	}

	graph := make(map[string][]string, len(byID))
	for _, edge := range g.Edges {
		if !back[edge] {
			graph[edge.From] = append(graph[edge.From], edge.To)
		}
	}

	onCycle := stronglyConnected(g.Nodes, graph)

	var found []GraphFinding
	for _, node := range g.Nodes {
		if onCycle[node.ID] {
			found = append(found, finding(node.ID, CodeUnboundedCycle,
				"the node %q is on a cycle that never ends: only a loop may go back, and it counts", node.ID))
		}
	}
	return found
}

// emptyLoops reports a loop whose body goes straight back to it.
func (g ScenarioGraph) emptyLoops(outgoing map[string][]Edge) []GraphFinding {
	var found []GraphFinding
	for _, node := range g.Nodes {
		if node.Kind != NodeLoop {
			continue
		}
		for _, edge := range outgoing[node.ID] {
			if edge.When == "body" && edge.To == node.ID {
				found = append(found, finding(node.ID, CodeEmptyLoop, "the loop %q has an empty body", node.ID))
			}
		}
	}
	return found
}

// stronglyConnected marks the nodes that lie on a cycle: those in a strongly
// connected component of more than one node, or with an edge to themselves
// (Tarjan's algorithm, without recursion so that a long chain cannot overflow the
// stack).
func stronglyConnected(nodes []Node, next map[string][]string) map[string]bool {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	counter := 0
	marked := map[string]bool{}

	type frame struct {
		id   string
		edge int
	}

	for _, root := range nodes {
		if _, visited := index[root.ID]; visited {
			continue
		}

		work := []frame{{id: root.ID}}
		index[root.ID], low[root.ID] = counter, counter
		counter++
		stack = append(stack, root.ID)
		onStack[root.ID] = true

		for len(work) > 0 {
			top := &work[len(work)-1]

			if top.edge < len(next[top.id]) {
				to := next[top.id][top.edge]
				top.edge++

				if to == top.id {
					marked[to] = true
				}
				if _, visited := index[to]; !visited {
					index[to], low[to] = counter, counter
					counter++
					stack = append(stack, to)
					onStack[to] = true
					work = append(work, frame{id: to})
				} else if onStack[to] && index[to] < low[top.id] {
					low[top.id] = index[to]
				}
				continue
			}

			if low[top.id] == index[top.id] {
				var component []string
				for {
					last := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[last] = false
					component = append(component, last)
					if last == top.id {
						break
					}
				}
				if len(component) > 1 {
					for _, id := range component {
						marked[id] = true
					}
				}
			}

			finished := top.id
			work = work[:len(work)-1]
			if len(work) > 0 {
				parent := &work[len(work)-1]
				if low[finished] < low[parent.id] {
					low[parent.id] = low[finished]
				}
			}
		}
	}

	return marked
}

// GraphRefused is what a run of a graph is refused with when the graph is no longer
// sound: the reasons, so that the person sees what to fix and not only that they
// may not go on.
//
// A graph is sound when it is stored. Rules get added, though, and a graph stored
// under the older ones is checked again at the moment it would cost something.
type GraphRefused struct {
	Findings []GraphFinding
}

func (e *GraphRefused) Error() string {
	if len(e.Findings) == 0 {
		return ErrInvalidGraph.Error()
	}
	more := ""
	if len(e.Findings) > 1 {
		more = fmt.Sprintf(" (and %d more)", len(e.Findings)-1)
	}
	return e.Findings[0].Err.Error() + more
}

// Is makes a refusal an ErrInvalidGraph, whichever way it is asked.
func (e *GraphRefused) Is(target error) bool { return target == ErrInvalidGraph }
