package domain

import "fmt"

// StepKind is one instruction in the flattened scenario.
type StepKind string

const (
	// StepNode is one node of the graph, to be emitted as it is.
	StepNode StepKind = "node"
	// StepIf opens the true branch of a condition, StepElse the false one,
	// and StepEndIf closes both.
	StepIf StepKind = "if"
	// StepElse opens the false branch.
	StepElse StepKind = "else"
	// StepEndIf closes both branches.
	StepEndIf StepKind = "end_if"
	// StepLoop opens a loop and StepEndLoop closes it.
	StepLoop StepKind = "loop"
	// StepEndLoop closes a loop.
	StepEndLoop StepKind = "end_loop"
)

// Step is one instruction. Node carries the node it came from, so whoever
// renders the steps has everything it needs without looking at the graph again.
type Step struct {
	Kind StepKind
	Node Node
}

// maxSteps bounds what one graph may flatten into.
//
// Both branches of a condition are written out in full, so a graph whose
// branches meet again repeats what follows. That is fine for the shapes a
// scenario really has, and this is what stops a pathological one from becoming
// a script nobody can read or store.
const maxSteps = 2000

// Steps flattens the graph into the sequence a generator writes down.
//
// Everything about the shape of the scenario is decided here: which branch
// opens when, where a loop closes, how deep things nest. Whoever renders them
// only has to walk the list, which is what keeps the k6 side free of graph
// logic.
func (g ScenarioGraph) Steps() ([]Step, error) {
	byID := make(map[string]Node, len(g.Nodes))
	for _, node := range g.Nodes {
		byID[node.ID] = node
	}

	walk := &graphWalk{nodes: byID, edges: g.edgesByNode()}

	if err := walk.from(g.Start, ""); err != nil {
		return nil, err
	}

	return walk.steps, nil
}

// edgesByNode groups the edges by the node they leave, so the walk can find
// them without scanning the whole list each time.
func (g ScenarioGraph) edgesByNode() map[string][]Edge {
	grouped := make(map[string][]Edge, len(g.Nodes))
	for _, edge := range g.Edges {
		grouped[edge.From] = append(grouped[edge.From], edge)
	}
	return grouped
}

// graphWalk collects the steps while following the edges.
type graphWalk struct {
	nodes map[string]Node
	edges map[string][]Edge
	steps []Step
}

// from follows the chain starting at id and stops when it reaches stopAt, or
// when there is nowhere left to go.
//
// stopAt is how a loop body ends: the body comes back to its own loop node, and
// that is where the body stops rather than going round for ever.
func (w *graphWalk) from(id, stopAt string) error {
	for id != "" && id != stopAt {
		if len(w.steps) >= maxSteps {
			return fmt.Errorf("%w: the graph does not fit in %d steps; branches that meet again are written out twice",
				ErrInvalidGraph, maxSteps)
		}

		node := w.nodes[id]

		switch node.Kind {
		case NodeEnd:
			w.steps = append(w.steps, Step{Kind: StepNode, Node: node})
			return nil

		case NodeCondition:
			if err := w.condition(node, stopAt); err != nil {
				return err
			}
			return nil

		case NodeLoop:
			if err := w.loop(node); err != nil {
				return err
			}
			id = w.target(node.ID, "after")

		default:
			w.steps = append(w.steps, Step{Kind: StepNode, Node: node})
			id = w.target(node.ID, "")
		}
	}

	return nil
}

// condition writes both branches out in full. Nothing follows an if, because
// each branch runs to its own end.
func (w *graphWalk) condition(node Node, stopAt string) error {
	w.steps = append(w.steps, Step{Kind: StepIf, Node: node})
	if err := w.from(w.target(node.ID, "true"), stopAt); err != nil {
		return err
	}

	w.steps = append(w.steps, Step{Kind: StepElse, Node: node})
	if err := w.from(w.target(node.ID, "false"), stopAt); err != nil {
		return err
	}

	w.steps = append(w.steps, Step{Kind: StepEndIf, Node: node})
	return nil
}

// loop writes the body between the two markers. The body has to come back to
// the loop it belongs to: a body that wanders off is a loop that never repeats,
// and it would read on the canvas as if it did.
func (w *graphWalk) loop(node Node) error {
	before := len(w.steps)

	w.steps = append(w.steps, Step{Kind: StepLoop, Node: node})
	if err := w.from(w.target(node.ID, "body"), node.ID); err != nil {
		return err
	}
	w.steps = append(w.steps, Step{Kind: StepEndLoop, Node: node})

	if len(w.steps) == before+2 {
		return fmt.Errorf("%w: the loop %q has an empty body", ErrInvalidGraph, node.ID)
	}

	return nil
}

// target is where the edge labelled when leads, or "" when there is none.
func (w *graphWalk) target(from, when string) string {
	for _, edge := range w.edges[from] {
		if edge.When == when {
			return edge.To
		}
	}
	return ""
}
