package domain

import (
	"errors"
	"strings"
	"testing"
)

// codesOf says which findings a graph has, by node: "island:unreachable".
func codesOf(findings []GraphFinding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.NodeID+":"+string(f.Code))
	}
	return out
}

func has(findings []GraphFinding, node string, code FindingCode) bool {
	for _, f := range findings {
		if f.NodeID == node && f.Code == code {
			return true
		}
	}
	return false
}

func TestAGraphThatIsSoundHasNoFinding(t *testing.T) {
	if findings := validGraph().Check(); len(findings) != 0 {
		t.Fatalf("a sound graph has findings: %v", codesOf(findings))
	}
	if err := StarterGraph().Validate(); err != nil {
		t.Fatalf("the starter graph: %v", err)
	}
}

// The point of a report: everything wrong is said at once, each on its node,
// where one error at a time would send somebody round the edit-and-save loop
// once per fault.
func TestEveryFaultIsReportedAtOnceOnItsOwnNode(t *testing.T) {
	graph := ScenarioGraph{
		Start: "login",
		Nodes: []Node{
			{ID: "login", Kind: NodeHTTPRequest, Method: "POST", Path: "login"}, // no leading slash
			{ID: "each", Kind: NodeLoop, Iterations: 0},                         // no maximum
			{ID: "body", Kind: NodeThinkTime, Seconds: 1},
			{ID: "island", Kind: NodeThinkTime, Seconds: 1}, // wired to nothing
			{ID: "done", Kind: NodeEnd},
		},
		Edges: []Edge{
			{From: "login", To: "each"},
			{From: "each", To: "body", When: "body"},
			{From: "each", To: "done", When: "after"},
			{From: "body", To: "each"},
			{From: "island", To: "done"},
		},
	}

	findings := graph.Check()

	for _, wanted := range []struct {
		node string
		code FindingCode
	}{
		{"login", CodeInvalidNode},
		{"each", CodeInvalidNode},
		{"island", CodeUnreachable},
	} {
		if !has(findings, wanted.node, wanted.code) {
			t.Errorf("missing %s on %q, got %v", wanted.code, wanted.node, codesOf(findings))
		}
	}

	for _, f := range findings {
		if f.Message == "" || f.Err == nil || !errors.Is(f.Err, ErrInvalidGraph) {
			t.Errorf("a finding with no message or no error: %+v", f)
		}
		if f.NodeID != "" && !strings.Contains(f.Message, f.NodeID) {
			t.Errorf("the message %q does not name its node %q", f.Message, f.NodeID)
		}
	}
}

// Only a loop may send the run back: a cycle is bounded by the maximum the loop
// has, and any other is a scenario that never ends.
func TestOnlyALoopMaySendTheRunBack(t *testing.T) {
	bounded := ScenarioGraph{
		Start: "outer",
		Nodes: []Node{
			{ID: "outer", Kind: NodeLoop, Iterations: 3},
			{ID: "inner", Kind: NodeLoop, Iterations: 2},
			{ID: "call", Kind: NodeHTTPRequest, Method: "GET", Path: "/a"},
			{ID: "done", Kind: NodeEnd},
		},
		Edges: []Edge{
			{From: "outer", To: "inner", When: "body"},
			{From: "outer", To: "done", When: "after"},
			{From: "inner", To: "call", When: "body"},
			{From: "inner", To: "outer", When: "after"},
			{From: "call", To: "inner"},
		},
	}
	if findings := bounded.Check(); has(findings, "outer", CodeUnboundedCycle) || has(findings, "inner", CodeUnboundedCycle) || has(findings, "call", CodeUnboundedCycle) {
		t.Errorf("a loop inside a loop was called unbounded: %v", codesOf(findings))
	}

	unbounded := ScenarioGraph{
		Start: "first",
		Nodes: []Node{
			{ID: "first", Kind: NodeHTTPRequest, Method: "GET", Path: "/a"},
			{ID: "pick", Kind: NodeCondition, Expression: "response_status == 200"},
			{ID: "again", Kind: NodeThinkTime, Seconds: 1},
			{ID: "done", Kind: NodeEnd},
		},
		Edges: []Edge{
			{From: "first", To: "pick"},
			{From: "pick", To: "done", When: "true"},
			{From: "pick", To: "again", When: "false"},
			{From: "again", To: "first"}, // back to the start, with no loop to count it
		},
	}
	findings := unbounded.Check()
	for _, id := range []string{"first", "pick", "again"} {
		if !has(findings, id, CodeUnboundedCycle) {
			t.Errorf("%q is on a cycle nothing bounds, got %v", id, codesOf(findings))
		}
	}
	if has(findings, "done", CodeUnboundedCycle) {
		t.Error("the end is not on the cycle")
	}
	if err := unbounded.Validate(); !errors.Is(err, ErrInvalidGraph) {
		t.Errorf("an unbounded cycle was accepted by Validate: %v", err)
	}
}

// A step wired back onto itself is the smallest cycle there is.
func TestAStepThatLeadsToItselfIsACycle(t *testing.T) {
	graph := ScenarioGraph{
		Start: "wait",
		Nodes: []Node{{ID: "wait", Kind: NodeThinkTime, Seconds: 1}, {ID: "done", Kind: NodeEnd}},
		Edges: []Edge{{From: "wait", To: "wait"}},
	}
	if findings := graph.Check(); !has(findings, "wait", CodeUnboundedCycle) {
		t.Errorf("got %v", codesOf(findings))
	}
}

// A path that never reaches an End is said on the graph as a whole, since no node
// is the one to blame.
func TestNoPathToAnEndIsReported(t *testing.T) {
	graph := ScenarioGraph{
		Start: "a",
		Nodes: []Node{
			{ID: "a", Kind: NodeThinkTime, Seconds: 1},
			{ID: "b", Kind: NodeThinkTime, Seconds: 1},
		},
		Edges: []Edge{{From: "a", To: "b"}, {From: "b", To: "a"}},
	}
	findings := graph.Check()
	if !has(findings, "", CodeNoEnd) {
		t.Errorf("no End on the way, and it was not said: %v", codesOf(findings))
	}
}

// A body that goes straight back to its loop repeats nothing.
func TestALoopWithNothingInItIsReportedOnTheLoop(t *testing.T) {
	graph := ScenarioGraph{
		Start: "each",
		Nodes: []Node{{ID: "each", Kind: NodeLoop, Iterations: 3}, {ID: "done", Kind: NodeEnd}},
		Edges: []Edge{{From: "each", To: "each", When: "body"}, {From: "each", To: "done", When: "after"}},
	}
	if findings := graph.Check(); !has(findings, "each", CodeEmptyLoop) {
		t.Errorf("got %v", codesOf(findings))
	}
}

func TestWiringFaultsAreReportedOnTheNodeThatHasThem(t *testing.T) {
	graph := ScenarioGraph{
		Start: "call",
		Nodes: []Node{
			{ID: "call", Kind: NodeHTTPRequest, Method: "GET", Path: "/a"},
			{ID: "pick", Kind: NodeCondition, Expression: "response_status == 200"},
			{ID: "done", Kind: NodeEnd},
		},
		Edges: []Edge{
			{From: "call", To: "pick"},
			{From: "call", To: "done"},                 // a request leads to one place
			{From: "pick", To: "done", When: "true"},   // and no false way out
			{From: "done", To: "call"},                 // an End leads nowhere
			{From: "pick", To: "ghost", When: "false"}, // to a node that is not there
		},
	}
	findings := graph.Check()

	for _, wanted := range []struct {
		node string
		code FindingCode
	}{
		{"call", CodeWrongExits},
		{"done", CodeWrongExits},
		{"ghost", CodeUnknownEdgeNode},
	} {
		if !has(findings, wanted.node, wanted.code) {
			t.Errorf("missing %s on %q, got %v", wanted.code, wanted.node, codesOf(findings))
		}
	}
}

func TestTheStartHasToBeInTheGraph(t *testing.T) {
	graph := validGraph()
	graph.Start = "elsewhere"
	if findings := graph.Check(); !has(findings, "", CodeUnknownStart) {
		t.Errorf("got %v", codesOf(findings))
	}
}

func TestNodesSharingAnIdAreReportedOnThatId(t *testing.T) {
	graph := validGraph()
	graph.Nodes = append(graph.Nodes, Node{ID: "call", Kind: NodeEnd})
	if findings := graph.Check(); !has(findings, "call", CodeDuplicateID) {
		t.Errorf("got %v", codesOf(findings))
	}
}

// The findings come in the order of the graph, the same every time: a list that
// reshuffled between two presses of the button would look like new problems.
func TestFindingsKeepTheOrderOfTheGraph(t *testing.T) {
	graph := ScenarioGraph{
		Start: "a",
		Nodes: []Node{
			{ID: "a", Kind: NodeHTTPRequest, Method: "GET", Path: "/a"},
			{ID: "z", Kind: NodeThinkTime, Seconds: 1},
			{ID: "m", Kind: NodeThinkTime, Seconds: 1},
			{ID: "done", Kind: NodeEnd},
		},
		Edges: []Edge{{From: "a", To: "done"}, {From: "z", To: "done"}, {From: "m", To: "done"}},
	}
	first := strings.Join(codesOf(graph.Check()), ",")
	if first != "z:unreachable,m:unreachable" {
		t.Errorf("the findings are %q", first)
	}
	for i := 0; i < 20; i++ {
		if again := strings.Join(codesOf(graph.Check()), ","); again != first {
			t.Fatalf("the order changed: %q then %q", first, again)
		}
	}
}

func TestValidateStillSaysTheFirstThingThatIsWrong(t *testing.T) {
	graph := validGraph()
	graph.Nodes[0].Path = "no-slash"
	graph.Nodes = append(graph.Nodes, Node{ID: "island", Kind: NodeEnd})

	err := graph.Validate()
	if !errors.Is(err, ErrInvalidGraph) || !strings.Contains(err.Error(), "call") {
		t.Fatalf("got %v", err)
	}
}
